# リポジトリ設定

このディレクトリには、リポジトリ単位の GitHub 設定を JSON で格納しています（分岐のパターン、ブランチルールセット、デプロイの environment、ラベル定義）。このボイラープレートから派生したリポジトリを、Web UI をクリックして回るのではなく再現可能な形で構成するためのものです。

## 宣言と実態の突合

保護設定 —— `branch-protection.json` と `environments.json` —— は、宣言から適用し、実態と突き合わせます（ADR-0603 決定4-6）。判定は `scripts/repo-settings` が持ちます。

| 操作 | make | 書き換えるもの | 誰が |
| --- | --- | --- | --- |
| 生成 | `repo-settings-apply` | `branch-protection.json` の include の区間 | 誰でも |
| 宣言の整合 | `repo-settings-check` | 無し | CI（required）と pre-commit |
| 突合 | `repo-settings-drift` | 無し（GET のみ） | CI（毎日と手動）、誰でも |
| 適用 | `repo-settings-sync-app` | GitHub の ruleset / environment | CI（`repo-settings-sync.yaml`。既定ブランチへの push と手動） |
| 初回と回復の適用 | `apply-branch-protection` / `apply-environments` | GitHub の ruleset / environment | 管理者の資格情報を持つ人 |
| App の確認 | `repo-settings-drift-app` | 無し（GET のみ） | App の秘密鍵を持つ人 |

- 宣言を変える Pull Request は、merge の push で CI が適用します（ADR-0603 決定30）。書き込みは `github-app.json` が宣言する GitHub App の短命の token で行い、書く前に App の実際の権限・install 先・token が届くリポジトリが宣言どおりかを確かめ、ずれていれば1件も書きません。書いた後は ruleset を App の token で（`bypass_actors` を含めて）、environment を `GITHUB_TOKEN` で読み直し、ずれが残れば失敗します。失敗は `notify.yaml` で通知されます。
- App の作成、秘密鍵の登録、初回の適用の手順は [`docs/setup/repository-settings-app.md`](../../docs/setup/repository-settings-app.md) にあります。App は `bypass_actors` に入れません（ADR-0603 決定22）。
- App の秘密鍵は environment `repo-settings` の secret です。この environment は保護ブランチの run から参照でき、job の `if:` が既定ブランチに限ります。**既定ブランチ以外の保護ブランチへ merge された workflow が鍵に届くことは防げません**（ADR-0603 保証範囲）。App の設定画面（権限、install 先）を変えられる主体による変更は、次の適用の失敗で検出します。
- 管理者による初回の適用は `make setup-repo` も呼びます。
- 突合は Pull Request とは独立に毎日走り（`.github/workflows/repo-settings.yaml`）、ずれは失敗として通知されます。管理画面で設定を変えると、ここで赤になります。直すのは、宣言を実態へ寄せる Pull Request か、宣言の再適用です（ADR-0601 決定13）。
- 適用は、宣言に無い environment を消しません。消すと secret ごと失われるためで、突合がそれを報告し、人が消すか宣言へ足します。
- **CI の突合は ruleset の `bypass_actors` を見ません。** GitHub はこの値を ruleset への write 権限を持つ呼び出しにしか返さず、CI の `GITHUB_TOKEN` は読み取りの権限です（ADR-0603 決定6）。突合は「対象外」と出力し、比べたことにはしません。`bypass_actors` が空であることは、宣言の側を `repo-settings-check` が見て、実態の側は、CI の適用が書いた後に App の token で読み直すときと、管理者が手元で `make repo-settings-drift` を実行したときに見ます。

## branches.json

分岐のパターンの単一宣言です（ADR-0603 決定1）。`deploy` は固定名で保護するブランチ、`lines` は接頭辞で表すブランチの線（`/` で終わる）です。保護対象は両者の和です。

読み手のうち、`branch-protection.json` の include と environment の deployment branch policy はここから生成します。`scripts/` のブランチ操作（`base-branch` / `release` / `repo-setup`）と workflow の起動条件は、まだパターンを自分で持っています。

## environments.json

