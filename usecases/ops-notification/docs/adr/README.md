# Architecture Decision Records (ops-notification)

`ops-notification` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-topic-per-account-in-workload-root.md) | 通知の Topic を各アカウントの workload root に1つ置き、アカウントを越えて送らない | Accepted |

### 01 送り手と死活

Topic へ何が届くか（送り手）と、届く経路が働いているか（心拍）を、Topic の入口の判断として1つの群に置く。

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-fixed-publishers.md) | Topic の送り手を budgets・cloudwatch・costalerts・events に固定し、入力にしない | Accepted |
| [0102](0102-daily-heartbeat.md) | 通知経路の死活を、毎日 09:00（Asia/Tokyo）の心拍で人が見る | Accepted |

### 02 宛先

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-chat-destination-read-only.md) | 通知の宛先を Chatbot の Slack の channel にし、guardrail と channel role を同じ読み取りの policy で絞る | Accepted |
