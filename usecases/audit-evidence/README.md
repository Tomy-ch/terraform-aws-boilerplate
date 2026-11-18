# audit-evidence

組織の監査の証跡（組織の trail と AWS Config の配信）を、改変できない形で保管するユースケース。ログ保管（log-archive）のアカウントに、Object Lock を掛けた S3 Bucket と、証跡を暗号化する鍵と、Bucket の S3 サーバーアクセスログの受け口を1組だけ持つ（[audit-evidence/ADR-0001](docs/adr/0001-log-archive-placement.md)）。**experimental** であり、昇格と削除の条件は同 ADR が持つ。

## 解決する問題

記録される側の主体（管理アカウントを含む）が、自分の記録を消すことも、鍵を止めて読めなくすることもできない場所に、証跡を保持の期間のあいだ残す（[`docs/v1.0-plan/scope.md`](../../docs/v1.0-plan/scope.md) 第10.1節）。書き手は AWS のサービスだけで、人と CI の主体は object を書かず、消さず、保持を変えない（[audit-evidence/ADR-0102](docs/adr/0102-archive-bucket-policy.md)）。

## 実行形態とネットワーク境界

- AWS の上で動く処理は無い。組織の trail と Config が、AWS のサービスとして Bucket に書く
- 外への egress は無い。Bucket は Public Access Block の4項目をすべて有効にし、ACL を無効にする（`BucketOwnerEnforced`）
- TLS でない要求は、Bucket Policy の Deny で拒否する
- サーバーアクセスログは CloudWatch Logs の配送で同じアカウントの Log Group へ送る（[audit-evidence/ADR-0104](docs/adr/0104-server-access-logs-to-cloudwatch.md)）

## 公開契約

| 入力 | 型 | 意味 |
| --- | --- | --- |
| `project` | `string`（英小文字と数字） | 名前の先頭に置くプロジェクトの token |
| `env` | `log-archive` / `sandbox` | 置くアカウントの env の token。log-archive は保持を COMPLIANCE で、sandbox は GOVERNANCE の1日で掛け、sandbox だけ destroy で object を消す（[audit-evidence/ADR-0101](docs/adr/0101-object-lock-retention.md)） |
| `retention_days` | `number`（365 以上 3653 以下の整数、既定値なし） | 証跡の各版をロックし、その後に失効させるまでの日数。log-archive では誰も短縮できない。sandbox では使わない |
| `writers` | `object({ cloudtrail_organization_trail = object({ trail_name, owner_account_id, organization_id, region }), config_source_accounts = optional(set(string), []) })` | 書き手。`cloudtrail_organization_trail` は組織の trail の名前・所有する管理アカウントの ID・組織の ID・リージョン、`config_source_accounts` は Config が配信するアカウントの ID。Bucket Policy と鍵の Key Policy の文は module が組む（[audit-evidence/ADR-0102](docs/adr/0102-archive-bucket-policy.md)・[ADR-0201](docs/adr/0201-archive-key-cross-account-services.md)） |
| `access_log_retention_days` | `number`（365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653） | サーバーアクセスログの Log Group の保持日数 |
| `notify_topic_arn` | `string`（SNS の Topic の ARN） | 配信の途絶の alarm の通知先（`ops-notification` の Topic）。Bucket と同じアカウントとリージョンでなければ plan で落ちる（[audit-evidence/ADR-0301](docs/adr/0301-delivery-stall-alarm.md)） |

すべての入力は既定値を持たない（`config_source_accounts` を除く）。

Bucket の完全な名前は `<project>-<env>-audit-<account_id>-<region>-an` で、63文字を超えると plan で落ちる。`env = log-archive`・region が `ap-northeast-1` のとき、`project` は14文字までになる。鍵の alias は `alias/<project>-<env>-audit`、アクセスログの Log Group と鍵の alias は `<project>-<env>-audit-access-log` である。

