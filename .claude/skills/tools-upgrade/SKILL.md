---
name: tools-upgrade
description: >-
  Audit the tool versions pinned in `mise.toml` `[tools]` against upstream latest and apply the ones the user approves. The classification is not re-derived here — `make tool-cooldown-outdated` already resolves each tool's backend through `mise registry`, applies the window that backend deserves (14 days for GitHub Releases, 7 for package registries), and reports 今すぐ上げられる / 窓待ち / 上流を確認できず. This skill reads that verdict, hands every 窓待ち entry to `/supply-chain-triage` when the answer would change what the user does, confirms the set, edits `mise.toml`, and re-runs the gates. Use on a routine cadence or after a security advisory. Do NOT use it to upgrade the Go runtime (`/go-upgrade` — the downstream propagation differs), to pin GitHub Actions or Docker images (`/actions-pin`, `/images-pin`), or to patch one advisory-named dependency.
---

# Tool Version Upgrade

`mise.toml` の `[tools]` が pin している道具の版を、上流の最新と突き合わせて上げる。

**分類はここで作らない。** `make tool-cooldown-outdated` が既にやっている —— バックエンドを
`mise registry` に尋ねて解決し、そのバックエンドに応じた窓（GitHub Release 経由は14日、
パッケージレジストリ経由は7日）を当て、分類まで出す。**表をここへ写すと、mise が変わった次の瞬間に
ずれる**（`scripts/README.md` の `tool-cooldown/` の行がその理由を述べている）。

## 窓がツールではなくバックエンドで決まる理由

タグは別のコミットへ付け替えられるので、GitHub Release 経由のものは `pin-actions` / `pin-images` と
同じ14日。パッケージレジストリ（go / npm）は公開された版が不変なので `go-cooldown` と同じ7日。

**言語ランタイム（`core:` バックエンド）は受容したリスクとして除外されている。** 汚染された go や
node の配布物は供給網の1リンクの失敗ではなく言語の信頼モデルの失敗であり、クールダウンでは守れない。
`outdated` の「ランタイム除外 N 件」はこれである。

## When to Use

- 定期の棚卸し（月次 / 四半期）
- セキュリティ勧告のあと
- 窓で止まっていた版が、明けたので採りに行くとき

Do NOT use it for:

- **Go のランタイム** — `/go-upgrade`。写しへの伝播（`scripts/go.mod` と Dockerfile）が違う
- **node のランタイム** — 同じく `mise.toml` を直したら `make versions-apply` が要る
- GitHub Actions / Docker image の固定 — `/actions-pin` / `/images-pin`
- 勧告が名指しした1件の依存 — そちらは `go.mod` の話で、このスキルの対象ではない

## Arguments

| Token | 意味 | 既定 |
| --- | --- | --- |
| `advisory` | 勧告を起点にした実行。窓待ちの全件を `/supply-chain-triage` へ回す | 棚卸し（triage は既定で走らせない） |

## 変更してよい範囲

`AGENTS.md` *v1.0.0 までの暫定* がリポジトリ直下の設定ファイルの編集を解除している。触れてよいのは次だけ。

- `mise.toml` の `[tools]` —— **ユーザーが明示的に承認したエントリだけ**
- `.github/tool-cooldown-bypass.toml` —— 理由と失効期限を書けるときだけ（ADR-0702 決定18）
- `scripts/go.mod` / `docker/tools/Dockerfile` —— `make versions-apply` の出力としてのみ

**解除されないもの**: `AGENTS.md` / `LICENSE` / Accepted ADR の本文、生成物（*トリップワイヤ* 4）。

## 手順

### 1. 棚卸しを取る

```sh
make tool-cooldown-outdated
```

出力は3つの数を持つ —— **今すぐ上げられる / 窓待ち / 上流を確認できず**。

**「上流を確認できず」を0件でないまま進めない。** それは「最新である」とは違う。確認できなかった
ものを「変わっていない」と読むのが、このリポジトリが繰り返し塞いでいる故障の形である
（ADR-0702 決定14）。

### 2. 窓待ちを triage する（判断が変わるときだけ）

