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
| 供給網の衛生の報告 | 週次と既定ブランチ（線のどれか）への push | OpenSSF Scorecard の所見を code scanning へ送る。何も止めない |
| 上流の報告 | 週次と手動 | 宣言より新しい版や、手で写した一覧と上流とのずれを issue へ書く。何も止めない |
| apply | デプロイブランチ `admin` への push | 管理アカウントの identity root と workload root を、この順に apply する。失敗を通知し、失敗を戻す revert のブランチを用意する |
| lock の解除 | 人が手動で起動する | apply の job の timeout を超えて残った state の lock を外す |
| lock を残す | 人が手動で起動する | lock の解除を障害の前に試すために、apply と同じ role で state の lock を取り、外さずに残す |
| リリース昇格のゲート | デプロイブランチ宛の Pull Request | 修正版の無い脆弱性も含めて落とす |

ブランチの名前は [`branches.json`](../settings/branches.json) から `make repo-settings-apply` が生成し、
`repo-settings-check` がずれを落とす。apply の起動条件（`on.push.branches`）は、job の `environment:` の env の
デプロイブランチ1つになる。ほかの workflow は、生成する行の末尾に印 `# branch-set: <集合>` を置く。

| 集合 | 展開 | 置ける行 |
| --- | --- | --- |
| `ruleset` | デプロイブランチと各線（`<接頭辞>**`）。ruleset が保護する集合と同じ | `branches:` |
| `deploy` | デプロイブランチ | `branches:`、`contains(fromJSON('[...]'), <ref>)` を持つ `if:` |
| `lines` | 各線（`<接頭辞>**`） | `branches:`、`(startsWith(<ref>, '<接頭辞>') \|\| ...)` を持つ `if:` |

印の無い `branches:` / `branches-ignore:` の一覧（apply の workflow のものを除く）と、ブランチの名前か ref を値に持つ
値（`github` と `inputs` の下の値と、それを関数や演算で加工した値）を式の中で作った文字列と比べる式（印の行で生成した
項を除く）と、式として読めない式は、`repo-settings-check` が落とす。見分ける規則は検査が持つ（[`scripts/README.md`](../../scripts/README.md)
の `repo-settings/` の行）。`env:` や step・job の出力へ移した値の比較と、シェルの中の比較は見ない。

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

`repo-settings.yaml` の `deploy-source-check` は、対象かどうかを job の中で決める。デプロイブランチ宛でない
Pull Request では「対象外」と出力して成功する（ADR-0603 決定31-32）。判定は base に依るので、`repo-settings.yaml` は
`pull_request` の `edited`（base の付け替えを含む）でも走り直す。付け替える前の「対象外」の成功が、付け替えた後の
デプロイブランチ宛の Pull Request を通さない。この job は Pull Request の base の
commit を checkout し（`ref: github.event.pull_request.base.sha`、`fetch-depth: 0`）、そこにある宣言と
`scripts/repo-settings` で判定する。Pull Request は検査のコードと宣言を書き換えられない。job の定義そのものは
Pull Request の側のもので、書き換えうる。`repo-settings.yaml` と `scripts/repo-settings/` は code owner の
対象に置く（ADR-0603 の保証しないこと）。

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

`terraform-gates.yaml` の `plan-admin-identity` と `plan-admin-workload` は、environment `admin-plan` の下で OIDC により plan の
role を assume し（ADR-0602 決定14）、管理アカウントの identity root と workload root を plan する。path で絞らない
—— AWS の側の状態は、どのファイルを変えたかと無関係に plan を変える。

- **公開されるログに plan の全文を出さない。** 全文は plan の出力の Bucket へ置き、ログと job summary
  には終了コード・件数・object の key・SHA-256・版 ID と、全文から導いた値のうち許可の文法
  （`scripts/lib/publicdiag`）に当たるものだけを出す（ADR-0602 決定13、`.makefiles/terraform/operation/ci.mk`）。
  失敗の診断（AWS の service・operation・error code・拒否された Action、条件の失敗のファイルと行）、変わる
  resource の address、Policy の違反の規則の ID と件数がそれに当たり、違反の文は全文へ置く。全文は、
  init・plan・apply の `-json` の stream と、plan の人の読む差分（`terraform show`）を順に持つ。identity root の plan の、CI の role の policy の
  文書の評価（`scripts/bootstrap-check` の `plan-permissions`）も、区分ごとの件数と期待と違う行の要約だけを出す
- **全文は、ログの版 ID で読み、SHA-256 を確かめる。** plan の role は `pr/*`・`branch/*` のどの key にも
  書けるので、版を指定せずに読んだ最新の版は、別の run が書いたものでありうる（ADR-0602 の保証しないこと）。
  `aws s3api get-object --bucket <Bucket> --key <key> --version-id <版 ID> plan.txt` の後、
  `openssl dgst -sha256 -binary plan.txt | base64` がログの sha256 と一致することを確かめる。全文は state の鍵で
  暗号化されており、読む主体には Key Policy による復号が要る。Key Policy が復号を許すのは CI の role だけなので、
  人がこの手順を実行できる主体は無い（`usecases/state-backend/README.md` の unsupported）
- **AWS のアカウント ID は environment `admin-plan` の secret `AWS_ACCOUNT_ID` に置く。** 宣言
  （`environments.json`）は secret を持たないので、管理者が置く。無ければ拒否の step で失敗する
- **fork・Dependabot の Pull Request では、資格情報を取る前の step で失敗する。** skip にしない
  （ADR-0602 決定14）。Copilot coding agent の Pull Request は、人が workflow の実行を承認したときだけ走る。
  この承認を省くリポジトリの設定を有効にしないこと（設定を突き合わせる検査は無い）
- **plan の role を assume した後、plan の前に、apply 系の role の assume が拒否されることを確かめる。**
  `make ci-assume-denied` が同じ job の OIDC token で、`pull_request` で起動する種別のほかのすべての role
  （`scripts/lib/cientry` の `DeniedRoles` が登録簿から求める）を assume し、
  すべてが STS の `AccessDenied` でなければ落ちる。資格情報は置かない。plan の job のこの step は skip できず、
  欠ければ `make oidc-job-lint` が落ちる。失敗した assume は run ごとに CloudTrail に残る（session 名は `gha-denied-` で始まる）
- **リリース線宛の plan は、管理アカウントの今の状態に対する差分である。** 管理アカウントへ当たっているのは
  デプロイブランチ `admin` の木なので、リリース線へ merge されてまだ `admin` へ昇格していない変更も差分に出る
  （ADR-0603 決定13）。その差分は、リリース線の Pull Request が足す変更より大きいことがある
- job の形（environment、取る role、起動条件、拒否の step、拒否の確認、terraform の経路、`run:` と `env:` に置けるもの）は
  `make oidc-job-lint` が見る。拒否の step の本文はシェルなので字面からは選べず、code owner のレビューが持つ

### apply の job

