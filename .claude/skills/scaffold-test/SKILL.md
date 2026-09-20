---
name: scaffold-test
description: >-
  Generate a Go unit test file for an existing function / method under `scripts/`, following the viewpoints declared by the nearest ancestor README's Test Strategy section — which for this repository is `scripts/README.md`, the one place that owns them. One `TestXxx` per function or method: 1:1, no bundling. **この規約を機械的に強制する仕組みはこのリポジトリに無い** —— 守るのは書き手であり、監査するのは `/test-review` の Lens 5 である。Use it whenever a new or changed subject needs unit tests, or someone asks 「テストを書いて」「テストを追加して」. Read-only on implementation code — never edits the subject under test. Do NOT use it to review existing tests (`test-review`).
---

# Scaffold Test

既存の関数・メソッドに対する Go の unit test を生成する。様式は `scripts/README.md` の
*Test Strategy* 節と、既存の `scripts/**/*_test.go` から取る —— ケース単位の `t.Parallel()`、
入れ子の `t.Run`、日本語のケース名、`正常系` / `異常系` の外側グルーピング。

**このリポジトリのテストが守っているのは機能ではなくゲートである。** 検査器が検査をやめたとき、
報告されるのはエラーではなく緑である（ADR-0702 決定16）。したがって生成するテストの中心は、
違反を捕まえるケースではなく**退化した入力に対してエラーを主張するケース**である —— 走査結果0件、
読めない対象ファイル、解釈できない構文。ここを落としたテストは、対象を失ったツールに対して
緑を返し続ける。

**table-driven の `for` ループはこのリポジトリでは違反ではない。** 輸入元の規約を持ち込まないこと
—— `/test-review` の Lens 1 が明示的にそう述べており、既存のテストも実際に使っている
（`scripts/tool-cooldown/outdated_test.go`、`scripts/pin-images/main_test.go`）。見るべきなのは、
ループが**ケースを1つの `t.Run` に畳んで、どのケースが落ちたか分からなくなっていないか**だけである。

## When to Use

- 実装が既に存在してコンパイルが通る関数・メソッドに unit test を足すとき。
- 新しい運用機構（`scripts/<tool>/`）を足した直後。ADR-0702 決定16 はテストの存在を要求する。
- `make cover-gate` が落ちたとき（下限は `.makefiles/go/test.mk` の `COVERAGE_THRESHOLD` が持つ）。
- 手で直した関数の振る舞いが変わり、既存のテストが意図を写していないとき。

Do NOT use this skill for:

- **実 AWS を要するテスト** —— `modules/` にユースケースが入ってからの話で、層の割当は ADR-0401 決定4 が持つ。このスキルが作るのは同一パッケージの unit test である。
- 既存のテストファイル全体の書き直し —— このスキルは新しいテストを書く。部分的な手直しは手で行う。
- 実装コードの生成（テストファイルだけを書き、被テスト対象を編集しない）。
- 既存のテストのレビュー —— `/test-review` が所管する。

## What This Skill Reads / Writes

**Reads (always)**:

- `scripts/README.md` の *Test Strategy* 節 —— **観点の正本**。`/test-review` が同じ節を読んでレビューするので、生成側と監査側が対称に保たれる。**観点やアンチパターンの一覧をこのスキルへ写さない** —— 写した瞬間に、節が動いてもこちらは動かなくなる。
- [`docs/adr/0702-repository-operations-substrate.md`](../../../docs/adr/0702-repository-operations-substrate.md) 決定13-17 —— 観点の根拠。とくに決定13（0件は失敗）・決定14（解釈できない入力はエラー）・決定17（実物のツリーを読まない）。
- 対象のソースファイル —— signature、引数、戻り値、エラーセンチネル、同パッケージのヘルパ。
- 最も近い祖先の `README.md` で、**実際に Test Strategy 節を持つもの**まで歩いて解決する（見出しの語は README ごとに揺れる —— `Test Strategy` / `Test strategy` / `Testing Strategy` —— ので、語ではなく意味で合わせる）。節を持たないより近い README も読む。そこはその配下の命名・ヘルパ・不変条件を持つ。**一覧引きではなく歩いて解決すること** —— 下の一覧は歩いた先の現時点のスナップショットであり、そこに無い配下は「歩く先が無い」のではなく「まだ書かれていない」である。
  - `scripts/README.md` for `scripts/**` —— **いまはここ1つだけが Test Strategy 節を持つ**。
  - `modules/<use-case>/README.md` —— 未作成。作られたらそこが当該ユースケースの観点を持つ（ADR-0401 決定4、ADR-0102 決定2）。
