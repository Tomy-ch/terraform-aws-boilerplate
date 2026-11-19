# ADR-0202: repository policy は Allow を持たず、push の role 以外の書き込みとイメージの削除を拒否する

- Status: Accepted
- Date: 2026-11-19
- Scope: image-registry
- Related: repository-wide/ADR-0301, image-registry/ADR-0102

## Context

ECR の repository へのアクセスは、同じアカウントの IAM の policy と repository policy の組で決まる。repository の pull は同じアカウントの Compute の Execution Role が IAM で持つ（#94。越境の pull は無い）。不変タグは同じタグの上書きを止めるが、タグを消してから同じタグで push することは止めない。同じアカウントの別の主体が IAM で書き込みの権限を得れば、push の role を通らずにイメージを置ける。

## Decision

1. repository policy は Allow を持たない。
2. 次の2つの Deny を持つ。Principal はアカウント（`arn:<partition>:iam::<account>:root`）とする。
   - `ecr:CompleteLayerUpload`・`ecr:InitiateLayerUpload`・`ecr:PutImage`・`ecr:UploadLayerPart` を、`aws:PrincipalArn` がその repository の push の role でない主体に拒否する
   - `ecr:BatchDeleteImage` を条件なしに拒否する。イメージを消すのは lifecycle policy だけとする
3. repository policy は repository に1つで、repository と同じファイルに置く。KICS の 69e7c320 は同じファイルの policy だけを見る。

## 検討した代替案

### 案A: repository policy を置かない

同じアカウントの IAM の権限だけで書き込みと削除ができる。KICS の 69e7c320 が所見を出す。

### 案B: Principal を `"*"` にする

アカウントの外の主体も覆えるが、Trivy の AVD-AWS-0032 は Effect を見ずに Principal `"*"` を所見とし、抑止が要る。アカウントの外の主体は Allow が無いので元から書けない。

## 保証範囲

- 保証すること: repository policy の形として、push の role 以外の書き込みとイメージの削除が拒否されること
- 保証しないこと: Principal にアカウントを置いた Deny が、アカウントの全 IAM 主体に効くこと。`ecr:BatchDeleteImage` の Deny が lifecycle による削除を止めないこと。どちらも実 AWS でしか見えない

## 検証方法

- Unit Test: `internal/repository/tests/repository.tftest.hcl` の `repository_policy_names_this_repository_and_publisher`
- Policy Test（`plan.image_registry`）: Allow の不在（`PLAN-ECR-07`）、書き込みの Deny の Action・Resource・Principal・条件の形（`PLAN-ECR-08`）、削除の Deny（`PLAN-ECR-09`）、repository ごとに1つ（`PLAN-ECR-10`）
- Security: KICS 69e7c320、Trivy AVD-AWS-0032、Checkov CKV_AWS_32

## 影響

イメージを手で消すには repository policy を変える Pull Request が要る。

## 見直し条件

- 別のアカウントからの pull を扱うとき（#94 が変わったとき）
- 実 AWS で、決定2 の Deny が効かないか、lifecycle を止めると分かったとき
