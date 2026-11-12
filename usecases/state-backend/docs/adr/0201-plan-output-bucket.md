# ADR-0201: CI の plan の出力を、保持日数で消える専用の Bucket に置く

- Status: Accepted
- Date: 2026-11-01
- Scope: state-backend
- Related: repository-wide/ADR-0602, repository-wide/ADR-0102, repository-wide/ADR-0207, state-backend/ADR-0101

## Context

CI の plan の全文は、公開されるログ・Pull Request のコメント・artifact に出さず、state Bucket と分けた Bucket へ plan の role が書く（repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定13）。その Bucket は bootstrap の構成物であり（同 決定1）、state の置き場と同じアカウントに要る。

段4 の Pull Request は、merge の前に、その head に対する CI の plan が変更なしを示す必要がある（同 決定10）。CI の plan は全文をこの Bucket に書くので、Bucket が段4 の apply より後にしか無いと、段4 の Pull Request の plan は書き込みで失敗し、merge の条件を満たせない。

plan の出力は、上書きの前の版を残し、日数が過ぎたら消えなければならない。plan の role は object を消せないので（同 決定13）、古い出力を消せるのは lifecycle だけである。state Bucket の lifecycle は現行版を失効させない（state-backend/ADR-0103 (expire-noncurrent-state-versions)）。plan の出力の Bucket は現行版を失効させるので、`modules/s3` を使うと、現行版の失効の有無を入力で切り替えることになる（repository-wide/ADR-0102 (use-case-centric-scope) 決定15）。

## Decision

1. state-backend が、state Bucket と別に、CI の plan の出力を置く Bucket を1つのアカウントに1つ持つ。名前は `<project>-<env>-tfplan-<account_id>-<region>-an` とする。
2. この Bucket は段4 の apply で作る。段1 の CLI では作らない。段2 の plan には追加として現れる（repository-wide/ADR-0602 決定5・8）。
3. この Bucket を `usecases/state-backend/internal/plan-output/` に、Public Access Block・`BucketOwnerEnforced`・版管理・暗号化・TLS を要求する Bucket Policy・lifecycle とともに置く（repository-wide/ADR-0102 決定15 の但し書き）。`modules/s3` は使わない。
4. lifecycle は、現行版を公開入力 `plan_output_retention_days`（既定7、1以上の整数）の日数で失効させ、上書きされた版と失効した版をさらに同じ日数で消し、未完了の multipart upload を1日で中止し、失効した版の delete marker を消す。7日と1日は、公式推奨の値ではない独自判断である。plan の全文は run の結果を読み直すためのもので、再 plan が新しい key に置くので長く残す理由は無い。7日は、週をまたいだレビューの間に読める長さとして選んだ。plan の全文は1回の PutObject で置くので、未完了の multipart upload は失敗からしか生じず、1日で中止する。
5. この Bucket を、state Bucket と同じ state の鍵による SSE-KMS で暗号化し、SSE-C を拒否し、S3 Bucket Key を使わない（state-backend/ADR-0101 (sse-kms-with-state-key)）。plan の role は鍵の `kms:GenerateDataKey` を Key Policy で持ち、CI の plan は暗号化の header を送らずに既定の暗号化に任せる。この Bucket は段4 の apply で作るので、段1 の CLI の暗号化は関わらない。
6. アクセスログ（段5 で state Bucket と同じく足す）、通知（書き込みを知らせる相手が無い）、リージョン間の複製（保持日数で消える一時の記録である）、IAM Access Analyzer、tag の検査を、`internal/plan-output/` の該当箇所で抑止する（repository-wide/ADR-0207 (default-value-policy) 決定11、repository-wide/ADR-0501 (development-tooling-composition) 決定13）。

## 検討した代替案

### 案A: `modules/s3` に現行版の失効を足し、入力で切り替える

`modules/s3` の lifecycle は state Bucket にも付く。state の現行版を失効させてはならないので、現行版の失効の有無を入力で切り替えることになる（repository-wide/ADR-0102 決定15）。

### 案B: Bucket を段5 で作る

