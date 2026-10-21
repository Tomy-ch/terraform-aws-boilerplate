# bootstrap 手順書

## 概要

管理アカウントに `state-backend` と `deployment-identity` の2ユースケースを立ち上げ、以後の変更を CI が apply できる状態にするまでの手順である。管理アカウントを作った直後から始め、次の順に進む。

1. アカウントの準備 —— root と一時 IAM user で、アカウントの設定と bootstrap の実行者を整える
2. 段1-5 —— 内容と順序は [ADR-0602](../adr/0602-bootstrap-and-ci-authentication.md) 決定4 が定める。段4 のローカルからの apply は同 決定9・10 の例外であり、実行の記録は Pull Request の本文に残す（同 決定10）
3. 一時 IAM user の削除 —— 段5 の成功の後に行う

各ユースケースの責務境界と公開契約は、それぞれの README が持つ。本書はそれらを再掲しない。ADR-0602 決定9 の回復の契機でも、同じ手順を踏む。

root でサインインするのは2回だけである。1回目は「root で行う作業」で、root でしか行えない作業と最初の IAM 主体の作成を済ませてサインアウトする。以降は一時 IAM user で作業する。2回目は「一時 IAM user の削除」で行う。

| # | 作業 | 実施主体 | root で行う理由 |
| --- | --- | --- | --- |
| 1-0 | Paid Plan への移行 | root | アカウント作成直後は root 以外の主体が無い |
| 1-1 | root の MFA 登録（2台以上） | root | root の認証情報は root でしか管理できない |
| 1-2 | Billing への IAM アクセス有効化 | root | 「Billing Tasks」に root 専用と明記されている |
| 1-3 | 一時 IAM user の作成 | root | 作業自体は root 専用ではない。IAM 主体が0件のため最初の1つだけ root で作る |
| 1-4 | root アクセスキーの不在確認 | root | root の認証情報の管理に当たる |
| 2-1 | 連絡先と代替連絡先3種の登録 | 一時 IAM user | 不要（資料に root 不要と明記されている） |
| 2-2 | Cost Explorer の有効化 | 一時 IAM user | 不要（1-2 を済ませていれば可） |
| - | 一時 IAM user の削除 | root | 削除の対象を除くと、他に人の主体が無い |

## アカウントの準備

### 事前準備

作業開始前に次の5点を揃える。root サインイン中に準備物が欠けると、root のセッションを長く開いたままにすることになる。

- [ ] MFA デバイスを2台用意する（例: パスキー対応のセキュリティキー2本、またはセキュリティキー1本＋認証アプリ）。root と一時 IAM user で別の MFA を使う場合は、その分も用意する
- [ ] root のメールアドレスとパスワードを手元で参照できる（パスワードマネージャー）
- [ ] 一時 IAM user 用の強いパスワードを生成しておく
- [ ] AWS CLI のバージョンが 2.32.0 以上（`aws --version`）。`aws login` はこれ未満では使えない
- [ ] primary contact に登録する電話番号が、今後も受信できる番号であることを確認する

### root で行う作業

5つの作業を続けて行い、最後にサインアウトする。コンソール上の表記は英語 UI 基準である。

#### 1-0. Paid Plan に移行する

Free plan のアカウントは、使えるサービスと機能が限られ、期間か credit が尽きると閉鎖される。作成時に Paid plan を選んでいれば不要である。

