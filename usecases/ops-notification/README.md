# ops-notification

1つのアカウントに、基盤系の通知を受ける暗号化した SNS の Topic を provider の region と us-east-1 に1つずつ、その2つを購読する Amazon Q Developer in chat applications（旧 AWS Chatbot。以下 Chatbot）の Slack の channel の構成を1つ、通知経路の死活を見る毎日の心拍（EventBridge Scheduler）を1つ持つユースケース。各アカウントの workload root から呼ばれる常駐層である（[ops-notification/ADR-0001](docs/adr/0001-topic-per-account-in-workload-root.md)、[scope.md 第9.3節・第10.2節](../../docs/v1.0-plan/scope.md)）。

## 解決する問題

基盤系のアラームと検知（CloudWatch の alarm、EventBridge の rule、Budgets、Cost Anomaly Detection）は、同じアカウントの通知先へ送られ、人が読める場所へ届かなければ意味を持たない。通知先はアカウントを越えない（#97）。基盤系のアラームは平常時に1通も来ないので、経路が壊れても気づけない。このユースケースは、通知先の Topic と宛先を持ち、毎日の心拍で経路が働いていることを人に見せる。

## 実行形態とネットワーク境界

- AWS の上で動く処理は、Scheduler の schedule（Topic への publish）と、Chatbot（Topic の購読と Slack への配送）だけである。コードを持たない
- Topic は provider の region に置き、TLS でない publish を Topic Policy の Deny で拒む。鍵は customer managed key（`modules/kms`）
- us-east-1 にしか出ない metric（Route 53 の DNSSEC、CloudFront）の alarm の通知先として、同じ形の Topic と鍵を us-east-1 に置く。resource の `region = "us-east-1"` で置き、provider を増やさない（[ops-notification/ADR-0001](docs/adr/0001-topic-per-account-in-workload-root.md) 決定1、[repository-wide/ADR-0208](../../docs/adr/0208-us-east-1-resources-by-region-argument.md)）
- Chatbot の構成は us-west-2 に置く（Chatbot の API の region。[ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md) 決定6）。通知は AWS の外（Slack）へ出るので、心拍の本文にアカウント ID と ARN を載せない
- provider の region の Topic へ publish できるのは、同じアカウントの4つのサービスと心拍の role だけで、us-east-1 の Topic へ publish できるのは、同じアカウントの CloudWatch と心拍の role だけである

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字、37文字以内） | 名前の先頭の token。最も長い名前は role 名 `<project>-log-archive-ops-heartbeat`（63文字） |
| `env` | `admin` / `security` / `log-archive` / `sandbox` / `dev` / `stg` / `prd` | Topic を置くアカウントの env の token（`state-backend`・`deployment-identity` と同じ token） |
| `chat` | `object({ slack = object({ team_id = string, channel_id = string }) })` | 通知を届ける Slack の workspace（Chatbot で認可済み）の ID と channel の ID。名前ではなく ID を受ける（Chatbot API Reference の `SlackTeamId`・`SlackChannelId` の形） |
| `permissions_boundary_arn` | `string`（同じアカウントの customer managed policy の ARN） | このユースケースが作る IAM role（心拍・channel）のすべてに付ける permissions boundary。policy は `deployment-identity` が持つ。この入力は IAM の表現を素通しする設定口ではなく、「作る role の権限の上限」を名指す識別子である。値は同じアカウントの customer managed policy の ARN に検証で限り、policy の内容は受けない（[ADR-0204](../../docs/adr/0204-no-generic-escape-hatch.md) 決定2 の Generic Escape Hatch に当たらない） |

4つはどれも既定値を持たない必須入力である。

| 出力 | 使う側 |
| --- | --- |
| `topic_arn` | 同じアカウントの送り手を持つユースケース（alarm を持つ module、`security-tooling` の検知の rule と alarm、`finops` の Budgets と Cost Anomaly Detection）。env の root が渡す |
| `us_east_1_topic_arn` | us-east-1 にしか出ない metric の CloudWatch の alarm を持つユースケース（Route 53 の DNSSEC の alarm など）。送り手は CloudWatch だけ。env の root が渡す |

