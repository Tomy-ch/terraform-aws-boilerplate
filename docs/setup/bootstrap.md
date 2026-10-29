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

名前付きプロファイル `tabp-bootstrap`（tabp は terraform-aws-boilerplate の略）を作る。Terraform は make のターゲット（`make init-admin-identity` / `make plan-admin-identity`）から実行し、ターゲットは、資格情報や terraform の振る舞いを変える呼び出し元の環境変数（`AWS_` で始まるもの、`TF_CLI_ARGS*`、`TF_VAR_*`、`TF_LOG*` ほか。一覧の正本は `scripts/tf-local` の `env-unset`）を外したうえで、`AWS_PROFILE=tabp-bootstrap` を渡す。AWS CLI を直接打つ段1 では `AWS_PROFILE=tabp-bootstrap` を自分で付ける。このプロファイルは一時 user と一緒に削除するもので、`default` を汚さず、強い権限が及ぶ範囲もコマンド単位に限定できる。Terraform は `aws login` の認証情報を直接読めるので、`credential_process` は使わない。リージョンは ap-northeast-1 とする。

| 対象 | 必要バージョン |
| --- | --- |
| AWS CLI | 2.32.0 以上 |
| Terraform 本体（S3 backend を使う場合） | 1.15.0 以上 |

S3 backend は Provider と別に、Terraform 本体に内蔵の SDK で認証情報を解決する。このため、S3 backend へ移行した後は Terraform 本体の条件も必要になる。

