# kms

customer managed key と、その Key Policy・alias を持つ一般 module（ADR-0102 決定12・15）。呼び出し1回が、鍵1本・Key Policy 1文書・alias 1つを作る。鍵の単位（usecase ごとか、共有か）はこの module では決めず、呼び出す usecase が決める。

## 持つもの

| resource | 値 |
| --- | --- |
| `aws_kms_key` | 対称の暗号化鍵（`SYMMETRIC_DEFAULT`、`ENCRYPT_DECRYPT`）、単一 region、有効、自動 rotation 365 日、削除の猶予 30 日、lockout の安全チェックを外さない。inline の `policy` と tag を持たない |
| `aws_kms_key_policy` | 下の文書。lockout の安全チェックを外さない |
| `aws_kms_alias` | `alias/<name>` |

| 入力 | 意味 |
| --- | --- |
| `name` | alias の名前の後半。英数字・`/`・`_`・`-` だけで 1-250 文字、`aws/` で始めない（KMS の `CreateAlias`）。前半 `<project>-<env>-<purpose>` は呼び出す usecase が組み、その形は usecase のテストで固定する |
| `users.roles` | 鍵を使う IAM role の ARN と、経由するサービス（`via`）と、利用の範囲（`access`）の組。同じアカウントと partition の role だけを受ける（ARN が plan で決まれば plan で、決まらなければ apply で落ちる）。同じ role と `via` の組を2回は受けない（`access` だけが違っても受けない） |
| `users.roles[*].access` | `read-write` は復号と鍵の生成、`read` は `objects` に名指した S3 の object の復号だけ。`via = "sns"` は `read-write` だけを受ける |
| `users.roles[*].objects` | `access = "read"` のとき1件以上の S3 の object の ARN（`arn:<partition>:s3:::<bucket>/<key>`）。`*`・`?` を受けない。鍵と同じ partition だけを受ける。`read-write` では空にする。S3 の ARN はアカウントを持たないので、この module は別のアカウントの Bucket の object かどうかを見ない。`conftest/plan` を掛ける root の plan では、`PLAN-KMS-12` が Bucket の名前（account regional namespace）で見て、委任のアカウントの、文の `kms:ViaService` の region の account regional namespace の Bucket の object でないもの（global namespace の Bucket を含む）を落とす |
| `users.roles[*].topics` | `via = "sns"` のとき1件以上の SNS の Topic の名前（ARN ではない。英数字・`_`・`-` の 1-256 文字で、FIFO の Topic を受けない）。module が鍵と同じアカウントと region の ARN を組む。`via = "s3"` では空にする |
| `users.services.logs` | 鍵で暗号化する CloudWatch Logs の Log Group の名前。module が鍵と同じアカウントと region の ARN を組む |
| `users.services.sns` | 鍵で暗号化する SNS の Topic の名前（`topic`）と、その Topic へ publish する同じアカウントの AWS のサービス（`publishers`。`budgets`・`cloudwatch`・`costalerts`・`events` から1つ以上）。同じ Topic を2回は受けない。`modules/sns` の `name`・`publishers` と同じ値を渡す |
| `users.services.cloudtrail` | 鍵で暗号化して配信する CloudTrail の trail の ARN。`*`・`?` を受けない。鍵と同じ partition だけを受け、鍵の外のアカウントの trail（組織の trail）も受ける |
| `users.services.config` | 鍵で暗号化して配信する AWS Config のアカウントの12桁の ID。鍵の外のアカウントも受ける |

`users` は合わせて1件以上を要る。管理の委任は暗号操作を含まないので、利用者の無い鍵は誰も使えない。

| 出力 | 使う側 |
| --- | --- |
| `key_arn` | 鍵を使う resource（S3 の暗号化と Bucket Policy の条件、backend の `kms_key_id`、Log Group の `kms_key_id`）。同じ apply で鍵を作って暗号化を始める消費者が既定の Key Policy の間に鍵を使わないよう、Key Policy の置き換えの後に決まる（`depends_on`） |
| `key_arn_for_iam` | `key_arn` と同じ値で、Key Policy の置き換えを待たない。鍵を使わず ARN だけを名指す場所（IAM の policy の Resource）にだけ使う。Key Policy が名指す role の IAM が鍵の ARN を要るとき、`key_arn` を使うと role → Key Policy → `key_arn` → role の IAM の循環になりうる |
| `alias_name` | 人の手順が鍵を名指す名前（`state-backend` の `state_key_alias_name`）。入力だけから決まるので、鍵より前に参照できる |
| `key_policy` | 呼び出す usecase のテスト。Key Policy の文書（JSON の文字列）を返し、渡した `users` から組まれた文を確かめるのに使う。権限の配線には使わない |

