---
name: sync-readme
description: >-
  Update one README so its file / directory listing matches what is actually on disk beneath it — missing entries, renamed paths, removed files, outdated one-line descriptions — while respecting nested README boundaries (a subdirectory with its own README gets a one-line digest and a link, never an inlined expansion) and never touching a terraform-docs generated region. Use it when files were added, renamed or removed under a documented directory and its README no longer describes what is there. Operates on exactly one README per invocation. Do NOT use it to write a README's substantive content — 責務境界 / supported / unsupported / 公開契約 belong to `modules/<use-case>/README.md` itself (ADR-0102 決定2, ADR-0701 決定2) — to judge prose quality, or to reconcile a README's claims with code behavior (`/impl-review` の declaration-drift レンズ).
---

# Sync README

README を、その直下に実際に在るファイル・ディレクトリの構成と突き合わせて直す。入れ子の README の
境界を尊重する —— 子ディレクトリが自分の README を持つなら、そこは1行の要約と参照リンクにし、
中身を展開しない。

**このスキルが直すのは「何が在るか」であって「それが何を保証するか」ではない。** 責務境界、
supported / unsupported、公開契約は `modules/<use-case>/README.md` そのものが所有する
（[ADR-0102](../../../docs/adr/0102-use-case-centric-scope.md) 決定2、
[ADR-0701](../../../docs/adr/0701-documentation-ownership-and-language.md) 決定2）。
一覧が古いことと、保証が間違っていることは別の欠陥であり、後者をこのスキルが書き足さない。

## When to Use

- README がその配下とずれた（ファイルの追加・削除・改名、説明が古い）。
- 入れ子の README に触れずに、1つのディレクトリの README を更新したい。
- 「書いてあるのに無い」「在るのに書かれていない」を、何を直すか決める前に洗い出したい。

**ツリー全体の README を再帰的に書き直すために使わない。** 1回の起動が扱うのは1つの README の
スコープだけである。ツリーを洗うなら、ディレクトリごとに起動する。

Do NOT use this skill for:

- README の**内容**を書くこと（上記のとおり、所有は当の README とその ADR にある）。
- 宣言と実体のずれ —— README が述べる保証が、コードの振る舞いと食い違っていること。
  `/impl-review` の `declaration-drift` レンズが持つ。
- **terraform-docs の生成区間**（`AGENTS.md` トリップワイヤ4、ADR-0701 決定4）。生成区間を
  編集することは、判断を待たずに作業を止める条件である。

## First Step: Confirm Input

**起動直後に `AskUserQuestion` を呼ぶ。**

1. **対象の README のパス** —— 引数や直前のメッセージにパスがあれば候補として挙げる。
2. **スコープの確認** —— 境界（その README が居るディレクトリ）と深さの上限（既定: 直下の子だけ。
   入れ子のディレクトリは、それ自身の README があればその要約で表す）。

**これらが確定するまで、ファイルツリーを読まず、何も書かない。**

**訳文ペアはこのリポジトリに無い**（ADR-0701 決定7）。正本は1ファイルなので、同期する相手も無い。

## How the Sync Works

### Scope

- README の居るディレクトリが**スコープの根**である。
- 根の直下のエントリを列挙する。
- エントリごとに:
  - **ファイル**: 1行の説明を添えて一覧へ入れる。
  - **README を持たないディレクトリ**: 一覧へ入れ、浅ければ目立つ子を要約してよい。
  - **README を持つディレクトリ**: 1行の要約とその README への相対リンクだけを入れる。
    **中へ再帰しない。**

### Detecting drift

README が書いているエントリと、実際のエントリを突き合わせる。

- **書いてあるのにディスクに無い** → 消す（他所からリンクされている等、荷重を持つなら消す前に確認する）。
- **在るのに書かれていない** → 足す。
- **改名らしい**（説明が同じでパスが似ている）→ パスを直す。
- **説明が古い**（役割が変わった）→ 中身から新しい役割を読み、直す。

### Things NOT to touch

- **terraform-docs の生成区間。** 生成器が定めたマーカの内側は、人が書く区間ではない。
  `variable` / `output` の一覧はそこが持つので、README の散文へ写さない（ADR-0701 決定2）。
- 隠しファイル・隠しディレクトリ（`.git`、`.DS_Store` 等）—— README が明示的に書いている場合を除く。
- `.gitignore` に掛かるもの（`tmp/`、ビルド成果物）。
- 生成物 —— `.terraform.lock.hcl`、`docker/images-pin.toml`、`.github/actions-pin.toml`、
  `make egress-apply` / `make versions-apply` が書くインラインブロック。
- 自分の README を持つ子ディレクトリの中身。

## Repo Conventions

