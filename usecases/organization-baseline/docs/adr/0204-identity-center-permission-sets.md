# ADR-0204: IAM Identity Center の permission set を閉じた一覧で持ち、管理アカウントへは USER、メンバーへは GROUP で割り当てる

- Status: Proposed
- Date: 2026-11-27
- Scope: organization-baseline
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0602, repository-wide/ADR-0604, organization-baseline/ADR-0001, organization-baseline/ADR-0201, deployment-identity/ADR-0201

## Context

人の通常の到達経路は IAM Identity Center である（repository-wide/ADR-0301 (least-privilege-policy) 決定19）。organization-baseline/ADR-0001 は、Identity Center の管理を委任せず、permission set と割当の変更を Pull Request と管理アカウントの CI でだけ行い、インスタンスの有効化と identity source の選択を人の手順にすると決めた。

repository-wide/ADR-0301 決定10 は、permission set に AWS managed policy を付けることを、設計記録の閉じた一覧に載るものに限って許す。同 決定19 は、管理アカウントで `sts:AssumeRoot` を許す主体を、break-glass の role と、`sts:AssumeRoot` を条件なしの Deny で拒む `AdministratorAccess` の permission set に限ることを求める。管理アカウントには SCP が効かないので、この限定は IAM の側で掛ける。

この ADR は、どの permission set をどの名前で持ち、誰へどう割り当て、Identity Center が有効になる前の段（bootstrap の段1-5）をどう通すかを決める。次の故障モードを持つ。

- permission set の名前は Provider で ForceNew であり、作り直すと全割当の IAM role が作り直され、role の ARN の接尾辞が変わる（IAM Identity Center User Guide「Referencing permission sets in resource policies」）
- `organization_admin`（`AdministratorAccess`）を持つ人の誤操作は、CI の role・state の Bucket Policy・state の鍵の Key Policy を書き換えうる
- 割当の主体の ID を repository に書くと、公開の repository と CI のログに載る
- Identity Center のインスタンスを読む data source は、インスタンスが無い間の plan を落とす

## Decision

1. permission set は次の閉じた一覧の5つとする。各 permission set に付ける AWS managed policy は表の1つだけで、`aws_ssoadmin_managed_policy_attachments_exclusive` だけで付ける。customer managed policy・permissions boundary・policy ごとの付与の resource は使わない。この表が repository-wide/ADR-0301 決定10 の閉じた一覧である。

   | キー | AWS managed policy | 割り当てる先 | セッション |
   | --- | --- | --- | --- |
   | `read_only` | `ReadOnlyAccess` | すべてのアカウント | 8時間 |
   | `terraform_plan` | `ReadOnlyAccess` | Terraform の root を持つアカウント | 1時間 |
   | `billing` | `job-function/Billing` | すべてのアカウント | 1時間 |
   | `support` | `AWSSupportAccess` | すべてのアカウント | 1時間 |
   | `organization_admin` | `AdministratorAccess` | 管理アカウントだけ | 1時間 |

   relay state・description・tag は書かない。tag は共通の単一入力でだけ付ける（repository-wide/ADR-0204 決定5）。
