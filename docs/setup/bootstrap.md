# bootstrap 手順書

## 概要

管理アカウントに `state-backend` と `deployment-identity` の2ユースケースを立ち上げ、以後の変更を CI が apply できる状態にするまでの手順である。管理アカウントを作った直後から始め、次の順に進む。

1. アカウントの準備 —— root と一時 IAM user で、アカウントの設定と bootstrap の実行者を整える
2. 段1-5 —— 内容と順序は [ADR-0602](../adr/0602-bootstrap-and-ci-authentication.md) 決定4 が定める。段4 のローカルからの apply は同 決定9・10 の例外であり、実行の記録は Pull Request の本文に残す（同 決定10）
3. 一時 IAM user の削除 —— 段5 の成功の後に行う

各ユースケースの責務境界と公開契約は、それぞれの README が持つ。本書はそれらを再掲しない。ADR-0602 決定9 の回復の契機でも、同じ手順を踏む。ただし、段5 で CI の apply が state を state の鍵で書いた後は、Key Policy に名指されない一時 IAM user は state を読めず、この手順をそのまま回復に使えない（[`usecases/state-backend/README.md`](../../usecases/state-backend/README.md) の unsupported）。

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

この user は、bootstrap の段1-4 を人の手元から進めるためだけの一時的な主体であり、このリポジトリが Terraform で生成する IAM ではない。そのため、2つの AWS managed policy を付けることは [ADR-0301](../adr/0301-least-privilege-policy.md) 決定8・10（Action wildcard と AWS managed policy を使わない）の対象外とする。段1-4 で作るものが何であるかが事前に確定しない以上、権限を絞った customer managed policy を手で書くと、bootstrap の途中で権限不足に当たる。代わりに、存在する期間を bootstrap の間に限り、終わったら「一時 IAM user の削除」で消す。path は指定しない（既定の `/`）。段3 で state Bucket の書き込みの例外に入れる値は、path を持たない名前である。

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

名前付きプロファイル `tabp-bootstrap`（tabp は terraform-aws-boilerplate の略）を作る。Terraform は make のターゲット（`make init-admin-identity` / `make plan-admin-identity`）から実行し、ターゲットは、資格情報や terraform の振る舞いを変える呼び出し元の環境変数（`AWS_` で始まるもの、`TF_CLI_ARGS*`、`TF_VAR_*`、`TF_LOG*` ほか。一覧の正本は `scripts/tf-local` の `env-unset`）を外したうえで、`AWS_PROFILE=tabp-bootstrap` と `TF_CLI_CONFIG_FILE=/dev/null` を渡す。後者は `~/.terraformrc` と `~/.terraform.d/*.tfrc` を読ませない —— そこにある `dev_overrides` は lock の hash の検証を経ずに provider を差し替える。AWS CLI を直接打つ段1 では `AWS_PROFILE=tabp-bootstrap` を自分で付ける。このプロファイルは一時 user と一緒に削除するもので、`default` を汚さず、強い権限が及ぶ範囲もコマンド単位に限定できる。Terraform は `aws login` の認証情報を直接読めるので、`credential_process` は使わない。リージョンは ap-northeast-1 とする。

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

state backend が無い時点では Terraform が state を保存できないので、Bucket だけを AWS CLI で作る（ADR-0602 決定5）。作るのは、`modules/s3` が宣言するもののうち state を保存するために要るものに限る —— 版管理、Public Access Block、Object Ownership。既定の暗号化、Bucket Policy、lifecycle、ログの配送、KMS は CLI で作らない。これらは段2 の plan に追加として現れ、段4 以降で Terraform が作る。既定の暗号化は state の鍵を名指すので、鍵より先には置けない。段4 の apply までは、AWS が新しい Bucket に置く既定（SSE-S3 と SSE-C の拒否）が掛かる（[state-backend/ADR-0101](../../usecases/state-backend/docs/adr/0101-sse-kms-with-state-key.md) 決定4）。

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

3. Public Access Block と版管理を設定する

   ```bash
   aws s3api put-public-access-block --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" \
     --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
   aws s3api put-bucket-versioning --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" \
     --versioning-configuration Status=Enabled
   ```

4. 設定を読み戻して確かめる

   ```bash
   aws s3api get-public-access-block --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-ownership-controls --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-versioning --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-encryption --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   ```

   Public Access Block の4項目が `true`、所有権が `BucketOwnerEnforced`、版管理が `Enabled` であること。1つでも違えば、手順3 をやり直す。3つがすべて一致するまで段2 へ進まない。暗号化は AWS の既定で、`AES256` で `SSE-C` を拒否していること。拒否していなければ、段2 へ進まずに Pull Request に記録して止まる（AWS が新しい Bucket の既定を変えたことになり、state-backend/ADR-0101 の見直し条件に当たる）。

名前や namespace を誤ったときは、空のうちに `aws s3api delete-bucket --bucket "$BUCKET"` で消して作り直す。

### 段2. Terraform で取り込み、plan で突合する

`env/admin/identity/` の root が、段1 の Bucket を `import` block で取り込む（ADR-0602 決定6）。backend の `bucket` と `kms_key_id` は block に書かず、init の引数で渡す。`make init-admin-identity` がアカウント ID から Bucket 名を組み、state の鍵の alias から鍵の ARN を求めて渡す。

段4 の apply までは state の鍵が無い。`make` は既定では鍵が無いと init で失敗するので、段2 から段4 の `make` には `TF_LOCAL_MISSING_STATE_KEY=sse-s3` を付けて、鍵を渡さない init を明示して選ぶ。この指定は、alias が無く、Bucket の既定の暗号化が `AES256` のときだけ効く。その間の state と lock file は SSE-S3 で書かれる。CI はこの指定を受けない。

段2 では apply しない。state を書くのは段4 のローカルの apply であり、ADR-0601 決定5 の例外は段4 だけに掛かる（ADR-0602 決定9）。plan は lock を取らずに実行する（ADR-0604 決定8）。