- 同パッケージの sibling test —— import、ヘルパの signature、フィクスチャの組み立て方、外界の差し替え方の構造的な手本。衝突したら README が勝つ。

**Writes (with confirmation)**:

- 被テスト対象の隣に1つのテストファイル。名前は `<subject>_test.go`（`<subject>` はソースの basename から `.go` を除いたもの）。既にあれば書き直さず、新しい `TestXxx` を追記する —— 先に訊く。

**Triggers (via `make`)**:

- `make go-fmt` —— 生成したテストファイルを整形する。
- `make go-test` —— 生成したテストを走らせる。
- `make go-test-cover` + `make cover-gate` —— 総カバレッジが下限を割っていないかを見る。

**Never touches**:

- 被テスト対象のソースファイル（`<subject>.go`）。
- 生成物（terraform-docs の生成区間、`*-pin.toml`、workflow のインラインブロック）。
- 他パッケージのテストファイル。

## First Step: Resolve Target

最初の行動は `AskUserQuestion`:

- Question: 「テストを書きたい対象を指定してください」
- Options (single-select):
  - 「対象ファイル全体」 — free-text path。そのファイルの top-level 関数・メソッドを列挙し、1つにつき1つの `TestXxx` を生成する。
  - 「ファイル内の特定関数 / メソッドのみ」 — free-text `<file>:<symbol>`。その1つだけを生成する。
  - 「キャンセル」.

解決後、上の歩き上がりの規則をパスへ適用して観点の持ち主を決める（固定のプレフィックス一覧と
照合しない）。解決した README を後続の手順のために保持する。

対象のファイルが存在しなければ中断し、パスの確認を求める。

## Step 1. Read Context

1. 解決した README を読む。命名、ヘルパの様式、その配下に固有の規約はそこが正本である。
2. 対象パッケージの `*_test.go` をすべて読む。抜き出すもの:
   - top-level のテストヘルパの signature。
   - `TestXxx` の冒頭で慣習的に宣言されるフィクスチャ。
   - 実際に使われている assert の様式と import の集合。
   - **外界の差し替え方** —— `PATH` の先頭へ置くシェルスクリプト、`httptest` サーバ、`runner` の継ぎ目。
3. ADR-0702 決定13-17 を1度読む。退化入力・センチネル・フィクスチャの所在の根拠がここにある。

sibling test と README が衝突したら README が勝つ。

## Step 2. Test-Perspective Subagent

観点の列挙を、コードを1行も生成する前に subagent（`subagent_type: general-purpose`）へ出す。
この手順は必須で、本体へ畳み込まない —— 観点は Test Strategy 節と対象の signature から導かれる
ものであって、このスキルの記憶からパターンマッチで出すものではない。

**このスキルは観点の種リストを持たない。** 観点は README の責務であり、README が今日書いている
ものへ委ねる。README が動けば観点も動き、スキルの編集は要らない。

プロンプトに入れるもの（日本語）:

- 対象の signature と doc コメント。
- Step 1 で読んだ **Test Strategy 節の全文**（見出しを含めて逐語で）。subagent の仕事は、その見出しを
  この対象に対する具体的な観点へ写像することである。
- **退化入力の表**（ADR-0702 決定13-15）。対象の入力経路それぞれについて、次が**エラーを主張する
  ケース**を持つかを問わせる: 走査結果0件 / 対象ファイルが読めない・存在しない / 解釈できない構文 /
  抽出件数が独立経路とずれる / 宣言にどれにも当たらないエントリがある。
- Step 1 で観察した sibling test の様式（二次的な参照として）。

期待する戻り: `TestXxx → t.Run(正常系) → t.Run(case)` / `t.Run(異常系) → t.Run(case)` の一覧。
各ケースに、それが Test Strategy 節のどの項（または退化入力のどの形）へ遡るかの注記を添える。

Fallback behavior:

- **歩き上がりがリポジトリ直下まで達して Test Strategy 節が見つからない** —— ギャップとして
  ユーザーへ差し出す（「`<歩いたパス>` のいずれにも Test Strategy 節がないため、sibling テストの
  様式と ADR-0702 決定13-17 からフォールバックで観点を導出しています。README を補完する余地が
  あります」）。**歩いた README を名指しする** —— どこに節が要るのかが見えるように。
