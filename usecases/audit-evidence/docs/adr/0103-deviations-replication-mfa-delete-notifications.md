# ADR-0103: 保管の Bucket に、リージョン間の複製・MFA delete・event notification を持たせない

- Status: Accepted
- Date: 2026-11-19
- Scope: audit-evidence
- Related: repository-wide/ADR-0207, repository-wide/ADR-0501, audit-evidence/ADR-0101, audit-evidence/ADR-0301

## Context

公式推奨のうち、次の3つをこのユースケースは採らない。

- AWS SRA「Log storage SHOULD be replicated and archived」と Security Hub の S3.7（リージョン間の複製）
- Security Hub の S3.20 と CIS AWS Foundations Benchmark 2.1.2（MFA delete）
- Security Hub の S3.11（event notification）

`docs/v1.0-plan/scope.md` 第7.2節は、DR（リージョン間の複製）を v1.0 の対象外とする。MFA delete は Bucket の所有者（root）しか有効にできず、lifecycle の設定と併用できない（Security Hub の S3.20 の文書）。

## Decision

1. リージョン間の複製を持たない。Checkov の CKV_AWS_144 を `internal/archive-bucket/main.tf` に限って抑止する。
2. MFA delete を持たない。版の削除は Bucket Policy の Deny（audit-evidence/ADR-0102）と Object Lock の COMPLIANCE（audit-evidence/ADR-0101）が防ぐ。
3. event notification を持たない。配信の途絶は request metrics の alarm（audit-evidence/ADR-0301）が、改変は Bucket Policy の Deny と組織の SCP が拒む。Checkov の CKV2_AWS_62 を `internal/archive-bucket/main.tf` に限って抑止する。
4. 3つを公式推奨からの逸脱として記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。抑止は repository-wide/ADR-0501 (development-tooling-composition) 決定13 に従う。

## 検討した代替案

### 案A: 別のリージョンに Object Lock の Bucket と鍵を置いて複製する

`docs/v1.0-plan/scope.md` 第7.2節の改訂を要る。Bucket・鍵・複製の role と権限が倍になる。

### 案B: root で MFA delete を有効にする

Terraform の経路に乗らず、lifecycle と併用できない。

### 案C: EventBridge への通知を有効にする

通知の受け手が無い。途絶の検知は alarm が持つ。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 高 | 中 | 中 | 中 |
| Testability | 低 | 低（root が要る） | 中 | 高 |
| Contract Clarity | 中 | 低 | 中 | 高 |
| Maintainability | 低 | 低 | 中 | 高 |
| Scope Control | 低（scope の外） | 低 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

リージョン障害のあいだ証跡を読めること。

## 保証範囲

- 保証すること: 決定1・3 の抑止が `internal/archive-bucket/main.tf` にだけ在ること
- 保証しないこと: リージョン障害の後に証跡を読めること

## 検証方法

- Security: `make checkov` が CKV_AWS_144・CKV2_AWS_62 を `internal/archive-bucket/main.tf` 以外の path で報告しないこと。抑止の宣言は同ファイルの `checkov:skip` が持つ

## 影響

Security Hub を有効にしたとき、S3.7 と S3.11 の所見が残る。

## 見直し条件

- DR が v1.0 の範囲に入ったとき（決定1）
- root を使わずに MFA delete を有効にする手段を AWS が提供したとき（決定2）
- 証跡の到着を契機に処理する消費者が現れたとき（決定3）
