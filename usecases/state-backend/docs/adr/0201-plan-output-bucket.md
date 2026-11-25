# ADR-0201: CI の plan と apply の出力を、保持日数で消える専用の Bucket に置く

- Status: Accepted
- Date: 2026-11-01
- Scope: state-backend
- Related: repository-wide/ADR-0602, repository-wide/ADR-0102, repository-wide/ADR-0207, state-backend/ADR-0101, state-backend/ADR-0105

## Context

CI の plan と apply の全文は、公開されるログ・Pull Request のコメント・artifact に出さず、state Bucket と分けた Bucket へ plan の role と apply-identity の role が書く（repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定13）。その Bucket は bootstrap の構成物であり（同 決定1）、state の置き場と同じアカウントに要る。

段4 の Pull Request は、merge の前に、その head に対する CI の plan が変更なしを示す必要がある（同 決定10）。CI の plan は全文をこの Bucket に書くので、Bucket が段4 の apply より後にしか無いと、段4 の Pull Request の plan は書き込みで失敗し、merge の条件を満たせない。

plan の出力は、上書きの前の版を残し、日数が過ぎたら消えなければならない。plan の role は object を消せないので（同 決定13）、古い出力を消せるのは lifecycle だけである。state Bucket の lifecycle は現行版を失効させない（state-backend/ADR-0103 (expire-noncurrent-state-versions)）。plan の出力の Bucket は現行版を失効させるので、`modules/s3` を使うと、現行版の失効の有無を入力で切り替えることになる（repository-wide/ADR-0102 (use-case-centric-scope) 決定15）。

## Decision

1. state-backend が、state Bucket と別に、CI の plan と apply の出力を置く Bucket を1つのアカウントに1つ持つ。名前は `<project>-<env>-tfplan-<account_id>-<region>-an` とする。名前の `tfplan` は apply の出力を表さないが、名前を変えると Bucket を作り直すことになるので変えない。
2. この Bucket は段4 の apply で作る。段1 の CLI では作らない。段2 の plan には追加として現れる（repository-wide/ADR-0602 決定5・8）。
3. この Bucket を `usecases/state-backend/internal/plan-output/` に、Public Access Block・`BucketOwnerEnforced`・版管理・暗号化・TLS を要求する Bucket Policy・lifecycle とともに置く（repository-wide/ADR-0102 決定15 の但し書き）。`modules/s3` は使わない。
4. lifecycle は、現行版を公開入力 `plan_output_retention_days`（既定7、1以上の整数）の日数で失効させ、上書きされた版と失効した版をさらに同じ日数で消し、未完了の multipart upload を1日で中止し、失効した版の delete marker を消す。7日と1日は、公式推奨の値ではない独自判断である。plan の全文は run の結果を読み直すためのもので、再 plan が新しい key に置くので長く残す理由は無い。apply の全文は失敗の診断のためのもので、何を適用したかは state の版と merge した Pull Request が持つので、plan と同じ日数で足りる。7日は、週をまたいだレビューの間に読める長さとして選んだ。plan と apply の全文は1回の PutObject で置くので、未完了の multipart upload は失敗からしか生じず、1日で中止する。
5. この Bucket を、state Bucket と同じ state の鍵による SSE-KMS で暗号化し、SSE-C を拒否し、S3 Bucket Key を使わない（state-backend/ADR-0101 (sse-kms-with-state-key)）。鍵の暗号操作を Key Policy で許す role と object は決定9 が定め、CI は暗号化の header を送らずに既定の暗号化に任せる。この Bucket は段4 の apply で作るので、段1 の CLI の暗号化は関わらない。
6. アクセスログ（持たない。state-backend/ADR-0105 (no-server-access-logs)）、通知（書き込みを知らせる相手が無い）、リージョン間の複製（保持日数で消える一時の記録である）、IAM Access Analyzer、tag の検査、HTTP の要求を受ける（KICS 4bc4dd4c。TLS の Deny は Unit Test が文書の完全一致で固定する）を、`internal/plan-output/` の該当箇所で抑止する（repository-wide/ADR-0207 (default-value-policy) 決定11、repository-wide/ADR-0501 (development-tooling-composition) 決定13）。
7. workload root の apply の出力を、plan の出力の Bucket と分けた Bucket に置く。名前は `<project>-<env>-tfapply-<account_id>-<region>-an` とする。保持日数（`plan_output_retention_days`）、lifecycle、暗号化、版管理、Public Access Block、Object Ownership は決定1-5 の Bucket と同じ module で同じにする。新しいアカウントでは段4 の apply で作り、既存のアカウントでは CI の apply-identity が作る（段1 の CLI では作らない）。apply の role は plan の出力の Bucket に書けず（deployment-identity/ADR-0201 決定5）、key の接頭辞で分けると、その Deny を接頭辞の列挙に緩めることになるためである。
8. 出力の2つの Bucket の Bucket Policy は、宣言した書き手（role と key の接頭辞の組）の外の主体による `s3:PutObject` と `s3:ReplicateObject` と、すべての主体による `s3:DeleteObject` と `s3:DeleteObjectVersion` と `s3:ReplicateDelete` を、例外なしで拒否する。plan の出力の Bucket の書き手は plan の role（`pr/`・`branch/`）と apply-identity の role（`apply/`）、apply の出力の Bucket の書き手は apply の role だけとする。書き手の外の主体を拒む文に加えて、接頭辞を持つ書き手の role ごとに、その接頭辞の外への `s3:PutObject` と `s3:ReplicateObject` を拒む文を1つ持つ。接頭辞が空の書き手（Bucket のすべての key）を持つ role には、この文を持たない。その文は何も拒まないからである。key の分離を IAM の Deny だけに頼らない。版と object を消すのは lifecycle だけである。
9. state の鍵の Key Policy は、CI の role ごとに、その role が読み書きする object に限って S3 経由の暗号操作を許す。plan の role には2つの root の state の復号と、plan の出力の Bucket の `pr/`・`branch/` の下の object の復号と鍵の生成を、apply-identity の role には identity root の state と lock file と、plan の出力の Bucket の `apply/` の下の object の復号と鍵の生成を、apply の role には workload root の state と lock file と、apply の出力の Bucket の object の復号と鍵の生成を、lock を外す2つの role にはそれぞれの root の lock file の object の復号だけを許す。書く object にも復号を許すのは、multipart の書き込みが復号と鍵の生成の両方を要るためである。object は暗号化コンテキスト `aws:s3:arn` で限る。出力の Bucket の object は、決定8 の書き手の接頭辞の `StringLike` の pattern で限る。出力の key は実行ごとに決まるからである（repository-wide/ADR-0301 決定11）。限る object は Bucket Policy の書き手と同じ宣言から組み、出力の Bucket の ARN は名前から組む。apply の出力の Bucket を CI が作る plan でも、Key Policy の文書が決まるためである。

