# dns-zone 手順書

## 1. 設定の概要

環境ごとの公開の Hosted Zone を、登録したドメインの下に置く（[`usecases/dns-zone/README.md`](../../usecases/dns-zone/README.md)）。zone・クエリログ・DNSSEC の鍵と署名・prd の委任の record は、各アカウントの workload root を CI が apply して作る。人が行うのは、Terraform が持たないドメインの登録と registrar の設定、アカウントをまたぐ値（子の zone の name server と DS）の写し、KSK の rotation である。

| 実施者 | 担当 |
| --- | --- |
| ドメインの registrar の契約者 | ドメインの登録、registrar への NS と DS の登録 |
| リポジトリの管理者 | env の値を書く Pull Request と、apply の後の確認 |

前提:

- 各環境のアカウントの workload root が `dns-zone` を呼べること（dev・stg・prd は #330、sandbox は #331）
- 同じアカウントの us-east-1 に、DNSSEC の alarm の通知先の Topic があること（#421）

## 2. 設定完了後の状態

- prd のアカウントに apex の zone（`<apex_domain>`）があり、registrar の NS と DS がその zone を指す
- dev と stg のアカウントに `<env>.<apex_domain>` の zone があり、prd の zone がその NS と DS を委任の record として持つ
- sandbox のアカウントに `sandbox.<apex_domain>` の zone がある。委任しない
- どの zone も DNSSEC で署名され、クエリログを us-east-1 の Log Group へ送る

## 3. 手順

順序を守る。子の zone を作る前に親へ NS を書かない。DS は、その zone の署名が有効になった後に足す。

### 3.1 ドメインを登録する

registrar で `apex_domain` を登録する。登録は Terraform の外で、利用者が registrar を選ぶ。

### 3.2 dev と stg の zone を作る

dev と stg の env の値に `apex_domain` を書き、`delegations` を空の map にして apply する。apply の出力の `name_servers` と `ds_record` を控える。

### 3.3 prd の zone を作り、dev と stg を委任する

prd の env の値に `apex_domain` と、3.2 で控えた値を `delegations` として書き、apply する。

```hcl
delegations = {
  dev = {
    name_servers = ["ns-...", "ns-...", "ns-...", "ns-..."]
    ds_record    = "<key tag> 13 2 <digest>"
  }
  stg = {
    name_servers = ["ns-...", "ns-...", "ns-...", "ns-..."]
    ds_record    = "<key tag> 13 2 <digest>"
  }
}
```

DS は、NS だけで委任が引けることを確かめた後に足してよい。

dev か stg の zone を作り直したとき（Policy の許可の宣言を通した置換）は、新しい zone の apply より先に、prd の `delegations` からその子の NS と DS を外して apply する。古い NS を親に残したままにしない。

### 3.4 registrar に prd の NS と DS を登録する

prd の apply の出力の `name_servers` を registrar の name server に、`ds_record` を registrar の DS に登録する。DS の登録は、name server の切り替えが行き渡った後に行う。

### 3.5 確かめる

```sh
dig +short NS <apex_domain>
dig +short NS dev.<apex_domain>
dig +dnssec +short SOA dev.<apex_domain>
```

NS が apply の出力と一致し、`+dnssec` の問い合わせに RRSIG が付くことを確かめる。

## 4. DS の不整合から戻す

DNSSEC の alarm が鳴ったときや、DS の不整合で名前が引けなくなったときは、次の順で行う（Route 53 Developer Guide「Disabling DNSSEC signing」「Troubleshooting DNSSEC signing」）。

1. 親の DS を外す（apex は registrar、dev と stg は prd の `delegations` の `ds_record` を消して apply）
2. DS の TTL（300 秒。registrar の DS は registrar の TTL）を待つ
3. KSK の状態が `ACTION_NEEDED` なら、鍵が有効で Key Policy が変わっていないことを確かめ、KSK を有効に戻す
4. 署名が戻ったことを 3.5 で確かめてから、DS を写し直す

このユースケースは DNSSEC を外した zone を持たない。署名そのものを外すことは契約の変更である。

## 5. KSK を入れ替える

KSK の rotation は Route 53 が自動では行わない。新しい KSK を足し、親の DS を新しい KSK の値へ写し、古い DS の TTL を待ってから古い KSK を外す。KSK の追加はこのユースケースの契約の変更を要する。
