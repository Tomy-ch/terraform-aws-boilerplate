---
name: settle-comments
description: >-
  Cut the dead weight out of the EXISTING STOCK of source-code comments in a chosen scope, tests included. Dead weight is three things: 経緯 (development history — "previously", "changed to", "originally"), 日数経過 (anything true only on the day it was written — "currently", "for now", "not yet", a deadline), and 読めば意味がわかるもの (a restatement of the code or of the identifier's own name). All three are deleted outright — git owns the history, a declaration owns a deadline, and the code owns what the code says. Two further deletions follow: a fact some test case, `golangci-lint` rule, gate (`adr-lint` / `required-check-lint` / `egress-check` / `pin-*-check`) or terraform-docs generated region already keeps true, dropped against a named `file:line`; and a Why written at several declarations, collapsed to one site. Relocation into `docs/adr/` / `modules/<use-case>/docs/adr/` / a README is the fallback for the remainder that cannot simply go, never the goal. Use it whenever a comment narrates development history or says "currently" / "for now"; whenever a comment restates the code, the identifier's name, or what a test or a linter already enforces; whenever a test file's comments restate its `t.Run` case names or narrate where the cases came from; whenever comments feel bloated, verbose, over-explained, or essay-like even though each line is individually true; whenever the same reason appears at several declarations and no one place is authoritative; whenever a doc comment has grown into a design argument, threat-model analysis, or rejected-alternative discussion; for a periodic hygiene pass over a package / area / whole repo; before a large PR or a boilerplate cut; and when someone asks 「コメントが長すぎる」「コメントを整理して」「テストと同じことをコメントが書いている」「この Why はコードに置くべきか」「コメントを ADR に移したい」「テストのコメントを整理して」. Modes: 確認して適用 (default), 自動適用 (`--apply`), 報告のみ (`--report-only`). Sole owner of the comment subject — no review skill carries a comment lens — and it runs **unconditionally as the last step of every implementation** over the declarations the change touched (`AGENTS.md` *作業手順* 6), not as a review whose return gets estimated. Do NOT use it to judge README / docs prose quality, or to delete `// Name は、〜です。` field comments — that convention is deliberately preserved.
---

# Settle Comments

**無駄なコメントを削る。** それがこのスキルの仕事である。

無駄とは3つを指す。

| 無駄 | 何が問題か | 判定 |
| --- | --- | --- |
| **経緯** —「以前は」「〜に変更した」「元々は」「輸入元では」 | 変更履歴は git が持つ。コードの隣に置いた写しは、次の変更で黙って古くなる（ADR-0701 決定3） | **削除** |
| **日数経過** —「現在」「今のところ」「まだ」「将来」「v1.0.0 までは」「14日後に」 | 書いた日にだけ正しい文。読む日には嘘になっているが、嘘になったことを誰も報せない | **削除** |
| **読めば意味がわかるもの** — コードの言い直し、識別子名の言い直し、手順の実況 | コードを読めば分かることを二度書いている。コードが動いたとき、写しだけが取り残される | **削除** |

これらに**移設先は無い。** どこかへ移せば残せるものではなく、消えるべきものである。移設は、
**消せない残りの受け皿**であって、このスキルの目的ではない。

そのうえで、削れるものがもう2種類ある —— 既に何かが守っている事実（**不要**）と、複数箇所へ
書かれた同じ Why（**集約**）。どちらも減らす方向の判定である。

## When to Use

- Comments in a package read as bloated / essay-like even though nothing in them is wrong.
- A doc comment has grown into a design argument (rejected alternatives, threat model, architecture policy).
- A comment restates what a test case, a lint rule, an architecture scan or a spec already enforces.
- Periodic hygiene pass over a package, a layer, or the repo.
- Before a boilerplate cut, where accumulated commentary becomes downstream reading burden.

Do NOT use for:

- README / `docs/**` の散文としての品質。本スキルは移設先へ**書き足す**だけで、既にそこにあるものを監査しない。
- README とコードの構造的な乖離。それは `/impl-review` の `declaration-drift` レンズが見る。

## Why this skill exists (read this before judging anything)

[ADR-0701](../../../docs/adr/0701-documentation-ownership-and-language.md) 決定1 は「1つの規則は1つの
文書が所有し、他は参照して再掲しない」と述べ、決定3 は「文書本文に変更履歴と経緯を書かない」と述べる。
コードコメントも同じ規律の下にある。それでもコメントは増え続ける。構造的な理由が2つあり、それを
理解していることが、このスキルが効く条件である:

1. **残す論証は、削る論証に常に勝つ。** 「この Why は自明でなく、検証可能である」は具体的な主張で
   ある。「量はコストである」は抽象的な主張である。具体は抽象に毎回勝つので、判断は残す方向へ倒れ、
   在庫は増える一方になる。量を論じ直しても勝てない —— 試みないこと。
2. **Review is diff-scoped.** A review of a change never re-examines what is already there. There is a
   path in and no path out.

So this skill does not re-litigate whether a Why is good. **上の3類型は、その論争に入らずに削れる。**
経緯・日数経過・言い直しは「自明でないか」を問うまでもなく無駄であり、「非自明だから残す」という
論証がそもそも当たらない。残りに対してだけ、下の**同じくらい具体的な問い**を立てる。

### Pass 0 — 3類型を落とす（他の問いを立てる前に）

各コメントについて、まずこれだけを見る。当たれば **削除** で終わり、管轄も守り手も問わない。

- **経緯か。** 「以前」「元々」「変更した」「だった」「輸入」「移行」「今回」—— 過去形で、いま
  読む人が何をしてよいかを1つも決めていない文。git log が持つ。
- **日数経過か。** 「現在」「今のところ」「まだ」「将来」「いずれ」「N日」「vX.Y までは」——
  書いた時点に依存し、**期限が来ても誰も落とさない**文。期限が実在するなら、それは宣言が持つべき
  ものである（`.github/*-bypass.toml` の期限は切れればゲートを落とす。コメントは落とさない）。
- **読めば意味がわかるか。** 直下の行が言っていることを日本語で言い直した文、識別子の名前を
  なぞった文（`// Limit は、取得件数の上限です。`）、`// まず X する。次に Y する。` の実況。

**3類型のいずれでもない場合にだけ、Pass 1 へ進む。**

### Pass 1 — 守り手がいるか

管轄より先に問う。これも移設を伴わずにコメントを消せるからである。

> **If this sentence turned false, would anything fail?**

If something would — a compile error, a test case, a `golangci-lint` rule, one of this repository's
own gates (`adr-lint` / `required-check-lint` / `actions-mise-pin-lint` / `egress-check` /
`pin-actions-check` / `pin-images-check` / `go-cooldown-gate` / `tool-cooldown-gate`), or a
terraform-docs generated region — then that mechanism is
what keeps the fact true and the comment is a second copy
of it. The two are not peers: the mechanism is updated whenever reality moves, because nothing
proceeds until it is green again, while the comment is updated only when someone remembers. The copy
is therefore the one that goes wrong, and it goes wrong with nothing turning red. Verdict **不要**.

Two bounds make this safe, and dropping either produces the worst edit this skill can make — deleting
a contract nothing else publishes:

- **The guard must be named as `file:line`.** "It reads as unnecessary" is not evidence; it is the
  same reading that wrote the duplicate. A sentence with no nameable guard is **unguarded**, which
  argues for keeping it.
- **どちらの写しが消えるかは、実行ごとに決めない。** doc コメントは**呼び出し側から見える契約を
  公表する**ものなので、テストがそれを言い直していても、コメントが写しに格下げされるわけではない。
  落とせるのは、呼び出し側から観測できない内容 —— なぜそういう決定になったのか —— の方である。
  そちらの持ち主は ADR-0701 決定2 の所有表が決める。

### Pass 2 — 管轄（消せない残りの行き先）

Pass 0 と Pass 1 を抜けたものだけがここへ来る。**ここで初めて移設が出てくるが、移設は削除の
代わりではない。** 移すのは、消すと失われるものが実在し、かつそれをコードの隣に置く理由が無い場合に
限る。

> **この判断を覆す人は、どの文書の更新を義務づけられるか。**

そこへ書き、コードからリンクし、コメントには **operative residue** だけを残す —— この宣言を編集する
人が破ってはならない1〜2文。残滓が3文以上になったなら、それは移設ではなく**移設しそこねた**状態で
ある。

答えが正直に「どの文書でもない —— その制約はこの呼び出し地点にしか存在しない」であるなら、コードが
管轄であり、コメントはそのまま残る。**ただしこれは既定ではなく例外である。** Pass 0 の3類型に
当たらず、守り手も無く、移設先も無い —— 3つを通過して初めて 維持 になる。

### テストファイルの読み方

3つの pass はテストにもそのまま当たる —— 経緯も日数経過も言い直しも、テストの中に同じ形で出る。
変わるのは、テスト特有の構造をどう扱うかだけである。

- **`t.Run` のケース名は判定対象外。** あれは日本語で書かれた仕様記述であって、コメントではない。
  名前を変えることはテストが何を主張しているかを変えることであり、コメント整理の権限ではない。
- **ケース名の直上のコメントが、そのケース名を言い直しているだけなら 削除。** Pass 0 の
  「読めば意味がわかるもの」がそのまま当たる。`// 退化した入力の pin。` が
  `t.Run("検査対象の job が0件なら成功で返さない", ...)` の上に載っている、という形である。
- **「このケースが無いと、何が黙って緑になるか」を述べるコメントは 維持 が既定。**
  これは非自明な Why であり、ケース名が言えるのは*何を検査するか*までで、*検査をやめたときに
  どちらへ壊れるか*は言えない（ADR-0702 決定13-16）。テストが縮んだ日にそれを止める唯一の記述に
  なることがある —— 本番側のセンチネル注記が 不要 と判定されて消えていれば、Pass 1 の正本は
  こちら側へ移っている。**どちらの写しが消えるかは、実行のたびに決め直さない。**
- **テストヘルパ**（フィクスチャの構築、スタブ、ログの差し替え）の doc コメントは、名前と
  シグネチャの言い直しになりやすい。非公開なので `revive` の制約も掛からない。

**識別子は本スキルの対象外である。** コメントで消す類型が関数名へ焼き込まれていることがある ——
`Test_check_輸入したケース` は、削除した経緯コメントと同じものを名前に持っている。本スキルは
コメントしか見ないのでこれを検出しない。気づいたら補遺で述べ、改名はテストの変更として別に扱う。

### The next question: what one comment at a time cannot see

Jurisdiction is asked of a single comment, and that leaves a blind spot with the same shape as the one
above. When the same Why is written at three call sites, **each copy passes the jurisdiction test
independently** — each is non-obvious, each sits at the site whose premise it states, each is
individually defensible. Judged one at a time they are three 維持. The redundancy is only visible when
the surrounding comments are read as one body, so a per-comment pass cannot find it no matter how
carefully it is run.

So every audit asks a second question of its **whole assigned package's** comment stock as a unit:

> **Is this content already carried at another declaration in this package, and if so, which single
> site owns it?**

Three shapes answer to it, and none of them is reachable per comment:

- **Scattered duplication** — one Why restated at several declarations. One site owns the concept; the
  rest shrink to a pointer.
- **Fragmentation** — a constraint split across declarations so that no single place states it, and a
  reader has to assemble it. The fix is to make one site whole, not to add a fourth fragment.
- **Aggregate over-explanation** — every comment is individually correct, yet the package's total
  commentary costs more to read than the code it explains.

This is the same trap as the diff-scope one, one level down: the argument to keep each copy wins every
time it is asked in isolation, so nothing ever consolidates. Ask it of the set instead.

### The last question: what one package at a time cannot see

The trap recurs at the next level out, and it is the reason the second question is asked of a package
rather than a file. A Why repeated across *packages* passes the second question in every auditor
independently, because each auditor sees only its own scope. Nobody is looking at the relation.

This is not hypothetical. A sweep of this repository found the same sentence, verbatim, at six
declarations in three packages; each auditor could only report its own two or four copies as separate
findings, and the run that produced the sentence had written it six times without ever being asked
once whether it belonged in the code at all.

**Only the integrator sees every package, so the third question is the integrator's** (Step 2.5). It
is asked mechanically rather than by judgment, because the auditors report 維持 as a count and their
content is therefore not comparable across reports:

> **Does the same comment line appear at declarations the auditors will judge separately?**

A cluster found this way is not automatically a 集約. Resolve it by jurisdiction first, and the answer
is usually different from the within-package case:

- **The repeated content's jurisdiction is a document** — then it is **移設 at every site** (or 短縮,
  when the document already says it). No declaration owns a concept that spans packages, so there is
  no site to consolidate into. What detecting the cluster buys is that N independent "keep" judgments
  become one visible decision.
