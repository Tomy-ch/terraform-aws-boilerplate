# Architecture Decision Records (security-tooling)

`security-tooling` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-security-account-delegated-admin.md) | 組織の検知を security のアカウントの委任管理者で持ち、experimental で始める | Accepted |

### 01 組織の trail と通知

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-organization-trail.md) | 組織の trail を、管理イベントの読み書きを全リージョンで記録し、audit-evidence と CloudWatch Logs へ送る形で置く | Accepted |
| [0102](0102-break-glass-and-root-usage-alarms.md) | break-glass・root・`sts:AssumeRoot` の使用を、組織の trail の Log Group の metric filter と alarm で通知する | Accepted |
