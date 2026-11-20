# Architecture Decision Records (dns-zone)

`dns-zone` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-one-zone-per-environment-account.md) | 各環境のアカウントに公開の zone を1つ置き、prd を apex とする | Accepted |

### 01 委任と record

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-delegation-values-passed-by-input.md) | 委任の NS と DS の値を入力で受け、prd の apply だけが書く | Accepted |
| [0102](0102-records-written-by-target-owner.md) | record を、指す先を持つユースケースが書く | Accepted |

### 02 zone の保護

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-zone-replacement-and-deletion-stopped.md) | zone の置換と削除を止め、force_destroy と accelerated recovery を false に固定する | Accepted |

### 03 us-east-1 の観測と署名

| ID | タイトル | Status |
| --- | --- | --- |
| [0301](0301-query-logging-in-us-east-1.md) | 公開 DNS のクエリログを us-east-1 の、このユースケースの鍵で暗号化した Log Group へ送る | Accepted |
| [0302](0302-dnssec-with-own-key.md) | DNSSEC をこのユースケースの非対称の鍵で有効にし、2つの alarm を置く | Accepted |
| [0303](0303-us-east-1-by-region-argument.md) | us-east-1 の資源を、provider の alias ではなく resource の region 引数で置く | Accepted |
