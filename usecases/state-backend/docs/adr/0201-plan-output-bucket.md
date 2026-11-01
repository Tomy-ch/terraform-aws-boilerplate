# ADR-0201: CI の plan の出力を、保持日数で消える専用の Bucket に置く

- Status: Accepted
- Date: 2026-11-01
- Scope: state-backend
- Related: repository-wide/ADR-0602, repository-wide/ADR-0102, repository-wide/ADR-0207, state-backend/ADR-0101

## Context

CI の plan の全文は、公開されるログ・Pull Request のコメント・artifact に出さず、state Bucket と分けた Bucket へ plan の role が書く（repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定13）。その Bucket は bootstrap の構成物であり（同 決定1）、state の置き場と同じアカウントに要る。

段4 の Pull Request は、merge の前に、その head に対する CI の plan が変更なしを示す必要がある（同 決定10）。CI の plan は全文をこの Bucket に書くので、Bucket が段4 の apply より後にしか無いと、段4 の Pull Request の plan は書き込みで失敗し、merge の条件を満たせない。

plan の出力は、上書きの前の版を残し、日数が過ぎたら消えなければならない。plan の role は object を消せないので（同 決定13）、古い出力を消せるのは lifecycle だけである。一方、state Bucket の lifecycle は段5 で足す（deployment-identity/ADR-0201 (enumerated-permissions) 決定3）。一般 module の `modules/s3` に lifecycle を足すと state Bucket にも付き、plan の出力の Bucket にだけ付けるには入力で resource の有無を切り替えることになる（repository-wide/ADR-0102 (use-case-centric-scope) 決定15）。

## Decision

1. state-backend が、state Bucket と別に、CI の plan の出力を置く Bucket を1つのアカウントに1つ持つ。名前は `<project>-<env>-tfplan-<account_id>-<region>-an` とする。
2. この Bucket は段4 の apply で作る。段1 の CLI では作らない。段2 の plan には追加として現れる（repository-wide/ADR-0602 決定5・8）。
3. この Bucket を `usecases/state-backend/internal/plan-output/` に、Public Access Block・`BucketOwnerEnforced`・版管理・暗号化・TLS を要求する Bucket Policy・lifecycle とともに置く（repository-wide/ADR-0102 決定15 の但し書き）。`modules/s3` は使わない。
4. lifecycle は、現行版を公開入力 `plan_output_retention_days`（既定7、1以上の整数）の日数で失効させ、上書きされた版と失効した版をさらに同じ日数で消し、未完了の multipart upload を1日で中止し、失効した版の delete marker を消す。
5. 段5 まで、この Bucket を state Bucket と同じく SSE-S3（`AES256`）で暗号化し、SSE-C を拒否する。段5 で state Bucket と同じ CMK による SSE-KMS へ切り替える。plan の全文は state の値と秘密を含みうるので、段5 までの間、その読み手を KMS の権限で絞れないことを受け入れる（state-backend/ADR-0101 (sse-s3-until-cmk) と同じ）。
6. 決定5 を公式推奨からの逸脱として記録し（repository-wide/ADR-0207 (default-value-policy) 決定11）、Security の各道具の検査を、その道具が報告する path に限って抑止する —— Trivy の AWS-0132 は `internal/plan-output/encryption.tf`、Checkov の CKV_AWS_145 は `internal/plan-output/main.tf` の `aws_s3_bucket.this`。アクセスログ（段5 で state Bucket と同じく足す）、通知（書き込みを知らせる相手が無い）、リージョン間の複製（保持日数で消える一時の記録である）、IAM Access Analyzer、tag の検査も、同じ箇所で抑止する。

## 検討した代替案

### 案A: `modules/s3` に lifecycle を足し、日数を入力にする

lifecycle が state Bucket にも付く。state の現行版を失効させてはならないので、現行版の失効の有無を入力で切り替えることになる（repository-wide/ADR-0102 決定15）。state Bucket の lifecycle が段5 から段4 へ前倒しになり、deployment-identity/ADR-0201 決定3 と食い違う。

### 案B: Bucket を段5 で作る

段4 の Pull Request の CI の plan が書き込みで失敗し、repository-wide/ADR-0602 決定10 の merge の条件を満たせない。

### 案C: plan の出力を state Bucket の別の key に置く

plan の role に state Bucket への書き込みを与えることになる。repository-wide/ADR-0602 決定13 が Bucket を分けている。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中 | 低（段4 の検証ができない） | 低（plan の role が state に書ける） | 中（段5 まで SSE-S3） |
| Testability | 中 | 低 | 中 | 高 |
| Contract Clarity | 低（入力の切り替え） | 中 | 低 | 高 |
| Maintainability | 中 | 中 | 中 | 中（`modules/s3` と同じ不変条件の写しを持つ） |
| Scope Control | 低（決定15 に反する） | 中 | 低（決定13 に反する） | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- `modules/s3` の不変条件を1箇所だけに書くこと。Bucket の不変条件（Public Access Block、所有権、版管理、暗号化、TLS）は `internal/plan-output/` にも書く
- 段5 までの間、plan の全文を読める主体を KMS の権限で限ること
- 保持日数を超えて plan の全文を残すこと

## 保証範囲

- 保証すること: plan の出力の Bucket が、公開されず、ACL を持たず、版を残し、TLS でない要求を拒否し、SSE-S3 で暗号化され、SSE-C を拒否し、保持日数を過ぎた版を消す設定を持つこと（いずれも宣言として。実 API での振る舞いは保証しない）
- 保証しないこと: lifecycle が実際に日数どおりに版を消すこと（S3 は失効を非同期に行う）。段5 までの間、plan の全文を読める主体を KMS の権限で限ること

## 検証方法

- Unit Test: `internal/plan-output/tests/` が、名前の組み立て、入力の検査、Bucket の属性、lifecycle の規則、Bucket Policy の文書を mock の plan と apply で確かめる
- Contract Test: `tests/contract.tftest.hcl` が公開入力 `plan_output_retention_days` と公開出力 `plan_output_bucket_name` / `plan_output_bucket_arn` を確かめる
- Security: `make trivy-config` が AWS-0132 を抑止した path 以外で報告しないこと。`make checkov` が CKV_AWS_145 を抑止した resource 以外で報告しないこと

## 影響

`plan_output_bucket_arn` を受ける deployment-identity は、plan の role に `s3:PutObject` と Bucket の設定の読み取りを、apply-identity の role に Bucket の設定の書き込みを与える（deployment-identity/ADR-0201 の表）。CI の plan の job は、Bucket の名前を identity root の output `plan_output_bucket_name` から得る。

## 見直し条件

- bootstrap の段5 で SSE-KMS へ切り替えたとき（決定5・6 を書き換え、抑止を外す）
- `modules/s3` が lifecycle を、入力で resource の有無を切り替えずに持てるようになったとき（決定3 を見直す）
- plan の全文を保持日数より長く残す要件ができたとき
