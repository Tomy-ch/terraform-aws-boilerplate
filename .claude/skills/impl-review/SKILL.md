---
name: impl-review
description: >-
  Local adversarial, low-bias review of THE CHANGE ITSELF, run on a model different from the implementer's, with lenses for ADR conformance, gate discipline, security, correctness and declaration drift — plus a mechanical stage that actually runs the gates the change touches. Scope (changed files / branch-vs-base diff / specific paths) and reviewer model are confirmed in one question at the start; findings are reported in Japanese and posted to the branch's PR as inline comments by default (`--no-comment` to skip). Read-only on source — every lens reports and the user fixes. Use before commit / PR to get an independent second opinion the implementer's own model would not surface. Its subject is the implementation and nothing else: it carries no test lens and no comment lens and chains no other skill — `/test-review` is the peer asked for separately under the Review Phase Protocol in `AGENTS.md`, and the comment stock is `/settle-comments`, which runs as the last step of implementing rather than as a review. Do NOT use it to review tests (`test-review`) or comments (`settle-comments`).
---

# Impl Review

Independent, adversarial, **different-model** code review you can run locally. The implementer's own model has blind spots; the whole point is to review with another model so those blind spots get caught. Built on the finder → verify pattern, plus a mechanical stage that runs the gates the change touches instead of predicting what they would say.

## When to Use

- Before committing / opening a PR, to get a second opinion the implementer's model would not produce on its own.
- 決定を持つ領域（`docs/adr/**`、`.github/settings/**`、`mise.toml`、`.github/egress.toml`、`*-pin.toml`）に触れたとき。宣言と実体のずれは、テストが通ったままで成立する。
- 検査そのもの（`scripts/**` のツール、`.makefiles/**`、workflow）を足したり直したりしたとき。**ゲートは「何も見ずに緑を報告する」方向へ壊れる**ので、通ったことは働いていることの証拠にならない。

Do NOT use this skill for:

- Style / formatting — `make go-fmt` / `make go-lint`.
- Applying fixes — this skill is read-only on source; it reports, the user fixes.
- Auditing the tests (`/test-review`) — a peer, not a sub-step — or the comments (`/settle-comments`), which the implementation already settled before this review was asked for.

## Contract

| | |
| --- | --- |
| **Owns** | 変更そのもの（adr-conformance / gate-discipline / security / correctness / declaration-drift） |
| **Never** | テスト観点（`test-review`）/ コメント観点（`settle-comments`）/ 他スキルの呼び出し |
| **Starts when** | レビュー可能な差分とその意図が存在するとき |
| **Stops when** | tier 1 の finding が人間の設計判断を要するとき |

## Core Idea — reviewer ≠ implementer

Bias reduction is the design constraint, not a nicety. Reviewers therefore run as **subagents on a different model than whoever wrote the code**:

- The reviewer agents (`adversarial-reviewer`, `review-verifier`) default to **`sonnet`** in their frontmatter, which differs from the usual Opus implementer.
- **The reviewer model is chosen by the user in Step 0.** The options are `fable` (Fable 5) / `sonnet` / `opus` / `haiku`, plus an *auto* default that resolves to a model ≠ the session's implementer. Pass the chosen model to every reviewer subagent via the `Agent` tool's `model` parameter (it takes precedence over the agent file's `sonnet` default) — e.g. `opus` for depth, `haiku` for a cheap divergent pass, `fable` for a fresh independent perspective.
- **The orchestrator MUST guarantee reviewer ≠ implementer.** If the user selects the same model as the session's implementer, warn that it undermines the different-model bias reduction and confirm before proceeding. Never silently let reviewer and implementer be the same model.
- Reviewer subagents are **read-only** (their agent files grant no Edit/Write) — they only return findings, and this skill never mutates source at all. What to change is the user's call, made from the report.

**This skill audits the change and nothing else.** It has no test lens and no comment lens, and it
invokes no other skill. Those are `/test-review`'s and `/settle-comments`'s subjects, each asked for and
run in its own right beside this one, per the Review Phase Protocol in `AGENTS.md`. A review skill
that offers to run the next one makes the subjects stop being independently answerable and lets a
drift in one skill's question silently drop the other two from every flow that went through it.

## Precedence — findings are ranked, not just collected

Reviewers disagree, overlap, and report the same fact in two vocabularies. Without a ranking the
report is a flat list in which a comment nit outranks a wrong aggregate boundary because its finder
called it "high". The tiers in the Step 2 table are that ranking:

| Tier | Lenses | What it decides |
| --- | --- | --- |
| 1 | `adr-conformance`, `adr-placement` | 決定に従っているか、その決定が正しい範囲を拘束しているか —— コードが**何であるべきか** |
| 2 | `gate-discipline`, `security`, `correctness` | それが**働くか** —— とくに、検査が黙って範囲を縮めていないか |
| 3 | `declaration-drift` | 宣言と実態が**離れていないか** |

**A change at a higher tier propagates downward; a lower tier does not, as a rule, act on a higher
one.** ADR に反する実装を直せば、その実装に対して検証した振る舞いは無効になる。命名の指摘が決定を
覆すことはない。 Four consequences follow, and each of them is a rule, not a
suggestion:

1. **Order the report by tier, then by severity within a tier** — never by severity alone. A tier-3
   `high` sits below an `adr-conformance` `medium`, because the ADR finding may delete the code the
   lower one is about.
2. **Mark a lower-tier finding 保留 while a higher-tier finding it depends on is unresolved.** Report
   it, say what it is waiting on, and do not present it as actionable. Re-check it after the
   higher-tier decision lands; it often disappears.
3. **When two tiers report the same fact, keep the higher tier's framing and fold the lower one in as
   corroboration** — one finding, not two. Two entries for one fact reads as two problems and
   double-counts the change's apparent risk.
4. **Agreement among lenses at the same tier raises confidence; agreement from a lower tier does
   not raise a higher finding's severity.** Two tier-2 lenses independently reaching the same defect
   is strong evidence — say so. A tier-3 lens agreeing with a tier-1 finding adds nothing to its
   severity, though it may be cited as support.

**The exception is criticality, and it is yours to notice, not to resolve.** A lower-tier finding
can be the more urgent one — a gate that reports green while inspecting nothing does not wait for an
ADR debate. When a lower-tier finding looks critical enough to outrank the tier above it,
**do not silently reorder: present both and ask the user.** The ranking exists so that ordinary
disagreements resolve without a human; a finding that breaks the ranking is exactly the case a human
should see.

## Step 0 — Confirm Scope

Call `AskUserQuestion` immediately. Default-detect scope by checking branch vs base; if there are unmerged commits, default to "changed files", otherwise "whole working tree / specific paths".

Resolve the base like this — the review has to cover the same diff the pull request shows, so an
existing PR's `baseRefName` wins; with no PR, `make base-branch` resolves the latest release line
(this repo's base is always a `release/*` branch) from `origin`'s live state. Do not fall back to
`gh repo view --json defaultBranchRef`: the GitHub default branch keeps answering with an earlier
release line, which silently widens the diff to a generation of changes nobody asked to review.

```sh
BASE=$(gh pr view --json baseRefName -q '.baseRefName' 2>/dev/null || make -s base-branch)
test -n "$BASE" || { echo "ベースブランチを解決できませんでした"; exit 1; }
git diff --name-only "origin/${BASE}...HEAD"
```

```text
質問: どの範囲をレビューしますか？
選択肢:
  - 変更ファイルのみ（ベースブランチとの diff）  ← 未マージのコミットがある場合の既定
  - 作業ツリーの未コミット変更（git status の差分）
  - 特定のパス/ファイルを指定
  - キャンセル
```

### Reviewer model selection

In the same `AskUserQuestion` call (a second question alongside scope), ask which model the
reviewer subagents run on. `fable` (Fable 5) is available alongside the existing tiers:

```text
質問: レビュアーをどのモデルで実行しますか？（バイアス低減のため 実装者 ≠ レビュアー を推奨）
選択肢:
  - 自動（実装者と異なるモデルを既定選択）  ← 既定
  - fable（Fable 5）
  - sonnet
  - opus（深掘り）
  - haiku（安価・高速な発散パス）
```

*Auto* resolves to the agent-file default (`sonnet`) when the implementer is not `sonnet`,
otherwise to a different tier. If the user picks the implementer's own model, warn (per Core
Idea) that it weakens the different-model guarantee and confirm before continuing. The chosen
model is passed to every `adversarial-reviewer` / `review-verifier` `Agent` call via the
`model` parameter in Step 2 and Step 3.

**Two questions, and no more.** There is no test question and no comment question here. Those
subjects belong to `/test-review` and `/settle-comments`, which the user asks for separately; folding
them in would put a decision about one subject inside a run started for another, and would make this
skill the single point through which the other two are remembered.

### Flags

- `--no-comment` — suppress Step 6 (do not post to the PR); produce the local report only. **Default is opt-out**: when an open PR exists for the current branch, Step 6 posts the surviving findings as inline review comments unless this flag is given.

## Step 1 — Gather Context

- Resolve the base ref and produce the review target: `git diff <base>...HEAD`（未コミットなら `git diff`）、
  および変更ファイル一覧（`git diff --name-only ...`）。
- **触れた区分を判定する。** 区分ごとに走るレンズが変わる。

  | 区分 | パス | 何が壊れ得るか |
  | --- | --- | --- |
  | 決定（root） | `docs/adr/**` | 決定と実装の乖離、supersede の連鎖、番号参照、**ユースケース固有の判断が root へ漏れていないか** |
  | 決定（use-case） | `modules/<use-case>/docs/adr/**` | root ADR の上書き、昇格すべき判断の据え置き |
  | 運用機構 | `scripts/**` | **検査が対象を失ったまま緑を返す** |
  | 宣言 | `mise.toml` / `.github/settings/**` / `.github/egress.toml` / `*-pin.toml` | 宣言と実態のずれ |
  | CI | `.github/workflows/**` / `.github/actions/**` | 起動しない check、権限の過剰、補間の注入 |
  | 実行形態 | `docker/**` / `docker-compose.yaml` / `.makefiles/**` | 版の出所の二重化、検査の定義の重複 |
  | インフラ | `modules/**` / `examples/**` / `*.tf` | 公開契約、最小権限、Policy invariant、テスト層の割当 |

- **宣言を触った変更は、それを読む側をすべて洗う。** 版・パターン・許可リストは複数箇所が読む。
  片方だけが更新された状態は、両方を実行するまで検出できない。

- **どの ADR 集合が支配するかを先に決める。** ADR は2階層である（ADR-0001 決定12-14）。

  ```sh
  # root
  ls docs/adr/*.md
  # 触れたユースケースの use-case ADR（存在すれば）
  ls modules/<use-case>/docs/adr/*.md 2>/dev/null
  ```

  **配置そのものが所有範囲を表す**（決定14）ので、「どこに置かれているか」は読み飛ばす書式ではなく、
  その決定が誰を拘束するかの宣言である。`modules/foo/` を触った変更は、root ADR と
  `modules/foo/docs/adr/` の**両方**に拘束される。`modules/bar/docs/adr/` には拘束されない。

## Step 2 — Fan-out Finders (different model, concurrent)

Spawn all finders concurrently (issue every `Agent` call in a single message). Apply the model rule
from Core Idea — pass the Step 0 user-selected reviewer model to every `Agent` call via the `model`
parameter.

すべて `adversarial-reviewer` を使う（`agentType: "adversarial-reviewer"`、`label` は `find:<lens>`）。

| Finder | Tier | Run when | 見るもの |
| --- | --- | --- | --- |
| `adr-conformance` | 1 | always | 変更が触れた領域を所有する ADR の決定に反していないか。**ADR を読まずに書けるなら、それはこのレンズの対象ではない** |
| `adr-placement` | 1 | `docs/adr/**` または `modules/*/docs/adr/**` を触れたとき | 決定が正しい階層に置かれているか、依存方向が root → use-case を保っているか |
| `gate-discipline` | 2 | `scripts/**` / `.makefiles/**` / `.github/workflows/**` を触れたとき | 検査が**対象を持たないまま成功で返る**経路、握り潰した戻り値、黙って範囲を縮める分岐 |
| `security` | 2 | always | 最小権限、資格情報の経路、秘密の露出、注入 |
| `correctness` | 2 | always | 論理の誤り、境界、エラーの扱い |
| `declaration-drift` | 3 | 宣言を触れたとき | 宣言を読む側がすべて追随しているか。版・パターン・許可リストの二重化 |

各プロンプトには必ず含める: レンズ名とその定義、base ref と変更ファイル一覧と diff、そして
**その領域を所有する ADR へのポインタ**（`docs/adr/README.md` と、該当する ADR のパス）。

### `adr-placement` レンズの定義

ADR-0001 の配置・所有権・依存方向・昇格を見る。**ADR の中身の是非ではなく、その決定が誰を拘束する
ことになっているか**が主題である。

- **配置**（決定12-14）—— repository-wide なら `docs/adr/`、特定ユースケースの内部実装に関する判断なら
  `modules/<use-case>/docs/adr/`。resource 単位の root ADR を新設していないか（決定16）。
- **主語**（決定15）—— 主語が AWS resource になっていないか。「Security Group をどう作るか」ではなく
  「当該ユースケースのネットワーク境界をどう保証するか」であるべき。
- **依存方向**（決定17-19）—— **root ADR が特定ユースケースの実装詳細へ依存していないか。** 例示として
  触れる場合も、その存在を前提とした記述になっていないか。これは `make adr-lint` の検査項目5が
  `modules/<use-case>/` への path 参照を機械的に見るが、**path を書かずに前提だけ持ち込む形は
  機械では捕まらない** —— そこがこのレンズの取り分である。
- **昇格**（決定20-22）—— 2つ以上のユースケースへ同じ原則が当たっているのに use-case ADR のままか。
  逆に、「同じ resource を使っている」「実装が似ている」だけを根拠に root へ上げていないか（決定21）。
  **昇格の判断そのものは停止点である**（`AGENTS.md`）—— finding として挙げ、勝手に決めない。
- **何を ADR にしないか**（決定24-25）—— 守られなくても何も壊れない規定が ADR に入っていないか。
  「その規定が破られたとき、何が壊れるか」を述べられるかで判定する。

### `gate-discipline` レンズの定義

このリポジトリの署名的な故障モードを見るレンズであり、他のリポジトリから持ち込んだものではない。

> ゲートは *inspecting nothing and reporting a clean run* の方向へ壊れる。

具体的に探すもの:

- 検査対象が 0 件のときに成功で返る経路（番兵が無い、または番兵を迂回できる）
- 解釈できない入力を**取りこぼし**として扱う（エラーにしていない）
- 戻り値のエラーを握り潰している
- 抽出を伴う検査で、抽出件数を独立した経路と突き合わせていない
- 検査の定義が2箇所にあり、片方だけが更新され得る
- 抑止に理由と撤回条件が無い、または抑止の単位が広すぎる（規則やスキャナ単位）

**No lens here audits the tests or the comments.** A finding that the change is untested belongs to
`/test-review`, and one about a comment's content belongs to `/settle-comments`. If a lens surfaces
either in passing, say so in the 補足 section as an observation and name the skill that owns it.

## Step 3 — Adversarial Verify

Collect all findings and **dedup** by (file, line, claim) — and when two lenses report the same fact,
apply Precedence rule 4: keep the higher tier's framing, fold the lower one in as corroboration, and
carry ONE finding forward. Textual dedup alone does not catch this: the same defect arrives worded as
an architecture violation and as a type-design suggestion, and shipping both double-counts it. For each surviving finding, spawn one `review-verifier` subagent (concurrently), handing it the single finding + the base ref. Use `agentType: "review-verifier"`, `label` like `verify:<file>`, and the Step 0 user-selected reviewer `model` (same reviewer ≠ implementer rule).

- Keep **CONFIRMED** and **PLAUSIBLE** findings. Drop **REFUTED** (but keep a count for the report).
- For a critical/high finding where a single verdict feels shaky, spawn 2–3 verifiers and go by majority — diversity beats one opinion on the findings that matter.

## Step 4 — Mechanical Verification（ゲートを実際に走らせる）

**レンズの報告より、決定的な検査の出力が上位に立つ。** 触れた区分に対応するゲートを実際に走らせ、
その結果を報告へ載せる。走らせずに「通るはず」と書かない。

| 触れた区分 | 走らせるもの |
| --- | --- |
| `scripts/**` | `make go-test` / `make go-lint` / `make go-fmt-check` |
| `docs/adr/**` / `modules/*/docs/adr/**` | `make adr-lint` |
| `.github/**` | `make actions-lint` / `make zizmor` / `make egress-check` / `make pin-actions-check` |
| `docker/**` | `make docker-lint` / `make trivy-config` |
| `*.md` | `make md-lint` |
| `modules/**` / `*.tf` | `terraform fmt -check` / `terraform validate` / `make trivy-config` |

**走らせる前に `make help` で対象が実在するか確かめる。** この表は現時点の配線であり、`make` が
持っていないターゲットを走らせたつもりで報告しない。Terraform 側の lint（TFLint / Conftest /
`terraform test`）はまだ `make` に配線されていない —— 無いものを「通った」と書くのが、このスキルが
最も避けるべき報告である。

**結果はそれ自身が報告したとおりに載せる。** 行や件数を落とすフィルタ越しに報告しない。
ゲートが落ちた場合、その出力はレンズの findings より先に置く。

## Step 5 — Synthesize Report (Japanese)

Produce one Japanese report:

```text
## ローカルレビュー結果（reviewer: <model> / implementer: <model>）

スコープ: <base>...HEAD（<N> files） / lens: <実際に走らせた lens のみを列挙>
実行したゲート: <走らせた make ターゲットと、それぞれの判定>
未監査の観点: テスト（/test-review）・コメント（/settle-comments）は本スキルの対象外

### CONFIRMED（要対応）
- [重大度] タイトル — path:行
  - 問題 / 根拠 / 修正案
  - 検証: verifier 判定（+ 該当すればゲートの出力）

### PLAUSIBLE（要確認・判断保留）
- ...

### 補足
- REFUTED: <n> 件（finder が挙げたが verifier が否定）
- 走らせなかったゲートと、その理由（触れていない区分 / 未配線）
- 他スキルが所管する観点として気づいた点（あれば。所管スキル名を添える）
```

The `lens:` line lists only the lenses that actually ran.

The **`未監査の観点:` line is mandatory**, and it is not boilerplate: this skill audits one of the
two review subjects and nothing of the comment stock, and a report that says nothing about either
reads as a full review to anyone who did not run them. State plainly that the tests were not looked
at here and that the comment pass belongs to the implementation that preceded this review,
so the omission is visible rather than inferred from a `lens:` list that never mentioned them. Do not
soften it into a recommendation — whether to run the other two is the user's call under the Review
Phase Protocol, and this line only records what this run did not cover.

Order by **tier first, then severity within the tier**, CONFIRMED before PLAUSIBLE (Precedence rule 1).
Mark every finding that is waiting on a higher-tier decision as `保留` and name what it waits on
(rule 2). Always state which gates ran and which did not — silent omission reads as "covered everything" when it was not.

## Step 6 — Post Findings as Inline PR Comments (default; opt out with `--no-comment`)

By default, post the surviving **CONFIRMED + PLAUSIBLE** findings to the branch's PR as **inline review comments** — one per finding, anchored to its `path:line`, instead of a single wall-of-text comment. **Never post REFUTED.** The Step 5 local report is still produced regardless; this step is additive.

Only this skill's own findings are posted. `/test-review` and `/settle-comments` produce their own output for the user to act on, and nothing here reaches into them — posting another skill's findings under this skill's review would make one subject's audit look like it happened inside another's.

Skip this step entirely when:

- invoked with `--no-comment`, OR
- no open PR exists for the current branch (`gh pr view` returns nothing) — keep the local report only and optionally offer to open a PR.

Posting to GitHub is an outward-facing action, so confirm **once** before posting — show the count and the target PR (`AskUserQuestion`: 「<N> 件の指摘を PR #<番号> にインラインコメントとして投稿しますか？」/「投稿する」「投稿しない（ローカルレポートのみ）」).

### Procedure

1. Resolve PR number, repo, and the commit the comments anchor to:

   ```sh
   gh pr view --json number,url -q '.number'        # PR number
   gh repo view --json nameWithOwner -q '.nameWithOwner'
   git rev-parse HEAD                                # anchor SHA
   git rev-parse @{u}                                # pushed head — warn if it differs from HEAD
   ```

   The anchor commit MUST be the commit pushed to the PR. If local `HEAD` ≠ `@{u}`, warn the user to push first (the API rejects comments whose `commit_id` is not on the PR).

2. Decide which findings can be inline. A GitHub inline comment must target a line present in the PR diff. Parse the diff hunks (`gh pr diff <PR> --patch` or `git diff <base>...HEAD`):
   - `(path, line)` inside an added/context hunk → inline comment, `side: "RIGHT"`.
   - `(path, line)` on a removed line → inline comment, `side: "LEFT"`.
   - Off-diff (the reviewer referenced unchanged context) → **cannot** be inline; fold it into the review summary `body`.

3. Build one review and post all comments atomically (a single review, not N standalone comments):

   ```sh
   gh api --method POST repos/<owner>/<repo>/pulls/<PR>/reviews --input payload.json
   ```

   `payload.json`:

   ```json
   {
     "commit_id": "<SHA>",
     "event": "COMMENT",
     "body": "🔎 impl-review (reviewer: <model>) — CONFIRMED <n> / PLAUSIBLE <m>\n\ndiff 外で行アンカー不可の指摘:\n- <path>: <要約>",
     "comments": [
       {
         "path": "<file>",
         "line": <n>,
         "side": "RIGHT",
         "body": "🔎 [CONFIRMED · high] <問題の要約>\n\n根拠: <...>\n修正案: <...>\n検証: <verifier 判定>"
       }
     ]
   }
   ```

   Use `event: "COMMENT"` — this is an advisory review, never `REQUEST_CHANGES` / `APPROVE`. Prefix every comment body with `🔎 impl-review` (or the `🔎 [verdict · severity]` tag) so the posts are distinguishable from human review.

4. Robustness: if the API rejects the batch (422 — a line is not in the diff), move the offending comment(s) to the summary `body` and retry. Report afterward what was posted inline vs. summarized — never silently drop a finding.

## Do / Do NOT

- ✅ Guarantee reviewer model ≠ implementer model (user selects it in Step 0; warn + confirm if they pick the implementer's model).
- ✅ Rank findings by tier (Precedence): order the report by tier, hold lower-tier findings that wait on a higher one, fold duplicate facts into the higher tier's framing, and ask the user when a lower-tier finding looks critical enough to outrank the tier above it.
- ✅ Run finders concurrently (one message, multiple `Agent` calls) via `adversarial-reviewer`, one per lens.
- ✅ Independently verify every finding before reporting; drop REFUTED.
- ✅ 触れた区分に対応するゲートを実際に走らせ、その出力を**それ自身が報告したとおりに**載せる。落ちたゲートは findings より先に置く。
- ✅ State on the `未監査の観点:` line of every report that the tests and the comment stock were not audited here.
- ✅ By default, post the CONFIRMED + PLAUSIBLE findings to the branch's PR as inline review comments (Step 6); suppress with `--no-comment` or when no open PR exists.
- ✅ Confirm once before posting to the PR (outward action); anchor each comment to its `path:line`, fold off-diff findings into the review summary.
- ❌ Post REFUTED findings, or use `REQUEST_CHANGES` / `APPROVE` — the posted review is advisory `COMMENT` only.
- ❌ Mutate source at all — every lens reports, the user fixes.
- ❌ Grow a lens that audits the tests or the comments, or invoke `/test-review` or `/settle-comments` from here.
- ❌ ゲートを走らせずに「通るはず」と書く、またはゲートの判定を行や件数を落とすフィルタ越しに報告する。
- ❌ Order the report by severity alone, report one fact as two findings from two tiers, let a lower-tier lens raise a higher finding's severity, or silently reorder the tiers when a lower finding looks critical — present both and ask.
- ❌ Let a reviewer run on the same model as the implementer.
- ❌ Report speculative style nits as findings, or pad the list to look thorough.

## Checklist

- [ ] Scope confirmed via `AskUserQuestion`; base ref resolved.
- [ ] Reviewer model selected in Step 0 and verified ≠ implementer model (warn + confirm if same).
- [ ] Finders fanned out concurrently via `adversarial-reviewer`（触れた区分に応じて `gate-discipline` / `declaration-drift` を含める）— no test lens, no comment lens.
- [ ] Duplicate facts folded into the higher tier; report ordered by tier then severity; lower-tier findings waiting on a higher one marked `保留`.
- [ ] Every finding independently verified; REFUTED dropped (count kept).
- [ ] 触れた区分のゲートを実際に走らせ、その出力を報告へ載せた（落ちたゲートは findings より先）。
- [ ] No other skill invoked from this run.
- [ ] Single Japanese report: CONFIRMED → PLAUSIBLE, 実行したゲートと走らせなかったゲートを明記, `未監査の観点:` line present.
- [ ] Unless `--no-comment` / no PR: confirmed once, then posted CONFIRMED + PLAUSIBLE as inline PR comments (off-diff → summary body); REFUTED excluded; `event: COMMENT`.
