# ADR-0201: 通知の宛先を Chatbot の Slack の channel にし、guardrail と channel role を同じ読み取りの policy で絞る

- Status: Accepted
- Date: 2026-11-19
- Scope: ops-notification
- Related: repository-wide/ADR-0102, repository-wide/ADR-0204, repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0602, repository-wide/ADR-0604, ops-notification/ADR-0001, ops-notification/ADR-0102, deployment-identity/ADR-0002

## Context

Amazon Q Developer in chat applications（旧 AWS Chatbot。以下 Chatbot）は、Topic を購読して Slack の channel に通知を描く。channel の構成は、Chatbot が引き受ける channel role と、その上限を決める guardrail を持つ。guardrail を省くと AWS managed の `AdministratorAccess` が掛かる（AWS Provider の `aws_chatbot_slack_channel_configuration`、Chatbot API Reference の `CreateSlackChannelConfiguration`）。

Chatbot の文書は、通知だけを描く最小の policy として `AWS-Chatbot-NotificationsOnly-Policy`（`cloudwatch:Describe*`・`cloudwatch:Get*`・`cloudwatch:List*` を Resource `"*"` に許す）を示す（Administrator Guide「IAM policies」）。2026-11-19 の Service Authorization Reference では、この3つの部分 wildcard は約60の Action に当たり、alarm と metric の読み取りのほかに、`GetSpaceCredentials`・`GetRecords`（ログとトレースの取得）・`GetTelemetryQueryResults` などのデータへ届く読み取りを含む。

repository-wide/ADR-0301 (least-privilege-policy) 決定7 は、語彙（`read` など）の Action 集合をユースケースの ADR で定義し、Policy Test で固定するとする。決定8 は Allow の `service:*` と `*` だけを禁じ、部分 wildcard は禁じない。

Chatbot の API は us-east-2・us-west-2・eu-west-1・ap-southeast-1 にだけ在り、AWS Provider v6.70.0 はほかの region の client を us-west-2 へ寄せる（`internal/service/chatbot/service_package.go`）。

## Decision

1. 宛先は Chatbot の Slack の channel とし、1つのアカウントに構成を1つ置く（`internal/chat-slack`）。使い手はこのユースケースだけなので、一般 module にしない（repository-wide/ADR-0102 (use-case-centric-scope) 決定12・15）。1つの構成が、provider の region の Topic と us-east-1 の Topic の2つを購読する。Chatbot の文書は、1つの構成に複数の region の Topic を組み合わせられるとする。
2. `read` の Action 集合を次の8つと定義する。通知に添えられた alarm と metric を Chatbot が描くのに要る読み取りに限る。

   | Action | Resource |
   | --- | --- |
   | `cloudwatch:DescribeAlarms`・`cloudwatch:DescribeAlarmHistory`・`cloudwatch:ListTagsForResource` | `arn:<partition>:cloudwatch:<region>:<account>:alarm:*`（`<region>` は provider の region と us-east-1。resource-level の権限を持つ） |
   | `cloudwatch:DescribeAlarmsForMetric`・`cloudwatch:GetMetricData`・`cloudwatch:GetMetricStatistics`・`cloudwatch:GetMetricWidgetImage`・`cloudwatch:ListMetrics` | `"*"`（Service Authorization Reference が resource-level の権限を持たないとする。`GetMetricData`・`ListMetrics` の `dataset` は任意の resource type で、metric の読み取りには当たらない） |

   Resource `"*"` の5つは `conftest/plan/decl.json` の `resource_wildcard_actions` に置き、別の statement に分ける（repository-wide/ADR-0301 決定9）。
