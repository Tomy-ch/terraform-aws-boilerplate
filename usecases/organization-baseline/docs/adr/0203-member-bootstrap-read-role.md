# ADR-0203: メンバーアカウントの立ち上げの plan に、StackSet で配る読み取りの role を使う

- Status: Accepted
- Date: 2026-11-25
- Scope: organization-baseline
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0602, repository-wide/ADR-0604, organization-baseline/ADR-0001, organization-baseline/ADR-0101, organization-baseline/ADR-0202, deployment-identity/ADR-0201

## Context

repository-wide/ADR-0604 決定6 は、メンバーアカウントの立ち上げの間、管理アカウントの identity root の apply を apply-identity の role がそのアカウントの `OrganizationAccountAccessRole` を引き受けて行い、plan を plan の role がそのアカウントにだけ置く立ち上げ用の読み取りの role を引き受けて行うと決める。読み取りの role は、統制の構成物として管理アカウントから配る（repository-wide/ADR-0602 決定1）。plan の role に `OrganizationAccountAccessRole` を引き受けさせないのは、Pull Request のコードを走らせる job が、立ち上げの間メンバーの管理者になるからである（repository-wide/ADR-0602 決定13）。

この ADR は、その読み取りの role を、この usecase がどう配り、何を許し、立ち上げの外でどう閉じるかを決める。

メンバーの側に読み取りの role を Terraform で先に作る形は成り立たない。その role を作る Pull Request の plan 自身が、メンバーへ届く provider を要するからである。管理アカウントの identity root がメンバーへ届く provider を持つのは、そのアカウントの立ち上げの間だけである（repository-wide/ADR-0604 決定6）。読み取りの role はその provider より前に要るので、管理アカウントの StackSet で置く（organization-baseline/ADR-0202）。

## Decision

1. メンバーアカウントの立ち上げの plan に使う読み取りの role `<project>-bootstrap-read`（path `/bootstrap/`）を、管理アカウントの CloudFormation StackSet（permission model は `SERVICE_MANAGED`）で、すべてのメンバーアカウントに配る。自動展開を有効にし、アカウントが組織を離れたときは stack を残さない。managed execution を有効にする。stack instance の対象は組織の Root、region は provider の region だけとする。StackSet の名前は role の名前と同じにする。
2. break-glass の StackSet（organization-baseline/ADR-0202）に同居させない。role の寿命と、立ち上げの外での扱い（決定5）が違うためである。
3. 信頼条件は、Principal を管理アカウントの root にし、`aws:PrincipalArn` を管理アカウントの plan の role の ARN に `ArnEquals` で限る。Principal に role の ARN を直接書かない。role を作り直すと、IAM が保存した一意 ID がずれて信頼が壊れるためである。plan の role の ARN は、型付きの入力 `bootstrap_read_principal_arn` で受ける（repository-wide/ADR-0301 決定18）。
4. 権限は1つの inline policy に、メンバーの `state-backend` と `deployment-identity` の refresh に要る S3・KMS・IAM の読み取りの Action だけを列挙する（repository-wide/ADR-0301 決定8）。state の object（`s3:GetObject`）、鍵の暗号操作、secret の値の読み取りは与えない。Resource は置き先のアカウントに限り、根拠は次のとおりである（repository-wide/ADR-0301 決定9・11）。
   - S3 の Bucket は `<project>-*-{tfstate,tfplan,tfapply}-<アカウント ID>-*` の prefix pattern にする。env の token は Bucket の名前の途中に在り、1つのテンプレートをすべてのメンバーへ配るので、env を名指せない。先頭の project の token と、置き先のアカウント ID で限る
   - KMS の鍵は `key/*` にする。鍵の ID は作るまで決まらず、テンプレートから参照できない。与えるのは鍵の設定の読み取り（`DescribeKey`・`GetKeyPolicy`・`GetKeyRotationStatus`・`ListResourceTags`）だけで、暗号操作を含まない
   - `kms:ListAliases` は resource-level の権限を持たないので Resource `"*"` にする（Service Authorization Reference）
   - IAM の CI の role と policy は `<project>-*-ci-*` の prefix pattern にする。S3 と同じく env を名指せないためである。OIDC provider は固定の ARN にする
   - `deployment-identity` が作るアカウントの前提の Chatbot の service-linked role は、固定の ARN（`role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot`）にする（deployment-identity/ADR-0002）。与えるのは `iam:GetRole` だけである

   管理アカウントの plan の role の inline policy の読みのうち、管理アカウントの構成物（組織、break-glass の user と group、StackSet、メールの parameter、password policy）と state の object・plan の出力にだけ掛かるもののほかを、すべて持つ。除いた Action は、理由と組にして `conftest/plan/decl.json` の `organization_baseline.bootstrap_read.management_only_actions` に宣言する。