- **One declaration genuinely owns the concept and the others can name it** — then it is a 集約 whose
  members span files. The pointer must name the owning declaration, because a reader in another
  package cannot find it by proximity.

Limit worth stating: a mechanical scan finds repeated *lines*, so it catches verbatim repetition and
misses paraphrase. Verbatim is the dominant shape — the same sentence gets copied, not re-derived —
and a scan that never claims to find paraphrase is more useful than a judgment call nobody performs.

## Authoritative sources — read at runtime, hardcode nothing

| Question | Source of truth |
| --- | --- |
| どの文書が何を所有するか（移設先の判定） | [ADR-0701](../../../docs/adr/0701-documentation-ownership-and-language.md) 決定1-2 の所有表 |
| 設計判断を root と use-case のどちらへ置くか | [ADR-0001](../../../docs/adr/0001-adr-process-and-placement.md) 決定12-14。**配置そのものが所有範囲を表す** |
| 文書本文に経緯と履歴を書かない | ADR-0701 決定3 |
| 検査手段を持たない規則を宣言しない / 未配線の道具を存在するものとして書かない | ADR-0701 決定12-13 |
| ADR をどこに置き、どう改訂するか | [ADR-0001](../../../docs/adr/0001-adr-process-and-placement.md)、`docs/adr/README.md` |
| 運用機構（`scripts/**`）の観点と規律 | [`scripts/README.md`](../../../scripts/README.md)、[ADR-0702](../../../docs/adr/0702-repository-operations-substrate.md) |
| workflow の規則 | [`.github/workflows/README.md`](../../../.github/workflows/README.md) |
| ユースケースの責務境界と公開契約 | `modules/<use-case>/README.md`（ADR-0102 決定2） |