3. この集合を1つの customer managed policy にし、guardrail と channel role の両方に置く。channel role に付く policy と guardrail は同じ1つだけとする。`user_authorization_required = true` にする。AWS managed policy を使わない（repository-wide/ADR-0301 決定10）。
4. 決定2 は公式推奨からの逸脱である（下の表。repository-wide/ADR-0207 決定11）。
5. channel role の信頼は、`chatbot.amazonaws.com` の `sts:AssumeRole` を `aws:SourceAccount`（`StringEquals`、自アカウントの固定値）で限る1文とする。Chatbot の文書は信頼の例を持たず、principal も confused deputy の条件も示さない（confused deputy の頁は無い）。principal は Chatbot の慣行に依り、条件は IAM の一般の指針（cross-service confused deputy prevention）に従う。`aws:SourceArn` は、Chatbot が渡す ARN の形を示す一次資料が無いので置かない。この信頼で Chatbot が role を引き受けられることは、sandbox で心拍が Slack に届くことで確かめる。
6. 構成の `region` は `us-west-2` を明示する。Provider が寄せる region と宣言を一致させるためで、AWS の文書の推奨ではない。この宛先は、env の配線（#222 PR-B）で egress の宣言に足す。Topic は provider の region と us-east-1 に置き、構成は us-west-2 から2つを購読する。
7. `logging_level = "NONE"` にする。ログは Terraform の管理の外の Log Group に出る。
8. Teams はまだ持たない。別の resource 型（`aws_chatbot_teams_channel_configuration`）で、届くことを確かめる tenant が無い（repository-wide/ADR-0401 (testing-policy) 決定3）。入力 `chat` は `teams` を後から足せる形にする。
9. Chatbot の service-linked role（`AWSServiceRoleForAWSChatbot`）を、このユースケースは作らない。アカウントの前提の構成物として identity root が先に作る（repository-wide/ADR-0602 決定1、repository-wide/ADR-0604 決定3、deployment-identity/ADR-0002）。`internal/chat-slack` は、その role を data source（`aws_iam_role`）で名前で読み、path が `/aws-service-role/management.chatbot.amazonaws.com/` であることを postcondition で確かめ、Chatbot の構成の precondition からその data を参照して、読み取りを構成の作成より前に置く。role が無ければ plan で失敗する。data source の失敗は文言を変えられないので、修復の手順は README の「plan で落ちたときに見るもの」が持つ。`data "aws_iam_roles"`（path_prefix で無くても空で返る）は採らない。`iam:ListRoles` を Resource `"*"` で plan と apply の role に与えることになるからである。

### 公式推奨からの逸脱（repository-wide/ADR-0207 決定11）

| 対象 | 理由 | 影響（security / availability / cost） | 代替案 | 見直し条件 |
| --- | --- | --- | --- | --- |
| 公式の `AWS-Chatbot-NotificationsOnly-Policy` の `cloudwatch:Describe*`・`Get*`・`List*` を使わず、決定2 の8つに限る | 部分 wildcard は、alarm と metric の読み取りのほかに、ログ・トレース・space の資格情報などデータへ届く読み取りに当たる。通知を描く経路にデータの読み取りを混ぜない。このユースケースの制約は、channel role と guardrail が AWS の外（Slack）の利用者の操作に掛かり、Chatbot が描くのは基盤系の alarm と metric の通知だけであることにある | security は、Slack の利用者と Chatbot が読める範囲が alarm と metric に狭まる。availability は、Chatbot が描画に8つの外の Action を要るなら、その部分が描かれない（sandbox で確かめる）。cost は変わらない | 公式の policy を Service Authorization Reference で列挙に開いた全集合（約60）を使う | sandbox で通知の描画が欠けたとき。公式の policy が変わったとき。AWS Provider の major 更新時（repository-wide/ADR-0206 (official-implementations-as-reference) 決定12） |

## 検討した代替案

### 案A: 公式の policy の全集合を列挙する

公式と同じ範囲になり逸脱は無いが、`read` にデータへ届く読み取りが入る。

### 案B: `CloudWatchReadOnlyAccess`・`ReadOnlyAccess`（AWS managed）

AWS が Action を足すと黙って広がる。repository-wide/ADR-0301 決定10 の例外に当たらない。

### 案C: email の subscription

確認待ちの間は届かず、Terraform は確認待ちの subscription を destroy できず残し、メールのリンクから認証なしで解除され、宛先が PII として state に残る。どれも届くことを検査する手段が無い。責務外とする（README の unsupported）。

## 意図的に捨てるもの

- チャットからの書き込みの操作
- provider の region と us-east-1 の外の alarm を Chatbot が読むこと（alarm の Resource は2つの region に限る）

## 保証範囲

policy の文書、guardrail と channel role の一致、`user_authorization_required` は Unit Test（`internal/chat-slack/tests/`）と Policy Test（`PLAN-NOTIFY-06`・`07`）が見る。描画に要る Action が8つで足りることは保証しない（sandbox で確かめる）。service-linked role の存在と path は、data source の postcondition が plan で見る（`internal/chat-slack/tests/` が、別の path で落ちることを見る）。role が在っても Chatbot が購読を作ることは保証しない。

## 検証方法

`internal/chat-slack/tests/`（service-linked role の path の postcondition を含む）、`plan.ops_notification`、`plan.iam_action_wildcard`（`PLAN-IAM-03`・`06`）。

## 影響

Chatbot が描く通知の部分（metric の画像など）が8つの Action で足りなければ、集合を足す本 ADR の変更が要る。

## 見直し条件

- 決定2 の逸脱の見直し条件
- Chatbot が信頼の principal か confused deputy の条件を文書で示したとき
- 2つ目のユースケースが Chatbot の構成を要るとき（一般 module へ移す）
- Teams の tenant で届くことを確かめる手段ができたとき
- AWS が、Chatbot の構成の作成と同時の service-linked role の作成で購読が欠けないことを示したとき
