# ADR-0202: メンバーアカウントの break-glass の role を、service-managed の StackSet で配る

- Status: Accepted
- Date: 2026-11-20
- Scope: organization-baseline
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0602, organization-baseline/ADR-0001, organization-baseline/ADR-0101, organization-baseline/ADR-0201, deployment-identity/ADR-0201

## Context

repository-wide/ADR-0301 (least-privilege-policy) 決定19 は、すべてのアカウントに break-glass の role を置き、その信頼条件を管理アカウントの break-glass の user と MFA に限ると決めている。organization-baseline/ADR-0201 は管理アカウントの側を決め、メンバーアカウントの role の置き方は、それを配る変更が決めるとした。

メンバーアカウントは、この usecase が組織を作った後に作られる。管理アカウントの identity root は管理アカウントの provider だけを持ち、メンバーアカウントへ届く provider を持たない（organization-baseline/ADR-0001）。

Well-Architected（2026-11-16 の版）の FND05-BP02 は、メンバーアカウントの break-glass の role を CloudFormation StackSets で配り、その role の変更を SCP で拒むことを勧める。Security Hub の FSBP の CloudFormation.4 は、stack に service role が付いていることを求め、service-managed の StackSet が作る stack を必ず FAILED にする（コントロールの文書の原文）。

service-managed の StackSet は、組織への信頼アクセスを CloudFormation の側で有効にした後でないと作れない。メンバーアカウントでは、service-linked role（`AWSServiceRoleForCloudFormationStackSetsOrgMember`）と、テンプレートを実行する service role（`stacksets-exec-*`）が作られる。service-linked role は SCP の対象にならず、`stacksets-exec-*` は SCP の対象になる（CloudFormation の User Guide「Activate trusted access with AWS Organizations」）。

## Decision

1. メンバーアカウントの break-glass の role を、管理アカウントの CloudFormation StackSet（permission model は `SERVICE_MANAGED`）で配る。自動展開を有効にし、新しく作るアカウントにも置く。アカウントが組織を離れたときは stack を残さない。managed execution を有効にし、自動展開と更新の操作が重なったときは、後の操作を失敗させずに待たせる。
2. stack instance の対象は組織の Root とし、region は provider の region だけとする。StackSets は管理アカウントへ stack を置かない。管理アカウントの role は organization-baseline/ADR-0201 決定4 が持つ。
3. StackSet の名前は break-glass の role の名前と同じにする。`deployment-identity` は、`break_glass_arns` の role の ARN からこの名前を組み、apply-identity の role の権限の Resource を限る（deployment-identity/ADR-0201 決定3・4）。
4. テンプレートは `AWS::IAM::Role` を1つだけ持つ。名前・path・セッションの長さ・信頼条件・managed policy（`AdministratorAccess` だけ）は、管理アカウントの break-glass の role と同じにする。信頼条件は、管理アカウントの break-glass の user の ARN（path と名前から組んだもの）と `aws:MultiFactorAuthPresent` に限る。inline policy と tag を持たない。
5. `protect-governance-roles` の SCP は、`stacksets-exec-*` の role を `aws:PrincipalArn` の例外にする。例外が無いと、StackSet がメンバーで role を作れない。例外にするのは StackSets の実行の role だけとし、break-glass の role の変更は、ほかのすべての主体に拒む。例外は role の名前で当たるので、同じ SCP で、`stacksets-exec-*` の role の作成と変更をすべての主体に拒む。この role を作るのは service-linked role で、SCP の対象にならない。
6. StackSet と stack instance に `prevent_destroy` を付け、`stateful_protection.protected_types` に載せる。CI の apply-identity の role に、その削除の権限を与えない（deployment-identity/ADR-0201）。
7. 組織への信頼アクセスの有効化（`ActivateOrganizationsAccess`）は、人がコンソールで行う。Organizations の API で有効にせず、CloudFormation の側で有効にする（organization-baseline/ADR-0001 決定5）。手順は `docs/setup/organization-baseline.md` が持つ。組織の作成と StackSet の作成が同じ apply に入る新しい管理アカウントでは、その apply は StackSet の作成で止まる。人が信頼アクセスを有効にした後、apply をやり直す。
8. FSBP の CloudFormation.4 から外れる（repository-wide/ADR-0207 決定11 の記録）。
   - 外れる推奨: FSBP の CloudFormation.4（stack に service role が付いていること）
   - 理由: service-managed の StackSet が作る stack は、service role を持てない。コントロールの文書は、この stack を必ず FAILED にすると述べる
   - ユースケース上の制約: 管理アカウントの identity root は、メンバーアカウントへ届く provider を持たない（organization-baseline/ADR-0001）。メンバーへ resource を置く経路は StackSets だけである
   - 影響: security は、すべてのメンバーアカウントで break-glass の stack が CloudFormation.4 の FAILED になる（Security Hub での抑止はこの usecase で行わない。検出結果の抑止は、それを持つ変更が決める）。stack の権限は CloudFormation の service role ではなく StackSets の実行の role が持つ。availability と cost への影響は無い
   - 代替案: 管理アカウントの外の provider（メンバーごとの alias）で role を作る。採らない理由は「検討した代替案」に在る
   - 見直し条件: StackSets の service-managed の stack が service role を持てるようになったとき。メンバーアカウントへ届く provider を、この root が持つと決めたとき

