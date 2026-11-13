# ADR-0104: state Bucket の object の書き込みを、Bucket Policy で CI の role と key と操作の組に限る

- Status: Accepted
- Date: 2026-11-13
- Scope: state-backend
- Related: repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0102, repository-wide/ADR-0204, repository-wide/ADR-0207, state-backend/ADR-0101, state-backend/ADR-0201

## Context

repository-wide/ADR-0604 (state-split-by-apply-role) 決定9 は、各 apply の role が自分の root の key と lock file だけに書き込め、lock の解除の role が対応する root の lock file を消すことだけができ、それ以外の書き込みを state Bucket の bucket policy で role の ARN で拒否するとする。repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定11 は、bootstrap の完了後に state backend への書き込みを CI の role に限るとする。IAM の Allow は与えた主体にしか効かず、AdministratorAccess の主体や将来足される IAM の主体を拒めない。

IAM の条件の評価では、否定の演算子は条件キーが要求に無いとき真になる。`aws:PrincipalArn` は、assume した role の要求で role の ARN を返す。

S3 User Guide は、`x-amz-server-side-encryption: aws:kms` を指定して鍵 ID の header を付けない PutObject が AWS 管理鍵 `aws/s3` で暗号化されると述べる箇所と、Bucket の既定の暗号化が鍵を持てばその鍵が使われると読める箇所を、両方持つ。`aws/s3` で暗号化されるなら、`aws/s3` は同じアカウントの主体が S3 経由で使えるので、その object は state の鍵の key policy の外で読める。state-backend/ADR-0101 (sse-kms-with-state-key) は state を state の鍵で暗号化し、読み手を state の鍵の key policy で限る。

## Decision

1. state Bucket の bucket policy に、object の書き込み（`s3:PutObject`・`s3:DeleteObject`・`s3:DeleteObjectVersion`、およびレプリケーションによる書き込みの `s3:ReplicateObject`・`s3:ReplicateDelete`）を、identity root と workload root の apply の role と identity root の lock の解除の role 以外のすべての主体に拒否する Deny を置く。Principal は `"*"`、例外は `aws:PrincipalArn` の `ArnNotEquals` で role の ARN を完全一致で書く。
2. 決定1 の各 role について、その role が書ける key の外への書き込みを拒否する Deny と、その key への許さない操作を拒否する Deny を置く。apply の role は自分の root の state の key と lock file への `s3:PutObject` と `s3:DeleteObject`、lock の解除の role は identity root の lock file への `s3:DeleteObject` だけを書ける。`s3:DeleteObjectVersion` とレプリケーションによる書き込みはどの role にも許さない。bucket policy はこの外側の層であり、各 role の IAM の Allow はさらに狭い（deployment-identity/ADR-0201 (enumerated-permissions) 決定5・9）。たとえば state の key への `s3:DeleteObject` は IAM が与えない。
3. 書き込みの Deny の Resource は object の ARN だけとし、Action は決定1 の5つに限る。Bucket の ARN と Bucket の設定の Action を含めない。Bucket Policy を書き換えられる主体が、拒否を誤ったときに直せるようにするためである。
4. 例外の role・key・操作は、`modules/s3` の型付きの入力で受ける。操作は「書き込み」と「削除だけ」の2値で受け、AWS の Action の文字列を受けない。policy の文を受ける入力を作らない（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定1・2）。
5. state Bucket への `s3:PutObject` のうち、次のものを全主体に拒否する。暗号化の方式の header が在って `aws:kms` でないもの。鍵 ID の header が在って state の鍵の ARN でないもの。方式の header が `aws:kms` で鍵 ID の header が無いもの。条件キーを持たない要求は拒否しない。条件キーは `Null` で在ることを確かめてから値を比べる。方式の header が `aws:kms` で鍵 ID の header が無い要求をどの鍵で暗号化するかによらず、この拒否は state の鍵以外で書かれる要求だけに当たる。
6. plan の出力の Bucket には書き込みの Deny を置かない。plan の出力を書く主体は plan の role だけで、repository-wide/ADR-0602 決定13 が IAM で限る。
7. 新しいアカウントの bootstrap の段4 の実行者（path を持たない IAM user）は、段5 の最初の変更が外すまで、決定1 の例外と、決定2 の identity root の apply の role と同じ key と操作と、決定5 の方式の header の拒否の例外に入る。例外は user の ARN で書く。`aws login` の session の要求で `aws:PrincipalArn` が user の ARN になることは、この repository の bootstrap の手順が `aws sts get-caller-identity` で観測する事実であり、AWS の文書の記述ではない。期間の終わりは、段5 の最初の変更の apply である。

