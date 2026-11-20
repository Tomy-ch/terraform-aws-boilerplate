# ADR-0201: break-glass を管理アカウントの IAM user と role で持つ

- Status: Accepted
- Date: 2026-11-19
- Scope: organization-baseline
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0602, organization-baseline/ADR-0001, organization-baseline/ADR-0101

## Context

repository-wide/ADR-0301 (least-privilege-policy) 決定19 は、IAM Identity Center が使えないときの緊急の到達経路を、この usecase が管理アカウントに提供すると決めている。形は、管理アカウントの IAM user 2つ（コンソールだけ、アクセスキーを持たず、ハードウェア MFA）、すべてのアカウントの break-glass の role（信頼条件は break-glass の user と MFA、権限は `AdministratorAccess`）である。

AWS の whitepaper「Organizing Your AWS Environment Using Multiple Accounts」は、管理アカウントの IAM user を break-glass に使う形を勧める。Well-Architected（2026-11-16 の版）の FND05-BP02 は、平時に使われない専用の非常用アカウントを勧める。SRA のチェックリストは IAM user を使わないことを MUST とし、break-glass を例外とする。Security Hub の IAM.2 は IAM user へ policy を直接付けないこと、IAM.7・IAM.15・IAM.16 はアカウントの password policy を求める。IAM User Guide の、MFA を管理する user の policy の例は、自分の MFA を外すことを MFA で認証したセッションに限る。

本 ADR は管理アカウントの側（user・group・role・password policy）を決める。メンバーアカウントの break-glass の role の置き方は、organization-baseline/ADR-0202 が決める。

## Decision

1. 管理アカウントに、IAM user `break-glass-1`・`break-glass-2` を path `/break-glass/` で置く。パスワードとハードウェア MFA は Terraform で作らず、人が設定して保管する。
2. 権限は IAM group `break-glass` の inline policy で与え、user に policy を直接付けない。与えるのは、各アカウントの break-glass の role の引き受け（MFA を要求）、自分のパスワードと MFA の管理、password policy の読み取りだけとする。自分の MFA を外すこと（`iam:DeactivateMFADevice`）は、MFA で認証したセッションに限る。アクセスキーなど長期の資格情報を作ることを Deny で拒む。
3. role の引き受けの Resource は、アカウントをワイルドカードにした break-glass の role の ARN とする。メンバーアカウントの ID はアカウントを作るまで決まらない（repository-wide/ADR-0301 決定11）。password policy の読み取り（`iam:GetAccountPasswordPolicy`）は resource-level の権限を持たないので、Resource `"*"` で与える（同 決定9）。
4. 管理アカウントの break-glass の role `<project>-break-glass` を path `/break-glass/` で置き、信頼条件を break-glass の user の ARN と `aws:MultiFactorAuthPresent` に限る。`AdministratorAccess` だけを付け、セッションは1時間とする。
5. 管理アカウントの IAM の password policy を持つ。14文字以上、24世代の再利用の禁止、英大文字・英小文字・数字・記号を求め、90日で失効させる。失効した user は自分でパスワードを変えられる（`hard_expiry` を有効にしない）。break-glass の user は45日より短い間隔で試すので、その試験でパスワードを替える。メンバーアカウントの password policy は持たない。メンバーでは IAM user の作成を SCP で拒む（organization-baseline/ADR-0101）。

## 検討した代替案

### 案A: 専用の非常用アカウントに IAM user を置く（Well-Architected FND05-BP02）

アカウントが1つ増え、そのアカウントが全アカウントへ届く経路を新たに持つ。管理アカウントは組織のすべてのアカウントへ届く権限をもともと持つので、そこへ置いても到達できる範囲は増えない（repository-wide/ADR-0301 決定19）。

### 案B: break-glass の user に policy を直接付ける

Security Hub の IAM.2 の所見になる。

### 案C: password policy に有効期限を置かない

Trivy・Checkov・KICS の既定のルールが、90日を超える有効期限を所見にする。期限を置いても、試験の間隔（45日より短い）のほうが短いので、運用は変わらない。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中（全アカウントへの経路が増える） | 中 | 中 | 中（管理アカウントは平時も CI が動く） |
| Testability | 低 | 高 | 高 | 高 |
| Contract Clarity | 中 | 中 | 中 | 高 |
| Maintainability | 低 | 中 | 中（所見の抑止が要る） | 高 |
| Scope Control | 低 | 高 | 高 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 平時に使われない（dormant な）専用の非常用アカウント
- IAM user を1つも持たない組織
- MFA を失くした user が、自分で MFA を外すこと。もう1つの break-glass の user か `organization_admin` が外す

## 保証範囲

- 保証すること: break-glass の user がちょうど2つで、長期の資格情報を Terraform で作らないこと、group の Allow が決定2 に限られること（MFA を外すのは MFA のセッションだけ）、role の信頼条件が break-glass の user と MFA に限られること、`AdministratorAccess` を付ける role が break-glass の role だけであること（`plan.organization_baseline_break_glass`）
- 保証しないこと: user にハードウェア MFA が登録されていること（人の手順。Security Hub の IAM.5・IAM.19 が見る）

## 検証方法

- Unit Test: `internal/break-glass/tests/break-glass.tftest.hcl` が user、group と membership、group の policy、role の信頼条件と、managed policy の attachment と排他、password policy を固定する
- Policy Test: `conftest/plan/organization_baseline_break_glass.rego`（`PLAN-BG-*`）
- 実環境: 人が45日より短い間隔（毎月）で、break-glass の user でサインインし、MFA を使って管理アカウントの break-glass の role を引き受け、通知が届くことを確かめる。手順は README が持つ

## 影響

管理アカウントは、IAM user を2つ持つ。CIS AWS Foundations Benchmark は45日使われていない資格情報を無効にするよう求めるので、試さずに置くと break-glass の user のパスワードがその所見になる。

## 見直し条件

- 非常用の専用アカウントを作ると決めたとき
- 2人目以降の人が break-glass の資格情報を持つとき（パスワードと MFA を別の人が持つ運用）
