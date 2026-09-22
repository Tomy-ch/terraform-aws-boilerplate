# scripts

`scripts/` は、このリポジトリ自身を検査・操作する**リポジトリ運用機構**を置く場所です
（[ADR-0702 (repository-operations-substrate)](../docs/adr/0702-repository-operations-substrate.md)）。
対象は Terraform コードの正しさではなく、リポジトリの構造・CI 定義・依存の固定・ブランチ運用・
保護設定・outbound 制限です。

**ここにあるものは `terraform` の実行経路に入りません**（ADR-0702 決定3）。`terraform fmt` /
`validate` / `test` / `plan` / `apply` は、この配下が1つも動かない状態で成功しなければなりません。
障害対応で必要になるのは apply であって lint だからです。ゲートとして CI の必須検査に入れることは
妨げません（決定5）——禁じているのは実行経路への挿入であって、ゲートとしての採用ではありません。

Go モジュールはこのディレクトリに根を持ちます（`scripts/go.mod`、module path は
`github.com/Tomy-ch/terraform-aws-boilerplate/scripts`）。言語に Go を採ったのは、ADR-0501 決定32 が
Terratest を次段階の導入対象としており、**Go はいずれこのリポジトリへ入るから**です（ADR-0702 決定6-7）。

*Script Categories* は現時点の一覧であって、定義ではありません。増えるものとして読んでください。

## Directory Structure

1ツール1ディレクトリ、名前はそれが何をするかです。**1つの決定を2つ以上が必要とするなら `lib/` が持ちます** —— 文書が所有する規則（フェンス長、lockfile の書式）や、複数のツールが同じ契約を共有する箇所です。言語の定型句（map のキーを並べる、相対パスを取る）は、同じ形をしていても各ツールに置きます（[0205](../docs/adr/0205-module-dependency-policy.md) 決定11）。
名前が運べない「何のためにあるか」は *Script Categories* が持ちます。

## Script Categories

### Workflow / Actions の検査

`actionlint` と `zizmor` が見るのは workflow の構文と template injection です。以下は、
それらが構造上表現できない規則だけを持ちます（ADR-0702 決定8-9：既製品で足りるものを自前実装しない）。

