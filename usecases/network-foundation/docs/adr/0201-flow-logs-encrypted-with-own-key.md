# ADR-0201: Flow Logs をこのユースケースの鍵で暗号化し、配送 role の DescribeLogGroups だけを Resource "*" にする

- Status: Accepted
- Date: 2026-11-19
- Scope: network-foundation
- Related: repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0402

## Context

`modules/vpc` は VPC 単位の Flow Logs（`ALL`）とその配送 role を持ち、宛先の Log Group の ARN を入力で受ける。`modules/log-group` は鍵による暗号化を必須にする。配送 role の permission policy は、resource-level の権限を持たない `logs:DescribeLogGroups` だけを Resource `"*"` にする（Service Authorization Reference の Amazon CloudWatch Logs）。Policy の `plan.iam_action_wildcard` は、宣言した Action のほかに Resource `"*"` の Allow を認めない（PLAN-IAM-03）。

## Decision

1. このユースケースが Flow Logs の Log Group 用の鍵を1本持つ（`modules/kms`、alias `<project>-<env>-network-flow-logs`）。鍵の Key Policy は、CloudWatch Logs のサービスに、暗号化コンテキストでこの Log Group だけの暗号化を許す。
2. 合成の順序は `modules/kms` → `modules/log-group` → `modules/vpc` とする。`modules/vpc` には Log Group の ARN を resource 参照のまま渡し、destroy が Flow Logs、Log Group の順になるようにする。
3. `conftest/plan/decl.json` の `resource_wildcard_actions` に `logs:DescribeLogGroups` を置く。根拠は Context の Service Authorization Reference である（repository-wide/ADR-0301 (least-privilege-policy) 決定9）。
4. Flow Logs の `log_format` の項目集合は `modules/vpc` が固定する独自判断（repository-wide/ADR-0207 (default-value-policy) 決定3 の優先順位6）であり、このユースケースが最初の呼び手として引き継ぐ。項目ごとの用途は `modules/vpc/README.md` が持つ。理由は、NAT Gateway など中間の ENI を越えた通信の元の送信元と宛先、接続の試行（SYN）、通信の方向と経路を、事後の調査で判別するためである。
5. 既定の Security Group の規則が空であること、Flow Logs が `ALL` で CloudWatch Logs 宛てであること、配送 role の trust の Condition を、Policy（`plan.network_foundation` の PLAN-NET-01〜03）で固定する。Checkov と Trivy は既定の Security Group の規則を見ない（変異で確かめた）。

## 検討した代替案

### 案A: 他のユースケースの鍵を受ける

アカウント単位のログの鍵を持つユースケースが無い。受けると、Log Group の名前を Key Policy へ渡す経路がユースケースをまたぐ。

### 案B: AWS が持つ鍵による既定の暗号化

`modules/log-group` の不変条件に反する。

### 案C: `log_format` を AWS の既定（v2）にする

NAT Gateway 越しの通信で元の送信元と宛先が分からない。

## 意図的に捨てるもの

- 鍵を Log Group ごとに分けない構成（このユースケースの Log Group は1つ）
- sandbox の destroy の後、鍵が削除の猶予のあいだ残ること（network-foundation/ADR-0001）

## 保証範囲

- 鍵の alias と Log Group の名前、Log Group の ARN が出力に渡ることは `tests/composition.tftest.hcl` が見る。
- 鍵の Key Policy の形は `plan.kms_key_policy` が、配送 role の Resource `"*"` の範囲は `plan.iam_action_wildcard` が見る。
- `logs:CreateLogGroup` を持たない配送 role で Flow Logs が届くことは確かめていない（`modules/vpc` の逸脱）。

## 検証方法

- `conftest/plan/network_foundation_test.rego`・`tests/composition.tftest.hcl`
- 配送の成否は sandbox の apply の経路（#331）で確かめる。

## 影響

- 環境ごとに鍵が1本あり、鍵の料金が掛かる。

## 見直し条件

- アカウント単位のログの鍵を持つユースケースができたとき。
- `logs:DescribeLogGroups` が resource-level の権限を持つようになったとき。
