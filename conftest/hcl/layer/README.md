# 層の規則

[ADR-0102](../../../docs/adr/0102-use-case-centric-scope.md) 決定14・17・18 の層の規則を、HCL のソースで検査する
（同 ADR の検証方法。[ADR-0501](../../../docs/adr/0501-development-tooling-composition.md) 決定5 がソースの構造の規約として
Conftest に割り当てる）。規則そのものは ADR が持ち、ここには再掲しない。

起動は `make layer-check` で、CI では Pull Request の Static Analysis の層の job（`terraform-gates.yaml` の `tf-lint`）が
TFLint の後に走らせ、apply する木では `terraform-apply.yaml` の `apply-layer-check` が走らせる。apply の job はその成功を
待つ（[`.github/workflows/README.md`](../../../.github/workflows/README.md) の「apply の job」）。検査そのものの検査は
`make layer-check-test` である。

| 段 | make | 判定するもの |
| --- | --- | --- |
| Rego のテスト | `layer-verify` | 各 deny の ID が違反と非違反の両方のテストを持ち、テストがすべて走って通ったこと（`infra-code-gate rego-tests`） |
| 検査 | `layer-detect` | 走査の root（`usecases` `modules` `env`）の下の全ファイルを1つの入力として掛ける（`--combine`）。Conftest が読んだ `.tf` と `.tf.json` の集合を `find` の一覧と突き合わせ、評価した規則の数を `layer.rego` の `deny contains` の数と突き合わせる（`tf-local policy-result`） |

`tf-local policy-result` は `layer.rego` の行頭の `deny contains` を数える。規則は字下げせずにこのファイルへ置く。

## 失敗の読み方

| ID | 意味 | 直し方 |
| --- | --- | --- |
| `LY001` | 宣言（`decl.json`）の形が違う。`attachments` が空でないオブジェクトでない、項目が `parents` と `optional` だけを持たない、`not_attachments` の理由が空、同じ型が両方の区分に在る | 宣言を直す |
| `LY002` | 走査の root に `.tf` も `.tf.json` も無い。または Conftest が読んだファイルと `find` の一覧が違う（symlink の構成ファイルなど） | 一覧の差のファイルを実体のファイルにするか、走査の root から外す |
| `LY003` | 走査の root に JSON の構成（`.tf.json`）が在る。検査はそれを解釈しない（ADR-0501 決定5、ADR-0103 決定11） | HCL（`.tf`）で書く |
| `LY101` | `usecases/<use-case>/` 直下の `.tf` に `resource` が在る（決定14） | module に置き、usecase は module を合成する |
| `LY102` | `usecases/<use-case>/` 直下の `.tf` に `provider` が在る（決定14） | env の root に置く |
| `LY103` | `usecases/<use-case>/` 直下の `.tf` に root module にしか置けない block（`import`、`terraform` の `backend` と `cloud`）が在る（決定14） | env の root に置く |
| `LY201` | module の `source` が `./` か `../` で始まるリテラルでない（決定17、ADR-0205 決定3） | リポジトリ内の相対パスにする |
| `LY202` | `source` を正規化すると走査の root の外を指す | リポジトリ内の層の置き場を指す |
| `LY203` | 呼び出し元の層が許さない先を `source` が指す（決定17） | 層の許す先を呼ぶ |
| `LY204` | `source` が指すディレクトリに `.tf` が無い | 先を直す |
| `LY205` | module を呼ぶディレクトリが、どの層の置き場（決定12）でもない | 層の置き場へ移す |
| `LY206` | module の呼び出しが循環する | 循環を断つ |
| `LY301` | attachment 型の resource の親を指す引数が、同じ module の、宣言した親の型の resource だけを参照していない（決定18） | 親の resource と同じ module に置き、親を参照する |
| `LY302` | `optional` でない attachment 型の resource が、宣言したどの親の引数も持たない | Provider が引数の名前を変えたなら宣言を直す |
| `LY303` | 使われている resource の型が、宣言のどの区分（attachment、親の型、`not_attachments`）にも無い | 下の基準で分類し、宣言に足す |

## 層の決め方

層はディレクトリの位置で決める（決定12）。`env/` の下のディレクトリは env、`usecases/<use-case>` は usecase、
`usecases/<use-case>/internal/<module>` は usecase の module、`usecases/<use-case>/reference` は reference、
`modules/<module>` は一般 module である。`source` はディレクトリと結んで `.` と `..` を字面で解いた後のパスで判定し、
先の実在は読んだ `.tf` のディレクトリの集合で判定する。

## 親を指す引数の辿り方

親を指す引数の値の `${...}` から参照（`<名前>.<名前>`）を取り出し、次のように辿った先の参照を判定する。

- `local.<名前>` は、同じディレクトリの `locals` の値の全体へ辿る
- `each.key` と `each.value` は、その resource の `for_each` の値の全体へ辿る
- 添字（`x[...]` の `[...]`）の中は親を選ぶだけなので辿らない。`count.index` は参照に数えない

辿った先の参照が無いか、1つでも、同じディレクトリに宣言された、宣言した親の型の resource でなければ違反とする。
`*` を親の型に持つ引数（親の型が多態のもの）は、同じディレクトリのどの型の resource でもよい。

## 宣言

[`decl.json`](decl.json) が持つ。

- `attachments`: attachment 型の resource の型ごとに、`parents`（親を指す引数から、親として認める型の配列へ）と
  `optional`（宣言したどの親の引数も書かなくてよいか）を持つ。引数は `protected_resource.s3_bucket.bucket_name` の
  ように block をまたいだ dotted な並びで書き、`dynamic` で書いた block も同じ引数として読む（`lib.hcl.values_at`）
- `not_attachments`: attachment 型でない型と、その理由
- `attachments` の親の型に現れる型（`*` を除く）は、親として分類済みと数える

### 分類の基準

attachment 型とは、親の resource をちょうど1つ名指し、その親の属性（policy、設定、規則の集合、関連付け）を書き換える
resource である。親と引数の組で同一性が決まり、自前の名前や ARN を持っても親の存在を前提にする。

- 2つの resource を結ぶ関連付け（`aws_iam_role_policy_attachment` など）は attachment 型に含め、親は守られる側
  （不変条件を持つ側）とする。もう一方の引数は宣言しない
- 自前の identity と lifecycle を持つ子 resource（`aws_lb_listener` など）と、アカウントかリージョンの単位で設定する
  resource は `not_attachments` に置く。親を Terraform が作らない型も、親を同じ module に置けないので `not_attachments` に置く
- 親を指す引数と親の型は、Provider の schema で確かめる（ADR-0501 決定21）。`optional` は、宣言した親の引数が
  schema でどれも `required` でないときに `true` とする

## 保証しないこと

- 宣言の正しさ。分類、親の引数、親の型、`optional` を誤った型は、その誤りの分だけ検査から外れる。`optional` を
  誤って `true` にした型は `LY302` が働かない。入れ子の block が必須かどうかは schema に現れないので、親の引数が
  入れ子に在る型の `optional` は schema の `required` だけでは決まらない
- 親の resource の inline の引数と attachment 型の resource の共存、同じ親への文書全体の attachment の重複（#348）
- 添字の中の参照で選ぶ親。`aws_s3_bucket.this[module.x.key]` は `aws_s3_bucket.this` を親として通す
- module の呼び出しが無い、層の置き場でないディレクトリの resource は `LY101` に当たらない。attachment 型であれば
  `LY301` が見る
- `.tftest.hcl` の `run` の `module` の `source`
- 文字列の中の `${...}` の外に書いた名前。`"${...}"` の形でない式は参照として読まない