- subagent が観点を1つも返さなければ、最小の既定（正常系1件 + 異常系1件）へ落として警告する。

## Step 3. Plan the Test Structure

観点を具体的なテストファイルの骨子へ写像する規則。

1. **1つの関数・メソッドにつき1つの `TestXxx`。** 関数 `Foo` は `func TestFoo(t *testing.T)`。
   非公開の `parseVersion` は `func Test_parseVersion(t *testing.T)`（既存の様式）。メソッド
   `(*Client).Fetch` は `func TestClient_Fetch(t *testing.T)`。同じ対象に複数の `TestXxx` を作らない。
   - **逆向きにも効く —— 公開されたすべての関数・メソッドが自分の `TestXxx` を持ち、1:1 は
     「弱いテストを避けたい」に優先する。** assert が今のところ薄いという理由で枠を消さない。
     枠を残しておけば、意味のあるテストが後から入る場所になる。他のテストが間接的に通っている
     ことは、枠が無くてよい理由にならない。
   - **ある対象の検証を別の対象の `TestXxx` へ畳み込まない** —— 畳み込まれた側のテストの責務が
     濁る。構築子を fixture として**呼ぶ**ことは畳み込みではない（構築子そのものについての assert を
     足すことが畳み込みである）。
2. **複数の対象を1つの `TestXxx` へ束ねない —— 1:1、例外なし。** 唯一の免除は**検証不能で到達不能な
   対象**（失敗経路が `tb.Fatalf` を呼ぶヘルパなど）で、それでも規約名の `TestXxx` を宣言し、
   `t.Skip("<なぜ検証できないか>")` を書く。**「他のテストがカバーしている」は免除ではない** ——
   そのテストが縮んだ後も緑のままになる。
   **この 1:1 を機械的に強制する仕組みはこのリポジトリに無い。** 監査するのは `/test-review` の
   Lens 5（シンボル網羅）であり、生成時に守るのは書き手である。
3. **最外の2つの `t.Run` グループは、リテラルの `正常系` と `異常系` だけ。**
   - `t.Run("正常系", ...)` / `t.Run("異常系", ...)` をそのまま使う。接頭辞ではなくその2文字である。
   - **禁じる形**: top-level の `t.Run("正常系_ケース名", ...)`。グループの軸（正常系 / 異常系）と
     ケースの説明の軸を混ぜている。
   - **正しい形**:

     ```go
     t.Run("正常系", func(t *testing.T) {
         t.Parallel()
         t.Run("宣言どおりなら差分なしで返す", func(t *testing.T) { ... })
     })
     t.Run("異常系", func(t *testing.T) {
         t.Parallel()
         t.Run("走査対象が0件ならエラーを返す", func(t *testing.T) { ... })
     })
     ```

   - どちらのグループも直後に `t.Parallel()` を呼ぶ。読みやすければ、グループの内側でさらに
     入れ子にしてよい。
   - 1つの `TestXxx` に `正常系` は最大1つ、`異常系` も最大1つ。片方しか無ければもう片方は置かない
     —— 空のグループを作らない。
4. **すべての `t.Run` が `t.Parallel()` を最初の文として呼ぶ。** `t.Setenv` を使うケースがそれと
   両立しないのは既知で、**迂回ではなくケース単位の宣言で解く**（Test Strategy 節）。他のケースと
   ポインタを共有して書き換えるブロックだけは外側の `t.Run` を直列に保ち、**なぜかをブロック上の
   コメントで述べる**（`-race` が違反を捕まえる）。直列ブロックの内側のケースはそれでも
   `t.Parallel()` を呼ぶ。
5. **ケース名は日本語で、`正常系_` / `異常系_` の接頭辞を持たない。** 最外はリテラルの
   `正常系` / `異常系`、その内側は入力のクラスと期待される結果を述べる自由な日本語の文
   （「`<入力のクラス>`の場合、`<結果>`」）。既にグループの下にいるので接頭辞は二重ラベルであり、
   `go test` の出力が `正常系 > 正常系_xxx` になる。
6. **`require` と `assert` の使い分け**（Test Strategy 節）:
   - `require.NoError` / `require.Error` / `require.ErrorIs` / `require.ErrorContains` —— エラーに
     関する assert すべて（testifylint の `require-error` が `assert.ErrorIs` を弾く）。
   - `require.NotNil` / `require.True` は、**後続がそれ無しでは panic する / 無意味になる場合に限る**。
   - `assert.Equal` / `assert.Len` / `assert.Contains` / `assert.True` / `assert.Empty` —— 終端の値の
     検証。ここでの失敗はテストを止めない。
