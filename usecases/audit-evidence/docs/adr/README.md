# Architecture Decision Records (audit-evidence)

`audit-evidence` ユースケースにだけ適用される設計判断。repository-wide の ADR は [`docs/adr/`](../../../../docs/adr/README.md) が持ち、配置と番号は [ADR-0001](../../../../docs/adr/0001-adr-process-and-placement.md) に従う。

## 一覧

### 00 配置

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-log-archive-placement.md) | 組織の証跡の保管庫を log-archive のアカウントに1つ置き、experimental で始める | Accepted |

### 01 保管の保護

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-object-lock-retention.md) | 証跡の各版を Object Lock の既定の保持でロックし、同じ日数で失効させる | Accepted |
| [0102](0102-archive-bucket-policy.md) | 保管の Bucket Policy は、主体の例外を持たない Deny と、型付きの書き手のサービスへの Allow だけを持つ | Accepted |
| [0103](0103-deviations-replication-mfa-delete-notifications.md) | 保管の Bucket に、リージョン間の複製・MFA delete・event notification を持たせない | Accepted |
| [0104](0104-server-access-logs-to-cloudwatch.md) | 保管の Bucket の S3 サーバーアクセスログを、CloudWatch Logs へ配送する | Accepted |

### 02 鍵

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-archive-key-cross-account-services.md) | 証跡の鍵をこのユースケースが持ち、鍵の外のアカウントの trail と Config に型付きで暗号化を許す | Accepted |

### 03 監視

| ID | タイトル | Status |
| --- | --- | --- |
| [0301](0301-delivery-stall-alarm.md) | 保管の Bucket への書き込みが1時間途絶えたら、同じアカウントの通知の Topic へ知らせる | Accepted |
