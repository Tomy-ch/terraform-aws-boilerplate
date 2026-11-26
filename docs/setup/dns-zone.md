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
- この手順書の make の入口は、zone を置くアカウントの profile（`PROFILE`）、その env の名前（`ENV`）、zone の名前（`ZONE`。prd は `<apex_domain>`、ほかは `<env>.<apex_domain>`）を受け、region を `ENV` の workload root（`env/<ENV>`）の provider の region から読む

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

dev と stg の env の値に `apex_domain` を書き、`delegations` を空の map にして apply する。apply の後に、それぞれのアカウントで次の入口を実行し、出力を控える。入口は zone の NS と、署名が `SIGNING` でただ1つの `ACTIVE` な KSK の DS を読み、prd の `delegations` に書く形で出す。NS と DS の形は `delegations` の validation と同じものを確かめる。署名がまだ `SIGNING` でなければ `ds_record` を省き、その理由を出す

```sh
make setup-dns-zone-delegation-values PROFILE=<dev のアカウントの profile> ENV=dev ZONE=dev.<apex_domain>
make setup-dns-zone-delegation-values PROFILE=<stg のアカウントの profile> ENV=stg ZONE=stg.<apex_domain>
```

### 3.3 prd の zone を作り、dev と stg を委任する

prd の env の値に `apex_domain` と、3.2 で入口が出した値を `delegations` として書き、apply する。

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

DS は、NS だけで委任が引けることを確かめた後に足してよい。3.2 の入口が `ds_record` を省いたときは、署名が戻ってから入口を実行し直して足す。

dev か stg の zone を作り直したとき（Policy の許可の宣言を通した置換）は、新しい zone の apply より先に、prd の `delegations` からその子の NS と DS を外して apply する。古い NS を親に残したままにしない。

### 3.4 registrar に prd の NS と DS を登録する

prd の zone の NS を registrar の name server に、KSK の DS を registrar の DS に登録する。★ 順序を守る。順を誤ると名前が引けなくなる（Route 53 の DNSSEC の文書が警告する）。

- DS は、name server の切り替えが行き渡った後（親の zone の NS の TTL を過ぎた後）に足す。行き渡る前は、古い name server が新しい KSK で署名していない応答を返し、DS と合わない
- registrar に別の鍵の DS が残ったまま name server を替えない。新しい zone の KSK がその DS と合わない

ドメインをこのアカウントの Route 53 Domains に登録したときは、prd のアカウントの profile で次の入口を順に実行する。

1. registrar の name server を zone の NS に替える。registrar に zone の KSK のものでない DS が在れば、替えずに止まる（先に 4 の手順1 で DS を外す）。一致していれば書かない

   ```sh
   make setup-dns-zone-registrar-ns PROFILE=<prd のアカウントの profile> ENV=prd ZONE=<apex_domain>
   ```

2. 切り替えが行き渡るのを待ってから、registrar に KSK の DS を足す。入口は、registrar の name server が zone の NS と一致し、zone の署名が `SIGNING` で KSK がただ1つ `ACTIVE` のときだけ書く。同じ公開鍵の DS が在れば書かない。行き渡ったかは入口が確かめられないので、3.5 の `dig +short NS <apex_domain>` が zone の NS を返すことを確かめてから実行する

   ```sh
   make setup-dns-zone-registrar-ds PROFILE=<prd のアカウントの profile> ENV=prd ZONE=<apex_domain>
   ```

ほかの registrar では、prd の apply の出力の `name_servers` を registrar の name server に登録し、行き渡った後に `ds_record` を registrar の DS に登録する。どちらも registrar の画面で行い、入口は無い。

### 3.5 確かめる

```sh
dig +short NS <apex_domain>
dig +short NS dev.<apex_domain>
dig +dnssec +short SOA dev.<apex_domain>
```

NS が apply の出力と一致し、`+dnssec` の問い合わせに RRSIG が付くことを確かめる。zone の側の署名は、zone ごとに次の入口で確かめる。署名が `SIGNING` で、KSK がただ1つ `ACTIVE` で、その KMS の鍵が us-east-1・`Enabled`・`ECC_NIST_P256`・`SIGN_VERIFY` であれば通る

```sh
make setup-dns-zone-signing-check PROFILE=<そのアカウントの profile> ENV=<env> ZONE=<zone の名前>
```

## 4. DS の不整合から戻す

DNSSEC の alarm が鳴ったときや、DS の不整合で名前が引けなくなったときは、次の順で行う（Route 53 Developer Guide「Disabling DNSSEC signing」「Troubleshooting DNSSEC signing」）。

1. 親の DS を外す（apex は registrar、dev と stg は prd の `delegations` の `ds_record` を消して apply）
2. DS の TTL（300 秒。registrar の DS は registrar の TTL）を待つ
3. KSK の状態が `ACTION_NEEDED` なら、鍵が有効で Key Policy が変わっていないことを確かめ、次の入口で KSK を有効に戻す。入口は、KSK がただ1つで `ACTION_NEEDED` で、KMS の鍵が 3.5 の要件に合うときだけ、project の名前の入力を受けて書く。Key Policy は入口が確かめないので、plan で差分が無いことを先に確かめる

   ```sh
   make setup-dns-zone-ksk-activate PROFILE=<そのアカウントの profile> ENV=<env> ZONE=<zone の名前>
   ```

4. 署名が戻ったことを 3.5 で確かめてから、DS を写し直す

このユースケースは DNSSEC を外した zone を持たない。署名そのものを外すことは契約の変更である。

## 5. KSK を入れ替える

KSK の rotation は Route 53 が自動では行わない。新しい KSK を足し、親の DS を新しい KSK の値へ写し、古い DS の TTL を待ってから古い KSK を外す。KSK の追加はこのユースケースの契約の変更を要する。
