# ADR-0102: break-glass・root・`sts:AssumeRoot` の使用を、組織の trail の Log Group の metric filter と alarm で通知する

- Status: Accepted
- Date: 2026-11-27
- Scope: security-tooling
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, ops-notification/ADR-0001, ops-notification/ADR-0101, organization-baseline/ADR-0201, security-tooling/ADR-0001, security-tooling/ADR-0101

## Context

repository-wide/ADR-0301 決定19 は、break-glass の IAM user のサインインと break-glass の role の引き受け、管理アカウントの root の使用と `sts:AssumeRoot` の呼び出しを、`ops-notification` の Topic へ必ず通知すると決めている。

これらの事象は1つのリージョンに出ない。

- IAM と STS の事象は、us-east-1 に記録される。
- IAM user の `ConsoleLogin` は、sign-in の endpoint のリージョンに記録される（AWS CloudTrail User Guide）。
- root の `ConsoleLogin` は、us-east-1・us-east-2・us-west-2 のどれかに記録される（同）。
- `sts:AssumeRoot` は、regional の STS の endpoint でだけ呼べる（AWS STS API Reference）。

管理アカウントの event history では、break-glass の user の `ConsoleLogin`・`AssumeRole`・`SwitchRole` が ap-southeast-2 に、root の `ConsoleLogin` が us-east-1・us-east-2・us-west-2 に出ていた（2026-11-27）。

Amazon EventBridge は、CloudTrail の事象をリージョンごとの bus に届ける。trail が無ければ届けない（Amazon EventBridge User Guide）。

security-tooling/ADR-0101 の multi-region の組織の trail は、すべてのリージョンの事象を1つの Log Group に集める。

## Decision

1. 組織の trail の Log Group（security-tooling/ADR-0101 決定5）に metric filter を置き、その metric の alarm を置く。alarm は、security のアカウントの `ops-notification` の Topic へ送る。送り手は `cloudwatch` で、`ops-notification` の固定の送り手に入る（ops-notification/ADR-0101）。
2. metric filter は次の5つの事象に当てる。
   - root の使用
   - break-glass の user の `ConsoleLogin`
   - break-glass の role の `AssumeRole`
   - コンソールの `SwitchRole`
   - `AssumeRoot`

   break-glass の user と role は、path `/break-glass/` で見分ける（organization-baseline/ADR-0201）。root の使用の filter は、Security Hub の CloudWatch.1 の文字列と一致させる。
3. alarm は、合計が1以上、周期1分、評価1回とする。`treat_missing_data` は `notBreaching`、3つの action には Topic だけを入れる（`docs/v1.0-plan/scope.md` 第9.3節）。
4. repository-wide/ADR-0301 決定19 の「`ops-notification` の Topic」は、security のアカウントの `ops-notification` の Topic を指すと読む。alarm と Topic は同じ security のアカウントに在るので、通知はアカウントを越えない（ops-notification/ADR-0001）。
5. Security Hub の CloudWatch.2 から CloudWatch.14（CIS の監視の項目）の metric filter と alarm は、ここでは置かない。この逸脱の記録は、下の「公式推奨からの逸脱」節が持つ（repository-wide/ADR-0207 決定11）。

## 公式推奨からの逸脱

- 外れる公式推奨: Security Hub の CloudWatch.2 から CloudWatch.14（CIS AWS Foundations Benchmark v1.2.0・v1.4.0 の監視の項目。認可されない API の呼び出し、MFA を使わないサインイン、IAM・CloudTrail・Config・Security Group・Network ACL・route table・VPC などの変更の監視）
- 外れる理由: repository-wide/ADR-0301 決定19 が通知を義務づけるのは、決定2 の事象だけである。ほかの項目は、組織の検知（GuardDuty・Security Hub）の中で何を通知するかと一緒に決める
- ユースケース上の制約: 項目ごとの filter の文字列と、通知の量は、組織の検知のほかの経路と重なりうる。重なりを決める前に置くと、同じ事象が二重に通知される
- 影響: security は、これらの変更が alarm で通知されない。availability と cost には影響しない
- 代替案: CIS の全項目を、この ADR で一度に置く
- 見直し条件: #473 で、GuardDuty と Security Hub の finding の通知を決めるとき

## 検討した代替案

### 案A: すべてのリージョンに Amazon EventBridge の rule を置き、home のリージョンの bus へ転送する

事象ごとに1通を送り、本文に主体の名前・送信元の IP・時刻を載せられる。

ただし、次が要る。

- 転送の rule（既定で有効なリージョンの数から home を除いた数）と、home の rule
- 転送の role と、Topic へ送る target の role
- `sts:AssumeRole` を拾う rule の、`ENABLED_WITH_ALL_CLOUDTRAIL_MANAGEMENT_EVENTS` の state（転送する側と受ける側の両方）

ほかにも次の費用がある。

- `ops-notification` の鍵の利用者に、target の role を足す入力の変更が要る（ops-notification/ADR-0101 の見直し条件）。
- rule は事象が届く管理アカウントの bus に置くしかなく、security-tooling/ADR-0001 決定1 に反する。
- 管理アカウントで opt-in のリージョンを有効にすると、そのリージョンに rule が無い。

### 案B: 案A と採用案の両方を置く

採用案の利点（region ごとの rule と role と、`ops-notification` の契約の変更を持たない）を失う。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 中（opt-in のリージョンが漏れる） | 高 | 高 |
| Testability | 低（実際の使用でしか確かめられない） | 低 | 高（`set-alarm-state` で Slack まで確かめられる） |
| Contract Clarity | 低（`ops-notification` の契約が変わる） | 低 | 高 |
| Maintainability | 低（リージョンごとの rule） | 低 | 高 |
| Scope Control | 低 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 事象ごとの1通。alarm が ALARM の間に重ねて起きた使用は、通知が増えない。
- 本文の主体の名前・送信元の IP・切り替え先のアカウント。通知は事象の種類までで、受けた人が CloudTrail で中身を読む。
- 速さ。CloudTrail から CloudWatch Logs への配送は平均で約5分遅れ、保証は無い。これに alarm の評価の周期が加わる。

## 保証範囲

- 保証すること: 無い。このユースケースは Terraform の実装を持たない
- 保証しないこと: コンソールの `SwitchRole` の事象の欄の名前。AWS の文書に記載が無いので、filter を置く変更が実際の事象から取る

## 検証方法

- filter と alarm を置く変更が、Unit Test を足す。filter の文字列、alarm の閾値・周期・`treat_missing_data`・3つの action を固定する
- 3つの action が同じ Topic だけを指すことは、Policy Test（`PLAN-ALARM-01`）が見る
- Slack まで届くことは `set-alarm-state` で、事象に filter が当たることは毎月の break-glass の試験（`docs/setup/organization-baseline.md` 3.6）で、人が確かめる

## 影響

`docs/setup/organization-baseline.md` 3.6 の「通知が届くこと」は、security のアカウントの Slack の channel で確かめる形になる。

## 見直し条件

- 通知の1通ごとに、主体・送信元の IP・時刻を載せることが要件になったとき（案A）
- 5分より短い遅れが要件になったとき
- #473 で、CloudWatch.2-14 の扱いを決めるとき（決定5）