実行のたびに読み、そのまま適用する。変わっているかもしれず、記憶にある版では足りない。

**コメントが何を含んでよいかを所有する文書は、このリポジトリにまだ無い。** その判定基準は現在
本スキルが持っている —— それ自体が ADR の候補であり、気づいたら補遺で述べる。ADR-0701 決定1
（1つの規則は1つの文書が所有する）に照らせば、スキルが規則を持っている状態は暫定である。

### Relocating is not dumping — each destination has an entry bar

A relocation only helps if the prose lands where that *kind* of knowledge is owned. A document that
accepts everything answers nothing, and `docs/adr/` is the one most at risk of becoming the default
bucket, because from inside a comment almost anything reads as "design rationale". Two misroutes to
refuse outright:

- **ライブラリや API の固有の挙動**（この SDK はこの環境変数を読む、`git ls-remote` はこの形で返す、
  AWS のこの argument はこう振る舞う）は選択肢の中からの選択ではない。呼んでいるものの性質であり、
  依存を上げれば変わる。居場所は呼び出し地点のコメントである。判定: 維持。
- **ユースケースの責務境界と supported / unsupported** は `modules/<use-case>/README.md` が所有する
  （ADR-0102 決定2、ADR-0701 決定2）。ADR へ送らない。
- **運用機構のテスト観点**は `scripts/README.md` の Test Strategy 節が所有する。

