---
name: go-upgrade
description: >-
  Upgrade the Go runtime this repository's operations tooling (`scripts/`) is built on. The target version is confirmed with the user, `mise.toml` `[tools] go` is updated, `make versions-apply` propagates it to `scripts/go.mod` and `docker/tools/Dockerfile`, and the base-image digest is re-pinned — which is where the two are coupled, because a just-published `golang:` image has no aged digest to step back to and `pin-images-resolve` fails closed by design. Optionally bundles a Go module dependency refresh, which the cooldown gate then judges. Use it when moving to a new Go release. Do NOT use it to bump other pinned tools (`mise.toml` `[tools]` の他の項目は `make tool-cooldown-outdated` で棚卸しする), to refresh only a digest (`/images-pin`), or to patch one vulnerable dependency.
---

# Go Version Upgrade

`scripts/` の運用機構が使う Go の版を上げる手順。**このリポジトリの Go は運用機構の実装言語であって、
成果物（Terraform）の言語ではない**（ADR-0702 決定6）。上げても `modules/` には影響しない。

## First Step: 目標の版を確認する

**起動直後に `AskUserQuestion` を呼ぶ。** 引数や直前の発言に版らしき文字列があっても、黙って採用しない。

1. `mise.toml` の `[tools]` の `go` を読んで現在の版を出す。
2. `AskUserQuestion` で訊く —— 「上げる先の Go の版を指定してください（例: `1.27.2`）」。現在の版を
   添える。候補が文脈にあるなら「候補: `X.Y.Z`」として出し、確認させる。
3. `X.Y.Z` の形であることを確かめ、以降 `<TARGET>` とする。

**版が確定するまでファイルを1つも触らない。**

## 前提

- 保護ブランチ（`production` / `develop` / `staging` / `release/*`）で作業しない。
- 最新のリリース線から作業ブランチを切る。`make base-branch` が `origin` の実状態から解決する
  —— `gh repo view --json defaultBranchRef` は古い答えを返す。

```sh
BASE=$(make -s base-branch)
git fetch origin "$BASE"
git switch -c feature/go-upgrade-<TARGET> "origin/$BASE"
```

`git switch -c … origin/release/*` は**新しいブランチの上流を保護ブランチに設定する**。初回の push は
必ず `git push -u origin <branch>` で明示する（`repo-ops` §3）。

## 変更してよい範囲

`AGENTS.md` の *v1.0.0 までの暫定* がリポジトリ直下の設定ファイルの編集を解除している。触れてよいのは次だけ。

- `mise.toml` の `[tools] go`
- `scripts/go.mod` / `scripts/go.sum`（`make versions-apply` と `go mod tidy` が書く）
- `docker/tools/Dockerfile` の `FROM golang:` のタグ（`make versions-apply` が書く）
- `docker/images-pin.toml`（`make pin-images-resolve` が書く）

**解除されないもの**: `AGENTS.md` / `LICENSE` / Accepted ADR の本文、生成物（*トリップワイヤ* 4）。

## 手順

### 1. リリースノートを読む

<https://go.dev/doc/devel/release> で `<TARGET>` の変更点を見る。運用機構が使っている標準ライブラリ
（`os/exec` / `encoding/json` / `regexp` / `path/filepath`）の挙動変更は、ゲートの判定を静かに変えうる。

### 2. `mise.toml` を直す

```toml
[tools]
go = "<TARGET>"
```

**版の正本はここだけである**（ADR-0501 決定19）。他はすべて写しで、次の段が反映する。

### 3. 写しへ反映する

```sh
make versions-apply
make versions-check
```

`scripts/go.mod` の `go` ディレクティブと `docker/tools/Dockerfile` の `FROM golang:` 2件が揃う。
**手で直さない** —— 対応表は `scripts/versions/main.go` の `rules` が持つ。

CI は `actions/setup-go` に `go-version-file: scripts/go.mod` を渡しているので、**workflow の編集は
要らない**。どこかが版を文字列で直書きしていたら、それは `versions` の対応表に足すべき写しである。

### 4. ベースイメージの digest を貼り直す —— **ここが結合点**

前段でタグが変わったが、**`@sha256:...` は古いイメージを指したままである**。Docker は digest を
優先するので、このままではビルドが黙って古いイメージを引く。

