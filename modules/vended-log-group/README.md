# vended-log-group

AWS のサービスが CloudWatch Logs の V2 permissions の配送（delivery source・delivery destination・delivery）で書くログの受け口を作る一般 module（ADR-0102 決定12・15）。呼び出し1回が、Log Group 1つと、そこへ配送サービスが書くことを許す resource policy と、配送の宛先の delivery destination を作る。S3 のサーバーアクセスログ、CloudFront の標準ログ（v2）、ALB・NLB のアクセスログのように、送り側のサービスが V2 permissions の配送で CloudWatch Logs へ書く Log Group に使う。書き手が IAM の主体である Log Group は [`modules/log-group`](../log-group/README.md) を使う。

## 持つもの

| resource | 値 |
| --- | --- |
| `aws_cloudwatch_log_group` | `modules/log-group` と同じ宣言（class `STANDARD`、保持日数と鍵と削除の保護は入力から、`skip_destroy = false`） |
| `aws_cloudwatch_log_resource_policy` | この Log Group にだけ付く policy（`resource_arn`）。下の1文 |
| `aws_cloudwatch_log_delivery_destination` | 宛先はこの Log Group。resource policy の後に作る |

| 入力 | 意味 |
| --- | --- |
| `name` | Log Group と delivery destination の名前。英数字と `-` `_` だけで 1-60 文字（delivery destination の名前の制約。`PutDeliveryDestination`）。`/` を使えないので、`/` で区切る名前の規約はこの module では成り立たない。delivery destination の名前はアカウントで一意である（`PutDeliveryDestination`）。呼ぶ usecase は同じ値を `modules/kms` の `users.services.logs` へ渡す |
| `delivery_sources` | この Log Group へ配送してよい delivery source の名前。1件以上、英数字と `-` `_` だけで 1-60 文字（`PutDeliverySource`）、重ねない。送り側の module が置く delivery source と同じ値を渡す |
| `retention_days`・`kms_key_arn`・`deletion_protection` | `modules/log-group` と同じ |

| 出力 | 使う側 |
| --- | --- |
| `log_group_name`・`log_group_arn` | `modules/log-group` と同じ |
| `delivery_destination_arn` | 送り側の module が置く `aws_cloudwatch_log_delivery` の宛先。resource policy の後に決まるので、それを名指す delivery も resource policy の後に作られる |

保持日数の下限と削除の保護の扱いは `modules/log-group` の README が持つ。

### resource policy の文書

文書は、テストが mock の下で中身を確かめられるよう `jsonencode` で組む。

| Sid | Principal | Action | Resource | Condition |
| --- | --- | --- | --- | --- |
| `AWSLogDeliveryWrite` | `delivery.logs.amazonaws.com` | `logs:CreateLogStream`・`logs:PutLogEvents` | この Log Group の `log-stream:*` | `aws:SourceAccount` が同じアカウント、`aws:SourceArn` が `delivery_sources` に名指した同じアカウントとリージョンの delivery source の ARN |

`aws:SourceArn` を delivery source の名前まで絞るのは、同じアカウントで配送を作れる主体が、自分の delivery source とこの Log Group を指す別の delivery destination を作るだけで、この Log Group へ書けるからである。`PutDeliveryDestination` は宛先の ARN を入力として受け、文書は宛先の所有の確認を述べない。resource policy は service principal にだけ効くので、それを止めるのは `aws:SourceArn` の絞り込みだけである。CloudWatch Logs の文書の例（`:*`）から外れる形であり、その記録は「公式推奨からの逸脱」が持つ。

Log Group に resource policy が無いと、配送を作る主体が権限を持つ場合に AWS が policy を自動で作る。その policy は Terraform の管理の外にある。delivery destination を resource policy の後に作り、その ARN を出力にすることで、送り側の delivery が必ず resource policy の後に作られるようにしている。

## 2つの module に分ける理由

この module は `modules/log-group` を呼ばずに、同じ Log Group の宣言を自分で持つ。resource policy は Log Group を親に取る attachment で、親と同じ module に置く（ADR-0102 決定18）。`modules/log-group` を呼ぶと親が別の module になる。逆に `modules/log-group` に resource policy を持たせると、書き手が IAM の主体である Log Group にも要らない Allow が付き、入力で有無を切り替えると決定15 の切り替えになる。重複した宣言は ADR-0205 決定11 が許す重複で、同じ値であることは2つの module の `tests/` が同じ観点で見る。

## 持たないもの

| 持たないもの | 行き先 |
| --- | --- |
| delivery source と delivery | 送り側の resource（Bucket など）を持つ module に置く。delivery source は送り側の resource を親に取る（ADR-0102 決定18） |
| V1 permissions の配送（AWS WAF）と、Route 53 の公開 DNS のクエリログ | delivery destination を使わず、resource policy の形も違う（WAF はアカウント単位、Route 53 は `route53.amazonaws.com`）。要る usecase の着手時に見直す |
| S3 や Firehose など CloudWatch Logs 以外の宛先 | 宛先の種類が違う。要る usecase の着手時に見直す |
| 別のアカウントからの配送（`PutDeliveryDestinationPolicy`） | 責務外（ADR-0301 決定18） |
| delivery destination の `output_format` | 置かない。受けられる形式は送り側の log type で決まり、AWS の S3 の例も省いている。一般 module は送り側の log type を知らない |
| `modules/log-group` の「持たないもの」の各項 | 同じ行き先 |

