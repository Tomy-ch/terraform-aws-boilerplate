---
name: test-review
description: >-
  Independent quality review of the tests that pin this repository down — the Go tests under `scripts/**` that hold the operations tooling, and the Terraform-side tests (`terraform test` / Contract / Policy) that hold `modules/<use-case>/` — structural compliance, gate-degeneracy coverage (does a case assert an error when the tool has nothing to inspect?), semantic quality (weak assertions, message-instead-of-sentinel, over-stubbing), branch x meaning completeness read from the subject source, and symbols with no test at all. Defaults to the changed test files; branch-vs-base or specific paths are selectable at the start. Read-only — it reports and the user fixes. Use it after writing or changing tests, before a PR, or whenever someone asks 「テストをレビューして」「このテストで十分か」「テスト観点が足りているか」. The only owner of the test viewpoint: `/impl-review` is the peer asked for separately under the Review Phase Protocol in `AGENTS.md`, `/settle-comments` runs as the last step of implementing rather than as a review, and none of them chains another. Do NOT use it to review implementation code (`impl-review`) or to judge comments (`settle-comments`).
---

# Test Review

Adversarial, low-bias review of Go test files. Read-only — surfaces what looks broken / under-tested / over-tested, and the user decides how to act.

## なぜこのスキルが要るか

このリポジトリのテストが守っているのは、機能ではなく**ゲートである**。

> 検査器が検査をやめたとき、報告されるのはエラーではなく緑である。
> （[ADR-0702](../../../docs/adr/0702-repository-operations-substrate.md) 決定16）

したがって「テストが通った」は「ツールが働いている」の証拠にならない。テストが**退化した入力**
—— 対象0件、読めないファイル、解釈できない構文 —— に対してエラーを主張していなければ、その
ツールは検査をやめた日に緑を返す。それを見つけるのがこのスキルの主目的である。

## When to Use

- commit / PR の前に、変更に含まれる `*_test.go` に対して。
- 新しい運用機構（`scripts/<tool>/`）を足した直後。ADR-0702 決定16 はテストの存在を要求するが、
  **存在することと、検査をやめた状態を落とすことは別**である。
- カバレッジは高いのにリグレッションが素通りするとき。構造としては準拠しているが意味が薄い、の兆候。
- 特定パッケージの単独監査として。

Do NOT use this skill for:

- 実装コードのレビュー — `/impl-review`。
- コメントの判定 — `/settle-comments`。
- 修正の適用 — 本スキルはファイルを編集しない。報告し、ユーザが直す。
- テストを**走らせる**こと — `make go-test` / `make go-test-cover` の仕事。ここは書かれたテストを読む。

## What This Skill Reads / Writes

**Reads (always)**:

- [`scripts/README.md`](../../../scripts/README.md) の **Test Strategy** 節 —— このリポジトリの
  テスト観点の正本。レイヤーの README は無く、ここが唯一の持ち主である。**この節をハードコードで
  複製しない**（ドキュメントが動いた瞬間にずれる）。実行時に読み、そこにある観点を適用する。
- [`docs/adr/0702-repository-operations-substrate.md`](../../../docs/adr/0702-repository-operations-substrate.md)
  決定13-17 —— 検査の規律とテストの要件。観点の根拠がここにある。
- [`docs/adr/0401-testing-policy.md`](../../../docs/adr/0401-testing-policy.md) —— レイヤーと責務の表。
  対象が `modules/**` / `examples/**` 側のテストである場合の観点はこちらが持つ。
- 対象の `*_test.go`。
- 対応する subject（同パッケージの、`_test` を除いた `.go`）。**code-origin の2レンズ（Lens 4 / Lens 5）
  はこれが無いと成立しない。**
- 同パッケージの sibling test —— 確立した書き方（ヘルパの signature、フィクスチャの組み立て方、
  外部コマンドの差し替え方）の参照。

