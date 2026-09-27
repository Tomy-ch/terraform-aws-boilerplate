---
name: repo-ops
description: >-
  Operational runbook for this repository's recurring, easy-to-trip-on gotchas — the container / host split of the tool runner, the supply-chain cooldown and pin gates, the lefthook hooks and their relationship to CI, and the branch / base resolution that several commands depend on. Read-only knowledge skill: it names the exact command, it does not silently mutate state. Use it for a symptom — 「make が go: command not found で落ちる」「push が bad revision で止まる」「触っていないのに pin / cooldown / egress のゲートが落ちる」「PR が required check を待ったまま進まない」「trivy が所見なしと言うが本当に見たのか」「hook の summary の記号の意味が分からない」. Do NOT use it for a goal with no symptom behind it (`how-to`), or to explain how something works rather than how to recover (`repo-truth`).
---

# Repo Ops Runbook

症状から是正手順を引くための索引。**手順書ではなく引き表である** —— 症状を見つけ、その節を読む。
取り消しの効かない手順（保護ブランチへ触れる、履歴を書き換える、実環境へ適用する）に当たる場合は、
`AGENTS.md` *トリップワイヤ* に従って**実行せずに止まる**。

このリポジトリの症状のほとんどは、次の3つの事実から出る。

1. **検査は2つの場所で走りうる。** `RUNNER_MODE=container`（既定）は `mise.toml` から組んだイメージの
   中で、`RUNNER_MODE=host` は用意済みの道具を直接実行する。CI は host で呼ぶ。**検査の定義は1箇所に
   しか無く、差は起動の前置きだけである**（ADR-0503 決定8）。
2. **ゲートは閉じる方向で落ちる。** pin / cooldown / egress / versions のいずれも、判断できない入力を
   「取りこぼし」ではなくエラーとして扱う。**自分が触っていないのに落ちるのは、多くの場合それが
   正しく働いている。**
3. **ベースブランチは `origin` の実状態から解決する。** GitHub のデフォルトブランチは活きている
   リリース線より遅れており、それを信じた道具は1世代古い答えを返す。

## Contract

| | |
| --- | --- |
| **Owns** | 既知の運用症状 → 是正手順の索引（§1 以降）と、どの文書が正本かの所在（§0） |
| **Never** | 索引に無い手順を発明する / 設計判断 / 取り消しの効かない操作の実行 |
| **Starts when** | 症状・失敗したゲート・想定外の挙動が提示されたとき |
| **Stops when** | 症状が索引に無い（`how-to` / `repo-truth` へ）、資料が矛盾する、要求が許可範囲を超える |

## Symptom index

| Symptom | Where the fix lives |
| --- | --- |
| どの文書が正本か分からない | §0 |
| `make` が `go: command not found` / `docker: not found` で落ちる | §1 |
| `git push` が `fatal: bad revision '<branch>'` で止まる | §2 |
| 素の `git push` が保護ブランチへ向きそう / 向いた | §3 |
| PR が required check を待ったまま進まない | §4 |
| `trivy-*` が「所見なし」と言うが、本当に見たのか疑わしい | §5 |
| `egress-check` が落ちる | §5 |
| cooldown ゲートが、いま宣言した版を拒む | §6 |
| `pin-actions-check` / `pin-images-check` が drift 以外の理由で落ちる | §7 |
| `versions-check` が落ちる | §8 |
| pre-push `secret-scan` が自分の足していない秘密を報告する | §9 |
| hook の summary の記号が読めない / 何が走ったのか分からない | §10 |
| `adr-lint` が落ちる | §11 |
| ローカルでは通るのに CI で落ちる（またはその逆） | §12 |

## 0. 正本の在処

**索引を機能名で検索するだけでは足りない。** `AGENTS.md` がそう述べている —— 文書は、それが所有する
関心事の名前で置かれているのであって、探し物の名前では置かれていない。