|Script|Description|Invoked By|
|---|---|---|
|`actions-shellcheck/`|Parse every `action.yaml` / `action.yml` under `.github/actions/**`, extract `runs.steps[].run` from the composite ones and check each script with `shellcheck` over stdin, remapping every finding back to its line in the `action.yaml`. Fills the gap that `actionlint` walks only `.github/workflows` and cannot be pointed at an action manifest (handed one directly, it parses it as a workflow and fails), so the shell inside a composite action was checked by nothing. The dialect comes from the step's `shell:` — passed to shellcheck as a shebang, which also settles the target shell without a `-s` flag; `pwsh` / `python` / `cmd` and an expression-valued `shell:` are counted as skipped instead. `${{ }}` expressions are masked to a placeholder that preserves the line count, the same approach `actionlint` takes for workflow `run:`. Per file, the number of extracted steps must equal the number a plain decode of the same YAML counts, and a mismatch exits non-zero — the two routes break independently, so a broken extractor cannot pass as a clean run; a `run:` written as a folded scalar (`>`) is rejected outright, because folding drops the line breaks a finding's position is mapped back through. Masking is also the reason this script says nothing about whether an expression was quoted — that question survives the mask only for a checker that reads the interpolation site itself, which is `make zizmor`'s job.|`make actions-lint` / `make actions-shellcheck`|
|`shell-lint/`|リポジトリ内の `*.sh` を `shellcheck` で検査する。composite action の中のシェルは `actions-shellcheck` が見るが、**ファイルとして置かれたシェルはどのゲートにも掛かっていなかった**。フックやセットアップの入口はそこに居る —— 編集のたび、あるいはセッションのたびに走るのに、壊れても CI は緑を返す。内容をそのまま渡すので、指摘の行・列は写し戻さずに使える（起動の詳細は `lib/shellcheck`）。走査対象が0件なら非ゼロで終わる。|`make actions-lint` / `make shell-lint`|
|`pr-comment-secret-lint/`|Split every workflow in `.github/workflows/` into jobs and fail when a job using `./.github/actions/upsert-pr-comment` references a secret other than `GITHUB_TOKEN`, workflow-wide `env:` included. Enforces a rule `actionlint` cannot express — see [`.github/workflows/README.md`](../.github/workflows/README.md) for why the rule exists. Reach: direct `secrets` references inside a `${{ }}` expression, whether `secrets.NAME`, `secrets['NAME']`, or the whole context (`toJSON(secrets)`); a secret read in one job and handed on through `needs.<job>.outputs` is beyond static reach and passes.|`make actions-lint` / `make pr-comment-secret-lint`|
|`pr-comment-fence-lint/`|Fail when a workflow's `run:` block emits a fixed-length Markdown fence around a PR comment body, and when a workflow that passes a body through interpolates a value into an inline code span. Enforces rules `actionlint` cannot express — see [`.github/workflows/README.md`](../.github/workflows/README.md) for why a fence must be sized from the text it wraps. Reach: literal fences in an `echo`, and a span written literally around a shell expansion — one built through a variable or assembled by `jq` is invisible here, and whether a given body is attacker-controlled is not decidable at all; both are left to the rule. The span check is file-scoped and keeps an exclusion map for a workflow whose body is not yet on a safe path: an entry names the issue tracking it, is printed on every run so a skipped file cannot pass for a checked one, and goes away when that issue is fixed.|`make actions-lint` / `make pr-comment-fence-lint`|
|`actions-cutoff-lint/`|Fail when a job carries no `timeout-minutes`, and when a step calling `./.github/actions/upsert-pr-comment` has an `if:` a cancelled job cannot reach or a `title:` with no cut-off heading. Enforces rules `actionlint` cannot express — see [`.github/workflows/README.md`](../.github/workflows/README.md) for what a cut-off has to leave behind and why the three are one check. Reach: `always()` / `cancelled()` in the condition, `failure()` deliberately not counting since it is false for a cancelled job; the literal `CUT OFF` in the title expression; jobs calling a reusable workflow are skipped because the key is invalid there. Structure is read by column rather than by a YAML parser, which holds because a block scalar's body is always more indented than its key — `actionlint` runs first in the same target and guarantees the input parses at all. A condition that negates its own reachability (`!always()`) is writable and not statically caught — the rule is what holds.|`make actions-lint` / `make actions-cutoff-lint`|
|`required-check-lint/`|保護設定が required とした context を報告する job が実在し、その workflow の起動条件が check を取りこぼさない形であることを検査する。`pull_request` に paths / branches のフィルタを置くと、除外された Pull Request では run が1件も起動せず、GitHub は報告されなかった context を「該当しない」とは解釈せずに待ち続ける —— **その Pull Request は恒久的にマージ可能にならない**。起動条件は `on:` から外し、job の `if:` で表現する（skip された job は skipped を報告し、required check はそれを成功として数える）。検査の入力は `.github/settings/branch-protection.json` そのもの。検査が独自の一覧を持つと、宣言を変えたときに片方だけが動く。|`make actions-lint` / `make required-check-lint`|
|`actions-mise-pin-lint/`|CI が mise 本体を導入する composite action の3点——`MISE_VERSION`（入れる版）/ `MISE_SHA256`（その版の digest）/ キャッシュキー（復元したバイナリをどの版・どの digest として扱うか）——が一致していることを検査する。版だけを上げて digest を据え置くと、キャッシュは古いバイナリを新しい版として返し続ける。キーが版と digest の両方を含んでいれば、どちらが動いてもキーが変わり、復元は外れる。3点のどれかが読み取れない時点で落とす —— 検証があるように見えて効いていない状態を許さない。|`make actions-lint` / `make actions-mise-pin-lint`|

