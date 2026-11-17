# Architecture Decision Records (deployment-identity)

`deployment-identity` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-ci-roles-in-management-account.md) | 管理アカウントに、IAM を変更できる CI の role を置く | Accepted |

### 01 信頼条件

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-trust-by-immutable-environment-subject.md) | 信頼条件を immutable 形式の environment の sub と、リポジトリと所有者の ID で限定する | Accepted |

### 02 権限

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-enumerated-permissions.md) | CI の role の権限を、Provider が呼ぶ API から導いた Action の列挙で与える | Accepted |
