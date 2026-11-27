# リポジトリ設定

このディレクトリには、リポジトリ単位の GitHub 設定を JSON で格納しています（分岐のパターン、ブランチルールセット、Pull Request の gate が呼ぶ検査、デプロイの environment、Private Vulnerability Reporting、ラベル定義）。このボイラープレートから派生したリポジトリを、Web UI をクリックして回るのではなく再現可能な形で構成するためのものです。

## 宣言と実態の突合

保護設定 —— `rulesets/`（並行期間は `branch-protection.json` も）、`environments.json`、`private-vulnerability-reporting.json` —— は、宣言から適用し、実態と突き合わせます（ADR-0603 決定4-6）。判定は `scripts/repo-settings` が持ちます。

| 操作 | make | 書き換えるもの | 誰が |
| --- | --- | --- | --- |
| 生成 | `repo-settings-apply` | ruleset の宣言の生成する区間（「rulesets/」）と、apply の workflow の `on.push.branches` と、印 `# branch-set: <集合>` の行 | 誰でも |
| 宣言の整合 | `repo-settings-check` | 無し | CI（required）と pre-commit |
| 突合 | `repo-settings-drift` | 無し（GET のみ） | 人が手元で（誰でも） |
| 適用 | `apply-branch-protection` / `apply-environments` / `apply-vulnerability-reporting` | GitHub の ruleset / environment / Private Vulnerability Reporting | 管理者の資格情報を持つ人 |
| デプロイブランチの作成 | `create-deploy-branch DEPLOY_ENV=<env>` | GitHub のブランチ（env のデプロイブランチを、デフォルトブランチの先頭から作る。在れば失敗する） | 管理者の資格情報を持つ人（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の段4 で1回） |
| デプロイブランチの確認 | `deploy-branch-exists DEPLOY_ENV=<env>` | 無し（GET のみ。env のデプロイブランチが無ければ失敗する） | 人が手元で（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の段4 の apply の前） |

- 宣言を変える Pull Request は、merge の後に管理者が適用します。CI は保護設定を書き込みません（ADR-0603 決定30）。適用は `make setup-repo` も呼びます。
- 突合は人が手元で実行します。保護設定は人が管理者の資格情報で守るもので、CI の定期実行で重ねて見ることはしません。適用の後と、管理画面で設定が変わったと疑うときに `make repo-settings-drift` を実行し、ずれがあれば失敗します。直すのは、宣言を実態へ寄せる Pull Request か、宣言の再適用です（ADR-0601 決定13）。
- ruleset は名前で実態と対応させます。`make apply-branch-protection` は、宣言の ruleset ごとに、実在しなければ POST、ずれていれば PUT を組んで送ります。宣言に無い名前の ruleset は消さず、その名前を出すだけです。`make repo-settings-drift` はそれをずれとして報告し、人が消すか宣言へ足します。同じ名前の ruleset が実態に2件あれば、突合も適用も失敗します（適用は何も送りません）。
- 適用は、宣言に無い environment を消しません。消すと secret ごと失われるためで、突合がそれを報告し、人が消すか宣言へ足します。
- **読み取りの権限の突合は ruleset の `bypass_actors` を見ません。** GitHub はこの値を ruleset への write 権限を持つ呼び出しにしか返しません（ADR-0603 決定6）。その権限が無ければ、突合は「対象外」と出力し、比べたことにはしません。`bypass_actors` が空であることは、宣言の側を `repo-settings-check` が見て、実態の側は管理者が自分の資格情報で `make repo-settings-drift` を実行したときに見ます。

## branches.json

分岐のパターンの単一宣言です（ADR-0603 決定1）。`deploy` はデプロイブランチ（固定名で保護するブランチ）の一覧、`lines` は接頭辞で表すブランチの線（`/` で終わる）を役割ごとに持ちます。保護対象は両者の和です。`revert` は apply の失敗を戻す revert のブランチの接頭辞（`/` で終わる）で、保護しません。無いか `lines` の接頭辞と重なれば `repo-settings-check` が落ちます（[`.github/workflows/README.md`](../workflows/README.md) の「apply の失敗の revert」）。