**Writes**: 何も書かない。出力は会話へ描画される日本語レポートのみ。

**Never touches**: テストファイル / subject / 生成物 / `.claude/` 配下。

## 2つの対象、1つの観点の持ち主

このリポジトリのテストは2つの区分に分かれ、**観点の正本が違う**。どちらを見ているかを最初に確定する。

| 区分 | 対象 | 観点の正本 |
| --- | --- | --- |
| 運用機構 | `scripts/**/*_test.go` | [`scripts/README.md`](../../../scripts/README.md) Test Strategy 節、[ADR-0702](../../../docs/adr/0702-repository-operations-substrate.md) 決定13-17 |
| インフラ | `modules/<use-case>/tests/**`、`examples/**`、`*.tftest.hcl`、Policy Test | [ADR-0401](../../../docs/adr/0401-testing-policy.md) 決定4 のレイヤー表、[ADR-0402](../../../docs/adr/0402-policy-test-scope-and-verification.md) |

**インフラ側のレンズは層の割当を見る。** ADR-0401 決定4 は故障モードごとに検出手段を割り当てており、
テストがどの層に属するかは書き手の好みではない。

| 層 | 検出する故障モード | 実 AWS |
| --- | --- | --- |
| Static Analysis | 構文・書式、既知の誤設定、本リポジトリの規約違反 | 不要 |
| Contract Test | 公開 `variable` / `output` の schema の意図しない変更 | 不要 |
| Policy Test | security / reliability invariant の違反 | 不要 |
| Unit Test | 入力から構成を導出するロジックの誤り | 不要 |
| Integration Test | resource 間の結合、AWS API 側の制約 | 必要 |
| E2E Test | 利用者視点の振る舞い、権限境界の実効性 | 必要 |

インフラ側で追加に見るもの:

- **層の取り違え。** 実 AWS を要さずに検出できる故障モードを Integration / E2E へ置いていないか
  （ADR-0401 決定5-6、ADR-0501 決定30）。逆に、resource 間の結合でしか出ない故障を Unit で
  検出したつもりになっていないか。
- **`terraform plan` の成功を正しさの根拠にしていないか**（ADR-0401 決定1）。
- **example がテストから参照されているか**（決定11-13）。参照されない example は
  documentation でも fixture でもなくなる。
- **security invariant が Policy Test として実装されているか**（決定18-20）。**レビューでの目視確認に
  依存した invariant は「保証されている」と扱えない。** 実装されていない invariant を README や ADR が
  保証として述べていれば、それは finding（ADR-0701 決定12）。
- **破棄の失敗を検出できる構成か**（決定14-16）。実 AWS を使うテストで、残留 resource を特定できる
  手段が無ければ finding。
- **Trivy と Conftest の境界**（ADR-0402）—— 「このリポジトリの ADR を読まなければ書けないルールか」で
  分かれる。読まずに書けるルールが Conftest 側に居たら、それは Trivy の取り分である。

**`modules/<use-case>/` を触ったなら、そのユースケースの `docs/adr/` も観点の出所になる**
（ADR-0001 決定12-14）。root ADR だけを読んで判定しない。

## First Step: Resolve Scope

`AskUserQuestion`:

- Question: 「test-review の対象スコープを指定してください」
- Options (single-select):
  - 「変更ファイル（作業ツリーの差分, 推奨）」 — `git diff --name-only` から `*_test.go` を抽出。
    新規追加（`--diff-filter=A`）も含める。
  - 「ブランチ base 比較」 — base は PR があればその `baseRefName`、無ければ `make -s base-branch`
    （`origin` の実状態から最新のリリースラインを解決する）。`gh repo view --json defaultBranchRef`
    は使わない —— GitHub のデフォルトブランチは活きているリリースラインより遅れており、レビュー範囲が
    1世代分黙って広がる。
  - 「特定パス / パッケージ（自由入力）」
  - 「キャンセル」

