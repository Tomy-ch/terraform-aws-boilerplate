# deployment-identity

GitHub Actions が長期のアクセスキーを持たずに AWS を操作するための、OIDC provider と CI の role を1つのアカウントに持つユースケース。bootstrap の構成物であり（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定1）、各 env の identity root から呼ばれる（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定3・5）。

## 解決する問題

CI は OIDC で role を assume して AWS を操作する（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定12）。role は plan / apply / apply-identity の3つに分かれ（同 決定13）、信頼条件は当該リポジトリの environment に限られる（同 決定14）。このユースケースは、その provider と3つの role、それぞれの権限を Terraform の Desired State に置く。

## 実行形態とネットワーク境界

- GitHub Actions のランナーが、AWS STS の公開 endpoint で `AssumeRoleWithWebIdentity` を呼ぶ。AWS の上で動く処理は無い
- 外への egress は無い

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、38文字以内） | role 名の先頭に置くプロジェクトの token。最も長い role 名（`<project>-sandbox-ci-apply-identity`）を IAM の上限の64文字に収める |
| `env` | `dev` / `stg` / `prd` / `sandbox` / `admin` | role を置くアカウントの env の token。GitHub の environment 名の前半にもなる |
| `github` | `object({ owner, owner_id, repository, repository_id })`（すべて `string`） | role を assume してよいリポジトリの所有者とリポジトリの名前、変わらない数値の ID |
| `state_bucket_arn` | `string`（S3 Bucket の ARN） | このアカウントの state Bucket（`state-backend` の `bucket_arn`） |
| `plan_output_bucket_arn` | `string`（S3 Bucket の ARN） | このアカウントの plan の出力の Bucket（`state-backend` の `plan_output_bucket_arn`）。plan の role はその `pr/*` と `branch/*` の key にだけ書ける |

| 出力 | 使う側 |
| --- | --- |
| `plan_role_arn` | GitHub Actions の plan の job の `role-to-assume`（#185） |
| `apply_role_arn` | 同 apply の job（#185）。段5 の state Bucket の Bucket Policy（[ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定9。#19） |
| `apply_identity_role_arn` | 同 apply-identity の job（#185）。段5 の Bucket Policy（#19） |

role と environment の名前は入力から決まる。利用者は選ばない（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定3）。

| 役割 | role 名 | GitHub の environment | 使う場面 |
| --- | --- | --- | --- |
| plan | `<project>-<env>-ci-plan` | `<env>-plan` | identity root と workload root の plan（lock を取らない） |
| apply | `<project>-<env>-ci-apply` | `<env>-apply` | workload root の apply |
| apply-identity | `<project>-<env>-ci-apply-identity` | `<env>-apply-identity` | identity root の apply |

- ログの所在: 無い。`AssumeRoleWithWebIdentity` の記録は CloudTrail が持ち、その配置は `account-baseline` と `audit-evidence` が持つ
- 基盤系アラームの通知先 Topic ARN: 受けない。フェーズ0 の `deployment-identity` は対象外である（[scope.md 第7節](../../docs/v1.0-plan/scope.md)）

## 持つもの

- OIDC provider（`https://token.actions.githubusercontent.com`、audience は `sts.amazonaws.com` だけ）
- 3つの role。信頼条件は [deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md)、権限は [deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) が決める
  - plan: state Bucket の両方の key の読み取りと、identity root の resource の読み取り
  - apply: workload root の key と lock file の読み書き。bootstrap の構成物、identity root の key と lock file、state Bucket の設定の変更は Deny する
  - apply-identity: identity root の key と lock file の読み書きと、identity root の resource の管理。自分を含む3つの role の信頼条件と権限を変更できる
- role ごとの inline policy と、inline policy と managed policy の排他（帯域外で足された policy を次の apply で剥がす）

これらは usecase に固有の module `internal/github-oidc/` が持つ（[ADR-0102](../../docs/adr/0102-use-case-centric-scope.md) 決定15）。信頼条件が GitHub の claim に、権限が bootstrap の構成物と state の key に固有で、どの usecase でも同じ形で成立する機構ではないためである。

## supported / unsupported

supported:

- github.com のリポジトリで、OIDC の `sub` が immutable 形式（`use_immutable_subject: true`）であるもの
- 1つのアカウントに、このユースケースの OIDC provider を1つ置く構成

unsupported:

- 旧形式の `sub`（`repo:<owner>/<repo>:...`）のリポジトリと、GitHub Enterprise Server（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定5）。旧形式のリポジトリは、opt-in で immutable 形式にしてから使う
- Enterprise Managed User の個人アカウントが所有するリポジトリ。login が `_` を含み、`github.owner` の検査が受け付けない。その login が `sub` にどう現れるかを確かめていない
- fork からの Pull Request で plan を成立させること。plan の job は fork の Pull Request で assume の前に失敗する（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14）
- 締め出しから回復する資格情報の発行と管理、break-glass（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定9、[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定18）
- 管理アカウントの root による、role と provider の変更を防ぐこと（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定11）
- 同じアカウントに、このユースケースの外で GitHub Actions の OIDC provider を作ること。provider は URL ごとに1つしか置けない

保証しないこと:

- 同じリポジトリへ Pull Request を開ける主体が、workflow を書き換えて plan の role を assume し、state を読むこと（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) の保証範囲）
- apply-identity の role が、自分と他の CI の role の権限を広げる変更を apply すること。止めるのは Pull Request のレビューである
- 帯域外で足された policy が、次の apply までの間に有効であること。検出は定期 plan（#77）、剥がすのは apply

## 所有者

