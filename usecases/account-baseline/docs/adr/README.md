# Architecture Decision Records (account-baseline)

`account-baseline` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-workload-root-in-every-account.md) | 全アカウントの workload root から呼び、アカウントかリージョンに1つしか置けない安全側の設定だけを持ち、experimental で始める | Accepted |

### 01 設定の置き方

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-default-regions-per-partition.md) | リージョンごとの設定を、partition ごとの既定で有効な全リージョンに置き、有効なリージョンが集合の外なら plan を止める | Accepted |
| [0102](0102-config-recorder-except-sandbox.md) | AWS Config を sandbox の外の全アカウント・全リージョンで記録し、log-archive へ配信する | Accepted |
| [0103](0103-service-linked-roles-by-arn-reference.md) | このユースケースは、AWS Config の記録に要る service-linked role だけを持つ | Accepted |

### 02 組織の拒否

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-baseline-settings-changed-only-by-ci-apply.md) | アカウント単位の安全側の設定は、CI の apply の role の外から止めさせず、opt-in のリージョンは誰にも有効にさせない | Accepted |