対象が `scripts/**` か `modules/**` かで走るレンズが変わる。両方が動いていれば両方を走らせ、
**レポートで区分を分ける** —— 観点の正本が違うものを1つの一覧に混ぜると、どちらの基準で見た
finding なのかが読み取れなくなる。

```sh
BASE=$(gh pr view --json baseRefName -q '.baseRefName' 2>/dev/null || make -s base-branch)
test -n "$BASE" || { echo "ベースブランチを解決できませんでした"; exit 1; }
git diff --name-only "origin/${BASE}...HEAD" -- 'scripts/**/*_test.go'
```

対象の `*_test.go` が1件も無ければ、その旨を告げて終わる。**テストがまだ無い subject を見たい
場合は、そのパスを「特定パス」で指定する** —— `*_test.go` の不在はまさに Lens 5 の主題であり、
Lens 1-3 はそれに対して読むものを持たない。

各対象について subject（同パッケージ、basename から `_test` を除いたもの）を解決する。

## Step 1. Read Context

対象ごとに:

1. `scripts/**` が対象なら `scripts/README.md` の Test Strategy 節と ADR-0702 決定13-17 を読む（各1回）。
2. `modules/**` / `examples/**` が対象なら ADR-0401 決定4-20、ADR-0402、および
   `modules/<use-case>/README.md` と `modules/<use-case>/docs/adr/` を読む。
3. subject を読む。
4. 同パッケージの sibling test を読む。

どちらの正本も当たらない対象（区分の外）に対しては、比較基準が無い。その場合は**そう明記する**
—— 基準の無いレンズが「何も出なかった」と報告すると、合格と読まれる。

## Step 2. Fan Out Five Adversarial Reviewers

`adversarial-reviewer` を5つ**並行**で起動する（`subagent_type: adversarial-reviewer`、既定 `sonnet`
＝実装者が Opus なら reviewer ≠ implementer が成立する。orchestrator はモデルを上書きしてよい）。

5つのうち2つは **code-origin（subject 起点）** である —— テストファイルではなく subject から読み
始めるので、**テストが1つも無いコード要素が視野に入る**。Lens 5（規約名の `TestXxx` が存在するか）と
Lens 4（テストがある関数の中で、各分岐が到達され、固有の結果を assert されているか）。残る3つ
（Lens 1 / 2 / 3）はテストファイルまたは規約から読み始める。テストファイル起点の読み方では構造上
見えない「到達可能だが未検証のコード」を拾うのが、この code-origin の対です。

各 subagent には同じ Step 1 のコンテキスト束（Test Strategy 節、ADR-0702 決定13-17、対象テスト、
subject、sibling test）を渡し、レンズのプロンプトだけを変える。

### Lens 1: Structural Compliance

`scripts/README.md` Test Strategy 節が課す機械的な規約への準拠を見る。実行時に読んだ内容を適用し、
ここに写しを持たない。現時点で節が課しているもの:

- `main` が引数を組み立てて `run` を呼ぶだけになっており、不純な依存（作業ディレクトリ、HTTP
  クライアント、時刻、コマンドランナー）が `run` の引数であること。**分岐がテストから到達可能で
  あること**がこの分け方の目的なので、`run` に到達していないテストは構造違反。
- `t.Parallel()` がケース単位で宣言されていること。`t.Setenv` を使うケースがそれと両立しないのは
  既知で、**迂回ではなくケース単位の宣言で解く**。`t.Parallel()` を親から一括で外していたら違反。
- サブケースが `t.Run` の中にあること（`t.Run` の外に裸の assert を置かない）。
- ケース名が日本語であること。
- エラーの assert が `require.*`、終端の値の assert が `assert.*` であること。
  `require.NotNil` / `require.True` は、**後続のコードがそれ無しでは panic する / 無意味になる場合に
  限り**正しい。後で誰も使わない値に対する `require.NotNil` は終端なので `assert.*` へ。
