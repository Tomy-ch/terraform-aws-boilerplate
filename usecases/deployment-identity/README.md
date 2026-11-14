# deployment-identity

GitHub Actions が長期のアクセスキーを持たずに AWS を操作するための、OIDC provider と CI の role を1つのアカウントに持つユースケース。bootstrap の構成物であり（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）、各 env の identity root から呼ばれる（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定3・5）。

## 解決する問題

CI は OIDC で role を assume して AWS を操作する（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定12）。role は plan / apply / apply-identity と、lock の解除の unlock-identity の4つに分かれ（同 決定13）、信頼条件は当該リポジトリの environment に限られる（同 決定14）。このユースケースは、その provider と4つの role、それぞれの権限を Terraform の Desired State に置く。

## 実行形態とネットワーク境界

- GitHub Actions のランナーが、AWS STS の公開 endpoint で `AssumeRoleWithWebIdentity` を呼ぶ。AWS の上で動く処理は無い
- 外への egress は無い

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、37文字以内） | role 名の先頭に置くプロジェクトの token。最も長い role 名（`<project>-sandbox-ci-unlock-identity`）を IAM の上限の64文字に収める。`state-backend` の Bucket 名の上限が、実効的にはさらに短く縛る |
| `env` | `dev` / `stg` / `prd` / `sandbox` / `admin` | role を置くアカウントの env の token。GitHub の environment 名の前半にもなる |
| `github` | `object({ owner, owner_id, repository, repository_id })`（すべて `string`） | role を assume してよいリポジトリの所有者とリポジトリの名前、変わらない数値の ID |
| `state_bucket_arn` | `string`（S3 Bucket の ARN） | このアカウントの state Bucket（`state-backend` の `bucket_arn`） |
| `state_key_arn` | `string`（KMS の鍵の ARN。alias の ARN と多リージョンの鍵は受けない） | このアカウントの state の鍵（`state-backend` の `state_key_arn`）。plan と apply-identity の role の鍵の権限と、apply の role の鍵の Deny をこの鍵に絞る。role と同じ partition・リージョン・アカウントの鍵でなければ、plan で（ARN が plan で決まらなければ apply で）落ちる。Key Policy の置き換えを待つ値を渡すと、identity root で依存が循環する（Key Policy が名指す role の作成が、この ARN を持つ apply-identity の inline policy の後に来るため） |
| `plan_output_bucket_arn` | `string`（S3 Bucket の ARN） | このアカウントの plan の出力の Bucket（`state-backend` の `plan_output_bucket_arn`）。plan の role はその `pr/*` と `branch/*` の key に、apply-identity の role は `apply/*` の key にだけ書ける |
| `access_log_group_arn` | `string`（Log Group の ARN。`:*` を付けない。名前は英数字と `-` `_` の60文字以内） | 2つの Bucket の S3 サーバーアクセスログを受ける Log Group（`state-backend` の `access_log_group_arn`）。apply-identity の Log Group の権限と、apply の role の Deny をこの Log Group に絞る |
| `access_log_destination_arn` | `string`（delivery destination の ARN） | アクセスログの delivery destination（`state-backend` の `access_log_destination_arn`） |
| `access_log_source_arns` | `list(string)`（delivery source の ARN。1件以上、重ねない） | 2つの Bucket の delivery source（`state-backend` の `access_log_source_arns`） |

`state_key_arn` は既定値を持たない必須入力で、`state_key_alias_name` の入力は無い。`access_log_` で始まる3つも既定値を持たない必須入力である。どれも利用者の入力の変更を要する破壊的変更である（[ADR-0202](../../docs/adr/0202-internal-topology-as-implementation-detail.md) 決定4）。`access_log_` の3つの ARN は、role と同じ partition・リージョン・アカウントの CloudWatch Logs の ARN でなければ plan で落ちる。