- OIDC provider と3つの role は、このユースケースが所有する。sandbox の掃除用 Role（#76）が provider を使うときは、そのときに provider の ARN を出力に足す
- state Bucket は `state-backend` が所有する。このユースケースは ARN を受け、role の権限の Resource に使う

## 依存するユースケースと判断 Issue

- ユースケース: `state-backend`（`bucket_arn`）。段5 の `state-backend` は、このユースケースの role の ARN を受ける（#19）
- 判断 Issue: #130、#98、#142、#96、#83。いずれも閉じている
- GitHub 側の environment は [`.github/settings/environments.json`](../../.github/settings/environments.json) が宣言する。名前はこのユースケースの `<env>-plan` / `<env>-apply` / `<env>-apply-identity` の写しで、`env/` の root が渡す `env` ごとに3つが宣言に揃っていることを `make repo-settings-check` が見る。role を assume する workflow は #185 が作る。それまでは、どの role も assume されない

段5 で apply-identity の権限を足すときは、権限の変更を先に merge してから、その権限で作る resource を apply する（[deployment-identity/ADR-0201](docs/adr/0201-enumerated-permissions.md) の影響）。

## 検証方法

- Unit Test（mock）: `internal/github-oidc/tests/` が、3つの role の信頼条件と policy の文書、名前、セッションの上限、OIDC provider の URL と audience、inline policy と managed policy の排他を確かめる。`tests/validation.tftest.hcl` が入力の検査を、`tests/contract.tftest.hcl` が公開 output を、`reference/tests/plan.tftest.hcl` が reference の値を確かめる
- Policy Test（`make tf-policy`）: `reference/tests/policy-input.tftest.hcl` の plan に対して、CI の role の address の全 instance の信頼条件の形（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14・15）と、Allow の Action の wildcard（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定8）と `NotAction` の不在を確かめる。Policy の一覧は [`conftest/plan/README.md`](../../conftest/plan/README.md)
- 段2 の突合と段4 の確認: 人が実 AWS で行う（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md)）
- 実環境テストの形: [ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の「破棄すると認証や組織の構造を失う」に当たる。destroy しない。Policy Test、拒否されるべき主体の assume や操作が拒否されることの確認、drift 0 で代える（[scope.md 第11.1節](../../docs/v1.0-plan/scope.md)）

apply の role の有効な権限の Policy Test（#209）は未配線で、CI の経路（#185）と sandbox のアカウント（#21）も無い。いずれも配線された後に遡って検証する。その間に検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- immutable 形式の environment の `sub` の形が実 token と違い、どの role も assume できないこと（[deployment-identity/ADR-0101](docs/adr/0101-trust-by-immutable-environment-subject.md) 決定7）
- deployment branch policy の扱いの想定違いで、plan の environment が Pull Request を許さないこと、apply 系の environment が Pull Request を許すこと
- 権限の列挙が Provider の実際の呼び出しを覆わず、CI の plan や apply が AccessDenied で失敗すること。apply-identity の読み取りが欠けると、その修正の apply も失敗し、回復の契機になる
- 実 API が読み戻した信頼条件や policy が宣言と違い、plan が毎回差分を出すこと
- fork からの Pull Request で OIDC token が得られるかどうか
- 拒否されるべき主体（他のリポジトリ、別の environment、Pull Request の job からの apply 系の role）の assume が拒否されること
- 権限の Resource に書いた state の key（`identity/terraform.tfstate`・`workload/terraform.tfstate`）が、identity root の `backend.tf` の `key` と一致すること。どちらも [ADR-0604](../../docs/adr/0604-state-split-by-apply-role.md) 決定7 の固定値を写しており、突き合わせる検査は無い。workload root はまだ無く、それを作る変更が同じ値を写す。片方だけを変えると、CI の role が state を読み書きできなくなる
- identity root が `deployment-identity` に渡す値（`github` の ID、`state_bucket_arn` と `plan_output_bucket_arn` の配線）が正しいこと。identity root のテストは role の ARN しか観測できず、見るのは段2 の plan だけである
- apply の role の有効な権限が bootstrap の構成物を変更できないこと（#209）。Unit Test は今の入力での文書を固定するだけである

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-10-25

- AWS / HashiCorp が publisher の GitHub Actions の OIDC の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `github-oidc` / `oidc` / `github` / `github-actions` / `iam-role`、`provider=aws` で引いた結果に、namespace `aws`・`aws-ia`・`hashicorp`・`aws-samples` が無い）
- 参照した一次資料: IAM User Guide の「Create a role for OpenID Connect federation」の GitHub の節、「Available keys for OIDC federation」の GitHub の表、「Create an OpenID Connect (OIDC) identity provider in IAM」、AWS Provider 6.70.0 の `aws_iam_openid_connect_provider` / `aws_iam_role` / `aws_iam_role_policy` / `aws_iam_role_policies_exclusive` / `aws_iam_role_policy_attachments_exclusive` の文書・schema・source
- 補助として参照した資料（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定3 の外）: GitHub の「OpenID Connect reference」と「Deployments and environments」

採用した知見:

- audience を `sts.amazonaws.com` の1つにし、信頼条件の `aud` と一致させる
- `sub` を完全一致で限定し、`repository_id` と `repository_owner_id` を条件キーに加える（IAM User Guide は名前ではなく変わらない識別子を使うことを勧める）
- `thumbprint_list` を設定しない。GitHub については IAM が自前の CA ライブラリで検証する。一度設定して後から外すと、Terraform は元の値を使い続ける
- environment の保護規則で、role を使える job を限る
- `aws_iam_role` の `inline_policy` と `managed_policy_arns`（deprecated）を使わず、個別の resource と排他の resource を使う

採用しなかった知見と理由:

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