1. plan する。`make plan-admin-identity` は先に `make init-admin-identity` を実行し、plan の出力を `tmp/plan/admin-identity.log` に残す。log の1行目は plan した commit である

   ```bash
   TF_LOCAL_MISSING_STATE_KEY=sse-s3 make plan-admin-identity
   ```

   make を使えない場合（`scripts/tf-local` を build できない等）は、段1 の手順1 と同じ bash で `BUCKET` を組み、terraform を直接実行する。backend の region（`env/admin/identity/backend.tf`）と provider の region（同 `providers.tf`）が同じであることを先に確かめる

   ```bash
   unset_env=$(env | grep -oE '^((AWS_|TF_CLI_ARGS|TF_VAR_|TF_LOG)[A-Za-z0-9_]*|TF_WORKSPACE|TF_DATA_DIR|TF_CLI_CONFIG_FILE|TF_REATTACH_PROVIDERS|ACTIONS_ID_TOKEN_REQUEST_URL|ACTIONS_ID_TOKEN_REQUEST_TOKEN)=' | sed 's/=$//; s/^/-u /')
   env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null \
     terraform -chdir=env/admin/identity init -input=false -lockfile=readonly -backend-config="bucket=${BUCKET}"
   env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null \
     terraform -chdir=env/admin/identity plan -lock=false -input=false
   ```

   この場合、plan の出力は log に残らない。どの commit で plan したかは `git rev-parse HEAD` で控える

2. 結果が `Plan: 4 to import, 30 to add, 0 to change, 0 to destroy.` であり、追加が次の30件だけであることを確かめる。取り込んだ resource に変更と破棄が0件で、追加が段1 で作らないもの（ADR-0602 決定5）に限られることをもって、段2 の完了とする（ADR-0602 決定8）。追加の件数と一覧は、identity root に resource を足す変更がこの手順とあわせて更新する。一覧を設定と突き合わせる検査は無い

   - `module.state_backend.module.s3` の `aws_s3_bucket_server_side_encryption_configuration.this`、`aws_s3_bucket_policy.this`、`aws_s3_bucket_lifecycle_configuration.this` の3件（1件目は state の鍵による既定の暗号化。[state-backend/ADR-0101](../../usecases/state-backend/docs/adr/0101-sse-kms-with-state-key.md)。3件目は state Bucket の lifecycle。[state-backend/ADR-0103](../../usecases/state-backend/docs/adr/0103-expire-noncurrent-state-versions.md)）
   - `module.state_backend.module.plan_output` の `aws_s3_bucket.this`、`aws_s3_bucket_public_access_block.this`、`aws_s3_bucket_ownership_controls.this`、`aws_s3_bucket_versioning.this`、`aws_s3_bucket_server_side_encryption_configuration.this`、`aws_s3_bucket_policy.this`、`aws_s3_bucket_lifecycle_configuration.this` の7件（CI の plan の出力の Bucket。[state-backend/ADR-0201](../../usecases/state-backend/docs/adr/0201-plan-output-bucket.md)）
   - `module.state_backend.module.kms` の `aws_kms_key.this`、`aws_kms_key_policy.this`、`aws_kms_alias.this` の3件（state の鍵）
   - `module.deployment_identity.module.github_oidc.aws_iam_openid_connect_provider.github`
   - `module.deployment_identity.module.github_oidc` の `aws_iam_role.apply_identity` と `aws_iam_role_policy.apply_identity` の2件
   - `module.deployment_identity.module.github_oidc` の `aws_iam_role.this` と `aws_iam_role_policy.this` を、それぞれ `["plan"]`・`["apply"]`・`["unlock_identity"]` の3件ずつ
   - `module.deployment_identity.module.github_oidc` の `aws_iam_role_policies_exclusive.this` と `aws_iam_role_policy_attachments_exclusive.this` を、それぞれ `["plan"]`・`["apply"]`・`["apply_identity"]`・`["unlock_identity"]` の4件ずつ
3. Bucket に Policy と lifecycle がまだ無いことを確かめる。段4 の apply は、在る Policy と lifecycle を黙って置き換える。段1 の手順1 と同じ bash で実行する。`make plan-admin-identity` は `ACCOUNT_ID` と `BUCKET` をシェルに残さないので、別のシェルなら先に段1 の手順1 のブロックを実行する

   ```bash
   aws s3api get-bucket-policy --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-lifecycle-configuration --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   ```

   順に `NoSuchBucketPolicy` と `NoSuchLifecycleConfiguration` のエラーが返ること。Policy か lifecycle が返ったら、誰がなぜ置いたかを確かめるまで段3 へ進まない

   state の鍵の alias もまだ無いことを確かめる。在ると、段4 の apply は鍵を作った後の alias の作成で落ち、alias の無い鍵が残る

   ```bash
   aws kms list-aliases --query "Aliases[?AliasName=='alias/tabp-admin-tfstate']" --output text
   ```

   何も出力されないこと

差分が出たら、Terraform の宣言か段1 の Bucket のどちらが誤っているかを見る。宣言を直すときも、安全側の値を弱めて差分を消さない（ADR-0501 決定14）。段1 の誤りなら、Bucket を消して段1 からやり直す。

plan の出力にはアカウント ID が入る。Pull Request へ載せるときは、アカウント ID を伏せた要約にする。

### 段3. deployment-identity の値を用意する

`env/<env>/identity/main.tf` の `module "deployment_identity"` が、OIDC provider と plan / apply / apply-identity / unlock-identity の4 role を作る（[`usecases/deployment-identity`](../../usecases/deployment-identity/README.md)）。段3 で人が行うのは、その入力の値を自分のリポジトリに合わせることだけである。

1. `github` の4つの値を、role を assume させるリポジトリのものにする。ID は次で得る

   ```bash
   gh api repos/<owner>/<repo> --jq '.owner.id, .id'
   ```

2. リポジトリの OIDC の `sub` が immutable 形式であることを確かめる。`use_immutable_subject` が `true` で、`sub_claim_prefix` が `repo:<owner>@<owner_id>/<repo>@<repo_id>` であること。`false` なら先へ進まない。旧形式の `sub` は unsupported である（`usecases/deployment-identity/README.md`）

   ```bash
   gh api repos/<owner>/<repo>/actions/oidc/customization/sub
   ```

