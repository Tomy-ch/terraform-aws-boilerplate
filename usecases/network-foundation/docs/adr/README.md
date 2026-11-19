# Architecture Decision Records (network-foundation)

`network-foundation` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-one-vpc-per-environment-account.md) | 各環境のアカウントに VPC を1つ置き、sandbox では常駐させない | Accepted |

### 01 トポロジ

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-two-tiers-and-fixed-cidr-slots.md) | subnet を private と isolated の2つの tier に分け、CIDR を固定の slot で割り付ける | Accepted |
| [0102](0102-routes-owned-by-network-foundation.md) | Route Table の経路をこのユースケースだけが inline で排他に持つ | Accepted |

### 02 観測

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-flow-logs-encrypted-with-own-key.md) | Flow Logs をこのユースケースの鍵で暗号化し、配送 role の DescribeLogGroups だけを Resource "*" にする | Accepted |
| [0202](0202-no-alarm-topic.md) | 基盤系アラームの通知先を受けない | Accepted |
