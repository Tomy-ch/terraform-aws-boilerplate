# ADR-0102: 通知経路の死活を、毎日 09:00（Asia/Tokyo）の心拍で人が見る

- Status: Accepted
- Date: 2026-11-19
- Scope: ops-notification
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, ops-notification/ADR-0001, ops-notification/ADR-0201

## Context

基盤系のアラームは平常時に1通も来ないので、経路（Topic・鍵・Chatbot の構成・Slack の認可）が壊れても気づけない。同じ Topic へ送る alarm は、その経路の故障を知らせられない。アカウントの外で死活を見る仕組みは無い（`docs/v1.0-plan/scope.md` 第9.3節、#97）。

EventBridge Scheduler は SNS の Publish を templated target として呼べ、本文に予定時刻（`<aws.scheduler.scheduled-time>`）を置換して渡す（EventBridge Scheduler User Guide「Templated targets」「Context attributes」）。実行 role の信頼の `aws:SourceArn` は schedule group の ARN でなければならない（同「Confused deputy prevention」）。Amazon Q Developer in chat applications は `version`・`source`・`content.description` を持つ JSON を custom notification として描く（同 Administrator Guide「Custom notifications」）。Scheduler が別の region の Topic へ送れることは文書に無い。

## Decision

1. Topic ごとに専用の schedule group と、毎日 09:00（`Asia/Tokyo`）、`flexible_time_window` を OFF にした schedule を置く。us-east-1 の Topic の schedule group と schedule は、resource の `region = "us-east-1"` で置き、名前の末尾を `-us-east-1` にする。target は Topic で、本文は custom notification の JSON（title で警報でないことを示し、description に env・region・予定時刻を載せる。region で、どちらの Topic を通った心拍かを区別する。アカウント ID と ARN は載せない）。Scheduler は生の input の中の `<aws.scheduler.scheduled-time>` を置換するので、`jsonencode` が `<` `>` に変えたエスケープを戻し、トークンを生の input にそのまま置く。
2. 実行 role を1つにし、その信頼は `scheduler.amazonaws.com` を `aws:SourceAccount` と2つの schedule group の ARN の `aws:SourceArn`（`StringEquals`）で限る。role を region ごとに分けると、名前が IAM の 64 文字の上限を超え、`project` の長さの上限を縮める契約の変更が要る。権限は2つの Topic への `sns:Publish` だけとし、鍵の暗号操作は Topic の鍵の Key Policy が SNS 経由に限って許す（repository-wide/ADR-0301 (least-privilege-policy) 決定12）。Key Policy が名指す role の ARN は、role の resource の属性ではなく名前から組む。属性は作成まで plan で決まらず、決まらない Principal を持つ Key Policy は Policy Test で読めない。ARN を名前から組めるので、resource 参照による依存の解決で足り、prefix の pattern は要らない（repository-wide/ADR-0301 決定11）。作成の順序は出力の `depends_on` で role の後に置く。
3. retry は 185 回・3600 秒を明示する。1日遅れの心拍は次の日の心拍と区別できない。DLQ を置かない。
4. 人が気づく形とする。channel を見る人が、毎朝の心拍が来ていないことに気づき、README の手順で原因を確かめる。自動の検知を置かない。
5. 時刻と timezone は独自の判断である（repository-wide/ADR-0207 (default-value-policy) 決定3・4。公式の推奨値は無い）。09:00 `Asia/Tokyo` は、始業の時刻に揃えて「来ていない」に気づける時刻として採る。入力にしない。
6. 止める口（`state = "DISABLED"` の入力）を契約に置かない。止めるときは Git の経路で変更する。
7. schedule の暗号化に customer managed key を使わず、AWS owned key のままにする（`kms_key_arn` を置かない）。AWS Well-Architected Framework（最新版、2026-11-16 更新）の SEC08-BP02「Enforce encryption at rest」は、鍵をデータの分類に対応させ、分類によらず1つの鍵を使うことを anti-pattern とし、customer managed key は分類が復号の policy を自分で持つことを求めるときに選ぶとする。心拍の本文は機微な値を持たない固定の文で、CMK は Key Policy に使い手（Scheduler と schedule を作る主体）を増やすだけで守りを足さない。Checkov の CKV_AWS_297 を `internal/heartbeat/main.tf` の `aws_scheduler_schedule.this` で抑止し（repository-wide/ADR-0501 (development-tooling-composition) 決定13）、その理由の正本を本決定とする。撤回は、本文が機微な値を持つときとする。

## 検討した代替案

### 案A: UTC の固定時刻

利用者の事情を含まないが、日本の始業の時刻と合わず、気づきが遅れる。

### 案B: 時間ごとの心拍

雑音が増え、警報が埋もれる。死活の確認には毎日1回で足りる。

### 案C: SQS の DLQ

読み手が無い。失敗には心拍が来ないことで気づく。Scheduler の文書は DLQ を推奨として書いていない。

### 案D: Scheduler の失敗を知らせる alarm

同じ経路を通るので、経路の故障を知らせられない。

## 意図的に捨てるもの

- 心拍の不在の自動検知
- 09:00 以外の時刻・timezone を利用者が選ぶこと
- 毎朝の心拍が1通であること（2つの Topic から2通が来る）

## 保証範囲

2つの schedule の固定値・本文・信頼と権限の文書は Unit Test（`internal/heartbeat/tests/`）が、信頼と権限の形は Policy Test（`PLAN-NOTIFY-04`・`05`）が見る。心拍が実際に Slack に届くことと、壊れた経路で届かなくなることは、設計の意図であり、sandbox の Integration Test と管理アカウントの apply の後の着信でだけ確かめる。

## 検証方法

README の「検証方法」の「心拍が見る故障」と、人が確かめる手順。

## 影響

Scheduler の schedule は Terraform 以外に何も作らない。心拍の本文の templated target の置換が SNS で効かなければ、本文を固定の文字列に直す。

## 見直し条件

- 心拍の本文が機微な値を持つとき（決定7）
- アカウントの外で死活を見る仕組み（obp）が稼働したとき
- 運用の始業の時刻か、通知を見る人の timezone が変わったとき
- `<aws.scheduler.scheduled-time>` が SNS の templated target で置換されないと sandbox で分かったとき
