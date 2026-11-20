# ADR-0001: 各環境のアカウントに公開の zone を1つ置き、prd を apex とする

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0102, repository-wide/ADR-0301, repository-wide/ADR-0401, repository-wide/ADR-0604, dns-zone/ADR-0101

## Context

各アカウントは自分の state と apply の経路を持ち、ユースケースは環境ごとにそのアカウントの root から呼ばれる（repository-wide/ADR-0604 (state-split-by-apply-role)）。Route 53 の Hosted Zone は公開面を持つユースケースが共有し、常駐層に置く（docs/v1.0-plan/scope.md 第10.2節）。zone の名前を環境ごとに入力で受けると、子の zone の名前と、親が書く委任の NS の名前が食い違いうる。sandbox の常駐の zone は、完了条件の「apply → 検証 → destroy」を常駐の実体では満たせない（repository-wide/ADR-0401 (testing-policy) 決定14）。

## Decision

1. 各環境のアカウント（dev・stg・prd・sandbox）の workload root が、このユースケースを1回呼び、公開の zone を1つ持つ。
2. zone の名前は入力 `apex_domain` と `env` から導く。prd は `apex_domain`、ほかは `<env>.<apex_domain>`。zone の名前を入力に取らない。
3. prd の zone が dev と stg の zone へ委任する（dns-zone/ADR-0101）。sandbox の zone は委任しない。
4. 完了条件は docs/v1.0-plan/scope.md 第11.1節の「複製を作れる」の区分に入れる。sandbox で、常駐する zone と名前・鍵・Log Group・resource policy を共有しない複製を作り、apply → 検証 → destroy する。複製は委任しない `apex_domain` で作り、API の状態で検証する。
5. 期待残存は、us-east-1 のクエリログの鍵と DNSSEC の鍵の2本（状態は削除待ち、期限は 30 日）で、識別子で照合する（repository-wide/ADR-0401 決定16）。

## 検討した代替案

### 案A: 入力 `zone_name` で zone の名前を受ける

子の zone の名前と、prd の委任の NS の名前の組み立てが別の値になり、食い違っても plan で分からない。

### 案B: prd の親の zone を別のユースケースに分ける

zone の持ち主が2つになり、`force_destroy` と置換の保護を二重に持つ。親の zone の中身（委任）はこのユースケースの契約として一般化できる（repository-wide/ADR-0102 (use-case-centric-scope) 決定5 の1）。

### 案C: 全環境の zone を1つの root で持つ

1つの apply の role が複数のアカウントへ書く（repository-wide/ADR-0301 (least-privilege-policy) 決定18 が既定で許さない）。

## 意図的に捨てるもの

- sandbox の zone を公開の DNS から引くこと。
- 環境の数と名前を利用者が変えること（`env` の4つに限る）。

## 保証範囲

- zone の名前の導出は `tests/composition.tftest.hcl` と `tests/contract.tftest.hcl` が mock の plan で見る。
- 複製での apply → 検証 → destroy は sandbox の apply の経路（#331）が無い間は行えない。

## 検証方法

- `tests/composition.tftest.hcl`・`tests/contract.tftest.hcl`・`tests/validation.tftest.hcl`

## 影響

- record を書くユースケースは、環境ごとに `zone_id` と `zone_name` を受ける。
- 期待残存の照合には us-east-1 の走査が要る（#76）。

## 見直し条件

- sandbox の zone に公開の到達性が要るとき（`delegations` の key に sandbox を足す）。
- 環境の集合が変わるとき。