2. permission set の名前は `<project>-<キーの _ を - にしたもの>` とする。Identity Center の上限の32文字に最も長い `<project>-organization-admin` を収めるため、`project` を13文字までに限る。この制限は、`identity_center` が null の間も掛ける。`project` はすべての resource の名前に在り、後から Identity Center を有効にするときに変えられないからである。
3. `organization_admin` の permission set に、Deny だけの inline policy を付ける。拒むのは、`sts:AssumeRoot`（Resource `"*"`、条件なし）と、bootstrap の構成物の書き込みである。bootstrap の構成物は、管理アカウントの CI の role と policy（`<project>-admin-ci-*`）、GitHub の OIDC provider、state・plan の出力・apply の出力の3つの Bucket とその object、state の鍵とする。構成物への Deny は、対象ごとに `NotAction` に読みの動詞（`Get*`・`List*`・`Describe*`）だけを置く形にし、読みを通して、新しい書き込みの Action も既定で拒む。break-glass の user・group・role は拒まない。break-glass の資格情報の設定（docs/setup/organization-baseline.md 3.4）を `organization_admin` で行うからである。この Deny は誤操作を止めるもので、境界ではない（保証範囲）。

   CI の role と policy の Deny は、ARN を列挙せずに `<project>-admin-ci-*` の pattern で書く（repository-wide/ADR-0301 決定11 の但し書き）。その記録は次のとおりである。
   - 覆うべき ARN が pattern に入ることの検査: `plan.organization_baseline_access`（`PLAN-IDC-03`）が、deployment-identity の reference の plan の、宣言 `deployment_identity.module` の下の IAM の role・customer managed policy・OIDC provider のそれぞれの `<種類>/<path><名前>` が、Deny が名指す ARN のどれかに当たることを検査する
   - pattern が覆うべき ARN の外に当てる範囲: 管理アカウントで `<project>-admin-ci-` で始まる名前の、path が `/` の IAM の role と customer managed policy のすべて。deployment-identity が作らない主体でもこの名前を持てば、`organization_admin` から書き込めない
   - 列挙しない理由: CI の role と policy は deployment-identity が持ち、足すたびにこの ADR の Deny を同じ変更で直すことになる。列挙の漏れは Deny の外れになり、pattern の過剰は書き込みを余分に拒むだけである
4. 管理アカウントへは USER で、メンバーアカウントへは GROUP で割り当てる。`organization_admin` は管理アカウントの USER にだけ割り当てる。管理アカウントについては IAM Identity Center User Guide の「Assign users only (not groups) to permission sets in the management account」を採り、Well-Architected FND02-BP03 の「Assign permission sets to groups rather than individual users」をメンバーにだけ採る。管理アカウントへ入る人を、group の membership の変更（Terraform の外）で増やせないようにするためである。
5. 入力 `identity_center` は、Identity Center を有効にする前（bootstrap の段1-5）は null とし、null のとき permission set・付与・割当とインスタンスの data source を作らない。組織のインスタンスは組織の後でしか有効にできず、data source の件数の検査が段4 の plan を落とすからである。null でないときは、provider の region にインスタンスがちょうど1つ在ることを data source の postcondition で確かめる。
6. permission set に `prevent_destroy` を付け、`conftest/plan/decl.json` の `stateful_protection.protected_types` に載せる。管理アカウントの CI の role に permission set の削除の権限を与えない（deployment-identity/ADR-0201）。割当は保護の型にせず、`identity_center.management_assignments` に `organization_admin` の割当が1つ以上在ることを入力の検査で強制する。人の入れ替えのたびに保護の許可の宣言を要さず、最後の管理者の割当を消す plan は plan の前に落とすためである。
7. 人の手元の日常の入口には `read_only` の permission set を使う。`organization_admin` は日常の入口にせず、`terraform_plan` は state の出力の鍵の読み手になりうるので日常の端末に持たせない。手順は docs/setup/organization-baseline.md の 3.10 が持つ。
8. 割当の主体は、人が付けた label（英小文字で始まる短い識別子。人の名前とメールを使わない）で入力と resource の address に書き、Identity Store の user ID と group ID は、管理アカウントの SSM の String の parameter `/<project>/organization-baseline/identity-center-principals`（JSON の object。`users` と `groups` のそれぞれに label から ID へ）から読む。メンバーアカウントのメールの parameter と同じ経路である。parameter の形、`users` と `groups` のそれぞれの ID の形（Provider が `principal_id` に課す形）、同じ label が `users` と `groups` の両方に無いこと、割当が名指す label が在ることを、data source の postcondition で確かめる。同じ label を両方に許すと、片方の ID の誤りがもう片方に隠れる。user と group は Terraform で持たない（`aws_identitystore_*` を使わない）。
9. 割当は、inline policy と managed policy の排他の付与の後に作る（`depends_on`）。`organization_admin` の割当が作る IAM role に、`sts:AssumeRoot` の Deny が無い瞬間を作らないためである。
10. IAM Identity Center User Guide の「Dedicated permission sets for the management account」から外れる（repository-wide/ADR-0207 (default-value-policy) 決定11 の記録）。
    - 外れる推奨: 管理アカウント専用の permission set を作り、メンバーアカウントの permission set と分ける
    - 理由: User Guide の理由は、委任管理者が管理アカウントの permission set を変えられないことである。この構成は Identity Center の管理を委任しないので（organization-baseline/ADR-0001 決定2）、管理アカウントの permission set を変えられるのは管理アカウントの CI だけで、その理由が当たらない
    - 影響: permission set の変更が、それを割り当てたすべてのアカウントへ同時に及ぶ
    - 代替案: 管理アカウント用の4つ（`read_only` など）を足して9つにする
    - 見直し条件: Identity Center の管理を委任するとき（organization-baseline/ADR-0001 の見直し条件と同じ）