`terraform-apply.yaml` の `apply-admin-identity` と `apply-admin-workload` は、管理アカウント（env `admin`）のデプロイブランチ
`admin` への push で起動し、OIDC により role を assume して（ADR-0602 決定14）、管理アカウントの root を apply する。
`apply-admin-identity` は environment `admin-apply-identity` の下で apply-identity の role を取って identity root を、
`apply-admin-workload` は environment `admin-apply` の下で apply の role を取って workload root を apply する。path で絞らない。
`on.push.branches` は `branches.json` から `make repo-settings-apply` が生成し、`make repo-settings-check` が
ずれを落とす（[`.github/settings/README.md`](../settings/README.md) の「branches.json」）。

- **管理アカウントへ apply するのは `admin` だけである**（ADR-0601 決定4）。リリース線（`release/*`）と
  hotfix の線（`hotfix/*`）は変更を集める線で、その push は apply を起動しない。1つの state に複数の
  ブランチが apply すると、遅れたブランチの push が古い構成を apply し直す。environment `admin-apply-identity`
  と `admin-apply` の deployment branch policy は `admin` だけを許す
- **変更は昇格で当てる。** 変更はまずリリース線へ merge し、そのリリース線から `admin` への Pull Request を
  merge すると apply が走る。`admin` 宛の Pull Request でも必須検査と plan が走る。`admin` は Pull Request の
  merge でだけ進む（ruleset）
- **`admin` へ入れてよい commit は、ブランチ名ではなく到達性で決まる。** required の `deploy-source-check` が、
  次をすべて満たす Pull Request だけを通す（ADR-0603 決定31）。head のブランチ名は見ない —— 保護パターンに
  当たるブランチは書き込み権限で作れるが、デフォルトブランチを変えられるのは管理者だけである
  - デフォルトブランチが `admin` の `from` の線（`release/`）のどれかであり、デプロイブランチ自身でない
  - head の commit が `origin/<デフォルトブランチ>` の祖先である
  - `git merge-tree --write-tree <base> <head>` が衝突せず、その木が head の木と一致する

  これにより `admin` の木は、常にリリース線でレビューされた commit の木と一致する。feature のブランチと
  hotfix の線からは `admin` へ入れられない
- **止めるときは、`admin` 宛の Pull Request を merge しない。** 後続の apply は `admin` への merge でだけ起きる
  （ADR-0601 決定19）。デフォルトブランチは動かさない
- **昇格は開くときに承認し、緑ならすぐ merge する。** どのデプロイブランチでも同じである。CI が緑で、plan の行と
  変わる resource の一覧が本文の見込みと一致すれば、すぐ merge する。merge の前に止めるのは ADR-0601 決定10 の
  場合だけである。リリース線への merge は止めない。昇格の Pull Request の CI の間に merge すると、head が進んで
  その CI が走り直すだけで、確かめていない木は入らない（`deploy-source-check`、ADR-0603 決定31）。リリース線への
  merge は次の昇格まで実環境に当たらず、apply の失敗は後続の apply で直る（ADR-0601 決定19）ので、apply の
  終わりも待たない
- **hotfix の間は `admin` へ昇格できない。** `scripts/release` がデフォルトブランチを `hotfix/*` へ移すと、
  デフォルトブランチが `from` の線でないので `deploy-source-check` が落ちる。hotfix を閉じたら、管理者が
  デフォルトブランチをリリース線へ戻し（`gh repo edit --default-branch release/vX.Y.Z`）、hotfix の修正を
  リリース線へ merge してから昇格する
- **Pull Request の plan を持ち越さない。** job が push の commit で plan し直し、その plan に Policy
  （identity root は `plan.stateful_protection`・`plan.kms_key_policy`・`plan.resource_policy_exclusive`・`plan.s3_bucket_namespace`・
  `plan.state_backend_write_deny`、workload root は `plan.stateful_protection`・`plan.s3_bucket_namespace`。ほかの Policy が workload root の実 plan に掛からない理由は `usecases/ops-notification/README.md` の
  保証しないもの）を掛け、通った plan だけを apply する（ADR-0601 決定6、ADR-0604 決定11、
  `.makefiles/terraform/operation/ci.mk` の `CI_APPLY`）。plan は lock を取る。変更の無い plan も apply する
- **古い commit を apply しない。** 再実行は元の run の commit と payload を使うので、job の `if:` は古い
  run を止めない。資格情報を取る前の step（`refuse-stale`）が、起動したブランチの先頭の commit を
  `git ls-remote` で取り直し、run の commit と違えば失敗する。先頭を1つに決められなくても失敗する。lint
  （`make oidc-job-lint`）が見るのは、その step が `head=$(git ... ls-remote ...)` で取って
  `test "$SHA" = "$head" || ... exit 1` で比べる行を持ち、REST API を呼ぶ道具（`gh`・`curl`・`wget`・
  `api.github.com`）を字面に持たないことまでである —— REST の上限は PR の CI と分け合う。どのブランチから
  起動できるかは environment が限る
- **concurrency は workflow 全体に1つ置き、実行中の apply を止めない。** 待っている run は後から来た
  run に置き換わるので、線の途中の commit は apply されず、最新の commit だけが apply される
- **公開されるログに terraform の出力を出さない。** ログには plan と apply の終了コード・件数と、全文を置いた
  object の key・SHA-256・版 ID と、許可の文法に当たる診断の行だけを出し、job summary には件数と apply の成否と
  同じ key・SHA-256・版 ID と診断の行だけを出す（plan の job と同じ。失敗の経路でも job summary を書く）。apply の全文は、identity root なら plan の全文と同じ Bucket の
  `apply/` の下へ apply-identity の role が置き、workload root なら apply の出力の Bucket へ apply の role が
  置く（ADR-0602 決定13）。Bucket の名前と key を得た後は、plan・Policy・apply の
  どこで失敗しても全文を置いてから失敗する。apply には job の `timeout-minutes` より短い時間
  （`scripts/tf-local` の `applyTimeout`）で SIGINT を送り、terraform がそれで止まれば全文を置いてから失敗する。
  job の cancel か `timeout-minutes` の到達で止まったときと、置き場への書き込みが失敗したときは、全文を置けない
- **AWS のアカウント ID は environment `admin-apply-identity` と `admin-apply` の secret `AWS_ACCOUNT_ID` に置く。**
  無ければ拒否の step で失敗する。置いてから「Re-run failed jobs」で再実行する（先頭の commit のままなら通る）
- 検出の job は `apply-infra-code-gate` として、この workflow に置く。`terraform-gates.yaml` の
  `infra-code-gate` と同名にすると、必須 context を報告する job が2つになる（`make required-check-lint`）