7. **失敗モードはパッケージレベルのセンチネルで、テストは `require.ErrorIs` で到達する。**
   部分文字列の assert は、呼び出し側が実際に使う情報（どのファイルがずれたか）を運ぶ場合に限り
   **上乗せ**する。単独の検査手段にしない。
8. **外界はその境界で差し替える。** `git` / `docker` / `gh` は `PATH` の先頭へ置いたシェルスクリプトで、
   HTTP は `httptest` サーバで。**判断のすぐ隣で差し替えない** —— ツールが組み立てた引数列そのものが
   テスト対象でなくなる。手書きの mock を新設する前に、同パッケージの sibling が既に持っている
   継ぎ目を探すこと。
9. **フィクスチャは `t.TempDir()` に建て、リポジトリの実物を読まない**（ADR-0702 決定17）。
   **実物を読むテストは1件でも違反**である —— 今日のリポジトリの内容で通ったり落ちたりするように
   なり、ツールについてのテストであることをやめる。

## Step 4. Confirm Plan

日本語で提示する。

- 対象のファイルパス。
- 観点の出所として解決した README。
- 生成する `TestXxx` ごとの、正常系 / 異常系のケース一覧。
- 各ケースがどの観点（または退化入力のどの形）から出たかの短い根拠。
- 提案するテストファイルの先頭 20 行ほどのプレビュー。
- skip として出す対象があれば、`TestXxx` + `t.Skip("<なぜ検証できないか>")` と、検証が不能な理由。

そのうえで `AskUserQuestion`:

- Question: 「以下の構成でテストを生成しますか？」
- Options: 「生成する」 / 「修正したい箇所を指摘する」 / 「キャンセル」.

## Step 5. Write the Test File

同パッケージの sibling test から取った様式で書く。骨格:

```go
func Test<Subject>(t *testing.T) {
    t.Parallel()

    t.Run("正常系", func(t *testing.T) {
        t.Parallel()

        t.Run("<日本語の入力クラス>の場合、<期待される結果>", func(t *testing.T) {
            t.Parallel()
            dir := t.TempDir()
            // フィクスチャを dir の下へ建てる
            actual, err := subject(dir)
            require.NoError(t, err)
            assert.Equal(t, <expected>, actual)
        })
    })

    t.Run("異常系", func(t *testing.T) {
        t.Parallel()

        t.Run("走査対象が0件ならエラーを返す", func(t *testing.T) {
            t.Parallel()
            _, err := subject(t.TempDir())
            require.ErrorIs(t, err, errNoTargets)
        })
    })
}
```

Additional generation rules:

- **各ケースは、その分岐を他と区別する結果を assert する。** エラー分岐は特定のセンチネルへの
  `require.ErrorIs`、成功分岐は結果の値・状態、境界のケースは**両側**。分岐が実行されたことだけを
  示す `require.NoError` / `assert.NotNil` 止まりの本体を出さない。
- **書き換えるツールのテストは、失敗した実行が何も書いていないことを assert する** —— エラーが
  返ったことではなく、**エラーの後のファイルの内容**に対して。
- **出力そのものが契約であるもの**（drift の一覧、`::warning::` 注釈）は、ロガーを捕まえて出力を
  assert する。
- **同パッケージのテストヘルパは、あるものをそのまま使う。** まだ無く、同じフィクスチャが生成する
  テストの中で3回以上繰り返されるなら、`t.Helper()` を付けた非公開のヘルパをファイル末尾に作る。
- **import は sibling test がやっているとおりに組み立てる。** 使わない import を足さない。

テストファイルが既にあれば書き直さない —— 既存のヘルパ宣言の後ろへ新しい `TestXxx` を追記する。
追記してよいかを先に `AskUserQuestion` で訊く。

## Step 6. Verify

順に走らせる。

1. `make go-fmt` —— 新しいテストファイルを整形する。対象外のファイルが整形されたら、その差分を
   ユーザーへ差し出す。