- 失敗モードがパッケージレベルのセンチネルであり、テストが `require.ErrorIs` で到達していること。
- フィクスチャが `t.TempDir()` の下にあり、リポジトリの実物を読んでいないこと（ADR-0702 決定17）。
  **実物を読むテストは1件でも違反**。今日のリポジトリの内容で通ったり落ちたりするようになる。
- 外界（`git` / `docker` / `gh` / レジストリ）が、その境界で差し替えられていること。

**table-driven の `for` ループは、このリポジトリでは違反ではない。** 他所の規約を持ち込まないこと。
ここで見るのは、ループが**ケースを1つの `t.Run` に畳んで、どのケースが落ちたか分からなくなって
いないか**だけである。

Output: `file:line` と違反した規約を添えた finding のリスト。

### Lens 2: 退化入力のカバレッジ（このリポジトリ固有・最重要）

**このレンズがこのスキルの存在理由である。** subject がゲートであるとき、Test Strategy 節の
「違反だけでなく、退化した入力を固定する」に対して、対応するケースが存在するかを見る。

subject の入力経路それぞれについて、次の退化形が**エラーを主張するケース**を持つか:

| 退化形 | 対応するケースが無いとどうなるか |
| --- | --- |
| 走査結果が**0件** | 「ゲートが外れた」と「合格した」が区別できない（ADR-0702 決定13） |
| 対象ファイルが読めない / 存在しない | 黙って対象範囲が縮む |
| 解釈できない構文（別記法の YAML、引用符付きキー、フロースタイル、壊れた TOML） | 取りこぼしが合格として報告される（決定14） |
| 抽出件数が独立経路の件数と食い違う | 抽出器が壊れても誰も気付かない（決定15） |
| 宣言に**どれにも当たらないエントリ**がある | 消えた対象へのバイパスが残り続ける（決定18） |

いずれかに対応するケースが無ければ **退化入力未固定** の finding → 重大度 **修正必須**。
subject の該当する入力経路の `file:line` と、提案する `t.Run` のケース名を添える。

**「テストが通っているから大丈夫」は反証にならない。** このレンズが探しているのは、テストが
通ったまま検査が何も見なくなる経路そのものである。

Output: 退化形ごとに、カバーの有無、subject の `file:line`、提案するケース名。

### Lens 3: Semantic Quality

assert が実際に意味を持っているかを、Test Strategy 節を SSOT として見る。実行時に読んだ内容を
適用し、ここにアンチパターンの目録をハードコードしない。現時点で節が課しているもの:

- **メッセージだけを見ている assert。** 部分文字列の assert が単独の検査手段になっている場合、
  文言を変えただけで別のエラーに対する通るテストへ静かに変わる。センチネルへの `require.ErrorIs`
  が下に無ければ finding。
- **窓に片側しか無い。** 閾値と比較するもの（日数、桁数）で、境界値そのものとその1つ手前の対に
  なっていない。内側に余裕のある1ケースでは `>=` と `>` を区別できない。
- **過剰なスタブ。** 外界をその境界ではなく、判断のすぐ隣で差し替えている。ツールが組み立てた
  引数列そのものがテスト対象でなくなる。
- **skip が実行として通っている。** `t.Skip` が既定の出力では見えず、報告より少ない検査で緑を残す。
  skip を失敗へ変える環境変数（`REQUIRE_SHELLCHECK` 型）が無い skip は finding。
- **失敗後のファイル内容を assert していない。** 書き換えるツールで、エラーが返ったことだけを
  assert し、作業ツリーが手つかずであることを見ていない。
- **出力そのものが契約なのに、出力を assert していない。** drift の一覧、`::warning::` 注釈。
- テスト名が assert より多くを約束している。

Output: `file:line`、違反した観点、なぜその assert が弱いかの一文。

### Lens 4: Viewpoint Gap — Branch × Meaning Completeness（subject 起点）

