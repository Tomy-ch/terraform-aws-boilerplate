# ADR-0101: 予算と異常の通知を同じアカウントの Topic 1つへだけ送り、予算を停止機構に使わない

- Status: Accepted
- Date: 2026-11-19
- Scope: finops
- Related: repository-wide/ADR-0204, repository-wide/ADR-0207, repository-wide/ADR-0402, finops/ADR-0001

## Context

通知の Topic は各アカウントの `ops-notification` が持ち、通知の経路の死活は Topic への毎日の心拍で見る。Budgets の通知の宛先は email か SNS の Topic、Cost Anomaly Detection の subscription の宛先は email か SNS の Topic で、SNS で受けられるのは即時の通知（`IMMEDIATE`）だけである。subscription が持てる Topic は1つである。

Budgets の課金データの反映には数時間から1日の遅れがある。予算の action は、予算の超過で IAM や SCP の policy を付け、EC2 や RDS を止める。

予算の通知の閾値と、Cost Anomaly Detection の通知の閾値には、AWS の文書が示す推奨値が無い。コンソールの予算のテンプレートは「超過したとき、または超過が予測されるとき」に通知すると書くが、値を示さない。

## Decision

1. 予算と異常の通知は、同じアカウントの `ops-notification` の Topic 1つへだけ送る。email の宛先を持たない。
2. Cost Anomaly Detection の subscription の頻度は `IMMEDIATE` にする。
3. 予算の action を持たない。予算は消し残しの検知に使い、止める仕組みにしない。
4. 予算の通知は、実績が予算の85%と100%を超えたときと、予測が100%を超えたときの3つに固定する。
5. 異常の通知は、影響額が入力の額以上で、かつ期待値に対して40%以上のときに送る。40% は固定する。
6. 決定4・5 の値は、明確な公式推奨が無いので独自判断とする（repository-wide/ADR-0207 (default-value-policy) 決定3 の6・決定4）。値の選び方は次のとおり。
   - 実績の85%: 超過の前に1度知らせ、月の残りの日数で手を打てる段を置く
   - 実績の100%: 超過そのもの
   - 予測の100%: 月末を待たずに、超過の見込みを知らせる。予測には約5週間の利用データが要るので、新しいアカウントでは実績の2つだけが働く
   - 異常の40%: 金額の閾値だけでは、費用の大きいアカウントで期待値に比べて小さな揺れも通知になる。期待値の40%に満たない上振れを日々の揺れとして落とすために、割合を AND で重ねる

## 検討した代替案

### 案A: email の宛先を入力で受ける

宛先が通知の経路の死活の監視の外に出て、個人の宛先が state に入る。Cost Anomaly Detection の日次・週次の要約は email でしか受けられない。

### 案B: 通知の閾値と式を入力で受ける

予算の通知の並びと、異常の閾値の式（`threshold_expression`）は、Provider と API の表現である（repository-wide/ADR-0204 (no-generic-escape-hatch)）。利用者に設定の方法を選ばせることになる（repository-wide/ADR-0207 (default-value-policy) 決定7・9）。

### 案C: 予算の action で止める

止める権限を持つ role が要り、反映の遅れのため止まる時点が予算の超過から1日ずれる。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 低（宛先が state に入る） | 中 | 低（止める権限） | 高 |
| Testability | 中 | 低 | 低 | 高 |
| Contract Clarity | 中 | 低 | 中 | 高 |
| Maintainability | 中 | 低 | 低 | 高 |
| Scope Control | 低（死活の外の経路） | 低 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 日次・週次の異常の要約
- 閾値と通知の並びを利用者が選ぶこと
- 予算の超過で自動的に止めること

## 保証範囲

- 保証すること: 予算の通知と異常の subscription の宛先が、同じ Topic 1つで、email を持たないこと。subscription の頻度が `IMMEDIATE` であること。入力の plan に予算の action の作成と更新が無いこと。決定4・5 の値。Topic が予算と同じ partition・アカウントにあること（resource の precondition が plan で落とす）
- 保証しないこと: 越境の防止を Policy Test で見ること。Policy の入力は呼び出し元のアカウントを持たないので、precondition と Unit Test だけが見る。通知が Topic に届くこと（Topic の policy と鍵の policy は `ops-notification` が持つ。Budgets が暗号化した Topic へ送るための鍵の policy の文例を AWS は示していない）。予算の超過から通知までの時間

## 検証方法

- Unit Test: `internal/budget/tests/budget.tftest.hcl` が予算の通知の3つの閾値と宛先の導出を、`internal/cost-anomaly/tests/anomaly.tftest.hcl` が subscription の宛先と閾値の導出を mock の plan で完全一致で確かめる。両方の `tests/` が、別の partition・アカウントの Topic を precondition が落とすことを確かめる
- Policy Test（`make tf-policy`）: `PLAN-FINOPS-01` が予算の action の作成と更新を、`PLAN-FINOPS-02` が通知を持たない予算と、通知の email の宛先と Topic の数を、`PLAN-FINOPS-03` が subscription の宛先と頻度を、`PLAN-FINOPS-05` が予算と subscription の Topic の食い違いと、usecase の instance ごとに予算と subscription が1つずつ揃わない入力を落とす。02・03 は plan で決まらない値も落とす。`PLAN-FINOPS-00` は予算と subscription が入力に無ければ落ちる

## 影響

`ops-notification` の Topic の policy と鍵の policy は、`budgets.amazonaws.com` と `costalerts.amazonaws.com` の送信を許す必要がある。

## 見直し条件

- AWS が予算の通知の閾値か、異常の通知の閾値の推奨値を示したとき
- Cost Anomaly Detection が SNS で要約を送れるようになったとき
- 予算の超過で止める必要が、ユースケースの要件として現れたとき