| キー | 意味 |
| --- | --- |
| `lines.release` | リリース線の接頭辞。`make base-branch` が最新を選び、`make branch-*` が切る線です |
| `lines.hotfix` | hotfix の線の接頭辞。`make hotfix-patch` が切る線です |
| `line_version` | 線の名前のうち接頭辞に続く版の形。受け付けるのは `v<major>.<minor>.<patch>` だけで、タグの版も同じ形です |

役割は `release` と `hotfix` の2つで、どちらも欠かせず、同じ接頭辞を持てません。

**デプロイブランチ**は、env ごとに1つ置く固定名の保護ブランチで、そこへの merge がその env への apply を起動します（ADR-0601 決定4）。リリース線（`release/*`）と hotfix の線（`hotfix/*`）は変更を集める線で、デプロイブランチを持つ env へ apply しません。`deploy` の各要素は次の形です。

```json
{ "branch": "admin", "env": "admin", "from": ["release/"] }
```

| キー | 意味 |
| --- | --- |
| `branch` | デプロイブランチの名前。保護対象に入ります |
| `env` | そのブランチの env。`deployment-identity` の env のどれかで、その env の environment の宣言を要します（`repo-settings-check` が見る）。そのブランチへの merge が apply するのは、その env の root（`env/<env>/`）と apply の job が在るときだけです |
| `from` | そのデプロイブランチが変更を受け取る線の接頭辞の一覧。各要素は `lines` のどれかで、重複できません。`env` を持つ要素だけが持ち、持つときは1つ以上が必須です |
| `release_base` | タグを打ち、新しい線を切る基点のブランチの印。宣言にちょうど1つ置きます |

- デプロイブランチへ入る Pull Request は、デフォルトブランチがその `from` の線のどれかであり、head の commit がデフォルトブランチから到達でき、merge の結果の木が head の木と一致するときだけ merge できます（ADR-0603 決定31）。これを見るのが required の `deploy-source-check` です（[`.github/workflows/README.md`](../workflows/README.md) の「apply の job」）。
- `env` を持つ要素は、`env/` の root が `deployment-identity` へ渡す `env` とちょうど対応します。`env` ごとにデプロイブランチが1つ在り、`env` を持つ要素がどれも `deployment-identity` の `env` を指すことを `repo-settings-check` が見ます。
- `branch` の重複、`env` の重複、`lines` に無いか重複する `from`、`env` を持つ要素の空の `from`、`env` を持たない要素の `from`、`lines` の役割の欠けと役割どうしの接頭辞の重複、`v<major>.<minor>.<patch>` でない `line_version`、`release_base` が0件か2件以上であることは、`repo-settings-check` が落とします。

読み手は次のとおりです。いずれも同じ読み手（`scripts/lib/branchdecl`）を通ります。

- ここから生成するもの: ruleset の宣言の include と `deploy-<branch>.json`（「rulesets/」）、environment の deployment branch policy、workflow の起動条件。apply の workflow（job の `environment:` が、`push` で起動する種別の role の `<env>-<role>` のもの。`make ci-hold-*` を走らせる lock を残す job は、同じ environment でも `workflow_dispatch` だけで起動するので数えない）の `on.push.branches` はその env のデプロイブランチ1つで、ほかの workflow は印 `# branch-set: <集合>` を持つ行を生成します（集合の一覧は [`.github/workflows/README.md`](../workflows/README.md) の「起動の方針」）。`make repo-settings-apply` が書き換え、`repo-settings-check` がずれを落とします
- 実行時に読むもの: `scripts/ci-gate`（gate と線の検証。base が線かデプロイブランチか、`plan_env` の env のデプロイブランチの `from`。Pull Request では base の commit の木から読む）、`scripts/repo-settings` の `source`（`deploy-source-check`）と `deploy-branch`・`create-deploy-branch`・`deploy-branch-exists`（`make create-deploy-branch`・`make deploy-branch-exists`）、`scripts/apply-revert`（`revert` の接頭辞と、デプロイブランチの `from`）、`scripts/base-branch`（`lines.release` と `line_version`）、`scripts/release`（`release_base`、線の役割の接頭辞と `line_version`）、`scripts/repo-setup`（`env` を持たないデプロイブランチ、`release_base`、線の接頭辞）