窓待ちは、**公開からの日数という代理指標だけで止めている**状態である。代理が答えようとしている
問い —— 発行元が変わっていないか、成果物が出所と一致するか、何が変わったか、新しい依存が現れたか
—— は直接答えられる。

`/supply-chain-triage` を回すのは、**答えが人の行動を変えるとき**だけにする。

| 状況 | triage する？ |
| --- | --- |
| 定期の棚卸しで、待てばよい | ❌ 待つのが答えである |
| 勧告があり、修正が新しい版にしか無い | ✅ 待つことが「脆弱なままでいる」を意味する |
| ユーザーが「今採って大丈夫か」と訊いた | ✅ |

triage は報告のみで、宣言も bypass も窓も触らない。

### 3. 出す案を確認する

日本語で、分類ごとに並べる。窓待ちは**いつ明けるか**を添える —— 「待つ」を選べる形にするため。
そのうえで `AskUserQuestion`（複数あるなら `multiSelect: true`）で、**上げる集合を確定させる**。

```text
ツール版の棚卸し（GitHub 14日 / レジストリ 7日）

✅ 今すぐ上げられる
  - <tool>: X.Y.Z → A.B.C（公開 N 日前）

⏳ 窓待ち
  - <tool>: X.Y.Z → A.B.C（公開 N 日前、明けるのは YYYY-MM-DD）

❓ 上流を確認できず
  - <tool>: 理由

除外（ランタイム）: go / node —— 窓では守れないため
```

### 4. `mise.toml` を直す

承認されたエントリだけ書き換える。**承認されていないものを「ついでに」上げない。**

### 5. 写しへ反映する

`go` / `node` を上げたなら、写し（`scripts/go.mod`、`docker/tools/Dockerfile`）が追随する。

```sh
make versions-apply
```

ただし**ランタイムの版上げはこのスキルの仕事ではない** —— `go` は `/go-upgrade` が、そこに付随する
digest の貼り直しまで含めて持っている。

### 6. 検証

```sh
make tool-cooldown-audit     # 全件を並べる。窓では落とさない
make tool-cooldown-gate BASE=origin/$(make -s base-branch)
make versions-check
make go-lint
make go-test
```

`gate` は base ref と比較して落とす。**窓に捕まったなら、それは正しく働いている** —— バイパスを
書く前に §2 へ戻る。

`audit` / `gate` はどちらも `.github/tool-cooldown-bypass.toml` の**期限切れ・3ヶ月超・どれにも
当たらないエントリ**で落ちる。無効なエントリは効力を失うので、失効したバイパスがそのツールを
通し続けることはない。

### 7. 報告

上げた版、窓で見送った版とその明ける日付、triage を回したならその判定、各ゲートの出力。
**commit も push もしない。**

## Notes

- **上流を確認できなかったものを「最新」と読まない。** 数が0でないまま報告を終えない。
- **バイパスは最後の手段である。** 理由を書けないものは抑止せず、値そのものを直す（ADR-0501 決定13）。
  バイパスには期限が要り、期限切れ・3ヶ月超・対象不存在はゲートを落とす（ADR-0702 決定18）。
- 版を下げる方向の「更新」を採らない。解決結果が pin より小さいなら、それは解決の失敗である。
- 道具はそのまま実行する。version manager のサブコマンドで包まない（ADR-0501 決定20）。
- このスキルは push しない。

## Checklist

- [ ] `make tool-cooldown-outdated` を走らせ、3つの数をそのまま報告した
- [ ] 「上流を確認できず」が0件でないまま進めていない
- [ ] 窓待ちを triage したのは、答えが人の行動を変えるときだけである
- [ ] 窓待ちに**明ける日付**を添え、「待つ」を選べる形にした
- [ ] `AskUserQuestion` で上げる集合を確定させ、承認されたものだけ書き換えた
- [ ] ランタイムを上げたなら `make versions-apply` を走らせた（`go` は `/go-upgrade` へ回した）
- [ ] `tool-cooldown-audit` / `gate` / `versions-check` / `go-lint` / `go-test` を走らせ、出力を
      そのまま報告した
- [ ] バイパスを書いたなら、理由と失効期限を書いた
- [ ] commit / push をしていない
