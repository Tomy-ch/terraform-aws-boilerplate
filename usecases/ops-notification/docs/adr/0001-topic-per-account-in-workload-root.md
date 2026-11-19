# ADR-0001: 通知の Topic を各アカウントの workload root に1つ置き、アカウントを越えて送らない

- Status: Accepted
- Date: 2026-11-19
- Scope: ops-notification
- Related: repository-wide/ADR-0604, repository-wide/ADR-0102, ops-notification/ADR-0101, ops-notification/ADR-0102

## Context

基盤系のアラームと検知の通知は、送り手（CloudWatch の alarm、EventBridge の rule、Budgets、Cost Anomaly Detection）と同じアカウントで生まれる。通知の Topic を1つのアカウントに集めると、他のアカウントからの publish を Topic Policy と Key Policy で許すことになり、送り手のアカウントが乗っ取られたときに集約先へ偽の通知を送れる。`docs/v1.0-plan/scope.md` 第9.3節は、通知先の Topic を各アカウントの `ops-notification` が1つずつ持ち、送れるのは同じアカウントの送り手だけとする（#97）。

state は apply する role ごとに root を分ける（repository-wide/ADR-0604 (state-split-by-apply-role) 決定2-5）。このユースケースは bootstrap の構成物ではないので、identity root ではなく workload root に置く。

## Decision

1. 1つのアカウントに Topic を1つ置き、そのアカウントの workload root から呼ぶ。
2. Topic へ publish できるのは、同じアカウントの AWS のサービス（ops-notification/ADR-0101 (fixed-publishers)）と、IAM で `sns:Publish` を与えた同じアカウントの role（心拍の role）だけとする。他のアカウントからの publish の口を持たない。
3. 宛先（Chatbot の Slack の channel）は、アカウントごとに構成する。7つのアカウントそれぞれで Slack の workspace の認可が要る。

## 検討した代替案

### 案A: セキュリティ用のアカウントに Topic を集め、各アカウントから送る

Topic Policy と Key Policy が他のアカウントの送り手を許す。送り手のアカウントの主体が集約先へ送れるようになり、`aws:SourceAccount` は送り手のアカウントを名指すだけで、乗っ取られたアカウントからの偽の通知を拒めない。

### 案B: 管理アカウントにだけ置く

管理アカウントの外の送り手の通知は、越境を要する。案A と同じ問題を持つ。

## 意図的に捨てるもの

- 通知の集約と、アカウントをまたいだ1つの channel への配送
- 乗っ取られたアカウントで通知が止められたときに、外から気づくこと（ops-notification/ADR-0102 (daily-heartbeat) の心拍は、来ないことに人が気づく形である）

## 保証範囲

Topic Policy が他のアカウントの主体と、`aws:SourceAccount` が自アカウントでない送り手を許さないことは、Policy Test（`PLAN-SNS-02`・`04`）が reference の plan で見る。

## 検証方法

`conftest/plan/sns_topic_policy.rego` の Rego のテストと、`make tf-policy` が reference の plan に掛ける `plan.sns_topic_policy`。

## 影響

各アカウントの workload root の apply が、Topic と鍵と心拍と Chatbot の構成を作る。env の配線は `env/<env>/` の workload root が持つ。

## 見直し条件

- 組織の外に通知の死活を見る仕組み（obp）が稼働したとき
- AWS が越境の送信に、送り手の主体を名指せる条件を提供したとき