消費者は alias ではなく `key_arn` で鍵を指す。alias を管理できる主体は、alias の付け替えで暗号化先をすり替えられる（KMS「Default key policy」の Warning）。alias の名前は人の識別と、人の手順と init の運用機構（`scripts/tf-local` の `state-key`）が鍵の ARN を引くことに使う。名前を変える変更は、それを引く消費者の手順の変更を伴う。

### Key Policy の文書

文書は、テストが mock の下で中身を確かめられるよう `jsonencode` で組む。利用者が0件の種類の文は出さない。principal と Log Group は並べ替えて重複を除くので、入力の順序で文書は変わらない。`Resource` はすべて `"*"`（Key Policy ではこの鍵を指す）。

| Sid | Principal | Action | Condition |
| --- | --- | --- | --- |
| `AccountKeyAdministration` | アカウント（`arn:<partition>:iam::<account>:root`） | 鍵の管理の21 Action（`policy.tf` の `administration_actions`）。暗号操作と `kms:CreateGrant` を含まない | 無し |
| `RolesViaS3ReadWrite` | `users.roles` のうち `via = "s3"`・`access = "read-write"` の role | `kms:Decrypt`・`kms:GenerateDataKey` | `kms:ViaService = s3.<region>.amazonaws.com` |
| `RolesViaS3Read<n>` | `users.roles` のうち `via = "s3"`・`access = "read"` の role。1 role に1文で、`<n>` は ARN の昇順の 0 からの番号 | `kms:Decrypt` | `kms:ViaService = s3.<region>.amazonaws.com` と、`StringEquals kms:EncryptionContext:aws:s3:arn` にその role の `objects` |
| `LogsService` | `logs.<region>.amazonaws.com` | `kms:Decrypt`・`kms:Describe*`・`kms:Encrypt`・`kms:GenerateDataKey*`・`kms:ReEncrypt*` | `ArnEquals kms:EncryptionContext:aws:logs:arn` に `users.services.logs` の Log Group の ARN（`:*` を付けない） |
| `RolesViaSns<n>` | `users.roles` のうち `via = "sns"` の role。1 role に1文で、`<n>` は ARN の昇順の 0 からの番号 | `kms:Decrypt`・`kms:GenerateDataKey*` | `kms:ViaService = sns.<region>.amazonaws.com` と、`StringEquals kms:EncryptionContext:aws:sns:topicArn` にその role の `topics` の Topic の ARN |
| `SnsPublisherBudgets` | `budgets.amazonaws.com`。`users.services.sns` のいずれかの `publishers` が `budgets` を含むとき | `kms:Decrypt`・`kms:GenerateDataKey*` | `StringEquals aws:SourceAccount` に鍵のアカウント |
| `SnsPublisherCloudWatch` | `cloudwatch.amazonaws.com`。同じく `cloudwatch` を含むとき | 同上 | 同上 |
| `SnsPublisherCostAlerts` | `costalerts.amazonaws.com`（Cost Anomaly Detection）。同じく `costalerts` を含むとき | 同上 | 同上 |
| `SnsPublisherEvents` | `events.amazonaws.com`。同じく `events` を含むとき | 同上 | `StringEquals kms:EncryptionContext:aws:sns:topicArn` に `events` を含む Topic の ARN |
| `CloudTrailEncrypt` | `cloudtrail.amazonaws.com` | `kms:GenerateDataKey*` | `StringEquals` の `aws:SourceArn` と `kms:EncryptionContext:aws:cloudtrail:arn` に `users.services.cloudtrail` の ARN |
| `CloudTrailDescribeKey` | `cloudtrail.amazonaws.com` | `kms:DescribeKey` | `StringEquals aws:SourceArn` に `users.services.cloudtrail` の ARN |
| `ConfigService` | `config.amazonaws.com` | `kms:Decrypt`・`kms:GenerateDataKey` | `StringEquals aws:SourceAccount` に `users.services.config` のアカウント |