**ADR を移設先に選んだら、root と use-case のどちらかを決める**（ADR-0001 決定12-14）。
`modules/<use-case>/` のコードから抜いた根拠が、そのユースケースの内部実装についてのものなら
`modules/<use-case>/docs/adr/` である。root へ置くと、**root ADR が特定ユースケースの実装詳細へ
依存する形**になり、決定19 に反する。判断がつかない場合は昇格の判断（決定20-22）に当たり、
それは停止点である —— 移設先を決めずに finding として差し出す。

`docs/adr/` takes only a **choice among alternatives with lasting consequences** or a deliberate
exclusion.

どの移設先にも当てはまらないとき、答えは2つに割れる。**Pass 0 の3類型に当たるなら 削除**である
——「移設先が無い」ことは、それが残る理由にならない。経緯はどこへも移せないから消すのであって、
移せないから残すのではない。**3類型のいずれでもなく、かつこの呼び出し地点にしか前提が無いなら
維持**であり、そのときはコードが正しい場所だったという証拠である。

悪い移設先を提案することは、何も提案しないより悪い —— 誤った移動は、放っておいたコメントより
ずっと元に戻しにくい。

## Step 0 — Confirm scope and apply mode

One `AskUserQuestion` call carrying **two** questions. Skip whichever question a flag or a caller
already answers it; skip the call entirely when both are fixed.

- 「settle-comments の対象スコープを選んでください」
  - 「指定パス配下（パッケージ / ディレクトリを続けて指定）」
  - 「ベースブランチとの diff で変更されたファイル」
  - 「区分全体（`scripts/` / `.github/workflows/` / `.makefiles/` / `docker/` / `modules/` から選択）」
  - 「キャンセル」
- 「検出結果をどう適用しますか？」
  - 「1 件ずつ確認して書き換える」 ← 既定
  - 「そのまま書き換える（1 件ずつの確認をしない。文書書き込みを伴う移設は対象外）」
  - 「報告のみ（書き込まない）」

Stock scope is the point of this skill. Diff scope exists so a large refactor can be swept without
naming every package by hand — but even then the subject is the **whole comment stock of the files the
change touched**, never the changed lines alone. A file judged in pieces cannot answer the second
question, and the duplication this skill exists to find lives between the pieces.

### Apply modes

| Mode | Selected by | What Step 4 does |
| --- | --- | --- |
| 確認して適用 | the default option, or `mode: confirm` | per-item approval, then write — every verdict is reachable |
| 自動適用 | `--apply`, or the second option | writes 短縮 / 削除 / verified 不要 / high-confidence 集約 with no per-item question; a 移設 needing a document write is reported, not applied |
| 報告のみ | `--report-only`, or `mode: report` | Step 3 renders the findings and the run ends; nothing is written |