## ネイティブ統合で解けない理由

ネイティブ統合で解けており、コードは持たない。メンバーへ role を配るのは AWS の StackSets で、テンプレートは `AWS::IAM::Role` を1つ宣言するだけの論理リソースの宣言である（repository-wide/ADR-0103 決定4）。`aws_cloudformation_stack_set` は、テンプレートに Lambda のコードを埋め込める型なので、型の分類でインフラ内コードに当たり（同 決定9）、許可リストに載る。テンプレートがコードを持たないことは `PLAN-BG-05` が見る。

## 入出力

- 入力: break-glass の role の名前と path、管理アカウントの break-glass の user の ARN（path と名前から組んだもの）、partition
- 出力: 各メンバーアカウントの IAM role 1つ

## 失敗時の扱い

stack instance の作成や更新が失敗すると、Terraform の apply は StackSet の操作の失敗で止まる（AWS Provider の `aws_cloudformation_stack_instances` は操作の完了を待つ）。作成の API が拒まれたときは、resource は state に入らない。API が通って操作が失敗したときは、resource は state に tainted で残り、次の plan は作り直しを求めて `prevent_destroy` で止まる。CI は削除の権限を持たないので、人が原因を直し、state の tainted を外す。権限の不足で拒まれたときは、権限とそれを要る resource を別の apply に分ける。手順は `docs/setup/organization-baseline.md` が持つ。

## 業務要件の変更で修正が必要にならない理由

中身は、Identity Center が使えないときの緊急の到達経路（repository-wide/ADR-0301 決定19）だけであり、業務データも業務の判断も持たない。

## 検討した代替案

### 案A: メンバーごとの provider の alias で role を作る

管理アカウントの identity root が、メンバーごとに `OrganizationAccountAccessRole` を引き受ける provider を持つ。アカウントを足すたびに provider と module の呼び出しを足す。`deny-bootstrap-role` の SCP は立ち上げの後に `OrganizationAccountAccessRole` を拒むので、立ち上げの後の変更が通らない。CloudFormation.4 からは外れない形になる。

### 案B: アカウントの立ち上げの手順で人が作る

人の手順が増え、role の中身が宣言から外れる。中身のずれを Policy で見られない。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 中（管理アカウントの CI がメンバーの bootstrap の role を使い続ける） | 低（宣言の外） | 中（StackSet の更新はすべてのメンバーへ届く。deployment-identity/ADR-0201 の保証しないこと） |
| Testability | 中 | 低 | 高（テンプレートを Unit Test と Policy Test で見る） |
| Contract Clarity | 中 | 低 | 高 |
| Maintainability | 低（アカウントごとに provider） | 低 | 高（新しいアカウントへ自動で置く） |
| Scope Control | 中 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- メンバーアカウントの break-glass の role の drift を Terraform の plan で見ること。StackSets の drift 検出の範囲であり、変更そのものは `protect-governance-roles` が拒む
- FSBP の CloudFormation.4 に適合すること（決定8）

## 保証範囲

- 保証すること: StackSet が service-managed で自動展開し、テンプレートが管理アカウントの break-glass の role と同じ1つの role だけを持つこと（Unit Test と `plan.organization_baseline_break_glass` の `PLAN-BG-05`）。`protect-governance-roles` が外すのは `stacksets-exec-*` だけで、その名前の role の作成と変更をすべての主体に拒むこと（`PLAN-ORG-06`）
- 保証しないこと: すべてのメンバーアカウントで stack instance が実際に `CURRENT` になること（人の確認。手順書が持つ）。メンバーで role が作られた後に変えられていないこと（`protect-governance-roles` と StackSets の drift 検出に依る）。StackSets が `stacksets-exec-*` の role を作り、変える経路が、SCP の対象にならない service-linked role であること（AWS の文書に依る。メンバーアカウントを作った後に、stack instance が `CURRENT` になることで人が確かめる）

## 検証方法

- Unit Test: `internal/break-glass/tests/break-glass.tftest.hcl` が、StackSet の設定とテンプレート、stack instance の対象、メンバーの role と管理アカウントの role が同じであることを固定する。`internal/organization/tests/organization.tftest.hcl` が `protect-governance-roles` の例外を固定する
- Policy Test: `conftest/plan/organization_baseline_break_glass.rego`（`PLAN-BG-05`）、`conftest/plan/organization_baseline_scp.rego`（`PLAN-ORG-06`）
- 実環境: メンバーアカウントを作った後、人が stack instance の状態と、メンバーの break-glass の role を引き受けられることを確かめる。手順は `docs/setup/organization-baseline.md` が持つ

## 影響

管理アカウントに StackSet が1つ増え、メンバーアカウントごとに stack が1つ置かれる。管理アカウントの CI の apply-identity の role は、この StackSet の作成と更新の権限を持つ（deployment-identity/ADR-0201）。

## 見直し条件

- 決定8 の見直し条件のいずれか
- 非常用の専用アカウントを作ると決めたとき（organization-baseline/ADR-0201 の見直し条件と同じ）