subject を読み、関数・メソッドごとに2軸の網羅行列を建てる。カバレッジ ≠ 意味の網羅：分岐は、
その分岐を他と区別する性質について何も assert しないケースによって実行され得る。

**Lens 5 との分担**: Lens 4 は `TestXxx` が既に存在するシンボルの**内側**を見る。「シンボルに
テストが1つも無い」は **Lens 5 の finding** であり、Lens 5 が既に挙げたシンボルの分岐をここで
列挙し直さない（1つのギャップであって N 個ではない）。

**Axis A — 分岐網羅**: subject の各論理分岐が、少なくとも1ケースで到達される。

- 条件分岐の肯定側・否定側それぞれにケースがある。
- 宣言または返却されるセンチネル（`errXxx`）それぞれに到達するケースがある。
- 制約付きの値に対する境界値の対（境界そのもの / 1つ手前）がある。
- ゼロ値・nil を拒む構築子に、拒否するケースがある。
- **外部コマンドの終了コードの分岐**（`Run()` が非ゼロを返す経路）にケースがある。ここは PATH の
  先頭に置いたスクリプトで到達できるので「再現不能」ではない。

カバーされていない分岐は **分岐未カバー** → 重大度 **追加検討**。subject の `file:line` と提案する
`t.Run` のケース名を挙げ、**criticality（1-10）** を本番影響で付け、降順に並べる：
9-10 ゲートが何も見なくなる / 秘密の露出 / 取り返しのつかない操作 · 7-8 誤った判定（通すべきものを
落とす、落とすべきものを通す）· 5-6 軽微な edge · 3-4 網羅性のための nice-to-have · 1-2 任意。
**構造準拠（修正必須）には criticality を付けない** —— あれは常に今直すものである。

**Axis B — 意味網羅**: カバーされている各分岐のケースが、その分岐に**固有の**結果を assert している。

- エラー分岐が `require.ErrorIs` で特定のセンチネルを assert している（`require.Error` だけではない）。
- 成功分岐が、他の分岐と区別される結果の値・状態を assert している（`require.NoError` だけではない）。
- 書き換えを行う分岐が、書き換え後のファイル内容を assert している（呼び出しが返ったことだけではない）。
- 境界のケースが、境界の**両側で異なる結果**を assert している（受け入れ側だけではない）。

カバー済みだが固有の結果を assert していない分岐は **分岐カバー済み・意味未検証** → 重大度 **再考**。

### Lens 5: Subject Symbol Completeness（subject 起点）

subject から読み始める。単一の仕事は、subject の公開シンボル表に対して「そもそもテストが存在するか」
に答えること。テストファイル起点の読み方（Lens 1）は、見つけた `TestXxx` しか判定できない。ゼロ
テストのシンボルはそこからは見えない —— 到達可能だが未検証のコードが漏れるのはその死角である。

1. **シンボル表を建てる。** subject（生成物でも `*_test.go` でもない `.go`）から、テストが期待される
   シンボルを列挙する: 公開の関数・メソッド・構築子、および分岐を持つ非公開のパッケージレベル関数。
   ADR-0702 決定16 は「検査ロジック」全体を対象にしているので、`run` から呼ばれる純関数は
   非公開でも対象。
2. **各シンボルを `TestXxx` へ突き合わせる。** 本体が `t.Skip` だけのものは、理由が「なぜ検証不能か」
   を述べている場合に限り満たされているとみなす。「他のテストがカバーしている」と述べる skip は
   **未充足** —— そのテストが縮んだ後も緑のままになる。
3. **未充足のシンボルを** **シンボル未カバー** → 重大度 **補完推奨** として挙げる。`symbol @ file:line`、
   提案する `TestXxx` 名と骨子、Lens 4 Axis A と同じ **criticality（1-10）** を添え、降順に並べる。

## Step 3. Verify Each Finding