| 出力 | 使う側 |
| --- | --- |
| `plan_role_arn` | 人が確かめるときの参照。CI の plan の job は assume の前に state を読めないので、この output を読まず、`scripts/tf-local` の `role-arn` が同じ名前の規則で ARN を組む（規則の一致は `tf-local` のテストが見る）。`state-backend` の `ci_role_arns.plan`（state の鍵の Key Policy） |
| `apply_role_arn` | workload root の apply の job（未配線。#222）。`state-backend` の `ci_role_arns.apply`（state Bucket の Bucket Policy の、workload root の state と lock file の書き込み。[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定9） |
| `apply_identity_role_arn` | `terraform-apply.yaml` の `apply-admin-identity`。`state-backend` の `ci_role_arns.apply_identity`（state の鍵の Key Policy と、state Bucket の Bucket Policy の identity root の state と lock file の書き込み） |
| `unlock_identity_role_arn` | `terraform-unlock.yaml` の `unlock-admin-identity`（`scripts/tf-local` の `role-arn` が同じ名前の規則で ARN を組む）。`state-backend` の `ci_role_arns.unlock_identity`（state の鍵の Key Policy の、lock file に限った復号と、state Bucket の Bucket Policy の lock file の削除） |

role と environment の名前は入力から決まる。利用者は選ばない（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定3）。

| 役割 | role 名 | GitHub の environment | 使う場面 |
| --- | --- | --- | --- |
| plan | `<project>-<env>-ci-plan` | `<env>-plan` | identity root と workload root の plan（lock を取らない） |
| apply | `<project>-<env>-ci-apply` | `<env>-apply` | workload root の apply |
| apply-identity | `<project>-<env>-ci-apply-identity` | `<env>-apply-identity` | identity root の apply |
| unlock-identity | `<project>-<env>-ci-unlock-identity` | `<env>-unlock-identity` | identity root の state に残った lock の解除（`terraform-unlock.yaml`） |

- ログの所在: 無い。`AssumeRoleWithWebIdentity` の記録は CloudTrail が持ち、その配置は `account-baseline` と `audit-evidence` が持つ
- 基盤系アラームの通知先 Topic ARN: 受けない。フェーズ0 の `deployment-identity` は対象外である（[scope.md 第7節](../../docs/v1.0-plan/scope.md)）

## 持つもの

- OIDC provider（`https://token.actions.githubusercontent.com`、audience は `sts.amazonaws.com` だけ）
- 4つの role。信頼条件は [deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md)、権限は [deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) が決める
  - plan: state Bucket の両方の key の読み取りと、identity root の resource の読み取り。state の鍵の設定の読み取りと、alias の一覧。段5 のログの構成物の設定の読み取り。ログの event は読めない
  - apply: workload root の key と lock file の読み書き。4つの role と OIDC provider、identity root の key と lock file、plan の出力の Bucket とその object は読み取りを含めて Deny する。state Bucket は `s3:ListBucket` のほかを、state Bucket の object は版の削除を、state の鍵は管理（読み取りを含む）を Deny する。Log Group・delivery destination・delivery source への `logs:*`、2つの Bucket の配送の許可、resource type を持たない Logs のアカウント単位の policy・index policy・resource policy の変更も Deny する。delivery source の Deny は、`access_log_source_arns` で渡したものに限られる
  - apply-identity: identity root の key と lock file の読み書きと、identity root の resource の管理。自分を含む4つの role の信頼条件と権限を変更できる。state の鍵の Key Policy と rotation を設定できる。鍵と alias の作成、鍵の削除・無効化、alias の削除・付け替えはできない。段5 のログの構成物（Log Group、resource policy、delivery destination、delivery source、delivery）を作れる。ログの構成物の削除と、保持の解除・削除の保護の解除・鍵の付け替えはできない。保持期間を短くすることはできる。plan の出力の Bucket の `apply/*` へ apply の出力を書ける。その Bucket の object の削除の権限は IAM で与えない
  - unlock-identity: identity root の lock file の一覧（`s3:ListBucket` を `s3:prefix` で限る）・読み取り・削除だけ。`s3:PutObject` と `s3:DeleteObjectVersion` を持たず、偽の lock を置けず、版も消せない。lock file の復号は、state の鍵の Key Policy がその object に限って許す
  - 暗号操作（`kms:Decrypt` など）は IAM では与えない。state の鍵の Key Policy が CI の role を名指して許す
- role ごとの inline policy と、inline policy と managed policy の排他（帯域外で足された policy を次の apply で剥がす）
- role の ARN を、role の resource ではなく partition・アカウント・role 名から組む。apply-identity の role と inline policy は他の role と別の resource で、他の role はその inline policy の書き込みの後に作る。role を足す変更は、1回の apply で、その role を管理する権限を apply-identity に足してから role を作る（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定4）

