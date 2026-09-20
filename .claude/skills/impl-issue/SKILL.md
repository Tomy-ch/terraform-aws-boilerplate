---
name: impl-issue
description: >-
  Drive a GitHub issue from environment setup to a merged PR as a semi-automatic pipeline whose stopping points are enumerated rather than judged. Use whenever the user hands over an issue URL or number to be worked end-to-end (「この issue やって」「着手して PR まで」), or asks to resume such a run. It cuts a working branch from the live release line, builds a written plan and holds it for the user's approval before coding, then runs to a merged PR, stopping only where its closed list says and recording every other call for the PR. It owns orchestration and no implementation judgment: the work is delegated to `commit` / `submit-pr`, to the unconditional `settle-comments` pass that ends implementation, and to the peer review skills `impl-review` / `test-review`, and design decisions are surfaced, never taken. Do NOT use it for a change with no issue behind it (`commit` + `submit-pr`), for reviewing an existing diff (`impl-review` / `test-review` / `settle-comments`), or for authoring the skills themselves.
argument-hint: '<issue-url-or-number> [--review-mode=all|harmful|issues] [--issue-mode=search|file] [--flow=record-on-tripwire|halt-on-tripwire] [--plan=full|draft-review|single]'
---

# Impl Issue

Semi-automatic issue → PR pipeline. The machine handles progression, bookkeeping, and detection; the
human keeps every judgment call. A long autonomous run stops being a black box because each departure
from the approved plan surfaces when it happens rather than at the end.

The commands live here so a run is reproducible from this file alone. Detail that drives itself stays
behind pointers: `make help`（実在する target の一覧）、`scripts/README.md`（運用機構の各ツールが
何を解いているか）、`repo-ops`（既知の症状と是正手順）。

## When to Use

- The user hands over an issue URL / number and wants it taken to a merged PR.
- The user asks to resume a run that stopped at a decision point.

Do NOT use it for a change with no issue behind it (`commit` + `submit-pr` directly), for reviewing an
existing diff (`impl-review` / `test-review` / `settle-comments`), or for authoring the skills
themselves.

## Contract

| | |
| --- | --- |
| **Owns** | issue → merged PR のパイプライン進行、承認済み計画と実物の突き合わせ、人間判断が要る瞬間の機械的検出 |
| **Never** | 未決の設計を独自に補完する / 実装判断そのもの（委譲先が持つ） |
| **Starts when** | 採択済みの issue が提示されたとき |
| **Stops when** | 下表の 5 箇所だけ。それ以外では停止せず、判断は PR コメントへ記録する |

## Stopping — the complete list

Where this pipeline stops is a specification, not a judgment. It stops here and nowhere else:

| # | Where | What is decided |
| --- | --- | --- |
| 1 | Step 0 | The four modes, in two back-to-back calls, before anything else |
| 2 | Step 3 | Approval of the written plan |
| 3 | Step 4 | A trip-wire whose row says halt |
| 4 | Step 7 | Which of the two peer review skills to run, each with its estimated return |
| 5 | Step 8 | ゲートが落ちた場合の扱い、および merge そのもの |

**A stop is the end of a turn, not a question.** Read as "asking the user something", the list is
easy to satisfy while breaking it: the failure that actually happens asks nothing. A phase completes,
a progress report is the natural thing to write, and the report ends the turn — no approval was
requested, so the prohibition below never fires, yet the run is over and the rest of the work is back
with the user. Apply the list to turn endings, not only to questions.

Reports are how a long run stays legible; letting one be the last thing in the turn is the failure.
Write it, then keep working in the same turn. 「続けます」 and 「次は〜します」 are evidence of this bug
rather than a plan — you can only write them because you already know the next step, which means
nothing is blocking you. Do that step instead of announcing it.

**Being blocked is not stopping.** A turn also ends where the run has handed off to something outside
itself — CI, a background command, a delegated agent — and nothing independent of that handoff is
left to do. Nobody was asked to decide anything there, and what resumes the run is the notification
rather than an answer; the Merge step is built on exactly this, which is why it waits through a
background command instead of a foreground loop. The test is whether a decision about the work is
sitting with the user: if one is, it is a stop and belongs to the five rows; if none is, the wait is
a block, and the turn ends only when nothing is left that does not wait on the same thing.