各 finding を独立した `review-verifier` subagent（`subagent_type: review-verifier`、既定 `sonnet`）へ
渡す。verifier は finder を信用せず、コードから結論を導き直す。曖昧さが残るなら CONFIRMED ではなく
PLAUSIBLE / REFUTED を付ける。

- **CONFIRMED** — 規約違反 / ギャップ / 弱い assert が実在し、再現可能。
- **PLAUSIBLE** — もっともらしいが、verifier が推論の連鎖を再現しきれなかった。
- **REFUTED** — 反証がある（引用行がコメントだった、その assert は文脈上十分だった等）。

検証は finding 間で並行に走らせる。REFUTED は報告から落とす（件数だけはサマリに残す —— ユーザが
ノイズフロアを知れるように）。

## Step 4. Synthesize Report

日本語のレポートを1本。

```text
# Test Review レポート

対象: <スコープ + ファイル一覧>
区分: 運用機構（scripts/**）/ インフラ（modules/**）—— 走らせた区分だけを挙げる
レンズ: 構造準拠 / 退化入力 / 意味的品質 / 観点ギャップ(branch×meaning) / シンボル網羅
      （インフラ区分は上記に加えて 層の割当 / Policy invariant の実装有無 / example の参照）
verifier 通過: CONFIRMED <n> 件 / PLAUSIBLE <m> 件 / REFUTED <k> 件（フィルタ済み）
未監査の観点: 実装（/impl-review）・コメント（/settle-comments）は本スキルの対象外

## サマリ
- 修正必須: <件数>
- 補完推奨: <件数>
- 再考: <件数>
- 追加検討: <件数>

## 退化入力（修正必須）
- [<severity>] <subject file:line> — <退化形>に対応するケースが無い
  - このまま壊れると: <検査が何も見なくなる経路の一文>
  - 提案: t.Run("<case name>", ...)
  - 出典: `scripts/README.md` Test Strategy / ADR-0702 決定<n>
  - verifier: CONFIRMED / PLAUSIBLE

## 構造準拠（修正必須）
- [<severity>] <file>:<line> — <違反した規約>
  - 詳細 / 出典 / verifier

## シンボル網羅（補完推奨・criticality 降順）
- シンボル未カバー: <symbol @ subject file:line>（対応する TestXxx 皆無）
  - criticality: <1-10> — 未検証で壊れた場合のリグレッション: <一文>
  - 提案: func Test<Symbol>(t *testing.T) — 骨子
  - verifier: CONFIRMED / PLAUSIBLE

## 観点ギャップ: 分岐網羅（追加検討・criticality 降順）
- 分岐未カバー: <subject file:line の分岐>
  - criticality / 提案するケース名 / verifier

## 観点ギャップ: 意味網羅（再考）
- 分岐カバー済み・意味未検証: <subject file:line> を <test file:line> がカバーするが固有 outcome 未 assert
  - 不足アサーション / verifier

## 意味的品質（再考）
- [<severity>] <file>:<line> — <弱い assert / 過剰なスタブ / 見えない skip>
  - 詳細 / verifier

## 補遺
- <レビュー過程で気付いた Test Strategy 節の補完候補 / ADR の改訂候補>
- <他スキルが所管する観点として気づいた点。所管スキル名を添える>
```

**退化入力を最初に置く。** このリポジトリで最も高く付く欠陥であり、順序がそれを述べる。

`未監査の観点:` の行は必須であり、定型文ではない。本スキルは3つのレビュー主題のうち1つしか
監査しない。それを述べないレポートは、走らせていない人には完全なレビューとして読まれる。

Severity mapping:

- **修正必須**（Lens 1 構造準拠 / Lens 2 退化入力）— `scripts/README.md` Test Strategy と ADR-0702 の
  規律に対する違反。CONFIRMED → 修正必須、PLAUSIBLE → 確認推奨。