### 決定の検査

|Script|Description|Invoked By|
|---|---|---|
|`adr-lint/`|ADR の構造を検査する。[ADR-0001 (adr-process-and-placement)](../docs/adr/0001-adr-process-and-placement.md) が自らの検証方法として挙げた項目を、そのまま実行するのがこのツール —— **規範を定めた ADR が自分の検査手段を持たない状態は、ADR-0401 決定3（検証できない主張を保証として扱わない）に自ら反する**。見るもの: ファイル名が `NNNN-kebab-case-title.md` に適合すること / 同一 scope 内で番号が重複しないこと / 先頭メタデータに Status・Date・Scope があること / `Superseded by` の参照先が実在すること / root ADR 本文が `modules/<use-case>/` 配下の path へ規範的に依存していないこと / 本文が参照する `ADR-NNNN` がすべて実在すること。最後の1つが要るのは、番号が identity ではなく順序になった（ADR-0001 決定5-9）ことから —— 番号は削除に伴って詰められるため、**参照が黙って別の決定を指す**経路が開く。併せて索引（`docs/adr/README.md`）が実ファイルと食い違っていないことも見る。索引は ADR の一覧が存在する唯一の場所で、そこがずれると読み手は決定へ到達できない。|`make adr-lint`|

### カバレッジのゲート

|Script|Description|Invoked By|
|---|---|---|
|`cover-gate/`|`go test -coverprofile` が書いたプロファイルの総カバレッジを下限と比べ、割っていれば非ゼロで終わる。総カバレッジは `go tool cover -func` の `total:` 行から取る —— プロファイルの集計規則（`-covermode` ごとの重み付け）を写し取らずに済ませるためで、ここが持つのは「取り出す」ことと「比較する」ことだけ。**`go tool cover` はプロファイルが記録する module 相対の package path を解決するために `go.mod` を要るので、起動ディレクトリを `-module`（既定 `scripts`）で固定する** —— リポジトリ直下から起動すると `go.mod file not found` で落ち、それは**カバレッジ不足ではなく道具の失敗**である。判定を awk のパイプラインで書くと、数値でないパーセンテージが `t+0` で 0 へ強制され、壊れたプロファイルがカバレッジ不足として報告される。|`make cover-gate`|

### 宣言の固定と drift 検出

宣言を1箇所へ置き、`apply` がそれを各所へ書き込み、`check` が drift で落ちる形です
（ADR-0702 決定10-12）。**`check` は `apply` と同じ判定を書き換えなしで実行します**——別実装にすると、
検査を通るが適用で壊れる状態、およびその逆が生まれます。