| 出力 | 使う側 |
| --- | --- |
| `bucket_name` | 組織の trail の `s3_bucket_name`、Config の delivery channel の `s3_bucket_name`。Bucket Policy の後に決まり、配信の設定が Bucket Policy の前に作られない |
| `bucket_arn` | 組織の trail の S3 data events の対象から、この Bucket を除く（記録のループを避ける） |
| `kms_key_arn` | 組織の trail の `kms_key_id`、Config の delivery channel の `s3_kms_key_arn`。Key Policy の後に決まる。trail は `kms_key_id` にこの鍵を必ず設定する。設定しないと、Bucket の既定の暗号化が S3 の暗号化コンテキストで鍵を使い、CloudTrail の文（trail の ARN の暗号化コンテキスト）に当たらず配信が落ちる |
| `access_log_group_arn` | Bucket の S3 サーバーアクセスログの所在（`docs/v1.0-plan/scope.md` 第9.3節の、出力契約に含めるログの所在）。購読する消費者はまだ無い |

- ログの所在: Bucket の S3 サーバーアクセスログは `access_log_group_arn` の Log Group が持つ。Bucket への API の呼び出しの記録は組織の trail が持つ
- 基盤系アラームの通知先 Topic ARN: `notify_topic_arn` で受ける

## supported / unsupported

supported:

- 組織の trail（管理アカウントが所有する1本）の配信の保管
- AWS Config の配信の保管（アカウントごとの prefix）
- log-archive での COMPLIANCE の保持と、sandbox での GOVERNANCE の1日の保持
- Bucket の S3 サーバーアクセスログの CloudWatch Logs への配送
- 配信の途絶の通知

unsupported:

| 要求 | 理由 |
| --- | --- |
| log-archive と sandbox 以外のアカウントに置く | [audit-evidence/ADR-0001](docs/adr/0001-log-archive-placement.md) |
| 組織で複数の保管庫（複数の Bucket、複数のリージョン） | 同上 |
| リージョン間の複製 | [audit-evidence/ADR-0103](docs/adr/0103-deviations-replication-mfa-delete-notifications.md) |
| storage class の移行 | 費用の最適化で一意の推奨が無く、調査の取り出しに遅延と最低保管期間が掛かる |
| legal hold と event hold | Bucket Policy が拒む（[audit-evidence/ADR-0102](docs/adr/0102-archive-bucket-policy.md)） |
| 書き手が object ごとに保持を指定すること | 既定の保持を超える指定と、別の mode の指定は Bucket Policy が拒む |
| ALB・VPC Flow Logs・CloudFront・WAF・Route 53 のログをこの Bucket へ寄せること | それぞれを出すユースケースの Log Group か Bucket が持つ（`docs/v1.0-plan/scope.md` 第9.3節）。ALB のアクセスログの宛先は SSE-S3 に限られる |
| trail と data events の selector を持つこと | 組織の trail（`security-tooling`）が持つ |
| Security Lake | v1.0 の範囲の外 |

未決:

- 証跡を読む経路。`docs/v1.0-plan/scope.md` 第10.1節は、security の調査用の role からの読み取りを許す。その role への Key Policy の文と Bucket の読み取りの許可は、後から足す。それまでは Key Policy に読み手が居ないので、誰も証跡を復号できない
- GuardDuty の finding の書き出しの書き手。形を一次資料で確かめた後に `writers` へ足す

## 所有者

| resource | 所有 |
| --- | --- |
| 保管の Bucket と attachment、delivery source と delivery、request metrics と alarm | [`internal/archive-bucket`](internal/archive-bucket/)（Object Lock は1つのユースケースだけが要る attachment なので、`modules/s3` ではなく Bucket ごとここに置く。[`modules/s3`](../../modules/s3/README.md)） |
| 証跡の鍵、アクセスログの鍵 | [`modules/kms`](../../modules/kms/README.md) を2回呼ぶ |
| アクセスログの Log Group、resource policy、delivery destination | [`modules/vended-log-group`](../../modules/vended-log-group/README.md) |
| 組織の trail、Config の delivery channel | このユースケースの外（`security-tooling`・`account-baseline`） |
| Bucket・鍵・配送の設定を変える操作の拒否 | 組織の SCP（`protect-log-vault`。`organization-baseline` が付ける） |

