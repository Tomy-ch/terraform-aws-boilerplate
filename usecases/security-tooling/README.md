# security-tooling

組織の検知を、security のアカウントの委任管理者で持つユースケース（[`docs/v1.0-plan/scope.md`](../../docs/v1.0-plan/scope.md) 第7節）。**experimental** であり、昇格と削除の条件は [security-tooling/ADR-0001](docs/adr/0001-security-account-delegated-admin.md) が持つ。

このディレクトリは設計記録（この README と `docs/adr/`）だけを持ち、Terraform の実装を持たない。

## 解決する問題

組織のすべてのアカウントの API の記録を、記録される側の主体が止めにくい場所で受け、平時には起きない事象を人へ知らせる。

- 管理アカウントには trail が無い。CloudTrail の event history は90日で消えるので、それより古い監査の記録が組織のどこにも残らない。
- break-glass の user のサインイン、break-glass の role の引き受け、root の使用、`sts:AssumeRoot` の呼び出しは、必ず通知すると決まっている（[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定19）。その通知の経路が無い。

## 持つもの（設計）

| 構成物 | 決定 |
| --- | --- |
| 組織の trail と、CloudWatch Logs への配送 | [security-tooling/ADR-0101](docs/adr/0101-organization-trail.md) |
| break-glass・root・`sts:AssumeRoot` の使用の metric filter と alarm | [security-tooling/ADR-0102](docs/adr/0102-break-glass-and-root-usage-alarms.md) |

GuardDuty、Config の集約、Access Analyzer（組織）、Inspector、Security Hub は、このユースケースが持つ予定の範囲で、決定はまだ無い（#473）。

## 公式実装の参照（[ADR-0206](../../docs/adr/0206-official-implementations-as-reference.md) 決定7・8）

参照日: 2026-11-27

- 参照した一次資料: [AWS SRA の Security Tooling のアカウント](https://docs.aws.amazon.com/prescriptive-guidance/latest/security-reference-architecture/security-tooling.html)、CloudTrail User Guide（[Organization trails](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/creating-trail-organization.html)、[Delegated administrator](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-delegated-administrator.html)、[Concepts](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-concepts.html)、[AWS console sign-in events](https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-event-reference-aws-console-sign-in-events.html)）、[IAM User Guide の CloudTrail との統合](https://docs.aws.amazon.com/IAM/latest/UserGuide/cloudtrail-integration.html)、Amazon EventBridge User Guide（[CloudTrail の事象](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-service-event-cloudtrail.html)、[リージョン越え](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-cross-region.html)）、[Amazon SNS の鍵の管理](https://docs.aws.amazon.com/sns/latest/dg/sns-key-management.html)、[Security Hub の CloudTrail の control](https://docs.aws.amazon.com/securityhub/latest/userguide/cloudtrail-controls.html)・[CloudWatch の control](https://docs.aws.amazon.com/securityhub/latest/userguide/cloudwatch-controls.html)
- AWS Provider 6.73.0（このリポジトリの lock の版）の `aws_cloudtrail`・`aws_cloudwatch_log_metric_filter`・`aws_cloudwatch_metric_alarm` の schema は、Terraform の実装を足す変更が確かめる。この README は Terraform の実装を持たない

採用した知見:

- 組織の trail を CloudTrail の委任管理者から管理する（SRA）。所有者は管理アカウントのまま（CloudTrail User Guide）
- 管理イベントの読み書きを記録する multi-region の trail（Security Hub CloudTrail.1）、保管時の暗号化（CloudTrail.2）、log file validation（CloudTrail.4）、CloudWatch Logs との統合（CloudTrail.5）
- root の使用の metric filter と alarm（Security Hub CloudWatch.1）

採用しなかった知見と理由:

- Amazon EventBridge の rule による通知: リージョンごとの rule と role と、`ops-notification` の鍵の契約の変更が要り、opt-in のリージョンが漏れる（[security-tooling/ADR-0102](docs/adr/0102-break-glass-and-root-usage-alarms.md)）
- Security Hub CloudWatch.2-14 の metric filter と alarm: [security-tooling/ADR-0102](docs/adr/0102-break-glass-and-root-usage-alarms.md) の「公式推奨からの逸脱」

## supported / unsupported

supported:

- security のアカウントの workload root（`env/security/`）から1回だけ呼ぶ構成

unsupported:

- 管理アカウントとメンバーアカウントの root から呼ぶこと（[security-tooling/ADR-0001](docs/adr/0001-security-account-delegated-admin.md)）
- 組織を作らない構成（`organization-baseline` の unsupported に従う）

## 所有者

- 組織の trail の所有者は、AWS の仕様で管理アカウントである。このユースケースは、委任管理者の security のアカウントから作る
- 委任管理者の指定は `organization-baseline` が持つ。CloudTrail の信頼アクセスは、人の手順で有効にする（organization-baseline/ADR-0001 決定5）
- 証跡の保管庫（Bucket と鍵）は `audit-evidence` が持つ
- 通知の Topic は、security のアカウントの `ops-notification` が持つ

## 依存するユースケースと判断 Issue

- ユースケース: `audit-evidence`（log-archive のアカウント）、`ops-notification`（security のアカウント）、`organization-baseline`（委任管理者の指定）、`deployment-identity`（security のアカウントの apply の role と permissions boundary）
- 判断 Issue: #473（このユースケース）、#610（break-glass と root の通知）
