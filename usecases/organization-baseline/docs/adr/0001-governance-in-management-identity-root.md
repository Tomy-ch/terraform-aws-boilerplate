# ADR-0001: 統制を管理アカウントの identity root に置き、Identity Center と root の中央管理を委任しない

- Status: Accepted
- Date: 2026-11-19
- Scope: organization-baseline
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0602, repository-wide/ADR-0604, organization-baseline/ADR-0101, organization-baseline/ADR-0201

## Context

`organization-baseline` は、AWS Organizations の組織・OU・メンバーアカウント、SCP・RCP・declarative policy、root の資格情報の中央管理、break-glass、IAM Identity Center の permission set と割当を持つ。これらは CI と人の到達と権限を組織として縛る仕組みであり、repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定1 の統制の構成物に当たる。統制の構成物は管理アカウントの identity root が持ち、apply-identity の role が apply する（同 決定13、repository-wide/ADR-0604 (state-split-by-apply-role) 決定3）。

AWS Security Reference Architecture（SRA）は、Identity Center の管理を Shared Services アカウントへ、root の資格情報の中央管理を Security Tooling アカウントへ委任し、管理アカウントで日常の管理を行わないことを勧める。SRA のチェックリストは identity source に企業の IdP を、Identity Center に multi-Region の構成を勧める。本 ADR はこれらから外れる構成を記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。

## Decision

1. `organization-baseline` は、管理アカウントの identity root（`env/admin/identity/`）だけが呼ぶ。workload root とメンバーアカウントの root は呼ばない。
2. IAM Identity Center の管理を委任しない。permission set と割当の変更は、Pull Request と CI（apply-identity の role）でだけ行う。
3. root の資格情報の中央管理（`RootCredentialsManagement`・`RootSessions`）を委任しない。`sts:AssumeRoot` を呼べる主体は、管理アカウントの break-glass の role に限る（organization-baseline/ADR-0201）。
4. Identity Center は、この usecase の provider と同じリージョン（`ap-northeast-1`）に、single-Region で、AWS owned key で置く。有効化と identity source の選択は人の手順とし、usecase は data source でインスタンスを読む。
5. 組織の信頼アクセスは、各サービスの側で有効化する。この usecase は `aws_service_access_principals` を書かず、変更を無視する。root の資格情報の中央管理の前提である IAM の信頼アクセスだけを、`aws_organizations_aws_service_access` で持つ。

## 検討した代替案

### 案A: Identity Center の管理を Shared Services アカウントへ委任する（SRA の形）

委任先は全メンバーアカウントへ権限を配れるアカウントになり、統制の構成物が2つのアカウントに分かれる。Shared Services アカウントを新たに作る。

### 案B: Identity Center の管理と root の中央管理をセキュリティのアカウントへ委任する

セキュリティのアカウントには検知を調べる日常の作業が入る。そこへ全アカウントへ届く経路（権限の配布と `sts:AssumeRoot`）を置くと、日常の出入りがあるアカウントを抜かれた被害が組織全体へ広がる。break-glass をセキュリティのアカウントに置かなかった理由（repository-wide/ADR-0301 (least-privilege-policy) 案F）と同じである。

### 案C: Identity Center を multi-Region にする

管理アカウントに multi-Region の customer managed key が要り、その Key Policy の誤りが Identity Center 自体に入れなくなる故障を作る。主リージョンの障害は break-glass（IAM。リージョンに依らない）で覆える。複製先の追加と鍵の切り替えは後からできる。

### 案D: 信頼アクセスの一覧を Terraform で持つ

`aws_service_access_principals` は authoritative で、一覧に無い principal をサービスの側で有効化すると次の apply が外し、そのサービスが組織で動かなくなる。AWS Organizations User Guide は、信頼アクセスを信頼されるサービスの側で有効化することを強く勧める。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 中（統制が2つのアカウントに分かれる） | 低（日常の作業が入るアカウントに全アカウントへの経路） | 中（鍵の誤りで入れなくなる） | 中 | 中（管理アカウントへの到達者が統制を変えられる。変更の正本は CI） |
| Testability | 低（アカウントが増える） | 中 | 中 | 高 | 高 |
| Contract Clarity | 中 | 中 | 中 | 中 | 高（統制の構成物が1つの root に揃う） |
| Maintainability | 低 | 中 | 低 | 低（サービスの有効化が apply と衝突） | 高 |
| Scope Control | 低（Shared Services アカウントを作る） | 中 | 中 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 管理アカウントへ人が入らずに Identity Center を管理すること（SRA の委任の目的）
- 主リージョンの障害の間、break-glass に頼らずに Identity Center から入り続けること
- 信頼アクセスの drift を plan で見ること（IAM を除く）

## 保証範囲

- 保証すること: 信頼アクセスの一覧を Terraform が書かないこと（決定5）
- 保証しないこと: この usecase を管理アカウントの identity root のほかの root が呼ばないこと（決定1）。これを見る検査は無い
- 保証しないこと: 管理アカウントの `organization_admin` を持つ人が、CI を通さずに Identity Center を変えること。SCP は管理アカウントに効かない
- 保証しないこと: 決定3 の「`sts:AssumeRoot` を呼べる主体は break-glass の role に限る」を、管理アカウントの `organization_admin`（`AdministratorAccess`）を持つ人に対して保つこと。この限定は、CI の role の権限と、`organization_admin` の permission set の inline の Deny（organization-baseline/ADR-0204）で保つ。`AdministratorAccess` を持つ人は新しい IAM の主体を作って許可を与えれば越えられ、管理アカウントには SCP が効かない。Deny は誤操作を止めるもので、境界ではない

## 検証方法

- 決定5: `conftest/plan/decl.json` の `inline_arguments` が `aws_organizations_organization` の `aws_service_access_principals` を書く変更を落とす（`plan.stateful_protection` の `PLAN-STATE-08`）

## 影響

管理アカウントの `organization_admin` と break-glass の role は、Identity Center と root の中央管理を変えられる。到達者を Identity Center の USER の割当に限り（管理アカウントへの割当は USER だけ）、変更の正本を CI に置く。

## 見直し条件

- 組織の管理者とは別に ID の担当者が現れたとき
- Shared Services のアカウントを作ると決めたとき（委任先は後から変えると移行が要るので、そのときに一緒に判断する）
- 2人目以降の人が日常の作業をし、主リージョンの障害の間も break-glass に頼らずに続ける必要が出たとき
- Identity Center につなぐ AWS managed application を使い始めたとき（customer managed key への対応が条件になる）
- identity source を外部 IdP にするとき（multi-Region では、IdP が複数の ACS URL を持てるかが効く）
