---
name: manage-skill
description: >-
  Create, update, evaluate, and optimize skills under this repository's `.claude/skills/`, wrapping Anthropic's official `skill-creator` methodology and layering this repo's conventions on top. This is the single entry point for ANY change to an existing skill under `.claude/skills/`; ALWAYS use it before hand-editing a `SKILL.md`. Use this WHENEVER the user wants to update / modify / change / edit / fix / improve / refactor / rename / extend / adjust / tune an existing skill — its steps, `description`, frontmatter, or behavior — or to build a new skill, author/scaffold a `/<name>` command, turn a repeated workflow into a skill, tune a skill's triggering description, compress descriptions that have grown into restatements of the body, normalize ones that have gone stale against it, or run evals/benchmarks on a skill — even if they don't say the word "skill-creator". Japanese triggers also apply, e.g. 「スキルを更新したい」「スキルを修正して」「このスキルの手順 / description / 挙動を変えて」「description が長い / 古い」. Do NOT use it for editing canonical docs (`docs/**`, `README.md`), other AI-tool configs (`.cursor/`, `.gemini/`, Copilot), or generated content.
argument-hint: '[skill-name] [--update|--new|--optimize]'
allowed-tools: Bash(ls:*), Bash(make md-lint:*), Bash(make help:*), Read, Write, Edit, Glob, Grep, AskUserQuestion, Skill, Agent
---

# Manage Skill

You have been invoked via `/manage-skill`. Argument string: `$ARGUMENTS`.

This skill authors and maintains skills under `.claude/skills/` for **this** repository. It is a
thin wrapper: the *methodology* (draft → test → review → improve → optionally optimize the
description) comes from Anthropic's official `skill-creator` skill, and this file layers the
repository's own conventions on top so the produced skill fits in alongside `commit`, `repo-ops`,
`impl-review`, and the rest.

## When to Use

Use this skill when the user wants to:

- Create a brand-new skill / `/<name>` command, or turn a repeated workflow from the current
  conversation into one.
- Update, modify, improve, refactor, rename, extend, or fix an existing skill under
  `.claude/skills/` — this is the entry point for any such change, used before hand-editing a
  `SKILL.md`.
- Optimize a skill's `description` for better triggering, compress one that has grown into a
  restatement of the body, or normalize one that has gone stale against it.
- Run evals/benchmarks on a skill.

Do NOT use it for:

- 正典となる文書の編集（`docs/**`、`README.md`、ADR）—— 所有は
  [ADR-0701](../../../docs/adr/0701-documentation-ownership-and-language.md) 決定2 が持ち、
  Accepted ADR の本文は `AGENTS.md` の *保護された文書* が守る。
- 他の AI ツールの設定（.cursor/、.gemini/、.github/copilot-instructions.md）—— `AGENTS.md`
  *エージェント設定ファイルの保護* の範囲外。

## Scope note (AGENTS.md)

`AGENTS.md` は `.claude/**` を「指示なしに触らない」側へ置いている。**このスキルの起動がその明示の
指示である** —— ただし `.claude/skills/**` に限り、この実行の間に限る。`AGENTS.md` が保護する経路は
ここでも保護される: `AGENTS.md` 本体、Accepted ADR の本文、`LICENSE`、生成物（terraform-docs の
生成区間、`.terraform.lock.hcl`、`*-pin.toml`、workflow のインラインブロック）へは触れない。

## Step 0 — Load the official methodology (do this first)

公式の `skill-creator` が *how* の正本である。このリポジトリは**プラグインを宣言していない**ので、
まず実在するかをパスで確かめる。

```bash
ls ~/.claude/plugins/marketplaces/*/plugins/skill-creator/skills/skill-creator/SKILL.md
```

見つかればその `SKILL.md` を全文読む。**Creating a skill** / **Running and evaluating test cases** /
**Improving the skill** / **Description Optimization** / blind comparison / packaging の記述はすべて
そのまま当たる。同梱物も、そのディレクトリから**そのまま**使う（再実装しない）:

- `scripts/` —— `aggregate_benchmark`、`run_loop`（description の最適化）、`package_skill` ほか。
- `eval-viewer/generate_review.py` —— レビューのビューア。HTML を手書きしない。
- `agents/`（`grader.md` / `comparator.md` / `analyzer.md`）と `references/schemas.md`。

**見つからなければ、入れない。** `AGENTS.md` *道具の導入* は「自分の判断で install しない」と述べて
おり、プラグインの導入はエージェント連携の install に当たる（プロジェクト規模の指示ファイルを書き込む）。
不在はユーザーへ**報告すべき所見**である。そのうえで、要約した方法論（draft → test → review →
improve）で続けてよいかを訊く。**「公式の手順に従った」と書かない** —— 読めなかったものに従うことは
できない。

## The repository overlay (this is what the wrapper adds)

公式の手順に従いつつ、食い違うところではこちらを優先する。従わないスキルはこのリポジトリに嵌まらない。

### 1. Placement and structure

- 新しいスキルは `.claude/skills/<name>/SKILL.md`。`<name>` は kebab-case で `name:` frontmatter と
  一致させる。**先に隣を見る** —— `commit` / `repo-ops` / `how-to` / `tool-map` / `impl-review` の
  形を確かめ、近い複製を足すより既存を編集・拡張する方を選ぶ。
- 同梱物（`scripts/` / `references/` / `assets/`）は公式の構成に従う。`SKILL.md` は 500 行程度に
  収め、詳細は `references/` へ出して明示的に指し示す。

### 2. Frontmatter conventions (match the existing skills)

- `name`: kebab-case、ディレクトリ名と同一。
- `description`: **1つの密な段落**、英語。公式の "pushy" な誘導方針に従う（何をするか、具体的に
  いつ使うか、そして**いつ使わないか**と、その主題を持つ兄弟スキルの名前）。密度と調子は `commit` /
  `repo-ops` の description に合わせる。
- **`description` の採用基準は「網羅」ではなく「起動の判断」である。** すべてのスキルの
  `description` は、呼ばれるかどうかに関わらず毎セッションの冒頭へ読み込まれる。だから1行が
  席を得るのは、**このスキルを呼ぶかどうかの判断を助けるとき**だけである。入れてよいのは5つ:
  何をするか、いつ使うか（日本語のトリガー語を含む）、いつ使わないかと代わりに誰が持つか、
  呼ぶ側が既に満たしているべき前提、ユーザーが名指しするであろうフラグ。残りは本体へ置く ——
  本体はスキルが実際に走るまで何の費用も生まない。内部の機構（subagent 名、既定モデル、
  fan-out の形、段の構成）、強制する規則、検証に使うコマンド、出力の形式、その理由づけ。
- **`description` から行を消す前に、本体が既にそれを持っているかを確かめる。** 両者がずれるのは
  まさに、片方にしか書かれていない事実があるときである。移動のつもりの削除は、指示の黙った消失になる。
- **腐った `description` は、そのスキルへ触れたついでに正す** —— スキルが走っている間に読まれない
  唯一の部分であり、嘘をついても何も落ちず、どのゲートも捕まえない。名指ししている兄弟スキル、
  レンズ、モード、フラグ、パス —— 本体と突き合わせ、同じ編集の中で直す。別件にしない。
- 上の2つは**掃き出しとしても効く** —— `.claude/skills/*/SKILL.md` 全体に対して同じ基準で走らせてよい。
- 任意: `argument-hint` と `allowed-tools`（引数を取る、あるいは固定の道具集合を使う slash command の
  とき。`commit` が手本）。要らなければ置かない。

### 3. Language rules

- `SKILL.md` の本体は**英語と日本語が混在してよい**。既存のスキルがそうなっている —— 輸入した骨格が
  英語で、このリポジトリの判断を述べる部分が日本語である。**書き換えるためだけに全訳しない。**