- ログの所在: このユースケースはログを持たない。Chatbot の `logging_level` は `NONE`、SNS の配送状況のログ（delivery status logging）も持たない。API の呼び出しの記録は CloudTrail が持つ
- 基盤系アラームの通知先 Topic ARN: 受けない。このユースケース自身がその Topic を持つ（[scope.md 第7節](../../docs/v1.0-plan/scope.md)）

## 持つもの

| 構成物 | 置き場 | 値 |
| --- | --- | --- |
| 鍵（CMK・Key Policy・alias `alias/<project>-<env>-ops-notification`） | `modules/kms` | 心拍の role（`via = "sns"`）と、4つの送り手のサービス principal に暗号操作を許す |
| Topic `<project>-<env>-ops-notification` と Topic Policy | `modules/sns` | 送り手は `budgets`・`cloudwatch`・`costalerts`・`events` に固定（[ops-notification/ADR-0101](docs/adr/0101-fixed-publishers.md)） |
| us-east-1 の鍵（alias `alias/<project>-<env>-ops-notification-us-east-1`）と Topic `<project>-<env>-ops-notification-us-east-1`・Topic Policy | `internal/us-east-1-topic` | `modules/kms`・`modules/sns` と同じ値。送り手は `cloudwatch` だけ（[ops-notification/ADR-0101](docs/adr/0101-fixed-publishers.md)）。AWS が us-east-1 に固定した metric の alarm のためで、使わないアカウントにも置く |
| 心拍（schedule group と schedule を provider の region に `<project>-<env>-ops-heartbeat`、us-east-1 に `<project>-<env>-ops-us-east-1`・role `<project>-<env>-ops-heartbeat`） | `internal/heartbeat` | 毎日 09:00（`Asia/Tokyo`）に2つの Topic へ1通ずつ。本文の region でどちらかを示す。role は2つの Topic への `sns:Publish` だけ（[ops-notification/ADR-0102](docs/adr/0102-daily-heartbeat.md)） |
| Chatbot の Slack の channel の構成・channel role `<project>-<env>-ops-chat`・読み取りの policy `<project>-<env>-ops-chat-read` | `internal/chat-slack` | 2つの Topic を購読する。guardrail と channel role に同じ読み取りの policy（[ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md)） |

`modules/sns` の `name`・`publishers` と、`modules/kms` の `users.services.sns` の `topic`・`publishers` には同じ値を渡す（`main.tf` の `local.publishers`）。両者の一致は Policy Test（`PLAN-NOTIFY-01`）が見る。us-east-1 の Topic と鍵の送り手は `internal/us-east-1-topic` の文書が持ち、宣言の `us_east_1.publishers` との一致を同じ規則が見る。

## 人が先に行うこと

- Slack の workspace を、そのアカウントの Chatbot のコンソールで認可し、team ID と channel ID を env の値に書く。認可は OAuth で、コマンドにできない。7つのアカウントそれぞれで要る。workspace の持ち主と、通知を見る channel は利用者が決める。手順は [`docs/setup/ops-notification.md`](../../docs/setup/ops-notification.md)
- 認可の前に apply すると、Chatbot の構成の作成が失敗する見込みである（確かめていない）
- 同じアカウントの identity root を先に apply する。Chatbot の service-linked role（`AWSServiceRoleForAWSChatbot`）は identity root の `deployment-identity` が作り、このユースケースは作らずに名前で読む。無ければ plan で落ちる（[ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md) 決定9）

## supported / unsupported

supported:

- 1つのアカウントに、provider の region の Topic 1つと us-east-1 の Topic 1つ・Slack の channel の構成1つ・心拍1組を置き、同じアカウントの4つの送り手の通知と、us-east-1 の CloudWatch の alarm の通知を Slack へ届ける構成

unsupported:

