# ops-notification 手順書

## 1. 設定の概要

アカウントごとに、基盤系の通知を Slack の channel へ届ける（[`usecases/ops-notification/README.md`](../../usecases/ops-notification/README.md)）。Topic・鍵・心拍・Chatbot の channel の構成は、そのアカウントの workload root を CI が apply して作る。人が行うのは、Terraform が持たない Slack の workspace の認可と、channel の用意、apply の後の確認である。認可は OAuth で、コマンドにできない。

| 実施者 | 担当 |
| --- | --- |
| Slack の workspace にアプリを追加できる人 | channel の用意と、Chatbot への workspace の認可 |
| そのアカウントのコンソールに入れる人 | Chatbot への workspace の認可、ID の控え |
| リポジトリの管理者 | ID を env の値に書く Pull Request と、apply の後の確認 |

7つのアカウント（admin・security・log-archive・sandbox・dev・stg・prd）のそれぞれで、そのアカウントの workload root が ops-notification を呼ぶ変更の merge より前に行う。認可の前に apply すると、Chatbot の構成の作成が失敗する見込みである（確かめていない）。

そのアカウントの identity root が、workload root より先に apply されていること。Chatbot の service-linked role（`AWSServiceRoleForAWSChatbot`）は identity root の `deployment-identity` が作り、workload root はそれを名前で読む。無ければ workload root の plan が落ちる（[`usecases/ops-notification/README.md`](../../usecases/ops-notification/README.md) の「plan で落ちたときに見るもの」）。sandbox のアカウントでも、role は sandbox の identity root の apply で作る（sandbox の apply の経路は #331 が配線する）。role を CLI やコンソールで作らない。identity root の apply が名前の重複で落ちる。role が在ることは、そのアカウントの profile で確かめる（`ENV` は env の名前）

```sh
make setup-ops-notification-chatbot-role-check PROFILE=<そのアカウントの profile> ENV=<env>
```

この手順書の make の入口は、region を `ENV` の workload root（`env/<ENV>`）の provider の region から読む。その root が無い env では動かない

## 2. 設定完了後の状態

- アカウントの Chatbot に、通知先の Slack の workspace が認可されている
- アカウントごとに、通知を受ける Slack の channel が1つある
- env の値に、その workspace の ID（`T` で始まる）と channel の ID（`C` で始まる）が書かれている
- 毎日 9時（Asia/Tokyo）の心拍が、その channel に2通届く。provider の region の Topic と us-east-1 の Topic から1通ずつで、title と本文の region で区別する（`usecases/ops-notification/internal/heartbeat/main.tf` の `schedule_expression`）

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

1. workspace の ID（`T` で始まる英数字）を控える。次の入口が、Chatbot に認可された ENABLED の workspace と、その team ID を出す。ENABLED の workspace が無ければ NG で終わるので、3.2 をやり直す

   ```sh
   make setup-ops-notification-workspace-check PROFILE=<そのアカウントの profile> ENV=<env>
   ```

2. channel の ID（`C` で始まる英数字）を控える。Slack で channel の名前を押し、開いた詳細の一番下に表示される
3. 非公開の channel なら、channel に Amazon Q のアプリを招待する（`/invite` か、channel の詳細の「インテグレーション」→「アプリを追加する」）
4. 2つの ID を、そのアカウントの env の root が ops-notification に渡す `chat.slack` の `team_id`・`channel_id` に書く。ID は秘密ではないが、公開のリポジトリなら workspace と channel を指す値が公開される

### 3.4 apply の後に確かめる

1. apply の直後に、2つの Topic のそれぞれに Chatbot の購読が1つずつあることを確かめる。apply の成功は、Chatbot が Topic を購読したことを保証しない。購読は Chatbot の service-linked role が構成の作成と更新の中で作り、作れなかったときも構成の作成は成功で返る。次の入口が、provider の region の Topic と us-east-1 の Topic のそれぞれで、Chatbot の https の購読がちょうど1つであることを確かめる

   ```sh
   make setup-ops-notification-subscriptions-check PROFILE=<そのアカウントの profile> ENV=<env>
   ```

2. 購読が無い Topic があれば、次の入口で、構成 `<project>-<env>-ops-chat` の SNS topics からその Topic だけを外して保存し、もう一度足して保存する。保存の前に、購読の無い Topic を示して project の名前の入力を求める。guardrail・role・logging・user の認可は今の値のまま渡し直す。最後の状態は Terraform の宣言と同じなので、次の plan に差分は出ない。保存の後に購読を読み直し、見えなければ失敗で終わる

   ```sh
   make setup-ops-notification-resubscribe PROFILE=<そのアカウントの profile> ENV=<env>
   ```

   API で保存し直したときに Chatbot が購読し直すかは文書に無い。入口が購読を確かめられずに終わったときは、Amazon Q Developer in chat applications のコンソールで、構成 `<project>-<env>-ops-chat` の SNS topics からその Topic だけを外して保存し、もう一度足して保存する。そのあと 1 をやり直す
3. 次の 9時（Asia/Tokyo）に、channel に心拍が2通届くことを確かめ、結果を Pull Request に書く。title の region が provider の region のものと `us-east-1` のものが1通ずつ来る
4. 2通とも届かなければ、3.2 の認可と、3.3 の ID と招待を見直す
5. 片方だけ届かなければ、届かない region の Topic・鍵・schedule を見る。手順は [`usecases/ops-notification/README.md`](../../usecases/ops-notification/README.md) の「心拍が来なかったときに見るもの」
