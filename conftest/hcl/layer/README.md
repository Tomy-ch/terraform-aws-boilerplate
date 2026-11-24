# 層の規則

[ADR-0102](../../../docs/adr/0102-use-case-centric-scope.md) 決定14・17・18 の層の規則と、
[ADR-0301](../../../docs/adr/0301-least-privilege-policy.md) 決定21 の attachment の参照を、HCL のソースで検査する
（各 ADR の検証方法。[ADR-0501](../../../docs/adr/0501-development-tooling-composition.md) 決定5 がソースの構造の規約として
Conftest に割り当てる）。何を禁じるかは ADR が持ち、ここには再掲しない。検査(3)でどの参照を辿り、何を違反とするかは
ここが持つ（同 検証方法）。

起動は `make layer-check` で、CI では Pull Request の Static Analysis の層の job（`terraform-gates.yaml` の `tf-lint`）が
TFLint の後に走らせ、apply する木では `terraform-apply.yaml` の `apply-layer-check` が走らせる。apply の job はその成功を
待つ（[`.github/workflows/README.md`](../../../.github/workflows/README.md) の「apply の job」）。検査そのものの検査は、
Rego のテスト（`layer-verify`）と、`scripts/hcl-input`・`scripts/hcl-tally`・`scripts/provider-schema` などの Go のテストである。

| 段 | make | 判定するもの |
| --- | --- | --- |
| Rego のテスト | `layer-verify` | 各 deny の ID が違反と非違反の両方のテストを持ち、テストがすべて走って通ったこと（`infra-code-gate rego-tests`） |
| 独立の数え | `layer-inputs` | [`scripts/hcl-tally`](../../../scripts/README.md) が、走査の root の下の通常のファイルの一覧、構成ファイル（`.tf`・`.tf.json`）の一覧、構文として読むファイルごとのトップレベルの block の行頭の数を、hcl-input とは別の経路で書く（ADR-0702 決定15）。root が1つでも無いか `.tf` が0件なら落ちる |
| Provider の schema | `layer-schema` | [`scripts/provider-schema`](../../../scripts/README.md) が、git が追跡している `.terraform.lock.hcl` を `hcl-tally` の一覧の lock と突き合わせ、`hashicorp/aws` の版ごとに、その版だけを持つ一時の構成を作る。`provider-schema fetch` がそれぞれを `init -lockfile=readonly` して `terraform providers schema -json` を取り、`provider-schema merge` が型ごとの書ける引数（`arguments`）・非推奨の書ける引数（`deprecated`。`make tf-policy` と同じ関数で取り出す）・block の名前（`blocks`）の和を JSON（`data.layer_schema`）に書く。lock が0件、Provider の block があるのに版が読めない、版の形が違う、init か schema の取得が失敗した、ある版の schema が読めないか型が0件のときは JSON を書かずに落ち、Conftest を起動しない。版ごとの schema は lock の内容が変わるまで `tmp/layer-check/schema/` に残し、取り直さない |
| HCL の入力 | `layer-detect` | [`scripts/hcl-input`](../../../scripts/README.md) が走査の root（`usecases` `modules` `env`）の下のファイルを読み、block・入れ子の block・属性を、式の構文の木と参照の列とともに JSON に書く（ADR-0501 決定5）。読んだファイルの集合を `hcl-tally` の一覧と突き合わせる。構文の誤り・知らない式・symlink・`.tf` が0件のときは JSON を書かずに落ち、Conftest を起動しない |
| 検査 | `layer-detect` | その JSON を1つの入力として掛ける。入力の形と、構文として読んだファイルの集合・トップレベルの block の数を独立の数え（`hcl-tally` のファイルの一覧・行頭の数え）と突き合わせ（`lib.hcl_input.input_errors`。ADR-0702 決定15）、入力の `.tf` と `.tf.json` の集合を構成ファイルの `hcl-tally` の一覧と突き合わせ、評価した規則の数を `layer.rego` の `deny contains` の数と突き合わせる（`tf-local policy-result`） |

`tf-local policy-result` は `layer.rego` の行頭の `deny contains` を数える。規則は字下げせずにこのファイルへ置く。