- **apply する木で、resource の作成の順序と層の規則と root の構成を確かめる。** `apply-resource-order-check` が
  `make resource-order-check`（[`conftest/hcl/resource_order/`](../../conftest/hcl/resource_order/README.md)）を、
  `apply-layer-check` が `make layer-check`（[`conftest/hcl/layer/`](../../conftest/hcl/layer/README.md)）を、
  `apply-root-check` が `make root-check`（[`conftest/hcl/root/`](../../conftest/hcl/root/README.md)）を
  走らせ、apply の job はその成功を `needs` と `if:` で待つ。Pull Request では必須検査の `terraform-gates.yaml` の
  `tf-lint` が同じ検査を走らせ、その配線が外れれば `make oidc-job-lint` が落ちる。ただし必須検査は base の
  更新を要求しないので、merge された木は検査されていないことがある。資格情報を取る job が `needs` に置ける job は、次に限る（`make oidc-job-lint`）。`revert-plan` の `needs` には
  検査の job を置かない —— revert は apply の job の結果だけで決まり、検査が落ちた apply の job は `skipped` で終わる
  - 検出の job と、この3つの検査の job。どれも決めた make だけを走らせ、skip も失敗の無視もできない形に限る
  - workload root の apply の job に限り、同じ workflow で同じ env の identity root の apply の job。待つ側の `if:` は、
    その `result == 'success'` を含む。identity root の apply が apply の role の権限と permissions boundary を書き、
    workload root の apply がそれを使うので、同じ run の中で順に当てる
- **root ごとに、plan・apply・lock の解除・lock を残す job を1つずつ置く。** root は `env/<env>/identity/`（identity root）と
  `env/<env>/`（workload root）に限り（ADR-0604 決定5）、それぞれに Pull Request で起動する plan の job が1つ以上、
  apply の job と lock の解除の job と lock を残す job がちょうど1つずつ要る。make の入口は `ci-<種別>-<env>-<root>`
  （種別は `scripts/lib/cientry` の登録簿の種別、`<root>` は `identity` か `workload`）。root を足す変更は、同じ変更でその job を足す。
  配置と job の過不足は `make oidc-job-lint` が、root が呼ぶ usecase と backend の設定（key が ADR-0604 決定7 の値で、
  lock file を使う）は `make root-check` が落とす
- **env ごとに、apply の workflow と lock の解除の workflow と lock を残す workflow を1つずつ置く。** 1つの workflow が複数の env の apply を
  持つと、`on.push.branches` を1つのデプロイブランチに決められない（`make repo-settings-check`）。同じ env の
  identity root と workload root の apply の job は同じ workflow に置く。lock の解除の job と lock を残す job も、それぞれ env ごとの
  workflow にまとめる。workflow のファイル名は変えない —— `apply-revert` は最後に成功した run を workflow の
  ファイル名で引くので、改名はその履歴を断つ
- **workload root の apply の全文は、apply の出力の Bucket に置く。** identity root の全文は plan の出力の
  Bucket の `apply/` に置く。apply の role は plan の出力の Bucket に書けない（`scripts/tf-local` の `ci-prepare` が選ぶ）
- job の形（environment、取る role、起動条件、検出・順序の検査・層の検査への依存、拒否の step、terraform の経路、concurrency、
  `run:` と `env:` に置けるもの）は
  `make oidc-job-lint` が、apply の入口が plan の失敗・終了コードと JSON の食い違い・Policy の違反と評価の欠け
  のどれでも apply を呼ばずに非ゼロで終わることは、`scripts/tf-local` の段のテストが偽物の
  terraform・aws・conftest で段を順に起動して見る

保証しないこと:

- **Pull Request の plan と、apply の plan が同じであること。** 必須検査は base の更新を要求しない
  （`branch-protection.json` の `strict_required_status_checks_policy: false`）ので、merge された木は
  最後に plan された木と違いうる。apply の job の plan と Policy が最後の関門である
- **新しいリリース線を、そのまま `admin` へ昇格できること。** 新しい線は `production` から切る。`admin` が
  新しい線に無い変更を持つと（古い線をリリースする前に新しい線を切った、など）、merge の結果の木が head の木と
  一致せず、`deploy-source-check` が落ちる。回復は、古い線を新しい線へ merge する Pull Request（base merge）で
  その変更の履歴を新しい線へ入れることである。その変更が要らなければ、続けて新しい線の上で revert する。
  そのうえで昇格する
- **GitHub の merge の結果が、`deploy-source-check` が `git merge-tree` で求めた木と一致すること。** 必須検査は
  base の更新を要求しないので、検査の後に `admin` かデフォルトブランチが進むこともある。違えば、apply の失敗の
  revert の判定が `admin` の先頭とその第2親の木の不一致として検出し、revert を用意しない

### lock の解除の job

`terraform-unlock.yaml` の `unlock-admin-identity` と `unlock-admin-workload` は、人が `workflow_dispatch` で起動する。
`unlock-admin-identity` は environment `admin-unlock-identity` の下で unlock-identity の role を assume して、管理アカウントの
identity root の state に残った lock file（`identity/terraform.tfstate.tflock`）を外す。`unlock-admin-workload` は
environment `admin-unlock` の下で unlock の role を assume して、workload root の lock file（`workload/terraform.tfstate.tflock`）を
外す。1回の起動で両方の root を見て、lock の無い root では何もしない。apply の job が timeout・cancel・
ランナーの障害で止まると lock file が残り、以後の apply は lock の取得で落ちる。書き込みの拒否
（ADR-0604 決定9）が入り、bootstrap の一時 IAM user の例外が外れた後は、state Bucket に書ける IAM の主体は CI の role だけなので、人はこの job で外す。

- **unlock-identity と unlock の role は、それぞれの root の lock file を外すことしかできない。** 持つのは、prefix を lock file の
  key に限った state Bucket の一覧と、lock file の読み取りと削除だけである。復号は、state の鍵の Key Policy が
  同じ lock file の object に限って許す。`s3:PutObject` と `s3:DeleteObjectVersion` は持たない
  （deployment-identity/ADR-0201 決定9・11）。environment は apply の environment と分け、デプロイブランチ
  `admin` だけを許す（ADR-0602 決定13-14）
- **apply の concurrency の group に入らない。** この workflow は自分の group を `cancel-in-progress: false`
  で持つ。apply の run を待たせも取り消しもせず、lock の解除の run どうしは直列になる。apply に対する安全性は
  次の3つが担う
  - **閾値**: 消すのは、apply の job の `timeout-minutes` に余裕を足した時間より古い lock だけである。apply の
    job はその時間を超えて走らないので、走っている apply の lock は閾値に届かない。古さは S3 が記録した
    lock file の `LastModified` で測る。閾値は `scripts/tf-local` の定数で、apply の job の
    `timeout-minutes` とのずれは tf-local のテストが見る
  - **読んだ版の条件**: 読んで判定した版だけを、その ETag を条件にした削除（`delete-object --if-match`）で
    消す。判定の後に lock file が置き換わっていれば、削除は失敗する。`ci-unlock` がこの形で消すことは
    `scripts/tf-local` の段のテストが見る
  - **Terraform の lock の取得**: Terraform v1.17.1（版は `mise.toml`。`scripts/tf-local` の `lockFormatTerraform`
    との一致は tf-local のテストが見る）の S3 backend は、lock file を `If-None-Match: *` の
    条件付きの書き込みで作る（`lockWithFile`）。lock file が在る間は apply の lock の取得が失敗するので、
    2つの apply が同時に lock を持つことは無い。この性質を見る検査はリポジトリに無い

  group が同じ root の apply の job の group と違うことは、`make oidc-job-lint` が workflow をまたいで照合する