Three moments look like stopping points and are not. Each is where an unlisted stop otherwise creeps
in:

- **A phase boundary.** The Step 3 approval covers Steps 4–9, because the plan enumerates the whole
  run and that is what was approved. A phase ending is not an event — and neither is a seam, where
  the run writes its record, recommends compacting, and continues; the PR seam asks first, but what
  it asks about is not the work.
- **A subagent's completion notification.** Reviews and audits fan out; a report arriving is where
  work resumes, not where it pauses.
- **A mode settled in Step 0.** That is spent authority. Re-confirming a fix which review mode already
  authorized asks the user to approve the same thing twice.

Asked one at a time, a stop always looks cheap while its cost is diffuse, so "ask" wins every
individual judgment. That is why the list above is closed rather than advisory.

## What this skill does NOT do

It holds no implementation judgment. It never decides which design to adopt, whether a reviewer is
right, or whether a finding deserves an issue. It routes those to the user and records the answer.

| Work | Owner |
| --- | --- |
| Commit splitting and execution | `commit` |
| Push + PR create/update | `submit-pr` |
| Review of the change itself | `impl-review` |
| Review of the tests | `test-review` |
| The comment stock of the touched declarations | `settle-comments`, unconditionally at the end of Step 4 |
| The implementation itself | you, following the approved plan |

The two review skills are peers: neither invokes the other, and each is asked for separately (Step 7).
`settle-comments` is not among them — it runs unconditionally at the end of Step 4.

## AI Modification Scope

`AGENTS.md` *変更してよい範囲* は既定を `modules/` / `examples/` / `docs/`（Accepted ADR の本文を除く）
に限り、リポジトリ直下の設定ファイル・`.github/`・`LICENSE` を「指示なしに触らない」側に置く。
*v1.0.0 までの暫定* が `LICENSE` を除いてそれを解除しており、**このスキルの起動がその指示に当たる** ——
issue が対象面を決めるのであって、CI・道具・イメージ・文書についての issue が既定の3箇所の中で
解けることはない。

The relaxation is bounded, and the bound is the plan:

- The Step 3 plan's **Files to touch** section is the permitted surface. 既定の3箇所の外にある
  敏感な経路は、編集される**前に**そこへ現れていなければならない —— glob で含意するのではなく、
  名指しで。
- Say so when presenting the plan, so the user approves the sensitive paths knowingly rather than
  discovering them in the diff.
- Reaching a sensitive path the plan does not list halts under either flow mode (trip-wire 1′,
  Step 4). Ask; do not widen the surface and report it afterwards.

**このスキルの実行中も解除されないもの**（issue が何を求めていても触らない）:

- `AGENTS.md` / `LICENSE`
- **Accepted ADR の本文**（ADR-0001 決定2 の immutable。改訂は supersede であり、それは停止点である）
- 生成物 —— terraform-docs の生成区間、`.terraform.lock.hcl`、tfautomv の出力、
  `make egress-apply` / `pin-actions-apply` / `pin-images-apply` / `versions-apply` が書く区間。
  **make ターゲット経由の再生成はよい。手で編集するのが違反である**（*トリップワイヤ* 4）
- `.claude/settings.json` の `permissions.deny` に載っているもの

## Step 0 — Confirm the four modes (two consecutive `AskUserQuestion` calls)

Ask before anything else, in two back-to-back calls: the three run-policy modes, then the plan mode.
Two calls rather than one because they answer different questions — how the run behaves, and what the
planning phase costs — and because a single call caps at four questions, which would leave no room to
ever add a fifth mode. **This is still one stopping point.** The user answers both without the run
doing anything in between; a second dialog is not a second stop.

Defaults are marked; the user's choice always wins.

### First call — run policy

**Review mode** — what happens to a review finding.

| Mode | Confirmed finding | Everything else |
| --- | --- | --- |
| `all` | Apply, even if the change is large | — |
| `harmful` *(default)* | Apply only what is clearly harmful within the change's scope | Route to issue mode |
| `issues` | Apply nothing | Route to issue mode |

**Issue mode** — how an unrelated finding becomes tracked.

| Mode | Behavior |
| --- | --- |
| `search` *(default)* | Search existing issues first; on a duplicate, comment there instead of filing |
| `file` | File without searching |

