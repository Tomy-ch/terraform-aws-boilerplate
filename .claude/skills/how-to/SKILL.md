---
name: how-to
description: >-
  Find the sanctioned way to carry out an operational goal in this repository and hand it back as a runnable procedure — prerequisites, the exact commands, how to tell it worked, how to undo it, and what is destructive about it. Use whenever someone wants to DO something and does not know the blessed route: 「リリースブランチはどうやって切る」「保護設定を流し込むには」「この検証はどのコマンド？」「ピンを上げ直したい」. It is goal-driven, which separates it from the symptom-driven `repo-ops`, and it routes to the skill that already owns a procedure rather than re-deriving it. Read-only knowledge — it surfaces the command and warns before anything destructive, and runs it only when explicitly asked to perform the operation. Do NOT use it for a symptom or a failing gate (`repo-ops`), to explain how something works rather than how to do it (`repo-truth`), or to compare undecided options (`research`).
argument-hint: '[goal] [--mode=lookup|run] [--dry-run]'
---

# How To

Answer "what is the sanctioned way to do this here" with a procedure that can actually be run.

## When to Use

- Someone wants to perform an operation and does not know the blessed route.
- Someone has a command in mind and wants to know whether it is the sanctioned one.
- Someone needs the prerequisites, the success signal, or the rollback for a procedure they already
  know the command for.

Do NOT use it for a symptom or a failing gate (`repo-ops`), to explain how something works rather
than how to do it (`repo-truth`), or to compare undecided options (`research`).

## Contract

| | |
| --- | --- |
| **Owns** | 目標 → 正規手順（前提 / 成功判定 / 復旧 / 警告つき）、および所有スキルへの routing |
| **Never** | コマンドの発明 / 所有スキルの手順の再記述 / 破壊的操作の無断実行 / ツールの独断インストール |
| **Starts when** | 実行したい操作があり、正規経路が不明なとき |
| **Stops when** | 索引を通読しても手順が無い（UNDEFINED）、候補が複数で決着しない（AMBIGUOUS）、権限超過（BLOCKED） |

## Why this exists, and why it is not `repo-ops`

The two are different doors, and merging them breaks both.

`repo-ops` is **symptom-driven**: something behaved unexpectedly, you match it against a curated index
of known gotchas, and section N is the fix. Its value is that everything in it was actually hit by
someone. It cannot conclude "no procedure exists", and teaching it to would make its silence
indistinguishable from an answer.

This skill is **goal-driven**: you know what you want to achieve and not how it is done here. That
question has no index. スキル群、`.makefiles/` の各 `.mk`、`.lefthook.yaml`、`.github/workflows/`、
`scripts/README.md` —— **そのどれも「X を行う正規の経路」を横断して並べてはいない**。`tool-map` は
スキルを、`make help` は target を並べるが、どちらも目標には答えない。

The failure this skill is shaped around is specific: **a plausible command is indistinguishable from
a documented one.** `make terraform-test` / `make plan` —— どちらも在って当然に見え、書き下せば
権威に読め、訊いた人はそのまま叩く。**どちらも存在しない**（`AGENTS.md` *現在の配線状態* が、
Terraform 側の検査も plan / apply の経路も未配線だと述べている）。

正規のコマンドを出すか、何も出さないか。**その2つだけが許される結末である。**
`make help` に無いものを書かない —— 存在しない target を書くことは、嘘をつくことと区別できない。

## Arguments, and the door check

| Argument | Effect |
| --- | --- |
| `--mode=lookup` *(default)* | Produce the procedure. Run nothing |
| `--mode=run` | Carry the operation out, confirming each destructive step individually |
| `--dry-run` | Prefer the `DRY_RUN=1` form of every step that offers one |

`--mode` exists for safety, not convenience. Without it, "did they want this run or explained?" is
inferred from phrasing — and the phrasing that means *do it* and the phrasing that means *show me*
differ by a particle. That inference is acceptable for a read; for `make apply-branch-protection`（GitHub の状態を書き換える）や `make branch-major`（タグとデフォルトブランチを動かす）it is not.
Absent the flag, assume `lookup`: showing a command to someone who wanted it run costs one more turn,
and the reverse costs a database.

**Then check you are the right door.** The neighbouring skills take the same nouns and differ only in
what the user is describing:

| The user is describing | Door |
| --- | --- |
| something that broke, or a gate that failed | `repo-ops` |
| how something works, what the rule is, whether it exists | `repo-truth` |
| an operation they want to perform | here |

A symptom arriving here is the case worth catching: assembling a procedure for a system that is
already in a broken state produces steps whose prerequisites do not hold. Say so and point at
`repo-ops` instead of proceeding.

## Step 1 — Route to the owning skill first

