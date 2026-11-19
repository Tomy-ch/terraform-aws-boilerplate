# 層の規則

[ADR-0102](../../../docs/adr/0102-use-case-centric-scope.md) 決定14・17・18 の層の規則を、HCL のソースで検査する
（同 ADR の検証方法。[ADR-0501](../../../docs/adr/0501-development-tooling-composition.md) 決定5 がソースの構造の規約として
Conftest に割り当てる）。何を禁じるかは ADR が持ち、ここには再掲しない。検査(3)でどの参照を辿り、何を違反とするかは
ここが持つ（同 検証方法）。

起動は `make layer-check` で、CI では Pull Request の Static Analysis の層の job（`terraform-gates.yaml` の `tf-lint`）が
TFLint の後に走らせ、apply する木では `terraform-apply.yaml` の `apply-layer-check` が走らせる。apply の job はその成功を
待つ（[`.github/workflows/README.md`](../../../.github/workflows/README.md) の「apply の job」）。検査そのものの検査は
`make layer-check-test` である。

| 段 | make | 判定するもの |
| --- | --- | --- |
| Rego のテスト | `layer-verify` | 各 deny の ID が違反と非違反の両方のテストを持ち、テストがすべて走って通ったこと（`infra-code-gate rego-tests`） |
| HCL の入力 | `layer-detect` | [`scripts/hcl-input`](../../../scripts/README.md) が走査の root の `.tf` を構文の木として読み、block と属性ごとの参照の列を JSON（`data.hcl_input`）に書く（ADR-0501 決定5）。読んだファイルの集合を `find` の一覧と突き合わせる。構文の誤り・知らない式・`.tf.json`・symlink・`.tf` が0件のときは JSON を書かずに落ち、Conftest を起動しない |
| 検査 | `layer-detect` | 走査の root（`usecases` `modules` `env`）の下の全ファイルを1つの入力として掛ける（`--combine`）。Conftest が読んだ `.tf` と `.tf.json` の集合を `find` の一覧と、`.tf` の集合を `hcl_input` のファイルの一覧と突き合わせ、評価した規則の数を `layer.rego` の `deny contains` の数と突き合わせる（`tf-local policy-result`） |

`tf-local policy-result` は `layer.rego` の行頭の `deny contains` を数える。規則は字下げせずにこのファイルへ置く。

## 失敗の読み方

`❌ hcl-input:` で始まる行は、Conftest より前の段の失敗である。その行が名指すファイルと位置を直す（`.tf.json` は HCL で、
symlink は実体のファイルで書く）。

| ID | 意味 | 直し方 |
| --- | --- | --- |
| `LY001` | 宣言（`decl.json`）の形が違う。`attachments` が空でないオブジェクトでない、項目が `parents` と `optional` だけを持たない、`not_attachments` の理由が空、同じ型が両方の区分に在る | 宣言を直す |
| `LY002` | 走査の root に `.tf` も `.tf.json` も無い。または Conftest が読んだファイルと `find` の一覧が違う（symlink の構成ファイルなど）か、Conftest が読んだ `.tf` と `hcl_input` のファイルの一覧が違う | 一覧の差のファイルを実体のファイルにするか、走査の root から外す。`hcl_input` との差は、2つのパーサの読むファイルが食い違っている |
| `LY003` | 走査の root に JSON の構成（`.tf.json`）が在る。検査はそれを解釈しない（ADR-0501 決定5、ADR-0103 決定11） | HCL（`.tf`）で書く |
| `LY004` | `data.hcl_input` が無いか、形が違う（`files` が文字列の配列でない、block・属性・参照・段の形が違う、`within` が添字の段を指さない）。形の崩れた入力の上では、それを読む規則（`LY002` の `hcl_input` の向き・`LY005`・`LY301`・`LY302`）を評価しない | `make layer-check` から起動する。`scripts/hcl-input` の出力の形と `layer.rego` の読む形を揃える |
| `LY005` | Conftest が読んだ resource の block（ファイル・型・名前）と `hcl_input` の resource の block が違う。または attachment 型の resource の、宣言した親の引数か `for_each` の数が、Conftest の側（`lib.hcl.values_at`）と `hcl_input` で違う | 2つのパーサの受理か block の畳み方が食い違っている。両方が同じに読む書き方へ直す |
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
| `LY303` | 使われている resource の型が、宣言の `attachments` にも `not_attachments` にも無い。親の型に現れるだけでは分類に数えない | 下の基準で分類し、宣言に足す |

