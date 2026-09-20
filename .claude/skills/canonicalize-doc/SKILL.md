---
name: canonicalize-doc
description: >-
  Create or sync an English-canonical / Japanese-translation Markdown pair for a README, SKILL, or ADR, preserving heading structure, code blocks, and link targets 1:1. **This skill cannot run today**: ADR-0701 決定7 forbids translation pairs (`*.ja.md`) and 決定6 makes Japanese canonical, so the skill exists for the v1.0.0 transition that flips the canonical side to English — and it refuses to produce a pair until 決定7 has been superseded by a human (a 停止点 owned by ADR-0001 決定2-3). Use it when that supersede has landed and a document needs its counterpart produced or re-synced. Do NOT use it to write a document's content (`sync-readme` / the owning document per ADR-0701 決定2), to edit Accepted ADR bodies, or to translate ad hoc — a one-off translation that lands in the tree is exactly what 決定7 forbids.
---

# Canonicalize Doc

正本と訳文の対を作る、あるいは同期させる。**ただしこのリポジトリでは、今日それを行ってはならない。**

## 走る前に止まる条件（これが最初の手順である）

[ADR-0701](../../../docs/adr/0701-documentation-ownership-and-language.md) は2つを述べている。

- **決定6** —— 文書の正本を日本語とする。
- **決定7** —— 同一内容の翻訳ペアを作らない。`*.ja.md` のような訳文ファイルを置かない。

このスキルの成果物はその決定7 が禁じているものそのものである。したがって:

1. **`docs/adr/0701-documentation-ownership-and-language.md` の Status を開いて確かめる。**
   `Accepted` のままで、決定7 を supersede した ADR が `docs/adr/README.md` の一覧に無いなら、
   **ここで止まる。** ユーザーへそう報せ、ファイルを1つも書かない。
2. ADR の supersede は**停止点**である（`AGENTS.md` *停止点*、ADR-0001 決定2-3）。
   このスキルがその判断を代行しない。**スキルが自分で規則の例外を宣言することは、抜け道である。**
3. supersede が済んでいるなら、その新しい ADR が定める**正本の側と訳文の命名**を読み、
   以下の既定より優先させる。このスキルの記述は、supersede が landing した時点の形を先取りした
   ものにすぎない。

v1.0.0 でこの切り替えを行う予定であること自体は、`AGENTS.md` *v1.0.0 までの暫定* が述べている
範囲の外にある —— **暫定節は ADR の運用を解除しない。**

## When to Use

決定7 が supersede された後に:

- 正本しか無い文書の対の片側を作る。
- 両方在るが、ずれた対を同期させる。

Supported document types（supersede 後の ADR が別を定めればそちらが勝つ）:

- Claude Code のスキル: `.claude/skills/<name>/` の `SKILL.md` とその対。
- README: 同じディレクトリに並べる `README.md` とその対。
- `docs/**` の文書: `docs/<path>/<name>.md` とその対。**Accepted ADR の本文は対象外**
  —— immutable であり（ADR-0001 決定2）、`AGENTS.md` の *保護された文書* が守る。

Do NOT use this skill for:

- 文書の**内容**を書くこと。何をどこへ書くかは ADR-0701 決定2 の所有表が持ち、README の
  同期は `sync-readme` が持つ。ここが行うのは言語の写しだけである。
- Accepted ADR の本文。
- terraform-docs の生成区間、`.terraform.lock.hcl`、`*-pin.toml`、workflow のインラインブロック
  （ADR-0701 決定4、`AGENTS.md` トリップワイヤ4）。

## First Step: Confirm Input

止まる条件を通過したうえで、**起動直後に `AskUserQuestion` を呼ぶ**。

1. **Source file path** —— ユーザーが指している文書（正本か訳文か）。
2. **Direction** —— 何を作るか:
   - `canonical-from-translation`: 訳文から正本を起こす。
   - `translation-from-canonical`: 正本から訳文を起こす。
   - `sync-both`: 両方在る。差分を突き合わせ、更新されるべき側を書き直す。

手順:

1. スキルの引数や直前のメッセージにパスがあれば候補として挙げる。
2. 周囲のディレクトリを見て、対になるファイルと、実際に使われている命名の慣習を検出する。
3. `AskUserQuestion` を呼ぶ。
   - Question 1: 「対象のファイルパスを確認してください」（検出した候補を添える）
   - Question 2: 「どちらの向きで作りますか？」（既に在るファイルから推奨を決めて添える）
   - `sync-both` なら: 「この同期で、どちらを正としますか？」

**パスと向きが確定するまで、翻訳のためにファイルを読み書きしない。**

## 変更してよい範囲

`AGENTS.md` *変更してよい範囲* に従う。このスキルの実行中に触れてよいのは:

- 確認した source ファイル。
- その対となるファイル（新規作成または書き直し）。
- それ以外は触れない。

実行中も保護され続けるもの: `AGENTS.md` / `CLAUDE.md`、Accepted ADR の本文、`LICENSE`、生成区間。

## Execution Steps

### 1. Read the source

確認した source を全文読む。`sync-both` なら両方読む。

### 2. Determine the output path

- `canonical-from-translation`: `foo.<訳文の接尾辞>.md` → `foo.md`（同じディレクトリ）。
- `translation-from-canonical`: `foo.md` → `foo.<訳文の接尾辞>.md`（同じディレクトリ）。
- `sync-both`: 正としなかった側を書き直す。

接尾辞と、どちらが正本かは、決定7 を supersede した ADR が持つ。**ここへ写さない。**

### 3. Translate (or sync)

- 見出しの構造、リストの入れ子、コードブロック、リンクの宛先を**そのまま**保つ。
- 訳すのは散文と、元から訳されていたコードブロック内のコメントだけ。
- **訳さないもの**: 識別子、ファイルパス、コマンド、コードそのもの。ADR-0701 決定8 が英語のまま
  置くと定めたもの（HCL の識別子、ファイル名、commit message の prefix、技術用語）は、
  どちら側でも英語のままである。
- スキルのファイルなら、frontmatter の規約は supersede 後の ADR が定めるものに従う。

### 4. Write the output

- 生成したファイルを出力先へ書く。
- `sync-both` なら、正としなかった側だけへ書く。

### 5. Verify

- 見出しの数と文言を突き合わせる。1:1 で対応すること。
- コードブロックがバイト単位で一致することを確かめる（散文を訳した箇所を除く）。
- 綺麗に対応付けられなかった節を報告し、どう解決するかをユーザーへ訊く。

### 6. Verify with Markdown Lint

書き終えたら:

```sh
make md-fix
make md-lint
```

`make md-fix` は `markdownlint-cli2 --fix` をリポジトリ全体へ掛け、自動で直せるもの（見出し・リスト・
コードブロック周りの空行、行末の空白、ファイル末尾の改行）を直す。`make md-lint` がその結果を
`.markdownlint-cli2.yaml` に照らして検査する。

残った違反は手で直す（見出しの階層、重複する見出し、裸の URL —— 自動修正が解けないもの）。
**`make md-lint` が綺麗に終わるまで、完了と報告しない。**

`make md-fix` はリポジトリ全体に効くので、対と無関係な Markdown を書き換えることがある。
完了を報告するとき、そのファイルを列挙する。

## Checklist

- [ ] **ADR-0701 決定7 の supersede を確かめた。** 済んでいなければ、ファイルを1つも書かずに止めた。
- [ ] source のパスを `AskUserQuestion` で確認した。
- [ ] 向きを確認した。`sync-both` なら、どちらを正とするかも確認した。
- [ ] 出力先が、supersede 後の ADR が定める命名に従っている。
- [ ] 見出しの構造とコードブロックが 1:1 である。
- [ ] `make md-lint` が綺麗に終わった。
- [ ] 対の外のファイルを書き換えていない。

## Notes

- 確認した対の外のファイルを書き換えない。
- 識別子・パス・コマンド・技術用語を訳さない。
- **一度きりの翻訳をツリーへ落とさない。** 決定7 が禁じているのはまさにそれであり、
  「対にしないから訳文ではない」という読み替えは通らない。