## 保証しないもの

- `modules/log-group` の「保証しないもの」の各項（鍵の Key Policy、対称鍵、`kms:DescribeKey`、鍵の無効化、保持日数の短縮、`0` の拒否、`null`、層検査）
- resource policy の文書と delivery destination の宛先は `tests/` が mock の apply で見る。Unit Test に属する（ADR-0501 決定9）
- delivery destination が resource policy の後に作られる順序は `depends_on` が持つ。mock の plan と apply には順序が現れないので、どのテストも見ていない
- AWS が resource policy を自動で作らないこと、Terraform の policy に AWS が文を足さないこと、`output_format` を置かないことで読み戻しに差分が出続けないことは、どのテストも確かめていない。最初の消費者の CI の apply の後の plan が `No changes.` を示すこと、`aws logs describe-resource-policies --resource-arn <Log Group の ARN> --policy-scope RESOURCE` が1件を返すことで確かめる
- `aws:SourceArn` に delivery source の ARN が入ること。AWS の文書がそう述べるのは S3・X-Ray の宛先だけである。外れていると配送が届かないが、apply の後の plan の `No changes.` と resource policy の件数では見えない。最初の消費者の delivery を作った後に、Log Group へ event が届くことで確かめる
- `delivery_sources` の件数の上限は validation が見ない。policy の文書の長さの上限（Provider の文書は 5120 文字、AWS は 51,200 bytes）に当たると apply が落ちる
- resource policy の文の security invariant（principal と Condition）を Policy で見る検査は、Log Group を含む最初の reference の root と同じ変更で足す
- Security の4つの道具のうち KICS の3件（保持日数、IAM Access Analyzer、tag）は抑止している。理由と撤回条件は `main.tf` と `policy.tf` の先頭が持つ

## 版

`modules/log-group` と同じ。delivery destination と resource policy の引数も、v6.68.0 の Provider の文書に在ることと、v6.70.0 の文書のとおりであることを確かめた。

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-13

- AWS / HashiCorp が publisher の、V2 permissions の配送の受け口の Registry module は見つからなかった
- 参照した一次資料: AWS Provider v6.70.0 の `aws_cloudwatch_log_resource_policy`・`aws_cloudwatch_log_delivery_destination` の文書、Amazon CloudWatch Logs User Guide の [Logs sent to CloudWatch Logs](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/AWS-logs-infrastructure-V2-CloudWatchLogs.html) と [Supported log destinations and permissions](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/AWS-logs-destinations-table.html)、[Logs sent to Amazon S3](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/AWS-logs-infrastructure-V2-S3.html)（bucket policy と KMS の鍵の policy の例）と [Logs sent to X-Ray](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/AWS-logs-infrastructure-V2-XRayTraces.html)、API Reference の [PutDeliveryDestination](https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_PutDeliveryDestination.html) と [PutDeliverySource](https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_PutDeliverySource.html)、Amazon S3 User Guide の [Delivering server access logs to CloudWatch Logs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/sal-cw-enabling.html)

採用した知見:

- resource policy の文の形（principal、Action、Resource、`aws:SourceAccount` と `aws:SourceArn` の条件）は CloudWatch Logs の文書の例のとおりにする。`aws:SourceArn` の値だけは、S3・X-Ray の宛先の文書と、S3 の宛先の KMS の鍵の policy の例のとおり delivery source の ARN にする
- policy は Log Group ごとに `resource_arn` で付ける。アカウント単位の policy はリージョンに10個までで、全 Log Group で共有される
- 複数の delivery source が1つの delivery destination を共有できる（`PutDeliveryDestination`、S3 の文書）

採用しなかった知見と理由:

- `/aws/vendedlogs/*` の接頭辞で policy を束ねる形: Log Group ごとの policy を Terraform が持つので要らない
- AWS による resource policy の自動作成に任せる形: policy が Terraform の管理の外に置かれる

## 公式推奨からの逸脱（ADR-0207 決定11）

この module を使うすべてのユースケースが、次の逸脱を引き継ぐ。

| 対象 | 理由 | 由来 | 影響（security / availability / cost） | 代替案 | 見直し条件 |
| --- | --- | --- | --- | --- | --- |
| resource policy の `aws:SourceArn` を、CloudWatch Logs の宛先の文書の例の `arn:<partition>:logs:<region>:<account>:*` ではなく、`delivery_sources` に名指した delivery source の ARN にする | 同じアカウントで配送を作れる主体が、自分の delivery source からこの Log Group へ書くことを塞ぐ（ADR-0301 決定2・9） | ユースケースの制約ではなく、この module の不変条件 | security は、同じアカウントの名指さない delivery source からの書き込みを塞ぐ。availability は、`aws:SourceArn` に入る値が delivery source の ARN でなければ配送が届かない（「保証しないもの」）。cost は変わらない | 文書の例の `:*` | 最初の消費者の配送が名指した delivery source の ARN で届かなかったとき。AWS が CloudWatch Logs の宛先の `aws:SourceArn` の値を文書に書いたとき |