デプロイの environment の宣言です。宣言は実態の environment の全集合で、宣言に無い environment が実在すれば突合が報告します —— 存在しない environment を参照した workflow は、その名前の environment を保護なしで作るためです。

| キー | 意味 |
| --- | --- |
| `name` | environment の名前。`*-plan` / `*-apply` / `*-apply-identity` は `usecases/deployment-identity` の role の信頼条件が名指す名前の写し。`env/` の root が `deployment-identity` へ渡す `env` ごとに3つが揃い、どの `env` にも当たらない3種の名前が無いことを `repo-settings-check` が見る（ADR-0501 決定5 の但し書き） |
| `deploy_from` | `protected`（保護対象のブランチだけ）、`protected-and-pull-requests`（同 + Pull Request の ref `refs/pull/*/merge`）、`any`（deployment branch policy を持たない） |
| `can_admins_bypass` | 管理者による保護の迂回。`false` だけを受けます（ADR-0602 決定14） |

- パターンはこのファイルに書きません。`deploy_from` の種類から、`branches.json` を都度展開します。environment のパターンの `*` は `/` に当たらないので、`lines` は1階層（`release/*`）だけを許します。ruleset が保護する集合より狭く、許可の側は狭い方へ倒れます。
- 名前の接尾辞が `deploy_from` を決めます。`*-apply` / `*-apply-identity` は `protected` だけ、`*-plan` は `protected-and-pull-requests` だけを受けます（ADR-0602 決定14）。
- 承認者と待機時間は持たず、突合は実態にそれらが無いことを要求します。承認者を置かないのは単独メンテナで承認できる人が自分しかいないためで、ADR-0601 決定7 が任意とする機構を使わない選択です。
- `copilot` は Copilot coding agent が作る environment です。宣言に含め、管理者の迂回を無効にします。
- `can_admins_bypass` は GitHub の REST の文書に載っていない値です。適用はこれを送り、突合は応答の値を見ます。適用の後も突合が「迂回が有効」と報告するなら、environment の設定画面で "Allow administrators to bypass configured protection rules" を外してください。

## branch-protection.json

`conditions.ref_name.include` は `branches.json` から `make repo-settings-apply` が生成する区間です。手で書き換えると `repo-settings-check` が落ちます。それ以外の値は手で書きます。各ルールの宣言内容は次のとおりです。

| ルール | 宣言している内容 |
| --- | --- |
| `deletion` | 対象ブランチを削除できない。 |
| `non_fast_forward` | 対象ブランチへの force-push を拒否する。 |
| `pull_request` | 対象ブランチへの変更は PR 経由に限る。承認 1 件、push 時に既存の承認を破棄、最終 push 後の再承認、全レビュースレッドの解決、CODEOWNERS レビュー、帰属の無い変更への追加の承認、マージ方法は merge commit に限定（squash と rebase merge を除外。ADR-0603 決定24）。 |
| `copilot_code_review` | Copilot が各 PR を自動レビューする。push のたび、および draft に対しても実行する。 |
| `code_quality` | GitHub の code quality ルールを `errors` 深刻度でブロックする。 |
| `required_status_checks` | 宣言したチェックがすべて成功してからマージする。 |

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
| コードと構成 | `go-test`、`test-mapping`、`tf-fmt-check`、`tf-lint`、`tf-validate-test`、`trivy-config`、`checkov`、`kics`、`semgrep`、`secret-scan` |

**ここへ context を足したら、それを報告する job が `pull_request` を絞っていないことを確かめること。**
GitHub は報告の無い check を「該当しない」とは読まず待ち続け、その pull request は恒久的に
マージ可能にならない。`make required-check-lint` がその不変条件を見ます —— 詳細は
[`.github/workflows/README.md`](../workflows/README.md) の「必須検査はすべての Pull Request で
報告される」。

## labels.json

`make create-default-labels` が作成するラベル定義（`name` / `description` / `color`）です。`make setup-repo` は先に `make delete-all-labels` を実行するため、このファイルは GitHub が既定で作るラベルへの追加分ではなく、意図するラベル集合そのものです。
