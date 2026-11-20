# リポジトリ設定

このディレクトリには、リポジトリ単位の GitHub 設定を JSON で格納しています（分岐のパターン、ブランチルールセット、デプロイの environment、Private Vulnerability Reporting、ラベル定義）。このボイラープレートから派生したリポジトリを、Web UI をクリックして回るのではなく再現可能な形で構成するためのものです。

## 宣言と実態の突合

保護設定 —— `branch-protection.json`、`environments.json`、`private-vulnerability-reporting.json` —— は、宣言から適用し、実態と突き合わせます（ADR-0603 決定4-6）。判定は `scripts/repo-settings` が持ちます。

| 操作 | make | 書き換えるもの | 誰が |
| --- | --- | --- | --- |
| 生成 | `repo-settings-apply` | `branch-protection.json` の include の区間と、apply の workflow の `on.push.branches` | 誰でも |
| 宣言の整合 | `repo-settings-check` | 無し | CI（required）と pre-commit |
| 突合 | `repo-settings-drift` | 無し（GET のみ） | 人が手元で（誰でも） |
| 適用 | `apply-branch-protection` / `apply-environments` / `apply-vulnerability-reporting` | GitHub の ruleset / environment / Private Vulnerability Reporting | 管理者の資格情報を持つ人 |
| デプロイブランチの作成 | `create-deploy-branch DEPLOY_ENV=<env>` | GitHub のブランチ（env のデプロイブランチを、デフォルトブランチの先頭から作る。在れば失敗する） | 管理者の資格情報を持つ人（[`docs/setup/bootstrap.md`](../../docs/setup/bootstrap.md) の段5 で1回） |

- 宣言を変える Pull Request は、merge の後に管理者が適用します。CI は保護設定を書き込みません（ADR-0603 決定30）。適用は `make setup-repo` も呼びます。
- 突合は人が手元で実行します。保護設定は人が管理者の資格情報で守るもので、CI の定期実行で重ねて見ることはしません。適用の後と、管理画面で設定が変わったと疑うときに `make repo-settings-drift` を実行し、ずれがあれば失敗します。直すのは、宣言を実態へ寄せる Pull Request か、宣言の再適用です（ADR-0601 決定13）。
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
| `env` | そのブランチへの merge が apply する env（`env/<env>/` の root）。env をまだ配線していないデプロイブランチは持ちません |
| `from` | そのデプロイブランチが変更を受け取る線の接頭辞の一覧。各要素は `lines` のどれかで、重複できません。`env` を持つ要素だけが持ち、持つときは1つ以上が必須です |
| `release_base` | タグを打ち、新しい線を切る基点のブランチの印。宣言にちょうど1つ置きます |

- デプロイブランチへ入る Pull Request は、デフォルトブランチがその `from` の線のどれかであり、head の commit がデフォルトブランチから到達でき、merge の結果の木が head の木と一致するときだけ merge できます（ADR-0603 決定31）。これを見るのが required の `deploy-source-check` です（[`.github/workflows/README.md`](../workflows/README.md) の「apply の job」）。
- `env` を持つ要素は、`env/` の root が `deployment-identity` へ渡す `env` とちょうど対応します。`env` ごとにデプロイブランチが1つ在り、`env` を持つ要素がどれも `deployment-identity` の `env` を指すことを `repo-settings-check` が見ます。
- `branch` の重複、`env` の重複、`lines` に無いか重複する `from`、`env` を持つ要素の空の `from`、`env` を持たない要素の `from`、`lines` の役割の欠けと役割どうしの接頭辞の重複、`v<major>.<minor>.<patch>` でない `line_version`、`release_base` が0件か2件以上であることは、`repo-settings-check` が落とします。

読み手は次のとおりです。いずれも同じ読み手（`scripts/lib/branchdecl`）を通ります。

- ここから生成するもの: `branch-protection.json` の include、environment の deployment branch policy、workflow の起動条件。apply の workflow（job の `environment:` が、`push` で起動する種別の role の `<env>-<role>` のもの。`make ci-hold-*` を走らせる lock を残す job は、同じ environment でも `workflow_dispatch` だけで起動するので数えない）の `on.push.branches` はその env のデプロイブランチ1つで、ほかの workflow は印 `# branch-set: <集合>` を持つ行を生成します（集合の一覧は [`.github/workflows/README.md`](../workflows/README.md) の「起動の方針」）。`make repo-settings-apply` が書き換え、`repo-settings-check` がずれを落とします
- 実行時に読むもの: `scripts/repo-settings` の `source`（`deploy-source-check`）と `deploy-branch`（`create-deploy-branch`）、`scripts/apply-revert`（`revert` の接頭辞と、デプロイブランチの `from`）、`scripts/base-branch`（`lines.release` と `line_version`）、`scripts/release`（`release_base`、線の役割の接頭辞と `line_version`）、`scripts/repo-setup`（`env` を持たないデプロイブランチ、`release_base`、線の接頭辞）

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

## branch-protection.json

`conditions.ref_name.include` は `branches.json` から `make repo-settings-apply` が生成する区間です。手で書き換えると `repo-settings-check` が落ちます。それ以外の値は手で書きます。各ルールの宣言内容は次のとおりです。