| 答えたいこと | 読む先 | 権威に見えるが違うもの |
| --- | --- | --- |
| どの make target が何を走らせるか | `.makefiles/**/*.mk`、一覧は `make help` | `.makefiles/README.md`。**ターゲットの一覧は持たない** |
| なぜそう決まっているか | `docs/adr/`（一覧は `docs/adr/README.md` —— **一覧が存在する唯一の場所**） | supersede 済みの ADR。Status を見る |
| ユースケースの責務境界・公開契約 | `modules/<use-case>/README.md`（**未作成**） | — |
| そのユースケース固有の判断 | `modules/<use-case>/docs/adr/`（**未作成**） | root の ADR。上書きしない（ADR-0001 決定17-19） |
| 運用機構の各ツールが何を解いているか | `scripts/README.md` | ツールのコードだけ。**なぜ**はここに無い |
| 運用機構のテスト観点 | `scripts/README.md` の *Test Strategy* 節 | — |
| workflow の規則・必須検査の考え方 | `.github/workflows/README.md` | — |
| ゲートや hook が実際に何を見るか | `.github/workflows/*.yaml`、`.lefthook.yaml` | スキルの本文。スキルは README に従う、逆ではない |
| 道具とランタイムの版 | `mise.toml`（他はすべて写し —— §8） | `scripts/go.mod`、Dockerfile —— どちらも写し |
| 保護設定 | `.github/settings/branch-protection.json` と `.github/settings/README.md` | GitHub の管理画面。宣言が正本（ADR-0603 決定4-7） |
| 変更してよい範囲・保護された文書 | `AGENTS.md` | — |

### 生成された写しを権威として引かない

terraform-docs が `README.md` の中へ書く区間、`make egress-apply` / `pin-actions-apply` /
`pin-images-apply` / `versions-apply` が書き込むインラインブロック、`.terraform.lock.hcl` ——
いずれも正本ではなく写しである。正本（`.github/egress.toml`、`*-pin.toml`、`mise.toml`）が動いた
後に追随する。**tracked なので検索から外れず、正本と同じ順位で出る。**

訳文ペアはこのリポジトリには無い（ADR-0701 決定7。正本は1ファイルだけ）。

### 資料が食い違ったら

`AGENTS.md` *指示の優先順位* に従う —— `AGENTS.md` → root の Accepted ADR →
`modules/<use-case>/` の Accepted ADR → ユーザーの指示。設計意図と実装方針については
**README > Code > SKILL**。**権威を主張する2つが食い違っていたら、そこで止まる**
（*トリップワイヤ* 5）—— 気づくことが仕事であり、解決することは仕事ではない。

## 1. `make` が `go: command not found` で落ちる

道具は mise が入れており、その shims が `PATH` に無い。**version manager のサブコマンドで包まない**
（ADR-0501 決定20）—— 直すのは `PATH` であって wrapper ではない。

```sh
eval "$(mise activate zsh)"     # 対話シェル
export PATH="$HOME/.local/share/mise/shims:$PATH"   # 非対話・スクリプト
```

`docker` が見つからない場合は別の症状で、`RUNNER_MODE=container`（既定）がツールランナーを
起動できていない。ホストの道具で済む検査なら `RUNNER_MODE=host` を付ける。

```sh
make go-test RUNNER_MODE=host
```

**ホストでしか成立しない検査がある。** `base-branch` / `release` / `pin-images-*` は
`RUN_SCRIPT_HOST` で定義されており、git の認証情報やホストの docker を要する。これらに
`RUNNER_MODE=container` を強いない。

## 2. `git push` が `fatal: bad revision '<branch>'` で止まる

pre-push の lefthook が、`origin/HEAD` の指す名前を**裸の rev として** diff しようとして失敗している。
`git clone` は既定ブランチのローカル参照を作るが、**clone 後にデフォルトブランチが変わった
checkout にはそれが無い**。

```sh
git symbolic-ref --short refs/remotes/origin/HEAD   # 例: origin/release/v0.1.0
git branch release/v0.1.0 origin/release/v0.1.0     # checkout はしない
```

`git branch` は参照を作るだけで、保護ブランチの checkout には当たらない（*トリップワイヤ* 7 は
checkout を禁じている）。**`--no-verify` で迂回しない** —— それは `secret-scan` を丸ごと飛ばす。

## 3. 素の `git push` が保護ブランチへ向く

`git switch -c <new> origin/release/vX.Y.Z` は、**新しいブランチの上流を保護ブランチに設定する**。
この状態で `git push` を引数無しで打つと、保護ブランチへ向かう。

```sh
git config --get branch.$(git rev-parse --abbrev-ref HEAD).merge   # refs/heads/release/... なら該当
git push -u origin <new-branch>                                     # 明示 refspec で上流を張り替える
```

初回の push は**必ず明示 refspec で行う**。張り替えた後は素の `git push` が安全になる。

## 4. PR が required check を待ったまま進まない

必須 context を報告する job が存在しないか、報告に到達していない。

