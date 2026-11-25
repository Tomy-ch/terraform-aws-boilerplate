# default-regions

partition ごとの、AWS が既定で有効とするリージョン（opt-in でないもの）の集合を出力する一般 module（ADR-0102 決定12、[ADR-0209](../../docs/adr/0209-default-regions-for-regional-controls.md)）。resource・provider・入力を持たない。リージョンごとの安全側の設定を置く usecase と、それを apply する CI の role の権限が、同じ集合を読む。

## 持つもの

| 出力 | 値 | 使う側 |
| --- | --- | --- |
| `by_partition` | partition を key にした、既定で有効なリージョンの set。`aws` の17だけを持つ | `account-baseline`（`for_each` と `region = each.key`）、`deployment-identity`（CI の role の `aws:RequestedRegion` の Deny と `ec2:Region` の条件） |

既定で有効なリージョンは、2019年3月20日より前に提供されたもので、無効にできない（AWS Account Management Reference Guide「Enable or disable AWS Regions in your account」）。集合は増えも減りもしないので、literal で持つ。

## 持たないもの

- `aws` の外の partition の集合。key を置かないので、読む usecase は plan を止めるか、文を置かない（ADR-0209 決定2・4）
- opt-in のリージョン（ADR-0209 の意図的に捨てるもの）
- 利用者の入力。集合を入力にしない（ADR-0204 決定2、ADR-0208 決定3）

## 写し

Terraform を読まない道具のための写しは次の2つに限る（ADR-0209 決定6）。

- `conftest/plan/decl.json` の `default_regions.aws`: account-baseline の reference の plan のリージョンとずれると `PLAN-ACB-01` が、CI の role の Deny とずれると `PLAN-REGION-02` が落とす
- `.github/egress.toml` の `region_set`: 集合に置く root を実行する job を宣言する変更が足す。置く region と宛先の region がずれると `make egress-check` が落とす

## 保証しないもの

- 集合が AWS の定義と一致すること。`tests/contract.tftest.hcl` は、テストに書いた17のリージョンとの完全一致を見るので、AWS の定義が変わってもテストは落ちない
- usecase が、この module を呼ばずに同じ集合を literal で書くことを拒むこと。レビューだけが見る

## 版

- Terraform は `>= 1.7.0, < 2.0.0`。provider を持たない

## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-25

- 参照した一次資料: AWS Account Management Reference Guide「Enable or disable AWS Regions in your account」（既定で有効な17のリージョンの表と、opt-in のリージョンの表）
- AWS / HashiCorp が publisher の、リージョンの集合を持つ Registry module は探していない。集合は AWS の定義の写しで、機構を持たないためである