| ルール | 宣言している内容 |
| --- | --- |
| `deletion` | 対象ブランチを削除できない。 |
| `non_fast_forward` | 対象ブランチへの force-push を拒否する。 |
| `pull_request` | 対象ブランチへの変更は PR 経由に限る。承認 1 件、push 時に既存の承認を破棄、最終 push 後の再承認、全レビュースレッドの解決、CODEOWNERS レビュー、帰属の無い変更への追加の承認、マージ方法は merge commit に限定（squash と rebase merge を除外。ADR-0603 決定24）。 |
| `code_quality` | GitHub の code quality ルールを `errors` 深刻度でブロックする。 |
| `required_status_checks` | 宣言したチェックがすべて成功してからマージする。各 context は `integration_id`（GitHub Actions）を持ち、Actions 以外が報告した同じ名前の status では満たせない（ADR-0603 決定34）。`repo-settings-check` が欠けを落とす。 |

### 単独メンテナのリポジトリに `pull_request` を適用する場合

GitHub では自分の PR を自分で承認できません。`required_approving_review_count: 1` かつ `bypass_actors` が空の場合、参加者がオーナー 1 人だけのリポジトリではこのルールを満たせる人間が存在せず、保護ブランチ向けの PR がすべて恒久的にマージ不能になります。そうしたリポジトリへ適用する前に、メンテナを `bypass_actors` に列挙するか、承認を要求するパラメータを両方とも下ろしてください（`required_approving_review_count: 0` と `require_last_push_approval: false`）。スレッド解決とマージ方法の制限はそれ単体でも効きます。

### `code_quality` は裏側の機能が先に報告している必要がある

GitHub の案内は、ruleset に Code Quality の閾値を宣言する**前に** Code Quality のワークフローが動作し PR へ結果を報告していることを確認せよ、というものです。確認しないまま宣言すると、このルールが全 PR のマージをブロックし得ます。機能の有効化はこのディレクトリの外にあるリポジトリ単位の操作なので、機能が無効なら無害だと仮定せず、適用前に確認してください。

### Required status check はすべての PR で報告経路を持つ必要がある

宣言する required context は、その赤が変更そのものについての事実であるものに限ります
（ADR-0603 決定9-11）。継承した状態を報告するチェックはこの一覧に入れません。固定した版の道具の
取得（TFLint の ruleset を含む）は、取得先の障害で赤になっても再実行で消え、判定を変えません。
`tf-validate-test` はそれと違い、root でない module の provider を実行時点の最新で解決するので、
checkout だけでは再現しない部分を持ちます。これは ADR-0603 決定10 が認めた例外です。

| 群 | context |
| --- | --- |
| 検査の設定と定義 | `actions-lint`、`adr-lint`、`commitlint`、`docker-lint`、`egress-check`、`md-lint`、`tf-gates-test`、`zizmor` |
| 宣言と実体のずれ | `mod-tidy-check`、`pin-actions-check`、`pin-images-check`、`pin-runners-check`、`repo-settings-check`、`skill-lint`、`versions-check` |
| サンプルの除去 | `sample-remove-check` <!-- sample:line --> |
| コードと構成 | `go-lint`、`go-fmt-check`、`go-test`、`test-mapping`、`tf-fmt-check`、`tf-lint`、`tf-validate-test`、`infra-code-gate`、`trivy-config`、`checkov`、`kics`、`semgrep`、`secret-scan` |
| デプロイブランチへの経路（ADR-0603 決定32 の例外） | `deploy-source-check`。デフォルトブランチの名前と到達性、デプロイブランチの今の木に依り、変更に誤りが無くても赤になりうる。デプロイブランチ宛でない Pull Request では対象外として成功する。検査のコードと宣言は Pull Request の base の commit のものを使う |
| plan（ADR-0603 決定12-14 の例外） | `plan-admin-identity`・`plan-admin-workload`。AWS の状態と資格情報の取得に依存し、変更に誤りが無くても赤になりうる。fork と Dependabot の Pull Request では拒否の step で失敗するので、それらは merge できない |

**ここへ context を足したら、それを報告する job が `pull_request` を絞っていないことを確かめること。**
GitHub は報告の無い check を「該当しない」とは読まず待ち続け、その pull request は恒久的に
マージ可能にならない。`make required-check-lint` がその不変条件を見ます —— 詳細は
[`.github/workflows/README.md`](../workflows/README.md) の「必須検査はすべての Pull Request で
報告される」。

## private-vulnerability-reporting.json

Private Vulnerability Reporting を有効にするかの宣言です。[`SECURITY.md`](../../SECURITY.md) が脆弱性の報告の経路として案内する機能で、保護設定の1つとして宣言から適用し、突き合わせます。

| キー | 意味 |
| --- | --- |
| `enabled` | 有効にするなら `true`。`true` か `false` のどちらかで、省略と `null` は `repo-settings-check` が落とします |

- GitHub はこの機能を公開のリポジトリにだけ提供します。公開でないリポジトリは `false` を宣言し、`SECURITY.md` の報告先を差し替えます。`true` のままなら、適用は何も送らずに失敗し、突合はずれとして報告します。
- `false` を宣言した公開でないリポジトリでは、突合はこの機能の実態を読まず、「対象外」と出力します。
- 突合がこの機能の実態を読めなかったときは、ruleset と environment の結果を出したうえで失敗します。読めなかったものを対象外に数えません。

## labels.json

`make create-default-labels` が作成するラベル定義（`name` / `description` / `color`）です。`make setup-repo` は先に `make delete-all-labels` を実行するため、このファイルは GitHub が既定で作るラベルへの追加分ではなく、意図するラベル集合そのものです。
