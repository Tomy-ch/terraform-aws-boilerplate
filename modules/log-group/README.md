# log-group

CloudWatch Logs の Log Group を、保持日数と鍵による暗号化を必ず持つ形で作る一般 module（ADR-0102 決定12・15）。呼び出し1回が Log Group を1つ作る。書き手が IAM の主体である Log Group（ECS の awslogs、CloudTrail、VPC Flow Logs など）に使う。AWS のサービスが CloudWatch Logs の V2 permissions の配送で書く Log Group は [`modules/vended-log-group`](../vended-log-group/README.md) を使う。

## 持つもの

| resource | 値 |
| --- | --- |
| `aws_cloudwatch_log_group` | class `STANDARD`、保持日数と鍵と削除の保護は入力から、`skip_destroy = false`。tag を持たない |

| 入力 | 意味 |
| --- | --- |
| `name` | Log Group の名前。英数字と `.` `-` `_` `/` `#` だけで 1-512 文字、`aws/` で始めない（CloudWatch Logs の `CreateLogGroup`）。鍵の Key Policy が名前で Log Group を名指すので、呼ぶ usecase は同じ値を `modules/kms` の `users.services.logs` へ渡す |
| `retention_days` | ログを残す日数。365 以上の、CloudWatch Logs が受ける値（365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653）に限る。`0`（無期限）を受けない |
| `kms_key_arn` | ログの暗号化に使う鍵の ARN。Log Group と同じ partition・リージョン・アカウントの鍵だけを受ける（plan で落ちる）。alias の ARN と多リージョンの鍵は受けない |
| `deletion_protection` | Log Group の削除を拒むか。既定値を持たない |

| 出力 | 使う側 |
| --- | --- |
| `log_group_name` | ECS の `awslogs-group` など、名前で Log Group を指す設定 |
| `log_group_arn` | usecase の出力契約「ログの所在」、書き手の IAM の Resource。API が付ける `:*` を除いた形で、log stream への Action には `<arn>:log-stream:*` を付ける |

保持日数を 365 以上に限るのは、AWS Security Hub の CloudWatch.16 の既定の parameter（`minRetentionTime` 365）と Checkov の CKV_AWS_338 に合わせるためである。CloudWatch.16 の値は利用者が変えられる parameter で、唯一の推奨値ではない。下限は保持日数を固定するのではなく許容の集合を狭めるもので、365 以上のどれを選ぶかは利用者の要件のまま残る（ADR-0207 決定8・13）。365 日より短い保持を要る usecase は、この module の責務外とする（「持たないもの」）。

削除の保護を一度 `true` にした Log Group を `false` に戻すときは、入力を `false` にする。Provider は引数を消しても保護を外さない（Provider の `aws_cloudwatch_log_group` の文書）。この module は値を常に渡すので、入力の変更がそのまま届く。

## 持たないもの

| 持たないもの | 行き先 |
| --- | --- |
| 365 日より短い保持 | 責務外（ADR-0102 決定5・7）。要る usecase は、逸脱として Log Group ごと `internal/` に置き、ADR-0207 決定11 の記録を持つ |
| `aws_cloudwatch_log_subscription_filter` | 置き場は未決。`docs/v1.0-plan/scope.md` 第9.3節（消費者が購読する）と第3.1節の検査(3)（attachment は親と同じ module）が食い違い、決める側に返している（#108） |
| `aws_cloudwatch_log_metric_filter`・`aws_cloudwatch_log_index_policy`・`aws_cloudwatch_log_stream` | 要る usecase が Log Group ごと `internal/` に置く（ADR-0102 決定15・18） |
| `aws_cloudwatch_log_data_protection_policy` | 何を機微とみなすかは usecase ごとに違い、一般 module が持つと入力の切り替えか、識別子を AWS の表現で受ける入力になる（ADR-0102 決定15、ADR-0204 決定2）。要る usecase が Log Group ごと `internal/` に置く |
| `aws_cloudwatch_log_resource_policy` | AWS のサービスが書く Log Group は `modules/vended-log-group`。アカウント単位の policy（`policy_name`）は置かない（リージョンに10個までの共有の資源で、親を持たない） |
| `STANDARD` 以外の class（`INFREQUENT_ACCESS`・`DELIVERY`・`ESSENTIALS`） | Subscription Filter などの機能の一部が使えない、または保持が1日か2日に固定される。class は作成後に変えられない |
| 別のリージョンの Log Group（Route 53 の公開 DNS のクエリログは us-east-1） | 呼んだ provider のリージョンに作る。要る usecase の着手時に見直す |
| サービスが Log Group を暗黙に作ること（ECS の `awslogs-create-group` など）の防止 | 書き手の module（ECS、IAM role）の不変条件 |

