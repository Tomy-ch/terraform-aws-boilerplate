# GitHub Actions Workflows

CI の workflow 定義。Pull Request を止めるゲート、供給網のクールダウン、秘密と誤設定の走査、
そしてリリース昇格の境界で効くゲートに分かれる。

道具の割当は [ADR-0501](../../docs/adr/0501-development-tooling-composition.md) 決定4 が、
検査の定義は `.makefiles/` が持つ。ここが持つのは**起動の条件と、GitHub Actions 固有の制約**だけである。

## 起動の方針

| 群 | いつ走るか | 何をするか |
| --- | --- | --- |
| Pull Request のゲート | すべての Pull Request | lint / テスト / 宣言と実体のずれで merge を止める |
| 供給網のクールダウン | Pull Request と週次 | 公開直後のバージョンの採用を窓で止め、バイパスの期限を回収する |
| 走査 | Pull Request と週次 | 秘密・誤設定・依存の脆弱性・workflow 自身の穴を報告する |
| リポジトリの衛生 | すべての Pull Request | 綴りと `.editorconfig` の宣言とのずれで merge を止める |
| 供給網の衛生の報告 | 週次と既定ブランチへの push | OpenSSF Scorecard の所見を code scanning へ送る。何も止めない |
| apply | デフォルトブランチの release line への push | 管理アカウントの identity root を apply する。失敗を通知する |
| lock の解除 | 人が手動で起動する | apply の job の timeout を超えて残った state の lock を外す |
| リリース昇格のゲート | deploy ブランチ宛の Pull Request | 修正版の無い脆弱性も含めて落とす |

## 必須検査はすべての Pull Request で報告される

[`branch-protection.json`](../settings/branch-protection.json) が名指しする context は、merge の前に
**報告されなければならない。** GitHub は報告の無い check を「該当しない」とは読まず、待ち続ける ——
その Pull Request は恒久的に merge 可能にならない。

したがって、必須 context を報告する workflow は `pull_request` の起動を**一切絞らない。**
`paths:` も `branches:` も、そこでは「run が起きるかどうか」を決めてしまい、起きなかった run は
何も報告しない。