5. 立ち上げの外では閉じる。`deny-bootstrap-role` は、`OrganizationAccountAccessRole` とこの role の両方の使用を拒む。`protect-governance-roles` は、StackSets の実行の role（`stacksets-exec-*`）のほかに、この role の変更を拒む（organization-baseline/ADR-0202 決定5 と同じ例外）。この usecase が持つのはメンバーの側の拒否だけである。管理アカウントの側で、この role の引き受けをどのアカウントに許すかは `deployment-identity` が決める（deployment-identity/ADR-0201 決定22）。
6. StackSet と stack instance に `prevent_destroy` を付け、`stateful_protection.protected_types` に載せる。
7. FSBP の CloudFormation.4 から外れる（repository-wide/ADR-0207 決定11 の記録）。
   - 外れる推奨: FSBP の CloudFormation.4（stack に service role が付いていること）
   - 理由: service-managed の StackSet が作る stack は、service role を持てない（organization-baseline/ADR-0202 決定8 と同じ）
   - ユースケース上の制約: 管理アカウントの identity root がメンバーアカウントへ届く provider を持つのは、そのアカウントの立ち上げの間だけである（repository-wide/ADR-0604 決定6）。読み取りの role はその provider より前に要るので、管理アカウントの StackSet で置く
   - 影響: security は、すべてのメンバーアカウントで、この stack も CloudFormation.4 の FAILED になる。抑止はこの usecase で行わない。availability と cost への影響は無い
   - 代替案: メンバーごとの provider の alias で role を作る。立ち上げの循環を解けない（Context）
   - 見直し条件: StackSets の service-managed の stack が service role を持てるようになったとき。メンバーアカウントへ届く provider を、立ち上げの外でもこの root が持つと決めたとき

## ネイティブ統合で解けない理由

ネイティブ統合で解けており、コードは持たない。メンバーへ role を配るのは AWS の StackSets で、テンプレートは `AWS::IAM::Role` を1つ宣言するだけである（repository-wide/ADR-0103 決定4）。

## 入出力

- 入力: role の名前と path、プロジェクトの token、引き受けてよい管理アカウントの role の ARN、組織の Root の ID
- 出力: 各メンバーアカウントの IAM role 1つ

## 失敗時の扱い

organization-baseline/ADR-0202 の「失敗時の扱い」と同じである。手順は `docs/setup/organization-baseline.md` が持つ。読み取りの Action が足りないときは、立ち上げの plan が `AccessDenied` で落ちる。書き込みへは広がらない。

## 業務要件の変更で修正が必要にならない理由

中身は、メンバーの立ち上げの構成物の読み取りだけであり、業務データも業務の判断も持たない。

## 検討した代替案

### 案A: plan の role に `OrganizationAccountAccessRole` の引き受けを許す

Pull Request のコードを走らせる job が、立ち上げの間メンバーの管理者になる。repository-wide/ADR-0602 決定13 を弱める。

### 案B: `OrganizationAccountAccessRole` の信頼条件に SourceIdentity を求め、メンバーの SCP で書き込みを拒む

信頼条件の変更は、その変更自身の plan が引き受けられない限り plan できない。循環を切るには、人が各メンバーで信頼条件を手で書き換える必要があり、変更の経路の外の直接の変更になる。

### 案C: メンバーの読み取りの role を Terraform で先に作る

作る Pull Request の plan が、メンバーへ届く provider を要する。循環が1段ずれるだけである。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 低（plan が管理者になる） | 中 | 中 | 高（読み取りだけ。立ち上げの外では SCP が閉じる） |
| Testability | 中 | 低（手作業を挟む） | 低（成り立たない） | 高（テンプレートを Unit Test と Policy Test で見る） |
| Contract Clarity | 中 | 低 | 低 | 高 |
| Maintainability | 中 | 低 | 低 | 中（plan の role の読みが増えたら、ここも増やす。Policy Test が知らせる） |
| Scope Control | 中 | 低 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 立ち上げの plan が、state の object を読むこと。立ち上げの間、メンバーの構成物の state は管理アカウントの state Bucket に在る
- FSBP の CloudFormation.4 に適合すること（決定7）

## 保証範囲

- 保証すること: StackSet が service-managed で自動展開して Root へ置き、テンプレートが1つの inline policy を持つ1つの role だけを持ち、信頼条件が管理アカウントの root と管理アカウントの plan の role の ARN に限り、inline policy が S3・KMS・IAM の wildcard の無い読み取りの Action の Allow だけで、Resource が置き先のアカウントに限られ（`kms:ListAliases` を除く）、state の object と鍵の暗号操作を含まないこと。管理アカウントの plan の role の読みのうち、宣言で除いたもののほかをすべて持つこと。`deny-bootstrap-role` がこの role の使用を拒み、`protect-governance-roles` がこの role の変更を拒むこと（Unit Test と `plan.organization_baseline_bootstrap_read` の `PLAN-BR-*`）
- 保証しないこと: 列挙した読み取りで、立ち上げの plan が実際に `AccessDenied` 無しで通ること（最初の立ち上げの CI の plan で確かめる）。すべてのメンバーで stack instance が `CURRENT` になること（人の確認。手順書が持つ）

## 検証方法

- Unit Test: `internal/bootstrap-access/tests/bootstrap-access.tftest.hcl` が StackSet の設定、テンプレート、信頼条件、読み取りだけの inline policy を固定する。`internal/organization/tests/organization.tftest.hcl` が `deny-bootstrap-role` と `protect-governance-roles` の文を固定する
- Policy Test: `conftest/plan/organization_baseline_bootstrap_read.rego`（`PLAN-BR-*`）
- 実環境: 立ち上げの最初の CI の plan が `AccessDenied` 無しで通ること。stack instance の状態は人が確かめる。手順は `docs/setup/organization-baseline.md` が持つ

## 影響

管理アカウントに StackSet が1つ増え、メンバーアカウントごとに stack が1つ置かれる。管理アカウントの CI の apply-identity の role は、この StackSet の作成と更新の権限を、plan の role はその読み取りとこの role の引き受けの権限を要する。それらは `deployment-identity` の権限であり、deployment-identity/ADR-0201 決定7・22 が決める。

## 見直し条件

- 決定7 の見直し条件のいずれか
- メンバーの立ち上げを、管理アカウントの identity root の外で行うと決めたとき
