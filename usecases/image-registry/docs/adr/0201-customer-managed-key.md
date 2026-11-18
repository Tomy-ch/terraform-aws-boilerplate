# ADR-0201: repository をアカウントに1本の CMK で暗号化する

- Status: Accepted
- Date: 2026-11-19
- Scope: image-registry
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0501

## Context

ECR の暗号化の既定は AES256 で、FSBP と CIS AWS Foundations Benchmark はそれを不合格にしない。ECR.5（CMK による暗号化）は FSBP に含まれない。一方、このリポジトリの Security の道具のうち Checkov（CKV_AWS_136）と KICS（0e32d561）は CMK を要求し、AES256 では抑止が2つ要る。暗号化の設定は repository の作成の後に変えられない。

## Decision

1. repository を `encryption_type = "KMS"` で、このユースケースが持つ CMK（`modules/kms`）で暗号化する。暗号化方式を入力にしない。
2. 鍵はアカウントに1本とし、全 repository が共有する。
3. 鍵の Key Policy は、ECR 経由（`kms:ViaService = ecr.<region>.amazonaws.com`）の `kms:CreateGrant`・`kms:DescribeKey`・`kms:RetireGrant` を、入力 `registry_manager_role_arn` の role だけに許す（`modules/kms` の `via = "ecr"`）。ECR は repository の作成者の権限で grant を作り、削除で廃止する（Amazon ECR User Guide「Encryption at rest」）。

## 検討した代替案

### 案A: AES256 を明示する

公式推奨からの逸脱ではないが、Checkov と KICS の抑止が要る。抑止できない値で満たせる所見を抑止しない（repository-wide/ADR-0501 決定13）。ECR.5 も満たさない。

### 案B: AWS 管理鍵（`aws/ecr`）

Checkov は通るが、KICS は CMK の ID を要求する。鍵を使う主体を Key Policy で名指せない。

### 案C: repository ごとに鍵

費用が repository の数に比例し、保証は増えない。

## 保証範囲

- 保証すること: repository が CMK で暗号化され、その鍵の grant を作れるのが apply の role の ECR 経由の要求だけであること
- 保証しないこと: ECR が Key Policy のこの形で grant を作れること（実 apply でしか見えない）

## 検証方法

- Unit Test: `modules/kms/tests/policy.tftest.hcl` の `roles_via_ecr` が Key Policy の文を見る
- Policy Test（`plan.kms_key_policy`）: Key Policy の委任と Principal の形（`PLAN-KMS-*`）
- Security: Checkov CKV_AWS_136、KICS 0e32d561、Trivy AVD-AWS-0033

## 影響

鍵の費用と、push・pull ごとの KMS の要求が掛かる。workload root の apply の role は、IAM の側にも `kms:RetireGrant` を要る（#222）。

## 見直し条件

- ECR が暗号化の設定の変更を許したとき
- Security の道具が AES256 を所見としなくなったとき