- **新しい lock は消さない。** ID などを出して失敗する。lock が無ければ何も消さずに成功する
- **CI のランナーが apply の job か lock を残す job で取った lock だけを消す。** apply の job は plan で lock を取ってから apply
  し、lock を残す job は plan で lock を取って止まるので、`Operation` が `OperationTypePlan` と `OperationTypeApply` の lock を消す。`Who` が `runner@` で
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
- **外した後、lock で落ちた apply の run を「Re-run failed jobs」で再実行する。** `admin` の先頭の
  commit のままなら `refuse-stale` を通る
- **`admin` から起動する**（`gh workflow run terraform-unlock.yaml --ref admin`）。hotfix の間も同じである。
  古い commit の拒否（`refuse-stale`）と検出への依存（`unlock-infra-code-gate`）は、apply の job と同じで
  ある。job の `if:` は ref で絞らず、起動できるブランチは environment `admin-unlock-identity` と `admin-unlock` が `admin` だけに
  限る。`admin` 以外から起動した run を environment が失敗にするか skip にするかを、GitHub の文書は述べていない
- **AWS のアカウント ID は environment `admin-unlock-identity` と `admin-unlock` の secret `AWS_ACCOUNT_ID` に置く。** 無ければ
  拒否の step で失敗する
- 通知は持たない。人が起動し、結果を画面で見る
- 許す AWS の宛先は `aws-state` クラス（STS と S3）だけである。egress の `root` はこの job も名指すが、
  型の宛先は `ci-unlock-*` だけを呼ぶ job に求めない（「ランナーの封じ込め」節）
- job の形は `make oidc-job-lint` が、`ci-unlock` が lock の無いとき以外は lock を消すか非ゼロで終わり、
  新しい lock・置き換わった lock・読めない lock・人の手元の apply の lock を消さないことは `scripts/tf-local` の段のテストが
  偽物の aws で見る

保証しないこと:

- **CI のランナーの plan と apply の lock が、apply の job か lock を残す job のものであること。** `Who` と `Operation` は lock の
  書き手が書く値で、`runner@` で始まる利用者がそれらの job の外で取った plan と apply の lock も、閾値を超えれば消す
- **読めない lock file を外すこと。** 中身を Terraform の lock として読めない lock file と、版 ID の無い
  lock file は、消さずに失敗する。一時 IAM user の書き込みが拒否された後は、
  それを外す手順がこのリポジトリに無い（ADR-0602 決定9）
- **起動できる人を絞ること。** write 権限を持つ人なら起動できる。environment の reviewer は置かない ——
  障害のときに人が1人しか居なくても外せるようにする。実行されるのは `admin` の先頭の workflow と
  コードだけで、できることは閾値を超えた lock file 1つを消すことに限られる

### lock を残す job

`terraform-lock-hold.yaml` の `hold-admin-identity` と `hold-admin-workload` は、人が `workflow_dispatch` で起動し、
それぞれの root の apply の job と同じ environment と role（identity root は `admin-apply-identity` と apply-identity、workload root は
`admin-apply` と apply）を取って、管理アカウントの root の state に lock を取り、外さずに残す。1回の起動で両方の root に lock を
残すので、閾値を過ぎて外すまで、両方の root の apply が止まる。lock の解除の role の権限（lock file の一覧・読み取り・削除と、その復号）は
lock が残る障害が起きるまで使われないので、bootstrap の段5 でこの job が残した lock を `Terraform Unlock` で外し、
障害の前に一度実物で評価する（ADR-0604 決定8）。Actions の画面で apply の run を取り消しても lock は残らない ——
terraform は取り消しの信号（SIGINT・SIGTERM）で lock を外してから止まる。

- **lock は terraform 自身に取らせる。** `CI_HOLD`（`.makefiles/terraform/operation/ci.mk`）は init の後、lock が
  無いことを確かめてから、lock を取る `terraform plan`（`-parallelism=1`）を裏で起動する。plan の全文に lock を
  取った後の行（resource の `Refreshing state...` か data source の `Reading...`）が現れたら SIGKILL で止める。
  lock file は Terraform の S3 backend が書くので、`Terraform Unlock` が相手にするのは障害で残る lock と同じ形の
  lock である。待つ判定は `scripts/tf-local` の `ci-hold-plan` が持つ
- **残った lock を確かめてから成功する。** 止めた後に lock file の一覧を取り直し、lock が残っていて、`Who` が
  `runner@`・`Operation` が `OperationTypePlan` の、lock の解除が消す形であることを `tf-local ci-hold-verify` で確かめる。
  ログに lock の説明と、`Terraform Unlock` が消せるようになる時刻（`LastModified` に閾値を足した時刻）を出す。
  plan が行を出す前に終わった、止めた後に lock が無い、消せない形の lock が残った、のどれでも失敗する。どの段で
  終わっても、裏の terraform は `ci-hold-plan` が止める
- **起動の前に lock が無いことを確かめる。** 前の apply が残した lock を、この job が残した lock と取り違えない。
  lock が在れば plan を起動せずに失敗する
- **apply しない。** state と AWS の resource を変えず、plan の出力も保存しない。plan の全文はランナーの外に出さない
- **apply の concurrency の group に入る。** workflow 全体の group を、apply の job と字面で同じ
  `terraform-apply-admin` にし、`cancel-in-progress: false` で持つ。apply と並んで走って lock を奪い合わず、
  実行中の apply の後ろで待つ。ただし GitHub は、同じ group で待っている run を後から来た run で置き換える。
  apply の run が待っている間にこの job を起動すると、その apply の run は走らずに取り消され、通知も出ない。
  起動の前に、`admin` で走っている apply の run と待っている run が無いことを確かめる（`docs/setup/bootstrap.md` 段5 手順6）
- **lock を残している間、その root の apply は lock の取得で落ちる。** 落ちた apply は通知され、lock の失敗は
  revert しない（「apply の失敗の revert」）。`Terraform Unlock` が消せるのは閾値を超えた後なので、それまでは
  apply が止まる。外した後、`admin` の先頭の commit の apply の run を再実行する
- **`timeout-minutes` は閾値の apply の job の時間を超えない。** この job が閾値より長く生きると、生きている
  plan が持つ lock を lock の解除が消しうる。`make ci-hold-*` を走らせる job の `timeout-minutes` が
  `scripts/tf-local` の閾値以下であることは、tf-local のテストが見る
- **lock を残す workflow に、ほかの CI の入口を持つ job を置かない。** lock の解除の workflow にも lock を残す job を
  置かない。同じ run の中で、lock を残す処理と apply か lock の解除が並んで走る（`make oidc-job-lint`）
- **`admin` から起動する**（`gh workflow run terraform-lock-hold.yaml --ref admin`）。古い commit の拒否
  （`refuse-stale`）と検出への依存（`hold-infra-code-gate`）は apply の job と同じである。順序・層・root の
  検査には依存しない —— apply しないからである