`issues` × `file` produces the most new issues of any combination. Before executing it, show the count
and confirm — a review easily yields a dozen findings, and a dozen new issues is itself noise.

**Flow mode** — what a trip-wire does. The names carry their trigger because this mode governs
trip-wires only; it is not a posture for the run, and it reaches none of the other four stopping
points.

| Mode | Behavior |
| --- | --- |
| `record-on-tripwire` *(default)* | Record the call and continue; surface every recorded call in one PR comment at the end |
| `halt-on-tripwire` | Stop at that trip-wire and ask |

**Neither mode reaches the trip-wires marked halt in Step 4.** Those are architecture, domain and
policy decisions, which `AGENTS.md` keeps behind a human gate unconditionally. The mode decides only
what happens at the remaining rows: `record-on-tripwire` continues and records them,
`halt-on-tripwire` asks about them too — worth picking when the user is present and wants scope
growth surfaced as it happens rather than at the end.

### Second call — plan mode

**Plan mode** — how much the planning phase spends. Step 3 has three stages; this decides which of
them run. **Every mode satisfies Step 3's invariant** (the plan is seen by a model that is not the
implementer's) — that is not what is being traded away here. What varies is how many passes the plan
gets, and how many of those land on a frontier tier.

| Mode | Stages | Cost |
| --- | --- | --- |
| `full` *(default)* | 3a + 3b + 3c | Three passes — the drafter's, plus two more on a top tier |
| `draft-review` | 3b + 3c | Two passes; the framing stage is skipped |
| `single` | 3b only, drafted by a model that is not the implementer's | One pass |

**Say what the mode costs when you ask, not just what it does.** This is the one mode whose price is
paid every run regardless of the issue's size, so a default that is never priced is a default that
gets paid by accident. A one-line documentation fix and a cross-layer feature do not deserve the same
planning budget, and the user is the only one who knows which this is before Step 3 has read anything.

Recommend `full` when the issue spans layers, changes a contract, or names a design decision;
`draft-review` when the shape is already settled and only the details need working out; `single` when
the change is small enough that the plan is a formality — and say which one you are recommending and
why, rather than presenting three unpriced options.

## The run record — where "record the call" writes to

This pipeline is told to record things: a trip-wire it continued past, a gate it skipped, a reviewer
finding it rejected, a judgment it deferred. Step 9 owes all of them to a PR comment at the end.

**They go in a file, appended as they happen** — beside the plan, under the gitignored `tmp/`, named
for the issue. Not into the conversation.

A long run outlives its own context. Whatever is only remembered gets summarized away somewhere in
the middle, and the failure is silent in both directions: Step 9 still writes a confident PR comment,
and nothing in it says an entry went missing. A file is also what makes the run resumable — a session
that picks this up later can recover which modes were settled and what has fired since, which no
amount of inspecting the branch will tell it.

Append one line per event, each carrying what happened and what was decided:

| Written at | Entry |
| --- | --- |
| Step 0 | the four settled modes |
| Step 3 | the plan file's path, and whether 3a / 3c ran |
| Step 4 | every trip-wire that fired — its number, what triggered it, and the call taken |
| Step 6 | every gate that did not run, and why |
| Step 7 | every finding rejected, or fixed differently than proposed, with the reason |
| Step 8 | what runtime verification covered, and what it did not |

Write the entry when the event happens, not in a batch at the end — a batch is exactly the thing a
compaction eats. Step 9 then builds its comment by reading this file, never by recalling the run.

## Seams — where compacting is cheap

Two points in the run carry almost nothing forward, because everything that matters is already on
disk. They are worth naming, because compaction that happens on its own lands wherever the window
happens to fill, which is routinely mid-implementation.

| Seam | Everything downstream needs | Where it already lives |
| --- | --- | --- |
| After Step 5 (implementation reconciled) | the approved plan, the diff, the calls taken so far | the plan file, `git diff`, the run record |
| After the PR is opened (Step 8, before runtime verification and while CI runs) | the PR, the branch, the calls taken so far | GitHub, `git`, the run record |

At either seam: **write the run record first, then recommend compacting, then keep going.** Recommend
it rather than merely mentioning it — a seam that announces an option nobody acts on saves nothing,
and the record has already made everything downstream recoverable, so there is no case for holding
context here.

