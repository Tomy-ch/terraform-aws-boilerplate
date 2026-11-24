# deployment-identity

GitHub Actions が長期のアクセスキーを持たずに AWS を操作するための、OIDC provider と CI の role を1つのアカウントに持つユースケース。bootstrap の構成物であり（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）、各 env の identity root から呼ばれる（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定3・5）。

## 解決する問題

CI は OIDC で role を assume して AWS を操作する（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定12）。role は plan / apply / apply-identity と、lock の解除の unlock-identity（identity root）と unlock（workload root）に分かれ（同 決定13）、信頼条件は当該リポジトリの environment に限られる（同 決定14）。このユースケースは、その provider と CI の role、それぞれの権限を Terraform の Desired State に置く。

## 実行形態とネットワーク境界

- GitHub Actions のランナーが、AWS STS の公開 endpoint で `AssumeRoleWithWebIdentity` を呼ぶ。AWS の上で動く処理は無い
- 外への egress は無い

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、33文字以内） | role 名の先頭に置くプロジェクトの token。最も長い role 名（`<project>-log-archive-ci-unlock-identity`）を IAM の上限の64文字に収める。`state-backend` の Bucket 名の上限が、実効的にはさらに短く縛る |
| `env` | `dev` / `stg` / `prd` / `sandbox` / `admin` / `security` / `log-archive` | role を置くアカウントの env の token。GitHub の environment 名の前半にもなる。`admin`（管理アカウント）では、apply-identity と plan の role に統制の構成物の権限が加わる |
| `github` | `object({ owner, owner_id, repository, repository_id })`（すべて `string`） | role を assume してよいリポジトリの所有者とリポジトリの名前、変わらない数値の ID |
| `state_bucket_arn` | `string`（S3 Bucket の ARN） | このアカウントの state Bucket（`state-backend` の `bucket_arn`） |
| `state_key_arn` | `string`（KMS の鍵の ARN。alias の ARN と多リージョンの鍵は受けない） | このアカウントの state の鍵（`state-backend` の `state_key_arn`）。plan と apply-identity の role の鍵の権限と、apply の role の鍵の Deny をこの鍵に絞る。role と同じ partition・リージョン・アカウントの鍵でなければ、plan で（ARN が plan で決まらなければ apply で）落ちる。Key Policy の置き換えを待つ値を渡すと、identity root で依存が循環する（Key Policy が名指す role の作成が、この ARN を持つ apply-identity の inline policy の後に来るため） |
| `plan_output_bucket_arn` | `string`（account-regional の S3 Bucket の ARN。名前は `-<account>-<region>-an` で終わる） | このアカウントの plan の出力の Bucket（`state-backend` の `plan_output_bucket_arn`）。plan の role はその `pr/*` と `branch/*` の key に、apply-identity の role は `apply/*` の key にだけ書ける |
| `apply_output_bucket_arn` | `string`（account-regional の S3 Bucket の ARN。名前は `-<account>-<region>-an` で終わる） | このアカウントの workload root の apply の出力の Bucket（`state-backend` の `apply_output_bucket_arn`）。apply-identity の role の作成と設定と、plan の role の読み取りをこの Bucket に絞る。apply の role はその object にだけ書ける |

| `break_glass_arns` | `list(string)`（path が `/break-glass/` の IAM user・group・role の ARN。1つ以上4つ以下。inline policy の長さが上限に収まることは、入力が許す最も長い名前の組成 —— `admin` は user・group・role を各1つ以上含むので group 2つと user・role、それ以外は group 4つ —— でテストが測る） | このアカウントの break-glass の ARN（`organization-baseline` の `break_glass_arns`）。apply の role はこれらへのすべての Action を拒む。`admin` では、plan と apply-identity の role の読み取りの Resource にもなり、user・group・role をそれぞれ1つ以上含める。role と同じ partition・アカウントの ARN でなければ plan で落ちる。ARN は作る前に決まる値（path と名前から組んだもの）を渡す。`admin` では、role の名前を、メンバーへ break-glass の role を配る StackSet の名前としても読み、apply-identity の role にその StackSet の作成と更新を許す（削除は許さない）。そのため role の名前は英字で始まり英数字とハイフンだけとし、違えば plan で落ちる |

`state_key_arn`・`apply_output_bucket_arn`・`break_glass_arns` は既定値を持たない必須入力で、`state_key_alias_name` の入力は無い。どちらも利用者の入力の変更を要する破壊的変更である（[ADR-0202](../../docs/adr/0202-internal-topology-as-implementation-detail.md) 決定4）。