`via` の語彙と、それぞれが許す Action:

| `via` | Action | 根拠 |
| --- | --- | --- |
| `s3` | `kms:GenerateDataKey`（書き込み）・`kms:Decrypt`（読み出し。multipart の書き込みも要る） | Amazon S3 User Guide「Using server-side encryption with AWS KMS keys」。Terraform の S3 backend の文書は `kms:Encrypt` も挙げるが、S3 の文書を採る（ADR-0207 決定3） |
| `sns` | `kms:GenerateDataKey*`・`kms:Decrypt`（暗号化した Topic への publish） | Amazon SNS Developer Guide「Managing Amazon SNS encryption keys and costs」。部分 wildcard の `kms:GenerateDataKey*` は文書の値のまま使う |

`kms:ViaService` は IAM の主体が AWS のサービスを経由して呼ぶ要求にだけ当たる条件で、role はこの鍵を直接は使えない。`access = "read"` の文は、S3 が渡す暗号化コンテキスト `aws:s3:arn` で object を限る。Bucket Key を使う Bucket ではコンテキストが Bucket の ARN になるので、object では限れない（Amazon S3 User Guide「SSE-KMS」）。暗号化コンテキストに Bucket の ARN を置く文は `PLAN-KMS-12` が落とす。CloudWatch Logs の文はサービス principal に対する許可で、暗号化コンテキストで Log Group を名指して限る。CloudTrail の文は、組織の trail の `aws:SourceAccount` が trail を限らない管理アカウントを指すので、`aws:SourceArn` と暗号化コンテキストの trail の ARN の完全一致で限る（CloudTrail User Guide「Configure AWS KMS key policies for CloudTrail」の例はアカウント単位の wildcard）。Config の文は Config の文書のとおり `aws:SourceAccount` で限る。trail と Config のアカウントが鍵の外のアカウントのとき、`conftest/plan` の `PLAN-KMS-05` は `decl.json` の `kms_key_policy.cross_account_sources` に宣言した Key Policy でだけそれを受ける。SNS の文は、SNS が Topic の ARN を渡す暗号化コンテキスト `aws:sns:topicArn` で Topic を限る（Amazon SNS Developer Guide「Setting up Amazon SNS topic encryption」の Key Policy の例）。`kms:ViaService` はサービス principal の要求には当たらない（KMS の条件キーの文書）。EventBridge から暗号化した Topic への送信は `aws:SourceAccount`・`aws:SourceArn`・`aws:SourceOrgID` の条件を受けない（Amazon SNS Developer Guide）ので、`SnsPublisherEvents` は暗号化コンテキストだけで限る。Budgets と CloudWatch は `aws:SourceAccount` で限り、Topic では限らない。

Key Policy の文書が 32768 文字（KMS の `PutKeyPolicy`）を超えると落ちる（role の ARN が plan で決まれば plan で、決まらなければ apply で）。

## まだ持たないもの

持つのは、`users.roles[*].via = "s3"`（`state-backend` の state の鍵）・`users.roles[*].via = "sns"`・`users.services.logs`（CloudWatch Logs）・`users.services.sns`（SNS の Topic）・`users.services.cloudtrail`・`users.services.config`（`audit-evidence` の証跡の鍵）である。`users.services.logs` は `network-foundation` と `audit-evidence` が、`via = "sns"` と `users.services.sns` は `ops-notification` が渡す。ほかは、それを使う消費者が着手するときに、同じ形（`via` の値か `users.services` の項と、その文）で足す。

| 足すもの | 足す消費者 |
| --- | --- |
| `via = "logs"` | それを要る消費者 |
| 他のアカウントへの許可と `aws_kms_grant` | data-refresh（#59）。方式（Key Policy か grant か）は未決 |
| us-east-1 など別の region の鍵 | その region の鍵を要る消費者。この module は呼ばれた provider の region に1本作る |

## 保証しないもの