Before assembling anything, check whether a skill already owns this procedure. Many do — commit は
`commit`、PR は `submit-pr`、ピンの更新は `actions-pin` / `images-pin`、リポジトリの事実を問うのは
`repo-truth`、未決の選択は `research`。**一覧を本文へ写さない** —— 実行時に `ls .claude/skills/` を
読むこと。写した瞬間に、増減したスキルとずれ始める。

When one does, **say so and stop.** Do not re-derive its steps. A procedure that exists in two places
diverges, and the copy is the one that rots —— 手順に当てはめた **README > Code > SKILL** である。

```bash
ls .claude/skills/                       # what exists
grep -l "<goal keywords>" .claude/skills/*/SKILL.md
```

Run `/tool-map` when the inventory itself is the question.

## Step 2 — Assemble from the registries, by concern

When no skill owns it, the procedure lives in one of these. Read the **index**, not a keyword search:
a target is named for what it does, not for how the goal was phrased.

| Registry | What it holds | Index |
| --- | --- | --- |
| make targets | 正規の操作のほぼすべて | `make help`（**`.makefiles/README.md` は存在しない**） |
| git hooks | commit / push 時に何が走るか | `.lefthook.yaml` |
| CI | PR で何が走るか、どの環境で | `.github/workflows/` と同ディレクトリの `README.md` |
| 運用機構 | 各ツールが何を解いているか | `scripts/README.md` |
| 決定 | なぜその手順なのか | `docs/adr/README.md` |
| 保護設定 | 誰が何を塞いでいるか | `.github/settings/README.md` |

`.claude/skills/repo-ops/SKILL.md` の §0 が「答えたいこと → 読む先」の表を持つ。**実行時に読むこと**
—— ここへ写さない。同スキルの各節は、目標の問いに対しても読む価値がある: 手順に付随する既知の
落とし穴は Warnings に入る。

Keyword search comes last, as a net for what the indexes missed.

**存在の確認は `make help` で行う。** `.mk` を読んで target 名を見つけても、`Makefile` が
その `.mk` を include していなければ実行できない。`make help` は実際に解決される一覧を出す。

## Step 3 — Establish the operational envelope

A command alone is not a procedure. Each of these is a separate question, and each has a source:

- **Prerequisites.** `RUNNER_MODE` はどちらが要るか（ホストの docker や git 認証を使う target は
  `RUN_SCRIPT_HOST` で定義されている）。`gh` の認証、`GITHUB_TOKEN`、特定のブランチ、
  先に走らせる `resolve` —— **target のレシピを読む。名前から推測しない。**
- **Expected result.** How does the caller know it worked — exit status, a file that changes, a
  check that turns green? A procedure whose success cannot be recognized will be declared successful.
- **Recovery.** What undoes it, and is undoing it even possible? Say plainly when it is not.
- **Warnings.** Destructive to shared state, environment-dependent, slow, or touching secrets.

Three properties of this repository make the envelope non-obvious and are worth checking every time:

- **`check` と `apply` の対。** `egress` / `pin-actions` / `pin-images` / `versions` はいずれも
  書き換えない `check` を持つ。**先に `check` を出す** —— 取り消しの効く手順を、効かない手順の
  前に置ける。
- **外へ出る操作。** `apply-branch-protection` は GitHub の状態を、`release` / `repo-setup` は
  タグ・ブランチ・デフォルトブランチを書き換える。いずれもローカルでは取り消せない。
- **トリップワイヤ。** 実環境への apply、履歴の書き換え、保護ブランチへの操作は、
  `AGENTS.md` が手順ではなく**停止**を要求している。手順を組む前にそこへ当たっていないか見る。

## Step 4 — Answer in this contract

Always this shape, in Japanese. Omit a field only when it genuinely does not apply, and say so
rather than dropping it silently.

````markdown
## 状態
FOUND | AMBIGUOUS | UNDEFINED | BLOCKED

## 手順
<何をする手順か、1 行>

## 出典
- `<path>` の `<target / 節 / job 名>`

## 前提条件
- <満たしていなければならないこと、必要な権限>

## コマンド
```bash
<正規に定義されたコマンド。存在するものだけ>
```
<UNDEFINED のときは「なし」と書き、なぜ近そうなコマンドを出さないのかを 1 行添える>

## 近いもの
- `<target / 手順>` — <ただし〜の点で目的が異なる>   ← FOUND 以外のときに書く

## 成功の判定
<どうなれば成功か>

## 復旧 / 切り戻し
<既存資料に定義されたもの。無いなら「定義なし」と書く>

## 注意
- <破壊性 / 環境差 / 共有インフラへの影響 / 未検証の点>

## 探索範囲
<通読したレジストリ / 確認した所有スキル / 検索した glob / 回さなかった掃引とその理由>
← UNDEFINED / AMBIGUOUS のときは必須