## 検討した代替案

### 案A: user と group を Terraform で持つ

`aws_identitystore_user`・`aws_identitystore_group` で持つ。user 名とメールが repository に載り、CI の role に identitystore の書き込みが要る。外部 IdP へ移ると、SCIM と Terraform が同じ directory を書き合う。

### 案B: 管理アカウント専用の permission set を持つ

決定10 の代替案。委任しない構成では得るものが無く、permission set と割当の型が増える。

### 案C: `organization_admin` の Deny を `sts:AssumeRoot` だけにする

bootstrap の構成物の誤った書き換えを止めない。CI の role の信頼条件や state の Key Policy を誤って変えると、CI の経路そのものが止まる。

### 案D: 割当を保護の型にする

人の入れ替えのたびに保護の許可の宣言が要る。割当の作り直しは role を作り直すが、permission set の名前が変わらない限り role の名前と ARN の形は保たれる。

### 案E: managed policy を付与ごとの resource で付ける

`aws_ssoadmin_managed_policy_attachment` は帯域外の付与を消さない。permission set は `prevent_destroy` で消えないので、付与を破棄の順序で守る必要も無い。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 案E | 採用案 |
| --- | --- | --- | --- | --- | --- | --- |
| Security | 中（ID と名前が載る） | 中 | 低（構成物を誤って変えうる） | 中 | 低（帯域外の付与が残る） | 中（Deny は誤操作の防止で境界ではない） |
| Testability | 中 | 中 | 高 | 中 | 中 | 高（Unit Test と `PLAN-IDC-*`） |
| Contract Clarity | 低 | 中 | 中 | 中 | 中 | 高（閉じた一覧と label） |
| Maintainability | 低（IdP と書き合う） | 低 | 高 | 低（入れ替えごとの宣言） | 中 | 高 |
| Scope Control | 低 | 低 | 高 | 中 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- permission set の一覧と中身を利用者が入力で変えること
- 管理アカウントへ group で入ること
- 管理アカウント専用の permission set（決定10）
- `organization_admin` で bootstrap の構成物を直すこと。直すのは CI の経路か break-glass の role である（repository-wide/ADR-0602 決定9）

## 保証範囲