- **AWS のアカウント ID は environment `admin-apply-identity` と `admin-apply` の secret `AWS_ACCOUNT_ID` を読む。** apply の job と
  同じ secret である
- 通知は持たない。人が起動し、結果を画面で見る
- 許す AWS の宛先は、その root の apply の job と同じクラスである。`ci-hold-*` は terraform を
  呼ぶ入口なので、egress の `root` に名指され、型の宛先を求められる（「ランナーの封じ込め」節）
- job の形（role・environment・concurrency の group の apply の job との一致、起動条件、拒否の step、入口）は
  `make oidc-job-lint` が、lock を残す入口の段が apply も削除も書き込みもせず、lock を確かめるまで成功しないことは
  `scripts/tf-local` の段のテストが偽物の terraform と aws で見る

保証しないこと:

- **plan が lock を持つ時間が、待つ判定と SIGKILL に間に合うこと。** plan が lock を取った後の行を出してから
  終わるまでの間に止められなければ、lock は残らず job は失敗する。起動し直せば足りるが、繰り返し失敗するなら
  root の plan が速すぎる
- **SIGKILL で止めた terraform の provider のプロセスが、すぐに止まること。** 片付けはランナーの job の終わりに
  任せる

### apply の失敗の revert

`terraform-apply.yaml` の apply の job が commit の宣言を AWS に対して評価した後に失敗すると、`revert-plan` が、
最後に成功した apply の commit へ宣言を戻す revert のブランチを用意するかを決め、`revert-push` がそれを push する。
Pull Request は人が通知の URL から開き、通常の経路（リリース線への検査・review・merge と、`admin` への昇格と
apply）で戻す。CI は Pull Request を
作らず、merge もしない。前の commit を apply し直すことはしない。決定は ADR-0601 決定17-19 が持ち、ここは
その運用の細則を持つ。

- **戻すのは、commit の中身が原因でありうる失敗だけである。** apply の入口（`.makefiles/terraform/operation/ci.mk` の `CI_APPLY` が起動する
  `scripts/tf-local` の段）が失敗した段を apply の job の outputs の `failure` に書き、`scripts/apply-revert` が読む。plan・Policy・apply・
  apply の打ち切りは戻す。init までの準備、plan と apply の lock の取得、apply が終わった後（全文の置き場への
  書き込みを含む）の失敗と、`refuse-stale` の拒否・資格情報の失敗・検出・順序の検査・層の検査の失敗（どれも apply の step に
  届かない）は戻さない。先に失敗した段は、その後に全文を置けなくても書き換えない。
  apply の step の結果か失敗の段を受け取れなければ、対象外と読まずに `revert-plan` が失敗する
- **戻す範囲は、最後に成功した apply の commit から失敗した commit までの first-parent の merge のすべてである。**
  待っている run は後から来た run に置き換わるので、1回の apply が複数の merge を含みうる。最後に成功した commit は、
  同じデプロイブランチ（`admin`）への push で起動した run のうち、apply の job が成功した最も新しい run の commit で、Actions の
  run と job の記録から読む（environment の deployment は `deployments: write` を持つ主体が API で作れるので読まない）。
  直近の 100 件に無い、祖先でない、first-parent の鎖が届かない、merge でない commit を挟む、木が同じ、一時の
  worktree で範囲を revert すると衝突するか木が一致しない —— いずれも用意せず、理由を通知する。
  木が同じなら失敗の原因は commit の中身に無く、revert を merge した後の apply が失敗しても次の revert は作られない
- **revert は `admin` の上では作らず、失敗した merge が運んだリリース線の commit の上で作り、デフォルトブランチへ
  向ける**（ADR-0601 決定17）。`admin` の先頭 `H` は昇格の merge で、その第2親 `H^2` が運んだリリース線の
  commit である。`revert-plan` は、デフォルトブランチが `admin` の `from` の線であること、`H^2` が
  `origin/<デフォルトブランチ>` の祖先であること、`H^2` の木が `H` の木と一致することを確かめる
  （`deploy-source-check` と同じ判定）。そのうえで範囲の merge を `H^2` の上で新しい順に `-m 1` で revert し、
  木が最後に成功した commit の木と一致することを見る。どれかが成り立たなければ用意せず、理由を通知する ——
  hotfix の間に失敗した apply もこれに当たり、hotfix を閉じた後に人が用意する。通知の URL が開く Pull Request の
  宛先はデフォルトブランチで、merge した revert は昇格で `admin` へ運ぶ。リリース線の先頭が `H^2` より進んで
  いれば revert の Pull Request が衝突しうるので、人が Pull Request の上で解く
- **`admin` の最初の apply が失敗したときは、revert を用意しない。** 成功した apply の run が無いので、
  `revert-plan` は理由を通知する。人は、リリース線で直して昇格するか、リリース線の先頭の上で
  `git revert -m 1 <失敗した admin の merge>` を行う Pull Request を開き、merge してから昇格する
- **identity root の apply が、同じ apply で足した権限の反映の遅れで落ちたときは、revert を使わずに再実行する。**
  role や Bucket を足す変更は、apply-identity の inline policy を書いてから作るが、その書き込みが IAM の評価へ
  反映される前に作成が評価されると `AccessDenied` で落ちる（usecases/deployment-identity/README.md の検証方法）。
  失敗の段は `apply` なので revert のブランチが用意されて通知が来るが、その Pull Request は開かない。run の画面の
  「Re-run failed jobs」で再実行し（`admin` の先頭が同じ commit なら `refuse-stale` を通る）、緑になったら
  用意された `revert/apply-<run>` のブランチを消す。再実行も同じ `AccessDenied` で落ちるなら反映の遅れではないので、
  止まって通知の revert を使うかを判断する
- **判定は `revert-plan` が持ち、書き込みの token を持つのは `revert-push` だけである。** `revert-plan` は
  `contents: read` と `actions: read` で、範囲を一時の worktree で実際に revert して木の一致を確かめ、同じ名前の
  ブランチの有無も見る。`revert-push` は `contents: write` だけを持ち、道具を導入せず、決まった commit を `revert-plan` が確かめたのと
  同じ順で1回の `git revert` に渡し、`--force` を付けずに push する。token は gh の credential helper が
  `GH_TOKEN` から push の1回にだけ渡す。job の形は `make contents-write-lint` が、revert の範囲・順序・`-m 1` は
  `scripts/apply-revert` のテストが `revert-plan` の側で見る。push の入口（`apply-revert.mk`）は git と gh を
  決まった順に呼ぶだけで、テストを持たない（ADR-0702 決定25）
- **ブランチの名前は `branches.json` の `revert` の接頭辞と run の番号から決める**（`revert/apply-<run>`）。
  同じ run の再実行は同じ名前になり、同じ木のブランチが在れば push せずに通知だけを出す。宣言は `repo-settings` と
  `apply-revert` が同じ読み手（`scripts/lib/branchdecl`）で読み、接頭辞が無いか `lines` と重なれば、どちらも落ちる
