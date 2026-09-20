---
name: dep-vuln-upgrade
description: >-
  Patch the specific Go modules a security advisory names, in `scripts/go.mod` — targeting only the named packages rather than a blanket refresh, including indirect modules, and handing every entry the cooldown catches to `/supply-chain-triage`. Use it whenever the user pastes a vulnerability report — Trivy (`make trivy-fs`), the dependency-review job, Dependabot, or a hand-written list of "package current → fixed (CVE)" lines — and wants those exact modules bumped to a fixed version. Safe patches are applied; only major-version bumps and deliberate cooldown overrides are asked about. Do NOT use it for a routine "bump everything" audit (`/tools-upgrade` owns `mise.toml`), to upgrade the Go runtime (`/go-upgrade`), to re-pin a GitHub Action or a Docker image (`/actions-pin`, `/images-pin`), or for a general dependency refresh with no advisory behind it (`go -C scripts get -u ./...`).
---

# Advisory-driven Dependency Patch

勧告が**名指しした**モジュールだけを、修正版まで上げる。

**このリポジトリの依存の解決系は1つだけである** —— `scripts/go.mod` の Go モジュール。
`modules/` に Terraform の実装が入れば provider の版という別の面が生まれるが、それは
`.terraform.lock.hcl` の話で、このスキルの対象ではない。

## 勧告はどこから来るか

| 出所 | 何を報告するか |
| --- | --- |
| `make trivy-fs` | 依存の既知の脆弱性。**`--ignore-unfixed`** なので、修正版のあるものだけ |
| `make trivy-fs-release` | 修正版の無いものも含む。リリース昇格のゲート |
| `dependency-review` の job | PR が持ち込む依存の変更 |
| 手書きの一覧 | 「package current → fixed (CVE)」の行 |

**`trivy-fs` が「所見なし」と言ったとき、見たことの証拠を確かめる。** 脆弱性 DB を取得できずに
FATAL で落ちても、ログの下だけ読むと「所見なし」に見える —— 実例がある（`repo-ops` §5）。

## When to Use

- 勧告が `scripts/go.mod` の直接・間接のモジュールを名指ししていて、最小限の版上げが要るとき

Do NOT use it for:

- 勧告の無い一般の依存更新 — `go -C scripts get -u ./...` と `make go-tidy-check`
- `mise.toml` の道具 — `/tools-upgrade`
- Go のランタイム — `/go-upgrade`
- GitHub Action / Docker image — `/actions-pin` / `/images-pin`
- **一次コードのマルウェア検査** — このスキルは依存しか見ない

## 変更してよい範囲

- `scripts/go.mod` / `scripts/go.sum` —— 勧告が名指ししたモジュールだけ
- `.github/go-cooldown-bypass.toml` —— 理由と失効期限を書けるときだけ（ADR-0702 決定18）
- `.trivyignore.yaml` —— **抑止は最後の手段である**。理由を書けないものは抑止せず、値を直す

**解除されないもの**: `AGENTS.md` / `LICENSE` / Accepted ADR の本文、生成物（*トリップワイヤ* 4）。

## 手順

### 1. 勧告を、名前と修正版の一覧へ正規化する

各行を `module / 現在の版 / 修正版 / 勧告 ID` にする。**直接と間接を区別する。**

```sh
go -C scripts list -m -f '{{.Path}} {{.Version}} {{if .Indirect}}(indirect){{end}}' all | grep <module>
go -C scripts mod why -m <module>     # なぜ木に在るのか
```

`mod why` が「誰も要求していない」と答えるなら、それは `go mod tidy` で消える枝である。上げる前に
確かめる —— **消える依存を上げるのは、上げ先の窓に無駄に捕まる。**

### 2. 上げ方を決める

| 立ち位置 | 手 |
| --- | --- |
| 直接依存 | `go -C scripts get <module>@<fixed>` |
| 間接依存で、直接の親が新しい版を要求していない | `go -C scripts get <module>@<fixed>` が間接のまま上げる |
| 間接依存で、親の版上げが要る | **親の版上げは別の判断である** —— 名指しされていないものを巻き込むので、`AskUserQuestion` で訊く |
| major をまたぐ | **必ず訊く。** import path が変わるので、コードの変更を伴う |

### 3. 当てる

```sh
go -C scripts get <module>@<fixed>
go -C scripts mod tidy
```

**名指しされたものだけを上げる。** `go get -u ./...` を混ぜない —— 落ちたときに、勧告の修正が原因か
巻き添えの更新が原因かが分からなくなる。

### 4. クールダウンの判定を受ける

```sh
make go-cooldown-gate BASE=origin/$(make -s base-branch)
```

窓は7日（公開された版が不変なため）。**捕まったら、それは正しく働いている。**

| 手 | 条件 |
| --- | --- |
| 窓が明けるまで待つ | 既定。**ただし待つことは「脆弱なままでいる」ことでもある** |
| `/supply-chain-triage` で証拠を取る | 修正が新しい版にしか無く、待てないとき |
| バイパスを書く | triage の判定を見たうえで、理由と失効期限を書けるとき |

**この判断はユーザーへ返す。** 脆弱性の重大度と供給網のリスクを天秤にかけるのは設計判断であり、
`AGENTS.md` 制約2 が人のゲートを要求している。

### 5. 検証

```sh
make go-tidy-check
make go-test
make go-lint
make trivy-fs
```

`trivy-fs` が当該 CVE を**報告しなくなったこと**を確かめる。報告が消えない場合、上げた版が
修正版でないか、別経路で古い版が残っている（`go -C scripts mod graph | grep <module>`）。

### 6. 報告

上げたモジュールと版、勧告 ID、`trivy-fs` の前後、クールダウンで見送ったものとその明ける日付、
巻き込んだ親があるならその理由。**commit も push もしない。**

## Notes

- **勧告が名指ししていないものを上げない。** 「ついでに」は、落ちたときの切り分けを壊す。
- **修正版の無い脆弱性**は `trivy-fs` の `--ignore-unfixed` に隠れる。`make trivy-fs-release` が
  それを含めて見る —— リリース昇格のゲートである。
- `.trivyignore.yaml` への追加は**新規の抑止**であり、`AGENTS.md` の停止点に当たる
  （ADR-0501 決定13）。理由・影響・撤回条件を書けないなら抑止しない。
- このスキルは push しない。

## Checklist

- [ ] 勧告を `module / 現在 / 修正版 / 勧告 ID` の一覧へ正規化した
- [ ] 各モジュールが直接か間接か、`mod why` で木に在る理由を確かめた
- [ ] **名指しされたものだけ**を上げた（`go get -u ./...` を混ぜていない）
- [ ] major をまたぐ版上げ、親を巻き込む版上げは `AskUserQuestion` で確認した
- [ ] `make go-cooldown-gate` を走らせ、捕まったなら判断をユーザーへ返した
- [ ] `go-tidy-check` / `go-test` / `go-lint` / `trivy-fs` を走らせ、出力をそのまま報告した
- [ ] `trivy-fs` が当該 CVE を報告しなくなったことを確かめた
- [ ] 抑止を足したなら、理由・影響・撤回条件を書いた
- [ ] commit / push をしていない