- email の subscription。確認待ちの間は届かず、Terraform は確認待ちの subscription を destroy できず AWS に残し（AWS Provider の `aws_sns_topic_subscription` の NOTE）、メールのリンクから認証なしで解除されて黙って止まり、宛先が PII として state と plan に残る。どれも届くことを検査する手段が無い（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定3）。見直し条件: SNS が email の購読の認証つきの解除を API で設定できるようになったとき
- アカウントを越えて通知を送ること・集約すること（[ops-notification/ADR-0001](docs/adr/0001-topic-per-account-in-workload-root.md)）
- 乗っ取られたアカウントで通知が止められたことに、外から気づくこと（#97 が受け入れた弱点）
- 心拍の不在の自動検知。見直し条件: アカウントの外の仕組み（obp）が稼働したとき
- アプリ系のアラートの経路（Grafana Alerting）の死活。obp が持つ（[scope.md 第9.3節](../../docs/v1.0-plan/scope.md)）
- チャットからの書き込みの操作（[ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md)）
- Chatbot の service-linked role を、このユースケースが作ること。identity root が先に作る（[ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md) 決定9、[deployment-identity/ADR-0002](../deployment-identity/docs/adr/0002-account-prerequisite-service-linked-roles.md)）
- provider の region と us-east-1 の外の alarm を Chatbot が読むこと（`PLAN-NOTIFY-07`、`internal/chat-slack/tests/`）
- aws の外の partition。us-east-1 は aws の partition の region で、Chatbot の構成も us-west-2 に置く（[ops-notification/ADR-0001](docs/adr/0001-topic-per-account-in-workload-root.md) 決定1）。`internal/us-east-1-topic` の precondition が plan で止める
- us-east-1 の Topic への CloudWatch のほかの送り手（[ops-notification/ADR-0101](docs/adr/0101-fixed-publishers.md)）
- metric alarm と composite alarm。alarm は metric を出す resource を持つ module が持つ（[scope.md 第9.3節](../../docs/v1.0-plan/scope.md)「アラームの置き場」）

まだ持たないもの（契約への一般化の候補）:

| 足すもの | 理由 |
| --- | --- |
| Microsoft Teams の channel（`chat.teams`） | 別の resource 型で、届くことを確かめる tenant が無い。`chat` は optional の属性として足せる形にしてある |
| 送り手の追加 | 4つのほかに同じアカウントの送り手が要るとき（[ops-notification/ADR-0101](docs/adr/0101-fixed-publishers.md)） |

## 所有者

- Topic・鍵・心拍・Chatbot の構成はこのユースケースが所有する
- Slack の workspace の認可は Terraform の管理の外で、利用者が持つ
- permissions boundary の policy は `deployment-identity` が持つ
- Chatbot の service-linked role は `deployment-identity` が持つ。このユースケースは識別子を受けず、data source（`aws_iam_role`）で名前で読む

## 依存するユースケースと判断 Issue

- ユースケース: `deployment-identity` の permissions boundary の ARN を受ける。workload root の apply の role がこのユースケースの resource と role を作る権限は、`deployment-identity` が持つ（[deployment-identity/ADR-0201](../deployment-identity/docs/adr/0201-enumerated-permissions.md) 決定13-17）。Chatbot の service-linked role は `deployment-identity` が identity root で先に作り、このユースケースはその存在を前提にする（[deployment-identity/ADR-0002](../deployment-identity/docs/adr/0002-account-prerequisite-service-linked-roles.md)）
- 判断 Issue: #97（アカウントごとの Topic と心拍）、#109（alarm を持たない）、#110（`modules/sns`・`modules/kms` の SNS の文）、#256（宛先を Chatbot にする）、#254（作る role の範囲。permissions boundary）
- env の配線: 管理アカウントは workload root（`env/admin/`）が呼ぶ。security・log-archive は #21、dev・stg・prd は #330 が配線する

## 検証方法