上の「落ちたら Conftest を起動しない」順序は、make が段ごとに止まることが持つ（`.makefiles/terraform/layer.mk`）。各段が
落ちる条件は、それぞれの道具の Go のテストが固定する。Conftest の終了コードは捨て、`tf-local policy-result` が結果を読めないか
規則の数と合わないことで落とす。

## 失敗の読み方

`❌ hcl-input:` で始まる行は、Conftest より前の段の失敗である。その行が名指すファイルと位置を直す（`.tf.json` は HCL で、
symlink は実体のファイルで書く）。

| ID | 意味 | 直し方 |
| --- | --- | --- |
| `LY001` | 宣言（`decl.json`）の形が違う。`attachments` が空でないオブジェクトでない、項目が `parents`・`optional`・`parent_arguments`・`whole_document` だけを持たない、`parent_arguments` の親の型がその項目の `parents` に無い、`whole_document` が `true` なのに親の引数が1つでない、`not_attachments` の理由が空、同じ型が両方の区分に在る、`unrelated_deprecated_arguments`・`inline_owned` の形が違うか親の型でない型を持つ、同じ引数が `parent_arguments` と `unrelated_deprecated_arguments` の両方に在る、`inline_owned` の引数がどの `parent_arguments` にも無い、同じ引数の書かないときの値が項目の間で食い違う | 宣言を直す |
| `LY002` | 走査の root に `.tf` も `.tf.json` も無い。または入力（hcl-input）が読んだ `.tf` と `.tf.json` の集合と、構成ファイルの `hcl-tally` の一覧が違う | 一覧の差のファイルを実体のファイルにするか、走査の root から外す |
| `LY003` | 走査の root に JSON の構成（`.tf.json`）が在る。検査はそれを解釈しない（ADR-0501 決定5、ADR-0103 決定11） | HCL（`.tf`）で書く |
| `LY004` | 入力の形が違う（`lib.hcl_input` の読む形。block・属性・式・参照・段の形、添字の段の式の種類、`within` が添字の段を指すこと）か、構文として読んだファイルの集合が `hcl-tally` の一覧と、ファイルごとのトップレベルの block の数が行頭の数えと違うか、その独立の数えが渡されていない。このときは `LY004` だけで落ち、入力を読むほかの規則を評価しない | `make layer-check` から起動する。`scripts/hcl-input` の出力の形と `conftest/hcl/lib/hcl_input.rego` の読む形を揃える |
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
| `LY304` | 親の型の resource が、attachment 型と同じ設定を書くと宣言した引数（`parent_arguments`）を書いている。値を問わない（`null`・`[]`・空の block・`dynamic` も書いたと数える）（決定18） | attachment 型の resource で書く |
| `LY305` | 文書全体を置き換える型（`whole_document`）の、同じ型の別々の resource が、同じ親を指す。親の選び方（下の「親の選び方」）がどちらも添字なしか、同じリテラルの添字か、一方が添字なし | 1つの resource にまとめる |
| `LY306` | Provider の schema との突き合わせ。`data.layer_schema` が無いか空か形が違う、親の型が schema に無いか書ける引数を1つも持たない、親の型の非推奨の書ける引数がどの `parent_arguments` にも `unrelated_deprecated_arguments` にも無い、宣言の引数が schema の書ける引数に無い、`unrelated_deprecated_arguments` の引数が非推奨でない、書かないときの値が schema の種類（block は `[]`、属性は `null`）と合わない | Provider の版を上げたなら、足された非推奨の引数を分類する。宣言の引数名を直す |
| `LY307` | 排他に持つと宣言した親の引数（`inline_owned`）を `parent_arguments` に持つ attachment 型の resource が、その親を指す（決定18 のただし書き） | 親の引数で書く |
| `LY308` | 文書全体を置き換える型の、同じ型の別々の resource が同じ親の葉を指し、同じ instance かを参照から判定できない。どちらかが `count` か `for_each` を持つか、親の選び方が判定できない | 1つの resource にまとめるか、別々の instance をリテラルの添字で指す |
| `LY309` | `aws_iam_role_policy_attachment` の `policy_arn` が、同じ module の `aws_iam_policy` だけを参照せず、AWS managed policy の ARN でもない（ADR-0301 決定21。下の「policy の参照」） | 同じ module に `aws_iam_policy` を置いて参照する。AWS managed policy は、ARN を partition だけ `data.aws_partition` から組んで書く |
| `LY303` | 使われている resource の型が、宣言の `attachments` にも `not_attachments` にも無い。親の型に現れるだけでは分類に数えない | 下の基準で分類し、宣言に足す |

