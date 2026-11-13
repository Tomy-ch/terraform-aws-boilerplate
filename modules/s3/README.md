# s3

S3 の Bucket と、その不変条件を持つ一般 module（ADR-0102 決定12・15）。どの usecase でも同じ形で成立する機構だけを持ち、usecase ごとの差異を入力の切り替えで抱えない。

## 持つもの

| resource | 値 |
| --- | --- |
| `aws_s3_bucket` | 名前は `<name>-<account_id>-<region>-an`、`bucket_namespace = "account-regional"`、`force_destroy = false`。完全な名前が63文字を超えると plan で落ちる |
| `aws_s3_bucket_public_access_block` | 4項目すべて `true` |
| `aws_s3_bucket_ownership_controls` | `BucketOwnerEnforced`（ACL を無効にする） |
| `aws_s3_bucket_versioning` | `Enabled` |
| `aws_s3_bucket_lifecycle_configuration` | Bucket 全体（空の filter）に規則1つ。上書きされた版を `noncurrent_version_retention_days` の日数で消し、失効した delete marker を消し、未完了の multipart upload を1日で中止する。現行版は失効させず、storage class を移さない（`usecases/state-backend/docs/adr/0103-expire-noncurrent-state-versions.md`） |
| `aws_s3_bucket_server_side_encryption_configuration` | `aws:kms`（SSE-KMS）で `kms_key_arn` の鍵を名指す、`bucket_key_enabled = false`、`blocked_encryption_types = ["SSE-C"]`。鍵が Bucket と同じ partition・region・アカウントでなければ plan で落ちる |
| `aws_s3_bucket_policy` | Deny だけの文書。(1) TLS でない要求（`aws:SecureTransport` が `false`）を、Bucket とその object のすべての操作について拒否する。(2) object の書き込み（`s3:PutObject`・`s3:DeleteObject`・`s3:DeleteObjectVersion`・`s3:ReplicateObject`・`s3:ReplicateDelete`）を、`object_writers` と `bootstrap_executor` の主体以外に拒否する。(3) 各主体の `keys` の外への書き込みと、`access` の外の操作と `s3:DeleteObjectVersion` とレプリケーションによる書き込みを、その主体に拒否する。(4) 暗号化の方式の header が `aws:kms` でない、鍵 ID の header が `kms_key_arn` でない、方式が `aws:kms` で鍵 ID の header が無い `s3:PutObject` を拒否する。(2)-(4) の Resource は object の ARN だけで、Bucket の設定の Action を含めない。主体は `aws:PrincipalArn` の `ArnNotEquals`・`ArnEquals` で ARN を完全一致で書き、条件キーは `Null` で在ることを確かめてから値を比べる（`usecases/state-backend/docs/adr/0104-state-object-write-deny.md`） |

`kms_key_arn` は KMS の鍵の ARN（`key/<key-id>`）だけを受け、alias の ARN と多リージョンの鍵を拒み、既定値を持たない。多リージョンの鍵（`mrk-`）を拒むのは、`modules/kms` が多リージョンの鍵を作らないからである。S3 は Bucket の暗号化の設定で鍵を検証しないので、同じ場所の鍵であることを precondition で見る。Bucket Key を使わない理由は `usecases/state-backend/docs/adr/0101-sse-kms-with-state-key.md` が持つ。

`object_writers` は書き込みを許す role の map で、1件以上を要り、既定値を持たない。key は Sid に入るので英小文字で始まる英数字に限り、`role_arn` は role の ARN（user・assumed-role・root・ワイルドカードを拒む）で互いに別、`keys` は1つ以上のワイルドカードを含まない key、`access` は `write`（`s3:PutObject` と `s3:DeleteObject`）か `delete`（`s3:DeleteObject` だけ）である。`bootstrap_executor` は bootstrap の間だけ書き込みを許す IAM user（path を持たない名前と `keys`）で、既定は `null`。ARN は module がアカウントと partition から組み、その user は `access = write` で入り、暗号化の方式の header の拒否の例外にも入る。どちらも AWS の Action の文字列や policy の文を受けない（ADR-0204 決定1・2）。

`noncurrent_version_retention_days` は1以上の整数を受け、既定値を持たない。現行版を失効させる消費者は、この module を使わず `internal/` に Bucket を置く（`usecases/state-backend/docs/adr/0201-plan-output-bucket.md`）。