**At the PR seam, ask.** Everything after it — `make serve`, the curl transcripts, the traces, the CI
logs — is the heaviest reading left in the run, so this is where compacting pays most and where an
unread notice costs most. The one exception is a run under standing full delegation with the user
away: there, announce it and continue, because a question nobody is present to answer stalls the run
at the moment it was told to finish on its own.

The Step 5 seam recommends without asking. It sits in front of work the run can get on with, so
blocking there buys nothing the PR seam does not already buy better.

**Neither seam is a stopping point, and the list stays at five.** That list enumerates where a
decision about the *work* is the user's to make; compaction decides nothing about the work and the run
proceeds either way. Asking at the PR seam does halt progress until it is answered — say so when the
run is unattended rather than treating the halt as free.

The implementation phase is the one that cannot be delegated to a subagent, so it is where a window
actually fills; the planning stages and the reviews already run as subagents and their windows never
reach this one. That asymmetry is why the seams sit where they do.

## Step 1 — Kickoff

```bash
printf '\033]0;%s\007' "<issue-number>-<slug>"   # label the window so parallel runs stay distinguishable
gh issue view <n> --json number,title,body,labels,state,comments
```

**Compare the issue against the actual base before writing anything.** An issue body is a snapshot of
the repo as it was when someone wrote it; line numbers, "X does not exist yet", and "Y has no consumer"
go stale. Verify each factual claim against the base you are about to branch from.

Then post a kickoff comment recording branch name, base commit, isolation method, and — most
importantly — **every discrepancy found above**. This marks the issue as taken and gets the
corrections to the user while they are still cheap.

```bash
gh issue comment <n> --body-file <file>
```

## Step 2 — Secure the environment

Do this before any code is touched, so nothing lands in a shared checkout. Which half applies depends
on whether the branch exists yet —— 切るのと再開するのは別の操作であり、既に作業が乗っている
ブランチに対して前者を走らせると、その作業を失う。

### 作業ブランチを切る

**このリポジトリに worktree の環に相当する機構は無い。** 共有のインフラも、確保すべき DB の
スロットも無い。要るのは作業ブランチ1本である。

```bash
# 1. 活きたリリース線を origin の実状態から解決する。
BASE=$(make -s base-branch)
test -n "$BASE" || { echo "ベースブランチを解決できませんでした"; exit 1; }

# 2. 古いローカル参照ではなく、いまの origin から切る。
git fetch origin "$BASE"
git switch -c feature/<n>-<slug> "origin/$BASE"
```

`make base-branch` は `origin` の実状態を読む。**他のものを使わない** —— ローカルの
`refs/remotes/origin/HEAD` は clone 時に固定され `git fetch` では更新されず、GitHub の
デフォルトブランチは1つ前のリリース線に留まり、ハーネスが出す "Main branch" の行も同じ
古い symref を報告する。**3つとも警告なしに答える**ので、1世代古い base から切ったブランチは、
皆が在ると思っているファイルが無いと分かるまで正しく見える。

ここで2つの落とし穴がある（`repo-ops` §2・§3）。

- **`git switch -c … origin/release/*` は、新しいブランチの上流を保護ブランチに設定する。**
  この状態で引数無しの `git push` を打つと保護ブランチへ向かう。初回の push は必ず
  `git push -u origin <branch>` で明示する。
- **pre-push の lefthook が `fatal: bad revision '<base>'` で止まることがある。**
  `origin/HEAD` の指す名前のローカル参照が無いときで、`git branch <base> origin/<base>`
  で参照だけ作れば解ける（checkout はしない）。

ユーザーの指示が、解決された線とは別のリリース版を名指ししていたなら、**切る前に訊く** ——
意図的な backport の宛先は、解決器が知りようのない唯一の場合である。

### 既にブランチが在るとき

作業は始まっている。観察して報告する。**やり直さない。**

**何よりも先に run record を読む。** ブランチを見れば作業がどこまで進んだかは分かるが、
**それについて何が決まったか**は分からない —— 確定したモード、発火したトリップワイヤ、
飛ばしたゲートは、あのファイルにしか無い。読まずに再開すると、判断の履歴が空のまま始まり、
Step 9 のコメントからその全部が落ちる。

下のコマンドはどれも読むだけである。

```bash
git rev-parse --abbrev-ref HEAD
git status --porcelain
git log --oneline "origin/$(make -s base-branch)..HEAD"
gh pr view --json number,state,url 2>/dev/null || echo 'PR: なし'
```