- Unit Test（mock の plan と apply）: `tests/validation.tftest.hcl` が入力の検査を、`tests/contract.tftest.hcl` が `topic_arn`・`us_east_1_topic_arn` の配線を、`tests/composition.tftest.hcl` が名前の組み立てと固定の送り手と鍵の利用者を、`tests/us-east-1-provider.tftest.hcl` が provider の region が us-east-1 のときに名前が衝突しないことを、`internal/us-east-1-topic/tests/` が us-east-1 の Topic と鍵の region・値・文書と aws の partition の外で止まることを、`internal/heartbeat/tests/` が2つの schedule の固定値・本文・role の信頼と権限を、`internal/chat-slack/tests/` が Chatbot の構成・読み取りの policy・channel role と、service-linked role の path の postcondition と、アカウントと partition と `aws-service-role/` の path の precondition が別の実体で plan を落とすことを、`reference/tests/plan.tftest.hcl` が reference の値を見る。管理アカウントの workload root が渡す permissions boundary の ARN は `env/admin/tests/` が見る。Topic と鍵の文書は `modules/sns/tests/`・`modules/kms/tests/` が見る
- Policy Test（`make tf-policy`）: `reference/tests/policy-input.tftest.hcl` の plan に対して、Topic Policy の形（`plan.sns_topic_policy`。TLS の Deny、Allow がサービス principal の `sns:Publish` だけで条件が自アカウントの `aws:SourceAccount` だけ、Topic ごとに Topic Policy が1つ）、合成の不変条件（`plan.ops_notification`。Topic Policy と Key Policy の送り手の一致、EventBridge の文が Topic を名指すこと、IAM で publish する role と Key Policy の SNS 経由の role の一致、2つの role の信頼、心拍の権限と target、guardrail と channel role の policy の一致、`read` の Action 集合、channel role の managed policy の排他と attachment の対（`plan.iam_role_attachment_pairing`）、作る role が permissions boundary を持つこと、us-east-1 の構成物の region）、Key Policy の形（`plan.kms_key_policy`。SNS の文を plan から導いた入力で見る）、Resource `"*"` の Action（`plan.iam_action_wildcard`）を確かめる。Topic と鍵の削除と置換、`aws_sns_topic` と `aws_kms_key` の inline の `policy` は、reference の plan で `plan.input`（`PLAN-INPUT-09`）が、CI の plan と apply で `plan.stateful_protection` が見る
- Static Analysis: `make layer-check`・`make infra-code-gate`・`make resource-order-check`
- 実環境テストの形: [ADR-0401](../../docs/adr/0401-testing-policy.md) 決定14 の「常駐する実体を持つが、同じ構成の複製を作れる」に当たる。`tests/integration/sandbox.tftest.hcl` が、実行の識別子の接頭辞 `it` を持つ `project`（`tests/integration/guard` が検証し、外れたら複製を作る前に止まる）で sandbox に複製を作り、確かめてから破棄する。実行は人が行い、CI と `make tf-test` は走らせない。複製が在る間に、人が次を確かめる: 心拍の role・CloudWatch の alarm（`set-alarm-state`）・EventBridge の rule・Budgets・Cost Anomaly Detection からの publish が Topic Policy と Key Policy で拒まれずに Slack まで届くこと、us-east-1 の alarm（`set-alarm-state`）と us-east-1 の心拍が us-east-1 の Topic を経て Slack まで届くこと、us-east-1 の構成物が `region` 引数で us-east-1 に作られること、TLS でない publish が拒まれること、心拍の本文の予定時刻が置換されること、Chatbot の描画が `read` の8つの Action で欠けないこと、Security Hub の SNS.4 の判定。期待残存（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定16）は、削除の猶予に入った鍵（鍵の ID・`PendingDeletion`・削除の日付）と、Slack の workspace の認可（team ID。Terraform の管理の外）である

destroy の失敗と残存（Chatbot の構成の削除で外れない SNS の購読を含む）を検出する手段が無い（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定16）。手段ができるまで（#76）と、sandbox のアカウント（#21）が無い間は、sandbox の Integration Test を実行しない。その間に検出できない故障モード（[ADR-0401](../../docs/adr/0401-testing-policy.md) 決定17）:

- destroy の失敗と、期待残存の外の残存（Chatbot の購読を含む）
- 4つのサービスと心拍の role からの publish が、Topic Policy か Key Policy で拒まれること。とくに EventBridge は Key Policy を暗号化コンテキストで、Topic Policy を `aws:SourceAccount` で限っており、どちらも一次資料に例が無い
- Chatbot が別の region の暗号化した Topic を購読できないこと（us-west-2 の構成が provider の region と us-east-1 の2つを購読する）、描画に `read` の外の Action を要ること
- us-east-1 の alarm と心拍の publish が、us-east-1 の Topic Policy か Key Policy で拒まれること
- `region` 引数で置いた us-east-1 の構成物が、実際には us-east-1 に作られないこと
- `<aws.scheduler.scheduled-time>` が SNS の templated target で置換されないこと
- 作成直後の role を名指す `PutKeyPolicy` が IAM の結果整合で落ちること

