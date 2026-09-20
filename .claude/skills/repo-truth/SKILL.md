---
name: repo-truth
description: >-
  Answer "how does this repository actually work right now" from its own primary sources, separating what the code and the governing documents state from what you inferred, and naming the gap instead of filling it — including reporting a conflict or an absence as the answer rather than resolving it silently. Use whenever someone asks where something is implemented, what the rule or convention is here, why a design is the way it is, what a term means in this codebase, or whether a procedure exists at all — 「このリポジトリではどうなってる？」「どこで認可してる？」「この規約の正本はどれ？」「そもそも決まってる？」. Reach for it over a plain grep: a document here is named for the concern it owns, so the file that governs a question routinely contains none of its words. Read-only: it never edits, never runs a mutating command, and never repairs the drift it finds. Do NOT use it for a general programming or library question with no repository-specific answer, for a known operational symptom with a documented fix (`repo-ops`), for an undecided design question that needs options compared (`research`), for reviewing a diff (`impl-review` / `test-review` / `settle-comments`).
argument-hint: '[question] [--depth=quick|full] [--kind=fact|rule|rationale|procedure|vocabulary|history]'
---

# Repo Truth

Answer what is true of this repository *right now*, from its own primary sources, with the reasoning
visible.

## When to Use

- Someone asks where something is implemented, or how a path actually behaves.
- Someone asks what the rule, convention, or authoritative document is here.
- Someone asks why a design is the way it is.
- Someone asks whether a procedure, rule, or term exists at all.

Do NOT use it for a general programming question with no repository-specific answer, for a known
operational symptom with a documented fix (`repo-ops`), for an undecided design question that needs
options compared (`research`), for reviewing a diff (`impl-review` / `test-review` /
`settle-comments`).

## Contract

| | |
| --- | --- |
| **Owns** | repo 内の現状の事実回答、事実と推論の分離、未定義 / 確認できず の判定 |
| **Never** | repo 外の一般論を主根拠にした将来設計 / 未決の採択 / 変更 / 見つけた drift の修復 |
| **Starts when** | 現状・規約・根拠・語義・存在有無を問われたとき |
| **Stops when** | 二つの権威が矛盾したとき（提起して停止）、索引を通読しないまま断定を求められたとき |

## Why this exists

Two failure modes produce almost every wrong answer about this repository, and neither is fixed by
reading harder.

**The first is answering from memory.** A layer rule, a make target's behavior, a status code — each
feels recallable and each is decided in a file that changed since. An answer stated in a confident
register is acted on; being approximately right is worse than saying you have not checked.

**The second is reading the wrong copy.** このリポジトリの検索空間には、権威に見えて権威でない
ものが2種類ある。**生成区間** —— terraform-docs が `README.md` の中へ書く区間、`make egress-apply` /
`pin-actions-apply` / `pin-images-apply` が書き込むインラインブロック、`.terraform.lock.hcl` ——
これらは正本ではなく写しで、正本（`.github/egress.toml`、`*-pin.toml`、`mise.toml`）が動いた後に
追随する。もう1つは**supersede された ADR** で、`docs/adr/` に残っていても既に効力が無い。
いずれも tracked なので `.gitignore` を見る検索では外れず、正本と同じ順位で出る。
**正本を特定する作業は、それを読む作業とは別の段である** —— そして答えが正しいかを決めるのは
そちらの段である。

なお訳文ペアはこのリポジトリには無い（ADR-0701 決定7 が禁じており、正本は1ファイルだけである）。

That is why this skill's output separates Evidence from Interpretation. The point is not politeness
about uncertainty; it is that a reader can only overturn a claim whose basis is visible.

## Arguments, and the door check

This skill asks nothing before starting. A modal confirming scope would cost more than most answers
are worth. What varies is expressed as arguments instead, so a chained caller states it rather than
letting this skill infer it:

| Argument | Effect |
| --- | --- |
| `--depth=quick` *(default)* | Skim the owning indexes. **未定義 is unavailable** — an absence can only be reported as 確認できず |
| `--depth=full` | Read the owning indexes in full, which is what earns the right to report 未定義 |
| `--kind=<kind>` | Skip the Step 1 classification; the caller already knows which corpus governs |

`--depth` is the real dial, and it is deliberately the cost of an authoritative absence: reading 110
ADR entries and a README chain is expensive, and nobody should pay it for a question that only needs
a pointer. Escalate to `full` on your own when the answer turns out to hinge on something not
existing — and say that you did.

**Then check you are the right door.** Three skills sit next to each other here and the distinguishing
signal is intent, not vocabulary — the same nouns appear in all three:

| The user is describing | Door |
| --- | --- |
| something that broke, or a gate that failed | `repo-ops` |
| an operation they want to perform | `how-to` |
| a choice nobody has made yet | `research` |
| how something works, what the rule is, whether it exists | here |
| something that could be read as more than one of the above | `question` — the router, which asks |

If this is the wrong door, say so in one line and name the right one. Answering anyway is worse than
mis-triggering, because the answer will be shaped like a knowledge answer for someone who needed a
procedure.

The row that matters most is the last one. A bare 「今」 or 「どうなの」 can mean the world outside, this
repository as it stands, or the diff in this window, and no amount of code reading settles which —
the ambiguity is in the asker. Send it to `question` rather than picking a reading, because picking
one produces a confident answer to a question nobody asked.

**構造の索引を作る道具は、このリポジトリにはまだ入っていない。** 入った場合でも、それは
この段が**使う道具**であって、問いの宛先ではない —— 索引の結果は、証拠と解釈の分離も、
走査した範囲の宣言も、未定義 / 確認できず の区別も運ばないからである。

## Step 1 — Classify what is being asked

The classification decides what counts as an answer, and — more importantly — what an absence
*means*. Do this before searching.

| Kind | What answers it | What an absence means |
| --- | --- | --- |
| Fact — how does it behave | the implementation on the request path | the path does not exist; say so rather than describing a plausible one |
| Rule — what should be done here | the governing document | **the rule may be undefined** — a finding in its own right, but only once Step 2 has exhausted the owning index |
| Rationale — why is it this way | `docs/adr/`（root）と `modules/<use-case>/docs/adr/`（当該ユースケース） | the decision was made implicitly and never recorded |
| Procedure — how is it done here | `.makefiles/**`, `.lefthook.yaml`, `.github/workflows/`, `scripts/README.md` | **no canonical procedure may exist** — same bar; never invent a command to close the gap |
| Contract — what does this module promise | `modules/<use-case>/README.md` の supported / unsupported と公開 `variable` / `output` | 責務外と判断されたか、まだ書かれていない（ADR-0102 決定5・7） |
| History — when and why did it change | `git log`, merged PRs | — |

A question often carries an unstated assumption about which kind it is. "How do I run only the
integration tests?" is a Procedure question; "why do integration tests need a DB slot?" is a
Rationale question, and they resolve in different corpora.

## Step 2 — Establish the search frontier, then locate the source

This is the step the skill exists for, and the one that decides whether the answer is trustworthy.

**You cannot conclude anything from a file you never opened, and this corpus is too large to have
opened it by accident.** 518 canonical Markdown files (after excluding 495 `*.ja.md` mirrors and 144
generated copies), 110 ADRs, 150 package READMEs, 52 `.mk` files — plus the code. Whatever you read,
most of it stayed unread, so *absence of a hit is not absence of an answer*.

Grep does not rescue this, and `AGENTS.md` says why in as many words: **a document is named for the
concern it owns**, so searching an index for your feature's words is not enough. The file that
governs your question is routinely one whose name does not contain any word in it.

### Search index-first, by concern

Read the indexes and pick entries by *what concern they own*, not by keyword match:

| Index | Covers |
| --- | --- |
| `docs/adr/README.md` | 受理されたすべての決定、1行ずつ —— **ADR の一覧が存在する唯一の場所** |
| `AGENTS.md` の *正典となる文書* | どの問いをどの文書が所有するか。索引の索引である |
| `scripts/README.md` | 運用機構の各ツールが何を解いているか、および Test Strategy |
| `.github/workflows/README.md` | workflow の規則、必須検査の考え方、結果コメント |
| `make help` | 実在する make ターゲットの一覧。`.makefiles/` に README は無い |
| the README chain from the path in question up to its nearest ancestor | 責務境界、supported / unsupported、設計意図 |
| `modules/<use-case>/README.md` と `modules/<use-case>/docs/adr/` | 当該ユースケースの公開契約と固有の判断（**未作成**） |

**索引を機能名で検索するだけでは足りない。** `AGENTS.md` がそう述べている —— 文書は、それが所有する
関心事の名前で置かれているのであって、探し物の名前では置かれていない。

Keyword search comes **last**, as a net for what the indexes missed — never as the primary method.

### Record the frontier before concluding

Keep track of what was actually covered: which indexes were read in full, which README chain was
walked, which globs were searched — **and which sweeps you decided not to run, with the reason**.
A frontier listing only what was covered reads identically whether the rest was ruled out or
forgotten, and only one of those is a finding about the repository. This is not bookkeeping — it is what makes an absence falsifiable,
and it draws a line this skill must not blur:

| Verdict | Requires |
| --- | --- |
| **未定義** — no rule / procedure exists | the owning indexes read **in full**, and the README chain walked |
| **確認できず** — could not establish it | anything less |

Downgrading to 確認できず is always available and costs nothing. Reporting 未定義 off a partial sweep
is worse than having no answer, because it reads as a settled fact and the next person builds on it.

Two things `repo-ops` section 0 establishes that this skill must not soften:

- **Precedence when sources disagree** は `AGENTS.md` *指示の優先順位* が持つ ——
  `AGENTS.md` → root の Accepted ADR → `modules/<use-case>/` の Accepted ADR → ユーザーの指示。
  設計意図と実装方針については **README > Code > SKILL**。
- **権威を主張する2つの情報源が食い違っていたら、そこで止まる。** 気づくことが仕事であり、
  解決することは仕事ではない（`AGENTS.md` *トリップワイヤ* 5）。

### Use the graph where structure beats text

Keyword search fails on exactly the questions this section opened with — a governing document named
for its concern, a caller that shares no vocabulary with the callee. Graphify indexes **structure**,
so it reaches what text search cannot. Use it; do not merely guard against it.

**構造の索引はこのリポジトリにまだ入っていない。** 入るまでの代替は、Go の道具で構造を直接引く
ことである —— `go -C scripts list ./...` で区分を並べ、`grep -rn "<symbol>" scripts/` で呼び出し側を
数え、数えた範囲を答えに添える。「X だけがこれをやっている」という主張は、**どこまで数えたかを
言わない限り成立しない**。

索引が入ったときのために、そのときも動かない原則を先に置く:

- **A node, an edge, or a generated summary is never the evidence.** It is how you reached the file;
  open that file and cite it. A graph answer that was not confirmed in source stays Interpretation.
- **State freshness whenever you used it.** 索引がどの commit のものかを言うこと。未 commit の
  作業についての問いに対して、索引は目が見えない —— 作り直すか `grep` を使う。小さな差分なら
  後者のほうが安い。
- **Its absence is normal.** It is gitignored and installed per machine, so fall back to index
  reading, search, `git log`, and direct reading — and record the frontier accordingly, because a
  structural sweep you could not run is part of what was left uncovered.

## Step 3 — Read the primary sources, and mark the seam

Open what you cite. A path you did not read is not evidence.

As you go, keep two piles apart, because they get merged the moment they are written into one
paragraph:

- **Evidence** — a sentence the source actually contains, or behavior the code actually expresses.
- **Interpretation** — anything you concluded by combining sources, by absence, or by analogy with a
  sibling. Inference is legitimate and often the whole value of the answer. Presenting it in the same
  register as evidence is not.

The most common leak is a claim of *scope*: "only X does this" is a claim about absence, and absence
is established by an exhaustive search or not at all. Reverse-traverse the graph
(`node .claude/scripts/graph-affected.ts <symbol> --depth 2`) rather than guessing at call sites; if
neither that nor an exhaustive search was run, narrow the claim to what was actually covered.

## Step 4 — Check currency and conflict

- **Currency.** A governing file may have moved this week. Check history for the paths you are
  citing (`git log --oneline -10 -- <paths>`) when the answer depends on it being current.
- **Conflict.** When two sources that both claim authority disagree, report both with their
  freshness — do not silently pick the one that answers the question. `AGENTS.md`'s *Conflicting
  Authority* section governs this, and it is deliberate: noticing a disagreement is the job,
  resolving one is not.
- **Drift is a finding, not a task.** When code and its README disagree, the README is the governing
  side and the code is the drift; say so and stop. 直すのはこのスキルの仕事ではなく、
  "correcting" a governing document to match the code is not this skill's call at all.

## Step 5 — Answer in this contract

Always this shape, in Japanese. Keep the Answer short enough to be read first.

```markdown
## 回答
<結論を 1〜3 文で>

## 根拠
- <主張> — `<path>` の `<symbol / target / 節>`
- <主張> — `<path>` の `<symbol / target / 節>`

## 推論
- <根拠から導いたこと。断定と区別できる書き方で>

## 矛盾 / 欠落
- <食い違う出典と、それぞれの鮮度> / <未定義 または 確認できず> / <古い可能性のある記述>
- 探索範囲: <通読した索引 / 辿った README 連鎖 / 検索した glob / 回さなかった掃引とその理由>
  ← 欠落を報告するときは必須

## 確度
High | Medium | Low — <そう判断した理由>

## 次にできること
<追加確認 / research / ADR 化 / issue 化 など。実行はしない>
```

Cite **symbols and paths, never line numbers** — a line number is stale by the next refactor, and a
reader who follows one to the wrong place trusts what they find there.

