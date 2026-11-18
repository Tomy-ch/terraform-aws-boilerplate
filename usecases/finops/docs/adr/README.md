# Architecture Decision Records (finops)

`finops` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-monitor-dimension-by-account.md) | 予算と異常の検知を全アカウントに置き、monitor の次元を管理アカウントとメンバーアカウントで分ける | Accepted |

### 01 通知

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-notify-through-ops-notification.md) | 予算と異常の通知を同じアカウントの Topic 1つへだけ送り、予算を停止機構に使わない | Accepted |