```sh
gh api "repos/<owner>/<repo>/rules/branches/$(make -s base-branch | sed 's|/|%2F|g')" \
  -q '.[] | select(.type=="required_status_checks") | .parameters.required_status_checks[].context' | sort
gh pr checks <PR> | awk -F'\t' '$2=="pass"{print $1}' | sort
```

差分が「宣言にあって報告されていない」側に出たら、その workflow が存在しないか、`on:` の
フィルタで起動していない。**`pull_request` トリガーに `branches:` / `paths:` のフィルタを置かない**
（ADR-0603 決定16-18）—— 除外された PR では run が起きず、報告の無い check を GitHub は待ち続ける。
起動条件は job の `if:` に置く。skip された job は skipped を報告し、それは成功として数えられる。

## 5. 検査が「所見なし」と言うが、見ていない

**この形がこのリポジトリで最も高く付く故障である。** 実例: `trivy-fs` が脆弱性 DB を取得できずに
FATAL で落ち、ログの下の方だけを読むと「所見なし」に見えていた。egress の許可リストを直したら、
その場で HIGH の CVE が1件出た。

疑ったら、**ゲートの終了コードと、走査件数を報告する行を読む**。件数を報告しない検査は、
ADR-0702 決定13 に反しているので、それ自体が finding である。

`egress-check` が落ちるのは、`.github/egress.toml`（正本）を変えて反映していないとき。

```sh
make egress-apply     # 25 箇所の allowed-endpoints ブロックへ反映
make egress-check
```

同じホストを複数の job が要るなら、`extra` を繰り返さず**クラスを作る** —— `.github/egress.toml`
の README がその規則を持つ。

## 6. cooldown ゲートが、いま宣言した版を拒む

`tool-cooldown` / `go-cooldown` は、公開されたばかりの版を採らない。**これは誤検知ではない。**

窓はツールではなく**バックエンド**が決める —— GitHub Release 経由は14日、パッケージレジストリ
経由は7日。言語ランタイム（`core:` バックエンド）は受容したリスクとして除外されている。

| 取れる手 | いつ |
| --- | --- |
| **窓が明けるまで待つ** | 既定。何も足さない |
| **1つ古い版を宣言する** | 上げる理由が「最新だから」のとき |
| バイパスを書く | 理由と失効期限を書けるときだけ（`.github/*-bypass.toml`） |

**理由を書けないものは抑止せず、値そのものを直す**（ADR-0501 決定13）。バイパスは期限切れ・
3ヶ月超・どれにも当たらないエントリのいずれでもゲートを落とすので、失効したバイパスが
通し続けることはない（ADR-0702 決定18）。

## 7. `pin-actions-check` / `pin-images-check` が drift 以外で落ちる

どちらも fail-closed で、drift より広い条件で落ちる。**いずれもリポジトリ側の状態の問題**で、
上流の問題ではない。

| 失敗 | 意味 | 直し方 |
| --- | --- | --- |
| lockfile に解釈できない行 | `"key" = "<40-hex>"` でも空行でもコメントでもない行 | `make pin-actions-resolve`、または当該行を消す |
| lockfile にキーの重複 | merge の衝突を機械的に解決した残骸 | 同上 |
| lockfile に参照されていないエントリ | workflow を消したときの置き去り。lockfile が実態を映さなくなる | 同上 |
| 固定対象として解釈できない `uses:` | フロー mapping / 引用キー / ブロックスカラー / alias。**書き換えられないので固定されない** | 素のブロック記法へ書き直す。**抑止しない** |
| `未登録`（images） | lockfile に無い image が `FROM` / `image:` に現れた | `make pin-images-resolve` |

`resolve` が `既存ピンを維持` と言うのは cooldown が働いた印で、失敗ではない（§6）。

**exact なタグの SHA が動いていたら、それは refresh ではなく事象である。** 止めて、`apply` しない。

## 8. `versions-check` が落ちる

`mise.toml` の `[tools]`（正本）と、それを書き写している箇所がずれている。

```sh
make versions-apply
```

**写し先の一覧をここに置かない。** 対応表を持つのは `scripts/versions/main.go` の `rules()` で、
読める形の説明は [`scripts/README.md`](../../../scripts/README.md) の `versions/` の行が持つ
（ADR-0701 決定1）。ここへ写すと、次に写し先が増えた日にこの節だけが古いまま残る ——
実際そうなった。

**出力はファイル名までしか言わない。**

```text
❌ 版の写しが宣言からずれています: Dockerfile（make versions-apply で反映）
```

1つのファイルが複数の写しを持つので、**どの写しがずれたかは出力から分からない。**
`make versions-apply` を走らせて `git diff` を読むのが早い。

