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
| `users.roles` | 鍵を使う IAM role の ARN と、経由するサービス（`via`）の組。同じアカウントと partition の role だけを受ける（ARN が plan で決まれば plan で、決まらなければ apply で落ちる） |
| `users.services.logs` | 鍵で暗号化する CloudWatch Logs の Log Group の名前。module が鍵と同じアカウントと region の ARN を組む |

`users` は合わせて1件以上を要る。管理の委任は暗号操作を含まないので、利用者の無い鍵は誰も使えない。

| 出力 | 使う側 |
| --- | --- |
| `key_arn` | 鍵を使う resource（S3 の暗号化と Bucket Policy の条件、backend の `kms_key_id`、Log Group の `kms_key_id`）と、鍵を管理する role の IAM の Resource |
| `alias_name` | 鍵の ARN が無いときに鍵を名指す消費者（`state-backend` の `state_key_alias_name`）。入力だけから決まるので、鍵より前に参照できる |

消費者は alias ではなく `key_arn` で鍵を指す。alias を管理できる主体は、alias の付け替えで暗号化先をすり替えられる（KMS「Default key policy」の Warning）。alias の名前は人の識別と、手元の手順が鍵の ARN を引くことと、鍵の ARN が無いときに消費者の IAM が `kms:ResourceAliases` の条件で鍵を限ることに使う。名前を変える変更は、それを引く消費者の手順の変更を伴う。

### Key Policy の文書

文書は、テストが mock の下で中身を確かめられるよう `jsonencode` で組む。利用者が0件の種類の文は出さない。principal と Log Group は並べ替えて重複を除くので、入力の順序で文書は変わらない。`Resource` はすべて `"*"`（Key Policy ではこの鍵を指す）。

| Sid | Principal | Action | Condition |
| --- | --- | --- | --- |
| `AccountKeyAdministration` | アカウント（`arn:<partition>:iam::<account>:root`） | 鍵の管理の21 Action（`policy.tf` の `administration_actions`）。暗号操作と `kms:CreateGrant` を含まない | 無し |
| `RolesViaS3` | `users.roles` のうち `via = "s3"` の role | `kms:Decrypt`・`kms:GenerateDataKey` | `kms:ViaService = s3.<region>.amazonaws.com` |
| `LogsService` | `logs.<region>.amazonaws.com` | `kms:Decrypt`・`kms:Describe*`・`kms:Encrypt`・`kms:GenerateDataKey*`・`kms:ReEncrypt*` | `ArnEquals kms:EncryptionContext:aws:logs:arn` に `users.services.logs` の Log Group の ARN（`:*` を付けない） |

`via` の語彙と、それぞれが許す Action:

| `via` | Action | 根拠 |
| --- | --- | --- |
| `s3` | `kms:GenerateDataKey`（書き込み）・`kms:Decrypt`（読み出し。multipart の書き込みも要る） | Amazon S3 User Guide「Using server-side encryption with AWS KMS keys」。Terraform の S3 backend の文書は `kms:Encrypt` も挙げるが、S3 の文書を採る（ADR-0207 決定3） |

`kms:ViaService` は IAM の主体が AWS のサービスを経由して呼ぶ要求にだけ当たる条件で、role はこの鍵を直接は使えない。CloudWatch Logs の文はサービス principal に対する許可で、暗号化コンテキストで Log Group を名指して限る。

Key Policy の文書が 32768 文字（KMS の `PutKeyPolicy`）を超えると落ちる（role の ARN が plan で決まれば plan で、決まらなければ apply で）。

## まだ持たないもの

最初の消費者 `state-backend`（#19）が使うものだけを持つ。ほかは、それを使う消費者が着手するときに、同じ形（`via` の値か `users.services` の項と、その文）で足す。

| 足すもの | 足す消費者 |
| --- | --- |
| `via = "logs"`・`via = "sns"`、SNS へ publish するサービス（CloudWatch Alarm・EventBridge）の文 | ops-notification（#25） |
| CloudTrail の文（`kms:DescribeKey` は `aws:SourceArn` で trail を限る形） | account-baseline（#22）・audit-evidence（#23） |
| 他のアカウントへの許可と `aws_kms_grant` | data-refresh（#59）。方式（Key Policy か grant か）は未決 |
| S3 の暗号化コンテキストでの Bucket の限定 | S3 Bucket Key の有無が決まったとき（有無で object ARN と bucket ARN が入れ替わる） |
| us-east-1 など別の region の鍵 | その region の鍵を要る消費者。この module は呼ばれた provider の region に1本作る |
| Key Policy の置き換えの後に決まる鍵の ARN の出力 | 同じ apply で鍵を作り、その鍵で暗号化を始める消費者。`key_arn` は鍵の作成で決まるので、それを参照する resource は既定の Key Policy の間に鍵を使いうる |

## 保証しないもの