## 依存するユースケースと判断 Issue

- 書き手: 組織の trail（#22）、Config（#22）。trail の所有者と置き場が変わると `writers` の値が変わる
- 通知の Topic: `ops-notification`（#25）
- アカウント: log-archive のアカウント（#21）。workload root とその CI の経路が入るまで、このユースケースはどの root からも呼ばれない
- GuardDuty の finding の書き出し（#361）

## 検証方法

| 層 | 何を確かめるか | 道具 |
| --- | --- | --- |
| Unit | env からの mode・日数・`force_destroy` の導出、Bucket の不変条件、Bucket Policy と lifecycle の文書の完全一致、delivery と alarm の値、入力の検査 | `make tf-test`（`tests/`、`internal/archive-bucket/tests/`、`modules/kms/tests/`） |
| Contract | 4つの output の値と配線 | `tests/contract.tftest.hcl` |
| Policy | `PLAN-AUDIT-00`-`05`（宣言した address と alarm、既定の保持と Bucket 全体に掛かる失効の一致、Allow の形、Deny の8文の Action・Resource・Condition と例外の不在、Object Lock、trail と Config のアカウントの一致）、`PLAN-ALARM-01`、`PLAN-KMS-*`（宣言した鍵の外のアカウントの要求元を含む）、`PLAN-STATE-*`（Bucket・lifecycle・Object Lock の設定・鍵の削除と置換、lifecycle の失効の日数の短縮。保持の日数は失効と同じ入力から決まるので、保持の短縮はこの短縮として現れる。Object Lock の既定の保持だけを縮める変更は見ない） | `make tf-policy`（reference の root の plan）。CI の plan への適用は、log-archive の root を配線するときに足す |
| Static | 層の規則、型の分類 | `make layer-check`、`make infra-code-gate` |
| Integration（sandbox） | 下の故障モード | 未実施。sandbox の apply の経路と掃除の CI と apply の role の権限が揃った後に行う。破棄の後の期待残存は2つの鍵（識別子 = 鍵の ARN、状態 `PendingDeletion`、期限 = 削除の予定日） |

mock では検出できない故障モード:

- Object Lock の Bucket の作成が account regional namespace で通ること、`object_lock_enabled` を変えたときに置換になるか
- 組織の trail が Bucket Policy と Key Policy の条件（完全一致の暗号化コンテキストを含む）で配信できること
- Config がアカウントごとの文で配信できること
- 配信された object に既定の保持（mode と retain-until-date）が付くこと
- 削除・サービスでない主体の書き込み・保持の延長・別の mode・hold が実 API で拒まれること
- lifecycle の失効が Bucket Policy の Deny に当たらず、保持の後に版と delete marker が消えること
- sandbox で `force_destroy` が GOVERNANCE の object を消せること（destroy する主体が `s3:BypassGovernanceRetention` を要る）
- サーバーアクセスログが Log Group に届くこと
- request metrics が出て、alarm が OK になること

## 保証しないもの

- 組織の SCP（`protect-log-vault`）が入るまで、log-archive のアカウントで S3 と KMS の管理の権限を持つ主体が、Bucket Policy・lifecycle・Object Lock の既定の保持を変えること、鍵を止めること、配送を消すことを防ぐこと。既に書かれた object は、log-archive では COMPLIANCE の保持が守る
- `PLAN-KMS-05` が、鍵の外のどのアカウントを受けてよいかを限ること。受けるアカウントは入力の値で、Policy は値の形だけを見る
- log-archive の Log Group の削除の保護を、usecase のテストが確かめること。入れ子の module の属性をテストから参照できない
- trail だけの配信の途絶を知らせること。Config が書き込んでいる間は alarm は鳴らない

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-19