## 検討した代替案

### 案A: key ごとに持ち主以外を拒む（writer を key に閉じ込めない）

key ごとに「持ち主の role 以外」を `ArnNotEquals` で拒み、object 全体の例外を Action ごとに分ける。文は1種類で済むが、lock の解除の role が予約されていない key（将来の root の key など）を消すことを bucket policy で拒めず、apply の role も予約外の key に書ける。repository-wide/ADR-0604 決定9 の「それ以外の key と lock file への書き込みは、bucket policy で拒否する」を満たさない。

### 案B: `NotPrincipal` で書く

IAM User Guide は新しい resource-based policy に `NotPrincipal` を勧めず、permissions boundary を持つ IAM principal を値にかかわらず拒むとする。代わりに `"Principal": "*"` と `aws:PrincipalArn` の `ArnNotEquals` の Deny を例示しており、決定1 はその形である。

### 案C: 暗号化の header を条件にしない

backend の書き込みは鍵の ARN を header で送るので、正しい書き込みは拒まれない。しかし S3 User Guide の一方の読みでは、鍵 ID を付けない `aws:kms` の書き込みが `aws/s3` で暗号化され、state の鍵の key policy で読み手を限る設計（state-backend/ADR-0101）を1つの header の誤りで素通りさせる。もう一方の読みでも、決定5 は正しい書き込みを拒まない。鍵の渡し忘れ（`AES256` の明示）の検出も、運用機構の検査に bucket の側の層を重ねる。

### 案D: 鍵 ID が無い要求も拒む（`Null` を併記しない）

multipart の部分の upload と完了の要求は `s3:PutObject` で認可され、暗号化の header を持たない。否定の演算子はキーが無いと真なので、大きな state の書き込みが落ちうる。

### 案E: plan の出力の Bucket にも同じ Deny を置く

plan の出力を書く主体は plan の role だけで、IAM で限られている（repository-wide/ADR-0602 決定13）。plan の出力は保持日数で消え（state-backend/ADR-0201 (plan-output-bucket)）、state ではない。

### 案F: 段4 を `-target` で Bucket Policy の外に絞る

repository-wide/ADR-0202 (internal-topology-as-implementation-detail) 決定7 は、targeted apply を前提とする運用手順をドキュメント化しない。段4 の PR の plan が変更なしにならず、repository-wide/ADR-0602 決定10 を満たさない。段4 から段5 まで TLS の拒否も無くなる。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 案E | 案F | 採用案 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Security | 中（予約外の key の削除を拒めない） | 中（boundary を持つ主体を誤って拒む） | 中（`aws/s3` の書き込みを拒めない） | 高 | 高 | 低（段4 から段5 まで拒否が無い） | 高 |
| Testability | 高 | 中 | 高 | 低（multipart が落ちうる） | 中 | 低 | 高 |
| Contract Clarity | 中 | 低 | 高 | 中 | 中 | 低 | 高 |
| Maintainability | 高（文が1種類） | 中 | 高 | 中 | 中（Bucket が2つの形を持つ） | 低（手順が target の列挙を持つ） | 中（`NotResource` を読む） |
| Scope Control | 中 | 中 | 高 | 中 | 低（state でない Bucket に広げる） | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- AWS service principal が state Bucket へ書くこと。`aws:PrincipalArn` を持たない要求は決定1 の Deny に当たる。S3 Lifecycle の失効は Bucket Policy に評価されない
- apply-identity の role に対する拒否の保証。apply-identity は Bucket Policy を書き換えられる。決定2・5 は apply-identity の誤った書き込みを検出するが、意図した書き換えを止めない
- alias や鍵 ID で鍵を名指す書き手。決定5 の条件の値は鍵の ARN である
- 決定1 の5つの Action の外の object の操作。`s3:PutObjectAcl` は Bucket の ACL が無効なので効かない。`s3:PutObjectTagging`・`s3:DeleteObjectTagging` は state の内容を変えない。`s3:AbortMultipartUpload` は writer の進行中の multipart を他の主体が中止できるが、state を書き換えない。`s3:UpdateObjectEncryption` は書いた後の object の鍵を差し替えうるが、state の鍵の復号を要るので、できるのは既に state を読める key policy の主体だけである