これらは usecase に固有の module `internal/github-oidc/` が持つ（[ADR-0102](../../docs/adr/0102-use-case-centric-scope.md) 決定15）。信頼条件が GitHub の claim に、権限が bootstrap の構成物と state の key に固有で、どの usecase でも同じ形で成立する機構ではないためである。

## supported / unsupported

supported:

- github.com のリポジトリで、OIDC の `sub` が immutable 形式（`use_immutable_subject: true`）であるもの
- 1つのアカウントに、このユースケースの OIDC provider を1つ置く構成

unsupported:

- 旧形式の `sub`（`repo:<owner>/<repo>:...`）のリポジトリと、GitHub Enterprise Server（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定5）。旧形式のリポジトリは、opt-in で immutable 形式にしてから使う
- Enterprise Managed User の個人アカウントが所有するリポジトリ。login が `_` を含み、`github.owner` の検査が受け付けない。その login が `sub` にどう現れるかを確かめていない
- fork からの Pull Request で plan を成立させること。plan の job は fork の Pull Request で assume の前に失敗する（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14）
- 締め出しから回復する資格情報の発行と管理、break-glass（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定9、[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19）
- 管理アカウントの root による、role と provider の変更を防ぐこと（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定11）
- 同じアカウントに、このユースケースの外で GitHub Actions の OIDC provider を作ること。provider は URL ごとに1つしか置けない

保証しないこと:

- 同じリポジトリへ Pull Request を開ける主体が、workflow を書き換えて plan の role を assume し、state を読むこと（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) の保証範囲）
- apply-identity の role が、自分と他の CI の role の権限を広げる変更を apply すること。release line への merge がそのまま apply になるので、止めるのは Pull Request のレビューである。このユースケースの `.tf` と identity root（`env/<env>/identity/`）は `.github/CODEOWNERS` の対象ではなく（対象は `reference/tests/policy-input.tftest.hcl` だけ）、レビューを要求するかは ruleset の承認者数に依る。apply-identity の role の権限の拡大を見る Policy Test も無い（plan の role は `plan.deployment_identity_plan_role` が読み取りに限り、unlock-identity の role は `plan.deployment_identity_unlock_role` が lock file の一覧・読み取り・削除に限る。apply の role は `plan.deployment_identity_apply_role` が、bootstrap の構成物の ARN への Deny の形と、ほかの policy が付かないことを見る。Allow の拡大は見ないが、Allow は Deny に勝たない）
- apply の role が作るか渡す別の主体、bootstrap の構成物の ARN を通らない要求（S3 の access point 等）、Resource `"*"` でしか与えられない Action を経た、bootstrap の構成物の変更（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) の保証範囲。別の主体は #254）
- 帯域外で足された policy が、次の apply までの間に有効であること。検出は定期 plan（#77）、剥がすのは apply
- write 権限を持つ主体が、保護対象のパターンに当たるブランチを新しく作り、その上で書き換えた workflow から apply 系の environment の job を走らせること。apply の job のデフォルトブランチの条件はその workflow の中に在り、ruleset は `creation` の規則を持たない。`pull_request` と required status checks の規則が push によるブランチの作成を止めるかを、GitHub の文書は述べていない（#223）

## 所有者

- OIDC provider と4つの role は、このユースケースが所有する。sandbox の掃除用 Role（#76）が provider を使うときは、そのときに provider の ARN を出力に足す
- state Bucket と state の鍵、段5 のログの構成物は `state-backend` が所有する。このユースケースは Bucket と鍵の ARN と、ログの構成物の ARN を受け、role の権限の Resource と Condition に使う

## 依存するユースケースと判断 Issue

- ユースケース: `state-backend`（`bucket_arn`、`state_key_arn`、`plan_output_bucket_arn`、`access_log_group_arn`、`access_log_destination_arn`、`access_log_source_arns`）。`state-backend` は、このユースケースの `plan_role_arn`・`apply_role_arn`・`apply_identity_role_arn`・`unlock_identity_role_arn` を `ci_role_arns` に受ける
- 判断 Issue: #130、#98、#142、#96、#83。いずれも閉じている
- GitHub 側の environment は [`.github/settings/environments.json`](../../.github/settings/environments.json) が宣言する。名前はこのユースケースの `<env>-plan` / `<env>-apply` / `<env>-apply-identity` / `<env>-unlock-identity` の写しで、`env/` の root が渡す `env` ごとに4つが宣言に揃っていることを `make repo-settings-check` が見る。plan の role は `terraform-gates.yaml` の `plan-admin-identity` が、apply-identity の role は `terraform-apply.yaml` の `apply-admin-identity` が、unlock-identity の role は `terraform-unlock.yaml` の `unlock-admin-identity` が assume する。apply の role を assume する workflow はまだ無い（#222）