## Step 3 — Plan, then wait

**The invariant: the plan is seen by a model that is not the implementer's.** The three stages below
are one default way of satisfying it, not the rule — read the rule off the session's own model rather
than off a model name written here.

No later gate re-opens the plan: `impl-review` / `test-review` both take the finished
change as their subject, so whether the plan solves the issue at all is checked here or nowhere.
Drafting it well and drafting it unbiased are different jobs, so they run as separate stages. **The
three add no stopping point** — the approval at the end is the same single wait.

| Stage | Runs on | Produces | Runs in |
| --- | --- | --- | --- |
| 3a Framing | a tier at or near the top, on a family that is not the implementer's | the questions the plan must answer — nothing else | `full` |
| 3b Research and draft | the strongest tier available for research and drafting, as a subagent | the plan file | every mode |
| 3c Plan review | the same model as 3a; in `single`, the invariant is carried by 3b instead | findings, appended to the plan file as their own section | `full`, `draft-review` |

**Resolve each stage's model at runtime; do not read one off this file.** Model ids here are
`<family><generation>`, and the session states which model is running it, so both facts this section
needs — which family is the implementer's, and which tier is above which — are available when Step 3
executes. A name written into a skill is a snapshot of a roster that changes; the properties above do
not. Under `single`, 3b's model must be a family that is not the implementer's, because it is then
the only pass the plan gets.

### 3a — Framing

Hand it the issue body, the Step 1 issue-vs-base discrepancies, and the paths you have already read.

**It returns open questions, not answers.** It has read almost nothing of the repository at this
point, so anything it asserts is a generality — and a generality handed to a stronger drafter anchors
rather than widens. Ask it what must be decided and where a plan of this shape usually misses
something. Refuse a draft plan, a recommendation, or a
direction if it returns one.

### 3b — Research and draft

Run it as a subagent, so the research happens in a window that carries none of the orchestrator's
accumulated framing. Give it the issue, your Step 1 corrections, the paths you have already read, and
3a's questions. Tell it to verify your summary rather than trust it.

It owes an answer to **every** 3a question — either how it decided, or that the question does not
apply here.

The plan is a written artifact, not a chat message, because Step 5 compares against it mechanically.
Write it under the repo's gitignored `tmp/` (it may be a symlink to a directory outside the repo if
the operator prefers). It must contain:

| Section | Why it is required |
| --- | --- |
| Files to touch | Step 5 diffs this against `git diff --name-only` |
| Per-step deliverables | Lets a partially-finished run be resumed or handed over |
| Chosen options **and rejected ones, with reasons** | Trip-wire 2 fires when a rejected option is later adopted |
| Gate table | Fixes at plan time whether runtime verification is required, so it cannot be quietly dropped |

### 3c — Plan review

Whether it is required is derived, never assumed:

- **3b's model is the implementer's** — the ordinary case, where the strongest tier for drafting is
  also the one running this session. The plan has been seen by no other model yet, so **3c is
  required.**
- **3b's model already differs from the implementer's** — the session runs on a family that is not
  the strongest drafter. The invariant is satisfied the moment 3b finishes, and 3c is optional: run
  it when the change is large enough to be worth a second pass, skip it when it is not.

Append its findings to the plan file as their own section and **present them beside the plan, not
folded into it** — a reviewer that silently rewrote the plan would hide the disagreement at the moment
the user is being asked to approve it. The user arbitrates each finding at the approval below.

### Then wait

Present the plan and **wait for approval. Do not implement before it.**

**That approval covers Steps 4–9.** The plan enumerates the whole run, so no phase inside it needs
approving again; the run continues to the next stopping point on its own.

## Step 4 — Implement, watching five trip-wires

The plan is approved and implementation begins — the boundary between deciding and building, which
only this skill knows:

Follow the approved plan. These triggers are deliberately mechanical — relying on you to *notice* that
a decision was significant is exactly how drift goes unreported.