3. `env/<env>/identity/main.tf` の `module "state_backend"` に、`bootstrap_executor_user_name = "<一時 IAM user の名前>"` を足す。段4 の apply が作る Bucket Policy は、state Bucket の object の書き込みを CI の role に限る。この値が無いと、段4 の apply は Bucket Policy を作った後の state と lock file の書き込みで、自分を締め出す。この値で一時 IAM user は identity root の state と lock file に書き込め、暗号化の方式の header の拒否の例外にも入る。値は段5 の最初の変更で外す（段5 の手順6）（[state-backend/ADR-0104](../../usecases/state-backend/docs/adr/0104-state-object-write-deny.md) 決定7）
4. 値を変えた commit で Pull Request を開き、段2 の plan（`TF_LOCAL_MISSING_STATE_KEY=sse-s3 make plan-admin-identity`）をやり直す。件数と一覧が段2 の手順2 のとおりであること

### 段4. 一時 IAM user のローカル権限で apply する

段4 は ADR-0602 決定9・10 の例外であり、記録を残さない実行を認めない。段4 の Pull Request は、merge の前に、その head に対する CI の plan（`terraform-gates.yaml` の `plan-admin-identity`）が変更なしを示す必要がある（同 決定10）。environment は `.github/settings/environments.json` が宣言し、管理者が `make apply-environments` で適用する。plan の job が読む environment `admin-plan` の secret `AWS_ACCOUNT_ID`（管理アカウントの ID）は宣言に無く、管理者が置く（`gh secret set AWS_ACCOUNT_ID --env admin-plan`）。突合は secret を見ない。

段4 の apply の入口は make に作らない。terraform を直接実行する。以下はすべて段1 の手順1 と同じ bash で実行する。

1. Pull Request の head を checkout し、`git status` が何も示さないことを確かめ、`git rev-parse HEAD` を控える
2. 段1 の手順1 のブロックで `ACCOUNT_ID` と `BUCKET` を用意し、段2 の手順1 の「make を使えない場合」の1行目で `unset_env` を用意する
3. 作る対象がまだ無いことを確かめる。どれかが在れば止まり、誰がなぜ作ったかを確かめる。`import` で取り込まない（ADR-0202 決定7）

   ```bash
   aws iam list-open-id-connect-providers
   for r in plan apply apply-identity unlock-identity; do aws iam get-role --role-name "tabp-admin-ci-${r}"; done
   ```

   1つ目の結果に `token.actions.githubusercontent.com` が無く、4つの `get-role` がすべて `NoSuchEntity` を返すこと

4. plan して、その結果を保存する。件数と一覧が段2 の手順2 のとおりであることを確かめ、アカウント ID を伏せた要約を Pull Request に載せる（ADR-0602 決定10）。state の鍵はまだ無いので、init は鍵を渡さない

   ```bash
   env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null \
     terraform -chdir=env/admin/identity init -input=false -lockfile=readonly -backend-config="bucket=${BUCKET}"
   (umask 077 && mkdir -p tmp/plan && env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null \
     terraform -chdir=env/admin/identity plan -lock=false -input=false -out="$PWD/tmp/plan/admin-identity.tfplan")
   ```

5. apply の前の条件を確かめる（ADR-0602 決定10）。手順1 で控えた commit で、plan 以外の required check が成功していること。保護設定が承認者を要求しているなら、その承認を得ていること。満たさなければ apply しない
6. apply の直前に、Bucket に Policy と lifecycle がまだ無いことをもう一度確かめる。段4 の apply は、在る Policy と lifecycle を黙って置き換え、plan はそれを示さない

   ```bash
   aws s3api get-bucket-policy --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   aws s3api get-bucket-lifecycle-configuration --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"
   ```

   順に `NoSuchBucketPolicy` と `NoSuchLifecycleConfiguration` のエラーが返ること

7. 保存した plan を apply し、plan のファイルを消す。apply は lock を取る（ADR-0604 決定8。`-lock=false` を付けない）。plan のファイルは state の値とアカウント ID を含む

   ```bash
   env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null \
     terraform -chdir=env/admin/identity apply -input=false "$PWD/tmp/plan/admin-identity.tfplan"
   rm -f "$PWD/tmp/plan/admin-identity.tfplan"
   ```

8. apply の後に確かめる
   - `make plan-admin-identity` が `No changes.` を示す。示さなければ、その出力を Pull Request に記録して止まる。apply で state の鍵ができ、init が鍵の ARN を渡すようになるので、手順4 の init の backend の設定とは食い違い、そのままの `make` は init で失敗する。先に手元の backend の設定のキャッシュを消す。消すのは backend の設定の控えだけで、state（S3 の object）でも provider でもない。`TF_LOCAL_MISSING_STATE_KEY` は付けない

     ```bash
     rm -f env/admin/identity/.terraform/terraform.tfstate
     make plan-admin-identity
     ```

   - `aws s3api get-bucket-encryption --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"` と、同じことを plan の出力の Bucket（`"${BUCKET/-tfstate-/-tfplan-}"`）に行った結果が、どちらも `SSEAlgorithm` `aws:kms`、`KMSMasterKeyID` が `alias/tabp-admin-tfstate` の指す鍵の ARN、`BucketKeyEnabled` `false`、`SSE-C` の拒否を返す。state そのものは、段5 で state を書き換える最初の CI の apply まで SSE-S3 のままである
   - `aws iam get-open-id-connect-provider --open-id-connect-provider-arn "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/token.actions.githubusercontent.com"` の `ClientIDList` が `sts.amazonaws.com` だけである
   - 4つの role で、`aws iam list-role-policies` が役割の policy 名の1つだけ（`plan` / `apply` / `apply_identity` / `unlock_identity`。role 名と違い、apply-identity と unlock-identity の policy 名は下線でつなぐ）を、`aws iam list-attached-role-policies` が空を返す
   - `aws s3api get-bucket-policy` が、TLS でない要求を拒否する1文を返す
   - `aws s3api get-bucket-lifecycle-configuration --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"` が規則を1つだけ返し、その規則が `Status` `Enabled`、空の `Filter`、`Expiration` の `ExpiredObjectDeleteMarker` `true`（`Days` と `Date` が無い）、`NoncurrentVersionExpiration` の `NoncurrentDays` が `state_noncurrent_version_retention_days` の値、`AbortIncompleteMultipartUpload` の `DaysAfterInitiation` `1` を持つ
   - `aws kms describe-key --key-id alias/tabp-admin-tfstate` が `KeyState` `Enabled`、`KeySpec` `SYMMETRIC_DEFAULT`、`KeyUsage` `ENCRYPT_DECRYPT`、`MultiRegion` `false`、`Origin` `AWS_KMS` を、`aws kms get-key-rotation-status --key-id alias/tabp-admin-tfstate` が `KeyRotationEnabled` `true` と `RotationPeriodInDays` `365` を返す
   - `aws kms get-key-policy --key-id alias/tabp-admin-tfstate --policy-name default` が、アカウントへの委任の文と、plan と apply-identity の2つの role に S3 経由の `kms:Decrypt`・`kms:GenerateDataKey` を許す文と、unlock-identity の role に S3 経由の `kms:Decrypt` だけを、暗号化コンテキスト `aws:s3:arn` が identity root の lock file（`identity/terraform.tfstate.tflock`）の object の ARN であるときに許す文の3つだけを返す