|Script|Description|Invoked By|
|---|---|---|
|`pin-actions/`|Pin every external GitHub Actions `uses:` in `.github/workflows/**` and `.github/actions/**` to an immutable commit SHA. `resolve` walks the references and resolves each tag/branch to a SHA via `git ls-remote`, writing the lockfile `.github/actions-pin.toml`（宣言の正本） — with a supply-chain quarantine that refuses commits younger than `PIN_ACTIONS_MIN_AGE_DAYS` (default 14, keeping the existing pin instead). `apply` rewrites each `uses:` to `@<sha> # <tag>` from the lockfile. `check` runs the same comparison without writing and exits non-zero on any unpinned/stale/unregistered reference (for CI / hooks). Idempotent: an already-pinned line re-resolves off its trailing `# <tag>` comment.|`make pin-actions-resolve` / `pin-actions-apply` / `pin-actions-check`|
|`pin-images/`|Pin every `FROM` base image in `docker/*/Dockerfile` to an immutable digest. `resolve` collects each `image:tag` and resolves its current digest via `docker buildx imagetools inspect`, writing the lockfile `docker/images-pin.toml` (SSOT) — with a supply-chain cooldown that refuses digests whose image-config `created` is younger than `PIN_IMAGES_MIN_AGE_DAYS` (default 14). A mutable tag has no queryable history, so the step-back target is the tool's own prior lock entry; with none (bootstrap) the image is left tag-only. `apply` normalizes each `FROM` to `image:tag@sha256:...` from the lockfile, leaving a tag-only line where the lockfile carries no entry (which is how a quarantined image stays unpinned). `check` runs the same comparison without writing and exits non-zero on drift (for CI / hooks). The tag stays inline as the version SSOT.|`make pin-images-resolve` / `pin-images-apply` / `pin-images-check`|
|`egress/`|Generate every job's inline harden-runner `allowed-endpoints` from `.github/egress.toml` (SSOT), where a job declares the capability classes it belongs to (`base` / `mise` / `terraform` / `image`) plus its own `extra`, and the class definitions hold the hosts. `apply` rewrites the folded block of every `allowed-endpoints:` in `.github/workflows/*.yaml`; `check` runs the same comparison without writing and exits non-zero on drift (for CI / hooks). Fails closed rather than silently: a job whose block is missing from the SSOT, an SSOT entry no workflow claims, an `egress-policy` that disagrees with the SSOT, and a non-host line inside a block are all errors. The step must stay inline (harden-runner runs before checkout, so a composite action cannot hold it), so the SSOT is what removes the duplication instead — see [`.github/workflows/README.md`](../.github/workflows/README.md) § Runner Hardening.|`make egress-apply` / `egress-check`|

### 供給網のクールダウン

|Script|Description|Invoked By|
|---|---|---|
|`go-cooldown/`|Check `go.mod` against the supply-chain cooldown window using the publish time the Go module proxy reports (`<module>/@v/<version>.info`), so no extra dependency is needed. `gate` compares against a base ref and fails on a **direct** requirement the change adds or upgrades that was published inside the window; an indirect one is reported instead, since MVS can hold it above a direct dependency's lower bound where the pull request cannot lower it. `audit` inventories every requirement and never fails on the window itself, because existing dependencies are grandfathered. Both fail on a bypass entry in `.github/go-cooldown-bypass.toml` that has expired, reaches beyond three months, or matches nothing in `go.mod`, and an invalid entry loses its effect so a lapsed bypass cannot keep letting its module through. Unlike pnpm's `minimumReleaseAge`, Go enforces no window at resolution time — this check is the guard, not a detector for one.|`make go-cooldown-gate BASE=<ref>` / `make go-cooldown-audit`|
|`tool-cooldown/`|`mise.toml` が宣言するツールの版を、供給網のクールダウン窓に対して検査する。窓はツールではなく**バックエンド**が決める: GitHub Release 経由（aqua / ubi / github）で解決するものは14日で、タグは別のコミットへ付け替えられるため `pin-actions` / `pin-images` と揃える。パッケージレジストリ経由（go / npm）は7日で、公開された版が不変であるため `go-cooldown` と揃える。lockfile そのもの（推移的依存）は対象外で、`go-cooldown` が直接要求だけを見るのと同じ理由による。公開時刻は GitHub Releases API / Go module proxy / npm registry から取る。**Release を出さず tag だけを打つ上流**では、aqua の定義（`type: http`）が指す配布物へ HEAD を投げ、配布元が付けた `Last-Modified` を採る —— tag の日付へ退かないのは、annotated tag の `tagger.date` が打つ側の書く値であり、古く見せかけられるためである。定義の取得元は commit へ固定する（動く参照を読むと、同じ宣言への判定が実行時刻で変わる）。`goos` ごとの `overrides` を base の url より優先し、展開した URL は**その版を指していること**・ホストが補間で作られていないこと・`https` であることを検めてから叩く —— 版を含まない固定 URL は、どの版に対しても同じ古い時刻を返して窓を素通しにする（版はクエリや素片ではなく**path** に在ることを要求する）。HEAD はリダイレクトを追わない —— 検証を通した宛先とは別のホストが公開時刻を名乗る経路を塞ぐ。解釈できない定義（未知のテンプレート関数を含む）は誤魔化さず「取得できなかった」として落とす。短縮名のバックエンドは `mise registry` に尋ねて解決する —— ここに表を持つと、mise が変わった次の瞬間にずれる。**言語ランタイム（`core:` バックエンド）は受容したリスクとして除外する** —— 汚染された go / node の配布物は供給網の1リンクの失敗ではなく言語の信頼モデルの失敗であり、クールダウンでは守れない。`gate` は base ref と比較して落とし、`audit` は全件を並べて窓では落とさない。どちらも `.github/tool-cooldown-bypass.toml` の期限切れ・3ヶ月超・**どれにも当たらないエントリ**で落ちる。無効なエントリは効力を失うので、失効したバイパスがそのモジュールを通し続けることはない。|`make tool-cooldown-gate BASE=<ref>` / `make tool-cooldown-audit` / `make tool-cooldown-outdated`|
|`versions/`|`mise.toml` の `[tools]` が宣言する言語ランタイムの版を、それを書き写している箇所 —— `docker/tools/Dockerfile` の `FROM golang:` 2件と `FROM node:` 1件、`scripts/go.mod` の `go` ディレクティブ —— へ反映し、ずれを検出する（ADR-0501 決定19 が版の正本を単一 manifest と定める）。**ずれてもビルドは通る**ので、この検査が無いと「ずれている」と「揃っている」が緑で区別できない —— 落ちるのは、ずれた版に無い機能を使ったときだけである。一致件数は下限ではなく**厳密な件数**で見る: 写しが増えたことも減ったことも、宣言との対応が崩れた合図である。`FROM` の照合はコメント行を除き、レジストリを前置した `FROM docker.io/library/golang:` は別物として掴まない。全ての rule を検証し終えるまで1バイトも書かない。|`make versions-apply` / `versions-check`|