- **`.github/workflows/` を戻す範囲は CI が push できない。** `GITHUB_TOKEN` は workflows の権限を持てない。
  通知が載せる `make apply-revert REVERT_HEAD=… REVERT_COMMITS='…' REVERT_BRANCH=…` を手元で
  そのまま実行すると、手元の資格情報で同じ revert を push する
- **後続の apply は止めない。** revert が merge される前に別の昇格が入っても、その apply は走る。apply は `admin` の
  先頭の木全体を当てるので、成功すれば Git と実環境は一致し、用意した revert は要らなくなる。失敗すれば次の
  revert が最後に成功した commit から計算し直され、前の revert を含む。止めたいときは、`admin` 宛の Pull Request を
  merge しない
- **通知は失敗の通知と別に届く。** `notify-revert` が critical の deploy の系統へ、Pull Request を開く URL か、
  手元のコマンドか、用意しなかった理由を、検出のモードで送る。失敗の通知（`notify-failure`）は revert の job を
  待たない。revert の対象でない失敗では送らない
- **root を足したら、その apply の job を revert の判定へ足す。** `revert-plan` の `needs`・`if:` の
  `needs.<job>.result == 'failure'`・step の `APPLY_REVERT_RESULTS`（job ごとの result・apply の step の結果・失敗の段）に
  足し、`notify-failure` の `needs` と、その job の成功を通知する job を足す。`APPLY_REVERT_WORKFLOW` は
  その workflow 自身のファイル名である。過不足は `make oidc-job-lint` が workflow ごとに落とす。最後に成功した
  commit は、名指したすべての apply の job が成功した run の commit になる。skipped の job（identity root の
  apply が失敗して待っていた workload root の job など）は戻す対象なしに数え、ほかの job の失敗で判定する。
  revert は commit 単位なので、片方の root だけが失敗しても、成功した側の変更も戻す
- 古い revert のブランチは自動では消えない。後の apply が成功したら、人が消す

保証しないこと:

- **revert の apply が成功すること。** revert が stateful な resource の削除を含めば `plan.stateful_protection` で
  落ちうる。Bucket Policy が CI の role を拒んだ場合（`docs/setup/bootstrap.md` の診断の節）も同じ理由で落ちる
- **`GITHUB_TOKEN` が `.github/workflows/` を変える push を拒むこと。** 権限の一覧に workflows が無いことまでは
  文書で確かめたが、拒否そのものは公式の文書に無い。拒まれなければ、`revert-push` が push するだけである
- **人が URL から開いた Pull Request で、必須検査が承認待ちにならずに走ること。** `GITHUB_TOKEN` が起こした
  イベントの run は承認を要するが、Pull Request を開くのは人である。この経路を通した実例はまだ無い
- **失敗した job の outputs が後続の job へ渡ること。** 渡らなければ apply の step の結果が空になり、`revert-plan`
  が失敗して通知へ出る。黙って働かなくはならない
- **apply の job を足した後の最初の run が失敗したときに、revert のブランチを用意すること。** 最後に成功した commit は、
  名指したすべての apply の job が成功した run から探すので、足した job が一度も成功していなければ戻す先が無く、
  用意しなかった理由を通知する。その run の失敗は、通知の後に人が直す
- **`contents: write` の token がタグと保護されていないブランチへ書けないこと。** 書けるのは `revert-push` の間だけで、
  走るのは `admin` に merge された木の `make ci-revert-push` だけである

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
| `notify.yaml:notify`、`tool-outdated-report.yaml:report`、`partitions-drift-report.yaml:report` | 15 | 実測できる完了 run が無い |
| `md-lint.yaml:adr-lint`、`md-lint.yaml:skill-lint`、`md-lint.yaml:ref-record-lint`、`go-test.yaml:test-mapping` | 15 | 同上（新設） |
| `go-lint.yaml:go-lint`、`go-lint.yaml:go-fmt-check` | 15 | 同上（新設） |
| `terraform-gates.yaml` の `changes` と plan の2 job を除く6 job、`terraform-apply.yaml:apply-infra-code-gate`、`terraform-apply.yaml:apply-resource-order-check`、`terraform-apply.yaml:apply-layer-check`、`terraform-apply.yaml:apply-root-check` | 15 | 同上（新設） |
| `terraform-unlock.yaml` の3 job | 15 | 同上（新設）。apply の job と違い terraform を呼ばない |
| `terraform-apply.yaml:revert-plan`、`terraform-apply.yaml:revert-push` | 15 | 同上（新設）。terraform を呼ばない |
| `terraform-gates.yaml` の plan の2 job、`terraform-apply.yaml` の apply の2 job | 30 | AWS の API を呼ぶ。role の session（3600 秒）より短く置く |
| `sonarcloud.yaml` の3 job、`sample-remove-check.yaml:sample-remove-check` | 15 | 同上（新設） <!-- sample:line --> |
| `checkov.yaml:checkov`、`kics.yaml:kics`、`semgrep.yaml:semgrep` | 15 | 実測できる完了 run が無い（新設） |
| `secret-scan.yaml` | 15 | 実測は Pull Request のみ。週次は履歴全体を走査し、完了 run が無い |
| `hygiene.yaml` の2 job、`scorecard.yaml:scorecard` | 15 | 実測できる完了 run が無い（新設） |
| `repo-settings.yaml:repo-settings-check`、`repo-settings.yaml:deploy-source-check` | 15 | 同上（新設） |
| `api-budget.yaml:api-budget` | 15 | 同上（新設） |

限界に触れ始めた job は実測を追い越している。数字を小突かず、測り直して式を当て直す。
再利用可能 workflow を呼ぶ job は `timeout-minutes` を持てない（キーが無効）ので、検査は除外し、
上限は呼ばれる側の job が持つ。

3つの規則が1つの検査に同居しているのは、これが3つの方針ではないからである —— 上限の無い job が
打ち切りを生み、2つのコメント規則がそれを読めるものにする。どれか1つだけ直しても Pull Request は
何も良くならない。

## 通知の起動条件

通知は受け手ごとに5つの系統に分かれる。deploy の3つは、環境の役割（`docs/v1.0-plan/scope.md` 第10節）で分ける。

| 系統 | 送るもの |
| --- | --- |
| セキュリティ | 走査（`govulncheck.yaml`・`secret-scan.yaml`・`trivy-fs.yaml`・`zizmor.yaml`・`scorecard.yaml`）の失敗と検出 |
| critical の deploy | 管理アカウント（admin）と本番（prd）の apply の成功と失敗 |
| promotion の deploy | 本番へ上がる途中の段（dev・stg）の apply の成功と失敗 |
| validation の deploy | 検証の器（sandbox）の apply の成功と失敗 |
| 保守 | 上流の報告（`partitions-drift-report.yaml`）の失敗 |

critical は、壊れると他の環境へ響く環境である。本番は利用者へ、管理アカウントは state backend と CI の
role を通じてすべての環境へ響く。env を足すときは、その役割に合う系統へ割り当てる。