Confidence is judged on the sources, not on how sure you feel:

| Level | When |
| --- | --- |
| High | Multiple current primary sources agree, and they govern the question asked |
| Medium | A single primary source, an implicit one, or one whose currency is unverified |
| Low | Mostly inference, sources conflict, or the deciding source could not be reached |

When only part of the question could be answered, return the verified part and name the rest as a
gap. A precise partial answer beats a complete-looking one.

## Gaps are an answer

"There is no canonical procedure for this" is a finding this skill owns, and it exists because the
alternative is worse: an invented-but-plausible command reads exactly like a documented one, and the
next person runs it.

State it as a result, and publish the frontier with it, so the absence is falsifiable:

> 正規手順は**未定義**。`make help`（全 target）と `scripts/README.md` を通読し、
> `.lefthook.yaml` / `.github/workflows/` を確認したが該当なし。近いのは `<target>`（ただし〜の点で
> 目的が異なる）。

If the indexes were not read in full, the verdict is **確認できず**, and it says which index was left
unread. The two are not interchangeable: one is a finding about the repository, the other is a
finding about how far this run got.

`repo-ops` deliberately does not carry either verdict — it is a lookup table of known symptoms, and
teaching it to conclude "undefined" would turn it into a general search skill. When a symptom is not
in its index, the question comes here.

## Standalone by design

This skill is invoked in its own right and chains into nothing. It reports what `Next action` would
be — `research` for an undecided design question, `repo-ops` for a known symptom — and the user
decides whether to run it.

That is the same reason the review skills are peers under the Review Phase Protocol in
`AGENTS.md`: a skill that runs the next one for you removes that decision from the user, and a drift
in this skill's judgment would then silently redirect every flow that passed through it.

## Do / Do NOT

- ✅ Say so and redirect when this is the wrong door, instead of answering anyway.
- ✅ Search index-first by concern; keep keyword search as the last net, never the first move.
- ✅ Record the frontier — indexes read in full, README chain walked, globs searched.
- ✅ Say 確認できず whenever the owning indexes were not exhausted; reserve 未定義 for when they were.
- ✅ Open every source you cite; cite symbols and paths.
- ✅ Keep Evidence and Interpretation in separate sections.
- ✅ Report a conflict with both sources and their freshness.
- ✅ Report an absence as the answer, with what was searched.
- ✅ Reach for the graph where structure beats text — `affected` for callers and scope claims — then
  confirm what it pointed at in source, and state its freshness.
- ✅ Record a sweep you deliberately did **not** run, with its reason. A frontier that lists only what
  was covered cannot be told apart from one where the rest was forgotten.
- ✅ Answer in Japanese.
- ❌ Answer from memory about anything the repository decides.
- ❌ 生成された写しを権威として引く —— terraform-docs の生成区間、`make *-apply` が書いた
  インラインブロック、`.terraform.lock.hcl`、supersede 済みの ADR。
- ❌ Treat a skill body as authority over a README, or a code fact as authority over a governing
  document.
- ❌ Resolve a conflict between two authorities on your own.
- ❌ Report 未定義 from a keyword search, or from indexes that were not read in full.
- ❌ Invent a command, a rule, or a rationale to close a gap.
- ❌ Edit anything, run a mutating command, or repair the drift you found.
- ❌ Claim a scope ("only X does this") from a partial search.
- ❌ Write line numbers into the answer.

## Checklist

- [ ] Door check done — a symptom goes to `repo-ops`, an operation to `how-to`.
- [ ] `--depth` resolved; 未定義 claimed only under `full`, and any escalation to `full` stated.
- [ ] Question classified; what an absence would mean is settled before searching.
- [ ] Indexes read by concern before any keyword search; `repo-ops` section 0 read at runtime.
- [ ] Frontier recorded — which indexes in full, which README chain, which globs.
- [ ] No `*.ja.md` read; generated trees excluded from search.
- [ ] Every cited source actually opened; symbols and paths, no line numbers.
- [ ] Evidence and Interpretation separated; scope claims backed by exhaustive search.
- [ ] Currency checked where the answer depends on it; conflicts reported with both sources.
- [ ] Graph accounted for either way — used where structure beats text (`affected` for any scope
      claim) with its output confirmed in source and its freshness stated, or deliberately not run
      with that stated in the frontier alongside the reason.
- [ ] Contract emitted in full, in Japanese, with Confidence and its reason.
- [ ] 未定義 used only on exhausted indexes; otherwise 確認できず, naming what was left unread.
- [ ] Gaps stated as results with the frontier attached; nothing invented to fill one.
- [ ] Nothing edited, nothing run that mutates, no chained skill invoked.