`name` は英小文字・数字・ハイフンだけを受け、S3 が予約している接頭辞を拒む。前半が `<project>-<env>-...`（docs/v1.0-plan/scope.md 第3節）に沿うかは、この module では検査しない。前半は呼び出す usecase が組み、その形は usecase のテストで固定する。

| 出力 | 使う側 |
| --- | --- |
| `bucket_name` | `state-backend`（backend の設定と、env の identity root の段2 の `import` block の id） |
| `bucket_arn` | `state-backend` を経て `deployment-identity`（CI の role の IAM で Resource を state Bucket に絞る） |
| `noncurrent_version_retention_days` | `state-backend` の Contract Test（公開入力が規則の日数に届くこと） |
| `kms_key_arn` | `state-backend` の Contract Test（既定の暗号化が state の鍵を名指すこと） |
| `object_writers` | `state-backend` の Contract Test（`ci_role_arns` と `bootstrap_executor_user_name` が書き込みの例外へ届くこと） |

`bucket_name` は Bucket の属性ではなく、module が組んだ名前を返す。`import` block の id に使えるよう、Bucket を参照しない（参照すると取り込み先との間で循環する）。値が Bucket の名前と一致することは `tests/naming.tftest.hcl` が見る。

Bucket Policy は、テストが mock の下で文書の中身を確かめられるよう `jsonencode` で組む（`mock_provider` の下では `aws_iam_policy_document` の `json` が意味の無い文字列になる）。Bucket に置ける Policy は1つだけなので、TLS の拒否と書き込みの拒否は同じ文書に置く。

## まだ持たないもの

最初の消費者 `state-backend`（#19）の bootstrap の段に合わせて、同じ module に足していく。段1 の CLI が作る構成は段2 で `import` し、plan がそれらに変更を示さないことを確かめる（ADR-0602 決定8）。段1 で CLI が作らないもの（同 決定5）は、段2 の plan に追加として現れる。

| 足すもの | 段 |
| --- | --- |
| S3 サーバーアクセスログの CloudWatch Logs への配送 | #19 の段5 |