## environments.json

デプロイの environment の宣言です。宣言は実態の environment の全集合で、宣言に無い environment が実在すれば突合が報告します —— 存在しない environment を参照した workflow は、その名前の environment を保護なしで作るためです。

| キー | 意味 |
| --- | --- |
| `name` | environment の名前。`<env>-<role>`（role は `scripts/lib/cientry` の登録簿の role）は `usecases/deployment-identity` の role の信頼条件が名指す名前の写し。`env/` の root が `deployment-identity` へ渡す `env` ごとに登録簿のすべての role の名前が揃い、どの `env` にも当たらない `<env>-<role>` の名前が無いことを `repo-settings-check` が見る（ADR-0501 決定5 の但し書き）。登録簿の role と `deployment-identity` が作る role の過不足の無い一致は `lib/cientry` のテストが見る |
| `deploy_from` | `protected`（保護対象のブランチだけ）、`protected-and-pull-requests`（同 + Pull Request の ref `refs/pull/*/merge`）、`deploy-branch`（名前の env のデプロイブランチ1つだけ。`deployment-identity` の `<env>-<role>` の名前にだけ使えます）、`any`（deployment branch policy を持たない） |
| `can_admins_bypass` | 管理者による保護の迂回。`false` だけを受けます（ADR-0602 決定14） |