### 心拍が見る故障（設計の意図）

次の故障では心拍が Slack に届かなくなる、という設計の意図である。故障を注入して確かめてはいない。

| 故障 | 心拍への現れ |
| --- | --- |
| Topic の削除、鍵の無効化・削除の予約、Key Policy から心拍の role の文が欠けること | publish が失敗し、届かない |
| Chatbot の構成の削除、購読の解除、Slack の認可の解除、channel の削除 | 配送されず、届かない |
| schedule の停止、心拍の role の権限の欠落 | publish されず、届かない |
| us-east-1 の Topic・鍵・Key Policy・schedule の同じ故障と、Chatbot の us-east-1 の Topic の購読の解除 | us-east-1 の心拍（本文の region が `us-east-1`）だけが届かない |

心拍が見ない故障: 4つのサービスの経路（Topic Policy と Key Policy の各送り手の文）だけが壊れること、Chatbot が alarm や GuardDuty の event を描けないこと、Topic Policy の TLS の Deny の欠落。これらは Policy Test が宣言を、sandbox の Integration Test が作った時点を見る。実行時の変化は、`terraform-drift.yaml` の定期の plan（管理アカウントの workload root）が見る。

### 心拍が来なかったときに見るもの

AWS Well-Architected Framework（2024-06-27 版）の OPS10-BP02「Have a process per alert」に当たる手順。通知の channel を見る人が、次の順に確かめる。

来なかった心拍の本文の region で、どちらの Topic かを分ける。provider の region の心拍なら schedule `<project>-<env>-ops-heartbeat` と provider の region の Topic と鍵を、us-east-1 の心拍なら us-east-1 の schedule `<project>-<env>-ops-us-east-1` と us-east-1 の Topic と鍵を見る。

1. Scheduler の schedule が `ENABLED` で、CloudWatch の `AWS/Scheduler` の `TargetErrorCount`・`InvocationDroppedCount` が増えていないか
2. Topic の `NumberOfMessagesPublished` と `NumberOfNotificationsFailed`
3. Topic に Chatbot の購読（endpoint `https://global.sns-api.chatbot.amazonaws.com`）があるか。`NumberOfMessagesPublished` が増えていて、`NumberOfNotificationsDelivered` と `NumberOfNotificationsFailed` が増えていなければ、購読が無い。付け直しは [`docs/setup/ops-notification.md`](../../docs/setup/ops-notification.md) の 3.4
4. Chatbot の構成と、Slack の workspace の認可
5. 鍵の状態と Key Policy の `RolesViaSns0`

### plan で落ちたときに見るもの

Chatbot の service-linked role の読み取りで plan が落ちたとき（[ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md) 決定9）。data source の読み取りの失敗は文言を変えられないので、ここに手順を置く。

1. `reading IAM Role (AWSServiceRoleForAWSChatbot)` が `NoSuchEntity` で落ちた: そのアカウントの identity root がまだ apply されていない。同じアカウントの identity root を apply し（CI の apply-identity の job。新しいアカウントでは bootstrap の段4）、workload root の plan をやり直す。destroy の plan（sandbox の Integration Test の破棄を含む）も同じ文言で止まる
2. path の postcondition が落ちた: `AWSServiceRoleForAWSChatbot` という名前の、service-linked role でない role が在る。その role を消し、identity root を apply する
3. Chatbot の構成の precondition（service-linked role のアカウントと partition）が落ちた: data source が返した ARN が、provider のアカウントと partition の role ではない。provider の設定（assume する role と region）を確かめる

role を CLI やコンソールで作らない。identity root の apply が名前の重複で落ちる。作ってしまったら、identity root の `import` block で取り込む（[deployment-identity/ADR-0002](../deployment-identity/docs/adr/0002-account-prerequisite-service-linked-roles.md) 決定4）。

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-19、AWS Provider v6.70.0 の文書。us-east-1 の Topic は 2026-11-21、AWS Provider v6.73.0 の文書で確かめた（下の us-east-1 の項）

