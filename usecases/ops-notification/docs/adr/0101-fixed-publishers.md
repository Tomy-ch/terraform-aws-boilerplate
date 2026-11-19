# ADR-0101: Topic の送り手を budgets・cloudwatch・costalerts・events に固定し、入力にしない

- Status: Accepted
- Date: 2026-11-19
- Scope: ops-notification
- Related: repository-wide/ADR-0201, repository-wide/ADR-0207, repository-wide/ADR-0301, ops-notification/ADR-0001

## Context

`modules/sns` の `publishers` と `modules/kms` の `users.services.sns` は、Topic と鍵へ publish する AWS のサービスを受ける。暗号化した Topic へ publish するには、Topic Policy と Key Policy の両方が送り手を許す必要があり、片方を欠くと通知は黙って拒まれる。

`docs/v1.0-plan/scope.md` 第9.3節は、alarm を metric を出す resource の module が持ち、CloudWatch からの publish を Topic と鍵が受けることを `ops-notification` が持つとする。alarm を持つ module はどの usecase にも現れ、Topic の側は送り手が増えたことを知らない。`aws:SourceAccount` の条件は plan の後でなければ評価されないので、alarm を持つ側が「Topic が自分を受けるか」を plan で確かめる手段も無い。

使う送り手は、CloudWatch（alarm）、EventBridge（GuardDuty の検知などの rule）、Budgets と Cost Anomaly Detection（`finops`）である。Cost Anomaly Detection は `costalerts.amazonaws.com` が SNS の購読として送る（AWS Cost Management User Guide「Creating an Amazon SNS topic for anomaly notifications」）。

## Decision

1. 送り手を `budgets`・`cloudwatch`・`costalerts`・`events` の4つに固定し、公開契約の入力にしない。
2. `modules/sns` の `publishers` と `modules/kms` の `users.services.sns[*].publishers` に、同じ固定の集合を渡す。
3. 送り手の居ないアカウントでも同じ4つを許す。広がるのは、同じアカウントの4つのサービスが Topic へ送れることだけである（`aws:SourceAccount` の固定値。EventBridge は暗号化コンテキストで Topic を名指す）。
4. Topic Policy と Key Policy の送り手の一致は Policy Test（`PLAN-NOTIFY-01`・`02`）が見る。

## 検討した代替案

### 案A: `publishers` を入力にし、env が送り手を足す

alarm を持つ module を足す変更が env の `publishers` の更新を忘れると、alarm は鳴っても通知が Topic Policy と Key Policy で黙って拒まれる。このユースケースの最悪の故障と同じ形であり、それを plan で検出する手段が無い。

### 案B: `cloudwatch` を、alarm を持つ module が現れたアカウントだけで足す

案A と同じ理由で、足すのを忘れた変更を検出できない。

## 意図的に捨てるもの

- 送り手の居ないアカウントで、使われない許可を持たないこと

## 保証範囲

固定の集合は `tests/composition.tftest.hcl` が、Topic Policy と Key Policy の送り手が宣言の集合と一致することは Policy Test が見る。実際に4つの経路が拒まれずに届くことは、sandbox の Integration Test で人が確かめる（README の「検証方法」）。

## 検証方法

`tests/composition.tftest.hcl`、`plan.ops_notification`（`PLAN-NOTIFY-01`・`02`）。

## 影響

送り手を足すには、このユースケースと `modules/sns`・`modules/kms` の語彙の変更が要る。

## 見直し条件

- 4つのほかに、同じアカウントのサービスから Topic へ送る送り手が要るとき
- EventBridge の送信が暗号化コンテキストの条件で拒まれると sandbox で分かったとき（送り手の usecase が role を持つ形へ移る）