### Flags

- `--apply` — 自動適用. Fixes the mode, so the mode question is not asked.
- `--report-only` — 報告のみ. Detect and report; never write.
- Both at once is a contradiction, not a precedence puzzle: say so and fall back to the mode
  question rather than silently picking one.

## Step 1 — Resolve targets

生成区間だけを除く。**テストは含める** —— `*_test.go` を外す根拠はどこにも無く、外した先に
受け皿も無い。コメントという主題を持つスキルは本スキルだけで、`/test-review` はテストの観点を
見るのであってコメントの内容は見ない。

```sh
find <scope> -name '*.go'
```

Go 以外（`*.tf`、シェル、Dockerfile、`.makefiles/*.mk`、`.github/**/*.yaml`、`docker-compose.yaml`）
も同じ基準で対象に入れる。**そちらの方が危険度が高い** —— `revive` が見ないうえ、このリポジトリで
最も規範的な散文は workflow と Makefile のコメントに載っている（ADR-0701 決定9 はコードコメントを
対象に含めている）。選んだスコープに入るなら含めること。

生成区間は対象外である。terraform-docs が書く区間は、生成器が定めるマーカで人が書く区間と分離されて
おり（ADR-0701 決定4）、編集しない。

Rank by comment volume so the sweep starts where the payoff is, and tell the user the ranking:

```sh
for f in <files>; do
  printf '%s %s\n' "$(grep -cE '^[[:space:]]*(//|#)' "$f")" "$f"
done | sort -rn | head -30
```

Group the resolved files by directory — that is the fan-out unit, because jurisdiction is a
per-directory judgment（`scripts/<tool>/`、`.github/workflows/`、`modules/<use-case>/` のそれぞれが、
移設先の候補となる README を持ちうる）。

## Step 2 — Fan out read-only auditors in parallel

For each package group, spawn one auditor via the **Agent tool** (`subagent_type: comment-reviewer`),
all in a **single message with multiple tool calls** so they run concurrently. Give each:

- the package directory and its resolved file list
- the instruction to read `references/audit-prompt.md` in this skill's directory and follow it verbatim
- the repo-root-relative paths of the authoritative sources above

`references/audit-prompt.md` is the auditors' single source of instructions — do not paraphrase it
into the spawn prompt, or the two will drift and the auditors will disagree with each other. It is also
what reconciles the agent with this skill: `comment-reviewer` carries its own diff-scoped taxonomy, and
under this skill the verdict vocabulary and output format come from `audit-prompt.md` instead. Say so
in the spawn prompt.

Each auditor runs **all passes** described in *Why this skill exists* — the per-comment jurisdiction
question and the per-package stock question — over the same files it has already read. The second pass
costs reading no extra material; what it adds is a question, and the findings it produces (verdict
**集約**) name a *set* of comments rather than one. Hand each auditor any cross-package cluster from
Step 2.5 that touches its files, so it judges those comments knowing they are repeated elsewhere.

Auditors are **strictly read-only**. They surface verdicts with evidence and a proposed landing
form; they never call `AskUserQuestion` and never write. Approval and every write happen in this
integrator, single-threaded, so parallel auditors cannot contend.

If subagents cannot be spawned in the current environment, follow `references/audit-prompt.md`
inline per package instead; the rest of the flow is unchanged.

## Step 2.5 — Scan for repetition across packages (integrator, mechanical)

Run this in the same message as the fan-out, before the auditors report. It is the last question from
*Why this skill exists*, and it belongs here because no auditor can see another auditor's scope.

Collect the comment lines of every resolved file, normalise away leading markers and indentation, drop
lines shorter than a clause, and report any text that appears at declarations in **more than one
file**:

```sh
for f in <resolved files>; do
  grep -hE '^[[:space:]]*(//|#|--)' "$f" \
    | sed -E 's@^[[:space:]]*(//|#|--)[[:space:]]?@@' \
    | awk -v f="$f" 'length($0) > 30 { print f "\t" $0 }'
done | sort -t$'\t' -k2 \
  | awk -F'\t' '{ n[$2]++; src[$2] = src[$2] "\n    " $1 }
                 END { for (k in n) if (n[k] > 1) print "[" n[k] "] " k src[k] }'
```

The `grep` is load-bearing: without it the pipeline clusters code and blank lines too, and every run
reports one enormous meaningless cluster. That is not a hypothetical — it is what the first draft of
this step did.

The exact pipeline matters less than the property: it is **deterministic and cheap**, so it runs on
every sweep rather than when someone suspects duplication. Tune the length floor to the scope — too
low and boilerplate field comments dominate, too high and a one-line Why slips through.