- 暗号操作は、Key Policy で名指した role とサービスだけに許す。IAM で `AdministratorAccess` を持つ人の主体も、そのままでは復号できない。名指されない主体が鍵で暗号化された object を読むには、委任された `kms:PutKeyPolicy` で文を足す必要があり、その変更は次の apply で戻る。bootstrap の途中の一時 IAM user もこれに当たる（state を SSE-KMS にした後の段5 の手順の扱いは #19 が持つ）
- Key Policy の Deny は持たない。鍵の管理（`kms:PutKeyPolicy` を含む）はアカウントへ委任しているので、どの主体が Key Policy を書き換えられるかは IAM が決める。書き込みを拒む Deny は、置くなら消費者の IAM の側に置く（ADR-0604 決定10）。deployment-identity は、鍵の ARN を参照できるまで Deny を持たない
- 入力から導いた値（鍵の属性、alias の名前、Key Policy の文書、validation と precondition）は `tests/` が mock の plan と apply で見る。Unit Test に属する（ADR-0501 決定9）。`aws_kms_key` の inline の `policy` を使わないことは、plan で値が決まらない（computed）ので、テストではなくこのファイルの構成が持つ
- Key Policy の文書の security invariant は `conftest/plan` の `plan.kms_key_policy`（`PLAN-KMS-*`）が持ち、`state-backend` の reference の root の plan に掛かる。`aws_kms_key_policy` は `conftest/plan/decl.json` の `required_documents` に在るので、Key Policy が入力から消えると Policy が落ちる
- アカウントへの委任の文を除く部分 wildcard の Action（`kms:Describe*` など）と、Key Policy の `Resource "*"` を見る検査は無い
- 鍵は作成の時点では既定の Key Policy（アカウントへの `kms:*`）を持ち、alias の作成の後に `aws_kms_key_policy` が置き換える。alias は鍵の ARN を持たない消費者が鍵を名指す識別子（`alias_name`）なので、置き換えが落ちても alias の無い鍵を残さない順にしている。置き換えが落ちたときは、alias の付いた鍵が既定の Key Policy のまま次の apply まで残る。alias の作成が落ちたときは、alias の無い鍵が残る。この順序を見るテストは無い。`aws_kms_key_policy` を destroy すると、Provider は既定の Key Policy を書き戻す（Provider の `aws_kms_key_policy` の NOTE）。destroy と、`moved` を伴わない address の変更（置換）は、`conftest/plan` の `plan.stateful_protection` が CI の plan と apply で拒む。CI の plan と apply が掛かるのは、CI の入口を持つ root だけである。`removed`（`destroy = false`）は state から外すだけで、鍵の側は変わらない
- `plan.kms_key_policy` の Rego のテストが使う委任の文は、`policy.tf` の列挙を手で写したもので、一致は検査しない。この module の出力が `PLAN-KMS-*` を通ることは、`state-backend` の reference の root の plan が確かめる
- CloudWatch Logs の文は、CloudWatch Logs の文書の例のとおり `kms:Describe*` を暗号化コンテキストの条件と同じ文に置く。KMS の文書は、`kms:EncryptionContext:*` の条件を受ける操作に `DescribeKey` を挙げておらず、それ以外の操作にこの条件を使うと拒否されるとする。この食い違いの下で CloudWatch Logs への配送と読み出しが通ることは、どのテストも見ていない
- 実際の AWS に対して、次はどのテストも確かめていない。最初の消費者の CI の apply と、その後の CI の plan が `No changes.` を示すことで確かめる（#19）。sandbox での複製の作成・破棄、残存の検出、期待残存（削除の猶予中の鍵）の照合は、sandbox のアカウント（#21）と掃除の CI（#76）が揃うまで行わない
  - lockout の安全チェックが、委任の文と、鍵を管理する role の IAM で通ること
  - Key Policy の読み戻しが宣言と一致し、差分が出続けないこと（要素1つの Principal の正規化、role の作り直しで principal が一意 ID に置き換わること）
  - 作成直後の role を Principal に置く `PutKeyPolicy` が、IAM の結果整合で落ちないこと
  - `via = "s3"` の2つの Action で、S3 backend が state と lock file を読み書きできること
  - 鍵を管理する role が、Provider が鍵・Key Policy・alias の作成・読み取りで呼ぶ API を、委任の21 Action と IAM で持てること。削除で呼ぶ API は、最初の消費者の管理 role が持たないので（deployment-identity/ADR-0201 の「与えないもの」）、その経路では確かめない
- 層検査（attachment の同居、`source` の方向）は未配線である（AGENTS.md「現在の配線状態」）。配線した後に、この module も検査の対象として確かめる
- `aws` 以外の partition でのサービス principal と `kms:ViaService` の値は確かめていない
- Security の4つの道具のうち KICS の3件（鍵の inline の policy、IAM Access Analyzer、tag）は抑止している。理由と撤回条件は `main.tf` と `policy.tf` の先頭が持つ

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。引数と既定値は v6.68.0 の Provider の文書で確かめた。`terraform test` を走らせたのは、lock の無いこの module で init が選んだ 6.72.0 だけである
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.16.6（`mise.toml`）だけである

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-07

