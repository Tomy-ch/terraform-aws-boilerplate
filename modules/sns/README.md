# sns

暗号化した SNS の Topic と、その Topic Policy を持つ一般 module（ADR-0102 決定12・15）。呼び出し1回が、Topic 1つと Topic Policy 1文書を作る。Topic へ publish できるのは同じアカウントの AWS のサービスと、IAM で `sns:Publish` を与えた同じアカウントの role だけで、他のアカウントからの publish の口を持たない。

## 持つもの

| resource | 値 |
| --- | --- |
| `aws_sns_topic` | 標準の Topic（FIFO ではない）、customer managed key での暗号化、署名 v2（SHA256）、`tracing_config = "PassThrough"`。inline の `policy`・`delivery_policy`・`display_name`・配送状況のログの role・tag を持たない |
| `aws_sns_topic_policy` | 下の文書 |

| 入力 | 意味 |
| --- | --- |
| `name` | Topic の名前。英数字・`_`・`-` だけで 1-256 文字（FIFO の `.fifo` を受けない）。前半 `<project>-<env>-<purpose>` は呼び出す usecase が組み、その形は usecase のテストで固定する |
| `kms_key_arn` | 暗号化に使う鍵の ARN。鍵の ARN だけを受け、alias の ARN・`alias/aws/sns`・多リージョンの鍵を受けない。Topic と同じ partition・リージョン・アカウントの鍵だけを受ける（鍵の ARN が plan で決まれば plan で、決まらなければ apply で落ちる） |
| `publishers` | Topic へ publish する同じアカウントの AWS のサービス。`budgets`・`cloudwatch`・`events` から選ぶ。空を受ける（role だけが publish する Topic） |

| 出力 | 使う側 |
| --- | --- |
| `topic_arn` | 送り手（alarm の通知先、EventBridge の rule と Scheduler の target、Budgets の通知先）、購読する側、role の IAM の `sns:Publish` の Resource。送り手が既定の Topic Policy の間に Topic を使わないよう、Topic Policy の置き換えの後に決まる（`depends_on`） |

鍵の Key Policy は `modules/kms` が持つ。呼び出す usecase は、同じ `name` と `publishers` を `modules/kms` の `users.services.sns` へ、publish する role を `users.roles[*]`（`via = "sns"`、`topics` に `name`）へ渡す。Topic の暗号化に要る鍵の許可は、Topic を暗号化する SNS ではなく、publish する側（サービス principal と role）に要る（Amazon SNS Developer Guide「Managing Amazon SNS encryption keys and costs」）。

### Topic Policy の文書

文書は、テストが mock の下で中身を確かめられるよう `jsonencode` で組む。`Resource` は module が `name` から組む Topic の ARN で、plan で決まる。`publishers` に無いサービスの文は出さない。

| Sid | Effect | Principal | Action | Condition |
| --- | --- | --- | --- | --- |
| `DenyInsecureTransport` | Deny | `"*"` | `sns:Publish` | `Bool aws:SecureTransport = "false"` |
| `AllowBudgetsPublish` | Allow | `budgets.amazonaws.com` | `sns:Publish` | `StringEquals aws:SourceAccount` に Topic のアカウント |
| `AllowCloudWatchPublish` | Allow | `cloudwatch.amazonaws.com` | 同上 | 同上 |
| `AllowEventsPublish` | Allow | `events.amazonaws.com` | 同上 | 同上 |

IAM の role の `sns:Publish` は Topic Policy に書かない。同じアカウントの IAM の主体は、identity policy の許可だけで publish できる。Topic Policy の Allow は、サービス principal だけになる。

## まだ持たないもの

ほかは、それを使う消費者が着手するときに、同じ形（入力の値か属性と、その文）で足す。

| 足すもの | 足す消費者 |
| --- | --- |
| SQS 宛の subscription と、その DLQ（`redrive_policy`） | realtime-delivery（#41）・email-delivery（#42） |
| SES の configuration set からの publish | email-delivery（#42） |
| `sms-voice` と platform application の送信 | sms-push-delivery（#44） |
| email・https の subscription | 要る消費者。未確認の subscription は destroy で AWS に残り（AWS Provider の `aws_sns_topic_subscription` の NOTE）、届くことと購読が続くことを検査する手段を持たない |
| 配送状況のログ（`*_failure_feedback_role_arn` と、その role） | 要る消費者。経路の死活は ops-notification の毎日の心拍が見る |

## 持たないもの