起動条件は代わりに job の `if:` に置く。GitHub はそこでは逆の結論を出す ——
**`if:` で skip された job は `skipped` を報告し、required check はそれを成功として数える。**
path で絞る workflow は [`dorny/paths-filter`](https://github.com/dorny/paths-filter) を使う
`changes` job で条件を計算し、報告する job をその出力でゲートする。リリース昇格のゲートは
`github.base_ref` を `if:` で直接読む —— 条件がブランチ名であり、ファイル一覧を要さない。

ゲートは **fail open** で書く。

```yaml
    needs: changes
    if: ${{ !cancelled() && needs.changes.outputs.relevant != 'false' }}
```

`relevant` はフィルタが判定に到達しなかった場所では空になる（pull_request 以外のイベントで skip
された、あるいは失敗した）。空は `false` ではないので、仕事は走る。逆に書くと静かに壊れる ——
skip が誰も稼いでいない緑を必須 context へ渡し、**フィルタが壊れた瞬間こそがそれの起きる瞬間**である。

`push` 側の起動は自分の `paths:` を保つ。そちらは context を報告しないので、絞っても何も塞がない。

`make required-check-lint` が、すべての必須 context がちょうど1つの job から宣言されていることと、
その workflow の `pull_request` にフィルタが無いことを見る。**後半がこの行き止まりを閉じている。**
そして条件が1箇所にしか書かれていないからこそ検査できる。フィルタとその否定へ割ると、不変条件は
2つの一覧の一致へ移り、それを突き合わせられるものはここに無い。

### インフラ内コードの検出への依存

`terraform` を AWS credentials 付きで実行する job は、インフラ内コードの検出の成功に依存させる
（ADR-0601 決定9、ADR-0103）。`needs:` は workflow をまたげないので、その job を置く workflow の中に
`make infra-code-gate` を走らせる job を置き、`needs:` で依存する。条件は
`needs.<id>.result == 'success'` と書き、上の fail open の形を使わない —— fail open で書くと、検出が
落ちても credentials を伴う job が走る。検出の job は path で絞らない。判定を変える入力（宣言、Rego、
ADR、code owner の宣言）が広く、skip された required check は成功として数えられるためである。

### plan の job

`terraform-gates.yaml` の `plan-admin-identity` は、environment `admin-plan` の下で OIDC により plan の
role を assume し（ADR-0602 決定14）、管理アカウントの identity root を plan する。path で絞らない
—— AWS の側の状態は、どのファイルを変えたかと無関係に plan を変える。

- **公開されるログに plan の全文を出さない。** 全文は plan の出力の Bucket へ置き、ログと job summary
  には終了コード・件数・object の key・SHA-256・版 ID と、Policy の違反の文（resource の address）だけを
  出す（ADR-0602 決定13、`.makefiles/terraform/operation/ci.mk`）
- **全文は、ログの版 ID で読み、SHA-256 を確かめる。** plan の role は `pr/*`・`branch/*` のどの key にも
  書けるので、版を指定せずに読んだ最新の版は、別の run が書いたものでありうる（ADR-0602 の保証しないこと）。
  `aws s3api get-object --bucket <Bucket> --key <key> --version-id <版 ID> plan.txt` の後、
  `openssl dgst -sha256 -binary plan.txt | base64` がログの sha256 と一致することを確かめる
- **AWS のアカウント ID は environment `admin-plan` の secret `AWS_ACCOUNT_ID` に置く。** 宣言
  （`environments.json`）は secret を持たないので、管理者が置く。無ければ拒否の step で失敗する
- **fork・Dependabot の Pull Request では、資格情報を取る前の step で失敗する。** skip にしない
  （ADR-0602 決定14）。Copilot coding agent の Pull Request は、人が workflow の実行を承認したときだけ走る。
  この承認を省くリポジトリの設定を有効にしないこと（設定を突き合わせる検査は無い）
- job の形（environment、取る role、起動条件、拒否の step、terraform の経路、`run:` と `env:` に置けるもの）は
  `make oidc-job-lint` が見る。拒否の step の本文はシェルなので字面からは選べず、code owner のレビューが持つ

### apply の job

`terraform-apply.yaml` の `apply-admin-identity` は、release line への push で起動し、environment
`admin-apply-identity` の下で OIDC により apply-identity の role を assume して（ADR-0602 決定14）、
管理アカウントの identity root を apply する。path で絞らない。

- **apply するのは、release line のうちデフォルトブランチであるものだけである。** 1つの state に複数の
  ブランチが apply すると、遅れたブランチの push が古い構成を apply し直す。`hotfix/*` と deploy ブランチ
  （`develop`・`staging`・`production`）の push は起動しない。hotfix の間（`scripts/release` が
  デフォルトブランチを `hotfix/*` へ移す）は、release line の push も apply しない
- **hotfix を閉じたら、管理者がデフォルトブランチを release line へ戻す**
  （`gh repo edit --default-branch release/vX.Y.Z`）。戻さない限り、release line への merge は apply の
  job を skip し続け、run は skip だけを示す
- **Pull Request の plan を持ち越さない。** job が push の commit で plan し直し、その plan に Policy
  （`plan.stateful_protection`）を掛け、通った plan だけを apply する（ADR-0601 決定6、ADR-0604 決定11、
  `.makefiles/terraform/operation/ci.mk` の `CI_APPLY`）。plan は lock を取る。変更の無い plan も apply する
- **古い commit を apply しない。** 再実行は元の run の commit と payload を使うので、job の `if:` は古い
  run を止めない。資格情報を取る前の step（`refuse-stale`）が、デフォルトブランチとその先頭の commit を
  API から取り直し、どちらかが違えば失敗する
- **concurrency は workflow 全体に1つ置き、実行中の apply を止めない。** 待っている run は後から来た
  run に置き換わるので、線の途中の commit は apply されず、最新の commit だけが apply される
- **公開されるログに terraform の出力を出さない。** ログには plan の終了コード・件数・Policy の違反の文と
  apply の成否だけを、job summary には件数と apply の成否だけを出す。apply の全文はランナーの外に出さない
- **AWS のアカウント ID は environment `admin-apply-identity` の secret `AWS_ACCOUNT_ID` に置く。**
  無ければ拒否の step で失敗する。置いてから「Re-run failed jobs」で再実行する（先頭の commit のままなら通る）
- 検出の job は `apply-infra-code-gate` として、この workflow に置く。`terraform-gates.yaml` の
  `infra-code-gate` と同名にすると、必須 context を報告する job が2つになる（`make required-check-lint`）
- **apply する木で、resource の作成の順序を確かめる。** `apply-resource-order-check` が
  `make resource-order-check`（[`conftest/hcl/resource_order/`](../../conftest/hcl/resource_order/README.md)）を
  走らせ、apply の job はその成功を `needs` と `if:` で待つ。Pull Request では必須検査の `terraform-gates.yaml` の
  `tf-lint` が同じ検査を走らせ、その配線が外れれば `make oidc-job-lint` が落ちる。ただし必須検査は base の
  更新を要求しないので、merge された木は検査されていないことがある。資格情報を取る job が `needs` に置ける job は、検出の job とこの順序の検査の job だけで、どちらも
  決めた make だけを走らせ、skip も失敗の無視もできない形に限る（`make oidc-job-lint`）
- workload root（`env/<env>/*.tf`）の apply の job と lock の解除の job、`make ci-apply-<env>` と
  `make ci-unlock-<env>`、environment `admin-apply` の secret、workload root の lock を外す role と
  その environment は、workload root を足す変更が足す
- job の形（environment、取る role、起動条件、検出と順序の検査への依存、拒否の step、terraform の経路、concurrency、
  `run:` と `env:` に置けるもの）は
  `make oidc-job-lint` が、`CI_APPLY` が plan の失敗・終了コードと JSON の食い違い・Policy の違反と評価の欠け
  のどれでも apply を呼ばずに非ゼロで終わることは、`scripts/tf-local` の `TestCiRecipes` が偽物の
  terraform・aws・conftest でレシピを走らせて見る

保証しないこと:

- **Pull Request の plan と、apply の plan が同じであること。** 必須検査は base の更新を要求しない
  （`branch-protection.json` の `strict_required_status_checks_policy: false`）ので、merge された木は
  最後に plan された木と違いうる。apply の job の plan と Policy が最後の関門である
- **新しい release line が、古い線の未リリースの変更を持つこと。** 新しい線は `production` から切るので、
  古い線をリリースする前に切ってデフォルトブランチを移すと、apply が古い線の変更を巻き戻す。
  `scripts/release` の手順（タグを打ってから次の線を切る）を外れたときを検出する検査は無い

### lock の解除の job

`terraform-unlock.yaml` の `unlock-admin-identity` は、人が `workflow_dispatch` で起動し、environment
`admin-unlock-identity` の下で unlock-identity の role を assume して、管理アカウントの identity root の
state に残った lock file（`identity/terraform.tfstate.tflock`）を外す。apply の job が timeout・cancel・
ランナーの障害で止まると lock file が残り、以後の apply は lock の取得で落ちる。書き込みの拒否
（ADR-0604 決定9）が入り、bootstrap の一時 IAM user の例外が外れた後は、state Bucket に書ける IAM の主体は CI の role だけなので、人はこの job で外す。

- **unlock-identity の role は、この lock file を外すことしかできない。** 持つのは、prefix を lock file の
  key に限った state Bucket の一覧と、lock file の読み取りと削除だけである。復号は、state の鍵の Key Policy が
  同じ lock file の object に限って許す。`s3:PutObject` と `s3:DeleteObjectVersion` は持たない
  （deployment-identity/ADR-0201 決定9）。environment は apply の environment と分け、保護ブランチだけを
  許す（ADR-0602 決定13-14）
- **apply の concurrency の group に入らない。** この workflow は自分の group を `cancel-in-progress: false`
  で持つ。apply の run を待たせも取り消しもせず、lock の解除の run どうしは直列になる。apply に対する安全性は
  次の3つが担う
  - **閾値**: 消すのは、apply の job の `timeout-minutes` に余裕を足した時間より古い lock だけである。apply の
    job はその時間を超えて走らないので、走っている apply の lock は閾値に届かない。古さは S3 が記録した
    lock file の `LastModified` で測る。閾値は `scripts/tf-local` の定数で、apply の job の
    `timeout-minutes` とのずれは tf-local のテストが見る
  - **読んだ版の条件**: 読んで判定した版だけを、その ETag を条件にした削除（`delete-object --if-match`）で
    消す。判定の後に lock file が置き換わっていれば、削除は失敗する。レシピがこの形で消すことは
    `TestCiRecipes` が見る
  - **Terraform の lock の取得**: Terraform v1.16.6（版は `mise.toml`。`scripts/tf-local` の `lockFormatTerraform`
    との一致は tf-local のテストが見る）の S3 backend は、lock file を `If-None-Match: *` の
    条件付きの書き込みで作る（`lockWithFile`）。lock file が在る間は apply の lock の取得が失敗するので、
    2つの apply が同時に lock を持つことは無い。この性質を見る検査はリポジトリに無い

  group が同じ root の apply の job の group と違うことは、`make oidc-job-lint` が workflow をまたいで照合する
- **新しい lock は消さない。** ID などを出して失敗する。lock が無ければ何も消さずに成功する
- **CI のランナーが apply の job で取った lock だけを消す。** apply の job は plan で lock を取ってから apply
  するので、`Operation` が `OperationTypePlan` と `OperationTypeApply` の lock を消す。`Who` が `runner@` で
  始まらない lock と、`Operation` がそのどちらでもない lock は、古くても ID などを出して失敗する（ADR-0604 決定8）。人の手元の apply の
  lock は、その apply を実行した人が `terraform force-unlock` で外す
- **版を指定せずに消す。** 版管理の下で delete marker になり、消した lock の版は lifecycle の日数
  （state-backend/ADR-0103）まで残る。ログに消した lock の版 ID と delete marker の版 ID を出すので、
  run のログから版を辿れる。版 ID の無い（版管理の外の）lock は消さずに失敗する
- **terraform は呼ばない。** `terraform force-unlock` は init を要し、lock の ID を lock file から読むので
  ID の照合が自己参照になり、削除に条件を付けない
- **ログに出すのは lock の ID・Operation・Who・Version・Created・LastModified・経過分と版 ID だけである。**
  `Path`（Bucket 名がアカウント ID を含む）と `Info` は出さない。`Who` は GitHub-hosted runner の利用者
  （`runner@` で始まる）のときだけそのまま出し、それ以外は伏せる —— 人の手元の利用者名とホスト名を
  公開のログに出さない
- **自動では起動しない。** 残った lock は apply が途中で止まった信号であり、外す前に人が止まった run の
  ログを見る。push でも起動しない —— merge のたびに lock を消す経路になる
- **外した後、lock で落ちた apply の run を「Re-run failed jobs」で再実行する。** release line の先頭の
  commit のままなら `refuse-stale` を通る
- 古い commit の拒否（`refuse-stale`）と検出への依存（`unlock-infra-code-gate`）は、apply の job と同じで
  ある。job の `if:` は ref で絞らないので、デフォルトブランチ以外から起動した run は skip ではなく
  `refuse-stale` で失敗する。hotfix の間は、デフォルトブランチ（`hotfix/*`）から起動する
- **AWS のアカウント ID は environment `admin-unlock-identity` の secret `AWS_ACCOUNT_ID` に置く。** 無ければ
  拒否の step で失敗する
- 通知は持たない。人が起動し、結果を画面で見る
- 許す AWS の宛先は `aws-state` クラス（STS と S3）だけである。egress の `root` はこの job も名指すが、
  型の宛先は `ci-unlock-*` だけを呼ぶ job に求めない（「ランナーの封じ込め」節）
- job の形は `make oidc-job-lint` が、`CI_UNLOCK` が lock の無いとき以外は lock を消すか非ゼロで終わり、
  新しい lock・置き換わった lock・読めない lock・人の手元の apply の lock を消さないことは `scripts/tf-local` の `TestCiRecipes` が
  偽物の aws でレシピを走らせて見る

保証しないこと:

- **CI のランナーの plan と apply の lock が、apply の job のものであること。** `Who` と `Operation` は lock の
  書き手が書く値で、`runner@` で始まる利用者が apply の job の外で取った plan と apply の lock も、閾値を超えれば消す
- **読めない lock file を外すこと。** 中身を Terraform の lock として読めない lock file と、版 ID の無い
  lock file は、消さずに失敗する。一時 IAM user の書き込みが拒否された後は、
  それを外す手順がこのリポジトリに無い（ADR-0602 決定9）
- **起動できる人を絞ること。** write 権限を持つ人なら起動できる。environment の reviewer は置かない ——
  障害のときに人が1人しか居なくても外せるようにする。実行されるのはデフォルトブランチの先頭の workflow と
  コードだけで、できることは閾値を超えた lock file 1つを消すことに限られる

## 結果コメント

Pull Request のコメントは、**検査が報告すべきことを持っているときにだけ作られる。** 通った検査が
1つずつコメントを残すと、誰も訊いていないことが並び、言うことのある1件が埋まる。

判定は `upsert-pr-comment` の `status` が運ぶ。コメントの作成を抑えるのは文字列 `success` だけで、
それ以外の値 —— 打ち切られた job が残す空文字を含めて —— はコメントを作る。

**沈黙は修正の報告にならない。** `success` が抑えるのは*作成*であって更新ではない。前の push で
落ちた検査は、自分の赤いコメントを緑の結果でその場に上書きする。読み手は、失敗が報告された場所で
それが解決したことを知る —— 不在に気づくことによってではなく。

**判定は肯定側から導く。** 手前のステップが `status` を出すならそれを渡し、綺麗な状態が件数や
フラグで表されるなら、その値そのものを検査する（`steps.<id>.outputs.count == '0' && 'success' || 'findings'`）。
所見の側を検査してはならない。差が出るのは**生成側のステップが走らなかったとき**である ——
出力は空になり、それは綺麗な値ではないので、検査は黙らずに報告する。逆に書くと、途中で終わった
run がすべて合格に見える。

## ジョブの打ち切り

job は判定に到達しないまま止まりうる —— timeout、cancel、ランナーの障害。そのとき Pull Request に
何が見えるかは、走っていた道具の性質ではなく、job とコメントステップの宣言の仕方から決まる。
**そしてここでの既定はすべて間違った側である。** `make actions-cutoff-lint` が下の規則を課す。

**`upsert-pr-comment` を呼ぶステップは、cancel の後でも到達可能でなければならない。**
Actions は、状態検査関数を含まない `if:` へ暗黙の `success() &&` を前置する。打ち切られた job は
コメントステップを skip し、Pull Request には何の痕跡も残らない —— 一方 `Fail if …` は通常
`always()` を持つので check は赤くなる。**理由の読めない赤は、どちらの半分よりも悪い。**
条件には `always()` か `cancelled()` が要る。`failure()` は資格を持たない —— cancel された job で
false になる。

**body ファイルの不在は、ステップの失敗ではなく打ち切りとして報告する。** 早くに打ち切られた job は
ファイルを書くステップまで到達しないので、不在はまさにコメントが生き延びるべき場合の通常の形である。
`upsert-pr-comment` は打ち切りの通知を出し、呼び出し側の見出しを差し替える —— job が走らなかったと
述べる本文を、呼び出し側が設定したどの題も説明できない。

**不在は検査の半分でしかないので、呼び出し側は打ち切り用の見出しも渡す。** 多くの検査ステップは
出力を `tee` で body ファイルへ流し、`title` を終了コードから後で設定する。検査の途中で打ち切られた
job は**書きかけのファイル**を残し、action はそれを完成したものと区別できない。呼び出し側が
`${{ steps.<id>.outputs.title || '## ⚠️ <check>: CUT OFF (no result produced)' }}` でもう半分を運ぶ。
GitHub の式の罠に注意 —— `cond && '' || X` は常に `X` になる。空文字が falsy なので、見出しは
truthy 側へ置く。

**すべての job が `timeout-minutes` を持つ。** 無ければ GitHub の既定 360 分まで走り、1つのハングが
ランナーを6時間押さえる。値は実測の最大 × 3 を 5 分単位へ切り上げ、下限 10 分。完了した run が
まだ無い job は 15 分。

| job | 分 | 式から外れる理由 |
| --- | --- | --- |
| `go-test.yaml:go-test` | 20 | 実測 約5分 |
| `notify.yaml:notify`、`tool-outdated-report.yaml:report` | 15 | 実測できる完了 run が無い |
| `md-lint.yaml:adr-lint`、`md-lint.yaml:skill-lint`、`go-test.yaml:test-mapping` | 15 | 同上（新設） |
| `terraform-gates.yaml` の `changes` と `plan-admin-identity` を除く6 job、`terraform-apply.yaml:apply-infra-code-gate`、`terraform-apply.yaml:apply-resource-order-check` | 15 | 同上（新設） |
| `terraform-unlock.yaml` の2 job | 15 | 同上（新設）。apply の job と違い terraform を呼ばない |
| `terraform-gates.yaml:plan-admin-identity`、`terraform-apply.yaml:apply-admin-identity` | 30 | AWS の API を呼ぶ。role の session（3600 秒）より短く置く |
| `sonarcloud.yaml` の3 job、`sample-remove-check.yaml:sample-remove-check` | 15 | 同上（新設） <!-- sample:line --> |
| `checkov.yaml:checkov`、`kics.yaml:kics`、`semgrep.yaml:semgrep` | 15 | 実測できる完了 run が無い（新設） |
| `secret-scan.yaml` | 15 | 実測は Pull Request のみ。週次は履歴全体を走査し、完了 run が無い |
| `hygiene.yaml` の2 job、`scorecard.yaml:scorecard` | 15 | 実測できる完了 run が無い（新設） |
| `repo-settings.yaml:repo-settings-check` | 15 | 同上（新設） |

限界に触れ始めた job は実測を追い越している。数字を小突かず、測り直して式を当て直す。
再利用可能 workflow を呼ぶ job は `timeout-minutes` を持てない（キーが無効）ので、検査は除外し、
上限は呼ばれる側の job が持つ。

3つの規則が1つの検査に同居しているのは、これが3つの方針ではないからである —— 上限の無い job が
打ち切りを生み、2つのコメント規則がそれを読めるものにする。どれか1つだけ直しても Pull Request は
何も良くならない。

## 通知の起動条件

スケジュール（週次）を持つ走査は、job が `failure` または `cancelled` で終わったとき `notify.yaml` を
呼ぶ。Pull Request の失敗は作者に見えている。**スケジュールの失敗は誰にも見えない** —— それが
通知の存在理由である。`cancelled` を含めるのは、timeout やランナー障害で殺された job が `failure`
ではなくそちらを報告するため。

報告のみの走査は所見があっても job が緑のままなので、failure モードは決して発火しない。それらは
検出モードで `notify.yaml` を呼び、実行者・ref・commit と所見そのものを名指しする。どちらのモードも
webhook の secret が未設定なら配信を省いて run を緑のままにする —— 作られたばかりのリポジトリが、
送れない通知で落とされることはない。

どの起動条件で検出通知を出すかは、正しい受け手が誰かから決まる。脆弱性の走査ではスケジュール実行
だけである。Pull Request では所見は既に、それを持ち込んだ作者宛のコメントに在る。**週次の所見は、
止まっているコードに対して新しく公開された勧告であり、放っておくと誰にも届かない。**

| workflow | 発火条件 | 起動 |
| --- | --- | --- |
| `trivy-fs.yaml` | 修正版のある CRITICAL / HIGH / MEDIUM | schedule |
| `govulncheck.yaml` | 運用機構（`scripts/`）の Go のコードの呼び出し経路が届く既知脆弱性 | schedule |
| `tool-outdated-report.yaml` | 宣言より新しい版が上流に在る | schedule |

`terraform-apply.yaml` は push で起動するが、スケジュールと同じ理由で failure モードを持つ —— merge の後の
失敗は誰にも見えず、merge したのに apply されない状態を黙って作る。検出の job か順序の検査の job が落ちると
apply の job は `skipped` で終わるので、3つの job のどれの `failure` / `cancelled` でも通知する。通知が名指す
job は、apply の job が走ったならそれ、走らなければ成功しなかった検査の job（検出の job を先に見る）である。`terraform-unlock.yaml` は
通知を持たない —— 人が起動し、結果を画面で見ている。

秘密の走査（gitleaks / trufflehog / Trivy secret）と `zizmor`（high）は所見で job を落とすので、
failure モードが既に届けている。`scorecard.yaml` は failure モードだけを持つ —— 所見の届け先は
code scanning の alert である。`trivy-license` は誰もまだ問題だと合意していないライセンスを報告するため、
意図的に繋いでいない。

## Go のクールダウン

Go には `min-release-age` に相当するものが無い。`go get` に「新しすぎる版を拒む」と言わせる手段は
存在しない。これが道具とゲートの関係を反転させる —— pnpm は解決時に拒むので resolver がゲートだが、
ここでは**検査がゲートそのもの**であり、報告だけに留めると窓はどこにも存在しなくなる。

`go-cooldown.yaml` は Pull Request でゲートし、対象は変更が追加・更新した require だけである。
`go.mod` に既に在るものは据え置かれ、窓はこれから先に効く —— 継承した状態にすべてのブランチを
人質に取らせない。落とすのは**直接**の require だけ。間接の版は MVS が選び、直接依存自身の下限より
上に置かれうる。そこを下げることは Pull Request にできないので、落とせば手当のできない赤になる。

窓は **7日**（`scripts/go-cooldown/main.go` の `defaultWindowDays`）。Go module は install script を
持たない —— `go mod download` は何も実行しない —— ので、公開直後の版が install 時に機械を取る類は
存在しない。窓が買うのは、悪意あるコードがビルドされ出荷されるまでの時間である。

緊急の迂回は [`go-cooldown-bypass.toml`](../go-cooldown-bypass.toml) が持ち、全エントリが期限を持つ
（[ADR-0702](../../docs/adr/0702-repository-operations-substrate.md) 決定18）。期限は `go.mod` が
変わらなくても訪れる。**だからスケジュール実行が要る** —— Pull Request の起動だけでは、期限が切れる
瞬間を誰も見ない。

## ランナーの封じ込め

このディレクトリのすべての job は、最初の step で `bullfrogsec/bullfrog` を `egress-policy: block`・
`dns-policy: allowed-domains-only`・`enable-sudo: false` で、自分の `allowed-domains` とともに開始する。
外向きの接続と名前解決を一覧に照らし、それ以外を拒む —— 侵害された action や道具のダウンロードが、
その job に用の無い宛先へ持ち出せない。sudo を外すのは、sudo が残る job では step の中から通信制限
そのものを外せるからである。決定と受け入れた限界は [ADR-0702](../../docs/adr/0702-repository-operations-substrate.md)
決定20-23 が持つ。

このステップは**すべての job にインラインで置かれる。これは好みではなく制約である。**
ローカルの composite action（`uses: ./.github/actions/*`）はリポジトリを checkout して初めて解決され、
bullfrog は checkout の**前**に走らなければならない —— checkout 自体が外向きの呼び出しであり、
それを守ることが目的だからである。括り出すと、塞ぐために置いた窓が開く。

**固定されていないのは一覧の出所の方である。** [`.github/egress.toml`](../egress.toml) が SSOT で、
`make egress-apply` が各 job の `with:` を丸ごと書き、`make egress-check` がずれで落ちる。step が
`name` / `uses` / `with` 以外のキー（`if:` や `continue-on-error:`）を持つこと、job の最初の step で
ないこと、steps を持つ job に bullfrog が無いこと、steps を読み取れない job（`steps:` は4桁、step は
6桁のハイフンで始める）も、同じ検査が拒む。拒む条件の一覧は
[`scripts/README.md`](../../scripts/README.md) の `egress/` の行が持つ。

**job は宛先ではなく能力のクラスを宣言する。** job が何に到達するかは、その job が*何をするか*から
決まり、job 自身の素性からは決まらない —— 実行は `make` から docker、コンテナ内の `mise` へ降りるので、
必要な宛先は YAML から見えない。

| クラス | 宛先 | 当たるもの |
| --- | --- | --- |
| `base` | GitHub API / web / codeload、`objects` / `raw` / `release-assets.githubusercontent.com`、`*.actions.githubusercontent.com`、結果と cache の置き場のうち bullfrog の agent が既定で許さない番号の `productionresultssa<N>.blob.core.windows.net`（足し方は宣言のコメント）。IP は `127.0.0.100`（理由と外す条件は宣言のコメント。IP はドメインの判定を経ずに通すので、宣言の検査が `127.0.0.0/8` の外を拒む） | **すべての job に暗黙に適用される。** `classes` に書かない |
| `mise` | mise 自身の配布元と、`mise.toml` が解決するすべての backend —— aqua / GitHub releases、Go の toolchain と module proxy、npm レジストリ、および Sigstore（mise が artifact attestation を検証する） | 道具を導入するすべての job |
| `terraform` | Terraform Registry と provider の配布元 | Terraform を実行する job（[ADR-0205](../../docs/adr/0205-module-dependency-policy.md) により外部 module は引かない） |
| `trivy` | 脆弱性 DB と checks バンドルの OCI の配布元 | Trivy を実行する job |
| `docker-hub` | Docker Hub の registry・認証・layer の配布元（R2 は Docker Hub のバケットのホストだけ） | 上流のイメージで道具を実行する job（Checkov / KICS / Semgrep） |
| `deps-dev` | deps.dev（パッケージの供給網の情報） | 依存を評価する job（Dependency Review、Scorecard） |
| `aws-state` | STS・S3 の endpoint（region と wildcard の理由は宣言のコメント） | OIDC で role を assume し、state の Bucket に触れる job（lock の解除の job） |
| `aws` | `aws-state` に加えて IAM・KMS・S3 Control の endpoint | Terraform で AWS の API を呼ぶ job |

1つの job に固有のものは、その job の `extra` へ置く —— 走査のデータ源、通知先の `hooks.slack.com`。
2つ目の job の `extra` に現れた宛先は、クラスへ移す。

**クラスは意図的に粗い。** 細かく割れば allowlist は締まるが、分類の判断が増える。**そして間違った
クラスに置かれた job こそ、この仕組みが取り除こうとしている失敗である。** 必要より少し広いクラスも、
その外側はすべて拒む。

宛先はポートを付けないドメイン名で書く。bullfrog は port で絞らないので、ポート付きは宣言の検査が
拒む。`*.example.com` は apex を含まない。宛先を足すには `.github/egress.toml` を編集し（能力から
導かれるならクラスへ、そうでなければ job の `extra` へ）、`make egress-apply` を実行して生成された
`with:` を commit する。インラインの `with:` を手で編集しない —— `make egress-check` が拒む。

**Terraform を実行する job の AWS の宛先は、Terraform の設定から突き合わせる。** 宣言の `root` が
CI の実行する root とそれを実行する job を、`type` が resource / data source の型の呼ぶ宛先を持つ。
`make egress-check` は root から相対パスの module を辿って型を集め、どの `type` にも当たらない型と、
`type` の宛先を持たない job と、`type` の宛先を許すのにどの root にも名指されていない job で落ちる。
job が実際に実行する root は、job が呼ぶ `make ci-plan-*` / `ci-apply-*` / `ci-unlock-*` を `.makefiles/terraform/operation/ci.mk`
の呼び出しの引数から読み（配置は ADR-0604 決定5）、`root` の名指しと食い違えば落ちる。`type` の宛先を
求めるのは、その root の `ci-plan-*` か `ci-apply-*` を呼ぶ job だけである —— `ci-unlock-*` は terraform を
呼ばないので、それだけを呼ぶ job には求めない。区別は呼ぶ入口から導くので、lock の解除の job が plan か
apply の入口も呼べば、その job は型の宛先を求められる。root を名指す job のどれも `ci-plan-*` / `ci-apply-*`
を呼ばなければ落ちる。region を含む
`type` の宛先を使う root では、辿る aws の provider の `region` がただ 1 つでなければならず、宛先の region が
それに一致しなければ落ちる。job の外（`jobs:` より前の `env:` など）に入口の名前が在っても落ちる。
新しい AWS のサービスの型を足す変更は、その `type` と、クラスへの宛先の追加を同じ変更に含める。
Terraform を実行する job を足す変更は、その root の `root` の宣言を同じ変更に含める。接頭辞の `type` は、
当たる型がすべて同じ宛先を呼ぶと言える範囲に限る —— 同じサービス名の下で別の endpoint を呼ぶ型
（S3 の directory bucket など）は、型名で別に宣言する。宛先が欠けると、Provider は遮断された呼び出しを再試行し続け、
job は理由を出さずに時間切れになる —— plan が通っても apply で初めて呼ぶ API があるので、plan の
成功はこの欠落を示さない。backend や資格情報の交換のように型を経ない呼び出し、`ci.mk` の入口を経ずに
terraform を実行する job、workflow の行に現れない入口の呼び出し（composite action の中など）は、
この突き合わせの外にある。

塞がれた宛先は、run の job summary に bullfrog が出す接続ごとの判定の表に現れる。job のログからは
理由の見えない失敗に当たったとき読むのはそこで、直し方はクラスか `extra` を広げることであって、
`audit` へ落とすことではない。

## ランナーの版

`runs-on:` が名指しする label は [`.github/runners-pin.toml`](../runners-pin.toml) が SSOT で、
`make pin-runners-apply` が各 workflow へ書き込み、`make pin-runners-check` がずれで落ちる。
label を手で書き換えない —— check が拒む。

**何を固定し何を固定しないかは [ADR-0505](../../docs/adr/0505-ci-runner-label-pinning.md) が所有する。**
止まるのは OS の版が入れ替わることだけで、同じ label の下でのイメージ更新は止まらない。

## 共有 composite action

再利用する composite action は [`.github/actions/`](../actions/) に置く。

| action | 役割 |
| --- | --- |
| `setup-mise` | digest を検証してから pin された mise を導入し、呼び出し側が名指しした道具を入れる |
| `upsert-pr-comment` | マーカーによる PR コメントの upsert（既存を検出 → 更新 / 作成）。`status: success` は既存を更新するが新規は作らない |
| `notify-detail` | 走査の Markdown 要約を所見の行だけへ削る。検出通知が、PR コメント向けの散文を運ばずに所見を名指しできる |

### mise の導入

mise を要する workflow は [`setup-mise`](../actions/setup-mise/action.yaml) を使う。action は pin された
mise のバイナリをリトライ付きでファイルへ落とし、版と digest でキャッシュし、実行の前に毎回 SHA256 を
検証する。**復元されたというだけでキャッシュを信用しない** —— 不一致は捨てて取り直し、2度目の
不一致で job を落とす。

道具の版は [`mise.toml`](../../mise.toml) に残る。呼び出し側が渡すのは、必要な道具の指定だけである。
`make actions-mise-pin-lint` が、action の版・digest・キャッシュキーの三点の不一致を拒む。mise 自身を
更新するときは、release の `SHASUMS256.txt` から checksum を取り、三点を同時に更新する。

### キャッシュの安全性

キャッシュはブランチ単位である —— run が復元するのは自分の ref のものと既定ブランチのものだけ ——
ので、Pull Request の run が、後の `release/*` の push が復元するキャッシュを書くことはできない。
だから通常の CI ではキャッシュを有効にしたままにする。

汚染が可能になるのは、**信用できない PR のコードが信用された scope で実行され、その間にキャッシュが
保存されるとき**である。`pull_request_target` と `workflow_run` は base ref の scope で走るので、そこで
PR の head を checkout する workflow は、特権のある run が読む場所にキャッシュを残す。この2つを
組み合わせない。信用できないコードを扱う workflow はキャッシュを切る。

### PR コメントのフェンス

**攻撃者が内容を左右できるテキストを囲むフェンスは、そのテキストから長さを決める。固定しない。**
`upsert-pr-comment` は本文中で最長のバッククォート連続より1つ長いフェンスを計算するが、それは
`details-summary` を渡した経路でのみである。その入力が無ければ本文は素通しになる —— 呼び出し側の
いくつかは、描画されることを意図した Markdown（見出し・表・自前の `<details>`）を書くからである。

素通しの経路で本文の一部を自分でフェンスする呼び出し側は、そのフェンスを自分で所有する。固定の
3連バッククォートは、ソース行を再現する本文なら何でも閉じられる —— Pull Request の作者が書いた
ファイルを引用する linter は、3連バッククォートをブロックの内側へ落とし、以降が bot の名前で
生きた Markdown として描画される。

**同じ規則がインラインのコードスパンにも掛かる。**長さ1のフェンスに過ぎないためである。path が
効く場合で、保持できないバイトは NUL と `/` だけなので、バッククォート・`@`・リンク記法はすべて
使える。呼び出し側は、リポジトリ由来の path をスパンにも裸の Markdown にも置かず、一覧そのものから
取った長さで一覧全体をフェンスする。

呼び出し側は本文を action の `max-length` 未満に保つ必要もある。**これはフェンスの付与より前に
適用される** —— そこで切られた本文は閉じフェンスを失う。

`make pr-comment-fence-lint` は機械的に判定できる部分を見るが、本文が攻撃者由来かどうかは判定できない。
**規則の方が linter より広い。**

## 補足

- **PR へコメントする job に secret を渡さない。** secret のマスクが覆うのは、ランナーが job の出力を
  ログへ取り込む経路だけである。ステップが `tee` でファイルへ書いたバイトはそこを通らず、
  `upsert-pr-comment` は本文をまさにそのファイルから読む。ログでマスクされて見えた値が、公開された
  コメントへ生のまま着地する。`make pr-comment-secret-lint` が、この action を使う job へ
  `GITHUB_TOKEN` 以外が渡されたときに落ちる。検査は直接の `secrets` 参照だけを読むので、
  `needs.<job>.outputs` 経由なら迂回できる —— **保つのは linter ではなく規則である。**
- **`upsert-pr-comment` は自分のコメントを、bot の作者と先頭のマーカーで突き合わせる。** 公開リポジトリ
  では誰でもマーカーを持つコメントを投稿でき、ここの workflow はすべて同じ bot でコメントするので、
  マーカーも作者も単独ではコメントを識別しない。action が書いた本文は必ずマーカーで始まり、
  仕込まれたものはそうならない。`github-token` は bot として投稿するトークンでなければならない ——
  PAT はユーザとして投稿し、その コメントを action は決して見つけられず、毎回新しいコメントを残す。
- **`run:` の本文へ補間された式はコードであり、それを見るのは zizmor だけである。** `${{ }}` は
  シェルが何かを解釈する前に置換されるので、引用符の無い `github.event.*` はコマンドを終わらせて
  攻撃者のものを始める。shellcheck 系のゲートはこれに構造的に盲目である（理由は
  [`scripts/README.md`](../../scripts/README.md) の `actions-shellcheck` の行）。式を `env:` へ束ね、
  シェルでは `"$VAR"` として読む —— 値はそこへデータとして届く。
- zizmor の audit への例外は `.github/zizmor.yml` に置く。`ignore` はファイル単位であり、同じ audit に
  当たる**新しい** workflow は落ちる。エントリは恒久の allowlist として残さず、元の所見が直った時点で
  消す（[ADR-0501](../../docs/adr/0501-development-tooling-composition.md) 決定13）。
- **GitHub は 60 日 commit の無いスケジュール workflow を自動で無効にし、それを黙って行う。**
  生かし続けることはこのテンプレートの範囲外である —— keepalive の job は用意しない。静かになった
  リポジトリは、Actions タブから再有効化することになると思っておくこと。
- テンプレートから作られたリポジトリは、すべての workflow が `disabled_fork` 状態で始まり、何も
  走らない。`make enable-workflows` が列挙して有効化する。冪等で、再実行してよい。
- `trivy-fs.yaml` は check を落とさない。所見は修正版の有無にかかわらず code scanning と PR コメントへ
  書かれ、止める判定はリリース昇格のゲートが持つ。**昇格が既知の脆弱性を黙って出荷することも、
  通常の Pull Request が自分の持ち込んでいない脆弱性に人質を取られることも、どちらも起こさない。**
- `govulncheck.yaml` も check を落とさない。