9. 権限を確かめる。[deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md) の表の Action が plan と apply-identity の role に許され、plan の role に変更系が無く、apply の role の bootstrap の構成物への変更と、plan の出力の置き場の読み取りが拒否されること。unlock-identity の role の権限は、state の鍵の block の後で確かめる

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
   plan_bucket="arn:aws:s3:::${BUCKET/-tfstate-/-tfplan-}"
   sim plan "arn:aws:s3:::${BUCKET}" $reads
   sim apply-identity "arn:aws:s3:::${BUCKET}" $reads s3:PutBucketPolicy s3:PutLifecycleConfiguration
   sim apply-identity "$key" s3:PutObject
   sim apply-identity "$(arn plan)" iam:PutRolePolicy iam:UpdateAssumeRolePolicy
   sim plan "$plan_bucket" $reads
   sim plan "${plan_bucket}/pr/1/x" s3:PutObject
   sim apply-identity "$plan_bucket" $reads s3:PutLifecycleConfiguration
   sim apply-identity "${plan_bucket}/apply/1/x" s3:PutObject
   sim apply "arn:aws:s3:::${BUCKET}" s3:ListBucket
   sim plan "$key" s3:PutObject
   sim plan "$(arn plan)" iam:PutRolePolicy
   sim plan "${plan_bucket}/pr/1/x" s3:DeleteObject s3:DeleteObjectVersion
   sim plan "${plan_bucket}/x" s3:PutObject
   sim apply-identity "${plan_bucket}/pr/1/x" s3:PutObject s3:DeleteObject
   sim apply-identity "${plan_bucket}/apply/1/x" s3:DeleteObject s3:DeleteObjectVersion
   sim apply "$(arn apply-identity)" iam:PutRolePolicy iam:UpdateAssumeRolePolicy sts:AssumeRole
   sim apply "arn:aws:s3:::${BUCKET}" s3:PutBucketPolicy s3:DeleteBucketPolicy s3:PutLifecycleConfiguration s3:PutBucketTagging s3:TagResource
   sim apply "$key" s3:PutObject s3:DeleteObject
   sim apply "$plan_bucket" s3:PutBucketPolicy s3:PutLifecycleConfiguration s3:GetBucketPolicy
   sim apply "${plan_bucket}/pr/1/x" s3:PutObject s3:DeleteObjectVersion
   ```

   期待は順に、15行・17行・1行・2行・15行・1行・16行・1行・1行がすべて `allowed`、続く6つが1行・1行・2行・1行・2行・2行ですべて `implicitDeny`、最後の5つが3行・5行・2行・3行・2行ですべて `explicitDeny` であること。行数か判定が1つでも違えば、Pull Request に記録して止まる。`simulate-principal-policy` が評価するのは role の policy だけで、Bucket Policy は入らない。plan の role の実効の権限は手順11 の CI の plan が、apply-identity の role の実効の権限は段5 の最初の apply の run が確かめる。ただし `apply/*` への書き込みの実効の権限は、apply の出力を置く CI の job が入るまで確かめる経路が無い。apply の role を assume する job は無い（#222）ので、apply の role の実効の権限を確かめる経路は無い

   state の鍵の権限（[deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定3-5）は次で確かめる。`simc` は条件キーを `--context-entries` で与える `sim` である

   ```bash
   state_key=$(aws kms describe-key --region "$REGION" --key-id alias/tabp-admin-tfstate --query KeyMetadata.Arn --output text)
   other="arn:aws:kms:${REGION}:${ACCOUNT_ID}:key/00000000-0000-0000-0000-000000000000"
   simc() { role=$1 res=$2 ctx=$3; shift 3
     aws iam simulate-principal-policy --policy-source-arn "$(arn "$role")" --resource-arns "$res" --context-entries $ctx \
       --action-names "$@" --query 'EvaluationResults[].[EvalActionName,EvalDecision]' --output text; }
   sim apply-identity "$state_key" kms:DescribeKey kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListResourceTags kms:EnableKeyRotation kms:PutKeyPolicy
   sim apply-identity "*" kms:ListAliases
   sim plan "$state_key" kms:DescribeKey kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListResourceTags
   sim plan "*" kms:ListAliases
   sim apply-identity "*" kms:CreateKey
   sim apply-identity "$state_key" kms:CreateAlias kms:ScheduleKeyDeletion kms:DisableKey kms:DisableKeyRotation kms:EnableKey kms:UpdateKeyDescription kms:TagResource kms:Decrypt
   sim apply-identity "$other" kms:DescribeKey kms:PutKeyPolicy
   sim plan "$state_key" kms:PutKeyPolicy kms:Decrypt kms:GenerateDataKey
   sim plan "$other" kms:DescribeKey
   sim apply "$state_key" kms:Decrypt kms:GenerateDataKey
   sim apply "$state_key" kms:CancelKeyDeletion kms:CreateAlias kms:DeleteAlias kms:DescribeKey kms:DisableKey kms:DisableKeyRotation kms:EnableKey kms:EnableKeyRotation kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListGrants kms:ListKeyPolicies kms:ListResourceTags kms:PutKeyPolicy kms:RevokeGrant kms:RotateKeyOnDemand kms:ScheduleKeyDeletion kms:TagResource kms:UntagResource kms:UpdateAlias kms:UpdateKeyDescription
   simc apply-identity "*" "ContextKeyName=kms:BypassPolicyLockoutSafetyCheck,ContextKeyValues=true,ContextKeyType=boolean" kms:PutKeyPolicy
   ```

   期待は順に、6行・1行・4行・1行がすべて `allowed`、続く6つが1行・8行・2行・3行・1行・2行ですべて `implicitDeny`、最後の2つが21行・1行ですべて `explicitDeny` であること。CI の role は鍵と alias を作れず、鍵の権限は state の鍵の ARN に限られる（別の鍵の行は `implicitDeny`）。apply の role は state の鍵の管理の Action を読み取りも含めて Deny され、暗号操作は Deny されない（`implicitDeny`）。暗号操作は IAM では許さず、state の鍵の Key Policy が CI の role を名指して許す。`simulate-principal-policy` は IAM の評価だけを見るので、KMS が実際の要求で Deny をどう評価するかは確かめない

   段5 の S3 サーバーアクセスログの構成物の権限（同 決定3-5）は次で確かめる

   ```bash
   L="arn:aws:logs:${REGION}:${ACCOUNT_ID}"
   lg="${L}:log-group:tabp-admin-state-backend-access"
   dd="${L}:delivery-destination:tabp-admin-state-backend-access"
   src="${L}:delivery-source:tabp-admin-tfstate"
   src2="${L}:delivery-source:tabp-admin-tfplan"
   dl="${L}:delivery:x"
   dest="ContextKeyName=logs:DeliveryDestinationResourceArn,ContextKeyType=string,ContextKeyValues"
   sim plan "$lg" logs:ListTagsForResource
   sim plan "*" logs:DescribeLogGroups logs:DescribeResourcePolicies
   sim plan "$dd" logs:GetDeliveryDestination logs:ListTagsForResource
   sim plan "$src" logs:GetDeliverySource logs:ListTagsForResource
   sim plan "$src2" logs:GetDeliverySource logs:ListTagsForResource
   sim plan "$dl" logs:GetDelivery logs:ListTagsForResource
   sim apply-identity "${lg}:*" logs:CreateLogGroup logs:PutRetentionPolicy
   sim apply-identity "$lg" logs:ListTagsForResource
   sim apply-identity "*" logs:DescribeLogGroups logs:DescribeResourcePolicies logs:PutResourcePolicy
   simc apply-identity "$dd" "${dest}=${lg}" logs:PutDeliveryDestination
   sim apply-identity "$dd" logs:GetDeliveryDestination logs:ListTagsForResource logs:CreateDelivery
   sim apply-identity "$src" logs:PutDeliverySource logs:GetDeliverySource logs:ListTagsForResource logs:CreateDelivery
   sim apply-identity "$src2" logs:PutDeliverySource logs:GetDeliverySource logs:ListTagsForResource logs:CreateDelivery
   sim apply-identity "$dl" logs:CreateDelivery logs:GetDelivery logs:ListTagsForResource
   sim apply-identity "arn:aws:s3:::${BUCKET}" s3:AllowVendedLogDeliveryForResource
   sim apply-identity "$plan_bucket" s3:AllowVendedLogDeliveryForResource
   sim plan "${lg}:log-stream:x" logs:GetLogEvents
   sim plan "${lg}:*" logs:CreateLogGroup logs:GetLogRecord logs:GetQueryResults
   sim apply-identity "${lg}:*" logs:DeleteLogGroup logs:DisassociateKmsKey logs:PutLogGroupDeletionProtection
   sim apply-identity "$lg" logs:TagResource
   simc apply-identity "$dd" "${dest}=arn:aws:s3:::other" logs:PutDeliveryDestination
   sim apply-identity "$dl" logs:DeleteDelivery logs:UpdateDeliveryConfiguration
   sim apply-identity "${L}:delivery-source:other" logs:CreateDelivery logs:PutDeliverySource
   sim apply-identity "${L}:delivery-destination:other" logs:CreateDelivery logs:PutDeliveryDestination
   sim apply-identity "${L}:log-group:other:*" logs:CreateLogGroup
   sim apply "${lg}:*" logs:CreateLogGroup logs:PutRetentionPolicy
   sim apply "$lg" logs:ListTagsForResource
   sim apply "$dd" logs:PutDeliveryDestination
   sim apply "$src" logs:PutDeliverySource
   sim apply "$src2" logs:PutDeliverySource
   sim apply "arn:aws:s3:::${BUCKET}" s3:AllowVendedLogDeliveryForResource
   sim apply "$plan_bucket" s3:AllowVendedLogDeliveryForResource
   sim apply "*" logs:PutAccountPolicy logs:DeleteAccountPolicy logs:PutIndexPolicy logs:DeleteIndexPolicy logs:PutResourcePolicy logs:DeleteResourcePolicy
   ```

   期待は順に、1行・2行・2行・2行・2行・2行・2行・1行・3行・1行・3行・4行・4行・3行・1行・1行がすべて `allowed`、続く9つが1行・3行・3行・1行・1行・2行・2行・2行・1行ですべて `implicitDeny`、最後の8つが2行・1行・1行・1行・1行・1行・1行・6行ですべて `explicitDeny` であること。plan の role はログの event を読めない。apply-identity は削除と保護を弱める変更を持たず、名指した外の Log Group・delivery source・delivery destination と、Log Group 以外の宛先への delivery destination を作れない。apply の role は、名前で決まるログの構成物と2つの Bucket の配送の許可と、resource type を持たない Logs のアカウント単位の変更を Deny される。`simulate-principal-policy` は IAM の評価だけを見る。CloudWatch Logs が実際の要求をどの ARN で認可するか、条件キーに何が入るかは、ログの構成物を作る CI の apply が確かめる

   unlock-identity の role の権限（[deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定9）は次で確かめる。`s3:ListBucket` の条件キー `s3:prefix` は `simc` で与える

   ```bash
   lock="${key}.tflock"
   simc unlock-identity "arn:aws:s3:::${BUCKET}" "ContextKeyName=s3:prefix,ContextKeyValues=identity/terraform.tfstate.tflock,ContextKeyType=string" s3:ListBucket
   sim unlock-identity "$lock" s3:GetObject s3:DeleteObject
   simc unlock-identity "arn:aws:s3:::${BUCKET}" "ContextKeyName=s3:prefix,ContextKeyValues=identity/terraform.tfstate,ContextKeyType=string" s3:ListBucket
   sim unlock-identity "$lock" s3:PutObject s3:DeleteObjectVersion
   sim unlock-identity "$key" s3:GetObject s3:PutObject s3:DeleteObject
   sim unlock-identity "$state_key" kms:Decrypt
   ```

   期待は順に、1行・2行がすべて `allowed`、続く4つが1行・2行・3行・1行ですべて `implicitDeny` であること。一覧は lock file の key の prefix に限られ、state の key は読めない。`kms:Decrypt` は IAM では許さず、state の鍵の Key Policy が lock file の object に限って許す

10. 実行の記録を Pull Request の本文に残す。実行者（一時 IAM user）、日時、対象アカウント（ID は伏せる）、適用した commit（ADR-0602 決定10）。role の ARN を載せるときも、アカウント ID を伏せる
11. merge の前に、Pull Request の head に対する CI の plan が変更なしを示すことを確かめる（ADR-0602 決定10）。この plan が、immutable 形式の environment の `sub` で role を assume できることの最初の確認になる（[deployment-identity/ADR-0101](../../usecases/deployment-identity/docs/adr/0101-trust-by-immutable-environment-subject.md) 決定7）

途中で失敗したとき:

- lock が残ったら、他に apply が走っていないことを確かめてから `terraform force-unlock <ID>` で外す
- 作られた resource は state に入っている。原因を直した commit を同じ Pull Request に足し、手順4 からやり直す。記録も新しい commit で取り直す
- Key Policy の書き込みが `MalformedPolicyDocumentException` で落ちたら、作ったばかりの role が IAM にまだ伝わっていない。commit を変えずに手順4 からやり直す
- `EntityAlreadyExists` が返ったら、手順3 に戻る
- やり直しの手順6 で Policy か lifecycle が返ったら、それが前の apply の書いたものとして state に在ることを `terraform state list` で確かめる。state に在るものは手順4 の plan が差分を示すので、手順6 の確認に代える。state に無いものが返ったら、誰がなぜ置いたかを確かめるまで止まる

### 段5. CI が apply する

段4 の後の変更は、release line への merge を契機に、CI の apply の job（`terraform-apply.yaml` の `apply-admin-identity`）が apply-identity の role で apply する（ADR-0602 決定4）。人は apply しない。起動の条件と job の規則は [`.github/workflows/README.md`](../../.github/workflows/README.md) の「apply の job」が持つ。

1. apply の workflow を足す Pull Request を merge する前に、管理者が environment `admin-apply-identity` に secret `AWS_ACCOUNT_ID`（管理アカウントの ID）を置く（`gh secret set AWS_ACCOUNT_ID --env admin-apply-identity`）。宣言は secret を持たず、突合も見ない
2. merge の後の最初の run で、`apply-infra-code-gate`・`apply-resource-order-check`・`apply-admin-identity` が緑であること、ログの plan の要約が `No changes.` であること、Policy の違反が無いこと、`OIDC の sub` の行が `:environment:admin-apply-identity` で終わることを確かめる。run の URL を issue に残す（アカウント ID を載せない）。この run は apply の経路の最初の確認であり、段5 の成功ではない
3. 段5 の変更（[`usecases/state-backend/README.md`](../../usecases/state-backend/README.md) の段5 の行）は、前の変更の CI の apply が緑で、その後の CI の plan が変更なしを示してから merge する。apply-identity の権限を足す変更は、その権限で作る resource の変更より先に merge し、その apply が緑になってから次を merge する。待っている run は後から来た run に置き換わるので、続けて merge すると2つが1回の apply に入る
   - plan の job は Pull Request でだけ起動する。apply の後の「変更なし」は、緑になった apply の run を Actions の画面から「Re-run all jobs」で再実行し、ログの plan の要約が `No changes.` であることで確かめる。run の URL を Pull Request に記録する
   - 再実行の apply の job は、plan の要約が `No changes.` でなくても apply まで進む。`No changes.` でなかったときは、その差分は再実行が適用している。要約と run の URL を Pull Request に記録し、次の merge へ進まずに止まる
   - 再実行は次の merge より前に行う。release line の先頭が動いた後は、apply の job が古い commit を拒んで落ちる。そのときは、次の Pull Request の CI の plan が、その Pull Request の変更だけを示すことで代える
4. 権限を足す変更の apply の後、段4 の手順9 と同じ形で、足した Action を `simulate-principal-policy` で確かめ、Pull Request に記録する。Condition を持つ Action は、条件キーを `--context-entries` で与えて確かめる。単一の値の演算子（`Bool` など）は条件キーが無いと一致しない。state の鍵の権限を変えたときは、段4 の手順9 の state の鍵の block で、ログの構成物の権限を変えたときは、同じ手順のログの構成物の block で確かめる
5. lock の解除の workflow（`terraform-unlock.yaml`）を足す Pull Request を merge する前に、管理者が environment `admin-unlock-identity` を作り、secret `AWS_ACCOUNT_ID`（管理アカウントの ID）を置く。environment は、その Pull Request のブランチを checkout した手元で `make apply-environments` を実行して宣言から作り、secret は `gh secret set AWS_ACCOUNT_ID --env admin-unlock-identity` で置く。merge の前に作るのは、存在しない environment を参照した workflow が走ると、GitHub がその environment を保護なしで作るためである（[`.github/settings/README.md`](../../.github/settings/README.md)）。作った後、管理者はその Pull Request のブランチで `make repo-settings-drift` を実行し、宣言と実態が一致することを確かめてよい
6. 一時 IAM user の書き込みを拒否する変更を merge する前に、残った lock を `Terraform Unlock` で外す試行を1回行う。その変更は、段3 で `bootstrap_executor_user_name` を足したアカウントでは、それを外す変更（段5 の最初の変更）であり、足していないアカウントでは、書き込みの拒否を入れる変更である。lock の解除の role の削除と、Key Policy が lock file に限って許す復号は、lock が在るときにしか評価されないので、障害の前に実物で一度評価する。一時 IAM user のコマンドは、段1 の手順1 のブロックで `ACCOUNT_ID` と `BUCKET` を用意してから実行する
   1. identity root を変更する Pull Request を merge し、その CI の apply の run を、`apply` の step が lock を取った後に Actions の画面で取り消す
   2. 一時 IAM user で lock file が在ることを確かめる

      ```bash
      aws s3api list-objects-v2 --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --prefix identity/terraform.tfstate.tflock
      ```

      Terraform は取り消しの信号で lock を外して止まりうる。lock file が返らなければ、この試行は lock を残せていない。手順1 からやり直す
   3. lock file の `LastModified` から、apply の job の `timeout-minutes` に余裕を足した時間（`scripts/tf-local` の定数）を超えるまで待つ
   4. `Terraform Unlock` を起動し、緑であること、ログに lock の ID・経過分・消した lock の版 ID・delete marker の版 ID が出ること、`OIDC の sub` の行が `:environment:admin-unlock-identity` で終わること、手順2 の一覧が lock file を返さないことを確かめる
   5. 取り消した apply の run を「Re-run failed jobs」で再実行し、緑になることを確かめる。release line の先頭の commit のままであること
   6. 結果と run の URL を Pull Request に記録する（アカウント ID を載せない）。手順4 が失敗したら、一時 IAM user で lock file を消してから（「途中で失敗したとき」の `delete-object`）、Pull Request に記録して止まる

途中で失敗したとき:

- 失敗は通知（`notify.yaml`）で届く。原因は run の失敗した step のログから読む。apply の全文はランナーの外に出ない。CI の apply が state を state の鍵で書いた後は、一時 IAM user は Key Policy に名指されないので state を読めず、`make plan-admin-identity` も init が state を読めずに落ちる。terraform のエラーの詳細を見る手段は、失敗した step のログに出る範囲に限られる。plan の出力の Bucket の全文も、段4 で state の鍵で暗号化された後は一時 IAM user には読めない（[`usecases/state-backend/README.md`](../../usecases/state-backend/README.md) の unsupported）
- 書き込みの拒否を入れる変更か、一時 IAM user の例外を外す変更の apply が落ちたときは、診断から始める。ログは apply の失敗を1行に畳むので、拒否で落ちたかは読めない。一時 IAM user で `aws s3api get-bucket-policy --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID"` を読んで宣言（`modules/s3/policy.tf` と `usecases/state-backend/main.tf`）と比べ、`aws s3api list-objects-v2 --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --prefix identity/terraform.tfstate.tflock` で lock の残存を見る。ランナーの `errored.tfstate` はランナーとともに失われるが、失われるのはその apply の Bucket Policy の更新の分で、次の apply の refresh が読み直す。Bucket Policy の例外が CI の role の ARN か state の鍵の ARN と食い違って自分を拒んでいたら、`Terraform Unlock` も同じ原因で効かないことがある。その状態から回復する手順は、このリポジトリは提供しない（ADR-0602 決定9）。Pull Request に記録して止まる
- 拒否の step が secret の不在で落ちたときは、secret を置いてから「Re-run failed jobs」で再実行する。release line の先頭の commit のままなら通り、新しい commit は要らない
- lock が残ったら、Actions の画面から `Terraform Unlock`（`terraform-unlock.yaml`）を起動して外す。規則は [`.github/workflows/README.md`](../../.github/workflows/README.md) の「lock の解除の job」が持つ。job は apply の job の `timeout-minutes` に余裕を足した時間より新しい lock を消さずに失敗するので、そのときは時間を置いて起動し直す。外した後、lock で落ちた apply の run を「Re-run failed jobs」で再実行する
- 段5 のログの構成物の作成が途中で落ちて taint が残ったときは、CI の role はログの構成物の削除の権限を持たないので、次の apply の置き換えは AccessDenied で落ちる。CI だけで回復する手順は、このリポジトリは提供しない。Pull Request に記録して止まる（[deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md) の保証しないこと）
- 人の手元の apply が残した lock は、`Terraform Unlock` では外れない（CI のランナーが apply の job で取った lock だけを外す）。その apply を実行した人が `terraform force-unlock <ID>` で外す
- 一時 IAM user の書き込みが拒否される前は、一時 IAM user で lock file を消してもよい。apply の run が走っていないことを Actions の画面で確かめてから行う。lock file が state の鍵で書かれた後は、`terraform force-unlock` は lock file を読めないので使えない。消すのは版を指定しない削除で、`force-unlock` が行う削除と同じである（版管理の下で delete marker になり、前の版は lifecycle の日数まで残る）。段1 の手順1 のブロックで `ACCOUNT_ID` と `BUCKET` を用意してから実行する

  ```bash
  aws s3api delete-object --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --key identity/terraform.tfstate.tflock
  ```

  一時 IAM user の書き込みが拒否された後は、人の主体は lock file を消せず、`Terraform Unlock` だけが外す。`Terraform Unlock` は、Terraform の lock として読めない lock file を消さない

段5 の成功は、次のすべてを満たしたときとする。

- 段5 の変更をすべて CI の apply が apply し、それぞれの後の CI の plan が変更なしを示した
- 段5 の変更の apply の後、2つの Bucket の `get-bucket-encryption` が段4 の手順8 と同じ値を返した
- state を書き換える最初の CI の apply の後、`aws s3api head-object --bucket "$BUCKET" --expected-bucket-owner "$ACCOUNT_ID" --key identity/terraform.tfstate` が `ServerSideEncryption` `aws:kms` と、`alias/tabp-admin-tfstate` の指す鍵の ARN の `SSEKMSKeyId` を返した。HeadObject の API Reference が `kms:Decrypt` を求めるのは checksum mode のときだけなので、一時 IAM user でも読める見込みである。`AccessDenied` が返ったら、Pull Request に記録して止まる。CI の plan は state を読めたことしか示さず、暗号化の方式を見ないので、代わりにならない
- 一時 IAM user の書き込みが拒否された後、例外に無い IAM の主体（一時 IAM user）による state Bucket への `put-object` が `AccessDenied` を返した。Bucket Policy は `simulate-principal-policy` に入らないので、実際の要求で確かめる。key は state の key でも lock file でもないもの（`verify/<日時>`）にする。拒まれなかったら、その object を消し、Pull Request に記録して止まる
- 一時 IAM user の書き込みを拒否する前に、手順6 の試行で `Terraform Unlock` が残った lock を外し、取り消した apply の run の再実行が緑になった
- 段3 で `bootstrap_executor_user_name` を足したアカウントでは、段5 の最初の変更がそれを外し、その apply の後の `get-bucket-policy` の例外に一時 IAM user の ARN が無かった。Policy Test は env の root を入力にしないので、外したことはこの確認だけが見る
- 一時 IAM user の書き込みが拒否された後、`Terraform Unlock` が lock の無い状態で緑で走り、ログが `✅ lock はありません` を示し、`OIDC の sub` の行が `:environment:admin-unlock-identity` で終わった。この run が、unlock-identity の role が書き込みの拒否の下で lock file の一覧を読めることの確認になる。run の URL を Pull Request に記録する（アカウント ID を載せない）

## 一時 IAM user の削除

bootstrap の段5 が成功するまでは削除しない。それまでは、段5 の手順6 で lock file の在否を確かめる主体と、一時 IAM user の書き込みが拒否されるまで残った lock file を消す主体として残しておく。段5 で CI の apply が state を state の鍵で書いた後は、一時 IAM user は state を読めず、段4 をやり直す手段にはならない。この削除が、bootstrap の実行者に書き込みの権限を残さない（ADR-0602 決定11）ことの最後の手順である —— 削除までは、一時 IAM user が Bucket Policy の拒否を外せる。削除の後、このアカウントに人の IAM の主体は無く、plan の出力の全文を読める人の主体も無い（[`usecases/state-backend/README.md`](../../usecases/state-backend/README.md) の unsupported）。

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
- [ ] `TF_LOCAL_MISSING_STATE_KEY=sse-s3 make plan-admin-identity` が `4 to import, 30 to add, 0 to change, 0 to destroy` を示し、追加が段2 の手順2 の30件だけだった
- [ ] 段1 の読み戻しで、既定の暗号化が `AES256` で `SSE-C` を拒否していた
- [ ] `get-bucket-policy` が `NoSuchBucketPolicy` を、`get-bucket-lifecycle-configuration` が `NoSuchLifecycleConfiguration` を返し、`alias/tabp-admin-tfstate` が無かった
- [ ] plan の要約（アカウント ID を伏せたもの）を Pull Request に載せた

段3 の終了時:

- [ ] `env/<env>/identity/main.tf` の `github` の4つの値が、自分のリポジトリのものである
- [ ] `use_immutable_subject` が `true` で、`sub_claim_prefix` が `repo:<owner>@<owner_id>/<repo>@<repo_id>` である
- [ ] `module "state_backend"` の `bootstrap_executor_user_name` が、一時 IAM user の名前である
- [ ] 値を変えた commit で、段2 の plan が段2 の手順2 の件数と一覧を示した

段4 の終了時:

- [ ] OIDC provider と4つの role が、段4 の前に存在しなかった
- [ ] 段4 の plan の要約（アカウント ID を伏せたもの）を Pull Request に載せた
- [ ] apply の前に、その commit で plan 以外の required check が成功し、承認者が要求される設定ではその承認を得ていた
- [ ] apply の直前に `get-bucket-policy` が `NoSuchBucketPolicy` を、`get-bucket-lifecycle-configuration` が `NoSuchLifecycleConfiguration` を返した
- [ ] apply の後の `make plan-admin-identity` が `No changes.` を示した
- [ ] provider の audience、role の policy、Bucket Policy と lifecycle、2つの Bucket の既定の暗号化、state の鍵と Key Policy の読み戻しが宣言どおりだった
- [ ] `simulate-principal-policy` の行数と判定が、段4 の手順9 の期待どおりだった
- [ ] 実行の記録（実行者、日時、対象アカウント、commit）を Pull Request の本文に残した
- [ ] merge の前に、CI の plan が変更なしを示した

段5 の終了時:

- [ ] apply の workflow の merge の前に、environment `admin-apply-identity` の secret `AWS_ACCOUNT_ID` を置いた
- [ ] lock の解除の workflow の merge の前に、environment `admin-unlock-identity` を宣言から作り、その secret `AWS_ACCOUNT_ID` を置いた
- [ ] 最初の run が緑で、plan の要約が `No changes.`、`OIDC の sub` が `:environment:admin-apply-identity` で終わった
- [ ] 段5 の変更の各 apply の後、CI の plan が変更なしを示した
- [ ] 2つの Bucket の `get-bucket-encryption` が、state の鍵による SSE-KMS と `BucketKeyEnabled` `false` を返した
- [ ] `head-object` で、state が state の鍵で暗号化されていた
- [ ] 一時 IAM user の `put-object` が state Bucket で `AccessDenied` を返した
- [ ] 一時 IAM user の書き込みを拒否する前に、取り消した apply の残した lock を `Terraform Unlock` が外し、その apply の再実行が緑になった
- [ ] 段3 で足した `bootstrap_executor_user_name` を段5 の最初の変更で外し、`get-bucket-policy` の例外に一時 IAM user の ARN が無かった
- [ ] 一時 IAM user の書き込みが拒否された後、`Terraform Unlock` が lock の無い状態で緑で走った

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