## 検討した代替案

### 案A: `modules/s3` に現行版の失効を足し、入力で切り替える

`modules/s3` の lifecycle は state Bucket にも付く。state の現行版を失効させてはならないので、現行版の失効の有無を入力で切り替えることになる（repository-wide/ADR-0102 決定15）。

### 案B: Bucket を段5 で作る

段4 の Pull Request の CI の plan が書き込みで失敗し、repository-wide/ADR-0602 決定10 の merge の条件を満たせない。

### 案C: plan の出力を state Bucket の別の key に置く

plan の role に state Bucket への書き込みを与えることになる。repository-wide/ADR-0602 決定13 が Bucket を分けている。

### 案D: deployment-identity が Bucket を持つ

plan の role の所有者が、その書き先の設定も持つ。deployment-identity は主体と権限を持ち、置き場を持たない。state Bucket と同じ鍵による暗号化の設定が、2つのユースケースにまたがる。

### 案E: plan の出力を独立したユースケースにする

Bucket 1つと lifecycle のために、公開契約と bootstrap の段の割当をもう1組持つことになる。state の Bucket と同じ不変条件と鍵を共有する方が、写しが少ない。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 案E | 採用案 |
| --- | --- | --- | --- | --- | --- | --- |
| Security | 中 | 低（段4 の検証ができない） | 低（plan の role が state に書ける） | 中 | 中 | 中 |
| Testability | 中 | 低 | 中 | 高 | 高 | 高 |
| Contract Clarity | 低（入力の切り替え） | 中 | 低 | 中（主体と置き場が混ざる） | 低（公開契約が増える） | 高 |
| Maintainability | 中 | 中 | 中 | 低（暗号化の設定が2つにまたがる） | 低（不変条件の写しが増える） | 中（`modules/s3` と同じ不変条件の写しを持つ） |
| Scope Control | 低（決定15 に反する） | 中 | 低（決定13 に反する） | 中 | 低（ユースケースが増える） | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- `modules/s3` の不変条件を1箇所だけに書くこと。Bucket の不変条件（Public Access Block、所有権、版管理、暗号化、TLS）は `internal/plan-output/` にも書く
- 保持日数を超えて plan と apply の全文を残すこと

## 保証範囲

- 保証すること: plan の出力の Bucket が、公開されず、ACL を持たず、版を残し、TLS でない要求を拒否し、state の鍵による SSE-KMS で暗号化され、Bucket Key を使わず、SSE-C を拒否し、保持日数を過ぎた版を消す設定を持つこと（いずれも宣言として。実 API での振る舞いは保証しない）。apply の出力の Bucket（決定7）は同じ module で作り、同じ設定を持つこと。2つの出力の Bucket を指す `aws_s3_bucket_policy` が、それぞれ宣言した1つだけであること（Policy Test）。2つの出力の Bucket の Bucket Policy の文書が決定8 のとおりであること（Unit Test の文書の完全一致）と、その文の数が宣言と一致すること（Policy Test の `PLAN-RPE-03`）。state の鍵の Key Policy が決定9 の role に、決定9 の object に限って暗号操作を許し、形が `plan.kms_key_policy` の規則を満たすこと（Contract Test の文書の照合と、Policy Test の `PLAN-SBW-06`・`plan.kms_key_policy`・`PLAN-UNLOCK-04`）。plan・apply-identity・apply の role の復号と鍵の生成の object が、その role の IAM の `s3:GetObject`・`s3:PutObject` の Resource と一致すること（Policy Test の `PLAN-SBW-10`。reference の plan で）
- 保証しないこと: lifecycle が実際に日数どおりに版を消すこと（S3 は失効を非同期に行う）。切り替えの前に書かれた出力が、保持日数の2倍を過ぎるまで SSE-S3 で残ること。決定8 の Deny が実際の要求で文書どおりに評価されること（人が merge の前に `make bootstrap-verify STAGE=output-policy` で確かめる）。決定9 の object の限定が、S3 の実際の暗号化コンテキストに文書どおりに当たること