2. `make go-lint` —— testifylint の `require-error` ほか、静的解析が通ることを見る。
3. `make go-test` —— 生成したテストが通ることを確かめる。
4. `make go-test-cover` + `make cover-gate` —— 総カバレッジが下限を割っていないことを見る。
   下限は `.makefiles/go/test.mk` の `COVERAGE_THRESHOLD` が持つ。**ここへ数値を写さない。**
5. **退化入力のケースを摂動で確かめる。** これがこのリポジトリで最も高く付く欠陥なので、
   「エラーを主張するケース」を書いたら、**対象側の番兵を一時的に落として、そのケースが
   FAIL することを確かめ、戻す。** 摂動の下でも緑のままのケースは、何も守っていない。
   退化入力のケースに対しては必須であり、通常のケースについては回帰を固定する目的のものに対して行う。

`make go-test` が落ちたとき:

- 生成したテストファイルはそのまま残す。ユーザーが読めるように。
- 落ちたテスト名と assert のメッセージを差し出す。
- **自動で巻き戻さない** —— テストを直すのか対象を直すのかはユーザーが決める。

`make cover-gate` が落ちたときは、不足している観点を挙げて次の起動へ渡す。

## Constraints (Summary)

- ❌ 生成したテストへ、コードの言い直しや *why* の散文コメントを足すこと。ケースの意図は日本語の
  `t.Run` 名が運ぶ。コメントが要るのは `-race` の直列ブロックの理由だけで、それも1行に収める。
- ❌ 同じ関数・メソッドに複数の `TestXxx`。
- ❌ 複数の対象を1つの `TestXxx` へ束ねること（1:1、例外なし）。
- ❌ 「他のテストがカバーしている」を理由にした `t.Skip`。skip してよいのは検証不能なものだけで、
  *なぜ検証できないか*を書く。
- ❌ 被テスト対象のソースファイルを編集すること。
- ❌ 生成物を編集すること。
- ❌ `assert.ErrorIs` / `assert.NoError`（testifylint の `require-error`）。
- ❌ 理由を書かずに `t.Parallel()` を省くこと。
- ❌ 英語のケース名。
- ❌ 最外の `t.Run("正常系_xxx", ...)` / `t.Run("異常系_xxx", ...)` の接頭辞形。
- ❌ リポジトリの実物のツリーを読むテスト（ADR-0702 決定17）。
- ❌ **他所のリポジトリの規約を持ち込むこと。** table-driven の `for` ループはここでは違反ではない。
- ✅ 退化入力（0件 / 読めない / 解釈できない）に対して**エラーを主張する**ケースを持つこと。
- ✅ すべての入れ子で `t.Parallel()`（例外は理由付きで）。
- ✅ サブケースごとに `t.Run`。
- ✅ 日本語のケース名。最外はリテラルの `正常系` / `異常系`、内側は接頭辞なしの自由な日本語の文。
- ✅ エラーは `require`、終端の値は `assert`。
- ✅ センチネルへの `require.ErrorIs` で失敗モードへ到達すること。
- ✅ 外界はその境界で差し替えること。
- ✅ フィクスチャは `t.TempDir()` の下に建てること。
- ✅ 生成したテストファイルのヘルパに `t.Helper()`。
- ✅ 退化入力のケースを摂動で確かめること（対象の番兵を落とし、FAIL を確認し、戻す）。

## Checklist

Before reporting completion, confirm:

- [ ] 対象と観点の持ち主（README）を解決した。
- [ ] Step 1 で README の Test Strategy 節・ADR-0702 決定13-17・sibling test を読んだ。
- [ ] Step 2 の観点 subagent が走った。
- [ ] 対象の入力経路それぞれについて、退化入力の形を突き合わせた。
- [ ] 生成した `TestXxx` が対象と 1:1 である。skip は検証不能なものだけで、その理由が他のテストを
      名指していない。
- [ ] 最外の `t.Run` のグループ名がリテラルの `正常系` / `異常系` である。内側のケース名に
      `正常系_` / `異常系_` の接頭辞が無い。
- [ ] すべての `t.Run` の最初の文が `t.Parallel()` である。例外にはコメントがある。
- [ ] エラーの assert がすべて `require.*`、終端の値の検証がすべて `assert.*` である。
- [ ] フィクスチャが `t.TempDir()` の下にあり、リポジトリの実物を読んでいない。
- [ ] 被テスト対象のソースファイルを編集していない。
- [ ] `make go-fmt` / `make go-lint` / `make go-test` が通った。`make cover-gate` が落ちていない。
- [ ] 退化入力のケースを摂動で確かめた。
