# 保護設定の適用 App の手順書

## 概要

保護設定（ブランチの ruleset とデプロイの environment）を CI から適用するための GitHub App を作り、リポジトリへ登録し、初回の適用を行うまでの手順である。担い手と条件は [ADR-0603](../adr/0603-branch-protection-and-required-checks.md) 決定30 が定め、宣言・適用・突合の関係は [`.github/settings/README.md`](../../.github/settings/README.md) が持つ。本書はそれらを再掲しない。

本書の作業はすべて人が行う。エージェントは実リポジトリの保護設定と App を書き換えない。

| # | 作業 | 実施主体 | 時点 |
| --- | --- | --- | --- |
| 1 | 宣言の値を決める（ruleset の値の書き換え） | リポジトリの管理者 | 初回の適用の前 |
| 2 | 初回の適用（ruleset と environment） | リポジトリの管理者 | App の登録の前 |
| 3 | App の作成 | App を所有するアカウントの管理者 | 初回の適用の後 |
| 4 | App の宣言と environment `repo-settings` | リポジトリの管理者 | `repo-settings-sync.yaml` を既定ブランチへ入れる前 |
| 5 | App による適用の確認 | リポジトリの管理者 | 4 の後と、`repo-settings-sync.yaml` が既定ブランチへ入った後 |
| - | 鍵の更新と失効 | リポジトリの管理者 | 随時 |

## 1. 宣言の値を決める

ruleset の値は `.github/settings/branch-protection.json` を、environment は `.github/settings/environments.json` を、Pull Request で変える。管理画面で直接変えない（ADR-0603 決定7 —— 宣言には実際に適用する値を書く）。

1. 実態と宣言のずれを読む。

   ```sh
   make repo-settings-drift
   ```

2. ずれの1件ごとに、宣言を実態へ寄せるか、実態を宣言へ寄せるかを決める。寄せる向きが決まっていない項目は、その判断を持つ issue で先に決める。**決めずに次へ進まない** —— 以後は merge のたびに CI が宣言全体を適用するので、実態だけに残した値は黙って上書きされる。
3. 宣言を変えるなら、その Pull Request を merge する。

## 2. 初回の適用

App も environment `repo-settings` もまだ無いので、初回は管理者の資格情報で宣言から適用する。

1. 既定ブランチを checkout した手元で、管理者として `gh auth login` 済みであることを確かめる。
2. 適用する。

   ```sh
   make apply-branch-protection apply-environments
   ```

3. 突合が `✅` を示すことを確かめる。

   ```sh
   make repo-settings-drift
   ```

   environment の「管理者による保護の迂回が有効です」が残る場合は、その environment の設定画面で "Allow administrators to bypass configured protection rules" を外す（`.github/settings/README.md`）。

## 3. App の作成

GitHub の [Registering a GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app) に従い、次の値で作る。ここに無い項目は既定のままにする。

| 項目 | 値 | 理由 |
| --- | --- | --- |
| Webhook の Active | 外す | App はイベントを受けない |
| Repository permissions の Administration | Read and write | ruleset・environment・deployment branch policy の書き込み |
| Repository permissions の Metadata | Read-only | すべての App に要る |
| 上記以外の permissions | No access | 宣言（`github-app.json`）は2つ以外を許さない。広げると適用が止まる |
| Where can this GitHub App be installed? | Only on this account | 他のアカウントへの install を許さない |

1. 作成後、App の設定画面で App ID を控える（秘密ではない）。
2. "Generate a private key" で秘密鍵（PEM）を作り、手元に保存する。**この鍵は次の4で secret に登録した後、手元から消す。**
3. "Install App" から、このリポジトリだけを選んで install する（"Only select repositories"）。

App の権限と install 先を変えられるのは、App を所有するアカウントの管理者である。変更はこのリポジトリからは検査できず、次の適用が「App の実態が宣言と一致しません」で失敗することで検出する（ADR-0603 保証範囲）。

## 4. App の宣言と environment `repo-settings`

1. `environments.json` に environment `repo-settings`（`deploy_from: protected`）が在ることを確かめ、無ければ足す Pull Request を merge して、`make apply-environments` で適用する。
2. App の宣言 `.github/settings/github-app.json` を Pull Request で置く。値は公開してよい。

   ```json
   {
     "app_id": 1234567,
     "permissions": { "administration": "write", "metadata": "read" },
     "repository_selection": "selected"
   }
   ```

3. environment `repo-settings` に secret `REPO_SETTINGS_APP_PRIVATE_KEY` として PEM の全文を登録する。repository secret には置かない —— 同じリポジトリのどの Pull Request の workflow からも参照できてしまう。

   ```sh
   gh secret set REPO_SETTINGS_APP_PRIVATE_KEY --env repo-settings < <PEM のパス>
   ```

4. 手元の PEM を消す。

## 5. App による適用の確認

1. 手順4 の2の Pull Request を checkout した手元で、App の token で ruleset を読み、App の宣言との突き合わせと `bypass_actors` の突合が通ることを確かめる。鍵は環境変数で一時的に渡し、シェルの履歴に残さない。

   ```sh
   REPO_SETTINGS_APP_PRIVATE_KEY="$(cat <PEM のパス>)" make repo-settings-drift-app
   ```

   「突合の対象外（観測できない）: ruleset の bypass_actors」が出るなら、App の token に `bypass_actors` が返っていない。CI の適用は書いた後の読み直しで失敗するので、`repo-settings-sync.yaml` を既定ブランチへ入れる前に原因を確かめる。
2. `repo-settings-sync.yaml` が既定ブランチへ入った後、Actions の画面から `Repo Settings Sync` を手動で起動し、成功することを確かめる。以後は既定ブランチへの push のたびに走る。

## 鍵の更新と失効

- 鍵が漏れた疑いがあるときは、App の設定画面でその鍵を削除する。これが第一手である（ADR-0302 決定4 と同じ考え方 —— 履歴や secret からの除去では漏洩を取り消せない）。
- 更新は、新しい鍵を作り、secret を上書きし、手順5 の2で確かめてから古い鍵を削除する。
- App を使えない間の適用は、手順2 の管理者による適用で行う。
