# ADR-0001: 各環境のアカウントに VPC を1つ置き、sandbox では常駐させない

- Status: Accepted
- Date: 2026-11-19
- Scope: network-foundation
- Related: repository-wide/ADR-0102, repository-wide/ADR-0301, repository-wide/ADR-0401, repository-wide/ADR-0604

## Context

VPC は、ECS・RDS・VPC Endpoint など、VPC の中に資源を置くすべてのユースケースの前提である。共有の基盤を持つユースケースを資源ごとに分け、各環境のアカウントに置き、共有基盤用のアカウントを設けないことは決まっている（#94）。sandbox は検証のたびに apply して destroy する（#86）。

## Decision

1. dev・stg・prd・sandbox のアカウントに、このユースケースが VPC を1つずつ置く。管理・security・log-archive のアカウントには置かない（入力の `env` がこの4つだけを受ける）。
2. 他のユースケースは、このユースケースの出力（VPC と subnet の ID、Route Table の ID、ログの所在）を入力で受ける。他のユースケースの内部の address に依存しない（repository-wide/ADR-0102 (use-case-centric-scope) 決定16）。
3. sandbox では常駐させない。destroy の後に、Flow Logs の Log Group を暗号化する鍵が削除の猶予（30日）のあいだ残る。これを期待残存とし、識別子（alias `<project>-<env>-network-flow-logs` の鍵）・状態（削除待ち）・期限（30日）で宣言する（repository-wide/ADR-0401 (testing-policy) 決定16）。

## 検討した代替案

### 案A: 共有基盤用のアカウントに VPC を置き、各環境へ共有する

共有基盤の持ち主の判断（#94）で採らなかった。環境が3つで越境を設計しない方針（repository-wide/ADR-0301 (least-privilege-policy) 決定18）では、共有の利点が小さく、RAM の共有と越境の権限が増える。

### 案B: sandbox でも常駐させる

sandbox の常駐の判断（#86）で採らなかった。VPC と Flow Logs は検証のたびに作り直せる。

## 意図的に捨てるもの

- 1つのアカウントに VPC を2つ以上置く構成
- VPC peering と、アカウントをまたぐ VPC の接続（README の unsupported）

## 保証範囲

- `env` が dev・stg・prd・sandbox のどれかであることは validation が見る。
- どのアカウントに apply するかは env の root と apply の経路が決める。このユースケースは見ない。

## 検証方法

- `tests/validation.tftest.hcl` が `env` の許容値を固定する。
- sandbox の apply → 検証 → destroy と期待残存の照合は、sandbox の apply の経路（#331）で行う。

## 影響

- 環境ごとに Flow Logs の鍵が1本ずつ増える。

## 見直し条件

- 共有基盤用のアカウントを設けると決めたとき。
- 1つのアカウントに複数の VPC が要るユースケースが現れたとき。