## エスカレーション
<未定義・矛盾・権限超過のとき、誰が何を決めれば進むか>
````

The four states are not interchangeable:

| State | Meaning |
| --- | --- |
| `FOUND` | A sanctioned procedure exists and is reproduced above |
| `AMBIGUOUS` | Several candidates exist and the sources do not settle which governs — present all, pick none |
| `UNDEFINED` | The owning indexes were read in full and no sanctioned procedure exists |
| `BLOCKED` | A procedure exists but the requested operation exceeds what was authorized here |

`UNDEFINED` carries the same bar as `repo-truth`'s: it requires the owning registries read **in full**,
and the frontier published with it. Short of that the state is `AMBIGUOUS` or the answer is 確認できず
— naming which index was left unread. Downgrading costs nothing; a wrong `UNDEFINED` reads as a fact
about the repository.

Under `UNDEFINED` the `コマンド` field says **なし**, and says in one line why the obvious-looking
command is not being offered. Leaving the field empty invites the next reader to supply the command
themselves, which is the failure this skill exists to prevent, one step removed. What *does* belong is
the near miss — under `近いもの`, named together with the respect in which its purpose differs. A
reader who is told only "there is no procedure" will go looking, and the nearest thing they find is
the one this skill already examined and rejected.

## Step 5 — Running it

Under `--mode=lookup` this step does not happen: the procedure is the deliverable. Under
`--mode=run`, carry it out — and note that the flag authorizes the *procedure*, not each destructive
step inside it. Those are still confirmed one at a time.

Before anything destructive, say what it will destroy and wait. `AGENTS.md` and `repo-ops` both draw
this line, and shared infrastructure is why: dropping a database, `chown -R` over a tree, or stopping
the shared compose project reaches every other checkout on the machine, and the person who asked was
usually thinking only about their own.

Never install anything on your own initiative. A missing tool is a finding to report; `AGENTS.md`
governs, and this repository usually already ships the capability behind a `make` target.

## Standalone by design

This skill hands over a procedure and stops. It names what to run next — `repo-ops` when the
procedure fails on a known gotcha, `repo-truth` when the goal turned out to be a knowledge question
— and the user decides. 手順が無いと分かったなら、それを issue にする価値があると述べるところまでで、
起票は代わりにやらない。

It also does not absorb the skills it routes to. 所有スキルを名指しすれば、それが答えの全部である。
そのスキルの手順をここへ書き写すと、**このスキルが防ぐために存在している二つ目の写し**ができる。

## Do / Do NOT

- ✅ Treat a missing `--mode` as `lookup`; never infer authorization to run from phrasing.
- ✅ Check for an owning skill first, and stop there when one exists.
- ✅ Read the registries by index, not by keyword.
- ✅ Establish prerequisites, success signal, recovery, and warnings as separate questions.
- ✅ Cite the file and the target / section each step came from.
- ✅ Check shared-infrastructure impact and `DRY_RUN=1` availability every time.
- ✅ Report `UNDEFINED` with the frontier, or downgrade to `AMBIGUOUS` / 確認できず.
- ✅ Under `UNDEFINED`, write `なし` in `コマンド` with the reason, and name the near miss under
  `近いもの` — including the respect in which its purpose differs.
- ✅ Record a sweep you deliberately did **not** run, with its reason.
- ✅ Warn before anything destructive and wait.
- ✅ Answer in Japanese.
- ❌ Invent a command, a flag, or a target that you did not read in a registry.
- ❌ Restate the steps of a skill that owns the procedure.
- ❌ Report `UNDEFINED` from a keyword search or from indexes not read in full.
- ❌ Pick between conflicting sources — present both and escalate.
- ❌ Run a destructive command without saying what it destroys, or any command the user asked only to
  be shown.
- ❌ Install a tool because one is missing.

## Checklist

- [ ] Door check done — a symptom goes to `repo-ops`, a knowledge question to `repo-truth`.
- [ ] `--mode` resolved; absent the flag, treated as `lookup` and nothing was run.
- [ ] Owning skill checked; routed and stopped if one exists.
- [ ] Registries read by index; `repo-ops` section 0 read at runtime; keyword search used last.
- [ ] Prerequisites, expected result, recovery, and warnings each established from a source.
- [ ] Shared-infrastructure impact and `DRY_RUN=1` availability checked.
- [ ] Contract emitted in full, in Japanese, with an explicit state.
- [ ] `UNDEFINED` only on exhausted indexes, with the frontier published in `探索範囲`.
- [ ] Under `UNDEFINED`: `コマンド` says なし with a reason, and `近いもの` names the near miss.
- [ ] Nothing invented; every command traced to the file that defines it.
- [ ] Destructive steps flagged and confirmed before running; nothing run that was only to be shown.