系統ごとの secret の名前と、`notify.yaml` を呼ぶ job の系統は
[`.github/notify-channels.toml`](../notify-channels.toml) が宣言する。`make notify-channel-lint`（`make actions-lint`
に含まれる）が、`notify.yaml` を呼ぶすべての job がその系統の secret を渡していることを確かめ、宣言に無い
呼び出しと、呼び出しの無い宣言を落とす。

- **系統は、呼び出し側が `notify.yaml` の `slack-webhook-url` へ渡す secret で選ぶ。** Incoming Webhook は
  URL ごとに宛先のチャンネルが決まるので、`notify.yaml` の secret の入力は1つである。通知の job は
  environment を持たないので、secret はリポジトリの secret である
- **secret が未設定の系統は、notice を出して配信を省く。別の系統へ代わりに送らない** —— `notify.yaml` は
  代わりの宛先を持たず、critical の通知が別の系統の宛先へ届く取り違えは起きない
- **deploy は成功も通知する。走査は成功を通知しない** —— deploy の成功は何がいつ適用されたかの記録であり、
  走査の成功は何も起きなかったことの報告にすぎない。変更の無い apply も成功として通知し、plan の件数の行
  （`No changes.` か `Plan: …`）を載せて変更の有無を見分けられるようにする。件数の行は apply の job の
  outputs で渡り、terraform の出力は含まない
- promotion と validation の系統は、dev・stg と sandbox の apply の job を足す変更が配線し、その通知の job を宣言に足す

スケジュール（週次）を持つ走査は、job が `failure` または `cancelled` で終わったとき `notify.yaml` を
呼ぶ。Pull Request の失敗は作者に見えている。**スケジュールの失敗は誰にも見えない** —— それが
通知の存在理由である。`cancelled` を含めるのは、timeout やランナー障害で殺された job が `failure`
ではなくそちらを報告するため。

報告のみの走査は所見があっても job が緑のままなので、failure モードは決して発火しない。それらは
検出モードで `notify.yaml` を呼び、実行者・ref・commit と所見そのものを名指しする。どのモードも
webhook の secret が未設定なら配信を省いて run を緑のままにする —— 作られたばかりのリポジトリが、
送れない通知で落とされることはない。

どの起動条件で検出通知を出すかは、正しい受け手が誰かから決まる。脆弱性の走査ではスケジュール実行
だけである。Pull Request では所見は既に、それを持ち込んだ作者宛のコメントに在る。**週次の所見は、
止まっているコードに対して新しく公開された勧告であり、放っておくと誰にも届かない。**

| workflow | 発火条件 | 起動 |
| --- | --- | --- |
| `trivy-fs.yaml` | 修正版のある CRITICAL / HIGH / MEDIUM | schedule |
| `govulncheck.yaml` | 運用機構（`scripts/`）の Go のコードの呼び出し経路が届く既知脆弱性 | schedule |

`tool-outdated-report.yaml` は `notify.yaml` を呼ばず、宣言より新しい版が上流に在ることを issue へ書く。
`partitions-drift-report.yaml` は、写しと上流のずれと、上流と比べられなかったことを issue へ書き、比べられなかった回は
job も落とす。issue へ書く前の段（setup・egress・checkout）で落ちた回は issue に現れないので、スケジュールの失敗を
保守の系統へ通知する。

`terraform-apply.yaml` は push で起動するが、スケジュールと同じ理由で failure モードを持つ —— merge の後の
失敗は誰にも見えず、merge したのに apply されない状態を黙って作る。検出・順序の検査・層の検査の job が落ちると
apply の job は `skipped` で終わるので、検査と apply のどの job の `failure` / `cancelled` でも通知する。通知が名指す
job は、workload root の apply が失敗か取り消しならそれ、次に identity root の apply が走ったならそれ、どちらも走らなければ
成功しなかった検査の job（検出の job を先に見る）である。
apply の job が `success` で終わったときは、apply の job ごとの `notify-success-<env>-<root>` が成功のモードで通知する。revert の結果は
`notify-revert` が検出のモードで別に送る（「apply の失敗の revert」）。`terraform-unlock.yaml` は
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
最初の step（`id: harden-runner`、`continue-on-error: true`）が落ちたときだけ、2番目の step（回復）が
30 秒待ってから `sudo apt-get update` を実行し、同じ設定の3番目の step（`id: harden-runner-retry`）がやり直す。
3番目が落ちれば job は落ちる。bullfrog は agent の取得とパッケージの導入を再試行せず、配布元の一時的な
エラーで必須の job を落とすためである。間を置かないやり直しは効かなかった —— 取得の失敗は 0.5 秒後にも
同じ応答で続き、bullfrog は `apt-get update` をせずに `apt-get install` するので、索引に無いパッケージは何度
やり直しても入らない。回復の step は `continue-on-error: true` と `timeout-minutes: 2` を持ち、失敗しても
止まっても再試行へ進む。job を落とすかは再試行が決める。
再試行が成功するのは、agent が準備の印（`/var/run/bullfrog/agent-ready`）を書いたときだけである。本体が
agent を起動した後で落ちた場合、待った後の再試行は sudo を外された後の導入で落ちうる —— 安全側に落ちるが、
遅れて起動した agent の印で成功する経路は失う。どちらでも、通信制限の無いまま job が進むことはない
（v0.11.1 の `action/src/main.ts` で確かめた。版を上げる Pull Request は確かめ直す。ADR-0702 決定23）。

**status の関数（`always()`・`failure()`・`cancelled()`）を持つ step は、bullfrog の成功を条件の先頭に持つ。**
status の関数は暗黙の `success()` を外すので、本体と再試行が2つとも落ちた job でも走り、そのとき通信制限は
無い。`if:` は `(steps.harden-runner.outcome == 'success' || steps.harden-runner-retry.outcome == 'success') && always()` のように、この条件と `&&` で始め、その後ろで `||` を使うなら括弧で囲む
（`&&` より弱く結ぶので、先頭の条件を迂回する）。`make egress-check` がこの形を求め、`make egress-apply` は
書き換えない（式は人が書く）。status の関数は step の `if:` の式の中にだけ書く —— `- if:` の書き出し、引用符で囲んだキー、
フロースタイル、`run:` の中に現れると、読めない形として落ちる。YAML のアンカーとエイリアスも同じ理由で使わない。job の `if:` は別の job を決めるもので、その job の bullfrog が守るので対象外である。
外向きの接続と名前解決を一覧に照らし、それ以外を拒む —— 侵害された action や道具のダウンロードが、
その job に用の無い宛先へ持ち出せない。sudo を外すのは、sudo が残る job では step の中から通信制限
そのものを外せるからである。決定と受け入れた限界は [ADR-0702](../../docs/adr/0702-repository-operations-substrate.md)
決定20-23 が持つ。

このステップは**すべての job にインラインで置かれる。これは好みではなく制約である。**
ローカルの composite action（`uses: ./.github/actions/*`）はリポジトリを checkout して初めて解決され、
bullfrog は checkout の**前**に走らなければならない —— checkout 自体が外向きの呼び出しであり、
それを守ることが目的だからである。括り出すと、塞ぐために置いた窓が開く。