- 暗号操作は、Key Policy で名指した role とサービスだけに許す。IAM で `AdministratorAccess` を持つ人の主体も、そのままでは復号できない。名指されない主体が鍵で暗号化された object を読むには、委任された `kms:PutKeyPolicy` で文を足す必要があり、その変更は次の apply で戻る。bootstrap の途中の一時 IAM user もこれに当たる（段5 の後の扱いは `docs/setup/bootstrap.md` 段5 と `usecases/state-backend/README.md` の unsupported が持つ）
- Key Policy の Deny は持たない。鍵の管理（`kms:PutKeyPolicy` を含む）はアカウントへ委任しているので、どの主体が Key Policy を書き換えられるかは IAM が決める。書き込みを拒む Deny は、置くなら消費者の IAM の側に置く（ADR-0604 決定10）。deployment-identity は apply の role に、上の委任の Action のすべてを鍵の ARN に対する Deny で与える
- 入力から導いた値（鍵の属性、alias の名前、Key Policy の文書、validation と precondition）は `tests/` が mock の plan と apply で見る。Unit Test に属する（ADR-0501 決定9）。`aws_kms_key` の inline の `policy` を書かないことは、`conftest/plan` が見る。`state-backend` の reference の root の plan では `plan.input`（`PLAN-INPUT-09`）が、CI の plan と apply では `plan.stateful_protection`（`PLAN-STATE-08`）が見る（ADR-0402 決定17・18、ADR-0604 決定13）
- Key Policy の文書の security invariant は `conftest/plan` の `plan.kms_key_policy`（`PLAN-KMS-*`）が持ち、`state-backend` の reference の root の plan に掛かる。`aws_kms_key_policy` は `conftest/plan/decl.json` の `required_documents` に在るので、Key Policy が入力から消えると Policy が落ちる
- アカウントへの委任の文を除く部分 wildcard の Action（`kms:Describe*` など）と、Key Policy の `Resource "*"` を見る検査は無い
- role の文の暗号化コンテキストのうち、`plan.kms_key_policy` が見るのは、SNS 経由の role の Topic の ARN（`PLAN-KMS-11`）と、S3 経由の read の文の object の ARN（`PLAN-KMS-12`）である。12 が read の文と読むのは `kms:Decrypt` を許し `kms:GenerateDataKey` を許さない文で、`GenerateDataKey` を足した文は object で限らなくても通る。state の鍵では暗号操作の principal の集合を `PLAN-SBW-06` が宣言と比べるが、ほかの鍵にそれを見る検査は無い。`RolesViaS3ReadWrite` は `kms:ViaService` で S3 経由に限るだけで object では限らず、限ることを見る検査は無い
- `PLAN-KMS-12` は、account regional namespace の Bucket（名前が `-<アカウント>-<region>-an` で終わる）をそのアカウントにしか作れないこと（Amazon S3 User Guide「[Namespaces for general purpose buckets](https://docs.aws.amazon.com/AmazonS3/latest/userguide/gpbucketnamespaces.html)」）を前提に、Bucket の名前で所有を読む。この性質は実際の AWS で確かめていない。Bucket を account regional namespace で作ることは、Bucket を作る各所の固定値（`modules/s3`、`state-backend` の plan の出力の Bucket、`audit-evidence` の保管の Bucket）が持ち、すべての `aws_s3_bucket` にそれを求める Policy は無い。global namespace の Bucket の object を読む read の文は 12 が落とす
- 鍵は作成の時点では既定の Key Policy（アカウントへの `kms:*`）を持ち、alias の作成の後に `aws_kms_key_policy` が置き換える。alias は鍵の ARN を持たない消費者が鍵を名指す識別子（`alias_name`）なので、置き換えが落ちても alias の無い鍵を残さない順にしている。置き換えが落ちたときは、alias の付いた鍵が既定の Key Policy のまま次の apply まで残る。alias の作成が落ちたときは、alias の無い鍵が残る。この順序を見るテストは無い。`key_arn` の `depends_on` が消費者の resource を Key Policy の置き換えの後に作らせることも、mock の plan と apply には順序が現れないので、どのテストも見ていない。`aws_kms_key_policy` を destroy すると、Provider は既定の Key Policy を書き戻す（Provider の `aws_kms_key_policy` の NOTE）。destroy と、`moved` を伴わない address の変更（置換）は、`conftest/plan` の `plan.stateful_protection` が CI の plan と apply で拒む。CI の plan と apply が掛かるのは、CI の入口を持つ root だけである。`removed`（`destroy = false`）は state から外すだけで、鍵の側は変わらない
- `key_arn_for_iam` を鍵を使う resource に渡すことを拒む検査は無い。渡せば、その resource は既定の Key Policy の間に作られうる
- `plan.kms_key_policy` の Rego のテストが使う委任の文は、`policy.tf` の列挙を手で写したもので、一致は検査しない。この module の出力が `PLAN-KMS-*` を通ることは、`state-backend` と `audit-evidence` の reference の root の plan が確かめる。後者が CloudWatch Logs・CloudTrail・Config の文を持つ
- CloudWatch Logs の文は、CloudWatch Logs の文書の例のとおり `kms:Describe*` を暗号化コンテキストの条件と同じ文に置く。KMS の文書は、`kms:EncryptionContext:*` の条件を受ける操作に `DescribeKey` を挙げておらず、それ以外の操作にこの条件を使うと拒否されるとする。この食い違いの下で CloudWatch Logs への配送と読み出しが通ることは、どのテストも見ていない
- 実際の AWS に対して、次はどのテストも確かめていない。最初の消費者の CI の apply と、その後の CI の plan が `No changes.` を示すことで確かめる（#19）。sandbox での複製の作成・破棄、残存の検出、期待残存（削除の猶予中の鍵）の照合は、sandbox のアカウント（#21）と掃除の CI（#76）が揃うまで行わない
  - lockout の安全チェックが、委任の文と、鍵を管理する role の IAM で通ること
  - Key Policy の読み戻しが宣言と一致し、差分が出続けないこと（要素1つの Principal の正規化、role の作り直しで principal が一意 ID に置き換わること）
  - 作成直後の role を Principal に置く `PutKeyPolicy` が、IAM の結果整合で落ちないこと
  - `via = "s3"` の2つの Action で、S3 backend が state と lock file を読み書きできること
  - 鍵を管理する role が、Provider が鍵・Key Policy・alias の作成・読み取りで呼ぶ API を、委任の21 Action と IAM で持てること。削除で呼ぶ API は、最初の消費者の管理 role が持たないので（deployment-identity/ADR-0201 の「与えないもの」）、その経路では確かめない
- SNS の文（`RolesViaSns<n>`・`SnsPublisher*`）が `PLAN-KMS-*` を通ることは、`ops-notification` の reference の root の plan が確かめる。Rego のテストの SNS の文は `policy.tf` を手で写したもので、一致は検査しない
- 次は実際の AWS で確かめていない。確かめるのは、最初の消費者（ops-notification）の sandbox での検証である
  - `SnsPublisherEvents` の暗号化コンテキストの条件で、EventBridge から暗号化した Topic への送信が通ること。一次資料の Key Policy の例は `sns.amazonaws.com` の文にこの条件を使うもので、EventBridge の送信に同じ暗号化コンテキストが載ることを述べる文書は見つかっていない
  - `RolesViaSns<n>` の `kms:ViaService` と暗号化コンテキストの条件で、role（EventBridge Scheduler の心拍など）からの publish が通ること
  - `SnsPublisherBudgets` の形で Budgets から暗号化した Topic への通知が通ること（AWS Cost Management の Key Policy の例は、文書の表示が欠けていて読めていない）
  - `SnsPublisherCostAlerts` の形で Cost Anomaly Detection から暗号化した Topic への通知が通ること
- `aws` 以外の partition でのサービス principal と `kms:ViaService` の値は確かめていない
- Security の4つの道具のうち KICS の3件（鍵の inline の policy、IAM Access Analyzer、tag）は抑止している。理由と撤回条件は `main.tf` と `policy.tf` の先頭が持つ

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。引数と既定値は v6.68.0 の Provider の文書で確かめた。`terraform test` を走らせたのは、lock の無いこの module で init が選んだ 6.72.0 だけである
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.17.1（`mise.toml`）だけである

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-07（SNS の文は 2026-11-19、AWS Provider v6.70.0 の文書。テストの実行は v6.73.0）

- AWS / HashiCorp が publisher の KMS の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=kms&provider=aws` の結果に namespace `aws-ia`・`hashicorp`・`aws`・`aws-samples` が無い）
- 参照した一次資料: AWS Provider v6.68.0 の `aws_kms_key`・`aws_kms_key_policy`・`aws_kms_alias`・`aws_partition`・`aws_region` の文書、AWS KMS Developer Guide（[Default key policy](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-default.html)、[Least-privilege permissions](https://docs.aws.amazon.com/kms/latest/developerguide/least-privilege.html)、[AWS KMS condition keys](https://docs.aws.amazon.com/kms/latest/developerguide/conditions-kms.html)）、AWS KMS API Reference（[CreateAlias](https://docs.aws.amazon.com/kms/latest/APIReference/API_CreateAlias.html)、[PutKeyPolicy](https://docs.aws.amazon.com/kms/latest/APIReference/API_PutKeyPolicy.html)）、Amazon S3 User Guide（[SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html)、[S3 Bucket Keys](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-key.html)）、Amazon CloudWatch Logs（[Encrypt log data using AWS KMS](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/encrypt-log-data-kms.html)、[CreateLogGroup](https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_CreateLogGroup.html)）、Amazon SNS Developer Guide（[Managing Amazon SNS encryption keys and costs](https://docs.aws.amazon.com/sns/latest/dg/sns-key-management.html)、[Setting up Amazon SNS topic encryption](https://docs.aws.amazon.com/sns/latest/dg/sns-enable-encryption-for-topic.html)）、Amazon CloudWatch User Guide（[Notifying users on alarm changes](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Notify_Users_Alarm_Changes.html)）、AWS Cost Management（[Creating an Amazon SNS topic for budget notifications](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-sns-policy.html)。暗号化した Topic の Key Policy の例は表示が欠けていて読めていない。[Creating an Amazon SNS topic for anomaly notifications](https://docs.aws.amazon.com/cost-management/latest/userguide/ad-SNS.html)）、Terraform の [S3 backend](https://developer.hashicorp.com/terraform/language/backend/s3)、[AWS KMS の料金](https://aws.amazon.com/kms/pricing/)

採用した知見:

- Key Policy は `aws_kms_key_policy` で持ち、`aws_kms_key` の inline の `policy` と併用しない（Provider の NOTE。併用すると互いに上書きする）
- 自動 rotation を有効にし、周期 365 日を明示する（Provider の既定は無効。ADR-0207 決定5）
- サービス principal の許可は、要求元か暗号化コンテキストで限る。CloudWatch Logs は Log Group の ARN を `ArnEquals` で名指す（`:*` を付けない）
- 経由するサービスでの利用は `kms:ViaService` で限る
- `kms:EncryptionContext:*` を `ForAllValues` で条件にしない（暗号化コンテキストの無い要求も通る）
- alias の名前に `alias/aws/` を使わない（AWS 管理鍵に予約）
- Cost Anomaly Detection（`costalerts.amazonaws.com`）の文は、AWS Cost Management User Guide「Creating an Amazon SNS topic for anomaly notifications」の Key Policy の例のとおり `aws:SourceAccount` だけで限る。例は `aws:SourceArn` を特定の subscription に限るときの任意の手段として挙げるだけで、送り手の subscription を鍵の側は知らないので置かない
- 暗号化した SNS の Topic へ publish する AWS のサービスには、そのサービス principal に `kms:GenerateDataKey*`・`kms:Decrypt` を許す。サービス principal の許可は `aws:SourceAccount` で限る。EventBridge はこの条件を受けないので、SNS が渡す暗号化コンテキストで Topic を限る

採用しなかった知見と理由:

- アカウントへの `kms:*` の委任（既定の Key Policy）: 鍵の実効的な境界が IAM へ移る。下の「公式推奨からの逸脱」
- console の key administrators の集合（`kms:Create*` など部分 wildcard）をアカウントへ委任する形: `kms:Create*` は `kms:CreateGrant` を含み、IAM で `kms:CreateGrant` を持つ主体が grant で暗号操作を得られる。部分 wildcard は AWS が API を足すと黙って広がる
- CloudWatch Logs の「Log Group ごとに別の鍵」: 鍵の単位は消費者が決める。この module は単位を決めない
- `aws_iam_policy_document`: mock の下で文書を確かめられない
- `deletion_window_in_days` を短くする: 猶予は保持の要件ではなく誤削除への備えであり、削除の猶予中の鍵は課金されない
- CloudWatch と Budgets の例の `ArnLike aws:SourceArn`（`alarm:*`・`budgets::<account>:*` のワイルドカード）: 下の「公式推奨からの逸脱」
- EventBridge の文を条件なしにする（SNS の文書の手順どおりの形）: 下の「公式推奨からの逸脱」

## 公式推奨からの逸脱（ADR-0207 決定11）

この module を使うすべてのユースケースが、次の逸脱を引き継ぐ。暗号操作を Key Policy に限る判断そのものは [ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定12 が持つ。

| 対象 | 理由 | 由来 | 影響（security / availability / cost） | 代替案 | 見直し条件 |
| --- | --- | --- | --- | --- | --- |
| アカウントへの委任を、既定の Key Policy の `kms:*` ではなく、鍵の管理の21 Action（暗号操作と `kms:CreateGrant` を含まない）にする | 暗号操作を Key Policy に限る理由は ADR-0301 案D。この module の分は、管理の Action を部分 wildcard ではなく列挙すること（ADR-0301 決定8 の Action wildcard の禁止にも当たらない） | ユースケースの制約ではなく、repository-wide/ADR-0301 決定12（記録は同 ADR の案D） | 原則の分は ADR-0301 案D。この module の分: security は、鍵を使う主体はすべて `users` で名指し、委任の文が暗号操作を含まないことは `PLAN-KMS-08` が見る。availability は、AWS が鍵の管理の API を足したとき、列挙を直すまでその操作を使えない | 原則の分は ADR-0301 案D。この module の分: console の key administrators の集合（部分 wildcard）の委任。採らない理由は「採用しなかった知見と理由」 | 原則の分は ADR-0301 の見直し条件。この module の分: AWS が鍵の管理の API を足したとき。AWS Provider の major 更新時（ADR-0206 決定12） |
| CloudWatch・Budgets の文の `aws:SourceArn` のワイルドカード（`ArnLike ...:alarm:*`・`arn:aws:budgets::<account>:*`）を置かず、`aws:SourceAccount` の固定値だけで限る | ワイルドカードの ARN は `aws:SourceAccount` と同じ範囲（アカウントのすべての alarm・budget）しか限らない。Topic Policy の側（`modules/sns`）と条件を揃える | ユースケースの制約ではなく、`modules/sns` の Topic Policy と同じ判断（`modules/sns/README.md`「公式推奨からの逸脱」） | security は、例と同じ範囲で変わらない。availability は、`aws:SourceAccount` を受けない経路があれば送信が拒まれる（未確認。上の「保証しないもの」） | 例のとおり `ArnLike aws:SourceArn` を足す | 送り手の alarm・budget を ARN で名指せる配線ができたとき。AWS が例を改めたとき |
| EventBridge の文に、手順に無い暗号化コンテキスト `kms:EncryptionContext:aws:sns:topicArn` の条件を置き、Topic を名指す | 条件の無い許可は、サービス principal の要求元を限らず、`PLAN-KMS-05` が落とす。EventBridge は `aws:SourceAccount`・`aws:SourceArn` を受けないので、SNS が渡す暗号化コンテキストで限る | ユースケースの制約ではなく、repository-wide/ADR-0301 決定12 と、この module の判断 | security は、名指した Topic の暗号操作だけに限る側へ強まる。availability は、EventBridge の送信にこの暗号化コンテキストが載らないなら拒まれる（未確認。上の「保証しないもの」）。cost は変わらない | 手順のとおり条件なしにする（ADR-0301 の例外を要する） | ops-notification の sandbox で EventBridge の送信が拒まれたとき。AWS が手順を改めたとき。AWS Provider の major 更新時（ADR-0206 決定12） |