```sh
make pin-images-resolve
make pin-images-apply
make pin-images-check
```

**多くの場合 `resolve` はここで閉じる方向に落ちる。** 新しい `golang:` タグは lockfile に前のエントリを
持たず、公開されたばかりなので `PIN_IMAGES_MIN_AGE_DAYS`（既定14日）の窓の中にある —— **退行先が無い**。
`images-pin` の rule 3 がこれを採らずに非ゼロで終わる。これは故障ではなく設計である。

> **Go の版上げは、そのベースイメージの窓が明けるまで着地できない。**

取れる手は2つだけで、**どちらも人が選ぶ**。

| 手 | 条件 |
| --- | --- |
| 窓が明けるまで待つ | 既定。`resolve` が採用可能になる日付を報告する |
| `days=0` で敷く | `/supply-chain-triage` で証拠を取ったうえで、リスクを受け入れると決めたとき |

**`resolve` を無理に通さない。タグと digest がずれたまま木に残さない。** どちらも「古いイメージで
ビルドしているのに新しい版に上げたつもり」を作る。

### 5. ローカルの Go を入れ替える —— **ユーザーの仕事**

```sh
mise install go
go version
```

**エージェントは `mise install` を実行しない**（`AGENTS.md` *道具の導入*: 自分の判断で install しない）。
ユーザーに依頼し、`go version` が `<TARGET>` を返すことを確認してもらう。

### 6. 依存を整える

```sh
go -C scripts mod tidy
make go-tidy-check
```

### 7.（任意）Go モジュールの依存を更新する

Go の版上げは依存を見直す自然な機会だが、**同じ変更に混ぜると、落ちたときにどちらが原因か分からなく
なる**。`AskUserQuestion` で訊く。

- **最新のマイナーまで**（`go get -u ./...`）
- **パッチのみ**（`go get -u=patch ./...`）—— 安全側
- **触らない** —— Go の版だけ上げる

major は自動で上げない（`go get -u` は設計上またがない）。

更新したら **`make go-cooldown-gate BASE=origin/$(make -s base-branch)` が判定する** —— 公開されたばかりの
版は7日の窓で止まる。止まったら `repo-ops` §6 を読む。**バイパスは理由と失効期限を書けるときだけ**
（ADR-0501 決定13）。

### 8. 検証

```sh
make go-fmt-check
make go-lint
make go-test
make versions-check
make pin-images-check
```

`make` が `go: command not found` で落ちるなら `repo-ops` §1。落ちたゲートは、それ自身が報告した
とおりに報告する —— **行や件数を落とすフィルタ越しに報告しない**。

### 9. 報告

上げた版、写しが揃ったこと、digest の状態（貼り直せたか、窓で止まったか、止まったならいつ明けるか）、
依存を更新したならその範囲、各ゲートの判定。**commit も push もしない** —— `/commit` と `/submit-pr` が
その持ち主である。

## Notes

- **`scripts/go.mod` / Dockerfile を手で編集しない。** `make versions-apply` が持ち主である。
  `mise.toml` を直して再実行する。
- **`go` ディレクティブは言語版の下限である。** 低い値を書いてもビルドは通るので、ずれても誰も
  気づかない —— だから `versions-check` が在る（`repo-ops` §8）。
- Go の版上げは `modules/` の Terraform には影響しない。実行エンジンの版は `mise.toml` の別項目が持つ。
- このスキルは push しない。

## Checklist

- [ ] `<TARGET>` を `AskUserQuestion` で確認した
- [ ] リリースノートを読んだ
- [ ] `mise.toml` の `[tools] go` を直した
- [ ] `make versions-apply` + `versions-check` が通った
- [ ] digest を貼り直した。**rule 3 で閉じたなら、それが新しいイメージに対する期待される結果である**
      —— 待つか `days=0` かを人へ返し、無理に通さず、タグと digest のずれを残していない
- [ ] ローカルの Go の入れ替えをユーザーへ依頼した（エージェントは install しない）
- [ ] `go -C scripts mod tidy` + `make go-tidy-check`
- [ ] （任意）依存の更新を `AskUserQuestion` で確認し、`go-cooldown-gate` の判定を報告した
- [ ] `go-fmt-check` / `go-lint` / `go-test` / `versions-check` / `pin-images-check` を走らせ、
      出力をそのまま報告した
- [ ] commit / push をしていない