## 保証しないもの

- 鍵の Key Policy が CloudWatch Logs のサービスに、暗号化コンテキストでこの Log Group を許すこと。この module では検査できない。`name` と `users.services.logs` の一致は、呼ぶ usecase のテストが見る
- 鍵が対称鍵であること（ARN から判定できない）
- `CreateLogGroup` を呼ぶ主体が鍵の `kms:DescribeKey` を要ること（CloudWatch Logs の暗号化の文書）。呼ぶ主体の IAM の関心事である
- 鍵を無効にするか削除すると、その鍵で暗号化したログを CloudWatch Logs が読めなくなること
- 保持日数を短くすると、古いログが消えること。これを止める検査は無い
- 入力から導いた値（class、`skip_destroy`、保持日数、鍵、削除の保護、validation と precondition）は `tests/` が mock の plan と apply で見る。Unit Test に属する（ADR-0501 決定9）
- 保持日数の `0` を拒むのは validation と `tests/` だけである。Security の道具は、KICS の ef0b316a を抑止したので `0` を見ない（Checkov の CKV_AWS_66・CKV_AWS_338 は `0` を通す）。Policy は、Log Group を含む最初の reference の root と同じ変更で足す
- `null` を渡す run は `expect_failures` で受けられないので、`nullable = false` を外してもテストは落ちない
- 層検査（attachment の同居、`source` の方向）は未配線である。配線した後に、この module も検査の対象として確かめる
- 実際の AWS での作成・読み戻し・破棄は、どのテストも確かめていない。最初の消費者の CI の apply と、その後の CI の plan が `No changes.` を示すことで確かめる
- Security の4つの道具のうち KICS の3件（保持日数、IAM Access Analyzer、tag）は抑止している。理由と撤回条件は `main.tf` の先頭が持つ

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。使う引数が v6.68.0 の Provider の文書に在ること、引数と許容値が v6.70.0 の文書のとおりであることを確かめた。`terraform test` を走らせたのは、lock の無いこの module で init が選んだ 6.73.0 だけである
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求である

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-13

- HashiCorp が publisher の Log Group の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=cloudwatch&provider=aws` などの結果に namespace `hashicorp` が無い）。AWS が publisher のものとして `aws-ia/cloudwatch-log-group` v1.0.1 を読んだ
- 参照した一次資料: AWS Provider v6.70.0 の `aws_cloudwatch_log_group` の文書、Amazon CloudWatch Logs API Reference の [CreateLogGroup](https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_CreateLogGroup.html)、Amazon CloudWatch Logs User Guide の [Encrypt log data using AWS KMS](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/encrypt-log-data-kms.html)、AWS Security Hub の [CloudWatch.16](https://docs.aws.amazon.com/securityhub/latest/userguide/cloudwatch-controls.html)

採用した知見:

- 鍵を既定値の無い必須入力にする（`aws-ia/cloudwatch-log-group`）
- 鍵は Log Group と同じリージョンに置く（CloudWatch Logs のサービス principal は鍵と同じリージョンで動く）
- class を明示する。API の既定と同じでも、作成後に変えられないので値を宣言に残す（ADR-0207 決定5）

採用しなかった知見と理由:

- `aws-ia/cloudwatch-log-group` の保持日数の既定 `0`（無期限）: 保持日数を明示する方針（`docs/v1.0-plan/scope.md` 第9.3節）に反する
- 同じ module の `name_prefix`: Provider が乱数を付け、Key Policy に渡す名前と一致させられない
- 同じ module の IAM role と、`logs:CreateLogGroup` を含む Resource `"*"` の policy: 最小権限に反し、Log Group の暗黙の作成を許す（ADR-0301 決定8・9）
- 同じ module の Service principal の文字列入力と `tags` の map 入力: AWS の表現をそのまま受ける入力になる（ADR-0204 決定2・5）
- CloudWatch Logs の「Log Group ごとに別の鍵」: 鍵の単位は消費者が決める。この module は単位を決めない

## 公式推奨からの逸脱（ADR-0207 決定11）

無い。