## 層の決め方

層はディレクトリの位置で決める（決定12）。`env/` の下のディレクトリは env、`usecases/<use-case>` は usecase、
`usecases/<use-case>/internal/<module>` は usecase の module、`usecases/<use-case>/reference` は reference、
`modules/<module>` は一般 module である。`source` はディレクトリと結んで `.` と `..` を字面で解いた後のパスで判定し、
先の実在は読んだ `.tf` のディレクトリの集合で判定する。

## 親を指す引数の辿り方

親を指す引数の式の参照は、入力の属性の `refs` から読む。`scripts/hcl-input` が式を構文の木として歩き、参照
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

### 親の選び方（LY305・LY308）

文書全体を置き換える型の resource ごとに、親を指す引数から辿った親の葉と、その葉の選び方を決める。選び方は、葉の参照の
最初の名前の直後の段で読む。段が無いか属性なら「添字なし」、添字で、入力の式の種類が `literal` なら「リテラル」
（数の形の文字列は数に直し、`this[0]` と `this["0"]` を同じに読む）、ほかは「判定できない」とする。`local` を経た葉は、
その `local` の定義の参照の選び方を引き継ぐ。`local` を添字で選んだ先（`local.all[each.key]`）と、`each` を経て
`for_each` の式へ辿った先の葉は「判定できない」とする。比べるのは、同じ型で同じディレクトリの別々の resource の組だけ
である。1つの resource の `count` と `for_each` の instance どうしは比べない。

### policy の参照（LY309）

`aws_iam_role_policy_attachment` の `policy_arn` は、次のどちらかであれば通す。

- 参照を上の辿り方で辿った先がすべて、同じディレクトリに宣言された `aws_iam_policy` である
- 式そのものが AWS managed policy の ARN である。リテラルなら `arn:<partition>:iam::aws:policy/<path><name>` の形
  （partition は `aws` か `aws-` で始まる名前）、テンプレートなら `arn:` と `${<partition>}` と `:iam::aws:policy/<path><name>`
  の3つの部分だけを持ち、補間の参照を辿った先が `data.aws_partition` だけである

ARN を `local` に置いて `policy_arn` から参照する形は、AWS managed の形として読まない（参照の辿り方で読み、違反になる）。
決定21 は role への付与だけを定めるので、user と group の attachment 型は見ない。

## 宣言

[`decl.json`](decl.json) が持つ。

- `attachments`: attachment 型の resource の型ごとに、`parents`（親を指す引数から、親として認める型の配列へ）と
  `optional`（宣言したどの親の引数も書かなくてよいか）を持つ。引数は `protected_resource.s3_bucket.bucket_name` の
  ように block をまたいだ dotted な並びで書き、`dynamic` で書いた block も同じ引数として読む。`dynamic.<名前>.content` を
  `<名前>` に畳む規則は `scripts/hcl-input` が持ち、何段の入れ子でも畳む
- `not_attachments`: attachment 型でない型と、その理由。親になる型も、それ自身が attachment 型でないことをここに置く
- `attachments` の各項目の `parent_arguments`: 親の型から、その attachment 型と同じ設定を書く親の引数と、書かないときの値
  （属性は `null`、block は `[]`）へ。無ければ空のオブジェクトを明示する。`whole_document`: 親の設定の文書全体を
  置き換える型か（真偽値）。省略を「無い」と読まないため、全項目で明示する
- `unrelated_deprecated_arguments`: attachment 型の親の型の、非推奨だが attachment 型と同じ設定を書かない引数と、
  理由（`reason`）と書かないときの値（`unwritten`）。非推奨の引数は、`parent_arguments` かここのどちらかに必ず置く（`LY306`）