Each cluster is then resolved by the rule in *The last question*: jurisdiction first (usually 移設 /
短縮 at every site), 集約 across files only when one declaration genuinely owns the concept. Clusters
whose members all sit in one package belong to that package's auditor; carry the rest yourself.

**A cluster is a finding even when every member is individually correct.** That is the whole point —
each copy already passed jurisdiction on its own, which is why nobody had noticed.

## Step 3 — Aggregate (read-only checkpoint)

Show the full surface before any decision, so the user sees the shape of the sweep rather than being
walked through unbounded one-by-one questions:

```text
settle-comments 検出結果（scope: <X>, 対象 <n> ファイル / <m> パッケージ）

[<package>]  削除 <c> / 短縮 <b> / 不要 <g> / 集約 <e> / 移設 <d> / 維持 <a>
  削減: <行数> 行（うち 経緯 <h> / 日数経過 <i> / 言い直し <j>）
  ...（各 finding: 場所・対象コメント・判定・根拠・着地形）

移設先の内訳: docs/adr/ <p> 件 / modules/<use-case>/docs/adr/ <q> 件 / modules/<use-case>/README.md <r> 件 / その他の README <s> 件
集約: <e> 件（対象コメント計 <t> 箇所 / 内訳 重複 <u> ・分散 <v> ・総量過多 <w>）
パッケージ横断の重複: <x> クラスタ（対象コメント計 <y> 箇所 / <z> パッケージにまたがる）
総 finding: <sum>（うち要判断 <k>）。<確認して適用のときだけ「これから 1 件ずつ確認します。」を続ける>
```

Count a 集約 finding **once**, not once per member comment — it is one decision. Report the member
count alongside it so the size of the edit is visible before anyone approves it.

`維持` findings are reported as a count only — they need no decision, and listing them in full buries
the ones that do. If nothing needs action, say so plainly and stop.

**削減の行数を先に出す。** このスキルの成果は削った量であり、移設先に書いた量ではない。

**In 報告のみ mode the run ends here**, and the aggregate above is not enough on its own. Render every
non-`維持` finding in full — the evidence, the comment before and after, and for a 移設 the exact prose
proposed for the destination — because no approval loop follows to reveal them one at a time. Close by
saying how to act on the report: re-run with `--apply` for the 削除 / 短縮 / 不要 / 集約, or in 確認して適用 for
those plus the 移設. For a 集約, render every member comment, not just the site that keeps the content —
a reader cannot judge a consolidation from the winner alone.

## Step 4 — Apply (integrator-side)

Not reached in 報告のみ mode. Between the other two the write itself is identical; what differs is who
approves it, and how much of the verdict set is in play.

### 自動適用 — no per-item question

Apply **削除**, **短縮**, **不要**, and **集約** as the auditor landed them, in one pass, and report what
was applied. **Pass 0 の3類型（経緯 / 日数経過 / 言い直し）による 削除 は、このモードの中心である**
—— 判断の余地が最も小さく、誤りの代償も最も小さい（消した文は git に残っている）。

Four exclusions come off that set first:

- **A finding whose comment contradicts the code, or the document it cites** (`誤り/陳腐化`) is
  reported, never applied. Which
  side is wrong — the comment or the code — is not a comment-cleanup call, and deleting the comment
  can erase the only surviving evidence of a bug.
- **`追記なし` 移設 is applied only after the integrator opens the destination and confirms the content
  is actually there.** With a human in the loop that claim is checked at approval time; unattended,
  an auditor that misread a section would strip the rationale from the code and point the residue at
  a document that never says it. When the check fails, report the finding instead of applying it.

- **A 不要 is applied only after the integrator opens the `file:line` it names and confirms the fact
  is actually there.** This is the same check as the `追記なし` 移設 below and for the same reason: an
  auditor that misread a test name or a spec section would delete the only place a contract is
  published, and unattended there is nobody to catch it. A `不要` whose named source does not carry
  the fact is reported, not applied — and one that names no source at all is not a finding.

- **A 集約 is applied only at `確度: high`, and only when its members share one file.** 短縮 risks the
  wrong wording at one site; a consolidation additionally picks *which declaration owns the concept*,
  and it has already shrunk the other sites by the time a wrong pick becomes visible. That is markedly
  harder to undo, so anything the auditor itself rated `medium` or `low` is reported for 確認して適用
  instead of applied. A 集約 whose members span files is withheld for the same reason one level up:
  the pointer has to name a declaration a reader cannot reach by proximity, and whether that
  declaration is really the owner is exactly the judgment a no-question mode cannot make.

A `追記なし` 移設 that survives the check is applied: the destination already states the content, so the
finding is really a 短縮 to the residue plus a link and touches no document. A same-file 集約 likewise
writes no document — it only moves content between comments — which is why it belongs to this mode
at all.

