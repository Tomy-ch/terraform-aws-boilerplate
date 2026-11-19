# ADR-0202: 基盤系アラームの通知先を受けない

- Status: Accepted
- Date: 2026-11-19
- Scope: network-foundation
- Related: repository-wide/ADR-0101

## Context

基盤系アラームの通知先（`ops-notification` が出力する SNS Topic の ARN）は、各ユースケースの入力契約に含める規則がある。CloudWatch の metric alarm は、その metric を出す resource を持つ module が持つ。このユースケースの resource（VPC、subnet、Route Table、既定の Security Group、Flow Logs、配送 role、鍵、Log Group）のうち CloudWatch の metric を出すのは Log Group だけで、その流入の metric は、ENI の無い VPC（このユースケースだけを apply した直後と sandbox）では途絶えるのが平常である。Flow Logs の配送の失敗は metric にならない。

## Decision

1. 基盤系アラームの通知先の Topic の ARN を入力に取らない。alarm を持たない。
2. 使われない入力は置かない。「念のため」という理由だけで設定項目を足さない（repository-wide/ADR-0101 (architecture-principles) 決定8 の17）。
3. 通知先の規則の除外に、このユースケースを足す。理由は「通知に値する metric を出す resource を持たない」である。

## 検討した代替案

### 案A: 入力だけ受けて使わない

使われない入力になる。値を渡す env の側に、何も起こらない配線が残る。

### 案B: Log Group の流入の途絶に alarm を置く

途絶が平常な環境で鳴り続ける。鳴らない側に倒すと何も検知しない。alarm を置く先は `modules/log-group` になり、全呼び手に Topic の入力が増えるか、入力で alarm の有無を切り替えることになる。

## 意図的に捨てるもの

- VPC の基盤の異常の通知（このユースケースには対象の metric が無い）

## 保証範囲

- 入力に Topic の ARN が無いことは `variables.tf` が持つ。

## 検証方法

- `tests/contract.tftest.hcl` が公開の出力を固定する。入力の一覧は README の公開契約が持つ。

## 影響

- `ops-notification` の実装の後も、このユースケースの env の配線に Topic の ARN は要らない。

## 見直し条件

- VPC か subnet の状態が CloudWatch の metric として出るようになったとき（たとえば subnet の空き IP）。
- Flow Logs の配送の失敗が metric になったとき。