段5 で apply-identity の権限を足すときは、権限の変更を先に merge してから、その権限で作る resource を apply する（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) の影響）。

## 検証方法

- Unit Test（mock）: `internal/github-oidc/tests/` が、4つの role の信頼条件と policy の文書、名前、セッションの上限、OIDC provider の URL と audience、inline policy と managed policy の排他を確かめる。`tests/validation.tftest.hcl` が入力の検査を、`tests/contract.tftest.hcl` が公開 output を、`reference/tests/plan.tftest.hcl` が reference の値を確かめる
- Policy Test（`make tf-policy`）: `reference/tests/policy-input.tftest.hcl` の plan に対して、CI の role の address の全 instance の信頼条件の形（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14・15）と、plan の role の Allow が読み取りと plan の出力の `pr/*`・`branch/*` への `s3:PutObject` だけであること（同 決定13）と、Allow の Action の wildcard（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定8）と `NotAction`・`NotResource` の不在と、Allow の Resource `"*"` が `resource_wildcard_actions` の宣言（`kms:ListAliases`・`logs:DescribeLogGroups`・`logs:DescribeResourcePolicies`・`logs:PutResourcePolicy`）に限られること（同 決定9）と、KMS の暗号操作の Allow の不在（同 決定12）と、plan の role の `logs:` の Action が宣言した一覧に限られ、宣言が規則の持つ閉じた集合の部分集合で plan の role の Allow と一致すること（`PLAN-ROLE-05`〜`09`）と、apply の role が resource type を持たない Logs の変更系を Resource `"*"` で拒むこと（`PLAN-APPLY-02`）を確かめる。`state-backend` の reference の plan と結合した入力で、apply の role の inline policy が apply の role に付き、bootstrap の構成物の S3・IAM の ARN ごとに読み取り以外を条件なしの Deny で拒み、鍵ごとに鍵の ARN への Deny が同じ module の Key Policy の委任の Action のすべてを覆い、ほかの policy が apply の role に付かないことと（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定5）、unlock-identity の role の Allow が state の Bucket の identity root の lock file の一覧・読み取り・削除に限られ、Key Policy がその復号を IAM と同じ lock file の object に限ることと（同 決定9）、apply-identity の role の plan の出力の Bucket の object への書き込みが `apply/*` への `s3:PutObject` だけで、ほかの policy が apply-identity の role に付かないことも確かめる（同 決定10）。Policy の一覧は [`conftest/plan/README.md`](../../conftest/plan/README.md)
- Static Analysis（`make resource-order-check`）: `internal/github-oidc` の `aws_iam_role.this` が `depends_on` に apply-identity の inline policy を持つことを、HCL のソースで確かめる（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定4）。宣言は [`conftest/hcl/resource_order/`](../../conftest/hcl/resource_order/README.md)
- 段2 の突合と段4 の確認: 人が実 AWS で行う（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md)）
- 実環境テストの形: [ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の「破棄すると認証や組織の構造を失う」に当たる。destroy しない。Policy Test、拒否されるべき主体の assume や操作が拒否されることの確認、drift 0 で代える（[scope.md 第11.1節](../../docs/v1.0-plan/scope.md)）

workload root の apply の経路（#222）と sandbox のアカウント（#21）は無い。いずれも配線された後に遡って検証する。その間に検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- immutable 形式の environment の `sub` の形が、environment `<env>-apply` の実 token と違うこと。plan と apply-identity の role は CI の plan と apply の assume が確かめる（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定7）
- deployment branch policy の扱いの想定違いで、plan の environment が Pull Request を許さないこと、apply 系の environment が Pull Request を許すこと
- 権限の列挙が Provider の実際の呼び出しを覆わず、CI の plan や apply が AccessDenied で失敗すること。apply-identity の読み取りが欠けると、その修正の apply も失敗し、回復の契機になる
- 実 API が読み戻した信頼条件や policy が宣言と違い、plan が毎回差分を出すこと
- fork からの Pull Request で OIDC token が得られるかどうか
- 拒否されるべき主体（他のリポジトリ、別の environment、Pull Request の job からの apply 系の role）の assume が拒否されること
- 権限の Resource に書いた state の key（`identity/terraform.tfstate`・`workload/terraform.tfstate`）が、identity root の `backend.tf` の `key` と一致すること。どちらも [ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定7 の固定値を写しており、突き合わせる検査は無い。workload root はまだ無く、それを作る変更が同じ値を写す。片方だけを変えると、CI の role が state を読み書きできなくなる
- identity root が `deployment-identity` に渡す値（`github` の ID、`state_bucket_arn`・`state_key_arn`・`plan_output_bucket_arn`・`access_log_` で始まる3つの配線）が正しいこと。identity root のテストは role の ARN と `state-backend` の出力しか観測できず、`deployment-identity` への配線を見るのは CI の plan だけである。鍵とログの構成物の ARN は precondition が partition・リージョン・アカウントまでを見るが、同じアカウントの別の resource との取り違えは見ない
- apply の role の Bucket・object・IAM の側の Deny が、実際の要求で文書どおりに評価されること。Policy Test は reference の plan で Deny の形を見るだけで、`simulate-principal-policy` は IAM の評価だけを見る。apply の role を assume する job は無い（#222）。apply の role が作るか渡す別の主体、構成物の ARN を通らない要求、Resource `"*"` でしか与えられない Action を経た変更も、どの検査も見ない
- apply の role の鍵の Deny が、実際の要求で文書どおりに評価されること。`simulate-principal-policy` は IAM の評価だけを見る。apply の role を assume する job は無い（#222）
- 同じ apply で role を足すとき、apply-identity の inline policy の書き込みが、role の作成（`iam:CreateRole`）までに IAM の評価へ反映されること。反映が遅れるとその apply は AccessDenied で失敗し、run の「Re-run failed jobs」で再実行する
- unlock-identity の role の lock file の削除と、暗号化コンテキストで限った復号が、実 API で通ること。lock file が在るときしか評価されないので、人が `docs/setup/bootstrap.md` 段5 の lock の項で一度試す
- apply の role の state Bucket の Deny が `s3:ListBucket` のほかを塞いでも、workload root の apply の経路が state Bucket のほかの Action を呼ばないこと。CI の init の前の `get-bucket-encryption`（`s3:GetEncryptionConfiguration`）は塞がれるので、#222 で apply の role を使う経路を配線するときに、その Action を決定5 の除外へ足すかを決める
- apply の role の鍵の Deny が読み取りも塞いでも、workload root の S3 backend が鍵の読み取り（`kms:DescribeKey`）を呼ばないこと。S3 の SSE-KMS の暗号操作は Deny に含まないが、backend の source では確かめていない（段5 の切り替えで見る）
- 段5 のログの権限が、実際の要求で認可されること。CloudWatch Logs が resource type を持たない Action を `"*"` で、Log Group の Action を `:*` の有無の形で、`PutDeliveryDestination` を `logs:DeliveryDestinationResourceArn` の値で評価することは、`simulate-principal-policy` では見えない。ログの構成物を作る CI の apply が初めて確かめる
- ログの構成物の作成が途中で落ちて taint が残ったときに、CI だけで回復すること。CI の role は削除の権限を持たない（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) の保証しないこと）

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-10-25（KMS の権限の分は 2026-11-07、alias の操作の権限は 2026-11-12、CloudWatch Logs と vended logs の権限は 2026-11-14）

- AWS / HashiCorp が publisher の GitHub Actions の OIDC の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `github-oidc` / `oidc` / `github` / `github-actions` / `iam-role`、`provider=aws` で引いた結果に、namespace `aws`・`aws-ia`・`hashicorp`・`aws-samples` が無い）
- 参照した一次資料: IAM User Guide の「Create a role for OpenID Connect federation」の GitHub の節、「Available keys for OIDC federation」の GitHub の表、「Create an OpenID Connect (OIDC) identity provider in IAM」、AWS Provider 6.70.0 の `aws_iam_openid_connect_provider` / `aws_iam_role` / `aws_iam_role_policy` / `aws_iam_role_policies_exclusive` / `aws_iam_role_policy_attachments_exclusive` の文書・schema・source
- KMS の権限について参照した一次資料: AWS KMS Developer Guide の「AWS KMS permissions」（Action ごとの resource と条件キー）、「AWS KMS condition keys」（`kms:BypassPolicyLockoutSafetyCheck`）、「Controlling access to aliases」、「Least-privilege permissions」、「Default key policy」、AWS Provider 6.70.0 の `internal/service/kms` の `key.go`・`alias.go`・`key_policy.go` の source
- CloudWatch Logs と vended logs の権限について参照した一次資料: Service Authorization Reference の CloudWatch Logs と Amazon S3 の頁（Action ごとの resource type と条件キー）、CloudWatch Logs API Reference の LogGroup 型（`arn` の `:*` の有無）・`PutDeliverySource`・`PutResourcePolicy`、CloudWatch Logs User Guide の「Logs sent to CloudWatch Logs」と「Encrypt log data using AWS KMS」、Amazon S3 User Guide の「Delivering server access logs to CloudWatch Logs」、AWS Provider 6.70.0 の `internal/service/logs` の `group.go`・`resource_policy.go`・`delivery_destination.go`・`delivery_source.go`・`delivery.go` の source
- 補助として参照した資料（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定3 の外）: GitHub の「OpenID Connect reference」と「Deployments and environments」

採用した知見:

- audience を `sts.amazonaws.com` の1つにし、信頼条件の `aud` と一致させる
- `sub` を完全一致で限定し、`repository_id` と `repository_owner_id` を条件キーに加える（IAM User Guide は名前ではなく変わらない識別子を使うことを勧める）
- `thumbprint_list` を設定しない。GitHub については IAM が自前の CA ライブラリで検証する。一度設定して後から外すと、Terraform は元の値を使い続ける
- environment の保護規則で、role を使える job を限る
- `aws_iam_role` の `inline_policy` と `managed_policy_arns`（deprecated）を使わず、個別の resource と排他の resource を使う
- `kms:ListAliases` は resource-level 権限を持たないので Resource `"*"`。alias の作成・付け替え・削除は alias と鍵の両方に権限を要るので、鍵の ARN への Deny がそれらも塞ぐ
- lockout の安全チェックの迂回を `kms:BypassPolicyLockoutSafetyCheck` の Deny で拒む（AWS の例と同じ形）
- vended logs の配送を設定する主体に、送り側の Bucket への `s3:AllowVendedLogDeliveryForResource` を与える（Amazon S3 User Guide）
- Log Group の `ListTagsForResource` には `:*` の無い ARN を、ほかの Action には `:*` を付けた ARN を使う（API Reference の LogGroup）

採用しなかった知見と理由:

- AWS の例の、`kms:CreateKey` を Condition 付きで IAM で与える形: state の鍵は段4 で作られた後に CI が作り直す経路が無く、使わない Resource `"*"` の Allow を残さない（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定3）
- CloudWatch Logs User Guide の、`PutResourcePolicy`・`DescribeResourcePolicies`・`DescribeLogGroups` を `arn:aws:logs:<region>:<account>:*` に与える例: Service Authorization Reference はこの3つに resource type を挙げないので、Resource `"*"` の判定元に従う（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定9）
- ログの構成物の削除の権限: Bucket と鍵と同じく、作り直しを権限の側でも止める
- CloudWatch Logs User Guide の、鍵を付けた `CreateLogGroup` の主体に鍵の `kms:DescribeKey` を与える形: Log Group の暗号化の構成は `state-backend` が決め、Log Group に鍵を付ける権限は、鍵を付ける変更が先に足す（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定3）
- `s3:AllowVendedLogDeliveryForResource` の `s3:logType`・`s3:deliverySourceArn` の Condition: source は `PutDeliverySource` を名指した ARN に既に限られ、条件キーが実際の要求に載るかを確かめていない
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

## 版

- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.16.6（`mise.toml`）だけである
- AWS Provider は `>= 6.68.0, < 7.0.0`。6.68.0 に固定した複製で `terraform validate` と `terraform test` が通ることを確かめた。reference と identity root の lock は 6.70.0 を固定する。権限の列挙は 6.70.0 の source から導いたもので、6.68.0 が同じ API を呼ぶことは確かめていない（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) 決定2）