**Do not apply a 移設 that would write to a destination document.** Report those with their count and
proposed landing form, and say that 確認して適用 is where they land. The reason is not caution in
general, it is the ADR question below: whether a rationale becomes a new record or a rewrite of an
existing one supersedes another is a repository-policy call under ADR-0001's immutability rule, and a
mode whose contract is "no questions" has no way to ask it. Keeping that one question alive would
break the contract; answering it silently would settle a policy question by generator.

Every guard in this file still holds — an exported Go declaration's doc comment is rewritten rather
than deleted, functional directives are untouched, and the out-of-scope list is out of scope.
自動適用 removes the question, not the rules.

### 確認して適用 — per-item approval

For each non-`維持` finding, in descending impact order:

1. Present the finding with its evidence and the concrete landing form — show the comment **before**
   and **after** as a diff, and for a 移設 also show the exact prose that will be added to the
   destination document. A verdict the user cannot see the result of is not reviewable.
2. `AskUserQuestion` with the options the auditor surfaced (typically 削除 / 短縮 / 不要 / 移設 / 維持 / 判断を保留。
   **削る側を先に並べる** —— 選択肢の順序は既定をつくるので、移設を先頭に置くと移設が既定になる)。
   For a **不要**, show the named `file:line` and what it says, not only the comment being dropped —
   the decision is whether that source really carries the fact, and it cannot be made from the
   comment alone.
   A **集約 is one question covering the whole set**, never one question per member. Splitting it
   produces incoherent outcomes the user never chose — approve the deletions but not the surviving
   site and the Why is gone; approve the survivor but not the deletions and nothing consolidated.
   Offer 集約 / 維持（現状のまま） / 別の site を本体にする / 判断を保留, and show every member.
3. On approval, write in this order — **destination document first, code second**. Reversing it
   creates a window where the rationale exists nowhere, and if the run is interrupted there, the
   reasoning is simply gone. When the auditor found the destination **already states the content**
   (`追記なし`), there is no document write: the finding lands as a 短縮 to the residue plus a link,
   and only the code changes.
4. If the destination is an **ADR**, do not decide the ADR's shape yourself. ADR-0001 は受理済みの
   ADR を immutable とし、改訂を supersede として扱う。新しい根拠が新規の記録として着地するのか、
   既存の決定を supersede するのかはリポジトリ方針の判断であって、コメント整理の判断ではない。
   Ask: 「root ADR を新規に起こす」 / 「use-case ADR（`modules/<use-case>/docs/adr/`）として起こす」 /
   「既存 ADR-NNNN を supersede する」 / 「ADR ではなく README へ」 / 「今回は移設しない」。
   ADR-0701 決定7 は訳文ペアを禁じているので、正本は日本語の1ファイルだけである。どれを選んでも、
   `docs/adr/README.md` の索引を同じ変更で更新する —— 索引は ADR の一覧が存在する唯一の場所であり、
   そこがずれると読み手は決定へ到達できない。`make adr-lint` がその整合を見る。
5. 移設先が README で、その追記が README の主張を実質的に変えるなら、README がコードの実状と一致
   しているかは別の確認であることを述べる。それは `/impl-review` の `declaration-drift` レンズの主題である。

In this mode, never batch-apply without per-item confirmation.

**ただし Pass 0 の3類型は close call ではない。** 経緯・日数経過・言い直しは、根拠を1行示せば
判断が済む。ここで時間を使うべきなのは 移設 と 集約 の方であり、3類型に1件ずつ長い説明を付けて
確認を重くすると、**削る作業が移設より高く付く**という、このスキルが避けようとしている歪みが
そのまま再現する。3類型は根拠と before/after だけを示して、短く訊く。

## Step 5 — Verify

Run this only when something was written. 報告のみ has nothing to verify; 自動適用 needs it most,
because no human read the edits one at a time.

- `make go-fmt` then `make go-lint` —— `revive` は、規約が要求する場所で doc コメントが消えたことを捕まえる。
- `make md-lint` when a Markdown destination was written.
- `make adr-lint` when an ADR was added or superseded —— 索引と本文の整合、および参照している
  `ADR-NNNN` が実在することを見る。
- workflow / `.makefiles/` のコメントを触ったなら `make actions-lint`。コメントに見えて実は
  ディレクティブだったものを消していないかは、そこで落ちる。
- Re-read each edited comment once: does the residue still stand on its own for someone who does not
  follow the link? A residue that only makes sense after reading the ADR has been cut too far, and
  that failure is invisible to every linter.
- **削った量を数えて報告する。** 削除 / 短縮 / 不要 で落ちた行数を出す。このスキルの成果はそれで
  あって、移設先へ書いた行数ではない。移設の行数が削除の行数を上回った回は、**在庫を文書へ移し替え
  ただけ**である可能性が高いので、そう述べる。