- 散文は日本語である（ADR-0701 決定9）。英語のまま置くのは決定8 が挙げるもの —— HCL の識別子、
  ファイル名・ディレクトリ名、技術用語。
- 既存の節の順序と体裁（表・箇条書き・散文）を保つ。ユーザーが明示的に再構成を求めた場合を除く。
- **まだ正しい散文をそのまま残す。** 文体を理由に書き直さない。差分を最小にする。
- **変更履歴と経緯を書かない**（ADR-0701 決定3）。「以前は」「〜に変更した」は git が持つ。

## 変更してよい範囲

`AGENTS.md` *変更してよい範囲* に従い、このスキルが書くのは**確認した1つの README** だけである。
実行中も保護され続けるもの: `AGENTS.md` / `CLAUDE.md`、Accepted ADR の本文、`LICENSE`、生成区間。
スコープの根の下の他のファイルは**読むが書かない**。

## Execution Steps

### 1. Read the target README

全文読み、次を把握する。

- 既存の節の構造。
- いま書かれているエントリ（ファイル、ディレクトリ、リンク）。
- 独自の慣習（「ディレクトリ構成」の表、下位の一覧）。
- **生成区間のマーカの位置** —— どこから先が人の書く区間でないか。

### 2. Enumerate the actual file tree

スコープの根の直下を列挙する。それぞれについて:

- 名前、種別（ファイル / ディレクトリ）、ディレクトリなら自分の README を持つか。
- ファイルは、役割が分かるだけ読む（package コメント、先頭の数行、`variable` の `description`）。
- 自分の README を持つディレクトリは、その README の最初の段落だけを読んで要約を取る。
- README を持たないディレクトリは、目立つ子を挙げてよい（深く再帰しない）。

### 3. Compute the diff

3つの一覧を作る。

- **足すもの**: ディスクに在り、README に無いか古い。
- **消すもの**: README に在り、ディスクに無い。
- **直すもの**: 両方に在るが、説明かパスが違う。

**消すものが他所から参照されていたら、消す前にユーザーへ差し出す。**

### 4. Apply the update

README を実態に合わせて書き直す。

- 新しいエントリを、既存の書式に合わせて適切な節へ入れる。
- 古いエントリを消す。
- 説明とパスを直す。
- 自分の README を持つ子ディレクトリは、1行の要約 + 相対リンク
  （`- [sub/](sub/README.md) —— 短い要約`）にする。
- 無関係な節（前書き、バッジ、ライセンス）は**逐語で保つ**。
- **生成区間へは触れない。**

### 5. Verify

- 更新後の README のエントリが、すべて実在するファイル・ディレクトリを指すことを確かめる。
- 無視対象を除く実在のエントリが、1つも漏れていないことを確かめる。
- 入れ子の README を展開していないことを確かめる。
- 生成区間が変わっていないことを確かめる（`git diff` で見る）。

### 6. Verify with Markdown Lint

```sh
make md-fix
make md-lint
```

`make md-fix` は `markdownlint-cli2 --fix` をリポジトリ全体へ掛ける。`make md-lint` がその結果を
`.markdownlint-cli2.yaml` に照らして検査する。残った違反は手で直す（見出しの階層、重複する見出し、
裸の URL）。**`make md-lint` が綺麗に終わるまで、完了と報告しない。**

`make md-fix` はリポジトリ全体に効くので、対象と無関係な Markdown を書き換えることがある。
完了を報告するとき、そのファイルを列挙する。

### 7. Report

足した / 消した / 直したエントリと、書いたファイルを報告する。

## Checklist

- [ ] 対象の README のパスを `AskUserQuestion` で確認した。
- [ ] スコープの根と深さの上限を確認した。
- [ ] ずれを計算した（足す / 消す / 直すの3つの一覧）。
- [ ] README を、構造を保ったまま実態に合わせた。
- [ ] 自分の README を持つ子ディレクトリが、1行の要約 + リンクになっている（展開していない）。
- [ ] **terraform-docs の生成区間へ触れていない**（`git diff` で確かめた）。
- [ ] 消したエントリが他所から参照されていないことを確かめた。
- [ ] `make md-lint` が綺麗に終わった。
- [ ] 対象の README 以外のファイルを書き換えていない。

## Notes

- 入れ子の README を再帰的に書き直さない。1回の起動は1つの README のスコープを扱う。
- 書かれているエントリを無条件に消さない。
- まだ正しい節を、体裁を理由に再構成しない。
- 従うべき慣習がまだ無い README（新設で構造が無い）なら、表 / 箇条書き / 散文のどれにするかを訊く。
- README が意図的にディレクトリの外の項目を書いている場合（リポジトリ直下の README がリポジトリ全体を
  並べている等）、それをずれとして扱う前にスコープを確認する。