形が壊れている場合（一致件数が合わない、宣言側の版が版の形をしていない）は、ラベルとパスが出る。

```text
go の照合値 (docker/tools/Dockerfile): 一致が 0 件、期待は 1 件
```

こちらは `apply` では直らない。**写しが消えたか、対応表がその形を知らないかのどちらか**であり、
`rules()` 側の更新が要る。

**ずれてもイメージのビルドは通る。** 落ちるのは、ずれた版に無い機能を使ったときだけである。
だからこの検査が要る —— 無いと「ずれている」と「揃っている」が緑で区別できない。
一致件数を**厳密**に見るのも同じ理由で、写しが増えたときも減ったときも落ちる。

## 9. pre-push `secret-scan` が自分の足していない秘密を報告する

走査対象は push 予定の commit 範囲で、作業ツリーではない。他人の commit を取り込んでいれば、
それも範囲に入る。

**混入を検出した場合の第一手は当該資格情報の失効であり、履歴からの除去ではない**
（ADR-0302 決定11-12）。push 済みであれば、履歴を書き換えても漏洩の事実は取り消せない。

誤検知なら `.gitleaksignore` へ。**理由を書けないものは足さない。**

## 10. hook の summary が読めない

`lefthook run pre-commit --force` の summary は、各コマンドを `✔️` か `🥊` で並べる。
**`🥊` は失敗である** —— バナーにも同じ絵文字が出るので、そちらと取り違えない。

```sh
lefthook run pre-commit --force; echo "EXIT=$?"
```

**パイプで受けると終了コードが消える。** `| tail` を付けると `tail` の成否が返るので、
判定は summary の記号か、パイプ無しの終了コードで読む。

`--force` が要るのは、commit 済みで作業ツリーが綺麗なときに素の `lefthook run pre-commit` が
「対象ファイルが無い」で全コマンドを skip するため。

hook は `--no-verify` で迂回される前提で設計されている（ADR-0501 決定26）。**権威は CI である。**
hook でのみ成立する検査を作らない。

## 11. `adr-lint` が落ちる

ADR-0001 が自らの検証方法として挙げた項目を、そのまま実行している。よくある原因は**採番の詰め直しを
途中で止めたこと** —— 決定7 は、ファイル名・見出し・索引・相互参照・ADR 以外からの参照を
**同一の変更で**更新することを要求している。片方だけ直すと、2つのファイルが同じ番号を名乗る。

`\b` を使って `ADR-\d{4}` を探すと**日本語の直後で一致しない**（`て` も `A` も `\w` なので境界が
立たない）。`ADR-(\d{4})(?!\d)` を使う。

## 12. ローカルでは通るのに CI で落ちる（またはその逆）

**検査の定義は1箇所にしか無い**（ADR-0503 決定8）。食い違う原因は定義ではなく、次のどれかである。

| 原因 | 見分け方 |
| --- | --- |
| 実行環境（`RUNNER_MODE`） | ローカルは container 既定、CI は host。道具の版はどちらも `mise.toml` が決めるので、**版のずれは §8 の症状** |
| hook の glob | `.lefthook.yaml` の `glob` に当たらない変更は、ローカルでは発火しない。CI は当たる |
| base の解決 | `make base-branch` は `origin` の実状態を読む。ローカルの `origin/*` が古ければ `git fetch` する |
| 触っていない範囲の指摘 | cooldown / pin / egress は継承した状態でも落ちる（§6・§7）。**変更が持ち込んでいない指摘で PR を人質にしない**（ADR-0603 決定11）ので、required に入っていない検査で落ちているなら、それは merge を塞いでいない |

**CI の結果は、失敗したステップのログから読む。** 実行全体のログを引くと、失敗と無関係な出力が
大量に混じる。

## Constraints

- ❌ 索引に無い手順を発明する。**無いことは finding である**（`how-to` / `repo-truth` へ）
- ❌ 取り消しの効かない操作を実行する —— force push、rebase、amend してからの push、保護ブランチの
  checkout、実環境への apply（`AGENTS.md` *トリップワイヤ* 7・8）
- ❌ ゲートの判定を、行や件数を落とすフィルタ越しに報告する
- ❌ 理由を書けない抑止を足す（ADR-0501 決定13）
- ✅ 破壊的な手順の前に、何が失われるかを言って待つ
- ✅ 検査が「所見なし」と言ったとき、**見たことの証拠**（件数・終了コード）を確かめる
- ✅ 日本語で答える