- `inline_owned`: 親の設定の集合全体を親の引数で排他に持つと宣言した引数と、理由。その引数は `LY304` の対象から外し、
  同じ設定を書く attachment 型をその親に置くことを `LY307` で落とす
- Policy Test は、保護の対象の型について `parent_arguments` と `unrelated_deprecated_arguments` から ADR-0402 決定17 の
  宣言を導く（`conftest/plan/README.md` の「宣言」）

### 分類の基準

attachment 型とは、親の resource をちょうど1つ名指し、その親の属性（policy、設定、規則の集合、関連付け）を書き換える
resource である。親と引数の組で同一性が決まり、自前の名前や ARN を持っても親の存在を前提にする。

- 2つの resource を結ぶ関連付け（`aws_iam_role_policy_attachment` など）は attachment 型に含め、親は守られる側
  （不変条件を持つ側）とする。もう一方の引数は宣言しない
- 自前の identity と lifecycle を持つ子 resource（`aws_lb_listener` など）と、アカウントかリージョンの単位で設定する
  resource は `not_attachments` に置く。親を Terraform が作らない型も、親を同じ module に置けないので `not_attachments` に置く
- 親を指す引数と親の型は、Provider の schema で確かめる（ADR-0501 決定21）。`optional` は、宣言した親の引数が
  schema でどれも `required` でないときに `true` とする
- `parent_arguments` に載せるのは、Provider の schema が非推奨とし、その attachment 型を使うよう告げる引数と、
  非推奨でなくても resource の NOTE が attachment 型との併用で上書きか衝突になると告げる引数である（ADR-0102 決定18）。
  NOTE が `lifecycle` の `ignore_changes` との併用を認める引数や、tag のように key の単位の衝突しか告げない引数は載せない
- `whole_document` は、Provider の docs か AWS の API の docs が、親の設定の全体を置き換える、または1つの親に1つしか
  置けないと明言する型だけを `true` とする。確かめられない型は `false` とする

## 保証しないこと

- 宣言の正しさ。分類、親の引数、親の型、`optional` を誤った型は、その誤りの分だけ検査から外れる。`optional` を
  誤って `true` にした型は `LY302` が働かない
- 文書全体を置き換える型の1つの resource の、`count` と `for_each` の instance どうしが同じ親を指す重複のうち、親を
  `each.key`・`count.index`（`for_each` が親の resource そのものなら `each.value` も）で直接選ばない形のもの
  （ADR-0102 保証範囲）。別々の resource の間で判定できない組は、`LY308` で落ちる側に倒す
- 決定18 の宣言の正しさ。同じ設定を書く親の引数（`parent_arguments`）は resource の NOTE が告げるものを選び、
  文書全体を置き換えるか（`whole_document`）は Provider の docs と AWS の API の docs で判定する。どちらも機械判定
  できないので、宣言を変える Pull Request のレビュー観点とする（ADR-0102 検証方法）
- lock を持たない一般 module が init で選ぶ Provider の版の schema。`layer-schema` は追跡している lock の版の和だけを読む
- 参照の直後の添字の中の参照で選ぶ親。`aws_s3_bucket.this[module.x.key]` は `aws_s3_bucket.this` を親として通す
- `dynamic` の `content` の中の反復変数（`protected_resource.value`）を `for_each` へ辿ること。反復変数は辿れない参照として違反になる
- 入れ子の block と属性の数。独立の数えと突き合わせるのはファイルの集合とトップレベルの block の数だけで、入れ子の
  block と属性は独立に数えない。`hcl-input` が入れ子の block（属性を持たないもの・`dynamic` を含む）と属性を落とさず出すことは、
  `hcl-input` の Go のテストが固定する
- module の呼び出しが無い、層の置き場でないディレクトリの resource は `LY101` に当たらない。attachment 型であれば
  `LY301` が見る
- `LY309` の、参照とリテラルを混ぜた式のリテラルの部分。`var.x ? aws_iam_policy.p.arn : "arn:aws:iam::123456789012:policy/x"`
  は、参照がすべて `aws_iam_policy` なので通る
- `.tftest.hcl` の `run` の `module` の `source`
- 走査の root の外の `.tf`。root の外に構成ファイルを置くことは `make infra-code-gate` が落とす