- ただしスキルの*実行時の振る舞い*は [ADR-0701](../../../docs/adr/0701-documentation-ownership-and-language.md)
  決定6-11 に従う: スキルが生成する利用者向けの出力（応答、コードコメント、`description`、
  commit message、Pull Request、文書）は**日本語**である。スキルを書くとき、その要求を指示の中へ焼く。
- **訳文ペア（`SKILL.ja.md`）を作らない**（ADR-0701 決定7）。正本は1ファイルである。

### 4. 存在しないものを名指ししない

このリポジトリで最も起きやすいスキルの壊れ方である。**スキルは検査を持たない散文であり、名指しした
先が消えても何も赤くならない。**

- **`make` の target は `make help` に在るものだけを書く。** 無い target を書くことは、嘘をつくことと
  区別できない（`how-to` が同じ規律を持つ）。Terraform 側の検査（TFLint / Conftest / `terraform test`）と
  plan / apply の経路は**まだ配線されていない**（`AGENTS.md` *現在の配線状態*）。
- **`.claude/skills/` に無いスキルを `/<name>` で指さない。** 輸入や新設でスキルが増えるなら、
  参照を足すのは**その回**である。
- **未配線の道具を、存在するものとして書かない**（ADR-0701 決定13）。
- **ADR の番号と決定番号は、引く前に開く。** 番号は identity ではなく順序であり、詰められることが
  ある（ADR-0001 決定5-9）。

新設・更新のたびに、名指しした先が実在することを**機械で**確かめる。読み返すだけでは見つからない。

- `make skill-lint` —— `make` target / `/<skill>` / パス。`.claude/skills/**` の Markdown が
  inline code span で名指ししたものを見る。フェンスの中は例示なので見ない。
- `make adr-lint` —— ADR の番号と slug。`docs/adr/` の**外**から指すパス形式のリンクも見る。

どちらも exit code で確かめる。出力を眺めて「通ったようだ」と書かない。

**この契約に例外は無い。抑止の仕組みを置いていない。** 存在しないものを述べたい文は珍しくない
——「この target は在って当然に見えるが存在しない」、他の AI ツールの設定を対象外として挙げる
一覧、特定のスキルではなく slash command 一般を指す語。**そういう対象はコードスパンに入れずに書く。**

```text
悪い: `make terraform-test` は存在しない   ← 検査は名指しと読む。文意は読めない
良い: make terraform-test は存在しない
```

口を作らないのは、抑止が溜まるからである。この仕組みを外すまで、リポジトリには抑止マーカが
18箇所あり、**そのうち4箇所は抑止する対象がとうに消えていた** —— 対象が消えてもマーカは残り、
誰も気づかない。例外の無い規則のほうが守られる。