- message data protection policy（`aws_sns_topic_data_protection_policy`）: 2026-04-30 から新規の顧客に提供されない（Amazon SNS「message data protection availability change」）。通知の本文に機微な値を載せないことは、送り手の責務である
- 他のアカウントからの publish: Topic は各アカウントに置き、アカウントを越えて送らない
- FIFO の Topic

## 保証しないもの

- 入力から導いた値（Topic の固定値、Topic Policy の文書、validation と precondition、出力）は `tests/` が mock の plan と apply で見る。Unit Test に属する（ADR-0501 決定9）。`aws_sns_topic` の inline の `policy` を書かないことは、mock の plan で値が決まらないのでテストが見ていない
- Topic Policy の文書の security invariant（TLS の Deny、Allow の Principal がサービス principal であること、Allow の Action が `sns:Publish` だけであること、条件が固定値であること）を見る Policy Test は無い。Topic Policy が唯一の policy であること、inline の `policy` を書かないこと、Topic の削除と置換を拒むことも同じである。いずれも、SNS の Topic を持つ最初の reference の root（ops-notification）と同じ変更で `conftest/plan` に足す。いまの入力には Topic が無く、宣言だけを足すと Policy が落ちる
- Topic Policy と鍵の Key Policy が同じ送り手を名指すことは、module の境界の外（呼び出す usecase が同じ値を両方へ渡す）で、検査は無い
- `aws_sns_topic_policy` を destroy すると、Provider は既定の Topic Policy（`Principal {"AWS":"*"}` と `AWS:SourceOwner` で `SNS:Publish`・`SNS:Subscribe`・`SNS:SetTopicAttributes` などを許す文書）を書き戻す（AWS Provider の `internal/service/sns/topic_policy.go`）。Topic の作成から Topic Policy の置き換えまでの間も、Topic は既定の Topic Policy を持つ。`topic_arn` の `depends_on` が送り手を置き換えの後に作らせることは、mock の plan と apply には順序が現れないので、どのテストも見ていない
- 次は実際の AWS で確かめていない。最初の消費者（ops-notification）の sandbox での検証で確かめる
  - CloudWatch・EventBridge・Budgets と、EventBridge Scheduler の role からの publish が、Topic Policy と鍵の Key Policy で拒まれないこと
  - TLS でない publish が拒まれること
  - Security Hub の SNS.4 が、この Topic Policy を公開アクセスを許さないと判定すること。固定値だけの条件は非公開とみなされる（AWS Config `sns-topic-no-public-access`）が、Deny の `Principal "*"` の扱いは文書から読み切れていない
- `aws` 以外の partition でのサービス principal の値は確かめていない
- Security の4つの道具の所見と、その抑止の理由と撤回条件は、`main.tf` と `policy.tf` の先頭が持つ

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。引数は v6.70.0 の Provider の文書と schema で確かめた。テストを実行して確かめたのは v6.73.0（`.terraform.lock.hcl`）だけである
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.17.1（`mise.toml`）だけである

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-19、AWS Provider v6.70.0

