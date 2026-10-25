# s3

S3 の Bucket と、その不変条件を持つ一般 module（ADR-0102 決定12・15）。どの usecase でも同じ形で成立する機構だけを持ち、usecase ごとの差異を入力の切り替えで抱えない。

## 持つもの

| resource | 値 |
| --- | --- |
| `aws_s3_bucket` | 名前は `<name>-<account_id>-<region>-an`、`bucket_namespace = "account-regional"`、`force_destroy = false`。完全な名前が63文字を超えると plan で落ちる |
| `aws_s3_bucket_public_access_block` | 4項目すべて `true` |
| `aws_s3_bucket_ownership_controls` | `BucketOwnerEnforced`（ACL を無効にする） |
| `aws_s3_bucket_versioning` | `Enabled` |
| `aws_s3_bucket_server_side_encryption_configuration` | `AES256`（SSE-S3）、`blocked_encryption_types = ["SSE-C"]` |
| `aws_s3_bucket_policy` | TLS でない要求（`aws:SecureTransport` が `false`）を、Bucket とその object のすべての操作について拒否する Deny 1文 |

`name` は英小文字・数字・ハイフンだけを受け、S3 が予約している接頭辞を拒む。前半が `<project>-<env>-...`（docs/v1.0-plan/scope.md 第3節）に沿うかは、この module では検査しない。前半は呼び出す usecase が組み、その形は usecase のテストで固定する。

| 出力 | 使う側 |
| --- | --- |
| `bucket_name` | `state-backend`（backend の設定と、bootstrap の段2 の `import` block の id） |
| `bucket_arn` | `state-backend` を経て #20（CI の role の IAM で Resource を state Bucket に絞る） |

`bucket_name` は Bucket の属性ではなく、module が組んだ名前を返す。`import` block の id に使えるよう、Bucket を参照しない（参照すると取り込み先との間で循環する）。値が Bucket の名前と一致することは `tests/naming.tftest.hcl` が見る。

Bucket Policy は、テストが mock の下で文書の中身を確かめられるよう `jsonencode` で組む（`mock_provider` の下では `aws_iam_policy_document` の `json` が意味の無い文字列になる）。Bucket に置ける Policy は1つだけなので、段5 の書き込みの拒否も同じ文書へ文を足す。

## まだ持たないもの

最初の消費者 `state-backend`（#19）の bootstrap の段に合わせて、同じ module に足していく。段1 の CLI が作る構成は段2 で `import` し、plan がそれらに変更を示さないことを確かめる（ADR-0602 決定8）。段1 で CLI が作らないもの（同 決定5）は、段2 の plan に追加として現れる。

| 足すもの | 段 |
| --- | --- |
| SSE-KMS への切り替え、root ごとの書き込みの拒否（ADR-0604 決定9）、lifecycle、S3 サーバーアクセスログの CloudWatch Logs への配送 | #19 の段5 |