### ブランチとリリースの操作

|Script|Description|Invoked By|
|---|---|---|
|`base-branch/`|Print the branch name of the latest release line — the branch a feature branch is cut from. The source is `origin`'s live state (`git ls-remote --heads origin 'refs/heads/release/*'`); no local ref is read, because `refs/remotes/origin/HEAD` is fixed at clone time and `git fetch` never updates it, and the GitHub default branch can still point at an earlier release line. Both go stale without warning, which is how a feature branch ends up cut from a generation-old base. "Latest" is the numeric comparison of `major` / `minor` / `patch`, the same basis `release/` uses to choose the next version, so the tool that creates these branches and the tool that resolves them agree: the commit date reorders under a hotfix or a base merge into an older line, and string order puts `v1.10.0` before `v1.9.0`. A remote with no `release/vX.Y.Z` branch is an error rather than an empty answer — a caller cannot tell an empty base from an unresolved one. Scope is `release/*` only, matching the rule this resolves ("cut a feature branch from the latest `release/*`"); a `hotfix/*` branch, which `make hotfix-patch` also makes the GitHub default, is not a candidate.|`make base-branch`|
|`release/`|Create a release tag (`tag`) or the next release branch (`branch`), deriving the next version from the newest semantic-version `git tag` with `-bump patch\|minor\|major`. The steps live here rather than in a Make recipe because both include operations that cannot be taken back — pushing a tag, creating a GitHub Release, moving the default branch — so exercising the branches for real would mean actually releasing. The sequencing and the abort conditions are pure functions pinned by tests.|`make tag-patch` / `tag-minor` / `tag-major` / `branch-patch` / `branch-minor` / `branch-major` / `hotfix-patch`|
|`repo-setup/`|The git / gh half of initialising this boilerplate as your own repository: `preflight` refuses to proceed when a `v0.0.0` tag is present, `bootstrap` recreates the tags, prepares `develop` / `staging` / `production` and moves the default branch, and `prune-release-notes` deletes every release note but `v0.0.0.md`. Labels, rulesets and workflow enablement stay in `setup-repository.mk`, which owns the overall chain. Here too the steps are Go because deleting tags in bulk and moving the default branch cannot be rehearsed without breaking a real repository.|`make setup-repo`|

### lib/

|Package|Description|
|---|---|
|`lib/xerrors/`|StackTrace を含むエラーのラップ・判定・結合。失敗モードはすべてパッケージレベルのセンチネルで、テストは `require.ErrorIs` で到達します。|
|`lib/ghfiles/`|`uses:` を書ける GitHub Actions 定義ファイル（`.github/workflows/**` と `.github/actions/**`）の列挙。列挙が1箇所にあるので、対象範囲が検査ごとにずれません。|
|`lib/workflow/`|workflow 定義を「桁」で読むための切り出し。ブロックスカラーの本体はキーより深く字下げされるという性質に依存し、YAML パーサでは失われる行位置を保ちます。|
|`lib/yamlblock/`|YAML のブロックスカラー（`key: \|` / `key: >-`）の中身の判定。|
|`lib/shellcheck/`|shellcheck の起動と結果の解釈。`actions-shellcheck` と `shell-lint` が同じ解釈を共有します。|
|`lib/lintreport/`|workflow に対する lint が見つけた違反の持ち方と、失敗出力の組み立て。|
|`lib/mdfence/`|Markdown のコードフェンスを、囲む本文から長さを決めて組む。長さを固定にすると本文側がフェンスを閉じて外へ抜けられます。規則の所有は [`.github/workflows/README.md`](../.github/workflows/README.md)。同じ計算が `upsert-pr-comment` の JavaScript にもあり、**両者の食い違いを検査する機構はありません**。|
|`lib/lockfile/`|pin の SSOT が使う `"key" = "value"` 形式の読み書き。書式は `pin-actions` と `pin-images` で同じで、値の形と見出しだけが違います。解釈できない行とキーの重複はエラーにします。|
|`lib/testenv/`|実行環境の都合による skip の入口。root では成立しないケースを `RequireNonRoot`、shellcheck 不在を `RequireShellcheck` に通し、`REQUIRE_NONROOT` / `REQUIRE_SHELLCHECK` が立っていれば skip せず失敗させます。|

## Test Strategy

これらはレイヤーではないので、レイヤーの README が観点を持ちません。観点はここにあります。
構造の規約（`t.Parallel()`、サブテストの粒度、`require` と `assert` の使い分け）も、他に持ち主が
いないのでここが正本です。

**この節は ADR-0702 決定16-17 の具体化です。** 検査器が検査をやめたとき、報告されるのはエラーでは
なく緑であるため、検査器自身の検証を省略できません。

- **シェルではなく判断をテストする。** 各ツールは、ファイルを読んで印字して終了コードを立てるだけの
  entry と、その隣にある判断とに分かれます。`main` は引数を組み立てて `run` を呼ぶだけで、`run` は
  不純な依存——作業ディレクトリ、HTTP クライアント、現在時刻、コマンドランナー——を引数で受け取ります。
  分岐そのものがテストから到達可能であることが、この分け方の目的です。
- **違反だけでなく、退化した入力を固定する。** ここにあるものの大半はゲートであり、ゲートは
  *inspecting nothing and reporting a clean run* の方向へ壊れます。壊れた glob、読めない対象ファイル、
  解釈できない lockfile の行、走査結果0件——これらはそれぞれ**エラーを主張するケース**を持ちます。
  黙って0件へ落ちる経路を作りません。「違反が無かった」と「何も見なかった」を区別し続けるのが、
  この観点です（ADR-0702 決定13-14）。
- **メッセージではなくセンチネルを assert する。** 失敗モードはすべてパッケージレベルのセンチネルで、
  テストは `require.ErrorIs` で到達します。呼び出し側が実際に使う情報——どのファイルがずれたか、
  どのキーが落ちたか——を運ぶ場合に限り、部分文字列の assert を**上乗せ**します。単独の検査手段には
  しません。文言を変えただけで、別のエラーに対する通るテストへ静かに変わるからです。
- **窓には両側を与える。** 閾値と比較するもの——日数のクールダウン窓、桁数——は、境界値そのものと
  その1つ手前を対にします。内側に余裕を持った1ケースでは `>=` と `>` を区別できません。
- **外界はその境界で差し替える。** `git` / `docker` / `gh` は `PATH` の先頭へ置いたシェルスクリプトで
  差し替え、ツールが組み立てた引数列そのものをテスト対象にします。GitHub API とモジュールレジストリは
  `httptest` サーバへ向けます。`t.Setenv` は `t.Parallel()` と両立しないので、`t.Parallel()` は
  ケース単位で宣言し、迂回しません。`actions-shellcheck` は例外で、実物の `shellcheck` を駆動し、
  不在なら skip します。**その skip が実行として通らないようにする**のが `REQUIRE_SHELLCHECK` で、
  skip は既定の出力では見えず、報告より少ない検査で緑を残すからです。権限を落として書き込みや削除の
  失敗を作るケースも同じで、root では落としたはずの権限が効かず成立しません。どちらも `lib/testenv`
  （`RequireShellcheck` / `RequireNonRoot`）を通し、`REQUIRE_SHELLCHECK` / `REQUIRE_NONROOT` で
  skip を失敗へ変えます。CI が両方を立てます。書き込みは一時ファイル経由（`lib/atomicwrite`）を
  通るので、**書き込み失敗のケースはファイルではなく親ディレクトリを読み取り専用にして作ります**
  —— rename はファイルの権限を見ないため、ファイルだけを落としても通ってしまいます。
- **取り返しのつかない手順は、計画として検証し、実行しない。** `release` と `repo-setup` はタグを
  push し、GitHub Release を作り、デフォルトブランチを動かします。手順は `runner` の継ぎ目を通し、
  テストは組み立てたコマンド列と中断条件を assert します。実際に走らせて確かめることは、実際に
  リリースすることだからです。
- **失敗した実行が何も書いていないことを証明する。** ファイルを書き換えるツールは、書く前にすべてを
  決めます。途中で中断したとき、作業ツリーが手つかずであることを assert します——エラーが返ったこと
  ではなく、**エラーの後のファイルの内容**に対して。
- **出力そのものが契約であるものは、出力を assert する。** drift の一覧、`::warning::` 注釈——人間か
  CI の注釈がそれをそのまま読むので、標準のロガーを捕まえて、出たものに対して assert します。
- **フィクスチャは `t.TempDir()` に建て、実物のツリーを読まない。** リポジトリ自身の workflow や
  lockfile を入力にしたテストは、今日の内容で通ったり落ちたりするようになり、ツールについての
  テストであることをやめます（ADR-0702 決定17）。

## Notes

- テストは `make go-test`（カバレッジ付きは `make go-test-cover`）で走ります。`.lefthook.yaml` の
  pre-commit にも載っています——ゲートは「検査をやめると緑になる」ため、テストをフックから外しません
- 整形と静的解析は `make go-fmt` / `make go-fmt-check` / `make go-lint`、依存の整理は
  `make go-tidy-check`
- 実行環境の切り替えは `.makefiles/runner.mk` の `RUNNER_MODE` が受けます。既定は
  `container`（`go_tool_runner`）、`RUNNER_MODE=host` で mise が入れた Go をそのまま使います。
  同じターゲット定義を前置きだけで切り替える形で、`X` と `X-ci` の二本立てにしません
  （[ADR-0503 (tool-execution-form)](../docs/adr/0503-tool-execution-form.md)）
- `repo-setup` はこの boilerplate から新しいリポジトリを作るときの一度きりの手順です