- AWS / HashiCorp が publisher の KMS の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=kms&provider=aws` の結果に namespace `aws-ia`・`hashicorp`・`aws`・`aws-samples` が無い）
- 参照した一次資料: AWS Provider v6.68.0 の `aws_kms_key`・`aws_kms_key_policy`・`aws_kms_alias`・`aws_partition`・`aws_region` の文書、AWS KMS Developer Guide（[Default key policy](https://docs.aws.amazon.com/kms/latest/developerguide/key-policy-default.html)、[Least-privilege permissions](https://docs.aws.amazon.com/kms/latest/developerguide/least-privilege.html)、[AWS KMS condition keys](https://docs.aws.amazon.com/kms/latest/developerguide/conditions-kms.html)）、AWS KMS API Reference（[CreateAlias](https://docs.aws.amazon.com/kms/latest/APIReference/API_CreateAlias.html)、[PutKeyPolicy](https://docs.aws.amazon.com/kms/latest/APIReference/API_PutKeyPolicy.html)）、Amazon S3 User Guide（[SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html)、[S3 Bucket Keys](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-key.html)）、Amazon CloudWatch Logs（[Encrypt log data using AWS KMS](https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/encrypt-log-data-kms.html)、[CreateLogGroup](https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_CreateLogGroup.html)）、Terraform の [S3 backend](https://developer.hashicorp.com/terraform/language/backend/s3)、[AWS KMS の料金](https://aws.amazon.com/kms/pricing/)

採用した知見:

- Key Policy は `aws_kms_key_policy` で持ち、`aws_kms_key` の inline の `policy` と併用しない（Provider の NOTE。併用すると互いに上書きする）
- 自動 rotation を有効にし、周期 365 日を明示する（Provider の既定は無効。ADR-0207 決定5）
- サービス principal の許可は、要求元か暗号化コンテキストで限る。CloudWatch Logs は Log Group の ARN を `ArnEquals` で名指す（`:*` を付けない）
- 経由するサービスでの利用は `kms:ViaService` で限る
- `kms:EncryptionContext:*` を `ForAllValues` で条件にしない（暗号化コンテキストの無い要求も通る）
- alias の名前に `alias/aws/` を使わない（AWS 管理鍵に予約）

採用しなかった知見と理由:

- アカウントへの `kms:*` の委任（既定の Key Policy）: 鍵の実効的な境界が IAM へ移る。下の「公式推奨からの逸脱」
- console の key administrators の集合（`kms:Create*` など部分 wildcard）をアカウントへ委任する形: `kms:Create*` は `kms:CreateGrant` を含み、IAM で `kms:CreateGrant` を持つ主体が grant で暗号操作を得られる。部分 wildcard は AWS が API を足すと黙って広がる
- CloudWatch Logs の「Log Group ごとに別の鍵」: 鍵の単位は消費者が決める。この module は単位を決めない
- `aws_iam_policy_document`: mock の下で文書を確かめられない
- `deletion_window_in_days` を短くする: 猶予は保持の要件ではなく誤削除への備えであり、削除の猶予中の鍵は課金されない

## 公式推奨からの逸脱（ADR-0207 決定11）

この module を使うすべてのユースケースが、次の逸脱を引き継ぐ。暗号操作を Key Policy に限る判断そのものは [ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定12 が持つ。

| 対象 | 理由 | 由来 | 影響（security / availability / cost） | 代替案 | 見直し条件 |
| --- | --- | --- | --- | --- | --- |
| アカウントへの委任を、既定の Key Policy の `kms:*` ではなく、鍵の管理の21 Action（暗号操作と `kms:CreateGrant` を含まない）にする | 暗号操作を Key Policy に限る理由は ADR-0301 案D。この module の分は、管理の Action を部分 wildcard ではなく列挙すること（ADR-0301 決定8 の Action wildcard の禁止にも当たらない） | ユースケースの制約ではなく、repository-wide/ADR-0301 決定12（記録は同 ADR の案D） | 原則の分は ADR-0301 案D。この module の分: security は、鍵を使う主体はすべて `users` で名指し、委任の文が暗号操作を含まないことは `PLAN-KMS-08` が見る。availability は、AWS が鍵の管理の API を足したとき、列挙を直すまでその操作を使えない | 原則の分は ADR-0301 案D。この module の分: console の key administrators の集合（部分 wildcard）の委任。採らない理由は「採用しなかった知見と理由」 | 原則の分は ADR-0301 の見直し条件。この module の分: AWS が鍵の管理の API を足したとき。AWS Provider の major 更新時（ADR-0206 決定12） |