- AWS / HashiCorp が publisher の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search` を `q` = `object-lock` / `cloudtrail` / `log-archive` / `audit`、`provider=aws` で引いた結果に、namespace `aws-ia`・`hashicorp`・`aws`・`aws-samples` が無い）
- 参照した一次資料: [AWS SRA の Log Archive のアカウント](https://docs.aws.amazon.com/prescriptive-guidance/latest/security-reference-architecture/log-archive.html)、Amazon S3 User Guide（[Locking objects with Object Lock](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)、[Object Lock considerations](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock-managing.html)、[Enabling server access logging](https://docs.aws.amazon.com/AmazonS3/latest/userguide/enable-server-access-logging.html)）、[Service Authorization Reference の S3](https://servicereference.us-east-1.amazonaws.com/v1/s3/s3.json)（`s3:object-lock-*` の条件キー）、CloudTrail User Guide（[S3 bucket policy for CloudTrail](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/create-s3-bucket-policy-for-cloudtrail.html)、[KMS key policies for CloudTrail](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/create-kms-key-policy-for-cloudtrail.html)）、AWS Config Developer Guide の S3 Bucket と KMS key の権限、CloudWatch Logs API Reference の [PutDeliverySource](https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_PutDeliverySource.html)、[Security Hub の S3 の control](https://docs.aws.amazon.com/securityhub/latest/userguide/s3-controls.html)・[CloudTrail の control](https://docs.aws.amazon.com/securityhub/latest/userguide/cloudtrail-controls.html)、AWS Provider 6.70.0 の `aws_s3_bucket`・`aws_s3_bucket_object_lock_configuration`・`aws_cloudwatch_log_delivery_source` の schema

採用した知見:

- 専用のアカウントに改変できない保管を置く（SRA）。COMPLIANCE の既定の保持（Security Hub S3.15）
- customer managed key による SSE-KMS（SRA、Security Hub CloudTrail.2・S3.17）
- 組織の trail の書き込みの3文と、`aws:SourceArn` を管理アカウントの trail の ARN にすること（CloudTrail User Guide）
- 既定の保持の変更と、保持の上限は Bucket Policy の Deny で縛る（S3 User Guide「Object Lock considerations」の例）
- Bucket Policy の TLS の要求（Security Hub S3.5）、Public Access Block と ACL の無効化（S3.2・S3.3・S3.8・S3.12）、版管理（S3.14）、lifecycle（S3.10・S3.13）

採用しなかった知見と理由:

- リージョン間の複製（SRA「SHOULD be replicated」、Security Hub S3.7）、MFA delete（S3.20、CIS 2.1.2）、event notification（S3.11）: [audit-evidence/ADR-0103](docs/adr/0103-deviations-replication-mfa-delete-notifications.md)
- S3 宛てのサーバーアクセスログ（S3.9、CloudTrail.7）: [audit-evidence/ADR-0104](docs/adr/0104-server-access-logs-to-cloudwatch.md)
- CloudTrail の Key Policy の暗号化コンテキストのアカウント単位の wildcard: trail の ARN の完全一致にした（[audit-evidence/ADR-0201](docs/adr/0201-archive-key-cross-account-services.md)）
- SSE-KMS 以外の書き込みを拒む Deny: CloudTrail の digest の配信が壊れる（[audit-evidence/ADR-0102](docs/adr/0102-archive-bucket-policy.md)）

## 公式推奨からの逸脱（[ADR-0207](../../docs/adr/0207-default-value-policy.md) 決定11）

| 外れる対象 | 記録 |
| --- | --- |
| Security Hub S3.7・SRA の複製、S3.20・CIS 2.1.2、S3.11 | [audit-evidence/ADR-0103](docs/adr/0103-deviations-replication-mfa-delete-notifications.md) |
| Security Hub S3.9・CloudTrail.7 | [audit-evidence/ADR-0104](docs/adr/0104-server-access-logs-to-cloudwatch.md) |