- **補完推奨**（Lens 5）— シンボルにテストが1つも無い。CONFIRMED → 補完推奨、PLAUSIBLE → 確認推奨。
- **再考**（Lens 3 / Lens 4 Axis B）— 通るが明らかにするものが少ない。CONFIRMED → 再考、PLAUSIBLE → 補強候補。
- **追加検討**（Lens 4 Axis A）— subject を読んで見つけた未カバー分岐への先回りの提案。

## Step 5. Next-Action Suggestion

レポートの末尾に具体的な次の一手を1つ:

- 「修正必須」があれば → 該当のケースを足す。**退化入力の finding は、足すケースが1行で書ける**
  ことが多い（対象0件のディレクトリを `t.TempDir()` で渡し、`require.ErrorIs` を書く）。
- 「補完推奨」「追加検討」だけなら → 提案したケースを足す。
- verifier 通過後0件なら → そう明言する（`「verifier 通過後 0 件です」`）。

## Standalone by design

本スキルは単独で起動され、他のレビュースキルの中からは呼ばれない。`/impl-review` は変更そのものを
監査する。両者は `AGENTS.md` の Review Phase Protocol における対等な peer であり、それぞれ個別に
依頼され、互いに委譲しない。コメントは第三の peer ではない —— `/settle-comments` は実装の最後の
手順として無条件に走るので、レビューが依頼される時点では既に済んでいる。

レビュースキルが次のスキルの実行を提案すると、主題が独立に答えられるものではなくなり、片方の
質問の劣化がそこを通ったすべての流れから他方を黙って落とす。

出口側にも連鎖しない。ユーザがレポートを読み、手で直す。

テスト観点の持ち主はこれで1つになる。Lens 5 が「シンボルにテストが無い」を、Lens 4 が
branch × meaning を所管し、本スキルの外にこれらを報告するレンズは存在しない。

## Constraints (Summary)

- ❌ ファイルを編集すること（read-only）。
- ❌ `make go-test` を走らせること（本スキルはテストを**読む**。通ったかどうかは別のターゲットの仕事）。
- ❌ finder の出力を検証せずに信用すること —— verifier の段は必須。
- ❌ Test Strategy 節の観点や、意味的品質のアンチパターン目録をここにハードコードすること
  —— SSOT は `scripts/README.md` で、実行時に読む。
- ❌ 他のリポジトリの規約を持ち込むこと。**table-driven の `for` ループはここでは違反ではない。**
- ✅ verifier は懐疑を既定とする（曖昧なら CONFIRMED より PLAUSIBLE）。
- ✅ 既定の reviewer モデルは `sonnet`。orchestrator は reviewer ≠ implementer を保つために上書きしてよい。
- ✅ 既定のスコープは変更ファイル。他のスコープも選べる。
- ✅ 最終レポートは日本語、レンズごとに分け、重大度タグを付ける。**退化入力を先頭に置く。**
- ✅ criticality（1-10）は Lens 4 Axis A と Lens 5 の finding に付す本番影響のソート鍵であり、
  レンズ由来の severity を置換しない。構造準拠と退化入力には付けない。

## Checklist

- [ ] スコープを解決した（変更ファイル / base 比較 / 明示パス）。
- [ ] 対象の `*_test.go` それぞれについて subject を特定した。
- [ ] `scripts/README.md` Test Strategy 節と ADR-0702 決定13-17 を Step 1 で読んだ。
- [ ] 5レンズすべてを並行で走らせた。
- [ ] Lens 2 が、subject の各入力経路について退化形の表を突き合わせた。
- [ ] Lens 5 がシンボル表を建ててから Lens 4 の分岐解析が走り、ゼロテストのシンボルを二重報告していない。
- [ ] Lens 4 が Axis A と Axis B の両方を走らせた。
- [ ] すべての finding が `review-verifier` を通った。REFUTED を落とし、件数だけ残した。
- [ ] レポートは日本語、退化入力が先頭、`未監査の観点:` の行がある。
- [ ] 次の一手は1つの具体的な推奨。
- [ ] ファイルを1つも編集していない。
