# ops-notification 手順書

## 1. 設定の概要

アカウントごとに、基盤系の通知を Slack の channel へ届ける（[`usecases/ops-notification/README.md`](../../usecases/ops-notification/README.md)）。Topic・鍵・心拍・Chatbot の channel の構成は、そのアカウントの workload root を CI が apply して作る。人が行うのは、Terraform が持たない Slack の workspace の認可と、channel の用意、apply の後の確認である。認可は OAuth で、コマンドにできない。

| 実施者 | 担当 |
| --- | --- |
| Slack の workspace にアプリを追加できる人 | channel の用意と、Chatbot への workspace の認可 |
| そのアカウントのコンソールに入れる人 | Chatbot への workspace の認可、ID の控え |
| リポジトリの管理者 | ID を env の値に書く Pull Request と、apply の後の確認 |

7つのアカウント（admin・security・log-archive・sandbox・dev・stg・prd）のそれぞれで、そのアカウントの workload root が ops-notification を呼ぶ変更の merge より前に行う。認可の前に apply すると、Chatbot の構成の作成が失敗する見込みである（確かめていない）。

## 2. 設定完了後の状態

- アカウントの Chatbot に、通知先の Slack の workspace が認可されている
- アカウントごとに、通知を受ける Slack の channel が1つある
- env の値に、その workspace の ID（`T` で始まる）と channel の ID（`C` で始まる）が書かれている
- 毎日 9時（Asia/Tokyo）の心拍が、その channel に届く（`usecases/ops-notification/internal/heartbeat/main.tf` の `schedule_expression`）

## 3. 手順

### 3.1 通知を受ける channel を用意する

1. Slack で、通知を受ける channel を作る。アカウントごとに別の channel にする。通知はアカウントを越えない
2. 名前は `#<project>-aws-<env>-ops` を勧める。`<project>` は env の root が usecase に渡す `project` の値、`<env>` は env の名前（`admin`・`security`・`log-archive`・`sandbox`・`dev`・`stg`・`prd`）を略さずに書く
   - `aws` で、AWS からの通知の channel だとわかり、ほかの用途の channel と紛れない
   - `<project>` で始まるので、channel の一覧でまとまる
   - `<env>` と `ops` は、ops-notification が作る resource の名前 `<project>-<env>-ops-*` とたどれる
   - 名前は勧めであって規約ではない。channel の名前は Terraform に入らない（渡すのは ID だけ）ので、後で変えても通知は止まらない
3. 心拍が届かない日に気づけるよう、毎日見る場所を選ぶ

### 3.2 Chatbot に Slack の workspace を認可する

1. そのアカウントのコンソールで、「Amazon Q Developer in chat applications」（旧 AWS Chatbot）を開く
2. 「チャットクライアント」で「Slack」を選び、「クライアントを設定」を押す（日本語のコンソールでの表記）
3. Slack の認可の画面で、右上の workspace が通知先の workspace であることを確かめ、「許可する」を押す。workspace がアプリの追加を承認制にしていれば、管理者の承認の後にやり直す
4. コンソールに戻り、workspace が一覧に表示されることを確かめる。「新しいチャネルを設定」は押さない。channel の構成は Terraform が作り、コンソールで作ると Terraform の管理の外に二重に残る

### 3.3 ID を控えて env の値に書く

1. workspace の ID（`T` で始まる英数字）を控える。コンソールの workspace の詳細か、ブラウザで Slack を開いたときの URL（`https://app.slack.com/client/T…/C…`）の `T` で始まる部分で見られる
2. channel の ID（`C` で始まる英数字）を控える。Slack で channel の名前を押し、開いた詳細の一番下に表示される
3. 非公開の channel なら、channel に Amazon Q のアプリを招待する（`/invite` か、channel の詳細の「インテグレーション」→「アプリを追加する」）
4. 2つの ID を、そのアカウントの env の root が ops-notification に渡す `chat.slack` の `team_id`・`channel_id` に書く。ID は秘密ではないが、公開のリポジトリなら workspace と channel を指す値が公開される

### 3.4 apply の後に確かめる

1. 次の 9時（Asia/Tokyo）に、channel に心拍が届くことを確かめ、結果を Pull Request に書く
2. 届かなければ、3.2 の認可と、3.3 の ID と招待を見直す