## 保証範囲

- 保証すること: Bucket Policy の文書が決定1-5 の形であること（Unit Test と Policy Test）
- 保証しないこと:
  - 管理アカウントの root による Bucket Policy の削除（repository-wide/ADR-0602 決定11）。apply-identity による Bucket Policy の書き換え
  - 実 API での評価のうち、例外の role が通る側は、この決定を入れる apply 自身と、その直後の再実行の plan の変更なしが一度だけ見る。例外に無い主体が拒まれる側は、人が一時 IAM user の `put-object` で一度だけ見る。例外の role が誤った暗号化の header で拒まれる側は、実 API で見ない
  - 条件キー `s3:x-amz-server-side-encryption-aws-kms-key-id` の値が、ARN 以外で鍵を名指した要求でどう表れるか
  - multipart の部分の upload と完了の要求で、S3 が暗号化の条件キーを埋めるか、埋めるならどの形か。鍵 ID の形で埋めるなら、state が multipart で書かれる大きさになったとき決定5 が書き込みを拒む
  - 方式の header が `aws:kms` で鍵 ID の header が無い要求を、S3 が `aws/s3` と既定の暗号化の鍵のどちらで暗号化するか（S3 User Guide の記述が食い違う）
  - 段4 の実行者の例外が段5 の最初の変更で外れること。Policy Test は env の root を入力にしないので、見るのは `docs/setup/bootstrap.md` 段5 の成功条件だけである

## 検証方法

- Unit Test: `modules/s3/tests/policy.tftest.hcl` が文書の完全一致を、`tests/writers.tftest.hcl` が入力の検査を見る
- Policy Test: `plan.state_backend_write_deny`（`PLAN-SBW-00`〜`09`。08 は TLS の拒否が在ること、09 は Deny でない文が無いこと）
- Contract Test: `ci_role_arns` の apply・apply_identity・unlock_identity と `bootstrap_executor_user_name` が `modules/s3` の例外へ届くこと
- 手順: 段5 の成功条件の、例外に無い主体の `put-object` が `AccessDenied` を返すこと（`docs/setup/bootstrap.md`）

## 影響

- `ci_role_arns` が `apply` を要る（公開契約の破壊的変更）
- 段5 の後、残った lock file を消せるのは lock の解除の role（CI の経路）だけになる
- 新しいアカウントの bootstrap は、段3 で `bootstrap_executor_user_name` を入れ、段5 の最初の変更で外す

## 見直し条件

- service principal の書き込みを要る消費者が `modules/s3` を使うとき
- lock を外す主体、または root の数が変わったとき（repository-wide/ADR-0604 決定5・9）
- root の key の名前（repository-wide/ADR-0604 決定7）が変わったとき
- state の鍵を alias や鍵 ID で名指す書き手が要るとき（決定5）
- S3 の文書の食い違い（決定5）が解けたとき