- AWS / HashiCorp が publisher の Chatbot・Scheduler の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=chatbot&provider=aws&limit=50`・`q=scheduler` の結果に namespace `aws-ia`・`hashicorp`・`aws`・`aws-samples` が無い）。SNS・KMS は `modules/sns`・`modules/kms` の README
- 参照した一次資料:
  - AWS Provider v6.73.0 の `aws_iam_role`・`aws_iam_policy`・`aws_iam_role_policy_attachment`・`aws_iam_role_policy_attachments_exclusive` の文書と、`internal/service/iam/role.go`・`policy.go`・`role_policy_attachments_exclusive.go`、IAM API Reference の DeletePolicy（2026-11-20）。managed policy を policy ごとの `aws_iam_role_policy_attachment` で付け、`aws_iam_role_policy_attachments_exclusive` を併用して同じ ARN を載せる（`aws_iam_role` の文書の形）。排他の resource は破棄で policy を外さず、`aws_iam_policy` の削除は付いたままだと失敗するため（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定21）。`force_detach_policies` は採らない（role の削除だけに効き、policy の削除と置換には効かない）
  - AWS Provider v6.70.0 の `aws_scheduler_schedule`・`aws_scheduler_schedule_group`・`aws_chatbot_slack_channel_configuration`・`aws_iam_role`・`aws_iam_policy`・`aws_iam_role_policy`・`aws_iam_role_policies_exclusive`・`aws_iam_role_policy_attachments_exclusive` の文書と、`internal/service/chatbot/service_package.go`・`internal/service/iam/role_policy_attachments_exclusive.go`
  - EventBridge Scheduler User Guide（[Confused deputy prevention](https://docs.aws.amazon.com/scheduler/latest/UserGuide/cross-service-confused-deputy-prevention.html)、[Templated targets](https://docs.aws.amazon.com/scheduler/latest/UserGuide/managing-targets-templated.html)、[Context attributes](https://docs.aws.amazon.com/scheduler/latest/UserGuide/managing-schedule-context-attributes.html)）
  - Amazon Q Developer in chat applications Administrator Guide（[IAM policies](https://docs.aws.amazon.com/chatbot/latest/adminguide/chatbot-iam-policies.html)、[Understanding permissions](https://docs.aws.amazon.com/chatbot/latest/adminguide/understanding-permissions.html)、[Custom notifications](https://docs.aws.amazon.com/chatbot/latest/adminguide/custom-notifs.html)、[Getting started](https://docs.aws.amazon.com/chatbot/latest/adminguide/getting-started.html)）と API Reference（[CreateSlackChannelConfiguration](https://docs.aws.amazon.com/chatbot/latest/APIReference/API_CreateSlackChannelConfiguration.html)）
  - AWS Cost Management User Guide（[Creating an Amazon SNS topic for anomaly notifications](https://docs.aws.amazon.com/cost-management/latest/userguide/ad-SNS.html)）。Topic Policy の `costalerts.amazonaws.com` の `SNS:Publish` と、Key Policy の `kms:GenerateDataKey*`・`kms:Decrypt`。`aws:SourceAccount` は任意の変形として示される
  - Service Authorization Reference（[Amazon CloudWatch](https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazoncloudwatch.html)、[Amazon EventBridge Scheduler](https://docs.aws.amazon.com/service-authorization/latest/reference/list_amazoneventbridgescheduler.html)）
  - AWS Well-Architected Framework（最新版の [SEC08-BP02](https://docs.aws.amazon.com/wellarchitected/latest/framework/sec08-bp02.html)「Enforce encryption at rest」。[2024-06-27 版](https://docs.aws.amazon.com/wellarchitected/2024-06-27/framework/)の OPS08-BP04「Create actionable alerts」・OPS10-BP02「Have a process per alert」・SEC10-BP07「Run simulations」。2026-11-16 更新の最新版は番号を振り直しており、警報は OPS06-BP01、シミュレーションは SEC10-BP04 である）
  - AWS Security Hub の[コントロールの一覧](https://docs.aws.amazon.com/securityhub/latest/userguide/securityhub-controls-reference.html)
  - us-east-1 の Topic（2026-11-21）: CloudWatch の [PutMetricAlarm](https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_PutMetricAlarm.html)・[Alarm actions](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/alarm-actions.html)（alarm と別の region の Topic を action に取れるとも取れないとも書いていない）、Route 53 の [Monitoring hosted zones using Amazon CloudWatch](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/monitoring-hosted-zones-with-cloudwatch.html)（DNSSEC を含む hosted zone の metric は us-east-1）、CloudFront の [Monitor CloudFront metrics with Amazon CloudWatch](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/monitoring-using-cloudwatch.html)、Chatbot の [Regions and quotas](https://docs.aws.amazon.com/chatbot/latest/adminguide/chatbot-regions.html)（1つの構成に複数の region の Topic を組み合わせられる）、EventBridge Scheduler の [Templated targets](https://docs.aws.amazon.com/scheduler/latest/UserGuide/managing-targets-templated.html)（SNS の別の region への配送は書いていない）、AWS Provider v6.73.0 の enhanced region support の guide と `aws_chatbot_slack_channel_configuration`・`aws_cloudwatch_metric_alarm` の文書。WAF（CloudFront の web ACL）と CloudFront 用の ACM の証明書の metric の region は明文を読めず、同じ形を取れる候補とだけする。Registry の AWS / HashiCorp が publisher の Chatbot・Scheduler の module は、v6.70.0 の参照日から変わらず無い

照らした Security Hub のコントロール:

| コントロール | 扱い |
| --- | --- |
| SNS.1（KMS での保存時の暗号化） | 従う（`modules/sns`） |
| SNS.4（Topic Policy が公開アクセスを許さない） | 従う（`modules/sns`）。実際の判定は sandbox で確かめる |
| SNS.3（tag） | 従わない。`modules/sns` の記録を引き継ぐ |
| KMS.3・KMS.4・KMS.5 | 従う（`modules/kms`） |
| KMS.1・KMS.2（IAM の policy で全鍵の復号を許さない） | 従う。IAM で KMS を与えない |
| IAM.1・IAM.21（full admin を許さない、customer managed policy のサービスの wildcard） | 従う。Chatbot の policy は Action を列挙し、guardrail を省かない |
| Scheduler・Chatbot | コントロールの一覧に無い |

採用した知見:

- Scheduler の実行 role の信頼に `aws:SourceAccount` と、schedule group の ARN の `aws:SourceArn` を `StringEquals` で置く（Confused deputy prevention の例）
- 心拍の本文を Chatbot の custom notification の形にし、警報でないことを title で示す（Well-Architected 2024-06-27 版 OPS08-BP04）
- Chatbot の guardrail を必ず置く
- us-east-1 にしか出ない metric の alarm の通知先を、alarm と同じ region の Topic にし、1つの Chatbot の構成で2つの region の Topic を購読する（Route 53・CloudFront・Chatbot の文書）
- 心拍が試さない経路（4つのサービス）は、sandbox の Integration Test を模擬の事象として試す位置づけにする（Well-Architected 2024-06-27 版 SEC10-BP07）

採用しなかった知見と理由:

- Scheduler の文書の権限の例（`sns:Publish` を Resource `"*"`）: Topic の ARN に限る（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定9）
- Scheduler の DLQ: 読み手が無く、失敗は心拍の不在で気づく
- us-east-1 の alarm から provider の region の Topic へ直接送ること、EventBridge で別の region へ転送すること、Scheduler から別の region の Topic へ送ること: 文書に無いか、alarm の action を Topic に持たない形になる（[ops-notification/ADR-0001](docs/adr/0001-topic-per-account-in-workload-root.md) 案C）
- Checkov CKV_AWS_297（Scheduler の customer managed key）: 本文は機微な値を持たない固定の文で、Well-Architected（最新版）SEC08-BP02 に照らし CMK は守りを足さない。`internal/heartbeat/main.tf` で抑止し、理由の正本は [ops-notification/ADR-0102](docs/adr/0102-daily-heartbeat.md) 決定7
- Chatbot の `AWS-Chatbot-NotificationsOnly-Policy` の部分 wildcard・`CloudWatchReadOnlyAccess`・`ReadOnlyAccess`: [ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md)
- Chatbot のログ（`logging_level` の `ERROR`・`INFO`）: 管理の外の Log Group を作る
- SNS の配送状況のログ: ログの role と Log Group が増え、経路の死活は心拍が見る（`modules/sns` の「まだ持たないもの」と同じ判断）

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

| 逸脱 | 記録 |
| --- | --- |
| Chatbot の読み取りの policy を、公式の Notifications only の部分 wildcard ではなく8つの Action に限る | [ops-notification/ADR-0201](docs/adr/0201-chat-destination-read-only.md) 決定4 |
| AWS Organizations: 管理アカウントに置くものを管理アカウントでしかできないものに限る。管理アカウントにも2つの Topic・鍵・心拍・Chatbot の構成を置く | [ops-notification/ADR-0001](docs/adr/0001-topic-per-account-in-workload-root.md) 決定4 |
| CloudWatch・Budgets の Allow に `aws:SourceArn` のワイルドカードを置かない、EventBridge の Allow に `aws:SourceAccount` を置く | [`modules/sns/README.md`](../../modules/sns/README.md) の「公式推奨からの逸脱」を引き継ぐ |
| 鍵の管理の委任を21 Action にする、CloudWatch・Budgets の文の `aws:SourceArn` を置かない、EventBridge の文に暗号化コンテキストを置く | [`modules/kms/README.md`](../../modules/kms/README.md) の「公式推奨からの逸脱」を引き継ぐ。us-east-1 の鍵と Topic（`internal/us-east-1-topic`）も同じ値を持つ |

## 保証しないもの

- Chatbot が通知を描くのに要る操作が、`read` の8つの Action で足りること。管理アカウントの実環境で、2つの region の心拍が Slack に届いたことで一度確かめた（2026-11-25）。これを続けて確かめる検査は無い
- Chatbot の service-linked role が在るときに、Chatbot が構成の作成で2つの Topic の購読を作ること。path は data source の postcondition が plan で見る
- role が無いときに plan が落ちること。provider の data source の読み取りの失敗（`NoSuchEntity`）に依り、mock の Unit Test では確かめない
- Chatbot の channel role の信頼（`chatbot.amazonaws.com` を `aws:SourceAccount` で限る）は、Chatbot の文書に例が無い（confused deputy の頁も無い）。principal は Chatbot の慣行に、条件は IAM の一般の指針に従う。この信頼で Chatbot が role を引き受けられることは、管理アカウントの実環境で心拍が Slack に届いたことで一度確かめた（2026-11-25）。これを続けて確かめる検査は無い
- IAM で Topic へ publish できる role と Key Policy の SNS 経由の role の一致（`PLAN-NOTIFY-03`）は、inline policy（`aws_iam_role_policy`）だけを数える。attach した managed policy の `sns:Publish` は数えない
- 心拍の role を同じ名前で作り直したとき、2つの Topic の鍵の Key Policy が書き直されず、KMS が古い role を名指し続けること。`plan.kms_key_policy` の `PLAN-KMS-10` は、宣言した state の鍵の Key Policy だけを見る
- CI の実 plan（workload root の plan と apply）には `plan.stateful_protection` と `plan.s3_bucket_namespace` だけを掛ける。Topic Policy の形（`plan.sns_topic_policy`）、合成の不変条件（`plan.ops_notification`）、Key Policy の形（`plan.kms_key_policy`）、Resource `"*"` の Action（`plan.iam_action_wildcard`）は、mock の plan でだけ見る。作成の plan では Topic と role の ARN が決まらず、`plan.kms_key_policy` と `plan.iam_action_wildcard` は identity root の宣言を入力に要るからである。CI の実 plan での守りは、permissions boundary と apply の role の権限の列挙が持つ
- `env` の値と、Topic が実際に在るアカウントの対応。env の root が決める
- 層検査・コード実行の検出・作成の順序の検査は HCL のソースの形だけを見る
- Security の道具の抑止: KICS の e592a0c5（IAM Access Analyzer）・e38a8e0a（tag）を `internal/heartbeat/main.tf`・`internal/chat-slack/main.tf` の先頭で、Checkov の CKV_AWS_297 を `aws_scheduler_schedule.this` で抑止している。理由と撤回条件は各行が持つ

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。引数は v6.70.0 の Provider の文書で確かめた。テストは v6.73.0 で実行した。`reference/.terraform.lock.hcl` の版は `providers/` の正本が持つ
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求である
