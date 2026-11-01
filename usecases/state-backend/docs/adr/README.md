# Architecture Decision Records (state-backend)

`state-backend` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-management-account-placement.md) | 管理アカウント自身の state Bucket を管理アカウントに置く | Accepted |

### 01 state の保護

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-sse-s3-until-cmk.md) | bootstrap の段5 まで state Bucket を SSE-S3 で暗号化する | Accepted |
| [0102](0102-no-mfa-delete-or-object-lock.md) | state Bucket に MFA delete と Object Lock を掛けない | Accepted |

### 02 plan の出力

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-plan-output-bucket.md) | CI の plan の出力を、保持日数で消える専用の Bucket に置く | Accepted |
