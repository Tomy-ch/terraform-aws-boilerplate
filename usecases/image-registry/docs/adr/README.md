# Architecture Decision Records (image-registry)

`image-registry` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 01 発火側

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-publisher-trust.md) | push の role の信頼条件を、発火側の immutable 形式の environment の sub と ID で限定する | Accepted |
| [0102](0102-publish-permission-vocabulary.md) | push の role の権限を、push とスキャン結果の読み取りの語彙に固定する | Accepted |

### 02 イメージの保護

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-customer-managed-key.md) | repository をアカウントに1本の CMK で暗号化する | Accepted |
| [0202](0202-repository-write-deny.md) | repository policy は Allow を持たず、push の role 以外の書き込みとイメージの削除を拒否する | Accepted |
| [0203](0203-count-based-lifecycle.md) | lifecycle はタグ付きのイメージを件数だけで消し、件数の短縮を取り返しのつかない変更とする | Accepted |

### 03 スキャン

| ID | タイトル | Status |
| --- | --- | --- |
| [0301](0301-registry-enhanced-scanning.md) | registry のスキャンを ENHANCED の継続スキャンとし、このユースケースだけが設定を持つ | Accepted |
