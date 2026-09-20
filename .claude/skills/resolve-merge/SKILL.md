---
name: resolve-merge
description: >-
  Land a merge correctly by classifying every conflicted path into a resolution class and applying the mechanical resolution each class already has — regenerate generated blocks from their declaration, re-run the pin resolvers instead of picking lockfile lines, union append-only indexes, and hand back anything semantic with its markers intact. It also regenerates the derived artifacts that did NOT conflict, because a derived file goes stale from the other side's changes without ever producing a marker. Use it right after running a merge, whether or not it conflicted. Do NOT use it to rebase, squash or force-push (`AGENTS.md` forbids all three), to resolve implementation conflicts for you, or to decide which side of a declaration is right.
---

# Resolve Merge

merge を正しく着地させる。衝突したパスをクラスへ分け、クラスごとに既に決まっている機械的な解決を
当てる。**機械的に解けないものが1つでも残ったら、マーカーを残したまま打ち切る。**

## Contract

| | |
| --- | --- |
| **Owns** | 衝突パスのクラス分けと、クラスごとの機械的解決（再生成 / resolver 再実行 / 和集合 / 派生の再伝播） |
| **Never** | 実装の意味的衝突を解く / 生成物の片側を選ぶ / rebase・squash・force-push / 宣言のどちらが正しいかを決める |
| **Starts when** | merge を実行した直後（**衝突の有無を問わず**） |
| **Stops when** | 機械的に解けないものが1つでも残ったとき —— その場でマーカーを残して打ち切り、commit しない |

## Why this exists

**このリポジトリで衝突マーカーが落ちる先の多くは、誰も手で編集すべきでないファイルである。**
`.github/workflows/*.yaml` の `allowed-endpoints` ブロック、ピンの lockfile、`scripts/go.mod` の
`go` 行、`docker/tools/Dockerfile` の `FROM` タグ —— どれにも生成器か resolver が在り、**どれにも
「正しい側」が無い**。

片側を選ぶと、最悪の結果になる —— マーカーの無いファイル、レビューを通るファイル、そして
**正本から再生成できなくなったファイル**。`egress-check` / `pin-*-check` / `versions-check` が
後で捕まえるが、それは別の人の Pull Request の上で、その人が触っていないコードの失敗として出る。

second reason is quieter. **衝突しなかったことは、終わっていることを意味しない。**
`allowed-endpoints` は `.github/egress.toml` から生成される —— 2つのブランチが別々の job を足せば、
テキストとしては1行も衝突しないまま、生成物だけが古くなる。**衝突の名前を持つスキルは、
まさにその場合に起動されない。** だからこのスキルは merge の名前を持つ。

## Arguments

| Argument | Effect |
| --- | --- |
| `--base=<ref>` | 解決せずこの base を使う。hotfix 線が絡むときは必須（Step 1） |
| `--class=<csv>` | 指定クラスだけ扱う。残りは報告して触らない |
| `--dry-run` | 分類と計画だけを出す。何も変えない |

## Step 1 — base を解決し、merge する

`AGENTS.md` *Git 規約* が支配する。ここで正しさを決めるのは2つ。

- **Pull Request の `baseRefName` が勝つ。** それがそのブランチが既に merge しようとしている先で
  ある。PR が無いときだけ `make base-branch` が `origin` の実状態から活きたリリース線を解決する。
  **`refs/remotes/origin/HEAD` からも `gh repo view --json defaultBranchRef` からも取らない** ——
  どちらも警告なしに1世代古い線を答える。
- **merge であって rebase ではない。** `AGENTS.md` が禁じているうえ、rebase は追記専用のファイルを
  積極的に壊す —— 同じ内容が両側で別のハッシュとして再着地し、独立した2つの追加として読める。

```bash
BASE=$(gh pr view --json baseRefName -q '.baseRefName' 2>/dev/null || make -s base-branch)
test -n "$BASE" || { echo "ベースを解決できませんでした"; exit 1; }
git fetch origin "$BASE"
git merge "origin/${BASE}"
```

**hotfix 線が絡むときは、解決せず訊く。** `make base-branch` は `release/*` しか見ないので
hotfix を名乗らない。ブランチ名から base を推測するのは、推測が最も高く付く場面での推測である。
人から `--base=<ref>` を受け取ること。

## Step 2 — 衝突したパスを全部分類する

```bash
git diff --name-only --diff-filter=U
```