| # | Trip-wire | Default | Why |
| --- | --- | --- | --- |
| 1 | 計画に無いファイルを触る、**既定の3箇所の中で**（`modules/` / `examples/` / `docs/`） | Record | 面は広がったが、`AGENTS.md` が既に許している範囲である |
| 1′ | 同じことを**その外で**（リポジトリ直下の設定ファイル、`.github/`、`scripts/`、`.makefiles/`、`docker/`） | **Halt** | 計画が許可された面である。広げるのは人の判断 |
| 2 | 計画が退けた選択肢、または第3の選択肢を採る | **Halt** | 退けたことには理由があった。黙って覆すとその理由が捨てられる |
| 3 | lint / CI の失敗が**設計の規則**に根ざしている | **Halt** | 書式ではない。満たすことが設計を変える |
| 4 | レビュアーの指摘を退ける、または提案と違う直し方をする | **Halt** | 指摘が正しくても提案された直し方が有害でありうる。その判断は1人のものではない |
| 5 | ゲートを飛ばす | Record | Step 6 が PR へ書くことを既に要求している |
| **6** | **`AGENTS.md` の*トリップワイヤ*のいずれかに当たる** | **Halt** | 判断が何を言おうと作業を止める。**続行そのものが誤りである** |

行6 が指しているのは8つある。外部 Terraform module への依存、Generic Escape Hatch の作成、
安全側の設定を弱めること、生成物の編集、権威を主張する2つの情報源の食い違い、検査手段を持たない
主張を文書が述べようとすること、履歴の書き換えや保護ブランチへの操作、実環境への直接の apply。
**これは flow mode で緩まない** —— `AGENTS.md` がそう書いている。

**The implementation is not finished until the comment pass has run.** Write the code bare, then invoke
`settle-comments` over the declarations this change touched. `AGENTS.md` puts it here rather than in
Step 7 because the judgment only works once generation has stopped, and it is **unconditional** — its
return is not estimated and the user is not asked whether to run it. Its own questions still apply:
pass the scope (the touched declarations) and the apply mode so it does not re-ask what this run has
already settled.

**Halt rows halt under either flow mode** — they are the human gate `AGENTS.md` places on architecture,
domain and policy decisions. When one fires, present the situation with your recommendation.
`halt-on-tripwire` extends that treatment to the Record rows; `record-on-tripwire` appends them to
the run record and continues, and Step 9 surfaces every recorded call in one PR comment — built by
reading that file, not by recalling the run.

### 生成物が絡むとき

このリポジトリに codegen は無い。代わりに在るのは**宣言 → 生成先**の対で、どれも `apply` と
`check` を持つ。

```bash
make versions-apply    # mise.toml       → scripts/go.mod / docker/tools/Dockerfile
make egress-apply      # .github/egress.toml → workflow の allowed-endpoints
make pin-actions-apply # .github/actions-pin.toml → uses: の @sha
make pin-images-apply  # docker/images-pin.toml   → FROM の @sha256
```

**生成先を手で編集しない**（*トリップワイヤ* 4）。正本を直して `apply` を走らせる。
`apply` の出力を計画の **Files to touch** に含めておくこと —— 含めずに走らせると、
行1′ が発火する。

## Step 5 — Reconcile the plan against reality

Run this before the gates. Compare:

- `git diff --name-only` against the plan's file list — report additions and untouched entries.
- Options actually taken against the plan's chosen/rejected lists.
- Gate table entries against what you actually ran.

Present the deltas. A long run drifts for good reasons; the problem is drift the user never saw. If
nothing drifted, say so in one line and move on.

## Step 6 — Local gates

`make go-fmt`、そのあと `lefthook run pre-commit --force`。これが `.lefthook.yaml` の
`pre-commit` を全部並列で回す —— **手で列挙した一覧に置き換えない**。置き換えると、新しく足された
検査がこの経路から静かに落ちる。

summary の `🥊` は失敗である（バナーの絵文字と同じなので取り違える）。パイプで受けると終了コードが
消えるので、判定は summary の記号か、パイプ無しの終了コードで読む（`repo-ops` §10）。

ローカルで走らせなかったものがあるなら、**PR にそう書く**。沈黙は「検証済み」と読まれる。

Runtime verification is deliberately *not* here. It belongs after the PR exists (Step 8), so CI runs
in parallel with it instead of after it.

## Step 7 — Review

A completed change has two review subjects, each owned by one skill: `impl-review` (the change) and
`test-review` (the tests). They are peers — neither invokes the other — so this step must not silently
pick one. The comment stock is **not** a third subject here: `settle-comments` already ran at Step 4 as
part of implementing, so there is nothing left to estimate.