**固定されていないのは一覧の出所の方である。** [`.github/egress.toml`](../egress.toml) が SSOT で、
`make egress-apply` が各 job の先頭の3つの step を本体の `uses:` を写して丸ごと書き、`make egress-check` がずれで
落ちる。bullfrog の step が `name` / `id` / `continue-on-error` / `if` / `uses` / `with` 以外のキーを持つこと、3つの step が
job の先頭に並んでいないこと、本体と再試行の間が生成した回復の step と一字一句同じでないこと、steps を持つ job に bullfrog が無いこと、steps を読み取れない job（`steps:` は4桁、step は
6桁のハイフンで始める）も、同じ検査が拒む。拒む条件の一覧は
[`scripts/README.md`](../../scripts/README.md) の `egress/` の行が持つ。

**job は宛先ではなく能力のクラスを宣言する。** job が何に到達するかは、その job が*何をするか*から
決まり、job 自身の素性からは決まらない —— 実行は `make` から docker、コンテナ内の `mise` へ降りるので、
必要な宛先は YAML から見えない。

| クラス | 宛先 | 当たるもの |
| --- | --- | --- |
| `base` | GitHub API / web / codeload、`objects` / `raw` / `release-assets.githubusercontent.com`、`*.actions.githubusercontent.com`、結果と cache の置き場のうち bullfrog の agent が既定で許さない番号の `productionresultssa<N>.blob.core.windows.net`（足し方は宣言のコメント）。IP は `127.0.0.100`（理由と外す条件は宣言のコメント。IP はドメインの判定を経ずに通すので、宣言の検査が `127.0.0.0/8` の外を拒む） | **すべての job に暗黙に適用される。** `classes` に書かない |
| `mise` | mise 自身の配布元と、`mise.toml` が解決するすべての backend —— aqua / GitHub releases、Go の toolchain と module proxy、node の配布元、npm レジストリ（npm の道具を lockfile から入れる pnpm と、`npm:` の宣言の公開時刻を引く tool-cooldown が使う）、Terraform Registry（provider の公開時刻と一覧を引く tool-cooldown の job が `extra` で足す）、および Sigstore（mise が artifact attestation を検証する） | 道具を導入するすべての job |
| `go` | mise が Go の版の一覧を引く先、Go の toolchain の配布元、module proxy（`mise` の部分集合） | Go だけを入れて `scripts/` の道具をビルドし、他の道具を入れない job（Checkov / KICS / Semgrep） |
| `terraform` | Terraform Registry と provider の配布元 | Terraform を実行する job（[ADR-0205](../../docs/adr/0205-module-dependency-policy.md) により外部 module は引かない） |
| `trivy` | 脆弱性 DB と checks バンドルの OCI の配布元 | Trivy を実行する job |
| `docker-hub` | Docker Hub の registry・認証・layer の配布元（R2 と S3 は Docker Hub の Bucket のホストだけ） | 上流のイメージで道具を実行する job（Checkov / KICS / Semgrep）、docker/tools のイメージを組む job（Versions Check） |
| `deps-dev` | deps.dev（パッケージの供給網の情報） | 依存を評価する job（Dependency Review、Scorecard） |
| `aws-state` | STS・S3 の endpoint（region と wildcard の理由は宣言のコメント） | OIDC で role を assume し、state の Bucket に触れる job（lock の解除の job） |
| `aws` | `aws-state` に加えて IAM・KMS・S3 Control の endpoint | Terraform で AWS の API を呼ぶ job |
| `aws-notify` | `aws` に加えて SNS・EventBridge Scheduler・Chatbot の endpoint | 通知の構成物を持つ workload root を Terraform で扱う job |

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
job が実際に実行する root は、job が呼ぶ CI の入口 `make ci-<種別>-*`（種別は `scripts/lib/cientry` の登録簿の種別）を
`.makefiles/terraform/operation/ci.mk` の呼び出しの引数から読み（配置は ADR-0604 決定5）、`root` の名指しと食い違えば落ちる。
`type` の宛先を求めるのは、その root で Terraform を実行する種別（登録簿の `Terraform`）の入口を呼ぶ job だけである ——
Terraform を実行しない種別の入口だけを呼ぶ job には求めない。区別は呼ぶ入口から導くので、lock の解除の job が
Terraform を実行する種別の入口も呼べば、その job は型の宛先を求められる。root を名指す job のどれも Terraform を
実行する種別の入口を呼ばなければ落ちる。region を含む
`type` の宛先の region の集合が、その `type` に当たる型のブロックを置く region の集合（ブロックの `region`
引数のリテラル。持たないブロックは、辿る aws の provider のただ 1 つの `region`）と一致しなければ落ち、
ブロックの `region` 引数（`action` は `config` の中）が region の形のリテラルでなくても、`action` の `region` を
読めない形（`config` の外の `region`、2 つ以上の `config`、dynamic の `config`）でも落ちる。`region = each.key` だけは、
ブロックの `for_each` を module の呼び出しの文脈で解いた集合の各 region として読み、解けない形・空の集合・region の形でない
要素で落ちる（解ける形は `scripts/README.md` の `egress/` の行）。region の集合の宛先は、宣言の `region_set` の集合と
宛先の `{<set>}` で書く（`[region_set.<name>]` の `regions = ["us-east-1", ...]` と、宛先の `"ec2.{<name>}.amazonaws.com"`。
1つの宛先に `{}` は1つで、どの宛先も使わない集合は落ちる）。`usecases/*/reference` の root も辿り、ブロックの `region` を解けなければ落ちる（宛先は求めない）。
AWS が region を固定した global のサービスの宛先は、`scripts/egress` の閉じた一覧が突き合わせから外す。job の外（`jobs:` より前の `env:` など）に入口の名前が在っても落ちる。
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
| `upsert-report-issue` | 週次の報告を、title を同一性の鍵にした1件の issue へ書く。呼び出し側が `open`（作成・更新）・`close`・`keep`（何もしない）を選ぶ |

### mise の導入

mise を要する workflow は [`setup-mise`](../actions/setup-mise/action.yaml) を使う。action は pin された
mise のバイナリをリトライ付きでファイルへ落とし、版と digest でキャッシュし、実行の前に毎回 SHA256 を
検証する。**復元されたというだけでキャッシュを信用しない** —— 不一致は捨てて取り直し、2度目の
不一致で job を落とす。

道具の版は [`mise.toml`](../../mise.toml) に残る。呼び出し側が渡すのは、必要な道具の指定だけである。
action は道具を `MISE_LOCKED` で [`mise.lock`](../../mise.lock) の取得元と照合値からだけ入れ、版と取得元を
GitHub API に問い合わせない —— 問い合わせると、並ぶ Pull Request の CI が `GITHUB_TOKEN` の API の上限を
分け合い、上限に当たった job が落ちる（ADR-0503 決定2）。lock に無い道具と宣言と違う版は入れずに落ちる。
版を変えたら `make mise-lock` で lock を書き直す。lock と宣言のずれは `make versions-check` が落とす。
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
