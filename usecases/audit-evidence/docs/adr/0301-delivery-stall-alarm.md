# ADR-0301: 保管の Bucket への書き込みが1時間途絶えたら、同じアカウントの通知の Topic へ知らせる

- Status: Accepted
- Date: 2026-11-19
- Scope: audit-evidence
- Related: repository-wide/ADR-0207, audit-evidence/ADR-0103

## Context

trail を止める、Bucket Policy で配信を拒む、鍵を止めるといった操作は、既に書かれた証跡を消さないが、以後の証跡を残させない。`docs/v1.0-plan/scope.md` 第9.3節は、通知のための metric alarm を、その metric を出す resource を持つ module が持ち、同じアカウントの通知の Topic へ送るとする。組織の trail は数分ごとに配信する。

S3 の request metrics は、要求が無い期間には metric を出さない。

## Decision

1. Bucket の `AWSLogs/` の下を数える request metrics と、`PutRequests` の1時間の合計が1未満なら発報する alarm を `internal/archive-bucket` に置く。
2. metric の欠落を途絶として読む（`treat_missing_data = "breaching"`）。
3. 3つの action（alarm・OK・insufficient data）に入力 `notify_topic_arn` の Topic だけを入れ、`actions_enabled` を有効にする。Topic は Bucket と同じアカウントとリージョンに限る。

## 検討した代替案

### 案A: alarm を持たず、Topic も受けない

証跡の途絶に誰も気づかない。`docs/v1.0-plan/scope.md` 第7節の基盤系アラームの通知先の要求から外れる。

### 案B: 途絶の検知を組織の trail の側に置く

途絶を観測できるのは書き込みを受ける Bucket で、metric を出す resource はこのユースケースが持つ。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 低 | 中 | 高 |
| Testability | 高 | 中 | 高 |
| Contract Clarity | 中 | 低 | 高 |
| Maintainability | 高 | 中 | 中（request metrics の費用） |
| Scope Control | 高 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

trail だけの途絶の検知。Config が書き込んでいる間は、trail が止まっても alarm は鳴らない。

## 保証範囲

- 保証すること: alarm の metric・期間・閾値・`treat_missing_data` が決定1・2 のとおりで、3つの action が入力の Topic だけを指すこと
- 保証しないこと: 実際の途絶で alarm が鳴ること。sandbox では書き手が居ないので alarm が鳴り続けること

## 検証方法

- Unit Test: `internal/archive-bucket/tests/alarm.tftest.hcl` が alarm の値を mock の apply で確かめる
- Policy Test: `PLAN-ALARM-01`（3つの action が同じ SNS の Topic 1つだけを指す）
- Security: `actions_enabled` は Checkov の CKV_AWS_319 が見る

## 影響

request metrics と alarm の月額が掛かる。

## 見直し条件

- trail だけの途絶を検知する要求が出たとき
- 書き手ごとに prefix を分けた metric が要るとき