## 検証方法

- Unit Test: `internal/plan-output/tests/` が、名前の組み立て、入力の検査、Bucket の属性、lifecycle の規則、Bucket Policy の文書を mock の plan と apply で確かめる
- Contract Test: `tests/contract.tftest.hcl` が公開入力 `plan_output_retention_days` と公開出力 `plan_output_bucket_name` / `plan_output_bucket_arn` / `apply_output_bucket_arn` を確かめる。`apply_output_bucket_arn` は apply の出力の Bucket の属性で、その Bucket の名前・lifecycle の日数・暗号化の鍵が決定7 のとおりであることを含む
- Unit Test: `internal/plan-output/tests/` が、暗号化の値（`aws:kms`、鍵の ARN、`bucket_key_enabled = false`）と `kms_key_arn` の validation と precondition を確かめる
- Contract Test: `tests/contract.tftest.hcl` が、この Bucket と apply の出力の Bucket の暗号化が `state_key_arn` の鍵を名指すことを確かめる
- Policy Test: `plan.resource_policy_exclusive`（`PLAN-RPE-00`〜`03`。00 は宣言した resource と policy が入力に在ること、03 は出力の2つの Bucket の Bucket Policy の文の数が宣言と一致すること）が、この Bucket と apply の出力の Bucket のそれぞれについて、宣言した Bucket Policy がその Bucket を指し、その Bucket を指す（または指す先が plan で決まらない）`aws_s3_bucket_policy` が宣言した1つだけであることを確かめる。Bucket Policy は Bucket に1つで、宣言の外の resource が同じ Bucket を指すと置き換えるので、`plan.deployment_identity_apply_identity_output` が読まない文書が実体になりうる
- Unit Test: `internal/plan-output/tests/policy.tftest.hcl` が、決定8 の Bucket Policy の文書を完全一致で確かめる
- Contract Test: `tests/contract.tftest.hcl` が、決定9 の Key Policy の文を、出力の Bucket の ARN を override しない mock の plan で照合する
- Policy Test: `plan.state_backend_write_deny` が、Key Policy の暗号操作を許す role の集合が宣言と一致すること（`PLAN-SBW-06`）と、plan・apply-identity・apply の role の復号と鍵の生成の object が IAM の S3 の object と一致すること（`PLAN-SBW-10`）を、`plan.kms_key_policy` が Key Policy の形と S3 経由の文が object で限られること（`PLAN-KMS-12`）を、`plan.deployment_identity_unlock_role`（`PLAN-UNLOCK-04`）が lock を外す role への復号がそれぞれの root の lock file の object に限られることを確かめる（決定9）
- 手順: 決定8 の Bucket Policy を変える昇格の merge の前に、人が `make bootstrap-verify STAGE=output-policy` で、書き手の書き込みが拒まれず、書き手の外と削除が拒まれることを `simulate-principal-policy --resource-policy` で確かめる（`docs/setup/bootstrap.md`）
- Security: `make trivy-config` が AWS-0132 を、`make checkov` が CKV_AWS_145 を報告しないこと（抑止は持たない）

## 影響

`plan_output_bucket_arn` を受ける deployment-identity は、plan の role に `s3:PutObject` と Bucket の設定の読み取りを、apply-identity の role に Bucket の設定の書き込みと `apply/*` への `s3:PutObject` を与える（deployment-identity/ADR-0201 の表）。CI の plan と apply の job は、Bucket の名前を名前の規則から `scripts/tf-local` の `bucket -kind tfplan` で組む（規則の一致は `tf-local` のテストが見る）。identity root の output `plan_output_bucket_name` は、人が確かめるときの参照である。`apply_output_bucket_arn` を受ける deployment-identity は、apply の role に apply の出力の Bucket の object への `s3:PutObject` を、apply-identity の role に Bucket の作成と設定の書き込みを、plan の role に Bucket の設定の読み取りを与える（deployment-identity/ADR-0201 決定12 と表）。workload root の apply の job は、apply の出力の Bucket の名前を `scripts/tf-local` の `bucket -kind tfapply` で組む。`plan_output_retention_days` を短くする変更には、state-backend/ADR-0103 (expire-noncurrent-state-versions) 決定7 の短縮の許可が要る（Policy Test）。

## 見直し条件

- `modules/s3` の lifecycle が、入力で resource や block の有無を切り替えずに現行版の失効を持てるようになったとき（決定3 を見直す）
- plan か apply の全文を保持日数より長く残す要件ができたとき
