# ADR-0208: us-east-1 にしか置けない資源は、resource の region 引数で置く

- Status: Accepted
- Date: 2026-11-20
- Scope: repository-wide
- Related: ADR-0001, ADR-0102, ADR-0204, ADR-0205, ADR-0501

## Context

いくつかの AWS のサービスは、資源や metric を us-east-1 にしか持たない。Route 53 の公開 Hosted Zone の query log と DNSSEC の鍵と DNSSEC の metric、CloudFront の metric と CloudFront に付ける ACM の証明書がそれに当たる。その metric の alarm と通知先の Topic も、alarm と同じ region に置く。CloudWatch の alarm が別の region の SNS Topic を action に取れることを、AWS の文書は述べていない。

env の root は provider を1つの region で渡す。usecase や module が `configuration_aliases` で us-east-1 の provider を求めると、そのディレクトリでの `terraform validate` が、渡されない provider を理由に落ちる。CI のランナーが出てよい宛先は `make egress-check` が root から辿る block の `region` から導き、region の形の literal でない `region` を落とす。`region = each.key` だけは、block の `for_each` を検査が閉じた形で解ける集合（literal に辿り着く `local`・呼び手の `var`・module の `output`。形の一覧は `scripts/README.md` の `egress/` の行）として読み、その各 region とする。スカラーの `region` は決定1 のとおり literal に限る。AWS Provider v6 は多くの resource に `region` 引数を持ち、plan の JSON に region が値として現れる。一般 module に `region` の入力を足さないことは、us-east-1 に資源を置くどの usecase にも当たるので、repository-wide に置く（ADR-0001 決定20）。

## Decision

1. us-east-1 にしか置けない資源と、その資源の metric の alarm の通知先は、usecase の resource に `region = "us-east-1"` を literal で書いて置く。変数・local・`configuration_aliases` を通さない。
2. 一般 module（`modules/`）に `region` の入力を足さない。us-east-1 に置く資源を一般 module と同じ値で持つ必要がある usecase は、その資源を自分の `internal/` に持つ。この重複は ADR-0205 決定11 の重複であり、値の一致は Policy Test が一般 module と同じ検査で見る。
3. us-east-1 は、usecase が利用者から受ける入力にしない。region を入力にすると、受け取る値の意味が AWS の表現に依存する（ADR-0204 決定2）。
4. us-east-1 に資源を置く usecase は、CI の role の権限を provider の region と us-east-1 の2つについて列挙する。ランナーの egress の宛先は、`make egress-check` が `region` の literal から導くものを宣言する。
5. us-east-1 は aws の partition の region である。aws の外の partition では、CI の role の権限に us-east-1 を足さない。

## 検討した代替案

### 案A: `configuration_aliases` で us-east-1 の provider を受ける

必須のゲートの `terraform validate` が usecase のディレクトリで落ちる。plan の JSON から resource の region を値として読めず、Policy Test が us-east-1 であることを見られない。

### 案B: 一般 module に optional の入力 `region` を足す

不変条件を1箇所に保てるが、`region = var.region` は literal でないので、その module を呼ぶすべての root の `make egress-check` が落ちる。egress の検査が module の変数を呼び手の値から解くようになれば取れる。

### 案C: alarm を provider の region に置き、通知先を provider の region の Topic にする

us-east-1 にしか出ない metric は、ほかの region の alarm から見えない。

## 意図的に捨てるもの

- us-east-1 に置く資源が、一般 module の変更に自動で追随すること。追随の漏れは、一般 module と同じ Policy Test が落とす範囲に限って検出する。

## 保証範囲

- CI の role の権限が provider の region と us-east-1 に及び、aws の外の partition では及ばないことは、CI の role を持つ usecase の Unit Test が見る。
- us-east-1 に資源を置く usecase の、資源の region と文書の形は、その usecase の Unit Test と Policy Test が見る。属性の値が一般 module と一致することは、両方の Unit Test が同じ値を固定することで保つ。片方の値を変えたときにもう片方を変え忘れても、どの検査も落ちない。
- `region` 引数を実際の AWS が受けることは、usecase ごとの sandbox の Integration Test が見る。sandbox の Integration Test を走らせる経路が無い間は確かめない。

## 検証方法

- CI の role を持つ usecase の Unit Test、us-east-1 に資源を置く usecase の `internal/*/tests/` と `conftest/plan/` の Policy、`make egress-check`

## 影響

- us-east-1 に資源を置く usecase は、`internal/` に一般 module と同じ値を写す。
- CI の apply・plan の role の権限は us-east-1 を含む。ランナーの egress の us-east-1 の宛先は、us-east-1 に資源を置く root を実行する job に限って持つ（`make egress-check` は root ごとに、置く region の宛先だけを求める）。

## 見直し条件

- `terraform validate` が `configuration_aliases` を持つディレクトリを単独で検証できるようになったとき。
- egress の検査が、スカラーの `region` に渡る module の変数を呼び手の値から解くようになったとき（`for_each` の集合は解くが、スカラーの `region` は決定1 を守るために解かない）。
- CloudWatch の alarm が別の region の Topic を action に取れることを AWS が文書にしたとき。