## 層の決め方

層はディレクトリの位置で決める（決定12）。`env/` の下のディレクトリは env、`usecases/<use-case>` は usecase、
`usecases/<use-case>/internal/<module>` は usecase の module、`usecases/<use-case>/reference` は reference、
`modules/<module>` は一般 module である。`source` はディレクトリと結んで `.` と `..` を字面で解いた後のパスで判定し、
先の実在は読んだ `.tf` のディレクトリの集合で判定する。

## 親を指す引数の辿り方

親を指す引数の式の参照は、`hcl_input` の属性の `refs` から読む。`scripts/hcl-input` が式を構文の木として歩き、参照
（根と、属性・添字・splat の段）を取り出す。リテラルの文字列、`$${` のエスケープ、コメント、`for` 式の反復変数（入れ子で
同名のものを含む）、括弧の無い1語のオブジェクトのキーは参照にならない。テンプレートの補間と指示子、条件式、関数の引数、
括弧つきのオブジェクトのキーの中の参照は、どれも参照になる。取り出した参照の集合は、`hclsyntax` の `Variables()` と
突き合わせてから書く。Rego は参照を、根と最初の段の名前（`aws_s3_bucket.this`、`local.x`）のトークンにして、次のように辿る。

- `local.<名前>` は、同じディレクトリの `locals` の、その名前の属性の参照の全体へ辿る。定義の無い `local` は辿れない参照として違反にする
- `each.key` と `each.value` は、その resource の `for_each` の参照の全体へ辿る
- 参照の根と最初の名前の直後の添字（`aws_s3_bucket.this[...]`、`local.all[...]`）の中は、親を選ぶだけなので辿らない。
  その添字の中のさらに入れ子の添字も辿らない。属性の後の添字、2つ目以降の添字、関数の呼び出し・括弧・タプルの後の
  添字、splat の後の式の中の添字は辿る。`count.index` は参照に数えない
- 最初の段が名前でない参照（根だけのものを含む）は、根だけのトークンになり、親として通らない

辿った先の参照が無いか、1つでも、同じディレクトリに宣言された、宣言した親の型の resource でなければ違反とする。
`*` を親の型に持つ引数（親の型が多態のもの）は、同じディレクトリのどの型の resource でもよい。

## 宣言

[`decl.json`](decl.json) が持つ。

- `attachments`: attachment 型の resource の型ごとに、`parents`（親を指す引数から、親として認める型の配列へ）と
  `optional`（宣言したどの親の引数も書かなくてよいか）を持つ。引数は `protected_resource.s3_bucket.bucket_name` の
  ように block をまたいだ dotted な並びで書き、`dynamic` で書いた block も同じ引数として読む。`dynamic.<名前>.content` を
  `<名前>` に畳む規則は、`lib.hcl.fold_dynamic` と `scripts/hcl-input` が同じ正規表現で持ち、どちらも何段の入れ子でも畳む
- `not_attachments`: attachment 型でない型と、その理由。親になる型も、それ自身が attachment 型でないことをここに置く

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
  誤って `true` にした型は `LY302` が働かない
- 親の resource の inline の引数と attachment 型の resource の共存、同じ親への文書全体の attachment の重複（#348）
- 参照の直後の添字の中の参照で選ぶ親。`aws_s3_bucket.this[module.x.key]` は `aws_s3_bucket.this` を親として通す
- `dynamic` の `content` の中の反復変数（`protected_resource.value`）を `for_each` へ辿ること。反復変数は辿れない参照として違反になる
- 2つのパーサ（`hcl-input` の `hclsyntax` と Conftest の hcl2 パーサ）が同じ書き方を同じに読むこと。受理が食い違う
  ファイル・resource・親の引数は `LY002`・`LY005` で落ちる側に倒れ、誤って落ちることはあっても黙って通ることはない
- module の呼び出しが無い、層の置き場でないディレクトリの resource は `LY101` に当たらない。attachment 型であれば
  `LY301` が見る
- `.tftest.hcl` の `run` の `module` の `source`
- 走査の root の外の `.tf`。root の外に構成ファイルを置くことは `make infra-code-gate` が落とす