| 出力 | 使う側 |
| --- | --- |
| `plan_role_arn` | 人が確かめるときの参照。CI の plan の job は assume の前に state を読めないので、この output を読まず、`scripts/tf-local` の `role-arn` が同じ名前の規則で ARN を組む（規則の一致は `tf-local` のテストが見る）。`state-backend` の `ci_role_arns.plan`（state の鍵の Key Policy） |
| `apply_role_arn` | workload root の apply の job と lock を残す job。`state-backend` の `ci_role_arns.apply`（state Bucket の Bucket Policy の、workload root の state と lock file の書き込み。[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定9。state の鍵の Key Policy の、S3 経由の暗号化と復号。apply の出力の Bucket の Bucket Policy の書き手） |
| `apply_identity_role_arn` | `terraform-apply.yaml` の `apply-admin-identity` と `terraform-lock-hold.yaml` の `hold-admin-identity`。`state-backend` の `ci_role_arns.apply_identity`（state の鍵の Key Policy と、state Bucket の Bucket Policy の identity root の state と lock file の書き込み） |
| `unlock_identity_role_arn` | `terraform-unlock.yaml` の `unlock-admin-identity`（`scripts/tf-local` の `role-arn` が同じ名前の規則で ARN を組む）。`state-backend` の `ci_role_arns.unlock_identity`（state の鍵の Key Policy の、lock file に限った復号と、state Bucket の Bucket Policy の lock file の削除） |
| `unlock_role_arn` | workload root の lock の解除の job。`state-backend` の `ci_role_arns.unlock`（state Bucket の Bucket Policy の、workload root の lock file の削除と、state の鍵の Key Policy の、その object に限った復号） |
| `github_oidc_provider_arn` | `image-registry` の `github_oidc_provider_arn`（push の role の信頼条件の Principal。[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定19） |
| `permissions_boundary_arns` | IAM の role を作る workload root の usecase（`ops-notification` の `permissions_boundary_arn`）。key は usecase の名前（`ops-notification`）、値は `<project>-<env>-ci-boundary-<usecase>` の ARN。workload root は identity root の state を読めないので、env の root が同じ名前から ARN を組んで渡す（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定15）。identity root の boundary と workload root が渡す ARN の一致は、env の root を配線するまで（#330）検査が無く、Policy Test（`PLAN-WRB-*`）は名前の形だけを見る |

role の ARN は、role の computed の `arn` ではなく、role の名前とアカウント・partition から組む。role の作成と置換の plan でも値が決まるので、それを名指す state の鍵の Key Policy が CI の `plan.kms_key_policy`（`PLAN-KMS-09`）で落ちない。`internal/github-oidc/tests/trust.tftest.hcl` の `role_arns_do_not_read_computed_arn` が見る。同じ名前のまま role を作り直す plan（taint、`-replace`、`moved` の無い address の変更）では ARN が変わらず、Key Policy が書き直されないので、作り直した role は鍵を使えない。CI はその plan を `PLAN-KMS-10` で落とす。Key Policy を同じ apply で作り直すには、`conftest/plan/decl.json` の `stateful_protection.allow` に Key Policy の置換を宣言する（`plan.stateful_protection` が Key Policy の置換を守るため）。名前を resource から読むことで role の作成を Key Policy の更新より先に走らせる順序は、テストが見ない

role と environment の名前は入力から決まる。利用者は選ばない（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定3）。

| 役割 | role 名 | GitHub の environment | 使う場面 |
| --- | --- | --- | --- |
| plan | `<project>-<env>-ci-plan` | `<env>-plan` | identity root と workload root の plan（lock を取らない） |
| apply | `<project>-<env>-ci-apply` | `<env>-apply` | workload root の apply と、その lock を残す job（ADR-0604 決定8） |
| apply-identity | `<project>-<env>-ci-apply-identity` | `<env>-apply-identity` | identity root の apply と、その lock を残す job（`terraform-lock-hold.yaml`。ADR-0604 決定8） |
| unlock-identity | `<project>-<env>-ci-unlock-identity` | `<env>-unlock-identity` | identity root の state に残った lock の解除（`terraform-unlock.yaml`） |
| unlock | `<project>-<env>-ci-unlock` | `<env>-unlock` | workload root の state に残った lock の解除 |

role の集合は CI の入口の種別の登録簿（`scripts/lib/cientry`）の role と過不足なく一致し、`internal/github-oidc` の `local.roles` との一致は `lib/cientry` のテスト（`TestCheckTerraformRoles`）が、この表の役割の列との一致は同じく `Test_registry_checkReadmeRoles` が見る。role を足すときは、同じ変更で登録簿の種別にその role を持たせる。どの入口も取らない role は作らない。

- ログの所在: 無い。`AssumeRoleWithWebIdentity` の記録は CloudTrail が持ち、その配置は `security-tooling` と `audit-evidence` が持つ
- 基盤系アラームの通知先 Topic ARN: 受けない。フェーズ0 の `deployment-identity` は対象外である（[scope.md 第7節](../../docs/v1.0-plan/scope.md)）

## 持つもの

- OIDC provider（`https://token.actions.githubusercontent.com`、audience は `sts.amazonaws.com` だけ）
- 表の role。信頼条件は [deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md)、権限は [deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) が決める。workload root の usecase の構成物の権限は、provider の region と us-east-1（aws の partition のとき）の2つに及ぶ（[ADR-0208](../../docs/adr/0208-us-east-1-resources-by-region-argument.md)）
  - plan: state Bucket の両方の key の読み取りと、identity root の resource の読み取り。state の鍵の設定の読み取りと、alias の一覧。CI の customer managed policy（名前が `<project>-<env>-ci-` で始まる policy）の読み取り。workload root の usecase の構成物（名前が `<project>-<env>-` で始まる Topic・Scheduler・IAM の role と policy・予算、鍵、Chatbot の構成、Cost Anomaly Detection の monitor と subscription）の読み取り。`admin` では、組織・OU・policy・root の資格情報の中央管理・password policy・break-glass の読み取り
  - apply: workload root の key と lock file の読み書きと、init の前の state Bucket の暗号化の設定の読み取りと alias の一覧と、apply の出力の Bucket の object への書き込み。名前が `<project>-<env>-ci-` で始まる IAM の role と customer managed policy（CI の role、apply-identity の role の manage の policy、統制の policy、workload の2つの policy、permissions boundary）と OIDC provider、identity root の key と lock file、plan の出力の Bucket とその object は読み取りを含めて Deny する。state Bucket は `s3:ListBucket` と `s3:GetEncryptionConfiguration` のほかを、state Bucket の object は版の削除を、state の鍵は管理（読み取りを含む）を Deny する。apply の出力の Bucket はすべてを、その object は `s3:PutObject` のほかを Deny する。統制の構成物の書き込み（Organizations の書き込みの動詞、メンバーの閉鎖（`account:CloseAccount`）、root の資格情報の中央管理、password policy の書き込み、Identity Center の permission set と割当の書き込み、`sts:AssumeRoot`）を Resource `"*"` で、path が `/break-glass/` の IAM user・group・role（break-glass の ARN を含む）へのすべての Action を Deny する。permissions boundary の除去（`iam:DeleteRolePermissionsBoundary`）と、lockout の安全チェックの迂回（`kms:CreateKey`・`kms:PutKeyPolicy`）を Deny する。tag `<project>:bootstrap` を持つ anomaly monitor と subscription（管理アカウントの bootstrap の前提の AWS services monitor と email の subscription に人が付ける）の更新・削除と tag の除去を Deny する。workload root の usecase の構成物を作る権限は、apply の role に付ける customer managed policy（`<project>-<env>-ci-apply-workload`）に置く（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定13-18）。その policy の IAM の書き込みは、小文字の usecase の接頭辞（ops-notification は `<project>-<env>-ops-`）の role と policy に限り、role の書き込みは usecase ごとの permissions boundary（`<project>-<env>-ci-boundary-<usecase>`）を付けるときだけ許す。鍵と Topic の削除は与えない。予算と anomaly monitor と subscription の削除は与える。`aws-portal` の Action は与えない（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定20）
  - apply-identity: identity root の key と lock file の読み書きと、identity root の resource の管理。自分を含む CI の role の信頼条件と権限を変更できる。state の鍵の Key Policy と rotation を設定できる。鍵と alias の作成、鍵の削除・無効化、alias の削除・付け替えはできない。plan の出力の Bucket の `apply/*` へ apply の出力を書ける。その Bucket の object の削除の権限は IAM で与えない。workload root の apply の出力の Bucket を作り、設定できる。その Bucket の削除の権限は与えない。CI の customer managed policy（apply-identity の role の policy `<project>-<env>-ci-apply-identity-manage`、apply の role の workload の policy `<project>-<env>-ci-apply-workload`・`<project>-<env>-ci-apply-workload-read`・`<project>-<env>-ci-apply-workload-2`、usecase ごとの permissions boundary `<project>-<env>-ci-boundary-<usecase>`、`admin` では統制の policy `<project>-admin-ci-apply-identity-governance`）の作成・版の更新・削除を inline で持つ。それらの読み取りは、名前が `<project>-<env>-ci-` で始まる policy に限って持つ。workload の3つの policy の apply の role への付与と、`<project>-<env>-ci-apply-workload-read` の plan の role への付与と、`<project>-<env>-ci-apply-identity-manage`（`admin` では加えて統制の policy）の自分の role への付与を、`iam:PolicyARN` で限って inline で持つ。`admin` では統制の構成物の読み取りも持つ。inline policy は自分を保つのに要る権限（核）だけを持ち、3つの Bucket の設定、apply の出力の Bucket の作成、OIDC provider の管理、state の鍵の rotation と Key Policy の設定は、env によらず自分の role に付ける `<project>-<env>-ci-apply-identity-manage` に置く。統制の構成物の作成と設定の権限は、統制の customer managed policy に置く（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定3・7・21）
  - unlock-identity: identity root の lock file の一覧（`s3:ListBucket` を `s3:prefix` で限る）・読み取り・削除だけ。`s3:PutObject` と `s3:DeleteObjectVersion` を持たず、偽の lock を置けず、版も消せない。lock file の復号は、state の鍵の Key Policy がその object に限って許す
  - unlock: workload root の lock file の一覧・読み取り・削除だけ。unlock-identity と同じ形で、受け持つ lock file だけが違う
  - 暗号操作（`kms:Decrypt` など）は IAM では与えない。state の鍵の Key Policy が CI の role を名指して許す
- role ごとの inline policy と、inline policy と managed policy の排他（帯域外で足された policy を次の apply で剥がす。managed policy は、apply の role の workload の2つの policy と、apply-identity の role の manage の policy と、`admin` の apply-identity の role の統制の customer managed policy だけを残す）
- apply の role が作る role の permissions boundary（role を作る workload root の usecase ごとに1つ。`ops-notification` の分は `<project>-<env>-ci-boundary-ops-notification`）。Allow はその usecase が作る role の要る Action、Deny は apply の role の bootstrap の Deny と同じ文（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定15・16）
- role の ARN を、role の resource ではなく partition・アカウント・role 名から組む。apply-identity の role と inline policy は他の role と別の resource で、他の role はその inline policy の書き込みの後に作る。role を足す変更は、1回の apply で、その role を管理する権限を apply-identity に足してから role を作る（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定4）

これらは usecase に固有の module `internal/github-oidc/` が持つ（[ADR-0102](../../docs/adr/0102-use-case-centric-scope.md) 決定15）。信頼条件が GitHub の claim に、権限が bootstrap の構成物と state の key に固有で、どの usecase でも同じ形で成立する機構ではないためである。

## supported / unsupported

supported:

- github.com のリポジトリで、OIDC の `sub` が immutable 形式（`use_immutable_subject: true`）であるもの
- 1つのアカウントに、このユースケースの OIDC provider を1つ置く構成

unsupported:

- 1つのアカウントに複数の env を置く構成と、Organizations に属さない単独のアカウントで使うこと（`docs/v1.0-plan/scope.md` 第10.1節、[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定1）
- 旧形式の `sub`（`repo:<owner>/<repo>:...`）のリポジトリと、GitHub Enterprise Server（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定5）。旧形式のリポジトリは、opt-in で immutable 形式にしてから使う
- Enterprise Managed User の個人アカウントが所有するリポジトリ。login が `_` を含み、`github.owner` の検査が受け付けない。その login が `sub` にどう現れるかを確かめていない
- fork からの Pull Request で plan を成立させること。plan の job は fork の Pull Request で assume の前に失敗する（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14）
- 締め出しから回復する手順。回復を行う人の経路は `organization-baseline`（未実装）の break-glass である（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定9、[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19）
- 管理アカウントの root による、role と provider の変更を防ぐこと（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定11）
- 同じアカウントに、このユースケースの外で GitHub Actions の OIDC provider を作ること。provider は URL ごとに1つしか置けない

保証しないこと:

- 同じリポジトリへ Pull Request を開ける主体が、workflow を書き換えて plan の role を assume し、state を読むこと（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) の保証範囲）
- apply-identity の role が、自分と他の CI の role の権限を広げる変更を apply すること。リリース線からデプロイブランチへの merge が apply になるので、止めるのはリリース線と昇格の Pull Request のレビューである。このユースケースの `.tf` と identity root（`env/<env>/identity/`）は `.github/CODEOWNERS` の対象ではなく（対象は `reference/tests/policy-input.tftest.hcl` だけ）、レビューを要求するかは ruleset の承認者数に依る。apply-identity の role の権限の拡大を見る Policy Test も無い（plan の role は `plan.deployment_identity_plan_role` が読み取りに限り、unlock-identity と unlock の role は `plan.deployment_identity_unlock_role` が lock file の一覧・読み取り・削除に限る。apply の role は `plan.deployment_identity_apply_role` が、bootstrap の構成物の ARN への Deny の形と、ほかの policy が付かないことを見る。Allow の拡大は見ないが、Allow は Deny に勝たない）
- apply の role が作るか渡す別の主体、bootstrap の構成物の ARN を通らない要求（S3 の access point 等）、Resource `"*"` でしか与えられない Action を経た、bootstrap の構成物の変更（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) の保証範囲。別の主体は #254）
- apply の role が、Identity Center の permission set と割当のほかの書き込み（instance・application）と identitystore の書き込み（user・group・グループの membership）を行わないこと。`DenyGovernanceChange` はこれらを拒まない（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) の保証範囲）
- 帯域外で足された policy が、次の apply までの間に有効であること。検出は定期 plan（#77）、剥がすのは apply
- write 権限を持つ主体が、保護対象のパターンに当たるブランチを新しく作り、その上で書き換えた workflow から plan の environment の job を走らせること。apply 系と lock の解除の environment はデプロイブランチだけを許すので、そこからは届かない。ruleset は `creation` の規則を持たず、`pull_request` と required status checks の規則が push によるブランチの作成を止めるかを、GitHub の文書は述べていない。デプロイブランチが無い間（作る前）は、その名前のブランチを作った主体が apply 系の environment に届く（#223）
- apply の role が、tag `<project>:bootstrap` を持たない anomaly monitor と subscription を、finops のものでなくても変更・削除すること。管理アカウントの bootstrap の前提の monitor と subscription は、人が [`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の 2-4 で tag を付けたときだけ Deny が守る（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) の保証範囲）

## 所有者

- OIDC provider と CI の role は、このユースケースが所有する。provider の ARN は出力 `github_oidc_provider_arn` で渡し、本リポジトリの外の CI を信頼する role（`image-registry` の push の role。[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定19）がそれを使う
- state Bucket と state の鍵は `state-backend` が所有する。このユースケースは Bucket と鍵の ARN を受け、role の権限の Resource に使う

## 依存するユースケースと判断 Issue

- ユースケース: `state-backend`（`bucket_arn`、`state_key_arn`、`plan_output_bucket_arn`、`apply_output_bucket_arn`）。`state-backend` は、このユースケースの `plan_role_arn`・`apply_role_arn`・`apply_identity_role_arn`・`unlock_identity_role_arn`・`unlock_role_arn` を `ci_role_arns` に受ける
- 判断 Issue: #130、#98、#142、#96、#83。いずれも閉じている
- GitHub 側の environment は [`.github/settings/environments.json`](../../.github/settings/environments.json) が宣言する。名前はこのユースケースの role の表の `<env>-<role>` の写しで、`env/` の root が渡す `env` ごとにすべての role の名前が宣言に揃っていることを `make repo-settings-check` が見る。plan の role は `terraform-gates.yaml` の `plan-admin-identity` が、apply-identity の role は `terraform-apply.yaml` の `apply-admin-identity` が、unlock-identity の role は `terraform-unlock.yaml` の `unlock-admin-identity` が assume する。apply の role は `terraform-apply.yaml` の `apply-admin-workload` と `terraform-lock-hold.yaml` の `hold-admin-workload` が、unlock の role は `terraform-unlock.yaml` の `unlock-admin-workload` が assume する

identity root に構成物を足すときは、権限の変更を先に merge してから、その権限で作る resource を apply する（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) の影響）。

## 検証方法

- Unit Test（mock）: `internal/github-oidc/tests/` が、CI の role の信頼条件と policy の文書、名前、セッションの上限、OIDC provider の URL と audience、managed policy の attachment、inline policy と managed policy の排他、apply の role の workload の2つの policy と usecase ごとの boundary の文書と長さ（provider の region に us-east-1 を並べた aws の partition の形を含む）を確かめる。`tests/validation.tftest.hcl` が入力の検査を、`tests/contract.tftest.hcl` が公開 output を、`reference/tests/plan.tftest.hcl` が reference の値を確かめる
- 上流との突き合わせ（`make partitions-drift`）: inline policy の長さを測る境界の候補（`tests/permissions.tftest.hcl` の `aws_partitions`）を、`internal/github-oidc` の版の制約を満たす最新の provider が使う partition と region の一覧と比べる。週次の `partitions-drift-report.yaml` がずれを issue へ書く。必須検査ではないので、ずれた写しのまま merge は止まらない
- Policy Test（`make tf-policy`）: `reference/tests/policy-input.tftest.hcl` の plan に対して、CI の role の address の全 instance の信頼条件の形（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14・15）と、plan の role の Allow が読み取りと plan の出力の `pr/*`・`branch/*` への `s3:PutObject` だけであること（同 決定13）と、Allow の Action の wildcard（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定8）と `NotAction`・`NotResource` の不在と、Allow の Resource `"*"` が `resource_wildcard_actions` の宣言（`kms:ListAliases`、統制の読み取り、resource-level 権限を持たない workload の Action）に限られること（同 決定9）と、KMS の暗号操作の Allow の不在（同 決定12）と、plan の role の Action の service の名前に wildcard を使わないこと（`PLAN-ROLE-05`）と、動詞が読み取りの形でない読み取りの Action が `plan_role.read_actions` の宣言に限られ、宣言が形を保ち、動詞が View で始まり、plan の role で使われること（`PLAN-ROLE-00`）を確かめる。`state-backend` の reference の plan と結合した入力で、apply の role の inline policy が apply の role に付き、bootstrap の構成物の S3・IAM の ARN ごとに読み取り以外を条件なしの Deny で拒み、鍵ごとに鍵の ARN への Deny が同じ module の Key Policy の委任の Action のすべてを覆い、inline policy と宣言した workload の2つの policy のほかの policy が apply の role に付かないことと（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定5・17）、unlock-identity の role の Allow が state の Bucket の identity root の lock file の一覧・読み取り・削除に限られ、Key Policy がその復号を IAM と同じ lock file の object に限ることと（同 決定9）、unlock の role の Allow が workload root の lock file の一覧・読み取り・削除に限られることと（同 決定11）、apply-identity の role の plan の出力の Bucket の object への書き込みが `apply/*` への `s3:PutObject` だけで（統制の customer managed policy の文書を含む）、宣言した inline policy と統制の customer managed policy のほかの policy が apply-identity の role に付かないことも確かめる（同 決定7・10）。apply の role の inline policy が、`conftest/plan/decl.json` に宣言した統制の書き込みの Action を、Resource `"*"` の条件なしの Deny で拒むことも確かめる（同 決定5。`PLAN-APPLY-06`）。apply-identity と apply の role の managed policy の排他と attachment が対になること（`PLAN-ATTACH-*`）と、apply の role の workload の2つの policy の IAM の書き込みが小文字の usecase の接頭辞に限られ、role の書き込みが boundary を条件に持ち、boundary の Deny が apply の role の Deny と同じ文であること（同 決定13-15。`PLAN-AWP-*`）と、ops-notification の reference の plan の role がその usecase の boundary を持つこと（`PLAN-WRB-*`）も確かめる。Policy の一覧は [`conftest/plan/README.md`](../../conftest/plan/README.md)
- Static Analysis（`make resource-order-check`）: `internal/github-oidc` の `aws_iam_role.this` が `depends_on` に apply-identity の inline policy を持つことを、HCL のソースで確かめる（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定4）。宣言は [`conftest/hcl/resource_order/`](../../conftest/hcl/resource_order/README.md)
- Static Analysis（`make layer-check`）: [ADR-0102](../../docs/adr/0102-use-case-centric-scope.md) 決定14・17・18 の層の規則を、HCL のソースで確かめる。規則と宣言は [`conftest/hcl/layer/`](../../conftest/hcl/layer/README.md)
- Static Analysis（`make root-check`）: [ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定2-4 の root が呼ぶ usecase の構成と、決定7・8 の backend 設定を、HCL のソースで確かめる。このユースケースを bootstrap の構成物を持つ usecase として宣言するのは [`conftest/hcl/root/decl.json`](../../conftest/hcl/root/decl.json)
- 段2 の突合と段4 の確認: 人が実 AWS で行う（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md)）
- 拒否の確認（`make ci-assume-denied`）: `terraform-gates.yaml` の `plan-admin-identity` が、Pull Request ごとに、plan の role を assume した後で、同じ job の OIDC token（`sub` は environment `admin-plan`）による、`pull_request` で起動する種別のほかのすべての role（ci.mk の `CI_DENIED_ROLES`。登録簿との一致は `scripts/lib/cientry` が見る）の assume が、STS の `AccessDenied` で拒否されることを実 AWS で確かめる。assume が通るか、拒否が `AccessDenied` でなければ（token の不正・接続の失敗を含む）job が落ちる。step の存在と位置は `make oidc-job-lint` が見る
- 実環境テストの形: [ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の「破棄すると認証や組織の構造を失う」に当たる。destroy しない。Policy Test、拒否されるべき主体の assume が拒否されることの確認（`make ci-assume-denied`）、drift 0 で代える（[scope.md 第11.1節](../../docs/v1.0-plan/scope.md)）

sandbox のアカウント（#21）は無い。配線された後に遡って検証する。それまでを含めて、検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- deployment branch policy の扱いの想定違いで、plan の environment が Pull Request を許さないこと、apply 系の environment が Pull Request を許すこと
- 権限の列挙が Provider の実際の呼び出しを覆わず、CI の plan や apply が AccessDenied で失敗すること。apply-identity の読み取りが欠けると、その修正の apply も失敗し、回復の契機になる
- 実 API が読み戻した信頼条件や policy が宣言と違い、plan が毎回差分を出すこと
- fork からの Pull Request で OIDC token が得られるかどうか
- 他のリポジトリの job からの assume が拒否されること。この repository の job は、他のリポジトリの `sub` を持つ token を作れない
- `<env>-plan` 以外の environment の job の token で、ほかの役割の role の assume が拒否されること（例: `admin-apply-identity` の token で unlock-identity の role）。`make ci-assume-denied` が試すのは plan の job の token だけで、ほかの組み合わせは信頼条件の形を Policy Test が見るだけである
- `make ci-assume-denied` の拒否が、信頼条件によるものであること。STS は role が無いときも `AccessDenied` を返すので、`.tf` に足してまだ apply していない role では、拒否と不在を区別しない。role の実在は、apply・lock の解除・lock を残す job の assume が示す
- 権限の Resource に書いた state の key（`identity/terraform.tfstate`・`workload/terraform.tfstate`）が、identity root の `backend.tf` の `key` と一致すること。どちらも [ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定7 の固定値を写している。`backend.tf` の `key` は `make root-check` が決定7 の値と突き合わせるが、権限の Resource の側を突き合わせる検査は無い。片方だけを変えると、CI の role が state を読み書きできなくなる
- identity root が `deployment-identity` に渡す値（`github` の ID、`state_bucket_arn`・`state_key_arn`・`plan_output_bucket_arn` の配線）が正しいこと。identity root のテストは role の ARN と `state-backend` の出力しか観測できず、`deployment-identity` への配線を見るのは CI の plan だけである。鍵の ARN は precondition が partition・リージョン・アカウントまでを見るが、同じアカウントの別の鍵との取り違えは見ない
- apply の role の Bucket・object・IAM の側の Deny が、実際の要求で文書どおりに評価されること。Policy Test は reference の plan で Deny の形を見るだけで、`simulate-principal-policy` は IAM の評価だけを見る。apply の role が作るか渡す別の主体、構成物の ARN を通らない要求、Resource `"*"` でしか与えられない Action を経た変更も、どの検査も見ない
- apply の role の鍵の Deny が、実際の要求で文書どおりに評価されること。`simulate-principal-policy` は IAM の評価だけを見る
- `state-backend` の出力の2つの Bucket の Bucket Policy が、plan の role の `pr/*`・`branch/*`、apply-identity の role の `apply/*`、apply の role の apply の出力の Bucket への書き込みを、実際の要求で拒まないこと。誤ると plan の job が全文を置けず、それを直す昇格の Pull Request も merge できない。Bucket Policy を変える昇格の merge の前に、人が `make tf-policy` の後の `make bootstrap-verify STAGE=output-policy POLICY_SOURCE=reference` で `simulate-principal-policy --resource-policy` の評価を確かめる。Policy Test は reference の plan で文の数を、Unit Test は文書を見るだけである
- 同じ apply で role を足すとき、apply-identity の inline policy の書き込みが、role の作成（`iam:CreateRole`）までに IAM の評価へ反映されること。反映が遅れるとその apply は AccessDenied で失敗する。失敗の段は apply なので、`revert-plan` が revert のブランチを用意して通知するが、その revert の Pull Request は開かない。run の「Re-run failed jobs」で再実行し、緑になったら用意された revert のブランチを消す。再実行も同じ AccessDenied で落ちるなら反映の遅れではないので、止まって通知の revert を使うかを判断する
- 同じ run の中で、identity root の apply が書いた apply の role の権限と permissions boundary が、続く workload root の apply までに IAM の評価へ反映されること。反映が遅れると `apply-admin-workload` が AccessDenied で失敗し、上と同じく revert のブランチと通知が出る。扱いも上と同じで、再実行して緑になれば revert のブランチを消す

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-10-25（KMS の権限の分は 2026-11-07、alias の操作の権限は 2026-11-12、Budgets と Cost Anomaly Detection の権限は 2026-11-21、Identity Center の Deny と apply-identity の manage の policy は 2026-11-24）

- AWS / HashiCorp が publisher の GitHub Actions の OIDC の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `github-oidc` / `oidc` / `github` / `github-actions` / `iam-role`、`provider=aws` で引いた結果に、namespace `aws`・`aws-ia`・`hashicorp`・`aws-samples` が無い）
- 参照した一次資料: IAM User Guide の「Create a role for OpenID Connect federation」の GitHub の節、「Available keys for OIDC federation」の GitHub の表、「Create an OpenID Connect (OIDC) identity provider in IAM」、AWS Provider 6.70.0 の `aws_iam_openid_connect_provider` / `aws_iam_role` / `aws_iam_role_policy` / `aws_iam_role_policies_exclusive` / `aws_iam_role_policy_attachments_exclusive` の文書・schema・source
- managed policy の付け方について参照した一次資料: AWS Provider v6.73.0 の `aws_iam_role`・`aws_iam_policy`・`aws_iam_role_policy_attachment`・`aws_iam_role_policy_attachments_exclusive` の文書と、`internal/service/iam/role.go`・`policy.go`・`role_policy_attachments_exclusive.go`、IAM API Reference の DeletePolicy（2026-11-20）。managed policy を policy ごとの `aws_iam_role_policy_attachment` で付け、`aws_iam_role_policy_attachments_exclusive` を併用して同じ ARN を載せる（`aws_iam_role` の文書の形）。排他の resource は破棄で policy を外さず、`aws_iam_policy` の削除は付いたままだと失敗するため（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定21）。`force_detach_policies` は採らない（role の削除だけに効き、policy の削除と置換には効かない）
- KMS の権限について参照した一次資料: AWS KMS Developer Guide の「AWS KMS permissions」（Action ごとの resource と条件キー）、「AWS KMS condition keys」（`kms:BypassPolicyLockoutSafetyCheck`）、「Controlling access to aliases」、「Least-privilege permissions」、「Default key policy」、AWS Provider 6.70.0 の `internal/service/kms` の `key.go`・`alias.go`・`key_policy.go` の source
- Budgets と Cost Anomaly Detection の権限について参照した一次資料: Service Authorization Reference の AWS Budgets と AWS Cost Explorer Service の頁と programmatic の JSON（`https://servicereference.us-east-1.amazonaws.com/v1/budgets/budgets.json`・`.../v1/ce/ce.json`、Version v1.5）、AWS Cost Management User Guide の「Using identity-based policies」と「Migrating access control for AWS Cost Management」、AWS Billing User Guide の「Migrating access control for AWS Billing」「Mapping fine-grained IAM actions reference」「Using the console to bulk migrate your policies」、AWS Provider v6.73.0 の `internal/service/budgets/budget.go`・`internal/service/ce/anomaly_monitor.go`・`anomaly_subscription.go`・`internal/provider/sdkv2/tags_interceptor.go` の source、Security Hub の IAM の controls の頁。採用した知見: API と IAM の Action の対応（`budgets:ModifyBudget`・`ViewBudget`）、ARN の形と条件キー、Read が tag を読む呼び出し、`ce:Update*`・`Delete*`・`UntagResource` が `aws:ResourceTag` を受けること。採用しなかった知見: Budgets の API の表が挙げる `aws-portal` の Action（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定20）、AWS managed policy の形（`AWSBudgetsReadOnlyAccess` の `budgets:Describe*`、`AWSBudgetsActionsWithAWSResourceControlAccess` の `budgets:*`。[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定8・10）
- apply の role の Identity Center の Deny について参照した一次資料: Service Authorization Reference の「Actions, resources, and condition keys for AWS IAM Identity Center」の Action の表（126 Action と Access level）。採用した知見: `sso:*AccountAssignment`・`sso:*FromPermissionSet`・`sso:*ToPermissionSet` は permission set と割当の書き込みにだけ当たり、Read・List の Action に当たらない。`sso:PutPermissionsPolicy` は Permissions management の書き込みである。採用しなかった知見: `sso:*PermissionSet` の形（Read・List の7つの Action に当たる。[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定5）
- apply-identity の inline policy の文を customer managed policy へ移すことについて参照した一次資料: IAM User Guide の「IAM and AWS STS quotas」（role の inline policy の合計 10,240 文字、customer managed policy は1つ 6,144 文字、どちらも空白を数えない。role あたりの managed policy は既定 20・上限 25）、「Choose between managed policies and inline policies」（多くの場合 inline より managed を勧める。版を持ち、前の版へ戻せる）、Security Hub の IAM.1・IAM.21（customer managed policy の Allow の `"Action": "*"`・`service:*` を見る）。採用した知見: 上限の値と、managed policy を使う推奨（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定21）。`-manage` は Action を列挙し、IAM.1・IAM.21 に当たらない。採用しなかった知見: inline policy の「1対1の関係を保ち、別の主体へ誤って付かない」利点を、apply-identity の role の権限のすべてに使うこと。付与は条件 `iam:PolicyARN` で自分の role に限り、apply の role は `policy/<project>-<env>-ci-*` を Deny で拒む（同 決定5・7）
- 補助として参照した資料（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定3 の外）: GitHub の「OpenID Connect reference」と「Deployments and environments」

採用した知見:

- audience を `sts.amazonaws.com` の1つにし、信頼条件の `aud` と一致させる
- `sub` を完全一致で限定し、`repository_id` と `repository_owner_id` を条件キーに加える（IAM User Guide は名前ではなく変わらない識別子を使うことを勧める）
- `thumbprint_list` を設定しない。GitHub については IAM が自前の CA ライブラリで検証する。一度設定して後から外すと、Terraform は元の値を使い続ける
- environment の保護規則で、role を使える job を限る
- `aws_iam_role` の `inline_policy` と `managed_policy_arns`（deprecated）を使わず、個別の resource と排他の resource を使う
- `kms:ListAliases` は resource-level 権限を持たないので Resource `"*"`。alias の作成・付け替え・削除は alias と鍵の両方に権限を要るので、鍵の ARN への Deny がそれらも塞ぐ
- lockout の安全チェックの迂回を `kms:BypassPolicyLockoutSafetyCheck` の Deny で拒む（AWS の例と同じ形）

採用しなかった知見と理由:

- AWS の例の、`kms:CreateKey` を Condition 付きで IAM で与える形: state の鍵は段4 で作られた後に CI が作り直す経路が無く、使わない Resource `"*"` の Allow を残さない（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定3）
- 既定の Key Policy の `kms:*` のアカウントへの委任と、IAM で暗号操作を与える形: 鍵を使う主体を Key Policy の1箇所に揃える（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定12）
- 鍵の ARN を `aws:ResourceAccount` で限る Condition: ARN の account の欄と同じで、何も狭めない
- AWS の例の `StringLike` と `repo:<owner>/<repo>:*`: リポジトリの全 job が assume できる（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 案A）
- `environment` / `ref` / `job_workflow_ref` / `actor_id` の条件キー: `environment` は `sub` と同じ値、ref は environment の deployment branch policy が限る（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14）、`actor_id` は人が増えると信頼条件の変更が要る
- AWS managed policy（`ReadOnlyAccess` 等）: [ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定10
- 1時間を超えるセッション: 漏洩したときの窓が広がる。apply が1時間を超える構成が入ったときに見直す
- session tag と source identity: GitHub の token は AWS の session tag の claim を持たない見込みで、確かめていない

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

記録は use-case ADR が持つ。一覧は [`docs/adr/README.md`](docs/adr/README.md)。

- 管理アカウントに、IAM を変更できる CI の role を置く: [deployment-identity/ADR-0001](docs/adr/0001-ci-roles-in-management-account.md)
- Budgets の Action に、Service Authorization Reference が並べる `aws-portal:ViewBilling`・`ModifyBilling` を与えない: [deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定20

## 版

- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.17.1（`mise.toml`）だけである
- AWS Provider は `>= 6.68.0, < 7.0.0`。6.68.0 に固定した複製で `terraform validate` と `terraform test` が通ることを確かめた。reference と identity root の lock は 6.70.0 を固定する。権限の列挙は 6.70.0 の source から（workload root の usecase の分は 6.73.0 の source から）導いたもので、6.68.0 が同じ API を呼ぶことは確かめていない（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定2）
