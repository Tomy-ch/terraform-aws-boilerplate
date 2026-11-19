# Architecture Decision Records (organization-baseline)

`organization-baseline` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-governance-in-management-identity-root.md) | 統制を管理アカウントの identity root に置き、Identity Center と root の中央管理を委任しない | Accepted |

### 01 組織の構造と policy

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-organizational-units-and-policies.md) | OU の木と、SCP・RCP・declarative policy の中身と付け先を内部で固定する | Accepted |

### 02 人の到達

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-break-glass.md) | break-glass を管理アカウントの IAM user と role で持つ | Accepted |
| [0202](0202-member-break-glass-via-stacksets.md) | メンバーアカウントの break-glass の role を、service-managed の StackSet で配る | Accepted |