1. [AWS コンソール](https://console.aws.amazon.com/) で「Root user」を選び、root のメールアドレスとパスワードでサインインする
2. [Upgrade plan](https://console.aws.amazon.com/billing/home?#/freetier/upgrade) を開く
3. 内容を確認し、「Upgrade account」を押す

#### 1-1. root の MFA を登録する（2台以上）

2台目は、1台を紛失したときの予備である。root には最大8台まで登録できる。

1. 右上のアカウント名 →「Security credentials」を開く
2. 「Multi-factor authentication (MFA)」→「Assign MFA device」を押す
3. デバイス名を入力し、種類（Passkey or security key / Authenticator app など）を選んで登録する
4. 2台目を同じ手順で登録する。追加の前に MFA での再認証を求められたら、1台目で認証する
5. 一覧に2台以上表示されることを確認する

#### 1-2. Billing and Cost Management への IAM アクセスを有効にする

これをしないと、AdministratorAccess を付けた IAM user でも請求画面が見えず、Cost Explorer も有効化できない。

1. 右上のアカウント名 →「Account」を開く（[Billing の Account ページ](https://console.aws.amazon.com/billing/home#/account)）
2. 「IAM user and role access to Billing information」の「Edit」を押す
3. 「Activate IAM Access」にチェックを入れ、「Update」を押す
4. 有効化されたことを示す表示を確認する

#### 1-3. 一時 IAM user を作る

| 項目 | 設定 |
| --- | --- |
| ポリシー（直接アタッチ） | AdministratorAccess, SignInLocalDevelopmentAccess |
| コンソールアクセス | 有効（カスタムパスワード） |
| MFA | 設定する |
| アクセスキー | 作らない（CLI と Terraform は `aws login`） |

この user は、bootstrap の段1-4 を人の手元から進めるためだけの一時的な主体であり、このリポジトリが Terraform で生成する IAM ではない。そのため、2つの AWS managed policy を付けることは [ADR-0301](../adr/0301-least-privilege-policy.md) 決定8・10（Action wildcard と AWS managed policy を使わない）の対象外とする。段1-4 で作るものが何であるかが事前に確定しない以上、権限を絞った customer managed policy を手で書くと、bootstrap の途中で権限不足に当たる。代わりに、存在する期間を bootstrap の間に限り、終わったら「一時 IAM user の削除」で消す。

1. IAM コンソール →「Users」→「Create user」を押す
2. ユーザー名を入力する（一時用と分かる名前にする）
3. 「Provide user access to the AWS Management Console」にチェックし、「I want to create an IAM user」を選ぶ
4. 「Custom password」を選び、事前に生成したパスワードを入力する
5. 「Permissions options」で「Attach policies directly」を選び、AdministratorAccess と SignInLocalDevelopmentAccess の2つにチェックする
6. 内容を確認して「Create user」を押す
7. 表示されたコンソールのサインイン URL（アカウント ID 入り）を控える
8. 作成したユーザー →「Security credentials」タブ →「Assign MFA device」で MFA を登録する（初回サインイン時に本人が登録してもよい。その場合は 2-0a で行う）
9. 同じタブの「Access keys」が空であることを確認する

#### 1-4. root のアクセスキーが存在しないことを確かめる

1. 右上のアカウント名 →「Security credentials」を開く
2. 「Access keys」の一覧が空であることを確認する
3. キーが1つでもあれば、「Actions」→「Deactivate」→「Delete」で削除する

#### 1-5. root からサインアウトする

右上のアカウント名 →「Sign out」。以後は一時 IAM user で作業し、root のパスワードと MFA デバイスは別々の場所に保管する。

### 一時 IAM user で行う作業

以下はすべて root 不要である。1-2 が済んでいることが前提となる。

#### 2-0a. 初回サインイン

1. 1-3 で控えたサインイン URL を開き、一時 IAM user でサインインする
2. 1-3 で MFA を登録していなければ、右上のユーザー名 →「Security credentials」→「Assign MFA device」で登録し、一度サインアウトして MFA 付きで入り直す

#### 2-0b. aws login 用のプロファイルを作る

名前付きプロファイル `tabp-bootstrap`（tabp は terraform-aws-boilerplate の略）を作り、Terraform は `AWS_PROFILE=tabp-bootstrap` を付けて実行する。このプロファイルは一時 user と一緒に削除するもので、手順はコピペ実行が前提である。このため、毎回付けるコストは問題にならない。`default` を汚さず、強い権限が及ぶ範囲もコマンド単位に限定できる。Terraform は `aws login` の認証情報を直接読めるので、`credential_process` は使わない。リージョンは ap-northeast-1 とする。

| 対象 | 必要バージョン |
| --- | --- |
| AWS CLI | 2.32.0 以上 |
| Terraform 本体（S3 backend を使う場合） | 1.15.0 以上 |

S3 backend は Provider と別に、Terraform 本体に内蔵の SDK で認証情報を解決する。このため、S3 backend へ移行した後は Terraform 本体の条件も必要になる。

1. `aws --version`、`terraform version` で上表を満たすことを確認する
2. 環境変数 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY` が設定されていないことを確認する（`env | grep AWS_`）。設定されていると `AWS_PROFILE` より優先される
3. `aws login --profile tabp-bootstrap` を実行し、リージョンを聞かれたら `ap-northeast-1` を入力する
4. ブラウザで一時 IAM user のセッションを選ぶ。root のセッションが一覧に出ても選ばない
5. `~/.aws/config` に次のブロックが作られたことを確認する

   ```ini
   [profile tabp-bootstrap]
   login_session = arn:aws:iam::<アカウントID>:user/<一時IAMユーザー名>
   region = ap-northeast-1
   ```

6. Arn が一時 IAM user であることを確認する

   ```bash
   aws sts get-caller-identity --profile tabp-bootstrap
   ```

7. provider ブロックと backend ブロックに `profile` を書かない状態で、次が通ることを確認する

   ```bash
   AWS_PROFILE=tabp-bootstrap terraform init
   AWS_PROFILE=tabp-bootstrap terraform plan
   ```

認証情報は `~/.aws/login/cache` にキャッシュされる。15分ごとに自動更新され、最長12時間で失効する。失効したら `aws login --profile tabp-bootstrap` を再実行する。

AWS Provider または Terraform 本体が `aws login` の認証情報を読めない版である場合は、`credential_process = aws configure export-credentials --profile tabp-bootstrap --format process` を持つ別のプロファイルを作り、`AWS_PROFILE` にはそちらを指定して回避する。

#### 2-0c. 1日の作業の終了時

1日の作業を終えたら `aws logout --profile tabp-bootstrap` を実行し、キャッシュを消す。

#### 2-1. 連絡先と代替連絡先3種を登録する

資料では、contact information と alternate contacts は root の認証情報を要しない作業である。

1. 右上のアカウント名 →「Account」を開く
2. 「Contact information」の「Edit」を押し、氏名・住所・電話番号を正確に入力して保存する
3. 「Alternate contacts」の「Edit」を押し、Billing / Operations / Security の3種を登録して保存する

primary contact の電話番号は特に正確にする。root の MFA デバイスを失ったとき、root のサインインはメールアドレスとこの電話番号での本人確認で回復する。

#### 2-2. Cost Explorer を有効にする

Cost Explorer は API では有効化できず、コンソールで初めて開くと有効になる。

1. Billing and Cost Management コンソールを開く
2. ナビゲーションの「Cost Explorer」を選ぶ
3. 「Welcome to Cost Explorer」画面が出たら「Launch Cost Explorer」を押す
4. 当月のデータの準備に約24時間かかるため、翌日以降に表示を確認する

有効化と同時に Cost Anomaly Detection が自動設定され、AWS services monitor と日次サマリーの通知が作られる。翌日以降に Cost Anomaly Detection の「Cost monitors」で monitor が存在すること、通知先メールが意図したアドレスであることを確認する。

## 一時 IAM user の削除

bootstrap の段5（CI による apply）が成功するまでは削除しない。それまでは、段4 をやり直す手段として残しておく。

削除は root で行う。一時 IAM user に自分自身を削除させると、削除の途中で自分の権限と認証情報を外すことになり、途中で止まったときに続きを行える主体が残らない。

1. 手元の端末で `aws logout --all` を実行し、キャッシュされた認証情報をすべて消す
2. `~/.aws/config` から `[profile tabp-bootstrap]` のブロックを削除する
3. root でサインインし、IAM コンソール →「Users」で一時 IAM user を選び、「Delete」で削除する（ログインプロファイル、MFA デバイス、policy のアタッチも併せて外れる）
4. IAM コンソール →「Credential report」でレポートを取得し、次を確認する
   - IAM user が0件である
   - root の行で、アクセスキーが無く、MFA が有効である
5. `~/.aws/credentials` に、このアカウントの静的なアクセスキーが残っていないことを確認する
6. root からサインアウトする

## 完了チェックリスト

アカウントの準備の終了時:

- [ ] アカウントが Paid plan である
- [ ] root に MFA が2台以上登録されている
- [ ] Billing の「IAM user and role access to Billing information」が有効
- [ ] 一時 IAM user に AdministratorAccess と SignInLocalDevelopmentAccess の2つだけが付いている
- [ ] 一時 IAM user に MFA が登録され、アクセスキーが0個
- [ ] root のアクセスキーが0個
- [ ] root からサインアウトし、root の認証情報を保管した
- [ ] `aws sts get-caller-identity --profile tabp-bootstrap` が一時 IAM user を返し、`AWS_PROFILE=tabp-bootstrap terraform plan` が通る
- [ ] primary contact と代替連絡先3種が登録済み
- [ ] Cost Explorer が有効で、Cost Anomaly Detection の monitor が存在する

一時 IAM user の削除の終了時:

- [ ] 資格情報レポートで IAM user が0件、root のアクセスキーが0個、root の MFA が有効
- [ ] 手元に `tabp-bootstrap` のプロファイルとキャッシュ、このアカウントの静的なアクセスキーが残っていない

## 参考資料

- [Tasks that require root user credentials（IAM User Guide）](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_root-user.html#root-user-tasks)
- [Multi-factor authentication for AWS account root user（IAM User Guide）](https://docs.aws.amazon.com/IAM/latest/UserGuide/enable-mfa-for-root.html)
- [AWS Multi-factor authentication in IAM（IAM User Guide）](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_mfa.html)
- [Choosing a plan（AWS Billing）](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/free-tier-plans.html)
- [Overview of managing access permissions（AWS Billing）](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/control-access-billing.html)
- [Getting set up with Billing](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/billing-getting-started.html)
- [SignInLocalDevelopmentAccess（AWS managed policy）](https://docs.aws.amazon.com/aws-managed-policy/latest/reference/SignInLocalDevelopmentAccess.html)
- [Login for AWS local development using console credentials（AWS CLI）](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sign-in.html)
- [Enabling Cost Explorer](https://docs.aws.amazon.com/cost-management/latest/userguide/ce-enable.html)