段1 の CLI は、この module と同じ名前（`-<account_id>-<region>-an`）と同じ namespace で Bucket を作る必要がある。その手順は [`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の段1 が持つ。

1つの usecase だけが要る attachment（Object Lock、CORS、通知など）は、この module に足さず、その usecase の `internal/` に Bucket ごと置く（ADR-0102 決定15・18）。

## 保証しないもの

- Security の4つの道具（ADR-0501 決定6）のうち、Public Access Block を外した値は Trivy・Checkov・KICS が、ACL を有効にした所有権の値は Checkov（CKV2_AWS_65）だけが落とす。Semgrep の固定した rule には、この module の値に当たるものが無い。TLS の要求を見る検査は KICS の 4bc4dd4c だけを確かめた（Trivy・Checkov・Semgrep は確かめていない）。4bc4dd4c は Statement を `concat` で組むこの module の Bucket Policy を読めないので `policy.tf` で抑止しており、同じ理由で KICS の Bucket Policy の Allow を見る検査も効かない。state Bucket の Bucket Policy が TLS の拒否を持つことは Policy Test の `PLAN-SBW-08` が、Deny 以外の文を持たないことは `PLAN-SBW-09` が、reference の plan で見る
- アクセスログ、通知、リージョン間の複製、MFA delete、IAM Access Analyzer、tag の検査と、Semgrep の版管理の誤検出と、KICS の TLS の検査（Statement を `concat` で組むので KICS が Deny を読めない）は抑止している。理由と撤回条件は各抑止の箇所（`main.tf` / `policy.tf` / `encryption.tf` の先頭と `aws_s3_bucket.this`）が持つ
- `bucket_namespace`・`force_destroy`・所有権・版管理・暗号化の値は `tests/naming.tftest.hcl` が mock の plan で見る。`kms_key_arn` の validation と precondition は `tests/encryption.tftest.hcl` が見る。Bucket Policy の文書は `tests/policy.tftest.hcl` が mock の apply で完全一致で、`object_writers` と `bootstrap_executor` の入力の検査は `tests/writers.tftest.hcl` が見る。lifecycle の規則と入力の検査は `tests/lifecycle.tftest.hcl` が見る。現行版の失効の `days`、filter の `prefix`、付く Bucket は plan では決まらないので、mock の apply で見る。いずれも module の宣言から導いた値の固定であり、Unit Test に属する（ADR-0501 決定9）
- TLS でない要求と、例外に無い主体の書き込みと、state の鍵以外の暗号化が実際の S3 API で拒否されることは、どのテストも見ていない（E2E。ADR-0401 決定4）
- AWS service principal の書き込み。`aws:PrincipalArn` を持たない要求は書き込みの拒否に当たる。書き込みを要る service principal を持つ usecase がこの module を使うときに見直す
- (2)-(4) の Action の外の object の操作（`s3:PutObjectAcl`・`s3:PutObjectTagging`・`s3:DeleteObjectTagging`・`s3:AbortMultipartUpload`）。ACL は無効で、ほかは object の内容を変えない
- multipart の部分の upload と完了の要求で、S3 が暗号化の条件キーを埋めるか、どの形で埋めるか。鍵 ID の形で埋めるなら、(4) が multipart の書き込みを拒む
- lifecycle が実際に日数どおりに版を消すこと（S3 は失効を非同期に行う）
- `noncurrent_version_retention_days` と `kms_key_arn` が既定値を持たないことと、null を受けないこと（`nullable = false`）。テストはどれも値を渡し、null を渡す run は `expect_failures` で受けられないので、どちらを外しても落ちない。見るのはレビューだけである
- lifecycle を版管理の後に作ること（`depends_on`）。作成の順序は mock では現れず、見るテストは無い

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。6.68.0 に固定した複製で `terraform validate` と `terraform test` が通ることを確かめた
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.16.6（`mise.toml`）だけである

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-10-25（lifecycle の項は 2026-11-08、SSE-KMS と Bucket Key の項は 2026-11-12、書き込みの拒否の項は 2026-11-13。AWS Provider 6.70.0 の `aws_s3_bucket_policy` の文書）

- AWS / HashiCorp が publisher の S3 の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=s3&provider=aws` の結果に namespace `aws-ia`・`hashicorp`・`aws` が無い）
- 参照した一次資料: AWS Provider 6.68.0 の schema と文書、Amazon S3 User Guide（[account regional namespace](https://docs.aws.amazon.com/AmazonS3/latest/userguide/gpbucketnamespaces.html)、[SSE-C の既定](https://docs.aws.amazon.com/AmazonS3/latest/userguide/default-s3-c-encryption-setting-faq.html)、[Bucket の命名規則](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html)、[security best practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/security-best-practices.html)、[Bucket Policy の例](https://docs.aws.amazon.com/AmazonS3/latest/userguide/example-bucket-policies.html)、[条件キーを使う Bucket Policy の例](https://docs.aws.amazon.com/AmazonS3/latest/userguide/amazon-s3-policy-keys.html)）、AWS Security Hub の [S3.5・S3.10](https://docs.aws.amazon.com/securityhub/latest/userguide/s3-controls.html)、[S3 Lifecycle の設定例](https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-configuration-examples.html)、AWS Provider 6.70.0 の `aws_s3_bucket_lifecycle_configuration` の文書。SSE-KMS と Bucket Key の項は、Amazon S3 User Guide（[Using SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html)、[Specifying SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/specifying-kms-encryption.html)、[Setting default encryption](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-encryption.html)、[S3 Bucket Keys](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-key.html)）、Amazon S3 API Reference の [PutBucketEncryption](https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutBucketEncryption.html)、AWS Provider 6.70.0 の `aws_s3_bucket_server_side_encryption_configuration` の文書

書き込みの拒否の項は、IAM User Guide（[NotPrincipal](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_elements_notprincipal.html)、[Condition operators](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_elements_condition_operators.html)、[Global condition context keys](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_condition-keys.html)）、Amazon S3 User Guide（[Using SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html) の Requiring server-side encryption、[Specifying SSE-KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/specifying-kms-encryption.html)、[Multipart upload overview](https://docs.aws.amazon.com/AmazonS3/latest/userguide/mpuoverview.html)）、KMS Developer Guide（[AWS KMS keys](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html)）

採用した知見:

- account regional namespace（AWS が security best practice とする）。他のアカウントが同じ名前を取れない
- SSE-C を `blocked_encryption_types` で明示して塞ぐ（AWS の既定に頼らない。ADR-0207 決定5）
- customer managed key による SSE-KMS で、鍵を ARN で名指す（[SSE-KMS の既定の暗号化](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-encryption.html) が alias ではなく ARN を勧める）
- inline の `versioning` / `acl` / `policy` などを使わず、個別の resource で書く（Provider が deprecated とする）
- TLS の要求は、`aws:SecureTransport` が `false` の要求を `Principal = "*"` で拒否する Deny で書く（Security Hub S3.5 の修正例と、S3 User Guide の HTTPS に限る例と同じ形）
- 書き込みの例外は、`NotPrincipal` ではなく `Principal = "*"` と `aws:PrincipalArn` の `ArnNotEquals` の Deny で書く（IAM User Guide「NotPrincipal」が新しい resource-based policy に `NotPrincipal` を勧めず、この形を代わりに示す）
- 特定の鍵を求める条件は、`s3:x-amz-server-side-encryption-aws-kms-key-id` に鍵の ARN を書く（S3 User Guide「Using SSE-KMS」の Requiring server-side encryption）。否定の演算子は条件キーが無い要求で真になるので、`Null` を併記して header の無い要求（multipart の部分の upload）を拒まない
- 版管理を持つ Bucket に lifecycle を置く（Security Hub S3.10）。上書きされた版の失効と失効した delete marker の除去を1つの規則に書く（S3 Lifecycle の設定例の Example 7 と同じ形）。lifecycle の resource は版管理の resource の後に作る（Provider の文書の `depends_on` の例）

採用しなかった知見と理由:

- 書き込みの例外を `NotPrincipal` で書く: IAM User Guide が新しい resource-based policy に勧めない（`usecases/state-backend/docs/adr/0104-state-object-write-deny.md` 案B）
- 鍵 ID の header が無い要求も拒む（`Null` を併記しない）: multipart の部分の upload を拒みうる（同 案D）

- global namespace と推測しにくい名前: 削除した名前を他人が取り直せる問題が残る
- MFA delete: root しか有効にできず、lifecycle と併用できない（[MFA delete](https://docs.aws.amazon.com/AmazonS3/latest/userguide/MultiFactorAuthenticationDelete.html)。記録は `usecases/state-backend/docs/adr/0102-no-mfa-delete-or-object-lock.md`）
- リージョン間の複製（security best practices の「Consider S3 Cross-Region Replication」）: 今の消費者 `state-backend` の責務外（`usecases/state-backend/README.md` の unsupported）。Checkov の CKV_AWS_144 はこの判断で抑止している。複製を要る usecase がこの module を使うときに見直す
- `aws_iam_policy_document`: mock の下で文書を確かめられない
- S3 Bucket Key（[S3 Bucket Keys](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-key.html)）: 文書は費用と KMS の要求数の削減を述べ、推奨とはしていない。有効にすると bucket-level key の有効期間は Key Policy を評価せずに復号が続きうり、暗号化コンテキストが Bucket の ARN になる（記録は `usecases/state-backend/docs/adr/0101-sse-kms-with-state-key.md` 決定6）
- `s3:TlsVersion` で TLS の最低版を求める: AWS は 2024-02 に AWS service API endpoint での TLS 1.0 / 1.1 の受け付けを終えると告知しており、1.2 より上の値は公式推奨で一意に決まらない
- Policy と Public Access Block の間の `depends_on`: Deny だけの Policy は public に当たらず、順序を要する根拠を一次資料で確かめられない
- 条件キーの例（Example 6）が勧める `aws:PrincipalIsAWSService = "false"` を Deny に足す形: AWS service が代わりに呼ぶ要求では `aws:SecureTransport` が落ちるので、足さないと service principal の読み書きを塞ぎうる。足すと Deny の範囲が狭まる。今の消費者（`state-backend`）には service principal の読み書きが無いので、Security Hub S3.5 の形を採る。service principal が読み書きする usecase がこの module を使うときに見直す

## 公式推奨からの逸脱（ADR-0207 決定11）

この module を使うすべてのユースケースが、次の逸脱を引き継ぐ。

| 対象 | 理由 | この module を使う他の usecase への影響 | 見直し条件 |
| --- | --- | --- | --- |
| MFA delete を掛けない（KICS c5b31ab9） | `usecases/state-backend/docs/adr/0102-no-mfa-delete-or-object-lock.md` | この module の Bucket は MFA delete を持たない | 同 ADR が変わったとき。`main.tf` 先頭の `kics-scan disable` も同時に見直す |
| リージョン間の複製を持たない（Checkov CKV_AWS_144） | `usecases/state-backend/README.md` の unsupported | リージョン障害への備えは、この module を使う usecase が持つ | 複製を要る usecase がこの module を使うとき。`main.tf` の `#checkov:skip=CKV_AWS_144` も同時に見直す |