- 保証すること: permission set が決定1 の5つで、名前とセッションが決定1・2 のとおりで、各 permission set に決定1 の AWS managed policy だけが排他で付き、ほかの付与の resource が無いこと。`AdministratorAccess` を付ける permission set にだけ inline policy が在り、それが Deny だけで、Resource `"*"`・条件なしの `sts:AssumeRoot` の Deny と、決定3 の構成物の書き込みの Deny を持ち、`NotAction` が読みの動詞だけであること。構成物の書き込みの Deny が、条件も `Action` も持たない形であり、構成物を名指すほかの形の Deny が無いこと。管理アカウントの CI の role・policy・OIDC provider が Deny の ARN に入ること（決定3 の pattern の検査）。`AdministratorAccess` を付ける permission set の割当が管理アカウントの USER だけで、それが1つ以上在り、管理アカウントへの割当が USER だけで、メンバーアカウントへの割当が GROUP だけであること。判定は permission set のキーでなく、付く policy で行う（Unit Test と `plan.organization_baseline_access` の `PLAN-IDC-*`）
- 保証しないこと: `organization_admin` の inline の Deny が、`AdministratorAccess` を持つ人を止めること。その人は新しい IAM の主体を作って許可を与えれば越えられ、管理アカウントには SCP が効かない。管理アカウントで `sts:AssumeRoot` を呼べる主体が break-glass の role だけであり続けることも、同じ理由で保証しない（organization-baseline/ADR-0001 の保証範囲）
- 保証しないこと: `PLAN-IDC-*` が env の実 plan に掛かること。掛かるのは reference の mock の plan の文書の形までである。実 plan では、`plan.stateful_protection` が permission set の削除・置換・管理からの除外を落とす
- 保証しないこと: `organization_admin` を持つ人が、Identity Center の directory の user・group・membership と、identity source・MFA・セッションの既定の設定を変えること（Terraform の外。drift を plan で見ない）。MFA が「サインインごと」であること（Provider に resource が無い。人の手順）
- 保証しないこと: 割当の相手。label から ID への対応は parameter `/<project>/organization-baseline/identity-center-principals` が持つので、それを書ける主体が、`organization_admin` を誰に割り当てるかを決める。管理アカウントの CI の role には、この parameter の書き込みを拒む（deployment-identity/ADR-0201）。残る書き手は、管理アカウントの管理者（`organization_admin` と break-glass の role）である。parameter の形と label の在ることは postcondition が確かめるが、ID が正しい人を指すことは確かめない
- 保証しないこと: 主体の ID を plan と state から隠すこと。ID は plan の JSON と state に平文で入る。どちらも暗号化した Bucket にだけ置く。repository・resource の address・公開のログには label だけが載る

## 検証方法

- Unit Test: `internal/access/tests/access.tftest.hcl` が、permission set の一覧と名前とセッション、排他の付与、inline policy の文の完全一致と入力の境界での長さ、割当の種類と対象、インスタンスが1つでないとき・parameter の形が違うとき・`users` と `groups` に同じ label があるとき・label が無いとき・対応表に無いアカウントのとき・管理アカウントへの割当が空のときに plan が落ちることを固定する。`tests/validation.tftest.hcl` が入力の検査（決定1・2・4・6・8）を、`tests/contract.tftest.hcl` が出力 `identity_center_instance_arn` を固定する
- Policy Test: `conftest/plan/organization_baseline_access.rego`（`PLAN-IDC-*`）。決定6 の保護は `plan.stateful_protection`
- 決定9: `conftest/hcl/resource_order/decl.json` が、割当の `depends_on` を HCL のソースで検査する（`make resource-order-check`）
- 決定7: 機械の検査は無い。手順書（docs/setup/organization-baseline.md の 3.10）の記述が保つ
- 実環境: 昇格の後に人が、AWS access portal から入れること、`organization_admin` の `sts:AssumeRoot` が拒まれること、次の CI の plan が変更を示さないことを確かめる。手順は docs/setup/organization-baseline.md が持つ

## 影響

管理アカウントの CI の apply-identity の role は、permission set・付与・inline policy・割当の作成と更新の権限と、管理アカウントへの割当で Identity Center が呼び手の資格情報で作る IAM role への書き込みの権限を要し、plan の role はその読み取りと parameter の読み取りを要する。それらは `deployment-identity` の権限であり、deployment-identity/ADR-0201 が決める。

人は Identity Center を有効にした後、user と group を directory に作り、label と ID の組を parameter に置いてから、`identity_center` を入れる Pull Request を出す。

## 見直し条件

- 決定10 の見直し条件
- 管理アカウントへ入る人が2人以上になり、group で割り当てる必要が出たとき
- permission set の一覧を増やす要求が出たとき（一覧の変更はこの ADR の変更である）
- Identity Center の構成（MFA など）を持つ resource を Provider が持ったとき