段1 の CLI は、この module と同じ名前（`-<account_id>-<region>-an`）と同じ namespace で Bucket を作る必要がある。その手順は [`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の段1 が持つ。

1つの usecase だけが要る attachment（Object Lock、CORS、通知など）は、この module に足さず、その usecase の `internal/` に Bucket ごと置く（ADR-0102 決定15・18）。

## 保証しないもの

- Security の4つの道具（ADR-0501 決定6）のうち、Public Access Block を外した値は Trivy・Checkov・KICS が、ACL を有効にした所有権の値は Checkov（CKV2_AWS_65）だけが落とす。Semgrep の固定した rule には、この module の値に当たるものが無い。TLS の要求を見る検査は、どの道具についても確かめていない
- 暗号化（Trivy AWS-0132、Checkov CKV_AWS_145）、アクセスログ、lifecycle、通知、リージョン間の複製、MFA delete、IAM Access Analyzer、tag の検査と、Semgrep の版管理の誤検出は抑止している。理由と撤回条件は各抑止の箇所（`.trivyignore.yaml`、`main.tf` / `policy.tf` / `encryption.tf` の先頭と `aws_s3_bucket.this`）が持つ
- `bucket_namespace`・`force_destroy`・所有権・版管理・暗号化の値は `tests/naming.tftest.hcl` が mock の plan で見る。Bucket Policy の文書は `tests/policy.tftest.hcl` が mock の apply で見る。どちらも Policy Test（#75）が配線されたら、そちらへ移す
- TLS でない要求が実際の S3 API で拒否されることは、どのテストも見ていない（E2E。ADR-0401 決定4）

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。6.68.0 に固定した複製で `terraform validate` と `terraform test` が通ることを確かめた
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.16.2（`mise.toml`）だけである

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-10-25

- AWS / HashiCorp が publisher の S3 の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=s3&provider=aws` の結果に namespace `aws-ia`・`hashicorp`・`aws` が無い）
- 参照した一次資料: AWS Provider 6.68.0 の schema と文書、Amazon S3 User Guide（[account regional namespace](https://docs.aws.amazon.com/AmazonS3/latest/userguide/gpbucketnamespaces.html)、[SSE-C の既定](https://docs.aws.amazon.com/AmazonS3/latest/userguide/default-s3-c-encryption-setting-faq.html)、[Bucket の命名規則](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html)、[security best practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/security-best-practices.html)、[Bucket Policy の例](https://docs.aws.amazon.com/AmazonS3/latest/userguide/example-bucket-policies.html)、[条件キーを使う Bucket Policy の例](https://docs.aws.amazon.com/AmazonS3/latest/userguide/amazon-s3-policy-keys.html)）、AWS Security Hub の [S3.5](https://docs.aws.amazon.com/securityhub/latest/userguide/s3-controls.html)

採用した知見:

- account regional namespace（AWS が security best practice とする）。他のアカウントが同じ名前を取れない
- SSE-C を `blocked_encryption_types` で明示して塞ぐ（AWS の既定に頼らない。ADR-0207 決定5）
- inline の `versioning` / `acl` / `policy` などを使わず、個別の resource で書く（Provider が deprecated とする）
- TLS の要求は、`aws:SecureTransport` が `false` の要求を `Principal = "*"` で拒否する Deny で書く（Security Hub S3.5 の修正例と、S3 User Guide の HTTPS に限る例と同じ形）

採用しなかった知見と理由:

- global namespace と推測しにくい名前: 削除した名前を他人が取り直せる問題が残る
- MFA delete: root しか有効にできず、lifecycle と併用できない（[MFA delete](https://docs.aws.amazon.com/AmazonS3/latest/userguide/MultiFactorAuthenticationDelete.html)。記録は `usecases/state-backend/docs/adr/0102-no-mfa-delete-or-object-lock.md`）
- リージョン間の複製（security best practices の「Consider S3 Cross-Region Replication」）: 今の消費者 `state-backend` の責務外（`usecases/state-backend/README.md` の unsupported）。Checkov の CKV_AWS_144 はこの判断で抑止している。複製を要る usecase がこの module を使うときに見直す
- `aws_iam_policy_document`: mock の下で文書を確かめられない
- `s3:TlsVersion` で TLS の最低版を求める: AWS は 2024-02 に AWS service API endpoint での TLS 1.0 / 1.1 の受け付けを終えると告知しており、1.2 より上の値は公式推奨で一意に決まらない
- Policy と Public Access Block の間の `depends_on`: Deny だけの Policy は public に当たらず、順序を要する根拠を一次資料で確かめられない
- 条件キーの例（Example 6）が勧める `aws:PrincipalIsAWSService = "false"` を Deny に足す形: AWS service が代わりに呼ぶ要求では `aws:SecureTransport` が落ちるので、足さないと service principal の読み書きを塞ぎうる。足すと Deny の範囲が狭まる。今の消費者（`state-backend`）には service principal の読み書きが無いので、Security Hub S3.5 の形を採る。service principal が読み書きする usecase がこの module を使うときに見直す

## 公式推奨からの逸脱（ADR-0207 決定11）

この module を使うすべてのユースケースが、次の逸脱を引き継ぐ。

| 対象 | 理由 | この module を使う他の usecase への影響 | 見直し条件 |
| --- | --- | --- | --- |
| KMS（customer managed key）ではなく SSE-S3 で暗号化する（Trivy AWS-0132、Checkov CKV_AWS_145） | `usecases/state-backend/docs/adr/0101-sse-s3-until-cmk.md` | 段5 までは、鍵の権限で読み手を絞れない。この間に他の usecase がこの module を使うと、その Bucket も SSE-S3 になる | state-backend の段5 で SSE-KMS に切り替えたとき。それより前に他の usecase がこの module を使うとき。`.trivyignore.yaml` と `main.tf` の `#checkov:skip=CKV_AWS_145` の抑止も同時に見直す |
