# ADR-0105: state Bucket と2つの出力の Bucket に、S3 サーバーアクセスログを持たせない

- Status: Accepted
- Date: 2026-11-14
- Scope: state-backend
- Related: repository-wide/ADR-0207, repository-wide/ADR-0501, repository-wide/ADR-0602, deployment-identity/ADR-0201, state-backend/ADR-0104, state-backend/ADR-0201

## Context

Security Hub の S3.9（S3 general purpose buckets should have server access logging enabled）は、汎用の Bucket に S3 サーバーアクセスログを有効にすることを推奨する。`docs/v1.0-plan/scope.md` 第9.3節は、S3 サーバーアクセスログを S3 ではなく CloudWatch Logs へ配送するとする。

state Bucket と2つの出力の Bucket の設定と object を変えられる主体は、CI の apply-identity の role と、管理アカウントの root である。apply-identity の role は、自分を含む CI の role の権限を変えられる（deployment-identity/ADR-0201 (enumerated-permissions) 決定6）。配送の構成物（delivery source・delivery destination・delivery・Log Group とその保持期間）は、同じアカウントの中に置かれる。これらの主体は、Bucket を改変する前に配送を止めるか、Log Group を消すか、保持期間を縮められる。

## Decision

1. state Bucket と2つの出力の Bucket に、S3 サーバーアクセスログを持たせない。`aws_s3_bucket_logging` も、CloudWatch Logs への配送の構成物も置かない。
2. 理由は、記録が、記録すべき主体に対して効かないことにある。3つの Bucket を改変できる主体は、同じ権限で記録を止められる。そのため、記録はその主体の改変に気づく手段にならない。
3. 改変には、振る舞いの検知（Amazon GuardDuty）で気づく形を採る。検知はこのユースケースの外に置き、フェーズ1 で入れる（#256）。
4. API の呼び出しの記録は CloudTrail が持つ。object の読み書き（data events）の記録と保管は `audit-evidence` が持つ（`docs/v1.0-plan/scope.md` 第7節）。
5. 決定1 を公式推奨からの逸脱として記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。Checkov の CKV_AWS_18 と KICS の f861041c を `modules/s3/main.tf` と `internal/plan-output/main.tf` で、SonarCloud の S6258 を `sonar-project.properties` で同じ2つの path に限って抑止する（repository-wide/ADR-0501 (development-tooling-composition) 決定13）。各抑止の理由の正本は本 ADR とする。外れる理由となるユースケース上の制約は、3つの Bucket を改変できる主体が、同じアカウントに置かれる配送の構成物も止められることである（Context）。

## 検討した代替案

### 案A: 同じアカウントの Log Group へ配送する（CloudWatch Logs の vended logs）

決定2 の主体は、配送と Log Group を止められる。Log Group を暗号化する鍵とその Key Policy、配送の権限と、apply の role の Deny が増える。

### 案B: 同じアカウントのログ先 Bucket へ配送する（`aws_s3_bucket_logging`）

案A と同じく、決定2 の主体が止められる。ログ先 Bucket は SSE-KMS を使えない（`docs/v1.0-plan/scope.md` 第9.1節）。

### 案C: 別のアカウントの改ざんできない保管先へ配送する

届いた記録は改変から守られる。それでも配送の設定は3つの Bucket のアカウントに在り、決定2 の主体はそれを止められる。保管先は `audit-evidence`（フェーズ1）に依存し、フェーズ0 の bootstrap の構成物がフェーズ1 のユースケースを待つことになる。

### 案D: このユースケースが3つの Bucket の data events の trail を持つ

`audit-evidence` と同じ記録を、もう1つ持つことになる。決定2 の主体は trail も止められる。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 中（改変する主体が止められる） | 中（同左） | 中（届いた記録は守られるが、止められる） | 中（同左） | 中（検知が入るまで改変に気づけない） |
| Testability | 低（配送の到着は実環境でしか見えない） | 低 | 低 | 低 | 高 |
| Contract Clarity | 中（Log Group と配送の ARN が公開契約に入る） | 中 | 低（フェーズ1 への依存） | 中 | 高 |
| Maintainability | 低（鍵、配送の権限、Deny が増える） | 中 | 低 | 中 | 高 |
| Scope Control | 中 | 中 | 低 | 低（`audit-evidence` と重なる） | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 3つの Bucket への要求のうち、CloudTrail が記録しないものの記録（認証に失敗した要求と、lifecycle による移行・失効・復元）
- `audit-evidence` が入るまで、3つの Bucket の object の読み書きの記録

## 保証範囲

- 保証すること: なし。本 ADR は記録を持たないことを決める
- 保証しないこと: フェーズ1 の GuardDuty が入るまで、3つの Bucket と bootstrap の構成物の改変を即座に知らせる仕組みは無い。GuardDuty が入った後に、どの改変を検知するか（本 ADR では確かめていない。検知のユースケースが決める）。`audit-evidence` が入るまでの object の読み書きの記録

## 検証方法

- `modules/s3` と `internal/plan-output` は `aws_s3_bucket_logging` と CloudWatch Logs の配送の resource を持たない。決定5 の抑止が Checkov・KICS・SonarCloud の検出を止めるので、それらの道具はこれを見ない。bootstrap の module に配送の resource を足すと、Policy Test（`make tf-policy`）の `PLAN-APPLY-02` が `conftest/plan/decl.json` での型の分類を求めて落ちる
- Security: `make checkov` が CKV_AWS_18 を、`make kics` が f861041c を、決定5 の2つの path 以外で報告しないこと。抑止の宣言は各ファイルの `#checkov:skip` と `kics-scan disable`、`sonar-project.properties` が持つ

## 影響

`deployment-identity` は、CI の role の権限を段4 の構成物に要る分に限る（deployment-identity/ADR-0201 決定3）。CloudWatch Logs と vended logs の権限はそこに含まれない。Security Hub の S3.9 を有効にしたアカウントでは、3つの Bucket が不合格になる。その結果の扱いは Security Hub を持つユースケースが決める。availability への影響は無い。cost は、配送と Log Group の分が掛からない。

## 見直し条件

- 監査か規制の要件が、3つの Bucket への要求のうち CloudTrail が記録しないものの記録を求めたとき
- 改変の検知（#256）を入れないと決めたとき、または管理アカウントを検知の対象に含めないと決めたとき
- 配送の設定を、3つの Bucket を改変できる主体が止められない形で置けるようになったとき（案C の前提が変わったとき）
