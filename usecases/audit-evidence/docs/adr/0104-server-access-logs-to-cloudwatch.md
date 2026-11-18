# ADR-0104: 保管の Bucket の S3 サーバーアクセスログを、CloudWatch Logs へ配送する

- Status: Accepted
- Date: 2026-11-19
- Scope: audit-evidence
- Related: repository-wide/ADR-0207, audit-evidence/ADR-0103

## Context

Object Lock を有効にした Bucket は S3 サーバーアクセスログの宛先にできず、宛先の Bucket は SSE-S3 に限られる（Amazon S3 User Guide「Object Lock considerations」「Enabling Amazon S3 server access logging」）。`docs/v1.0-plan/scope.md` 第9.1節は、S3 サーバーアクセスログを S3 ではなく CloudWatch Logs へ配送するとする。S3 は CloudWatch Logs の配送（delivery source・delivery destination・delivery）で `S3_SERVER_ACCESS_LOGS` を送れる（CloudWatch Logs API Reference「PutDeliverySource」）。

Security Hub の S3.9 と CloudTrail.7（CIS v3.0.0 3.4）は、S3 宛てのサーバーアクセスログを見る。

## Decision

1. 保管の Bucket のサーバーアクセスログを、`modules/vended-log-group` の Log Group へ配送する。delivery source と delivery は Bucket と同じ `internal/archive-bucket` に置く。
2. Log Group の保持日数は入力 `access_log_retention_days` で受け、Log Group は専用の鍵で暗号化する（audit-evidence/ADR-0201）。log-archive では Log Group に削除の保護を掛ける。
3. S3 宛てのログを持たないことを公式推奨からの逸脱として記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。Checkov の CKV_AWS_18 と KICS の f861041c を `internal/archive-bucket/main.tf` に限って抑止する。

## 検討した代替案

### 案A: SSE-S3 のログ用の Bucket を別に置く

`docs/v1.0-plan/scope.md` 第9.1節の SSE-S3 の例外を増やす。ログ用の Bucket 自身のログの置き場がまた要る。

### 案B: サーバーアクセスログを持たない

証跡の Bucket への読み出しと拒否された要求の記録を失う。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 中（SSE-S3） | 低 | 高 |
| Testability | 中 | 高 | 中（配送の到着は実環境） |
| Contract Clarity | 中 | 高 | 高 |
| Maintainability | 低 | 高 | 中 |
| Scope Control | 低 | 高 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

Security Hub の S3.9 と CloudTrail.7 の判定を PASSED にすること。

## 保証範囲

- 保証すること: delivery source が保管の Bucket を指し、log type が `S3_SERVER_ACCESS_LOGS` であり、delivery が入力の宛先へ配送すること
- 保証しないこと: ログが Log Group に届くこと（配送は best-effort で、届くまで数時間かかることがある）。log-archive の Log Group の削除の保護を、usecase のテストが確かめること（入れ子の module の属性をテストから参照できない）

## 検証方法

- Unit Test: `internal/archive-bucket/tests/access-log.tftest.hcl` が delivery source と delivery の値を mock の apply で確かめる
- Security: `make checkov kics` が CKV_AWS_18・f861041c を `internal/archive-bucket/main.tf` 以外の path で報告しないこと
- ログの到着は実環境で確かめる。どのテストも見ない

## 影響

Security Hub を有効にしたとき、S3.9 と CloudTrail.7 の所見が FAILED として残りうる。

## 見直し条件

- Security Hub と Checkov・KICS が CloudWatch Logs への配送を S3 のログとして認めたとき