- AWS / HashiCorp が publisher の SNS の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=sns&provider=aws&limit=50` の結果に namespace `aws-ia`・`hashicorp`・`aws`・`aws-samples` が無い）
- 参照した一次資料: AWS Provider v6.70.0 の `aws_sns_topic`・`aws_sns_topic_policy`・`aws_sns_topic_subscription` の文書と `internal/service/sns/topic_policy.go`、Amazon SNS Developer Guide（[Managing Amazon SNS encryption keys and costs](https://docs.aws.amazon.com/sns/latest/dg/sns-key-management.html)、[Setting up Amazon SNS topic encryption](https://docs.aws.amazon.com/sns/latest/dg/sns-enable-encryption-for-topic.html)、[Amazon SNS security best practices](https://docs.aws.amazon.com/sns/latest/dg/sns-security-best-practices.html)、[Verifying the signatures of Amazon SNS messages](https://docs.aws.amazon.com/sns/latest/dg/sns-verify-signature-of-message.html)、[Amazon SNS message data protection availability change](https://docs.aws.amazon.com/sns/latest/dg/sns-message-data-protection-availability-change.html)）、Amazon CloudWatch User Guide（[Notifying users on alarm changes](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Notify_Users_Alarm_Changes.html)）、Amazon EventBridge User Guide（[Using resource-based policies](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-use-resource-based.html)）、AWS Cost Management（[Creating an Amazon SNS topic for budget notifications](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-sns-policy.html)）、AWS Security Hub（[controls for Amazon SNS](https://docs.aws.amazon.com/securityhub/latest/userguide/sns-controls.html)）、AWS Config（[sns-topic-no-public-access](https://docs.aws.amazon.com/config/latest/developerguide/sns-topic-no-public-access.html)）

照らした Security Hub のコントロール:

| コントロール | この module の扱い |
| --- | --- |
| SNS.4（AWS Foundational Security Best Practices。Topic Policy が公開アクセスを許さない） | `Principal "*"` の Allow を出さず、Allow の条件を固定値だけにする |
| SNS.1（KMS での保存時の暗号化。AWS 管理鍵でも満たす） | customer managed key を必須にする。コントロールより強い側である |
| SNS.3（tag） | tag を持たない。tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。SNS.3 は AWS Foundational Security Best Practices の標準に入らない。CIS AWS Foundations Benchmark の SNS の項目は確かめていない |

採用した知見:

- AWS のサービスが publish する Topic は customer managed key で暗号化し、publish する側に `kms:GenerateDataKey*`・`kms:Decrypt` を許す（鍵の側は `modules/kms`）
- TLS でない publish を拒む Deny（Amazon SNS security best practices の例と同じ形）
- 署名 v2（SHA256）。API の既定は v1（SHA1）
- `Principal "*"` の Allow を置かない。サービス principal の許可は `aws:SourceAccount` で限る（`aws:SourceOwner` は使わない）
- Topic Policy は `aws_sns_topic_policy` で持ち、inline の `policy` と併用しない

採用しなかった知見と理由:

- CloudWatch と Budgets の例の `ArnLike aws:SourceArn` のワイルドカード: 下の「公式推奨からの逸脱」
- EventBridge の例の、条件の無い Allow: 下の「公式推奨からの逸脱」
- `alias/aws/sns`（AWS 管理鍵）: Key Policy を変えられず、AWS のサービスの publisher に鍵の許可を与えられない
- `aws_iam_policy_document`: mock の下で文書を確かめられない
- TLS の Deny を `sns:*` に広げる: 公式の例は `sns:Publish` で、広げる根拠を一次資料で持てない

## 公式推奨からの逸脱（ADR-0207 決定11）

この module を使うすべてのユースケースが、次の逸脱を引き継ぐ。

| 対象 | 理由 | 由来 | 影響（security / availability / cost） | 代替案 | 見直し条件 |
| --- | --- | --- | --- | --- | --- |
| CloudWatch・Budgets の Allow に、例の `ArnLike aws:SourceArn`（`arn:<partition>:cloudwatch:<region>:<account>:alarm:*`・`arn:<partition>:budgets::<account>:*`）を置かず、`aws:SourceAccount` の固定値だけで限る | ワイルドカードの ARN は `aws:SourceAccount` と同じ範囲（アカウントのすべての alarm・budget）しか限らない。Security Hub SNS.4 の規則（AWS Config `sns-topic-no-public-access`）は、固定値だけで許す policy を非公開とみなし、ワイルドカードを含む条件をそうみなさない。送り手の alarm と budget は Topic を持たない usecase が作り、Topic の側は名前を知らない（ADR-0102 決定16） | ユースケースの制約ではなく、この module の判断。鍵の側の同じ判断は `modules/kms/README.md` が持つ | security は、例と同じ範囲で変わらない。availability と cost は変わらない | 例のとおり `ArnLike aws:SourceArn` を足す。個々の alarm・budget の ARN を受ける | 送り手の alarm・budget を ARN で名指せる配線ができたとき。AWS が例か SNS.4 の規則を改めたとき。AWS Provider の major 更新時（ADR-0206 決定12） |
| EventBridge の Allow に、例に無い `aws:SourceAccount` の固定値の条件を置く | 例の条件の無い Allow は、サービス principal の要求元を限らない。Security Hub SNS.4 の規則（AWS Config `sns-topic-no-public-access`）は、条件の無いサービス principal の Allow を公開とみなしうる | ユースケースの制約ではなく、この module の判断 | security は、要求元を自分のアカウントに限る側へ強まる。availability は、EventBridge の送信が `aws:SourceAccount` を載せないなら拒まれる（未確認。上の「保証しないもの」）。cost は変わらない | 例のとおり条件の無い Allow にする | ops-notification の sandbox で EventBridge の送信が拒まれたとき。AWS が例を改めたとき。AWS Provider の major 更新時（ADR-0206 決定12） |