- パターンはこのファイルに書きません。`deploy_from` の種類から、`branches.json` を都度展開します。environment のパターンの `*` は `/` に当たらないので、`lines` は1階層（`release/*`）だけを許します。ruleset が保護する集合より狭く、許可の側は狭い方へ倒れます。
- 名前の接尾辞の role が `deploy_from` を決めます。登録簿で `pull_request` で起動する種別の role の名前は `protected-and-pull-requests` だけ、ほかの role の名前は `deploy-branch` だけを受け、`repo-settings-check` が見ます（ADR-0602 決定14）。`pull_request` で起動しない種別の environment は、保護パターンに当たる新しいブランチからは使えません。
- 新しい environment を参照する workflow は、その environment を `make apply-environments` で宣言から作り、必要な secret を置いてから merge します。先に merge すると、走った workflow が GitHub に保護なしの environment を作らせます。手順は [`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の段5 が持ちます。
- 承認者と待機時間は持たず、突合は実態にそれらが無いことを要求します。ADR-0601 決定7 が任意とする機構を使わない選択です。
  - このリポジトリで承認者を置かないのは、単独メンテナで承認できる人が自分しかいないためです。 <!-- sample:line -->
- `copilot` は Copilot coding agent が作る environment です。宣言に含め、管理者の迂回を無効にします。
- `can_admins_bypass` は GitHub の REST の文書に載っていない値です。適用はこれを送り、突合は応答の値を見ます。適用の後も突合が「迂回が有効」と報告するなら、environment の設定画面で "Allow administrators to bypass configured protection rules" を外してください。

## rulesets/

ブランチの ruleset の宣言です。役割ごとに1ファイルを置き、すべての保護対象に重ねます（ADR-0603 決定35）。ファイル名から `.json` を除いたものが ruleset の `name` で、突合は ruleset をこの名前で実態と対応させます。

| ファイル | 書き手 | 対象（include） | 持つルール |
| --- | --- | --- | --- |
| `common.json` | 人。include の区間だけを `make repo-settings-apply` が生成する | 保護対象の全集合 | `deletion`・`non_fast_forward`・`pull_request`・`code_quality` |
| `lines.json` | 人。include の区間だけを生成する | `env` を持たないデプロイブランチと各線（ADR-0603 決定36） | `required_status_checks`（`gate`） |
| `deploy-<branch>.json` | ファイル全体を、`branches.json` と `checks.json` から生成する | `env` を持つデプロイブランチ1つ | `required_status_checks`（`deploy-source-check`、`checks.json` でその env を `plan_env` に持つ検査の `contexts`、`line-gate` の順） |

並行期間は、古い単一の ruleset の宣言 `branch-protection.json` も残ります。include の区間を同じく生成し、`rulesets/` と並べて適用し突き合わせます。この ruleset はすべての保護対象に、上の4つのルールと、検査ごとの context を required とする `required_status_checks` を課します。並行期間の形と外す条件は [`.github/workflows/README.md`](../workflows/README.md) の「必須検査はすべての Pull Request で報告される（並行期間）」が持ちます。

生成する区間と `deploy-<branch>.json` を手で書き換えると `repo-settings-check` が落ちます。それ以外の値は手で書きます。`repo-settings-check` はあわせて次を落とします。

- `name` がファイル名と違う、役割が持つべきルール（`common` は `deletion`・`non_fast_forward`・`pull_request`、`lines` と `deploy-<branch>` は `required_status_checks`、`branch-protection.json` はそのすべて）が無い
- `common.json` の include が保護対象の全集合と一致しない、ほかの ruleset の include が `common.json` の include の外を含む
- `rulesets/` に、宣言の ruleset のほかの `.json` が在る（適用も突合もされず、消えたはずの保護が宣言に在るように読める）
- `checks.json` の `plan_env` が、`env` を持つデプロイブランチの無い env を名指す（その plan がどこでも required にならない）
- 下の各ルールの宣言内容（承認者数、bypass の列挙、`enforcement`・`target`・`exclude`、`integration_id`）の欠け

各ルールの宣言内容は次のとおりです。

| ルール | 宣言している内容 |
| --- | --- |
| `deletion` | 対象ブランチを削除できない。 |
| `non_fast_forward` | 対象ブランチへの force-push を拒否する。 |
| `pull_request` | 対象ブランチへの変更は PR 経由に限る。承認 1 件、push 時に既存の承認を破棄、最終 push 後の再承認は要求しない、全レビュースレッドの解決、CODEOWNERS レビュー、帰属の無い変更への追加の承認、マージ方法は merge commit に限定（squash と rebase merge を除外。ADR-0603 決定24）。 |
| `code_quality` | GitHub の code quality ルールを `errors` 深刻度でブロックする。 |
| `required_status_checks` | 宣言したチェックがすべて成功してからマージする。各 context は `integration_id`（GitHub Actions）を持ち、Actions 以外が報告した同じ名前の status では満たせない（ADR-0603 決定34）。`repo-settings-check` が欠けを落とす。 |

### 単独メンテナのリポジトリに `pull_request` を適用する場合

GitHub では自分の PR を自分で承認できません。`required_approving_review_count: 1` かつ `bypass_actors` が空の場合、参加者がオーナー 1 人だけのリポジトリではこのルールを満たせる人間が存在せず、保護ブランチ向けの PR がすべて恒久的にマージ不能になります。それでも宣言の値は下げません（ADR-0603 決定21）。メンテナを `bypass_actors` に列挙して解決することもしません（同 決定22）。

### `code_quality` は裏側の機能が先に報告している必要がある

GitHub の案内は、ruleset に Code Quality の閾値を宣言する**前に** Code Quality のワークフローが動作し PR へ結果を報告していることを確認せよ、というものです。確認しないまま宣言すると、このルールが全 PR のマージをブロックし得ます。機能の有効化はこのディレクトリの外にあるリポジトリ単位の操作なので、機能が無効なら無害だと仮定せず、適用前に確認してください。

### Required status check はその ruleset の対象の PR で報告経路を持つ必要がある

required とする context は、その赤が変更そのものについての事実であるものに限ります
（ADR-0603 決定9-11）。継承した状態を報告するチェックは入れません。固定した版の道具の
取得（TFLint の ruleset と、`providers/` の正本が固定する provider を含む）は、取得先の障害で赤になっても
再実行で消え、判定を変えません。例外は次の表の「例外」の行で、どれも required を置く ruleset をデプロイブランチの
ものに限ります。

| ruleset | required の context | 例外 |
| --- | --- | --- |
| `lines` | `gate`。gate が `checks.json` の検査を呼んで判定する（[`.github/workflows/README.md`](../workflows/README.md) の「Pull Request の gate」） | 無し |
| `deploy-<branch>` | `deploy-source-check` | ADR-0603 決定32。デフォルトブランチの名前と到達性、デプロイブランチの今の木に依り、変更に誤りが無くても赤になりうる。デプロイブランチ宛でない Pull Request では対象外として成功する。検査のコードと宣言は Pull Request の base の commit のものを使う |
| `deploy-<branch>` | その env の plan（`deploy-admin` は `plan-admin-identity`・`plan-admin-workload`） | ADR-0603 決定12-14。AWS の状態と資格情報の取得に依存し、変更に誤りが無くても赤になりうる。fork と Dependabot の Pull Request では拒否の step で失敗するので、それらは merge できない |
| `deploy-<branch>` | `line-gate` | ADR-0603 決定39。線への push ごとに、その commit の木で木だけで決まる検査をすべて走らせた結果で、変更の外の理由で赤になると昇格が止まる |

gate が呼ぶ検査の一覧は `checks.json` が持ちます。群に分けると次のとおりです。

| 群 | 検査（`checks.json` の `name`） |
| --- | --- |
| 検査の設定と定義 | `actions-lint`、`commitlint`、`docker-lint`、`egress-check`、`md-lint`、`zizmor` |
| 宣言と実体のずれ | `pin-actions-check`、`pin-images-check`、`pin-runners-check`、`repo-settings-check`、`tool-cooldown`、`go-cooldown`、`versions-check`、`tidy-check` |
| サンプルの除去 | `sample-remove-check` <!-- sample:line --> |
| コードと構成 | `hygiene`、`secret-scan`、`go-lint`、`go-test`、`terraform-gates`、`checkov`、`kics`、`semgrep`、`trivy-config` |
| plan（ADR-0603 決定12-14 の例外） | `terraform-plan-admin` |
| 合否に数えない（`required` が `false`） | `govulncheck`、`trivy-fs`、`trivy-secret`、`trivy-release-gate` |
| 合否に数えない（サンプル） | `dependency-review`、`sonarcloud` <!-- sample:line --> |

**ruleset に context を足したら、それを報告する job がその ruleset の対象の Pull Request で必ず報告することを確かめること。**
GitHub は報告の無い check を「該当しない」とは読まず待ち続け、その pull request は恒久的に
マージ可能にならない。`make required-check-lint` がその不変条件を見ます —— 詳細は
[`.github/workflows/README.md`](../workflows/README.md) の「直接 required とする context は必ず報告される」。
gate に検査を足すときは、ruleset ではなく `checks.json` に足します。

## checks.json

Pull Request の gate と線の検証が呼ぶ検査と、その path の対応の単一の宣言です（ADR-0603 決定17・19・20）。読み手は
`scripts/lib/checksdecl` で、`scripts/ci-gate`（gate と線の検証の判定）、`scripts/repo-settings`（`deploy-<branch>.json`
の生成）、`scripts/required-check-lint`（gate と線の検証の workflow との突き合わせ）、`scripts/sample-remove` が
それを通して読みます。gate と線の検証は、Pull Request では base の commit の、線の検証では push した commit の木の
宣言を読みます。

| キー | 意味 |
| --- | --- |
| `name` | 検査の名前。小文字・数字・ハイフンで、重複できません。gate と線の検証の workflow は、それを job `call-<name>` で呼びます |
| `workflow` | 呼ぶ workflow の `.github/workflows/` の下のファイル名（`.yaml`）。`workflow_call` で起動できなければなりません |
| `paths` | 変更したファイル（base と head の merge-base から head まで。rename は削除と追加に分ける）のどれかが当たれば走る path の一覧。空の配列は常に走ります。`/` で区切った segment の列で、segment は `path.Match` の形か segment 全体の `**`（0個以上の segment）だけを受けます。否定・波括弧・先頭と末尾の `/` は受けません |
| `scope` | `tree`（木だけで決まる検査）か `change`（変更の範囲で決まる検査）。線の検証は `tree` で `required` が `true` の検査だけを呼びます |
| `required` | `true` なら gate の合否に数えます。`false` の検査も走らせて判定の表に載せますが、合否に数えません。省略できません |
| `plan_env` | plan の検査が plan する env。持つ検査は、base がその env のデプロイブランチの `from` の線である Pull Request でだけ走り、線の検証では走りません |
| `contexts` | `plan_env` を持つ検査だけが1つ以上持ちます。その env のデプロイブランチの ruleset（`deploy-<branch>.json`）が直接 required とする context で、`deploy-gate.yaml` の同名の job が報告します |
| `bases` | その検査が走る Pull Request の base の役割（`line`・`deploy`）。省略すると両方で走ります |

- 未知のキー、欠けたキー、名前の重複、形の誤った値、解釈できない path は、どの読み手でも誤りです。読み飛ばすと、その検査が「走るべきでない」と判定されて gate が黙って範囲を縮めます。検査が0件の宣言は、`ci-gate` と `required-check-lint` では合格と区別して終了コード 2 で落ちます。
- `paths` が検査の実際の入力を覆っていることは、どの検査も見ません（ADR-0603 の保証しないこと）。覆っていなければ gate は Pull Request でその検査を省き、`scope` が `tree` なら線の検証が線への push で走らせます。

## private-vulnerability-reporting.json

Private Vulnerability Reporting を有効にするかの宣言です。[`SECURITY.md`](../../SECURITY.md) が脆弱性の報告の経路として案内する機能で、保護設定の1つとして宣言から適用し、突き合わせます。

| キー | 意味 |
| --- | --- |
| `enabled` | 有効にするなら `true`。`true` か `false` のどちらかで、省略と `null` は `repo-settings-check` が落とします |

- GitHub はこの機能を公開のリポジトリにだけ提供します。公開でないリポジトリは `false` を宣言し、`SECURITY.md` の報告先を差し替えます。`true` のままなら、適用は何も送らずに失敗し、突合はずれとして報告します。
- `false` を宣言した公開でないリポジトリでは、突合はこの機能の実態を読まず、「対象外」と出力します。
- 突合がこの機能の実態を読めなかったときは、ruleset と environment の結果を出したうえで失敗します。読めなかったものを対象外に数えません。

## labels.json

`make create-default-labels` が作成するラベル定義（`name` / `description` / `color`）です。`make setup-repo` は先に `make delete-all-labels` を実行するため、このファイルは GitHub が既定で作るラベルへの追加分ではなく、意図するラベル集合そのものです。Dependabot が Pull Request に付けるラベルは `.github/dependabot.yml` の各 ecosystem の `labels:` で名指し、このファイルにも載せます。名指したラベルがこのファイルにあること（大文字小文字を区別しない）は `make repo-settings-check` が検査します。宣言の形（0件でない、`name` が空でない、`color` が `#` を付けない6桁の16進、名前が大文字小文字を区別せずに重複しない）は、`make repo-settings-check` と、`make delete-all-labels`・`make create-default-labels` が GitHub に書く前に、同じ検査で検めます。`description` を省いたラベルは、既存のラベルの説明も空に書き換えます。
