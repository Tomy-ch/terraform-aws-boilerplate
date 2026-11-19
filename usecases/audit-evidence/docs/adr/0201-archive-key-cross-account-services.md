# ADR-0201: 証跡の鍵をこのユースケースが持ち、鍵の外のアカウントの trail と Config に型付きで暗号化を許す

- Status: Accepted
- Date: 2026-11-19
- Scope: audit-evidence
- Related: repository-wide/ADR-0301, repository-wide/ADR-0402, audit-evidence/ADR-0102

## Context

組織の trail は管理アカウントが所有し、Config は各アカウントで動く。どちらも log-archive のアカウントの鍵で証跡を暗号化する（Security Hub の CloudTrail.2・S3.17、AWS SRA「All security log S3 buckets SHOULD use SSE-KMS with customer managed keys」）。鍵を記録される側が持つと、鍵を止めて記録を読めなくできる。

CloudTrail の鍵の文は `kms:GenerateDataKey*` を `aws:SourceArn` と `kms:EncryptionContext:aws:cloudtrail:arn` で、`kms:DescribeKey` を `aws:SourceArn` で限る。組織の trail では `aws:SourceAccount` は管理アカウントを指し、trail を限らない（CloudTrail User Guide「Configure AWS KMS key policies for CloudTrail」）。Config の鍵の文は `aws:SourceAccount` で限る（AWS Config Developer Guide「Permissions for the KMS Key」）。

`conftest/plan` の `PLAN-KMS-05` は、サービス principal の要求元が鍵のアカウントの外を指すことを拒む。repository-wide/ADR-0301 (least-privilege-policy) 決定18 は、アカウントをまたぐ許可を、明示的なユースケースとして設計し、条件を型付きの契約で受けるときに許す。決定3 は、これをこのユースケースに当てたものである。

CloudWatch Logs は Log Group ごとに別の鍵を勧める（CloudWatch Logs User Guide「Encrypt log data using AWS KMS」）。

## Decision

1. 証跡の鍵はこのユースケースが `modules/kms` で持つ。Key Policy の CloudTrail の文と Config の文は `modules/kms` の `users.services.cloudtrail`（trail の ARN）と `users.services.config`（アカウントの ID）が組む。
2. CloudTrail の暗号化コンテキストの条件は、trail の ARN の完全一致にする。CloudTrail の文書の例（アカウント単位の wildcard）より狭い。
3. `conftest/plan/decl.json` の `kms_key_policy.cross_account_sources` に、鍵の外のアカウントの要求元を受ける Key Policy の address と、受けるサービスを宣言する。`PLAN-KMS-05` は、宣言した Key Policy の、宣言したサービスだけを principal に持つ文に限って、wildcard の無いアカウントの ID か、アカウントの欄を持つ同じ partition の ARN を受ける。
4. アクセスログの Log Group は、証跡の鍵と別の鍵で暗号化する。

## 検討した代替案

### 案A: SSE-S3 にする

鍵の文も Policy の例外も要らないが、CloudTrail.2・S3.17・SRA から外れ、鍵の権限で読み手を絞れない。

### 案B: `PLAN-KMS-05` をすべての鍵で緩める

宣言の無い鍵でも、別のアカウントのサービスに暗号操作を開けるようになる。

### 案C: 鍵を `internal/` に置く

CloudTrail の文は `modules/kms` の README がこのユースケースと account-baseline の消費者のために予定している。鍵の宣言が2箇所になる。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 低 | 低 | 高 | 高 |
| Testability | 高 | 高 | 中 | 高 |
| Contract Clarity | 高 | 低 | 中 | 高 |
| Maintainability | 高 | 中 | 低 | 中 |
| Scope Control | 中 | 低 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

Policy で、どの外のアカウントを受けてよいかを限ること。受けるアカウントは入力の値で、`PLAN-KMS-05` は値の形だけを見る。

## 保証範囲

- 保証すること: 証跡の鍵の Key Policy の CloudTrail の文と Config の文が、入力の trail とアカウントだけを要求元にすること。宣言の外の Key Policy と、宣言の外のサービスを並べた文が、鍵の外のアカウントを受けないこと。Bucket Policy と Key Policy が名指す trail が一致すること
- 保証しないこと: 完全一致の暗号化コンテキストで組織の trail の配信が通ること。鍵の削除と無効化を防ぐこと（組織の SCP が持つ）

## 検証方法

- Unit Test: `modules/kms/tests/policy.tftest.hcl` の `cloudtrail_service`・`config_service` が文書の完全一致を、`validation.tftest.hcl` が値の形を確かめる
- Policy Test: `PLAN-KMS-05` の `test_pass_declared_cross_account_sources`・`test_violation_cross_account_sources_not_declared`・`test_violation_declared_cross_account_sources_not_typed`・`test_violation_declared_cross_account_mixed_principal`、`PLAN-AUDIT-05`（trail と Config のアカウントの一致）
- 配信が通ることは実環境で確かめる。どのテストも見ない

## 影響

log-archive のアカウントで鍵の管理の権限を持つ主体は、組織の SCP が入るまで、鍵を止めて証跡を読めなくできる。

## 見直し条件

- 完全一致の暗号化コンテキストで配信が落ちたとき（文書の例の形へ戻し、`PLAN-KMS-05` の値の形を見直す）
- GuardDuty の finding の書き出しを足すとき（宣言のサービスに足す）
- 決定3 の宣言の様式を共有する二つ目のユースケースが現れたとき（root への昇格を見直す。repository-wide/ADR-0001 (adr-process-and-placement) 決定20）