- After a 集約, read every file it touched top to bottom rather than each edited site in isolation —
  the finding was about a body of comments, so the check has to be too. Two failures show up only this
  way: the surviving site does not actually carry what the shrunk ones gave up, and a pointer names a
  declaration a reader cannot find from where they are standing. When the members spanned packages,
  read the pointer from the *other* package's side: a reference that is obvious next to the owning
  declaration is often unnavigable from three directories away.

## Explicitly out of scope

- （除外ではない）**`// Name は、〜です。` の field comment** —— `// Limit は、取得件数の上限です。`
  は Pass 0 の3類型のうち「読めば意味がわかるもの」そのものであり、**削除の対象である**。
  `revive` の `exported` は型・関数・メソッド・変数・定数の doc コメントを要求するが、struct の
  **各フィールド**には要求しない。消しても lint は落ちない。
  残るのは、名前が運べないものを載せている場合だけ —— 単位（`// Timeout は秒。`）、境界
  （`// Limit は 0 で無制限。`）、nil の意味、呼び出し側が守るべき不変。そのときは 削除 ではなく
  **短縮**して、名前の言い直しの部分だけを落とす。
- **Package overviews** — a `// Package …` comment, wherever it lives。使い方と How はそこに属する。
  このリポジトリに `doc.go` は無く、概要はいずれも通常のソースの先頭に載っているので、ファイル名で
  書いた除外は何も除外しない。**`scripts/<tool>/main.go` の先頭にある「解いている問題」の記述は、
  この除外に含まれる** —— それはそのツールが存在する理由そのもので、他のどの文書も持っていない。
  A 集約 does not get to reopen this from the other side: an overview is not a landing site for
  consolidated content either. When the fragments really do add up to a package-level statement, that
  is a 移設 to the package README, which is already a supported destination and is where a reader
  looking for package-level prose goes.
- **生成区間**（terraform-docs のマーカ内、`// Code generated ... DO NOT EDIT`）。
- **機能を持つコメント / ディレクティブ** — `//go:generate`、`//nolint`、`//go:build`、`//go:embed`、
  `# tflint-ignore`、`# trivy:ignore`、shebang、`.makefiles/` の `##` ヘルプ注記。これらは散文ではない。
  **抑止（`//nolint` 等）に付いた理由と撤回条件は消さない** —— ADR-0501 決定13 がそれを要求しており、
  消せば抑止が理由を失う。
- **移設先の文書そのものの品質**。本スキルは移設先へ**書き足す**だけで、既にそこにあるものを監査しない。

## Standalone by design

This skill is invoked in its own right, never from inside another review skill. It is also **not one of the review subjects**: `AGENTS.md` puts it at the end of the implementation, unconditionally, because the judgment only works in the detection context — a run that is still generating code writes prose for free and cannot then assess whether it was earned. `/impl-review` audits the change and `/test-review` the tests; those two are peers asked for separately, and none of the three delegates to any other. A skill that offers to run the next one makes the subjects stop being independently answerable and lets one skill's drift silently drop the others from every flow that went through it.

That independence is also what keeps the sweep stock-level. Nothing hands it a diff, nothing filters which comments its auditors may read, and nothing removes a comment from a package before the stock pass sees it — so the duplication that lives *between* comments stays visible, whether it sits in one file, one package, or across three. A sweep that received only changed regions would be a second diff review wearing the word "stock".

The comment subject therefore has exactly one owner. When a diff's newly added comments need judging, they are judged here, as part of the file they now live in.

## Relationship to the existing reviewers

| | Unit judged | Verdicts | Owns |
| --- | --- | --- | --- |
| `/impl-review` | 変更そのもの | tier 付きの findings | ADR 適合 / ゲートの規律 / セキュリティ / 正しさ / 宣言と実体のずれ |
| `/test-review` | 変更を固定するテスト | 修正必須 / 補完推奨 / 再考 / 追加検討 | テスト観点 |
| **`settle-comments`**（本スキル） | **1つのコメント、ディレクトリの在庫全体、およびディレクトリ横断の反復** | **削除 / 短縮 / 不要 / 集約 / 移設 / 維持** | **無駄を削ること —— 経緯・日数経過・言い直しを落とし、守り手のある写しを落とし、重複を1箇所へ寄せる。移設は消せない残りの受け皿** |

No review skill carries a comment lens any more, so this is where the whole subject is answered —
both the comments a change just added and the ones the file was already carrying, judged together as
the body they now form.

All user-visible output — findings, questions, proposed prose, summaries — is written in **Japanese**
（ADR-0701 決定9、`AGENTS.md` *言語*）。