Follow the Review Phase Protocol in `AGENTS.md`: **estimate each skill's return from the context this
run already holds** — which layers the change touched, whether the tests moved at all, what an
earlier pass already covered — then ask the user per skill, stating that estimate and its reason, and
run what they approve. "Shall I run all three?" is not a question; it hands the cost back unpriced.

This step is where the estimate is cheapest to make: the plan, the diff, and the Step 5 reconciliation
are already in hand.

Handle findings per the review mode from Step 0. Auto-application is confined to what is
machine-checkable — formatting, lint fixes, comment-quality findings, regenerated artifacts. **A fix
that changes the design is always a decision point**, even under review mode `all`: `all` authorizes a
large rewrite, not an unreviewed one.

Then present every recorded trip-wire and deferred judgment together, in one place. Batching beats
trickling: the user sees the shape of the whole run at once.

## Step 8 — PR, then runtime verification, then merge

When this step merges the pull request, stamp it — a merge performed here is observed by nobody
else until the loop goes back to `gh` for it:

Open the PR first via `submit-pr`, so CI starts while you verify locally.

### merge のゲート —— 何が「動いた」の証拠か

**このリポジトリに起動して叩ける API は無い。** `modules/` が空である以上、実行時に確かめられる
振る舞いも無い。だからこの段が見るのは**ゲートが実際に何と言ったか**であり、それ以外ではない。

```bash
gh pr checks <n>
```

必須 context の宣言と、実際に報告された context を突き合わせる —— 差が出たら、その workflow が
存在しないか `on:` のフィルタで起動していない（`repo-ops` §4）。

```bash
gh api "repos/<owner>/<repo>/rules/branches/$(make -s base-branch | sed 's|/|%2F|g')" \
  -q '.[] | select(.type=="required_status_checks") | .parameters.required_status_checks[].context' | sort
```

**緑であることと、見たことは別である。** 検査が「所見なし」と言ったとき、走査件数か終了コードを
確かめる（`repo-ops` §5）。件数を報告しない検査は、それ自体が finding である。

**merge の可否は、実環境への適用の可否と同義である**（ADR-0601 決定10）。検証が失敗している
Pull Request を merge しない。

Terraform 側の検査（TFLint / Conftest / `terraform test` / plan）は**まだ配線されていない**。
`modules/` に実装が入るまで、この段はそれらを「走らせた」と報告しない（`AGENTS.md`
*現在の配線状態*）。

### Merge

Wait for CI without burning the session on a foreground sleep loop — run a background command that
exits once the checks settle, so one notification arrives:

```bash
until [ "$(gh pr checks <n> --json bucket --jq '[.[]|select(.bucket=="pending")]|length')" = "0" ]; do sleep 30; done
gh pr checks <n>
```

Then ask before merging. Row 5 of the stopping list puts the merge itself with the user, so green
checks are the precondition for the question rather than the answer to it: report what CI said and
what runtime verification found, and merge with `gh pr merge <n> --merge` once that is approved.

## Step 9 — Close out

Close the issue **manually** — auto-closing keywords do not fire when the PR targets a release branch
rather than the default branch:

```bash
gh issue comment <n> --body-file <handover> && gh issue close <n>
```

The handover comment covers what was decided, what surprised you relative to the plan, and what was
deliberately left undone.

Route findings that fall outside the change to the tracker per the issue mode. Under `search`, prefer
a follow-up comment on an existing issue over a new one — the issue count is itself a cost, and a
duplicate buries the original.

**Verify a finding against the running system before filing it.** A finding derived purely from
reading code can be wrong in a way static review cannot catch — most often because a layer outside
the one being read (middleware, DI wiring, the database) already handles the case. Step 8's runtime
stage is usually enough to check.

Finally, record in a PR comment any call not already visible in a commit message or the PR
description. **Read the run record and work down it**; every trip-wire recorded rather than halted on
lands here, and the file is the only place they all survive.

## Delegating without double-asking

Sub-skills ask their own questions. Since this skill already settled them with the user, pass the
answers as a payload so the sub-skill skips its own gate.