| Class | Paths | Resolution |
| --- | --- | --- |
| 生成されたインラインブロック | `.github/workflows/*.yaml` の `allowed-endpoints` | 行を選ばない。正本は `.github/egress.toml`。そちらを先に解決し、`make egress-apply` |
| ピンの lockfile | `.github/actions-pin.toml`、`docker/images-pin.toml` | 行を選ばない。`make pin-actions-resolve` + `apply` / `pin-images-resolve` + `apply` |
| 版の写し | `scripts/go.mod` の `go` 行、`docker/tools/Dockerfile` の `FROM` タグ | 正本は `mise.toml`。そちらを先に解決し、`make versions-apply` |
| 依存の lockfile | `scripts/go.sum` | 行を選ばない。`go -C scripts mod tidy` |
| 保護設定 | `.github/settings/branch-protection.json` | **和集合にしない。** 片方が required から外した検査を、機械的な和集合は黙って戻す。宣言の意図を人へ返す |
| 追記専用の索引 | `docs/adr/README.md` の表 | 両側のエントリの和集合 —— **ただし同じ番号が両側に現れたら Step 5 へ渡す** |
| ADR の本文 | `docs/adr/*.md` | **衝突すること自体が異常である。** ADR-0001 決定2 は Accepted ADR を immutable とする。片方が既存の本文を書き換えている。人へ返す |
| Terraform の lockfile | `.terraform.lock.hcl` | **まだ存在しない。** 入ったら `terraform providers lock` が持ち主で、行を選ばない |
| 実装 | それ以外すべて | **機械的でない。** マーカーを残して手渡す |

分類を先に終える。**どの行にも当たらないパスは、既定で実装である** —— 安全な向きだからである。
機械的なものを手渡す費用はメッセージ1つ、意味的なものを機械的に「解決」する費用は黙った誤 merge である。

## Step 3 — クラスごとに当てる

依存の順に解決する。**正本が先、生成物が後。**

生成のクラスでは、やることはどれも同じで、はっきり言う価値がある —— **ファイルを merge しない。
衝突を消して、作り直す。** 正本（`.github/egress.toml`、`mise.toml`）を両側から取り、それが
衝突していたならそこを解決し、そのうえで生成器を走らせる。

ピンの lockfile では、エントリを突き合わせずに resolver を走らせる。lockfile は tag → SHA の
キャッシュであり、**手で merge すると、そのタグに一度も対応したことのない SHA を持つエントリが
残りうる** —— 下流の検査はそれを権威として扱う。

**`pin-*-resolve` は網へ出て、クールダウンに当たる。** 窓に捕まったら、それは正しく働いている
（`repo-ops` §6）。無理に通さない。

## Step 4 — 衝突しなかった派生物を作り直す

**Step 2 が何も見つけなくても走らせる。そして走らせたと言う。**

```bash
make versions-check || make versions-apply
make egress-check   || make egress-apply
make pin-actions-check
make pin-images-check
```

`check` を先に出すのは、`resolve` が網へ出てクールダウンに当たるためである —— ずれていないのに
毎回 resolver を回すと、窓に捕まっただけの失敗を merge のたびに作る。

**派生物は、テキストとして衝突したからではなく、相手側の変更によって古くなる。**
この段を飛ばすと、ローカルでは緑の merge と、次の無関係な Pull Request での赤ができる。

## Step 5 — 終わり方は2つだけ

**全部が機械的に解けた場合** —— 解決したクラスと、衝突していないのに作り直したものを報告する。
`git add` はするが、**commit はしない** —— `/commit` がその持ち主である。

**1つでも残った場合** —— **マーカーを残したまま打ち切る。** 残ったパスと、なぜ機械的でないかを
述べる。部分的に解決したものは報告に含める。

次のものは必ず人へ返す。

- 実装の意味的な衝突
- **同じ ADR 番号が両側に現れた** —— ADR-0001 決定7 の採番の詰め直しが両側で走った形。
  番号は identity ではないので、どちらを詰め直すかは人が決める
- **Accepted ADR の本文の衝突** —— immutable の違反が既に起きている
- **保護設定の宣言の衝突** —— 片方が検査を外している可能性がある
- 宣言そのものの衝突（`mise.toml` の同じキー、`.github/egress.toml` の同じ job）

## Do / Do NOT

- ✅ 衝突の有無にかかわらず Step 4 を走らせ、走らせたと言う
- ✅ 生成物は作り直す。lockfile は resolver を走らせる
- ✅ 分類に当たらないパスを実装として扱う
- ✅ 残ったものはマーカーを残して手渡す
- ❌ rebase / squash / force-push（`AGENTS.md` *トリップワイヤ* 7）
- ❌ 生成物の片側を選ぶ
- ❌ 保護設定の required context を機械的に和集合にする
- ❌ Accepted ADR の本文を解決する
- ❌ commit する

## Checklist

- [ ] base は PR の `baseRefName`、無ければ `make base-branch`。`origin/HEAD` から取っていない
- [ ] merge であり、rebase していない
- [ ] 衝突したパスを全部分類した（当たらないものは実装として扱った）
- [ ] 正本を先に、生成物を後に解決した
- [ ] **衝突しなかった派生物も作り直した**（`check` を先に出した）
- [ ] 機械的に解けないものはマーカーを残して手渡した
- [ ] commit していない
