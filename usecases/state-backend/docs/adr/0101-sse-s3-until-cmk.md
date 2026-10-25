# ADR-0101: bootstrap の段5 まで state Bucket を SSE-S3 で暗号化する

- Status: Accepted
- Date: 2026-10-25
- Scope: state-backend
- Related: repository-wide/ADR-0207, repository-wide/ADR-0602, repository-wide/ADR-0501

## Context

AWS は、S3 の暗号化に customer managed key（CMK）による SSE-KMS を使うと、鍵の権限・ローテーション・監査を制御できるとする。Trivy は CMK を使わない Bucket を AWS-0132 として報告する。

bootstrap の段1 は AWS CLI で state Bucket を作り、段2 はそれを `import` block で取り込んで `terraform plan` が変更なしを示すまで突合する（repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定4・6・8）。CLI で作ってよいのは、Terraform が state を保存するために要るものだけである（同 決定5）。CMK と key policy はそれに当たらない。

## Decision

1. bootstrap の段1 から段4 まで、state Bucket の既定の暗号化を SSE-S3（`AES256`）とし、SSE-C を拒否する。
2. 段5 で CMK を作り、SSE-KMS へ切り替える。そのとき backend に `kms_key_id` を書く。
3. 決定1 を公式推奨からの逸脱として記録し（repository-wide/ADR-0207 (default-value-policy) 決定11）、Trivy の AWS-0132 を `modules/s3/encryption.tf` に限って抑止する（repository-wide/ADR-0501 (development-tooling-composition) 決定13）。

## 検討した代替案

### 案A: 段1 の CLI で CMK を作る

key policy の JSON が、検査を通らないまま実在する。repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定5 が退ける。

### 案B: AWS 管理鍵（`aws/s3`）で SSE-KMS にする

鍵の権限で読み手を絞れない点は SSE-S3 と変わらない。段5 で CMK へ移る際に、もう一度暗号化の設定が変わる。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 高（ただし未検査の key policy） | 低 | 低（段5 まで） |
| Testability | 低 | 中 | 高 |
| Contract Clarity | 低 | 中 | 高 |
| Maintainability | 低 | 低 | 高 |
| Scope Control | 低（決定5 に反する） | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

段5 までの間、鍵の権限で state の読み手を絞ること。

## 保証範囲

- 保証すること: 段5 までの state Bucket が SSE-S3 で暗号化され、SSE-C の書き込みを拒否すること
- 保証しないこと: 段5 までの間、state を読める主体を KMS の権限で限ること

## 検証方法

- Unit Test: `modules/s3/tests/naming.tftest.hcl` の `bucket_attributes` が、暗号化の値（`AES256`）と SSE-C の拒否を mock の plan で確かめる
- Security: `make trivy-config` が AWS-0132 を `modules/s3/encryption.tf` 以外の path で報告しないこと。抑止の宣言は `.trivyignore.yaml` が持つ

## 影響

段5 より前に他の usecase が `modules/s3` を使うと、その Bucket も SSE-S3 になる。その変更の中で抑止を見直す（`.trivyignore.yaml` の撤回条件）。

## 見直し条件

- bootstrap の段5 で SSE-KMS へ切り替えたとき（本 ADR を書き換え、抑止を外す）
- 段5 より前に state-backend 以外の usecase が `modules/s3` を使うとき