コマンドの例示をコマンドの形で見せたいなら、フェンス（```）で囲む。フェンスの中は検査しない。
**囲まずにバッククォートだけで書くことは、`how-to` が警告している失敗そのもの**である ——
それらしいコマンドは、記録されたコマンドと見分けがつかない。

### 5. Eval artifacts stay out of version control

公式の手順は `<skill-name>-workspace/` に反復・評価のディレクトリ、ベンチマーク、ビューアの出力を
書く。スキルの隣に置くと、追跡下の `.claude/skills/**` の中へ落ちる。**場所を上書きする**:
gitignore された `tmp/` の下（例 `tmp/skills/manage-skill/<skill-name>-workspace/`）へ置く。
評価の実行結果、ベンチマーク、フィードバックの JSON、ビューアの HTML を commit しない。

### 6. Reuse repo patterns when they fit the skill's shape

- 読み取り専用の分析をレンズごとに並行で走らせるなら、**統合役 + subagent** の形を写す
  （`impl-review` / `test-review` が `adversarial-reviewer` を並行で起動し、`review-verifier` で
  検証し、統合役が単一スレッドで報告を組む）。新しい agent type を作る前に、既存
  （`adversarial-reviewer` / `review-verifier` / `comment-reviewer` / `arch-verifier` /
  `impl-verifier`）で足りないかを見る。
- 決定的な多段の流れをユーザーの選択で分岐させるなら、自由文ではなく `AskUserQuestion` を使う
  （`commit` / `submit-pr` が手本）。
- **正本は実行時に読む** —— `scripts/README.md` の Test Strategy 節、`docs/adr/README.md`、
  `modules/<use-case>/README.md`。規則をスキルへ写さない。写した瞬間に、正本が動いてもこちらは動かなくなる。

## Creating a new skill

公式の **Creating a skill** の流れ（Capture Intent → Interview → Write SKILL.md → Test Cases）を
走らせ、そのうえで overlay を当てる —— 置き場所、frontmatter と description の密度、`tmp/` 配下の
評価ワークスペース、そして §4 の走査。

## Updating an existing skill

- どのスキルかを確定する（`.claude/skills/<name>/`）。`name` とディレクトリは変えない。
- インストールされたプラグインのスキルと違い、リポジトリのスキルはその場で書ける ——
  一時ディレクトリへ写す手順は要らない。
- 評価の基準線として、編集前のスキルを公式の手順どおり `tmp/` へスナップショットする。
- 編集後、§4 の走査を必ず走らせる。**description を本体と突き合わせて正す**（§2）。

## Run it once on a real problem before calling it done

スキルは指示書であり、その著者は唯一それを監査できない読者である —— 指示書が落としている文脈を
著者は頭の中に持っているので、読みながら穴を埋めてしまい、穴そのものは見えない。草稿を読み返しても、
レビューへ出しても、この手順が捕まえる失敗は表に出てこない。

**答えを自分がまだ知らない実際の問題**を1つ取り、手順を飛ばさずにスキルを通す。答えを知らないことが
効く —— 知っている問題では、記憶から実行を完成させてしまい、欠けている欄は名乗り出ない。

そして、**契約に欄が無いのに何かを書いた場所**と、**契約が要求しているのに書けなかった場所**を
すべて記録する。それが欠陥である。読んでいる間には1つも見えない:

- 必須の判定はあるのに、その根拠を置く場所が無く、実行が欄を発明した
- ある状態（`UNDEFINED`）に対して、隣の欄が何を言うべきかの規則が無い
- 分析から軸を1つ落とす実行モードがあるのに、その軸を落としたと書く場所が無い

**同じ形の穴が複数のスキルに出たら、個別に当てずに欄を揃える。** 繰り返す穴は、欠けている規約である。
3通りに直せば、1つの家族として読めない3つの契約ができる。

これはスキルの検証であって、その出力の品質の検証ではない。後者は公式の評価ループが測るもので、
2つは別の問いに答えている。

## Evaluating / optimizing

公式の test-run / benchmark / viewer / **Description Optimization** の流れをそのまま使う。
差し替えるのは2つだけ: ワークスペースは `tmp/` の下（§5）、description の最適化器へ渡すモデル ID は
このセッションを動かしているもの（環境 / system prompt が述べる）—— 起動の判定を実運用に合わせるため。

## Definition of Done

- 公式の `skill-creator` を読んだ、または**不在を報告し**、要約した方法論で進めることの承認を得た。
- `.claude/skills/<name>/SKILL.md` が在る。kebab の `name` = ディレクトリ名、密な英語の "pushy" な
  `description`。
- `description` が §2 の採用基準を通る —— 起動の判断を助けない行が無く、本体が支えない主張が無く、
  本体が持っていない事実を消していない。
- **§4 の走査を走らせた** —— `make skill-lint` と `make adr-lint` が、どちらも exit code 0 で返った。
- 評価の成果物を commit していない（ワークスペースは gitignore された `tmp/` の下）。
- 保護された経路へ触れていない。変更は `.claude/skills/**` に限られている。
- 答えを知らない実際の問題で1度通し、契約に欄が無かった場所・埋められなかった欄をすべて直した。
- ユーザーが出力を確認し、納得している（公式のループ）。
- `make md-lint` が通る。
