# bootstrap 手順書

## 1. 設定の概要

管理アカウントに、Terraform の state の置き場（`state-backend`）と、CI が AWS に入るための role（`deployment-identity`）を作る。これらは CI 自身では作れないので、最初の一度だけ人が作る。

| 実施者 | 担当 |
| --- | --- |
| root の持ち主 | アカウントの準備の一部と、最後の一時 IAM user の削除 |
| 一時 IAM user | それ以外の AWS 側の作業（bootstrap の間だけ使い、最後に削除する） |
| リポジトリの管理者 | GitHub の environment と secret |

管理アカウントを作った直後に行う。段の順序と例外は [ADR-0602](../adr/0602-bootstrap-and-ci-authentication.md) が定める。

> **注意**: 段5 の後は、一時 IAM user は state を読めない。この手順を障害からの回復には使えない（[`usecases/state-backend/README.md`](../../usecases/state-backend/README.md) の unsupported）。

## 2. 設定完了後の状態

- identity root への変更は、リリース線へ merge し、デプロイブランチ `admin` へ昇格したときに CI が apply する。人は apply しない
- state は state Bucket（`tabp-admin-tfstate-<アカウントID>-ap-northeast-1-an`）に暗号化して保存され、書き込めるのは CI だけである
- CI はアクセスキーを持たず、GitHub の OIDC で AWS に入る
- 残った lock は GitHub Actions の `Terraform Unlock` で外す
- アカウントは Paid plan で、連絡先・Cost Explorer・Cost Anomaly Detection が設定済みである
- root は MFA を2台以上持ち、アクセスキーを持たない。人の IAM user は無い

## 3. やること

完了条件は、その作業で実現した状態である。確認のコマンドは `docs/setup/scripts/` に在り、どれも読むだけで何も変えない。

