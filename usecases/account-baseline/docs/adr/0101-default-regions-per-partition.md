# ADR-0101: リージョンごとの設定を、partition ごとの既定で有効な全リージョンに置き、有効なリージョンが集合の外なら plan を止める

- Status: Accepted
- Date: 2026-11-24
- Scope: account-baseline
- Related: repository-wide/ADR-0204, repository-wide/ADR-0207, repository-wide/ADR-0208, account-baseline/ADR-0001, account-baseline/ADR-0102

## Context

EC2.7・IAM.28・Config.1・EC2.172・SSM.6・SSM.7・EMR.2 はリージョンごとに評価する。AWS SRA は EBS の既定の暗号化を「every account and Region」で、Config を「each member account and AWS Region that contains the resources」で有効にすることを求める。使わないリージョンでも、主体が資源を作ることはできる。

AWS のリージョンの管理の文書は、2019年3月20日より前に提供されたリージョンを既定で有効とし、既定のリージョンは無効にできないとする。aws の partition の既定のリージョンは17ある。それより後のリージョンは opt-in で、有効にすると集合が広がる。

repository-wide/ADR-0208 (us-east-1-resources-by-region-argument) 決定3 は、リージョンを利用者の入力にしない。入力にすると、受け取る値の意味が AWS の表現に依存する（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定2）。

## Decision

1. リージョンごとの設定は、partition を key にした既定で有効なリージョンの集合の literal を、usecase の中に1つだけ持ち、`for_each` と resource の `region = each.key` で置く。集合は入力にしない。provider は1つのままにする。
2. aws の partition の集合は、既定で有効な17のリージョンとする。ほかの partition の集合は持たず、`data.aws_partition` の postcondition で plan を止める。
3. アカウントで有効なリージョンが集合の外を含むなら、`data.aws_regions`（引数なし）の postcondition で plan を止める。opt-in のリージョンが有効なアカウントには置けない。
4. opt-in のリージョンの有効化は、メンバーの OU に付ける SCP で拒む。SCP の付け先は `organization-baseline` が持つ。管理アカウントには SCP が効かないので、管理アカウントの opt-in のリージョンは、決定3 の事後の検出だけになる。

## 検討した代替案

### 案A: provider のリージョンだけに置く

EC2.7・IAM.28・Config.1 ほかが、ほかのリージョンで外れる。外れることを repository-wide/ADR-0207 (default-value-policy) 決定11 の逸脱として記録することになる。使わないリージョンに作られた資源の暗号化・外部アクセス・構成の履歴を失う。

### 案B: `data.aws_regions` の名前を `for_each` に使う

plan のたびに集合が変わりうる。CI の role の権限と egress の宣言を、どのリージョンについて列挙するかを固定できない。opt-in のリージョンが黙って加わる。

### 案C: 組織の SCP が許すリージョンの集合を入力で受ける

リージョンを許可の一覧で絞る SCP は持たない（`organization-baseline` の SCP は拒否の一覧の方式）。既定のリージョンは無効にできないので、集合を絞っても、そこに資源を作られうることは変わらない。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 低 | 中（集合が黙って変わる） | 中 | 高 |
| Testability | 高 | 低（集合が plan で決まらない） | 中 | 高 |
| Contract Clarity | 高 | 中 | 低（AWS の表現を入力に取る） | 高 |
| Maintainability | 高 | 低 | 中 | 中（17倍の instance） |
| Scope Control | 中 | 低 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- opt-in のリージョンを使うこと
- aws の外の partition で使うこと

## 保証範囲

- 保証すること: reference の plan で、リージョンごとの設定と Config の記録が、宣言した集合のリージョンに過不足なく在ること。partition が `aws` でなければ plan で落ちること。アカウントで有効なリージョンが集合の外を含めば plan で落ちること
- 保証しないこと: 管理アカウントで opt-in のリージョンが有効にされることを拒むこと。決定3 の検出は Terraform の評価であって CI のゲートの Policy ではなく、その plan を走らせない経路では効かない。集合のすべてのリージョンへ CI の経路が apply できること（egress の宣言と apply の role の権限は #474 が持つ）

## 検証方法

- Unit Test: `tests/contract.tftest.hcl` の `workload_account` が aws の集合（17、opt-in の `af-south-1` を含まない）が届くことを、`unsupported_partition` が aws の外の partition で plan が落ちることを確かめる。`internal/regional-controls/tests/` の `opt_in_region_enabled` が、集合の外の有効なリージョンで plan が落ちることを、`empty_regions` が空の集合を拒むことを確かめる
- Policy Test（`make tf-policy`）: `PLAN-ABL-01` が、usecase の instance ごとに、リージョンごとの型と Config の型が `conftest/plan/decl.json` の `account_baseline.regions` に過不足なく在ることを見る。region が plan で決まらない変更も落とす

## 影響

- 各リージョンの instance が集合の数だけ増え、apply の role の権限と CI の egress も集合のリージョンについて要る
- 集合は usecase の `main.tf` と `conftest/plan/decl.json` の2箇所に在る。どちらかだけを変えると `PLAN-ABL-01` で落ちる

## 見直し条件

- opt-in のリージョンを使う利用者が現れたとき
- aws の外の partition で使う利用者が現れたとき
- 全リージョンに置く設定を持つ usecase が2つ以上になり、集合を repository-wide の決定にするとき（#474）
