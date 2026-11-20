# ADR-0001: 通知の Topic を各アカウントの workload root に provider の region と us-east-1 の1つずつ置き、アカウントを越えて送らない

- Status: Accepted
- Date: 2026-11-19
- Scope: ops-notification
- Related: repository-wide/ADR-0604, repository-wide/ADR-0102, repository-wide/ADR-0208, ops-notification/ADR-0101, ops-notification/ADR-0102, ops-notification/ADR-0201

## Context

基盤系のアラームと検知の通知は、送り手（CloudWatch の alarm、EventBridge の rule、Budgets、Cost Anomaly Detection）と同じアカウントで生まれる。通知の Topic を1つのアカウントに集めると、他のアカウントからの publish を Topic Policy と Key Policy で許すことになり、送り手のアカウントが乗っ取られたときに集約先へ偽の通知を送れる。`docs/v1.0-plan/scope.md` 第9.3節は、通知先の Topic を各アカウントの `ops-notification` が1つずつ持ち、送れるのは同じアカウントの送り手だけとする（#97）。

state は apply する role ごとに root を分ける（repository-wide/ADR-0604 (state-split-by-apply-role) 決定2-5）。このユースケースは bootstrap の構成物ではないので、identity root ではなく workload root に置く。

いくつかの AWS のサービスは metric を us-east-1 にしか出さない（Route 53 の DNSSEC、CloudFront）。その alarm の通知先の Topic は alarm と同じ region に置く。CloudWatch の alarm が別の region の Topic を action に取れることを、AWS の文書は述べていない（repository-wide/ADR-0208 (us-east-1-resources-by-region-argument)）。

## Decision

1. 1つのアカウントに、provider の region の Topic を1つと、us-east-1 の Topic を1つ置き、そのアカウントの workload root から呼ぶ。us-east-1 の Topic は、us-east-1 にしか出ない metric の alarm の通知先であり、入力によらず常に置く。us-east-1 の Topic と鍵は resource の `region = "us-east-1"` で置き、`modules/sns`・`modules/kms` と同じ値を `internal/` に写す（repository-wide/ADR-0208 決定1・2）。us-east-1 は aws の partition の region なので、このユースケースは aws の partition の外では動かない（Chatbot の構成の region も同じ。ops-notification/ADR-0201 (chat-destination-read-only) 決定6）。
2. Topic へ publish できるのは、同じアカウントの AWS のサービス（ops-notification/ADR-0101 (fixed-publishers)）と、IAM で `sns:Publish` を与えた同じアカウントの role（心拍の role）だけとする。他のアカウントからの publish の口を持たない。
3. 宛先（Chatbot の Slack の channel）は、アカウントごとに構成する。7つのアカウントそれぞれで Slack の workspace の認可が要る。
4. 管理アカウント（`env = "admin"`）にも、ほかのアカウントと同じく2つの Topic・鍵・心拍・Chatbot の構成を置く。AWS Organizations User Guide「Best practices for the management account」は、管理アカウントに置くものを管理アカウントでしかできないものに限ることを勧める。この推奨から外れる。Topic は同じアカウントの送り手の宛先で（決定1・2）、その送り手と同じアカウントで生まれて消える。管理アカウントの送り手（心拍と、管理アカウントにしか置けない構成物の通知）の宛先を別のアカウントに置くと、決定2 が拒む越境の publish を要る。管理アカウントには SCP が効かないので、Topic・鍵・role の守りは Topic Policy・Key Policy・permissions boundary と、apply の role の権限の列挙が持つ。

## 検討した代替案

### 案A: セキュリティ用のアカウントに Topic を集め、各アカウントから送る

Topic Policy と Key Policy が他のアカウントの送り手を許す。送り手のアカウントの主体が集約先へ送れるようになり、`aws:SourceAccount` は送り手のアカウントを名指すだけで、乗っ取られたアカウントからの偽の通知を拒めない。

### 案B: 管理アカウントにだけ置く

管理アカウントの外の送り手の通知は、越境を要する。案A と同じ問題を持つ。

### 案C: us-east-1 の Topic を置かず、us-east-1 の alarm から provider の region の Topic へ送る

AWS の文書が述べない挙動に依存する。届くことを確かめる手段も無い。

### 案D: us-east-1 の Topic を、provider の region が us-east-1 でないときだけ置く

resource の有無が provider の region で分かれ、テストと Policy の宣言が2つの形になる。provider の region が us-east-1 の利用者で冗長な Topic が1つ増えるより、構造が一様であることを取る。

## 意図的に捨てるもの

- 通知の集約と、アカウントをまたいだ1つの channel への配送
- 乗っ取られたアカウントで通知が止められたときに、外から気づくこと（ops-notification/ADR-0102 (daily-heartbeat) の心拍は、来ないことに人が気づく形である）
- us-east-1 の metric を使わないアカウントで、us-east-1 の Topic と鍵の費用（鍵1つ）を払わないこと

## 保証範囲

Topic Policy が他のアカウントの主体と、`aws:SourceAccount` が自アカウントでない送り手を許さないことは、Policy Test（`PLAN-SNS-02`・`04`）が reference の plan で見る。

2つ目の Topic が us-east-1 に在ることと、Topic Policy・Key Policy が1つ目と同じ形であることは、Unit Test（`internal/us-east-1-topic/tests/`）と Policy Test（`PLAN-NOTIFY-*`、`PLAN-SNS-*`、`plan.kms_key_policy`）が見る。aws の partition の外で止まることは、Unit Test（`internal/us-east-1-topic/tests/validation.tftest.hcl`）が見る。

## 検証方法

`conftest/plan/sns_topic_policy.rego` の Rego のテストと、`make tf-policy` が reference の plan に掛ける `plan.sns_topic_policy`。

## 影響

各アカウントの workload root の apply が、Topic と鍵と心拍と Chatbot の構成を作る。env の配線は `env/<env>/` の workload root が持つ。

## 見直し条件

- 組織の外に通知の死活を見る仕組み（obp）が稼働したとき
- AWS が越境の送信に、送り手の主体を名指せる条件を提供したとき
- 管理アカウントの送り手の通知を、越境の publish なしに別のアカウントで受けられるようになったとき（決定4）
- CloudWatch の alarm が別の region の Topic を action に取れることを AWS が文書にしたとき（us-east-1 の Topic を外す）