| Sub-skill | Pass through | Suppresses |
| --- | --- | --- |
| `commit` | The grouping you already presented | Its grouping-approval question |
| `submit-pr` | That a review already ran; the push decision | Its Phase 0 review prompt and push confirmation |
| `impl-review` | Scope, reviewer model | Its Step 0 |
| `test-review` | Scope, reviewer model | Its scope question |
| `settle-comments` (Step 4) | Scope **and apply mode** | Its scope and apply-mode questions |

**Every row is required, because a missing one reinstates a gate this skill already settled.** A
sub-skill whose default is to confirm per item — `settle-comments` is the one to watch — will do exactly
that when its apply mode does not arrive, and the omission is invisible until the questions start.

Asking the user the same thing twice trains them to approve without reading, which defeats the
decision points this skill exists to create.

## Do / Do NOT

- ✅ コードに触る前に作業ブランチを切る。初回の push は明示 refspec で行う。
- ✅ 再開時は run record を先に読み、ブランチ・作業ツリー・PR の状態を観察して報告する。やり直さない。
- ✅ Verify the issue's claims against the actual base, and put the discrepancies in the kickoff comment.
- ✅ Get the plan approved before implementing, and keep it as a file so Step 5 can diff against it.
- ✅ Put the plan through a model that is not the implementer's, and present that review's findings
      beside the plan rather than folded into it.
- ✅ Treat the five trip-wires as mechanical triggers, not as things to notice.
- ✅ Stop only at the five listed places — stopping means ending the turn, not only asking;
      append every other call to the run record as it happens.
- ✅ At a seam, write the record and recommend compacting — asking at the PR seam, announcing at the
  Step 5 one, and announcing at both when the user has delegated and left.
- ✅ Pass every sub-skill its settled answers, apply mode included.
- ✅ Say explicitly which gates ran and which did not.
- ✅ Read the traces, not just the status code.
- ✅ Verify a finding at runtime before filing an issue for it.
- ❌ Merge a change to implementation code that has never been exercised over HTTP.
- ❌ Present green CI as runtime verification.
- ❌ Auto-apply a fix that changes the design, in any mode.
- ❌ Ask for approval at a phase boundary, or treat a subagent's completion as one.
- ❌ End a turn on a progress report. Write the report and continue in the same turn.
- ❌ Let the framing stage return a plan, a recommendation, or a direction instead of questions.
- ❌ Pick which review skills run, or run one on the assumption another chains it.
- ❌ File an issue without checking for an existing one, unless issue mode says to.
- ❌ 保護ブランチへ push する、または素の `git push` に任せる（上流が保護ブランチを指しうる）。
- ❌ セッションが再開しただけでブランチを切り直す。
- ❌ Poll CI in a foreground sleep loop.
- ❌ Carry the run's decisions in context alone, or build Step 9's comment by recalling them.
- ❌ Treat a seam as a stopping point, or wait at one.

## Checklist

- [ ] Four modes confirmed in Step 0's two calls, with plan mode's cost stated when it was asked.
- [ ] Kickoff comment posted, including issue-vs-base discrepancies.
- [ ] Step 2 の正しい側を通った: `make base-branch` が解決した base から fetch して新しい
      ブランチを切った —— または既存のブランチを観察して報告し、切り直していない。
      base を `origin/HEAD` や GitHub のデフォルトブランチから取っていない。
- [ ] Plan built through the stages plan mode selected, every 3a question answered when 3a ran, all
      four sections present, seen by a model that is not the implementer's, and approved before
      implementation.
- [ ] Trip-wires handled per their row's default and the flow mode; nothing silently absorbed, every
      call appended to the run record when it happened.
- [ ] Both seams taken: record written, compaction recommended, and the PR seam asked unless the run
      was unattended under standing delegation.
- [ ] No stop outside the five listed places, and no turn ended at a phase boundary or on a
      progress report.
- [ ] Plan reconciled against the actual diff.
- [ ] Local gates run, or their delegation to CI stated in the PR.
- [ ] The comment pass run unconditionally at the end of Step 4. The two review skills each estimated
      and put to the user; the approved ones run with their
      answers passed through. Auto-application confined to machine-checkable fixes.
- [ ] Decision points presented together.
- [ ] PR opened, then runtime verification (curl + traces) completed — or its absence stated together
      with which of the two options was taken — before merging.
- [ ] Issue closed manually with a handover comment; unrelated findings routed per issue mode, each
      verified before filing; remaining judgment calls recorded in a PR comment.
