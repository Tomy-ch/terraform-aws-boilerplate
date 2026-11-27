# メンバーアカウントの立ち上げ 手順書

## 1. 設定の概要

メンバーアカウント（security・log-archive）に、その identity root の前提（`state-backend` と `deployment-identity`）を置く。resource は管理アカウントの identity root（`env/admin/identity`）が、立ち上げの中のアカウントへの provider で作り、admin への昇格で CI の apply-identity の role が apply する（[organization-baseline/ADR-0203](../../usecases/organization-baseline/docs/adr/0203-member-bootstrap-read-role.md)、[deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定22）。

- メンバーの provider は、apply-identity の role の session では `OrganizationAccountAccessRole` を、それ以外の session では立ち上げの読み取りの role（`/bootstrap/<project>-bootstrap-read`）を引き受ける
- 立ち上げの中のアカウントは、`env/admin/identity/main.tf` の `bootstrapping` にあるものだけである。外したアカウントでは、SCP（`deny-bootstrap-role`）と管理アカウントの IAM の Allow の両方から引き受けが消える
- デプロイブランチ `<env>` と environment は、`env/<env>/` の root とその apply の job が在るまで、CI の role の信頼条件の宛先であるだけで、merge しても何も apply しない
- 立ち上げの module が `env/admin/identity` に在る間は、新しいリポジトリの bootstrap の段2-4 を行えない（[`bootstrap.md`](bootstrap.md) の 2-5）

| 実施者 | 担当 |
| --- | --- |
| リポジトリの管理者 | environment・デプロイブランチ・保護設定の適用、昇格の Pull Request |
| 管理アカウントの管理者の資格情報（`organization_admin` の permission set か、それに代わるもの）を持つ人 | environment の secret `AWS_ACCOUNT_ID` の設定 |

[`organization-baseline.md`](organization-baseline.md) の 3.8 でアカウントを作り、3.5 の break-glass の role の確認を終えた後に行う。

## 2. 設定完了後の状態

- 各メンバーアカウントに、state Bucket・state の鍵・plan と apply の出力の Bucket・GitHub の OIDC provider・CI の5つの role が在る
- environment `<env>-plan`・`<env>-apply`・`<env>-apply-identity`・`<env>-unlock-identity`・`<env>-unlock` が在り、それぞれに secret `AWS_ACCOUNT_ID`（そのメンバーアカウントの ID）が在る
- デプロイブランチ `<env>` が在り、ruleset が保護する

## 3. 手順

### 3.1 立ち上げの Pull Request の merge の前

`env/admin/identity` にメンバーの `state-backend`・`deployment-identity` を足し、`.github/settings/` に environment とデプロイブランチを足す Pull Request は、`conftest/plan/decl.json` の `state_backend.instances` の `admin-identity` に、そのアカウントの2つの module の接頭辞と `creating`（理由）も足す。作成の plan では鍵の ARN が決まらず、`plan.state_backend_write_deny` が state Bucket の Bucket Policy と CI の role の IAM を読めないので、`creating` の間はその判定だけを止める（[`conftest/plan/README.md`](../../conftest/plan/README.md)）。

その Pull Request の merge の前に、リポジトリの管理者が次を行う。`<env>` は `security` と `log-archive` のそれぞれである。

1. `make apply-environments` で、宣言した environment を作る
2. `make create-deploy-branch DEPLOY_ENV=<env>` で、デプロイブランチを作る
3. `make apply-branch-protection` で、デプロイブランチを ruleset の保護に入れる
4. 管理アカウントの資格情報で、各 environment に secret `AWS_ACCOUNT_ID` を置く。値は組織の `<project>-<env>` のアカウントの ID で、組織の一覧から読んで gh の標準入力へ渡すので、画面にも引数にも出ない。既に在る secret は書かず、置き直すときだけ `REPLACE=1` を付ける

   ```bash
   make setup-organization-baseline-member-env-secrets PROFILE=<管理アカウントの profile> ENVIRONMENTS=<env>-plan,<env>-apply,<env>-apply-identity,<env>-unlock-identity,<env>-unlock REPO=<owner>/<リポジトリ>
   ```

5. `make repo-settings-drift` で、宣言と実態にずれが無いことを確かめ、その出力を Pull Request に書く

### 3.2 admin への昇格の後

1. 昇格の apply の job が成功したことを確かめる
2. `make setup-organization-baseline-stack-instances-check PROFILE=<管理アカウントの profile>` で、立ち上げの読み取りの StackSet の stack instance が、立ち上げの中のアカウントで `CURRENT` であることを確かめる
3. すぐに、`state_backend.instances` からそのアカウントの `creating` を外す Pull Request を作る。作成を終えた鍵に `creating` が残ると、すべての Pull Request の `plan-admin-identity` が `PLAN-SBW-11` で落ちる。その Pull Request の CI の `plan-admin-identity` が、変更を示さず、`plan.state_backend_write_deny` をすべて通ることを確かめ、Pull Request に書く

### 3.3 立ち上げを閉じる

メンバーアカウントの identity root（`env/<env>/identity`）が、そのアカウントの `state-backend`・`deployment-identity` を `import` block で取り込み、その root の CI の plan が変更なしを示した後に行う（[ADR-0604](../adr/0604-state-split-by-apply-role.md) 決定6）。

1. 1つの Pull Request で、`env/admin/identity` の次を同時に変える。`bootstrapping` から外すだけでは、provider と module が外したアカウントの ID を読めず、identity root の plan が落ちる
   - `bootstrapping` からそのアカウントの env を外す
   - そのアカウントの `module.state_backend_<env>`・`module.deployment_identity_<env>` を、`removed` block（`destroy = false`）に替える
   - そのアカウントの provider を消す
   - `conftest/plan/decl.json` の `state_backend.instances` の `admin-identity` から、そのアカウントの要素を消す
   - `conftest/plan/decl.json` の `stateful_protection.allow` の `admin-identity`（形は [`conftest/plan/README.md`](../../conftest/plan/README.md) の `stateful_protection` の行）に、`removed` にした保護の型の resource の address と理由を足す。管理からの除外は、許可の宣言が無いと Policy Test（`plan.stateful_protection`）が落とす。閉じ終えた後の変更で、その項目を取り除く
   - [`scripts/check-plan-bootstrapping.expected`](scripts/check-plan-bootstrapping.expected) から、そのアカウントの行を消す。立ち上げの中のアカウントが無くなれば、ファイルごと消す
2. その Pull Request の CI の `plan-admin-identity` が、そのアカウントの resource の削除を示さないことを確かめ、Pull Request に書く
