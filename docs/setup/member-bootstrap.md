# メンバーアカウントの立ち上げ 手順書

## 1. 設定の概要

メンバーアカウント（security・log-archive）に、その identity root の前提（`state-backend` と `deployment-identity`）を置く。resource は管理アカウントの identity root（`env/admin/identity`）が、立ち上げの中のアカウントへの provider で作り、admin への昇格で CI の apply-identity の role が apply する（[organization-baseline/ADR-0203](../../usecases/organization-baseline/docs/adr/0203-member-bootstrap-read-role.md)、[deployment-identity/ADR-0201](../../usecases/deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定22）。

- メンバーの provider は、apply-identity の role の session では `OrganizationAccountAccessRole` を、それ以外の session では立ち上げの読み取りの role（`/bootstrap/<project>-bootstrap-read`）を引き受ける
- 立ち上げの中のアカウントは、`env/admin/identity/main.tf` の `bootstrapping` にあるものだけである。外したアカウントでは、SCP（`deny-bootstrap-role`）と管理アカウントの IAM の Allow の両方から引き受けが消える

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

`env/admin/identity` にメンバーの `state-backend`・`deployment-identity` を足し、`.github/settings/` に environment とデプロイブランチを足す Pull Request の merge の前に、リポジトリの管理者が次を行う。`<env>` は `security` と `log-archive` のそれぞれである。

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
3. 次の Pull Request の CI の `plan-admin-identity` が変更を示さないことを確かめ、Pull Request に書く