1. `aws --version`、`terraform version` で上表を満たすことを確認する
2. 環境変数 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY` が設定されていないことを確認する（`env | grep AWS_`）。設定されていると `AWS_PROFILE` より優先される
3. `make aws-login` を実行し、リージョンを聞かれたら `ap-northeast-1` を入力する
4. ブラウザで一時 IAM user のセッションを選ぶ。root のセッションが一覧に出ても選ばない
5. `~/.aws/config` に次のブロックが作られたことを確認する

   ```ini
   [profile tabp-bootstrap]
   login_session = arn:aws:iam::<アカウントID>:user/<一時IAMユーザー名>
   region = ap-northeast-1
   ```

6. Arn が一時 IAM user であることを確認する。Terraform から読めることは、段2 の `make plan-admin-identity` が確かめる

   ```bash
   aws sts get-caller-identity --profile tabp-bootstrap
   ```

認証情報は `~/.aws/login/cache` にキャッシュされる。15分ごとに自動更新され、最長12時間で失効する。失効したら `make aws-login` を再実行する。

AWS Provider または Terraform 本体が `aws login` の認証情報を読めない版である場合は、`credential_process = aws configure export-credentials --profile tabp-bootstrap --format process` を持つ別のプロファイルを作り、`AWS_PROFILE` にはそちらを指定して回避する。make のターゲットは profile を `tabp-bootstrap` に固定しているので、この場合は使えない。

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

## 段1-5

### 段1. AWS CLI で state Bucket を作る

state backend が無い時点では Terraform が state を保存できないので、Bucket だけを AWS CLI で作る（ADR-0602 決定5）。作るのは `modules/s3` が宣言するものと同じ構成に限る —— 版管理、Public Access Block、Object Ownership、既定の暗号化（SSE-S3 と SSE-C の拒否）。Bucket Policy、lifecycle、ログの配送、KMS は CLI で作らない。これらは段2 の plan に追加として現れ、段4 以降で Terraform が作る。

名前と設定は `usecases/state-backend` と `modules/s3` の宣言から決まる。名前は `tabp-admin-tfstate-<アカウントID>-ap-northeast-1-an` で、account regional namespace に作る。タグは付けない（root に `default_tags` が無いので、付けると段2 で差分が出る）。

以下のコマンドは bash で実行する。fish など他のシェルを使っているなら、先に `bash` を起動してから貼り付ける。

1. 変数を用意する。`ACCOUNT_ID` が12桁の数字でなければ `BUCKET` を設定せずにエラーを出すので、その場合は `make aws-login` をやり直してからこの手順を繰り返す

   ```bash
   export AWS_PROFILE=tabp-bootstrap
   REGION=ap-northeast-1
   ACCOUNT_ID=$(aws sts get-caller-identity --region "$REGION" --query Account --output text)
   if [[ $ACCOUNT_ID =~ ^[0-9]{12}$ ]]; then BUCKET="tabp-admin-tfstate-${ACCOUNT_ID}-${REGION}-an"; echo "$BUCKET"; else unset BUCKET; echo "ACCOUNT_ID が12桁ではありません: '$ACCOUNT_ID'" >&2; fi
   ```

2. Bucket を作る。Object Ownership は作成時に `BucketOwnerEnforced` を指定する

   ```bash
   aws s3api create-bucket --bucket "$BUCKET" --region "$REGION" \
     --bucket-namespace account-regional \
     --create-bucket-configuration "LocationConstraint=${REGION}" \
     --object-ownership BucketOwnerEnforced
   ```

3. Public Access Block、版管理、既定の暗号化を設定する

   ```bash
   aws s3api put-public-access-block --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" \
     --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
   aws s3api put-bucket-versioning --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" \
     --versioning-configuration Status=Enabled
   aws s3api put-bucket-encryption --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" \
     --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"},"BlockedEncryptionTypes":{"EncryptionType":["SSE-C"]}}]}'
   ```

4. 設定を読み戻して確かめる

   ```bash
   aws s3api get-public-access-block --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-ownership-controls --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-versioning --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-encryption --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   ```

   Public Access Block の4項目が `true`、所有権が `BucketOwnerEnforced`、版管理が `Enabled`、暗号化が `AES256` で `SSE-C` を拒否していること。1つでも違えば、手順3 をやり直す。4つがすべて一致するまで段2 へ進まない。

名前や namespace を誤ったときは、空のうちに `aws s3api delete-bucket --bucket "$BUCKET"` で消して作り直す。

### 段2. Terraform で取り込み、plan で突合する

`env/admin/identity/` の root が、段1 の Bucket を `import` block で取り込む（ADR-0602 決定6）。backend の `bucket` は block に書かず、init の引数で渡す。`make init-admin-identity` がアカウント ID から Bucket 名を組んで渡す。

段2 では apply しない。state を書くのは段4 のローカルの apply であり、ADR-0601 決定5 の例外は段4 だけに掛かる（ADR-0602 決定9）。plan は lock を取らずに実行する（ADR-0604 決定8）。

1. plan する。`make plan-admin-identity` は先に `make init-admin-identity` を実行し、plan の出力を `tmp/plan/admin-identity.log` に残す。log の1行目は plan した commit である

   ```bash
   make plan-admin-identity
   ```

   make を使えない場合（`scripts/tf-local` を build できない等）は、段1 の手順1 と同じ bash で `BUCKET` を組み、terraform を直接実行する。backend の region（`env/admin/identity/backend.tf`）と provider の region（同 `providers.tf`）が同じであることを先に確かめる

   ```bash
   unset_env=$(env | grep -oE '^((AWS_|TF_CLI_ARGS|TF_VAR_|TF_LOG)[A-Za-z0-9_]*|TF_WORKSPACE|TF_DATA_DIR|TF_CLI_CONFIG_FILE|TF_REATTACH_PROVIDERS)=' | sed 's/=$//; s/^/-u /')
   env $unset_env AWS_PROFILE=tabp-bootstrap \
     terraform -chdir=env/admin/identity init -input=false -lockfile=readonly -backend-config="bucket=${BUCKET}"
   env $unset_env AWS_PROFILE=tabp-bootstrap \
     terraform -chdir=env/admin/identity plan -lock=false -input=false
   ```

   この場合、plan の出力は log に残らない。どの commit で plan したかは `git rev-parse HEAD` で控える

2. 結果が `Plan: 5 to import, 14 to add, 0 to change, 0 to destroy.` であり、追加が次の14件だけであることを確かめる。取り込んだ resource に変更と破棄が0件で、追加が段1 で作らないもの（ADR-0602 決定5）に限られることをもって、段2 の完了とする（ADR-0602 決定8）。追加の件数と一覧は、identity root に resource を足す変更がこの手順とあわせて更新する。一覧を設定と突き合わせる検査は無い

   - `module.state_backend.module.s3.aws_s3_bucket_policy.this`
   - `module.deployment_identity.module.github_oidc.aws_iam_openid_connect_provider.github`
   - `module.deployment_identity.module.github_oidc` の `aws_iam_role.this`、`aws_iam_role_policy.this`、`aws_iam_role_policies_exclusive.this`、`aws_iam_role_policy_attachments_exclusive.this` を、それぞれ `["plan"]`・`["apply"]`・`["apply_identity"]` の3件ずつ
3. Bucket に Policy がまだ無いことを確かめる。段4 の apply は、在る Policy を黙って置き換える。段1 の手順1 と同じ bash で実行する。`make plan-admin-identity` は `ACCOUNT_ID` と `BUCKET` をシェルに残さないので、別のシェルなら先に段1 の手順1 のブロックを実行する

   ```bash
   aws s3api get-bucket-policy --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   ```

   `NoSuchBucketPolicy` のエラーが返ること。Policy が返ったら、誰がなぜ置いたかを確かめるまで段3 へ進まない

差分が出たら、Terraform の宣言か段1 の Bucket のどちらが誤っているかを見る。宣言を直すときも、安全側の値を弱めて差分を消さない（ADR-0501 決定14）。段1 の誤りなら、Bucket を消して段1 からやり直す。

plan の出力にはアカウント ID が入る。Pull Request へ載せるときは、アカウント ID を伏せた要約にする。

### 段3. deployment-identity の値を用意する

`env/<env>/identity/main.tf` の `module "deployment_identity"` が、OIDC provider と plan / apply / apply-identity の3 role を作る（[`usecases/deployment-identity`](../../usecases/deployment-identity/README.md)）。段3 で人が行うのは、その入力の値を自分のリポジトリに合わせることだけである。

1. `github` の4つの値を、role を assume させるリポジトリのものにする。ID は次で得る

   ```bash
   gh api repos/<owner>/<repo> --jq '.owner.id, .id'
   ```

2. リポジトリの OIDC の `sub` が immutable 形式であることを確かめる。`use_immutable_subject` が `true` で、`sub_claim_prefix` が `repo:<owner>@<owner_id>/<repo>@<repo_id>` であること。`false` なら先へ進まない。旧形式の `sub` は unsupported である（`usecases/deployment-identity/README.md`）

   ```bash
   gh api repos/<owner>/<repo>/actions/oidc/customization/sub
   ```

3. 値を変えた commit で Pull Request を開き、段2 の plan をやり直す。件数と一覧が段2 の手順2 のとおりであること

### 段4. 一時 IAM user のローカル権限で apply する

段4 は ADR-0602 決定9・10 の例外であり、記録を残さない実行を認めない。**CI の plan の経路（#185）ができるまで、段4 を実行しない。** 段4 の Pull Request は、merge の前に、その head に対する CI の plan が変更なしを示す必要がある（同 決定10）。その plan を走らせる workflow と environment は #185 が作る。段4 は、#185 の Pull Request の上で行う。

段4 の apply の入口は make に作らない。terraform を直接実行する。以下はすべて段1 の手順1 と同じ bash で実行する。

1. Pull Request の head を checkout し、`git status` が何も示さないことを確かめ、`git rev-parse HEAD` を控える
2. 段1 の手順1 のブロックで `ACCOUNT_ID` と `BUCKET` を用意し、段2 の手順1 の「make を使えない場合」の1行目で `unset_env` を用意する
3. 作る対象がまだ無いことを確かめる。どれかが在れば止まり、誰がなぜ作ったかを確かめる。`import` で取り込まない（ADR-0202 決定7）

   ```bash
   aws iam list-open-id-connect-providers
   for r in plan apply apply-identity; do aws iam get-role --role-name "tabp-admin-ci-${r}"; done
   ```

   1つ目の結果に `token.actions.githubusercontent.com` が無く、3つの `get-role` がすべて `NoSuchEntity` を返すこと

4. plan して、その結果を保存する。件数と一覧が段2 の手順2 のとおりであることを確かめ、アカウント ID を伏せた要約を Pull Request に載せる（ADR-0602 決定10）

   ```bash
   env $unset_env AWS_PROFILE=tabp-bootstrap \
     terraform -chdir=env/admin/identity init -input=false -lockfile=readonly -backend-config="bucket=${BUCKET}"
   (umask 077 && mkdir -p tmp/plan && env $unset_env AWS_PROFILE=tabp-bootstrap \
     terraform -chdir=env/admin/identity plan -lock=false -input=false -out="$PWD/tmp/plan/admin-identity.tfplan")
   ```

5. apply の前の条件を確かめる（ADR-0602 決定10）。手順1 で控えた commit で、plan 以外の required check が成功していること。保護設定が承認者を要求しているなら、その承認を得ていること。満たさなければ apply しない
6. apply の直前に、Bucket に Policy がまだ無いことをもう一度確かめる。段4 の apply は、在る Policy を黙って置き換え、plan はそれを示さない

   ```bash
   aws s3api get-bucket-policy --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   ```

   `NoSuchBucketPolicy` のエラーが返ること

7. 保存した plan を apply し、plan のファイルを消す。apply は lock を取る（ADR-0604 決定8。`-lock=false` を付けない）。plan のファイルは state の値とアカウント ID を含む

   ```bash
   env $unset_env AWS_PROFILE=tabp-bootstrap \
     terraform -chdir=env/admin/identity apply -input=false "$PWD/tmp/plan/admin-identity.tfplan"
   rm -f "$PWD/tmp/plan/admin-identity.tfplan"
   ```

8. apply の後に確かめる
   - `make plan-admin-identity` が `No changes.` を示す。示さなければ、その出力を Pull Request に記録して止まる
   - `aws iam get-open-id-connect-provider --open-id-connect-provider-arn "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/token.actions.githubusercontent.com"` の `ClientIDList` が `sts.amazonaws.com` だけである
   - 3つの role で、`aws iam list-role-policies` が役割の policy 名の1つだけ（`plan` / `apply` / `apply_identity`。role 名と違い、apply-identity の policy 名は下線でつなぐ）を、`aws iam list-attached-role-policies` が空を返す
   - `aws s3api get-bucket-policy` が、TLS でない要求を拒否する1文を返す
9. 権限を確かめる。[deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md) の表の Action が plan と apply-identity の role に許され、plan の role に変更系が無く、apply の role の bootstrap の構成物への変更が拒否されること

   ```bash
   arn() { echo "arn:aws:iam::${ACCOUNT_ID}:role/tabp-admin-ci-$1"; }
   sim() { role=$1 res=$2; shift 2
     aws iam simulate-principal-policy --policy-source-arn "$(arn "$role")" --resource-arns "$res" \
       --action-names "$@" --query 'EvaluationResults[].[EvalActionName,EvalDecision]' --output text; }
   reads="s3:GetBucketAcl s3:GetBucketCORS s3:GetBucketWebsite s3:GetAccelerateConfiguration s3:GetBucketRequestPayment
     s3:GetBucketLogging s3:GetLifecycleConfiguration s3:GetReplicationConfiguration s3:GetBucketObjectLockConfiguration
     s3:GetBucketTagging s3:GetBucketPolicy s3:GetBucketVersioning s3:GetEncryptionConfiguration
     s3:GetBucketPublicAccessBlock s3:GetBucketOwnershipControls"
   key="arn:aws:s3:::${BUCKET}/identity/terraform.tfstate"
   sim plan "arn:aws:s3:::${BUCKET}" $reads
   sim apply-identity "arn:aws:s3:::${BUCKET}" $reads s3:PutBucketPolicy
   sim apply-identity "$key" s3:PutObject
   sim apply-identity "$(arn plan)" iam:PutRolePolicy iam:UpdateAssumeRolePolicy
   sim plan "$key" s3:PutObject
   sim plan "$(arn plan)" iam:PutRolePolicy
   sim apply "$(arn apply-identity)" iam:PutRolePolicy iam:UpdateAssumeRolePolicy
   sim apply "arn:aws:s3:::${BUCKET}" s3:PutBucketPolicy s3:DeleteBucketPolicy
   sim apply "$key" s3:PutObject s3:DeleteObject
   ```

   期待は順に、15行・16行・1行・2行がすべて `allowed`、続く2つがそれぞれ1行の `implicitDeny`、最後の3つが2行・2行・2行ですべて `explicitDeny` であること。行数か判定が1つでも違えば、Pull Request に記録して止まる。`simulate-principal-policy` が評価するのは role の policy だけで、Bucket Policy は入らない。実効の権限は、手順11 の CI の plan が確かめる

10. 実行の記録を Pull Request の本文に残す。実行者（一時 IAM user）、日時、対象アカウント（ID は伏せる）、適用した commit（ADR-0602 決定10）。role の ARN を載せるときも、アカウント ID を伏せる
11. merge の前に、Pull Request の head に対する CI の plan が変更なしを示すことを確かめる（ADR-0602 決定10）。この plan が、immutable 形式の environment の `sub` で role を assume できることの最初の確認になる（[deployment-identity/ADR-0101](../../usecases/deployment-identity/docs/adr/0101-trust-by-immutable-environment-subject.md) 決定7）

途中で失敗したとき:

- lock が残ったら、他に apply が走っていないことを確かめてから `terraform force-unlock <ID>` で外す
- 作られた resource は state に入っている。原因を直した commit を同じ Pull Request に足し、手順4 からやり直す。記録も新しい commit で取り直す
- `EntityAlreadyExists` が返ったら、手順3 に戻る

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
- [ ] `aws sts get-caller-identity --profile tabp-bootstrap` が一時 IAM user を返す
- [ ] primary contact と代替連絡先3種が登録済み
- [ ] Cost Explorer が有効で、Cost Anomaly Detection の monitor が存在する

段2 の終了時:

- [ ] state Bucket が `tabp-admin-tfstate-<アカウントID>-ap-northeast-1-an` の名前で、account regional namespace に在る
- [ ] `make plan-admin-identity` が `5 to import, 14 to add, 0 to change, 0 to destroy` を示し、追加が段2 の手順2 の14件だけだった
- [ ] `get-bucket-policy` が `NoSuchBucketPolicy` を返した
- [ ] plan の要約（アカウント ID を伏せたもの）を Pull Request に載せた

段3 の終了時:

- [ ] `env/<env>/identity/main.tf` の `github` の4つの値が、自分のリポジトリのものである
- [ ] `use_immutable_subject` が `true` で、`sub_claim_prefix` が `repo:<owner>@<owner_id>/<repo>@<repo_id>` である
- [ ] 値を変えた commit で、段2 の plan が段2 の手順2 の件数と一覧を示した

段4 の終了時:

- [ ] OIDC provider と3つの role が、段4 の前に存在しなかった
- [ ] 段4 の plan の要約（アカウント ID を伏せたもの）を Pull Request に載せた
- [ ] apply の前に、その commit で plan 以外の required check が成功し、承認者が要求される設定ではその承認を得ていた
- [ ] apply の直前に `get-bucket-policy` が `NoSuchBucketPolicy` を返した
- [ ] apply の後の `make plan-admin-identity` が `No changes.` を示した
- [ ] provider の audience、role の policy、Bucket Policy の読み戻しが宣言どおりだった
- [ ] `simulate-principal-policy` の行数と判定が、段4 の手順9 の期待どおりだった
- [ ] 実行の記録（実行者、日時、対象アカウント、commit）を Pull Request の本文に残した
- [ ] merge の前に、CI の plan が変更なしを示した

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
