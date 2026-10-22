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

`name` は英小文字・数字・ハイフンだけを受け、S3 が予約している接頭辞を拒む。前半が `<project>-<env>-...`（docs/v1.0-plan/scope.md 第3節）に沿うかは、この module では検査しない。前半は呼び出す usecase が組み、その形は usecase のテストで固定する。

| 出力 | 使う側 |
| --- | --- |
| `bucket_name` | #19（backend の設定と bootstrap の手順） |
| `bucket_arn` | #20（CI の role の IAM で Resource を state Bucket に絞る） |

## まだ持たないもの

最初の消費者 `state-backend`（#19）の bootstrap の段に合わせて、同じ module に足していく。段1 の CLI が作る構成を段2 で `import` し、plan が変更なしを示す必要があるので（ADR-0602 決定8）、段3 以降で足すものを先に持てない。

| 足すもの | 段 |
| --- | --- |
| Bucket Policy（TLS を要求する Deny） | #19 の段3-4 |
| SSE-KMS への切り替え、root ごとの書き込みの拒否（ADR-0604 決定9）、lifecycle、S3 サーバーアクセスログの CloudWatch Logs への配送 | #19 の段5 |

段1 の CLI は、この module と同じ名前（`-<account_id>-<region>-an`）と同じ namespace で Bucket を作る必要がある。その手順は #19 が `docs/setup/bootstrap.md` に書く。

Bucket Policy は、テストが mock の下で文書の中身を確かめられるよう `jsonencode` で組む（`mock_provider` の下では `aws_iam_policy_document` の `json` が意味の無い文字列になる）。

1つの usecase だけが要る attachment（Object Lock、CORS、通知など）は、この module に足さず、その usecase の `internal/` に Bucket ごと置く（ADR-0102 決定15・18）。

## 保証しないもの

- TLS を要求する Policy が入るまで、この module は平文の通信を拒否しない
- Public Access Block・所有権・版管理・暗号化の値は Trivy（`make trivy-config`）が手元で見る。CI の trivy-config は `.tf` の変更では起動しない
- `bucket_namespace` と `force_destroy` の値は `tests/naming.tftest.hcl` が mock の plan で見る。Policy Test（#74）が配線されたら、そちらへ移す

## 版

- AWS Provider は `>= 6.68.0, < 7.0.0`。6.68.0 に固定した複製で `terraform validate` と `terraform test` が通ることを確かめた
- Terraform は `>= 1.7.0, < 2.0.0`。下限はテストが使う `mock_provider` の要求であり、実行して確かめたのは 1.16.2（`mise.toml`）だけである

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-10-22

- AWS / HashiCorp が publisher の S3 の Registry module は見つからなかった（`https://registry.terraform.io/v1/modules/search?q=s3&provider=aws` の結果に namespace `aws-ia`・`hashicorp`・`aws` が無い）
- 参照した一次資料: AWS Provider 6.68.0 の schema と文書、Amazon S3 User Guide（[account regional namespace](https://docs.aws.amazon.com/AmazonS3/latest/userguide/gpbucketnamespaces.html)、[SSE-C の既定](https://docs.aws.amazon.com/AmazonS3/latest/userguide/default-s3-c-encryption-setting-faq.html)）

採用した知見:

- account regional namespace（AWS が security best practice とする）。他のアカウントが同じ名前を取れない
- SSE-C を `blocked_encryption_types` で明示して塞ぐ（AWS の既定に頼らない。ADR-0207 決定5）
- inline の `versioning` / `acl` / `policy` などを使わず、個別の resource で書く（Provider が deprecated とする）

採用しなかった知見と理由:

- global namespace と推測しにくい名前: 削除した名前を他人が取り直せる問題が残る
- MFA delete: root しか有効にできず、lifecycle と併用できない（[MFA delete](https://docs.aws.amazon.com/AmazonS3/latest/userguide/MultiFactorAuthenticationDelete.html)。#19 で記録する）
- `aws_iam_policy_document`: mock の下で文書を確かめられない

## 公式推奨からの逸脱（ADR-0207 決定11）

この記録は、#19 で `usecases/state-backend/docs/adr/` ができたときにそこへ移す（ADR-0207 決定11 は当該ユースケースの ADR を記録先とする）。

| 対象 | 理由（ユースケース上の制約） | 影響 | 代替案 | 見直し条件 |
| --- | --- | --- | --- | --- |
| KMS（customer managed key）ではなく SSE-S3 で暗号化する（Trivy AWS-0132） | state-backend の bootstrap の段1・段2 は SSE-S3 と決まっており（scope.md 第10.3節）、CMK は段5 で作る | 段5 までは、鍵の権限で読み手を絞れない。この間に他の usecase がこの module を使うと、その Bucket も SSE-S3 になる | 段1 の CLI で CMK を作る（ADR-0602 決定5 が退ける）、AWS 管理鍵（段1 の構成と一致せず、段2 の plan が変更なしにならない） | #19 の段5 で SSE-KMS に切り替えたとき。それより前に他の usecase がこの module を使うとき。`.trivyignore.yaml` の抑止も同時に見直す |