段4 の Pull Request の CI の plan が書き込みで失敗し、repository-wide/ADR-0602 決定10 の merge の条件を満たせない。

### 案C: plan の出力を state Bucket の別の key に置く

plan の role に state Bucket への書き込みを与えることになる。repository-wide/ADR-0602 決定13 が Bucket を分けている。

### 案D: deployment-identity が Bucket を持つ

plan の role の所有者が、その書き先の設定も持つ。deployment-identity は主体と権限を持ち、置き場を持たない。段5 で state Bucket と同じ CMK とログへ切り替える変更が、2つのユースケースにまたがる。

### 案E: plan の出力を独立したユースケースにする

Bucket 1つと lifecycle のために、公開契約と bootstrap の段の割当をもう1組持つことになる。state の Bucket と同じ不変条件と段5 の切り替えを共有する方が、写しが少ない。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 案E | 採用案 |
| --- | --- | --- | --- | --- | --- | --- |
| Security | 中 | 低（段4 の検証ができない） | 低（plan の role が state に書ける） | 中 | 中 | 中 |
| Testability | 中 | 低 | 中 | 高 | 高 | 高 |
| Contract Clarity | 低（入力の切り替え） | 中 | 低 | 中（主体と置き場が混ざる） | 低（公開契約が増える） | 高 |
| Maintainability | 中 | 中 | 中 | 低（段5 の切り替えが2つにまたがる） | 低（不変条件の写しが増える） | 中（`modules/s3` と同じ不変条件の写しを持つ） |
| Scope Control | 低（決定15 に反する） | 中 | 低（決定13 に反する） | 中 | 低（ユースケースが増える） | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- `modules/s3` の不変条件を1箇所だけに書くこと。Bucket の不変条件（Public Access Block、所有権、版管理、暗号化、TLS）は `internal/plan-output/` にも書く
- 保持日数を超えて plan の全文を残すこと

## 保証範囲

- 保証すること: plan の出力の Bucket が、公開されず、ACL を持たず、版を残し、TLS でない要求を拒否し、state の鍵による SSE-KMS で暗号化され、Bucket Key を使わず、SSE-C を拒否し、保持日数を過ぎた版を消す設定を持つこと（いずれも宣言として。実 API での振る舞いは保証しない）
- 保証しないこと: lifecycle が実際に日数どおりに版を消すこと（S3 は失効を非同期に行う）。切り替えの前に書かれた出力が、保持日数の2倍を過ぎるまで SSE-S3 で残ること

## 検証方法

- Unit Test: `internal/plan-output/tests/` が、名前の組み立て、入力の検査、Bucket の属性、lifecycle の規則、Bucket Policy の文書を mock の plan と apply で確かめる
- Contract Test: `tests/contract.tftest.hcl` が公開入力 `plan_output_retention_days` と公開出力 `plan_output_bucket_name` / `plan_output_bucket_arn` を確かめる
- Unit Test: `internal/plan-output/tests/` が、暗号化の値（`aws:kms`、鍵の ARN、`bucket_key_enabled = false`）と `kms_key_arn` の validation と precondition を確かめる
- Contract Test: `tests/contract.tftest.hcl` が、この Bucket の暗号化が `state_key_arn` の鍵を名指すことを確かめる
- Security: `make trivy-config` が AWS-0132 を、`make checkov` が CKV_AWS_145 を報告しないこと（抑止は持たない）

## 影響

`plan_output_bucket_arn` を受ける deployment-identity は、plan の role に `s3:PutObject` と Bucket の設定の読み取りを、apply-identity の role に Bucket の設定の書き込みを与える（deployment-identity/ADR-0201 の表）。CI の plan の job は、Bucket の名前を identity root の output `plan_output_bucket_name` から得る。`plan_output_retention_days` を短くする変更には、state-backend/ADR-0103 (expire-noncurrent-state-versions) 決定7 の短縮の許可が要る（Policy Test）。

## 見直し条件

- `modules/s3` の lifecycle が、入力で resource や block の有無を切り替えずに現行版の失効を持てるようになったとき（決定3 を見直す）
- plan の全文を保持日数より長く残す要件ができたとき