| 作業 | 内容 | 実施者 | 完了条件 |
| --- | --- | --- | --- |
| [事前準備](#事前準備) | MFA デバイスなどを揃える | 人 | 5点が揃っている |
| [アカウントの準備](#アカウントの準備)（1-0 〜 2-2） | アカウントの初期設定、一時 IAM user、`aws login`、連絡先、Cost Explorer | root、一時 IAM user | `check-account.sh` が `NG 0 件`、かつ[目で見る3点](#2-3-アカウントの準備を確かめる)を満たす |
| [段1](#段1-state-bucket-を作る) | state Bucket を CLI で作る | 一時 IAM user | `verify.sh stage1` が `NG 0 件` |
| [段2](#段2-plan-で突合する) | Terraform に取り込み、plan を期待値と照合する | 一時 IAM user | `check-plan.sh` が `不足 0 件、余剰 0 件`、`verify.sh before-apply` が `NG 0 件` |
| [段3](#段3-入力の値を用意する) | 自分のリポジトリの値を入れ、Pull Request を開く | 一時 IAM user | `sub` が immutable 形式、Pull Request の commit で `check-plan.sh` が一致 |
| [段4](#段4-ローカルから-apply-する) | Pull Request の commit を手元から apply し、merge する | 一時 IAM user、管理者 | plan が `No changes.`、`verify.sh stage4` と `permissions` が `NG 0 件`、CI の plan が変更なし、記録が Pull Request に在る |
| [段5](#段5-ci-の-apply-に移る) | 以後の変更を CI に apply させる | 管理者、CI | [段5 の成功条件](#段5-の成功条件)をすべて満たす |
| [削除](#一時-iam-user-の削除) | 一時 IAM user を消す | root | Credential report で IAM user が0件、root はアクセスキー無しで MFA が有効 |

- 上から順に行う。前の作業の完了条件を満たすまで次へ進まない
- 2-2 の結果は翌日以降に揃う。待つ間に段1 へ進んでよい
- 一時 IAM user は段5 の成功まで削除しない

## 4. 手順

コンソールの表記は英語 UI 基準である。

### 事前準備

- [ ] MFA デバイス2台（例: セキュリティキー2本、またはセキュリティキー1本＋認証アプリ）。root と一時 IAM user で別の MFA を使うなら、その分も
- [ ] root のメールアドレスとパスワード（パスワードマネージャー）
- [ ] 一時 IAM user 用に生成した強いパスワード
- [ ] AWS CLI 2.32.0 以上（`aws --version`）
- [ ] 今後も受信できる電話番号（primary contact に登録する）

### アカウントの準備

1-0 から 1-5 を root で続けて行い、2-0a 以降を一時 IAM user で行う。設定が正しいかは、最後の 2-3 でまとめて確かめる。

#### 1-0. Paid Plan に移行する（root）

作成時に Paid plan を選んでいれば不要。

1. [AWS コンソール](https://console.aws.amazon.com/) で「Root user」を選び、root でサインインする
2. [Upgrade plan](https://console.aws.amazon.com/billing/home?#/freetier/upgrade) を開き、「Upgrade account」を押す

#### 1-1. root の MFA を2台以上登録する（root）

1. 右上のアカウント名 →「Security credentials」→「Assign MFA device」で1台目を登録する
2. 同じ手順で2台目を登録する（再認証を求められたら1台目で認証する）

#### 1-2. Billing への IAM アクセスを有効にする（root）

1. 右上のアカウント名 →「Account」を開く（[Billing の Account ページ](https://console.aws.amazon.com/billing/home#/account)）
2. 「IAM user and role access to Billing information」の「Edit」→「Activate IAM Access」にチェック →「Update」

#### 1-3. 一時 IAM user を作る（root）

| 項目 | 設定 |
| --- | --- |
| ユーザー名 | 一時用と分かる名前（段3 で使うので控える） |
| path | 指定しない |
| ポリシー（直接アタッチ） | AdministratorAccess と SignInLocalDevelopmentAccess の2つだけ |
| コンソールアクセス | 有効（Custom password） |
| MFA | 登録する |
| アクセスキー | 作らない |

この user に AWS managed policy を付けることは [ADR-0301](../adr/0301-least-privilege-policy.md) 決定8・10 の対象外とし、bootstrap の後に削除する。

1. IAM コンソール →「Users」→「Create user」
2. ユーザー名を入力し、「Provide user access to the AWS Management Console」→「I want to create an IAM user」→「Custom password」を選んでパスワードを入力する
3. 「Attach policies directly」で上表の2つにチェックし、「Create user」を押す
4. 表示されたサインイン URL を控える
5. ユーザーの「Security credentials」タブで MFA を登録する（2-0a で本人が登録してもよい）

#### 1-4. root のアクセスキーを消す（root）

右上のアカウント名 →「Security credentials」の「Access keys」にキーがあれば、「Actions」→「Deactivate」→「Delete」で削除する。

#### 1-5. root からサインアウトする（root）

右上のアカウント名 →「Sign out」。root のパスワードと MFA デバイスは別々の場所に保管する。

#### 2-0a. 初回サインイン

1-3 で控えた URL から一時 IAM user でサインインする。MFA が未登録なら「Security credentials」→「Assign MFA device」で登録し、MFA 付きで入り直す。

#### 2-0b. aws login 用のプロファイルを作る

| 対象 | 必要バージョン |
| --- | --- |
| AWS CLI | 2.32.0 以上 |
| Terraform | 1.15.0 以上 |

1. `aws --version` と `terraform version` で上表を満たすことを確かめる
2. `env | grep AWS_` で `AWS_ACCESS_KEY_ID` と `AWS_SECRET_ACCESS_KEY` が設定されていないことを確かめる（設定されていると `AWS_PROFILE` より優先される）
3. `make aws-login` を実行し、リージョンに `ap-northeast-1` を入力する
4. ブラウザで一時 IAM user のセッションを選ぶ。**root のセッションは選ばない**

`~/.aws/config` に次のブロックが作られていれば完了。認証情報が失効したら（最長12時間）、`make aws-login` を再実行する。

```ini
[profile tabp-bootstrap]
login_session = arn:aws:iam::<アカウントID>:user/<一時IAMユーザー名>
region = ap-northeast-1
```

Terraform は `make` のターゲットか、本書のコマンドどおりに実行する。どちらも、資格情報や terraform の振る舞いを変える環境変数を外し（一覧の正本は `scripts/tf-local` の `env-unset`）、`TF_CLI_CONFIG_FILE=/dev/null` で CLI の設定を読ませない（`dev_overrides` が lock の hash の検証なしに provider を差し替えるのを防ぐ）。AWS CLI を直接打つときは `AWS_PROFILE=tabp-bootstrap` を付ける。

#### 2-0c. 1日の作業の終わりに

```bash
aws logout --profile tabp-bootstrap
```

#### 2-1. 連絡先を登録する

右上のアカウント名 →「Account」で、次を登録する。

- 「Contact information」: 氏名・住所・電話番号（root の MFA を失ったときの本人確認に使われるので、電話番号は正確に）
- 「Alternate contacts」: Billing / Operations / Security の3種

#### 2-2. Cost Explorer を有効にする

Billing and Cost Management コンソール →「Cost Explorer」→「Launch Cost Explorer」を押す。

#### 2-3. アカウントの準備を確かめる

1. 次を実行する

   ```bash
   docs/setup/scripts/check-account.sh
   ```

   最後の行が `NG 0 件` なら次へ。`呼び出しが失敗` と添えられた NG は、添えられたエラーを見て資格情報（`make aws-login`）を確かめる。それ以外の NG は、対応する作業（行頭の番号）をやり直す。2-2 の行だけが NG なら、翌日に再実行する。

2. コマンドで見られない次の3点を、コンソールで確かめる
   - root の「Security credentials」の MFA の一覧に2台以上ある（1-1）
   - 一時 IAM user で Billing and Cost Management の画面が開ける（1-2）
   - 翌日以降、Cost Explorer に当月のデータが表示され、Cost Anomaly Detection の通知先メールが意図したアドレスである（2-2）

### 段1. state Bucket を作る

以下は bash で実行する（fish などを使っているなら、先に `bash` を起動する）。

> **重要**: タグを付けない。暗号化・Bucket Policy・lifecycle も CLI で設定しない（段4 で Terraform が作る）。

1. 変数を用意する。このブロックは段2 から段5 でも使う。シェルを開き直したら再実行する

   ```bash
   export AWS_PROFILE=tabp-bootstrap
   REGION=ap-northeast-1
   ACCOUNT_ID=$(aws sts get-caller-identity --region "$REGION" --query Account --output text)
   if [[ $ACCOUNT_ID =~ ^[0-9]{12}$ ]]; then BUCKET="tabp-admin-tfstate-${ACCOUNT_ID}-${REGION}-an"; echo "$BUCKET"; else unset BUCKET; echo "ACCOUNT_ID が12桁ではありません: '$ACCOUNT_ID'" >&2; fi
   unset_env=$(env | grep -oE '^((AWS_|TF_CLI_ARGS|TF_VAR_|TF_LOG)[A-Za-z0-9_]*|TF_WORKSPACE|TF_DATA_DIR|TF_CLI_CONFIG_FILE|TF_REATTACH_PROVIDERS|ACTIONS_ID_TOKEN_REQUEST_URL|ACTIONS_ID_TOKEN_REQUEST_TOKEN)=' | sed 's/=$//; s/^/-u /')
   ```

   Bucket 名が表示されれば次へ。エラーが出たら `make aws-login` をやり直し、このブロックを再実行する。

2. Bucket を作り、Public Access Block と版管理を設定する

   ```bash
   aws s3api create-bucket --bucket "$BUCKET" --region "$REGION" \
     --bucket-namespace account-regional \
     --create-bucket-configuration "LocationConstraint=${REGION}" \
     --object-ownership BucketOwnerEnforced
   aws s3api put-public-access-block --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" \
     --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
   aws s3api put-bucket-versioning --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" \
     --versioning-configuration Status=Enabled
   ```

3. 確かめる

   ```bash
   docs/setup/scripts/verify.sh stage1
   ```

   最後の行が `NG 0 件` なら完了。NG があれば[トラブルシューティング](#段1-で-ng-が出た)へ。

### 段2. plan で突合する

> **重要**: 段2 では apply しない。

1. init する。段2 から段4 の `make` には `TF_LOCAL_MISSING_STATE_KEY=sse-s3` を付ける

   ```bash
   TF_LOCAL_MISSING_STATE_KEY=sse-s3 make init-admin-identity
   ```

   make を使えなければ[トラブルシューティング](#make-を使えない)へ。

2. plan を保存し、期待値と照合して、plan のファイルを消す。[段1 の手順1](#段1-state-bucket-を作る) のブロックを実行したシェルで行う

   ```bash
   : "${unset_env:?段1 の手順1 のブロックを先に実行する}" &&
     rm -f tmp/plan/admin-identity.tfplan &&
     (umask 077 && mkdir -p tmp/plan && env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1 \
       terraform -chdir=env/admin/identity plan -lock=false -input=false -out="$PWD/tmp/plan/admin-identity.tfplan") &&
     docs/setup/scripts/check-plan.sh tmp/plan/admin-identity.tfplan
   rm -f tmp/plan/admin-identity.tfplan
   ```

   最後の行が `# 結果: 不足 0 件、余剰 0 件` で、`# plan:` の行が `import 4 / add 41 / change 0 / destroy 0`、`# commit:` の行に `未コミットの変更あり` が無ければ次へ。期待値は [`check-plan.expected`](scripts/check-plan.expected) に在る。NG の行があれば[トラブルシューティング](#段2-の-plan-が一致しない)へ。

   > **重要**: `check-plan.sh` を実際の plan で初めて使うときは、plan の出力（`terraform plan` が表示したもの）の取り込みと追加の一覧を、`check-plan.expected` とも目で照合する。両者の判定が食い違ったら、止まって Pull Request に記録する。

3. 段4 で作るものがまだ無いことを確かめる

   ```bash
   docs/setup/scripts/verify.sh before-apply
   ```

   最後の行が `NG 0 件` で、終了コードが 0 なら完了。NG があれば、**誰がなぜ置いたかを確かめるまで段3 へ進まない**。

4. `check-plan.sh` の出力を Pull Request に載せる（アカウント ID は伏せてある）

### 段3. 入力の値を用意する

`env/<env>/identity/main.tf` を編集する。

1. `github` の4つの値を、自分のリポジトリのものにする。ID は次で得る

   ```bash
   gh api repos/<owner>/<repo> --jq '.owner.id, .id'
   ```

2. OIDC の `sub` の形式を確かめる

   ```bash
   gh api repos/<owner>/<repo>/actions/oidc/customization/sub
   ```

   `use_immutable_subject` が `true` で、`sub_claim_prefix` が `repo:<owner>@<owner_id>/<repo>@<repo_id>` であること。`false` なら**先へ進まない**（[`usecases/deployment-identity/README.md`](../../usecases/deployment-identity/README.md) の unsupported）。

3. `module "state_backend"` に、`bootstrap_executor_user_name = "<一時 IAM user の名前>"` を足す。段5 の最初の変更で外す（[state-backend/ADR-0104](../../usecases/state-backend/docs/adr/0104-state-object-write-deny.md) 決定7）

4. この変更を commit して Pull Request を開き、その commit で段2 の手順1・2 をやり直す。`check-plan.sh` が一致し、`# commit:` の行がその commit を示せば完了

### 段4. ローカルから apply する

段4 は ADR-0602 決定9・10 の例外である。apply は段3 の Pull Request の commit から行い、記録を Pull Request に残す。以下は段1 と同じ bash で実行する。

#### 適用前の準備

**事前に管理者が行うこと**: `make apply-environments` で environment を作り、`admin-plan` に secret を置く（値は管理アカウントの ID）。

```bash
gh secret set AWS_ACCOUNT_ID --env admin-plan
```

1. Pull Request の head を checkout する。`git status` が空であることを確かめ、`git rev-parse HEAD` を控える

2. [段1 の手順1](#段1-state-bucket-を作る) のブロックを実行する

3. 作るものがまだ無いことを確かめる

   ```bash
   docs/setup/scripts/verify.sh before-apply
   ```

   `NG 0 件` で終了コード 0 なら次へ。NG があれば**止まり**、誰がなぜ作ったかを確かめる。`import` で取り込まない。

#### plan と承認

4. init し、plan を保存して期待値と照合する

   ```bash
   : "${unset_env:?段1 の手順1 のブロックを先に実行する}" &&
     rm -f tmp/plan/admin-identity.tfplan &&
     env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1 \
       terraform -chdir=env/admin/identity init -input=false -lockfile=readonly -backend-config="bucket=${BUCKET}" &&
     (umask 077 && mkdir -p tmp/plan && env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1 \
       terraform -chdir=env/admin/identity plan -lock=false -input=false -out="$PWD/tmp/plan/admin-identity.tfplan") &&
     docs/setup/scripts/check-plan.sh tmp/plan/admin-identity.tfplan
   ```

   `不足 0 件、余剰 0 件` で、`# commit:` の行が手順1 で控えた commit を示し `未コミットの変更あり` が無ければ、その出力を Pull Request に載せて次へ（ADR-0602 決定10）。どれかが違えば apply せず、`rm -f tmp/plan/admin-identity.tfplan` で plan のファイルを消して原因を直す。

5. 次をすべて満たすことを確かめる。満たさなければ **apply せず**、`rm -f tmp/plan/admin-identity.tfplan` で plan のファイルを消す
   - 手順1 の commit で、plan 以外の required check が成功している
   - 保護設定が承認者を要求しているなら、承認を得ている

#### apply と検証

6. apply の直前に、もう一度確かめる

   ```bash
   docs/setup/scripts/verify.sh before-apply
   ```

   `NG 0 件` で終了コード 0 なら次へ。NG があれば止まる（やり直しの途中なら[トラブルシューティング](#段4-の-apply-が失敗した)へ）。

7. apply し、plan のファイルを消す。apply には `-lock=false` を付けない（lock を取る。ADR-0604 決定8）

   ```bash
   env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1 \
     terraform -chdir=env/admin/identity apply -input=false "$PWD/tmp/plan/admin-identity.tfplan"
   rm -f "$PWD/tmp/plan/admin-identity.tfplan"
   ```

   失敗したら[トラブルシューティング](#段4-の-apply-が失敗した)へ。

8. 作られたものを確かめる。1行目は backend の設定の控えだけを消す（state は消えない）。`TF_LOCAL_MISSING_STATE_KEY` は付けない

   ```bash
   rm -f env/admin/identity/.terraform/terraform.tfstate
   make plan-admin-identity
   BOOTSTRAP_EXECUTOR=<一時 IAM user の名前> docs/setup/scripts/verify.sh stage4
   ```

   段3 で `bootstrap_executor_user_name` を渡さなかったなら、3行目は `BOOTSTRAP_EXECUTOR=` を付けずに実行する。

   plan が `No changes.` を示し、`verify.sh stage4` が `NG 0 件` で終了コード 0 なら次へ。どちらかが違えば、出力を Pull Request に記録して**止まる**。

9. role の権限を確かめる

   ```bash
   docs/setup/scripts/verify.sh permissions
   ```

   `NG 0 件` で終了コード 0 なら次へ。NG があれば、出力を Pull Request に記録して**止まる**。確かめる権限は [deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md)、この確認で検出できないものは [`usecases/deployment-identity/README.md`](../../usecases/deployment-identity/README.md) にある。

#### 記録と merge

10. 実行の記録を Pull Request の本文に書く: 実行者（一時 IAM user）、日時、対象アカウント、適用した commit、手順8・9 の出力。アカウント ID は伏せる

11. Pull Request の CI の plan（`terraform-gates.yaml` の `plan-admin-identity`）が変更なしを示してから merge する。変更なしでなければ merge しない

### 段5. CI の apply に移る

段4 の後の変更は、リリース線へ merge し、リリース線からデプロイブランチ `admin` への Pull Request（昇格）を merge すると、CI（`terraform-apply.yaml` の `apply-admin-identity`）が apply する。リリース線への merge だけでは apply しない。**人は apply しない。** job の規則は [`.github/workflows/README.md`](../../.github/workflows/README.md) の「apply の job」にある。一時 IAM user のコマンドは、[段1 の手順1](#段1-state-bucket-を作る) のブロックを実行してから使う。

段5 は3つの工程から成る。

| 工程 | 手順 | 完了条件 |
| --- | --- | --- |
| [CI の apply を有効にする](#ci-の-apply-を有効にする) | 1・2 | `admin` の最初の run が緑で `No changes.` |
| [段5 の変更を適用する](#段5-の変更を適用する) | 3・4 | 各変更の後の再実行が `No changes.` |
| [lock の解除を用意して試す](#lock-の解除を用意して試す) | 5・6 | `Terraform Unlock` が lock を外し、取り消した run の再実行が緑 |

> **重要**: 一時 IAM user の書き込みを拒否する変更（段3 の `bootstrap_executor_user_name` を外す変更。足していないアカウントでは、書き込みの拒否を入れる変更）は、手順5・6 を済ませてから `admin` へ昇格する。

#### CI の apply を有効にする

1. 管理者が、デフォルトブランチを checkout した手元で、次を順に行う
   1. デフォルトブランチがリリース線（`release/` で始まる）であることを確かめる。`production` のままなら、最初のリリース線を切ってから進む。デフォルトブランチがリリース線でない間は、`admin` へ昇格できない

      ```bash
      gh repo view --json defaultBranchRef --jq .defaultBranchRef.name
      ```

   2. environment を宣言から適用する。apply 系と lock の解除の environment は、デプロイブランチ `admin` だけを許す

      ```bash
      make apply-environments
      ```

   3. secret を置く（値は管理アカウントの ID）。`admin-apply` と `admin-unlock` は workload root の apply と lock の解除の job が読む

      ```bash
      gh secret set AWS_ACCOUNT_ID --env admin-apply-identity
      gh secret set AWS_ACCOUNT_ID --env admin-apply
      gh secret set AWS_ACCOUNT_ID --env admin-unlock
      ```

   4. デプロイブランチ `admin` を、デフォルトブランチの先頭から作る。既に在れば失敗する

      ```bash
      make create-deploy-branch DEPLOY_ENV=admin
      ```

2. `admin` の最初の run で、次をすべて確かめる。作成の push で run が起きなければ、リリース線から `admin` への最初の Pull Request の merge の run がそれに当たる。run の URL を issue に残す（アカウント ID を載せない）
   - `apply-infra-code-gate`・`apply-resource-order-check`・`apply-layer-check`・`apply-admin-identity` が緑
   - ログの plan の要約が `No changes.`
   - Policy の違反が無い
   - `OIDC の sub` の行が `:environment:admin-apply-identity` で終わる

#### 段5 の変更を適用する

対象は [`usecases/state-backend/README.md`](../../usecases/state-backend/README.md) の段5 の行の変更である。

3. 変更を1つずつ当てる。1つごとに次を行う
   1. 変更の Pull Request をリリース線へ merge する
   2. リリース線から `admin` への Pull Request を開く。`deploy-source-check` を含む必須検査が緑になってから merge し、`admin` の CI の apply が緑になるのを待つ

      ```bash
      gh pr create --base admin --head release/vX.Y.Z --fill
      ```

   3. 次の昇格の前に、その `admin` の apply の run を Actions の画面で「Re-run all jobs」する
   4. 再実行のログの plan の要約が `No changes.` であることを確かめ、run の URL を Pull Request に記録する

   `No changes.` でなければ、要約と run の URL を Pull Request に記録して**止まる**。

   > **重要**: 再実行を確かめる前に、次を昇格しない。apply-identity の権限を足す変更は、その権限で作る resource の変更より先に昇格して apply する。

4. 権限を足す変更では、足した Action と期待の判定を、同じ変更で `docs/setup/scripts/verify.sh` の `permissions` に足す。Condition を持つ Action は、条件キーを `--context` で与える。apply の後に実行する

   ```bash
   docs/setup/scripts/verify.sh permissions
   ```

   `NG 0 件` で終了コード 0 であることを確かめ、出力を Pull Request に記録する。

#### lock の解除を用意して試す

5. lock の解除の workflow（`terraform-unlock.yaml`）を足す Pull Request を **merge する前に**、管理者がそのブランチを checkout した手元で実行する

   ```bash
   make apply-environments
   gh secret set AWS_ACCOUNT_ID --env admin-unlock-identity
   ```

   `make repo-settings-drift` で、宣言と実態が一致することを確かめてもよい。

6. lock を1つ残し、`Terraform Unlock` で外す
   1. identity root を変更する Pull Request をリリース線へ merge して `admin` へ昇格し、その `admin` の apply の run を、`apply` の step が lock を取った後に Actions の画面で取り消す
   2. 一時 IAM user で lock file が在ることを確かめる。返らなければ 1 からやり直す

      ```bash
      aws s3api list-objects-v2 --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --prefix identity/terraform.tfstate.tflock
      ```

   3. lock file の `LastModified` から、待ち時間（`scripts/tf-local` の定数）を超えるまで待つ
   4. `Terraform Unlock` を `admin` から起動し、次をすべて確かめる

      ```bash
      gh workflow run terraform-unlock.yaml --ref admin
      ```

      - run が緑
      - ログに lock の ID・経過分・消した lock の版 ID・delete marker の版 ID が出る
      - `OIDC の sub` の行が `:environment:admin-unlock-identity` で終わる
      - 2 のコマンドが lock file を返さない
   5. 取り消した apply の run を「Re-run failed jobs」で再実行し、緑になることを確かめる
   6. 結果と run の URL を Pull Request に記録する（アカウント ID を載せない）

   4 が失敗したら、[一時 IAM user で lock file を消し](#一時-iam-user-で-lock-file-を消す)、Pull Request に記録して**止まる**。

#### 段5 の成功条件

次をすべて満たせば段5 は成功である。満たさないものがあれば、出力を Pull Request に記録して**止まる**。

| # | 実現している状態 | 確かめ方 |
| --- | --- | --- |
| 1 | 段5 の変更がすべて適用され、それぞれの後の plan が変更なし | 手順3 |
| 2 | 書き込みを拒否する前に、`Terraform Unlock` が lock を外せた | 手順6 |
| 3 | 3つの Bucket と state が state の鍵で暗号化され、Bucket Policy の例外に IAM user が無い | 段5 の変更の apply の後に `docs/setup/scripts/verify.sh stage5` が `NG 0 件` で終了コード 0 |
| 4 | 一時 IAM user の書き込みが Bucket Policy で拒まれる | 下のコマンド |
| 5 | 書き込みの拒否の下で、`Terraform Unlock` が lock の無い状態を読める | 下の確認 |

条件4: 書き込みの拒否の後、一時 IAM user で次を実行する（存在しない key なので何も消えない）。`AccessDenied` が返り、メッセージが `with an explicit deny in a resource-based policy` で終わること。`put-object` で代えない。

```sh
aws s3api delete-object --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --key "verify/$(date -u +%Y%m%dT%H%M%SZ)"
```

条件5: 書き込みの拒否の後、`Terraform Unlock` を `admin` から起動する（`gh workflow run terraform-unlock.yaml --ref admin`）。緑で、ログが `✅ lock はありません` を示し、`OIDC の sub` の行が `:environment:admin-unlock-identity` で終わること。run の URL を Pull Request に記録する（アカウント ID を載せない）。

### 一時 IAM user の削除

> **注意**: 段5 が成功するまで削除しない。削除するまでは、一時 IAM user が Bucket Policy の書き込みの拒否を外せる。この削除で、bootstrap の実行者に書き込みの権限が残らない状態になる（ADR-0602 決定11）。

一時 IAM user に自分自身を削除させない。途中で止まったとき、続きを行える主体が残らない。

1. 手元で `aws logout --all` を実行する
2. `~/.aws/config` から `[profile tabp-bootstrap]` のブロックを削除する
3. **root で**サインインし、IAM コンソール →「Users」で一時 IAM user を「Delete」する
4. IAM コンソール →「Credential report」を取得する
5. root からサインアウトする

Credential report で IAM user が0件、root の行でアクセスキーが無く MFA が有効で、`~/.aws/credentials` にこのアカウントのアクセスキーが無ければ完了。

## トラブルシューティング

| 症状 | 対処 |
| --- | --- |
| `ACCOUNT_ID が12桁ではありません`、`一時 IAM user の資格情報ではありません` | `make aws-login` をやり直す |
| `check-account.sh` に NG がある | `呼び出しが失敗` と添えられていれば、そのエラーを見て資格情報を確かめる。それ以外は行頭の番号の作業をやり直す。2-2 だけなら翌日に再実行する |
| 段1 の `verify.sh stage1` に NG がある、Bucket の名前や namespace を誤った | [段1 で NG が出た](#段1-で-ng-が出た) |
| Terraform が `aws login` の認証情報を読めない | [aws login の認証情報を読めない](#aws-login-の認証情報を読めない) |
| `make` が使えない | [make を使えない](#make-を使えない) |
| `check-plan.sh` に NG がある | [段2 の plan が一致しない](#段2-の-plan-が一致しない) |
| `check-plan.sh` が `検証できません` で終わる | 示された理由を見る。init が済んでいなければ段2 の手順1 から、plan が失敗していれば原因を直して段2 の手順2 をやり直す |
| 段2・段4 で `unset_env` について「段1 の手順1 のブロックを先に実行する」と出る | 段1 の手順1 のブロックを実行し、同じシェルでやり直す |
| 段4 の apply が失敗した、lock が残った | [段4 の apply が失敗した](#段4-の-apply-が失敗した) |
| 段5 の CI の apply が失敗した | [段5 の CI の apply が失敗した](#段5-の-ci-の-apply-が失敗した) |
| 段5 で lock が残った | [段5 で lock が残った](#段5-で-lock-が残った) |
| 段5 の手順3 で再実行が `No changes.` にならない | 要約と run の URL を Pull Request に記録して止まる。再実行は差分を既に適用している |
| 段5 の手順3 の再実行が古い commit を理由に落ちた | 次の昇格の Pull Request の CI の plan が、その変更だけを示すことで代える |
| `admin` への昇格の Pull Request で `deploy-source-check` が落ちた | [段5 の CI の apply が失敗した](#段5-の-ci-の-apply-が失敗した) |
| `deploy-source-check` が `No rule to make target 'deploy-source-check'` で落ちた | この job は base の commit のコードで判定するので、base がまだこの検査を持たない。検査を足す変更を取り込む Pull Request でだけ起きる。ruleset の required にこの context が無ければ merge は止まらず、merge してよい。required にこの context が在るか、base に `make deploy-source-check` が在るのに出たなら止まる |

### 段1 で NG が出た

- Public Access Block・所有権・版管理の NG: 段1 の手順2 の `put-public-access-block` と `put-bucket-versioning` をやり直す。すべて `ok` になるまで段2 へ進まない
- 暗号化の行（`AES256` と `SSE-C` の拒否）の NG: **段2 へ進まず、出力を Pull Request に記録して止まる**（[state-backend/ADR-0101](../../usecases/state-backend/docs/adr/0101-sse-kms-with-state-key.md) の見直し条件）
- 名前や namespace の誤り: 空のうちに `aws s3api delete-bucket --bucket "$BUCKET"` で消し、段1 をやり直す

### aws login の認証情報を読めない

次を持つ別のプロファイルを作り、`AWS_PROFILE` にそちらを指定する。この場合 make は使えないので、[make を使えない](#make-を使えない)の手順で実行する。

```ini
credential_process = aws configure export-credentials --profile tabp-bootstrap --format process
```

### make を使えない

段2 の手順1 の init を terraform で直接実行する。

1. `env/admin/identity/backend.tf` と `providers.tf` の region が同じであることを確かめる
2. [段1 の手順1](#段1-state-bucket-を作る) のブロックを実行する
3. 次を実行する。続けて段2 の手順2 に進む

   ```bash
   : "${unset_env:?段1 の手順1 のブロックを先に実行する}" &&
     env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1 \
       terraform -chdir=env/admin/identity init -input=false -lockfile=readonly -backend-config="bucket=${BUCKET}"
   ```

### 段2 の plan が一致しない

`check-plan.sh` は、期待値に在って plan に無いものを `不足`、plan に在って期待値に無いもの（取り込んだ resource の `change`、`destroy` を含む）を `余剰` として示す。

- Terraform の宣言と段1 の Bucket のどちらが誤っているかを見る
- 宣言を直すときも、**安全側の値を弱めて差分を消さない**（ADR-0501 決定14）
- 段1 の誤りなら、Bucket を消して段1 からやり直す

### 段4 の apply が失敗した

| 症状 | 対処 |
| --- | --- |
| lock が残った | 他に apply が走っていないことを確かめ、`terraform force-unlock <ID>` で外す |
| `MalformedPolicyDocumentException` | commit を変えずに手順4 からやり直す |
| `EntityAlreadyExists` | 手順3 に戻る |
| その他のエラー | 原因を直した commit を同じ Pull Request に足し、手順4 からやり直す。記録も取り直す |
| やり直しの手順6 で `before-apply` が Policy か lifecycle を返す | `terraform state list` で、前の apply が作ったものとして state に在るかを見る。在れば手順4 の plan の差分で確かめたことにする。無ければ、誰がなぜ置いたかを確かめるまで止まる |

やり直しの手順4 では、前の apply が作ったものが state に入っているので、`check-plan.sh` は不足を示す。手順4 のブロックの `check-plan.sh` を `check-plan.sh --partial` に替えて実行する。apply 済みのものは `済み` として示され、最後の行が `余剰 0 件` なら次へ進んでよい。余剰があれば止まる。

### 段5 の CI の apply が失敗した

失敗は critical の deploy の通知（`notify.yaml`）で届く。原因は run の失敗した step のログから読む（人は state と plan の出力の全文を読めない）。

| 症状 | 対処 |
| --- | --- |
| 拒否の step が secret の不在で落ちた | secret を置き、「Re-run failed jobs」で再実行する |
| `admin` の最初の apply が落ちた | 成功した apply が無いので、revert のブランチは用意されない。原因を直す変更をリリース線へ merge して昇格する。戻すなら、リリース線の先頭の上で `git revert -m 1 <失敗した admin の merge>` を行う Pull Request を merge してから昇格する |
| 昇格の Pull Request で `deploy-source-check` が落ちた | ログの理由を見る。デフォルトブランチがリリース線でない（hotfix の間、最初のリリース線を切る前）なら、デフォルトブランチがリリース線に戻ってから昇格する。到達できない・木が一致しないなら、[`.github/workflows/README.md`](../../.github/workflows/README.md) の「apply の job」に従う |
| 書き込みの拒否を入れる変更か、段3 の値を外す変更の apply が落ちた | 下の診断を行う |

診断: 一時 IAM user で次を読む。

```bash
aws s3api get-bucket-policy --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
aws s3api list-objects-v2 --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --prefix identity/terraform.tfstate.tflock
```

- 1つ目を宣言（`modules/s3/policy.tf` と `usecases/state-backend/main.tf`）と比べ、2つ目で lock の残存を見る
- Bucket Policy の例外が CI の role か state の鍵の ARN と食い違っていたら、回復手順は無い（ADR-0602 決定9）。Pull Request に記録して**止まる**

### 段5 で lock が残った

| lock を残したもの | 対処 |
| --- | --- |
| CI の apply | `Terraform Unlock` を `admin` から起動し（`gh workflow run terraform-unlock.yaml --ref admin`）、落ちた apply の run を「Re-run failed jobs」で再実行する。新しすぎる lock では失敗するので、時間を置いて起動し直す（[`.github/workflows/README.md`](../../.github/workflows/README.md) の「lock の解除の job」） |
| 人の手元の apply | 実行した人が `terraform force-unlock <ID>` で外す |
| `Terraform Unlock` で外れない | 一時 IAM user の書き込みの拒否の前なら、[一時 IAM user で lock file を消す](#一時-iam-user-で-lock-file-を消す)。拒否の後は、人は消せない |

### 一時 IAM user で lock file を消す

一時 IAM user の書き込みが拒否される前に限る。

> **重要**: apply の run が走っていないことを Actions の画面で確かめてから消す。

```bash
aws s3api delete-object --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --key identity/terraform.tfstate.tflock
```

## この文書の保守

- identity root に resource を足す変更は、[`scripts/check-plan.expected`](scripts/check-plan.expected) を同じ変更で更新する
- apply-identity の権限を足す変更は、`docs/setup/scripts/verify.sh` の `permissions` を同じ変更で更新する（段5 の手順4）

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
