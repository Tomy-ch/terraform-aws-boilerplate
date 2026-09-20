# Terraform AWS Provider attachment 型 resource の親引数 対応表

> **この文書は暫定の置き場に在る。** Policy Test を配線したとき、その data として移す。移設後にこのパスへの参照が残らないよう、**リポジトリに残るものからこの文書を参照しない**（詳細は [README](README.md)）。

- 確認した Provider version: **hashicorp/aws 6.65.0**（2026-09-16 公開、2026-09-20 時点の latest）。確認日: 2026-09-20。
- 確認手段: provider リポジトリ `hashicorp/terraform-provider-aws` の `website/docs/r/*.html.markdown` を tag `v6.65.0` で参照し、Registry API（`GET https://registry.terraform.io/v2/provider-docs/<id>`）の本文と diff して一致（末尾改行のみ差）を確認した。
- URL 列の基底: `https://registry.terraform.io/providers/hashicorp/aws/6.65.0/docs/resources/` —— 以下 `r/<slug>` と略記する。

## 確認方法

- Terraform MCP Server は作業セッションに存在しなかった（ツール一覧に無い）。Registry のページ（`registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/<name>`）は JS 描画で WebFetch が本文を取れなかった。
- 代わりに、Registry が描画する元である provider リポジトリの `website/docs/r/*.html.markdown` を **tag `v6.65.0`** で sparse clone し（`/tmp/tabp/awsdocs/repo/`）、そこから各 resource の Argument Reference を読んだ。Registry API（`GET https://registry.terraform.io/v1/providers/hashicorp/aws` → `version: 6.65.0`, `published_at: 2026-09-16`）で latest = 6.65.0 を確認し、`GET https://registry.terraform.io/v2/provider-docs/<id>` の本文と clone した `sqs_queue_policy` / `vpc_security_group_ingress_rule` の 2 ファイルを diff して末尾改行以外 同一であることを確認した。
- 下表の引数名はすべてこの手順で読んだもの。**読んでいないものは表に載せていない**（「未確認」行は無い）。
- 引数の後ろの `!` は Required、無印は Optional（docs の表記どおり）。

## 1. 判定基準

**attachment 型** = 必須引数で **ちょうど1つの親 resource を名指し**し、その親の属性（policy / 設定 / ルール集合 / 関連付け）を書き換える effect を持ち、**親 + 引数の組で同一性が決まる**（自前の名前・ARN を持たないか、持っても親の存在を前提とする）resource。

- **S3 の `aws_s3_bucket_*`（versioning / SSE / public access block / ownership / object lock 等）は含める。** この層規則の目的は「親と設定が別 module に分かれて不変条件が壊れるのを防ぐ」ことであり、これらは bucket の不変条件そのものである（Provider v4 で親から分離されただけで、AWS API 上は bucket の属性）。費用: 検査対象が約 20 型増えるが、親引数は全て `bucket` で一様なので Rego の分岐は増えない。
- **2つの resource を結ぶ association**（`aws_iam_role_policy_attachment`、`aws_wafv2_web_acl_association`、`aws_route_table_association` 等）は含め、「親」は **守られる側 / 不変条件を持つ側**（要件 3.1 の表で入力を受け取る側）と定める。もう一方の引数は ARN/ID を跨いで受けてよい。
- **除外**: 自前の identity と lifecycle を持つ子 resource（`aws_lb_listener`、API Gateway の resource/method/integration 木、`aws_cognito_user_pool_client`、`aws_ecs_service`、`aws_rds_cluster_instance`、`aws_route53_record`、`aws_cloudwatch_event_rule`、`aws_eks_node_group` 等）。これらは「関心事単位の module 粒度」（3.1）で扱うものであり、attachment 検査で縛ると module 分割の自由度を不当に奪う。§2 末尾に一覧だけ置く。
- **親を持たないもの**（アカウント / リージョン単位）は検査対象外として §2 末尾に列挙する。

## 2. 対応表（検査対象 = attachment 型）

### S3

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_s3_bucket_policy` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_policy | bucket **名**。`aws_s3_bucket` 側の inline `policy` は **Deprecated** |
| `aws_s3_bucket_public_access_block` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_public_access_block | `skip_destroy` あり |
| `aws_s3_bucket_versioning` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_versioning | 親側 inline `versioning` は Deprecated |
| `aws_s3_bucket_server_side_encryption_configuration` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_server_side_encryption_configuration | `rule.apply_server_side_encryption_by_default.kms_master_key_id` は他 module の KMS を指してよい（親引数ではない） |
| `aws_s3_bucket_lifecycle_configuration` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_lifecycle_configuration | |
| `aws_s3_bucket_ownership_controls` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_ownership_controls | |
| `aws_s3_bucket_notification` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_notification | `queue.queue_arn` / `topic.topic_arn` / `lambda_function.lambda_function_arn` / `eventbridge` は宛先（親ではない）。API は bucket 全体の設定を置換するので **bucket ごとに 1 つ** |
| `aws_s3_bucket_object_lock_configuration` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_object_lock_configuration | `audit-evidence` |
| `aws_s3_bucket_logging` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_logging | `target_bucket`! は**ログ先** bucket（親ではない、別 module でよい） |
| `aws_s3_bucket_cors_configuration` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_cors_configuration | `media-ingest` の署名付き PUT |
| `aws_s3_bucket_acl` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_acl | `expected_bucket_owner` は Deprecated。destroy しても ACL は残る |
| `aws_s3_bucket_replication_configuration` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_replication_configuration | `rule.destination.bucket`! は宛先 ARN。`role`! は IAM。`data-refresh` で使うなら |
| `aws_s3_bucket_website_configuration` / `_request_payment_configuration` / `_accelerate_configuration` / `_intelligent_tiering_configuration` / `_metric` / `_inventory` / `_analytics_configuration` | `bucket`! | `aws_s3_bucket` | r/s3_bucket_website_configuration 他 各 slug | 40 ユースケースで使う見込みは薄いが親引数は同じ `bucket`。inventory/analytics の `destination.*.bucket_arn` は宛先 |

### SQS / SNS / KMS

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_sqs_queue_policy` | `queue_url`! | `aws_sqs_queue` | r/sqs_queue_policy | **URL** で指す（`aws_sqs_queue.x.id` / `.url`）。ARN 不可。親側 inline `policy` は現役（Deprecated ではない） |
| `aws_sqs_queue_redrive_policy` | `queue_url`! | `aws_sqs_queue` | r/sqs_queue_redrive_policy | `redrive_policy` JSON 内の `deadLetterTargetArn` は DLQ（親ではない）。親側 inline `redrive_policy` も現役 |
| `aws_sqs_queue_redrive_allow_policy` | `queue_url`! | `aws_sqs_queue`（DLQ 側） | r/sqs_queue_redrive_allow_policy | 親は **DLQ**。許可される source queue は JSON 内 |
| `aws_sns_topic_policy` | `arn`! | `aws_sns_topic` | r/sns_topic_policy | 引数名が素の `arn`。親側 inline `policy` 現役 |
| `aws_sns_topic_data_protection_policy` | `arn`! | `aws_sns_topic` | r/sns_topic_data_protection_policy | |
| `aws_sns_topic_subscription` | `topic_arn`! | `aws_sns_topic` | r/sns_topic_subscription | `endpoint`! は宛先（SQS ARN 等、別 module でよい）。SQS 側の queue policy は queue の module が生成する（3.1 の型付き入力） |
| `aws_kms_key_policy` | `key_id`! | `aws_kms_key` | r/kms_key_policy | `bypass_policy_lockout_safety_check` あり。親側 inline `policy` と排他（docs NOTE） |
| `aws_kms_alias` | `target_key_id`! | `aws_kms_key` | r/kms_alias | **ARN でも key_id でも可** |
| `aws_kms_grant` | `key_id`! | `aws_kms_key` | r/kms_grant | key ID か ARN。`grantee_principal`! は他 module の role でよい |

### IAM

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_iam_role_policy` | `role`! | `aws_iam_role` | r/iam_role_policy | role **名** |
| `aws_iam_role_policy_attachment` | `role`（Required） | `aws_iam_role` | r/iam_role_policy_attachment | `policy_arn` は managed policy（別 module でよい）。`aws_iam_policy_attachment` と併用不可 |
| `aws_iam_role_policies_exclusive` | `role_name`! | `aws_iam_role` | r/iam_role_policies_exclusive | 親側 `inline_policy` の後継 |
| `aws_iam_role_policy_attachments_exclusive` | `role_name`! | `aws_iam_role` | r/iam_role_policy_attachments_exclusive | 親側 `managed_policy_arns` の後継 |
| `aws_iam_policy_attachment` | `roles` / `users` / `groups`（いずれも Optional、複数） | `aws_iam_role` 等 | r/iam_policy_attachment | **アカウント全体で排他**。親が複数かつ Optional なので本検査の形に合わない。使わないことを推奨 |
| `aws_iam_user_policy` / `aws_iam_user_policy_attachment` | `user`! / `user` | `aws_iam_user` | r/iam_user_policy, r/iam_user_policy_attachment | 人間は Identity Center なので出番は薄い |
| `aws_iam_group_policy` / `aws_iam_group_policy_attachment` / `aws_iam_group_membership` | `group`! / `group` / `group`! | `aws_iam_group` | r/iam_group_policy 他 | 同上 |
| `aws_iam_instance_profile` | `role` | `aws_iam_role` | r/iam_instance_profile | **親が Optional**。自前の name を持つので子 resource 寄り。`ec2-service` |

### EC2 / VPC

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_vpc_security_group_ingress_rule` | `security_group_id`! | `aws_security_group` | r/vpc_security_group_ingress_rule | `referenced_security_group_id` は送信元（別 module でよい = `ingress_from`）。親側 inline `ingress`/`egress` と併用不可（WARNING） |
| `aws_vpc_security_group_egress_rule` | `security_group_id`! | `aws_security_group` | r/vpc_security_group_egress_rule | 同上 |
| `aws_security_group_rule` | `security_group_id`! | `aws_security_group` | r/security_group_rule | **旧世代**。docs が「Avoid using」と明記。`source_security_group_id` が相手側。新旧併用不可 |
| `aws_network_acl_rule` | `network_acl_id`! | `aws_network_acl` | r/network_acl_rule | |
| `aws_network_acl_association` | `network_acl_id`! + `subnet_id`! | `aws_network_acl`（守られる側は subnet とも言える） | r/network_acl_association | 双方向。親は NACL とする |
| `aws_route` | `route_table_id`! | `aws_route_table` | r/route | 宛先は `nat_gateway_id` / `vpc_endpoint_id` / `transit_gateway_id` 等 12 種の Optional から 1 つ。親側 inline `route` と併用不可 |
| `aws_route_table_association` | `route_table_id`! + `subnet_id` \| `gateway_id` | `aws_route_table` | r/route_table_association | `subnet_id` と `gateway_id` は排他 |
| `aws_main_route_table_association` | `vpc_id`! + `route_table_id`! | `aws_vpc` | r/main_route_table_association | |
| `aws_vpc_endpoint_policy` | `vpc_endpoint_id`! | `aws_vpc_endpoint` | r/vpc_endpoint_policy | 親側 inline `policy` 現役。`private-aws-access` |
| `aws_vpc_endpoint_route_table_association` / `_subnet_association` / `_security_group_association` | `vpc_endpoint_id`! + `route_table_id`! / `subnet_id`! / `security_group_id`! | `aws_vpc_endpoint` | r/vpc_endpoint_route_table_association 他 | 親側 inline `route_table_ids` / `subnet_ids` / `security_group_ids` と排他（docs NOTE） |
| `aws_vpc_security_group_vpc_association` | `security_group_id`! + `vpc_id`! | `aws_security_group` | r/vpc_security_group_vpc_association | |
| `aws_network_interface_sg_attachment` | `network_interface_id`! + `security_group_id`! | ENI | r/network_interface_sg_attachment | |
| `aws_eip_association` | `allocation_id` + `instance_id` \| `network_interface_id`（全て Optional） | `aws_eip` | r/eip_association | **NAT Gateway / LB には使うな**（inline `allocation_id` を使え）と NOTE。`controlled-external-egress` では出番なし |
| `aws_flow_log` | `vpc_id` / `subnet_id` / `eni_id` / `transit_gateway_id` / `transit_gateway_attachment_id` / `regional_nat_gateway_id`（全て Optional、1 つ） | `aws_vpc` 等 多態 | r/flow_log | **親が多態かつ全て Optional**。Rego は「6 つのうち非 null の 1 つ」を親と見る必要がある |
| `aws_ec2_tag` | `resource_id`! | 任意の EC2 resource | r/ec2_tag | 親型が多態 |

### ECR / Secrets Manager / RDS / ElastiCache / OpenSearch

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_ecr_repository_policy` | `repository`! | `aws_ecr_repository` | r/ecr_repository_policy | repository **名** |
| `aws_ecr_lifecycle_policy` | `repository`! | `aws_ecr_repository` | r/ecr_lifecycle_policy | |
| `aws_secretsmanager_secret_policy` | `secret_arn`! | `aws_secretsmanager_secret` | r/secretsmanager_secret_policy | **ARN のみ**。親側 inline `policy` 現役 |
| `aws_secretsmanager_secret_version` | `secret_id`! | `aws_secretsmanager_secret` | r/secretsmanager_secret_version | **ARN でも名前でも可**。`secret_string_wo`（write-only）あり |
| `aws_secretsmanager_secret_rotation` | `secret_id`! | `aws_secretsmanager_secret` | r/secretsmanager_secret_rotation | ARN でも名前でも可。`rotation_lambda_arn` は Lambda（2.2 のゲート） |
| `aws_db_instance_role_association` | `db_instance_identifier`! | `aws_db_instance` | r/db_instance_role_association | `role_arn`! は IAM |
| `aws_rds_cluster_role_association` | `db_cluster_identifier`! | `aws_rds_cluster` | r/rds_cluster_role_association | |
| `aws_rds_cluster_activity_stream` | `resource_arn`! | `aws_rds_cluster` | r/rds_cluster_activity_stream | 引数名が汎用の `resource_arn` |
| `aws_db_proxy_default_target_group` | `db_proxy_name`! | `aws_db_proxy` | r/db_proxy_default_target_group | |
| `aws_db_proxy_target` | `db_proxy_name`! + `target_group_name`! | `aws_db_proxy` | r/db_proxy_target | `db_cluster_identifier` / `db_instance_identifier` は登録対象 |
| `aws_rds_integration` | `source_arn`! + `target_arn`! | `aws_rds_cluster`（source）/ Redshift（target） | r/rds_integration | zero-ETL。**双方向で親を一意に決めにくい**。`business-analytics` で 1 module に束ねる前提なら問題なし |
| `aws_elasticache_user_group_association` | `user_group_id`! + `user_id`! | `aws_elasticache_user_group` | r/elasticache_user_group_association | |
| `aws_opensearch_domain_policy` | `domain_name`! | `aws_opensearch_domain` | r/opensearch_domain_policy | 親側 inline `access_policies` 現役 |
| `aws_opensearch_domain_saml_options` | `domain_name`! | `aws_opensearch_domain` | r/opensearch_domain_saml_options | |
| `aws_opensearch_authorize_vpc_endpoint_access` | `domain_name`! + `account`! | `aws_opensearch_domain` | r/opensearch_authorize_vpc_endpoint_access | |

### CloudWatch Logs / EventBridge / Scheduler

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_cloudwatch_log_resource_policy` | `policy_name` **または** `resource_arn`（排他、どちらか必須） | なし（`policy_name`）/ `aws_cloudwatch_log_group` 等（`resource_arn`） | r/cloudwatch_log_resource_policy | **アカウント単位と resource 単位の両モードを 1 型が持つ**。`policy_name` モードは親なしとして除外、`resource_arn` モードは attachment |
| `aws_cloudwatch_log_subscription_filter` | `log_group_name`! | `aws_cloudwatch_log_group` | r/cloudwatch_log_subscription_filter | `destination_arn`! は宛先（`analytics` の受け口、別 module でよい） |
| `aws_cloudwatch_log_metric_filter` | `log_group_name`! | `aws_cloudwatch_log_group` | r/cloudwatch_log_metric_filter | |
| `aws_cloudwatch_log_stream` | `log_group_name`! | `aws_cloudwatch_log_group` | r/cloudwatch_log_stream | |
| `aws_cloudwatch_log_data_protection_policy` | `log_group_name`! | `aws_cloudwatch_log_group` | r/cloudwatch_log_data_protection_policy | |
| `aws_cloudwatch_log_index_policy` | `log_group_name`! | `aws_cloudwatch_log_group` | r/cloudwatch_log_index_policy | |
| `aws_cloudwatch_log_delivery_source` | `resource_arn`! | ログを出す側の resource（多態） | r/cloudwatch_log_delivery_source | vended logs。親は **ログ生成 resource**（自前の `name` も持つ） |
| `aws_cloudwatch_log_delivery_destination_policy` | `delivery_destination_name`! | `aws_cloudwatch_log_delivery_destination` | r/cloudwatch_log_delivery_destination_policy | |
| `aws_cloudwatch_log_delivery` | `delivery_source_name`! + `delivery_destination_arn`! | delivery_source | r/cloudwatch_log_delivery | 双方向 |
| `aws_cloudwatch_event_target` | `rule`! | `aws_cloudwatch_event_rule` | r/cloudwatch_event_target | rule **名**。`event_bus_name` は Optional（名前 or ARN、省略で default）。`arn`! は宛先。3.1 は Rule/Target/Role/DLQ を 1 module に束ねると決めているので自然に満たす |
| `aws_cloudwatch_event_bus_policy` | `event_bus_name` | `aws_cloudwatch_event_bus` | r/cloudwatch_event_bus_policy | **親が Optional**（省略で default bus）。`aws_cloudwatch_event_permission` と併用不可 |
| `aws_cloudwatch_event_permission` | `event_bus_name` | `aws_cloudwatch_event_bus` | r/cloudwatch_event_permission | 同上。bus_policy と排他 |
| `aws_cloudwatch_event_archive` | `event_source_arn`! | `aws_cloudwatch_event_bus` | r/cloudwatch_event_archive | 自前の `name` を持つ |

### ECS / ELB / CloudFront / WAF / API Gateway

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_ecs_cluster_capacity_providers` | `cluster_name`! | `aws_ecs_cluster` | r/ecs_cluster_capacity_providers | `aws_ecs_cluster` 側に inline `capacity_providers` は無い（6.65.0 docs に存在しない） |
| `aws_ecs_tag` | `resource_arn`! | 任意の ECS resource | r/ecs_tag | |
| `aws_lb_listener_certificate` | `listener_arn`! + `certificate_arn`! | `aws_lb_listener` | r/lb_listener_certificate | ACM は env 入力（3 の表）なので `certificate_arn` は `var.` |
| `aws_lb_target_group_attachment` | `target_group_arn`! + `target_id`（Required） | `aws_lb_target_group` | r/lb_target_group_attachment | ECS Service は自分で登録するので出番は `ec2-service`（ASG 経由なら不要）・Lambda |
| `aws_cloudfront_monitoring_subscription` | `distribution_id`! | `aws_cloudfront_distribution` | r/cloudfront_monitoring_subscription | CloudFront の OAC / WAF / function は distribution 側の inline 引数（`origin_access_control_id`、`web_acl_id`、`function_association`）で、attachment 型は無い |
| `aws_wafv2_web_acl_association` | `resource_arn`!（守られる側）+ `web_acl_arn`! | `aws_lb` / API GW stage 等 | r/wafv2_web_acl_association | 親は **守られる側**（ALB module が web ACL ARN を入力で受ける）。**CloudFront には使うな**（inline `web_acl_id`） |
| `aws_wafv2_web_acl_logging_configuration` | `resource_arn`! | `aws_wafv2_web_acl` | r/wafv2_web_acl_logging_configuration | ここでの `resource_arn` は **Web ACL 自身** |
| `aws_api_gateway_rest_api_policy` | `rest_api_id`! | `aws_api_gateway_rest_api` | r/api_gateway_rest_api_policy | |
| `aws_api_gateway_method_settings` | `rest_api_id`! + `stage_name`! | `aws_api_gateway_stage` | r/api_gateway_method_settings | 親を **2 引数の組**で指す（stage の ARN/ID ではない） |
| `aws_api_gateway_usage_plan_key` | `usage_plan_id`! + `key_id`! | `aws_api_gateway_usage_plan` | r/api_gateway_usage_plan_key | |
| `aws_api_gateway_base_path_mapping` | `domain_name`!（+ `domain_name_id`）+ `api_id`! + `stage_name` | `aws_api_gateway_domain_name` | r/api_gateway_base_path_mapping | 親は domain name（名前で指す） |
| `aws_apigatewayv2_api_mapping` | `domain_name`! + `api_id`! + `stage`! | `aws_apigatewayv2_domain_name` | r/apigatewayv2_api_mapping | |

### Route53 / ACM / SES

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_route53_zone_association` | `zone_id`! + `vpc_id`! | `aws_route53_zone` | r/route53_zone_association | 親側 inline `vpc` と排他。docs は「順序制御が要る場合以外は非推奨」 |
| `aws_route53_vpc_association_authorization` | `zone_id`! + `vpc_id`! | `aws_route53_zone` | r/route53_vpc_association_authorization | |
| `aws_route53_query_log` | `zone_id`! + `cloudwatch_log_group_arn`! | `aws_route53_zone` | r/route53_query_log | |
| `aws_route53_key_signing_key` | `hosted_zone_id`! + `key_management_service_arn`! | `aws_route53_zone` | r/route53_key_signing_key | **`zone_id` ではなく `hosted_zone_id`**（同じサービス内で名前が揺れる） |
| `aws_route53_hosted_zone_dnssec` | `hosted_zone_id`! | `aws_route53_zone` | r/route53_hosted_zone_dnssec | |
| `aws_acm_certificate_validation` | `certificate_arn`! | `aws_acm_certificate` | r/acm_certificate_validation | ACM を env 入力にするなら validation 自体を tabp で作らない |
| `aws_sesv2_email_identity_policy` | `email_identity`! | `aws_sesv2_email_identity` | r/sesv2_email_identity_policy | |
| `aws_sesv2_email_identity_feedback_attributes` | `email_identity`! | `aws_sesv2_email_identity` | r/sesv2_email_identity_feedback_attributes | bounce/complaint 転送 |
| `aws_sesv2_email_identity_mail_from_attributes` | `email_identity`! | `aws_sesv2_email_identity` | r/sesv2_email_identity_mail_from_attributes | |
| `aws_sesv2_configuration_set_event_destination` | `configuration_set_name`! | `aws_sesv2_configuration_set` | r/sesv2_configuration_set_event_destination | `sns_destination.topic_arn`! 等は宛先 |
| `aws_sesv2_dedicated_ip_assignment` | `destination_pool_name`! + `ip`! | `aws_sesv2_dedicated_ip_pool` | r/sesv2_dedicated_ip_assignment | |
| `aws_ses_identity_policy` | `identity`! | `aws_ses_domain_identity` | r/ses_identity_policy | **名前でも ARN でも可**。v1 API |
| `aws_ses_domain_dkim` | `domain`! | `aws_ses_domain_identity` | r/ses_domain_dkim | v1。v2 では `aws_sesv2_email_identity.dkim_signing_attributes` が inline |
| `aws_ses_domain_mail_from` | `domain`! | `aws_ses_domain_identity` | r/ses_domain_mail_from | v1 |
| `aws_ses_identity_notification_topic` | `identity`! + `notification_type`! | `aws_ses_domain_identity` | r/ses_identity_notification_topic | 名前 or ARN。`topic_arn` は Optional（`""` で無効化） |
| `aws_ses_domain_identity_verification` | `domain`! | `aws_ses_domain_identity` | r/ses_domain_identity_verification | |
| `aws_ses_event_destination` | `configuration_set_name`! | `aws_ses_configuration_set` | r/ses_event_destination | v1 |
| `aws_ses_active_receipt_rule_set` | `rule_set_name`! | `aws_ses_receipt_rule_set` | r/ses_active_receipt_rule_set | 「有効化」なので親は rule set。`email-inbound`。**受信系は v1 にしか無い**（`sesv2_*` に receipt 系ファイルは存在しない） |

### Lambda / Glue / Athena / DynamoDB

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_lambda_permission` | `function_name`! | `aws_lambda_function` | r/lambda_permission | **名前でも ARN でも可**。`qualifier` あり。`source_arn` は呼び出し元（別 module でよい） |
| `aws_lambda_event_source_mapping` | `function_name`! | `aws_lambda_function` | r/lambda_event_source_mapping | 名前 or ARN。`event_source_arn` は Optional（SQS 等） |
| `aws_lambda_function_event_invoke_config` / `_function_url` / `_provisioned_concurrency_config` / `_recursion_config` / `_runtime_management_config` | `function_name`! | `aws_lambda_function` | r/lambda_function_event_invoke_config 他 | recursion_config だけ docs が「Name」のみ。他は名前 or ARN |
| `aws_lambda_layer_version_permission` | `layer_name`! + `version_number`! | `aws_lambda_layer_version` | r/lambda_layer_version_permission | |
| `aws_glue_partition_index` | `database_name`! + `table_name`!（+ `catalog_id`） | `aws_glue_catalog_table` | r/glue_partition_index | 親側 inline `partition_index` も現役 |
| `aws_athena_prepared_statement` | `workgroup`! | `aws_athena_workgroup` | r/athena_prepared_statement | |
| `aws_athena_named_query` | `workgroup` | `aws_athena_workgroup` | r/athena_named_query | **親が Optional**（省略で `primary`） |
| `aws_dynamodb_resource_policy` | `resource_arn`! | `aws_dynamodb_table`（table か stream） | r/dynamodb_resource_policy | |
| `aws_dynamodb_kinesis_streaming_destination` | `table_name`! | `aws_dynamodb_table` | r/dynamodb_kinesis_streaming_destination | `stream_arn`! は宛先。table ごとに 1 つ |
| `aws_dynamodb_contributor_insights` | `table_name`! | `aws_dynamodb_table` | r/dynamodb_contributor_insights | |
| `aws_dynamodb_table_replica` | `global_table_arn`! | `aws_dynamodb_table` | r/dynamodb_table_replica | DR は v1.0 対象外（7.2） |
| `aws_dynamodb_tag` | `resource_arn`! | `aws_dynamodb_table` | r/dynamodb_tag | |

### Cognito / AppConfig / EFS

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_cognito_user_pool_domain` | `user_pool_id`! | `aws_cognito_user_pool` | r/cognito_user_pool_domain | `certificate_arn` は us-east-1 の ACM |
| `aws_cognito_risk_configuration` | `user_pool_id`! | `aws_cognito_user_pool` | r/cognito_risk_configuration | `client_id` Optional で client 単位にもできる |
| `aws_cognito_user_pool_ui_customization` | `user_pool_id`（Required） | `aws_cognito_user_pool` | r/cognito_user_pool_ui_customization | |
| `aws_cognito_log_delivery_configuration` | `user_pool_id`! | `aws_cognito_user_pool` | r/cognito_log_delivery_configuration | |
| `aws_cognito_managed_user_pool_client` | `user_pool_id`! | `aws_cognito_user_pool` | r/cognito_managed_user_pool_client | 既存 client（`name_pattern`/`name_prefix`）を管理下に置く型 |
| `aws_cognito_identity_pool_roles_attachment` | `identity_pool_id`（Required） | `aws_cognito_identity_pool` | r/cognito_identity_pool_roles_attachment | |
| `aws_cognito_identity_pool_provider_principal_tag` | `identity_pool_id`（Required）+ `identity_provider_name` | `aws_cognito_identity_pool` | r/cognito_identity_pool_provider_principal_tag | |
| `aws_appconfig_extension_association` | `extension_arn`! + `resource_arn` | `aws_appconfig_application` 等 | r/appconfig_extension_association | **守られる側 `resource_arn` が Optional** |
| `aws_appconfig_deployment` | `application_id`! + `environment_id`! + `configuration_profile_id`! + `deployment_strategy_id`! | `aws_appconfig_environment` | r/appconfig_deployment | 4 親。`runtime-config` を 1 module に束ねる前提でないと満たせない |
| `aws_efs_file_system_policy` | `file_system_id`! | `aws_efs_file_system` | r/efs_file_system_policy | |
| `aws_efs_backup_policy` | `file_system_id`! | `aws_efs_file_system` | r/efs_backup_policy | |
| `aws_efs_replication_configuration` | `source_file_system_id`! | `aws_efs_file_system` | r/efs_replication_configuration | `destination.file_system_id` は Optional（省略で新規作成） |

### GuardDuty / Backup / Config / Access Analyzer / Auto Scaling / Network Firewall / Budgets

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_guardduty_malware_protection_plan` | `protected_resource.s3_bucket.bucket_name`!（**nested block**） | `aws_s3_bucket` | r/guardduty_malware_protection_plan | `media-ingest`。Rego は nested 属性を辿る必要がある。`role`! は IAM |
| `aws_guardduty_detector_feature` / `_publishing_destination` / `_filter` | `detector_id`! | `aws_guardduty_detector` | r/guardduty_detector_feature 他 | publishing_destination の `destination_arn`! は S3。feature は削除しても無効化されない |
| `aws_backup_vault_policy` / `_vault_lock_configuration` / `_vault_notifications` | `backup_vault_name`! | `aws_backup_vault` | r/backup_vault_policy 他 | `backup-restore` |
| `aws_backup_selection` | `plan_id`! | `aws_backup_plan` | r/backup_selection | `resources` / `selection_tag` は保護対象（別 module の ARN でよい） |
| `aws_config_configuration_recorder_status` | `name`! | `aws_config_configuration_recorder` | r/config_configuration_recorder_status | 引数名が素の **`name`** |
| `aws_accessanalyzer_archive_rule` | `analyzer_name`! | `aws_accessanalyzer_analyzer` | r/accessanalyzer_archive_rule | |
| `aws_autoscaling_attachment` | `autoscaling_group_name`! + `elb` \| `lb_target_group_arn` | `aws_autoscaling_group` | r/autoscaling_attachment | 親側 inline `load_balancers` / `target_group_arns` と排他（NOTE） |
| `aws_autoscaling_traffic_source_attachment` | `autoscaling_group_name`! + `traffic_source{identifier,type}`! | `aws_autoscaling_group` | r/autoscaling_traffic_source_attachment | 上の後継 |
| `aws_autoscaling_policy` / `_schedule` / `_lifecycle_hook` / `_group_tag` | `autoscaling_group_name`! | `aws_autoscaling_group` | r/autoscaling_policy 他 | |
| `aws_autoscaling_notification` | `group_names`!（**リスト**） | `aws_autoscaling_group` | r/autoscaling_notification | 親が複数 |
| `aws_networkfirewall_logging_configuration` | `firewall_arn`! | `aws_networkfirewall_firewall` | r/networkfirewall_logging_configuration | |
| `aws_networkfirewall_resource_policy` | `resource_arn`! | rule group / firewall policy | r/networkfirewall_resource_policy | |
| `aws_budgets_budget_action` | `budget_name`! | `aws_budgets_budget` | r/budgets_budget_action | `account_id` Optional |
| `aws_ce_anomaly_subscription` | `monitor_arn_list`!（リスト） | `aws_ce_anomaly_monitor` | r/ce_anomaly_subscription | 親が複数 |

### Organizations / Identity Center / EKS / Amplify / Lightsail / Redshift / End User Messaging

| resource 型 | 親を指す引数 | 親の resource 型 | 確認した URL | 備考 |
|---|---|---|---|---|
| `aws_organizations_policy_attachment` | `target_id`!（root/OU/account）+ `policy_id`! | OU / account | r/organizations_policy_attachment | 双方向。SCP は「守られる側 = target」とする |
| `aws_organizations_delegated_administrator` | `account_id`! + `service_principal`! | `aws_organizations_account` | r/organizations_delegated_administrator | |
| `aws_ssoadmin_managed_policy_attachment` / `_permission_set_inline_policy` / `_customer_managed_policy_attachment` / `_permissions_boundary_attachment` | `permission_set_arn`!（+ `instance_arn`! は常に必須） | `aws_ssoadmin_permission_set` | r/ssoadmin_managed_policy_attachment 他 | `instance_arn` は組織の識別子（data/var 由来）。親判定は `permission_set_arn` だけ見る |
| `aws_ssoadmin_account_assignment` | `permission_set_arn`! + `principal_id`! + `target_id`!（+ `instance_arn`!） | permission set | r/ssoadmin_account_assignment | 3 者 |
| `aws_ssoadmin_instance_access_control_attributes` | `instance_arn`! | Identity Center instance（tabp 外） | r/ssoadmin_instance_access_control_attributes | 親は Terraform 外 |
| `aws_ssoadmin_application_assignment` / `_application_access_scope` | `application_arn`! | `aws_ssoadmin_application` | r/ssoadmin_application_assignment 他 | |
| `aws_eks_access_entry` | `cluster_name`! + `principal_arn`! | `aws_eks_cluster` | r/eks_access_entry | |
| `aws_eks_access_policy_association` | `cluster_name`! + `principal_arn`! + `policy_arn`! | `aws_eks_access_entry` | r/eks_access_policy_association | 親を 2 引数の組で指す |
| `aws_eks_pod_identity_association` | `cluster_name`! + `namespace`! + `service_account`! | `aws_eks_cluster` | r/eks_pod_identity_association | `role_arn`! は IAM |
| `aws_eks_identity_provider_config` | `cluster_name`! | `aws_eks_cluster` | r/eks_identity_provider_config | |
| `aws_eks_addon` | `cluster_name`! | `aws_eks_cluster` | r/eks_addon | 自前の `addon_name` を持つ。子扱いでもよい |
| `aws_amplify_domain_association` | `app_id`! | `aws_amplify_app` | r/amplify_domain_association | `sub_domain.branch_name`! は branch |
| `aws_lightsail_instance_public_ports` | `instance_name`! | `aws_lightsail_instance` | r/lightsail_instance_public_ports | |
| `aws_lightsail_static_ip_attachment` / `_disk_attachment` | `instance_name`! + `static_ip_name`! / `disk_name`! | `aws_lightsail_instance` | r/lightsail_static_ip_attachment 他 | |
| `aws_lightsail_lb_attachment` / `_lb_certificate_attachment` / `_lb_https_redirection_policy` / `_lb_stickiness_policy` | `lb_name`!（+ `instance_name`! / `certificate_name`!） | `aws_lightsail_lb` | r/lightsail_lb_attachment 他 | |
| `aws_lightsail_bucket_access_key` / `_bucket_resource_access` | `bucket_name`!（+ `resource_name`!） | `aws_lightsail_bucket` | r/lightsail_bucket_access_key 他 | |
| `aws_lightsail_container_service_deployment_version` | `service_name`! | `aws_lightsail_container_service` | r/lightsail_container_service_deployment_version | |
| `aws_redshiftserverless_resource_policy` | `resource_arn`! | Redshift Serverless（docs は「ARN of the account」と書く） | r/redshiftserverless_resource_policy | docs の記述が曖昧 |
| `aws_pinpointsmsvoicev2_event_destination` | `configuration_set_name`! | `aws_pinpointsmsvoicev2_configuration_set` | r/pinpointsmsvoicev2_event_destination | `sms-push-delivery`。`aws_pinpointsmsvoicev2_configuration_set_event_destination` という名の resource は**存在しない** |
| `aws_pinpointsmsvoicev2_resource_policy` | `resource_arn`! | phone number / pool / opt-out list / sender ID | r/pinpointsmsvoicev2_resource_policy | 親型が多態 |
| `aws_pinpointsmsvoicev2_keyword` | `origination_identity_arn`! | phone number / pool | r/pinpointsmsvoicev2_keyword | |

### 子 resource として除外したもの（親引数は読んだが検査対象にしない）

`aws_lb_listener`(`load_balancer_arn`!)、`aws_lb_listener_rule`(`listener_arn`!)、`aws_api_gateway_resource`/`_method`/`_integration`/`_deployment`/`_stage`/`_authorizer`/`_gateway_response`(`rest_api_id`! [+ `resource_id`! / `parent_id`!])、`aws_apigatewayv2_integration`/`_route`/`_stage`/`_authorizer`/`_deployment`(`api_id`!)、`aws_route53_record`(`zone_id`!、3.1 の表で入力化される)、`aws_cloudwatch_event_rule`(`event_bus_name` Optional)、`aws_scheduler_schedule`(`group_name` Optional)、`aws_ecs_service`(`cluster` **Optional**、ARN)、`aws_rds_cluster_instance`(`cluster_identifier`!)、`aws_opensearch_vpc_endpoint`(`domain_arn`!)、`aws_cognito_user_pool_client`/`_identity_provider`/`_resource_server`/`_user_group`/`_user`(`user_pool_id`)、`aws_cognito_user_in_group`(`user_pool_id`!+`group_name`!)、`aws_appconfig_configuration_profile`/`_environment`(`application_id`!)、`aws_appconfig_hosted_configuration_version`(`application_id`!+`configuration_profile_id`!)、`aws_efs_mount_target`/`_access_point`(`file_system_id`!)、`aws_glue_catalog_table`/`_partition`(`database_name`!, `catalog_id` Optional)、`aws_lambda_alias`(`function_name`!)、`aws_eks_node_group`/`_fargate_profile`(`cluster_name`!)、`aws_amplify_branch`/`_webhook`/`_backend_environment`(`app_id`!)、`aws_lightsail_domain_entry`(`domain_name`!)、`aws_lightsail_lb_certificate`(`lb_name`!)、`aws_redshiftserverless_workgroup`/`_endpoint_access`(`namespace_name`! / `workgroup_name`!)、`aws_service_discovery_service`(`namespace_id` Optional / `dns_config.namespace_id`!)、`aws_organizations_account`/`_organizational_unit`(`parent_id` Optional)、`aws_dynamodb_table_item`(データ)、`aws_dynamodb_table_export`/`aws_db_instance_automated_backups_replication`(一回性のタスク・DR)。

これらを検査に含めるなら親引数は上記のとおりで、Rego の表に足すだけで済む。

### 親を持たないもの（アカウント / リージョン単位、検査対象外）

`aws_s3_account_public_access_block`(`account_id` Optional)、`aws_ecr_registry_policy`、`aws_ecr_registry_scanning_configuration`、`aws_cloudwatch_log_account_policy`、`aws_cloudwatch_log_resource_policy`（`policy_name` モード）、`aws_ebs_encryption_by_default`、`aws_ecs_account_setting_default`、`aws_api_gateway_account`、`aws_glue_resource_policy`、`aws_glue_data_catalog_encryption_settings`(`catalog_id` Optional)、`aws_sesv2_account_vdm_attributes`、`aws_sesv2_account_suppression_attributes`、`aws_organizations_resource_policy`、`aws_config_delivery_channel`（recorder を引数で指さない）、`aws_iam_openid_connect_provider`。

## 3. この検査が漏らすもの

「親引数に `module.` が含まれない」は**否定形の述語**であり、親が「同一 module の resource」であることを何も保証しない。抜け道は次のとおり。

1. **`var.` 経由**。`_shared/` や兄弟 `internal/` に attachment だけの module を作り、親 ID を variable で受ける。
2. **`data.` 経由**。`data.aws_sqs_queue.x.url` / `data.aws_s3_bucket.x.id` / `data.terraform_remote_state` / `data.aws_ssm_parameter`。要件 3 が usecase 間の受け渡しに remote state / SSM を認めているので、**usecase 間の attachment はこの形で必ず現れる**。
3. **`local.` の間接参照**。`locals { q = module.queue.url }` → `queue_url = local.q`。plan JSON の `configuration.*.expressions.<arg>.references` は `local.q` までしか展開しない。
4. **文字列組み立て**。`format("https://sqs.%s.amazonaws.com/%s/%s", ...)`、`"arn:aws:..."` リテラル、`each.value` / `each.key`（`for_each` の map を module 出力から組む）。
5. **nested block の親**。`aws_guardduty_malware_protection_plan.protected_resource.s3_bucket.bucket_name`、`aws_api_gateway_method_settings` の `rest_api_id` + `stage_name` の組、`aws_flow_log` の 6 択 Optional。トップレベル引数だけ見る Rego は素通しする。
6. **親側 inline 引数との二重管理**。`aws_sqs_queue.policy` と `aws_sqs_queue_policy` を別 module に置く（Provider は permanent diff を出すが CI の検査 3 には掛からない）。`aws_iam_role.managed_policy_arns`（Deprecated だが存在）は排他管理なので別 module の attachment を毎 apply 剥がす。
7. **検査 1 の死角**。attachment が usecase 直下でなく `_shared/` に居れば検査 1 は通り、検査 3 も `var.` 経由なら通る。

### 塞ぐ案（肯定形にする）

- **案 A: 親引数は「同一 module 内の managed resource への直接参照」でなければならない。** `terraform show -json` の `configuration` を走査し、attachment 型の親引数の `references` が `aws_<親型>.<name>`（`.id/.arn/.name/.url` 付き）**ちょうど 1 つ**で、かつその address が **同じ `module_calls` ノードの `resources` に存在**することを要求する。`var.` / `data.` / `local.` / `module.` / リテラル / `each.*` はすべて違反。親引数の表（本書 §2）を Rego のデータとして持ち、nested path は `protected_resource.s3_bucket.bucket_name` のように dotted path で書く。
  - 費用: (i) 表のメンテナンス（Provider の minor で resource が増える）。(ii) 親を持たない型（§2 末尾）と、親が Terraform 外にある型（`aws_ssoadmin_*` の `instance_arn`、`aws_cloudwatch_event_bus_policy` の default bus）を allow-list に載せる必要がある。(iii) **`data.` 経由の usecase 間 attachment が全面禁止になる**。これは要件 3.1 の「resource-level policy は親を所有する module が 1 文書だけ生成する」と整合する（policy は親の module が生成し、grantee ARN を入力で受ける）ので、要件と矛盾しない。ただし `aws_sns_topic_subscription` のように「親 = topic、相手 = 別 usecase の SQS」で subscription をどちらの usecase に置くかは要件側で決める必要がある（案 A は topic 側を強制する）。
  - `local.` を禁じるのは重い制約に見えるが、親引数 1 個の話なので実装上の不便は小さい。
- **案 B: plan の依存グラフで判定する。** `configuration` の `references` を再帰展開して（`local.x` → その `expression.references` → …）最終的な resource address に到達させ、attachment と親の module address prefix が一致することを見る。`var.` に到達したら「親が外部」として違反。
  - 費用: locals の再帰展開と `for_each`/`each.value` の解決を Rego で書くのは重く、`each.value` は静的に解けない場合がある（違反として倒すしかない）。案 A に比べて許容範囲は広がらないので、案 A で `local.` を禁じる方が安い。
- **補助: 二重管理の検出**（抜け道 6）。親 resource の inline 引数（`aws_sqs_queue.policy`、`aws_sns_topic.policy`、`aws_kms_key.policy`、`aws_secretsmanager_secret.policy`、`aws_opensearch_domain.access_policies`、`aws_vpc_endpoint.policy`、`aws_security_group.ingress/egress`、`aws_route_table.route`、`aws_route53_zone.vpc`、`aws_iam_role.inline_policy/managed_policy_arns`、`aws_s3_bucket` の Deprecated 群）と対応する attachment 型が**同一 plan 内に共存したら違反**にする。これは module 境界とは独立の検査で、同じ表に「親側 inline 引数」列を 1 つ足せば書ける。

## 4. Provider version への依存

**確認した version: hashicorp/aws 6.65.0（2026-09-16 公開、2026-09-20 時点の latest）。** 表の内容は次の行で version により変わる。

- **Security Group ルール**: `aws_vpc_security_group_ingress_rule` / `_egress_rule` が現行推奨、`aws_security_group_rule` は docs が「Avoid using」と明記する旧世代。両方 6.65.0 に存在し、併用不可。表は両方載せたが、検査は旧世代を**禁止**にしてよい。
- **S3**: `aws_s3_bucket` の inline `policy` / `versioning` / `acl` / `grant` / `cors_rule` / `lifecycle_rule` / `logging` / `website` は 6.65.0 docs で **Deprecated**。将来の major で消えれば standalone 型だけが経路になる（表はその前提で書いてある）。
- **IAM**: `aws_iam_role.inline_policy` / `managed_policy_arns` は Deprecated。後継の `aws_iam_role_policies_exclusive` / `aws_iam_role_policy_attachments_exclusive` は v5 系後半で追加された型なので、古い version を pin すると存在しない。
- **CloudWatch Logs**: `aws_cloudwatch_log_resource_policy` の `resource_arn`（resource 単位モード）は新しい引数。古い version では `policy_name` のみでアカウント単位固定。
- **EventBridge**: `aws_cloudwatch_event_permission` と `aws_cloudwatch_event_bus_policy` は排他。前者が旧型。
- **SES**: v1（`aws_ses_*`）と v2（`aws_sesv2_*`）で親引数名が違う（`identity`/`domain` vs `email_identity`）。受信（receipt rule set）は v1 にしか無いので `email-inbound` は v1 を避けられない。
- **Route53**: `aws_route53_zone_association` は inline `vpc` と排他で、docs が非推奨寄り。
- **Auto Scaling**: `aws_autoscaling_attachment` → `aws_autoscaling_traffic_source_attachment` へ後継が出ている。
- **ECS**: `aws_ecs_cluster` に inline `capacity_providers` は 6.65.0 に無く、`aws_ecs_cluster_capacity_providers` のみ。
- **全 regional resource に `region` 引数**（v6.0 で追加）。親引数ではないが、引数一覧を機械で読む Rego は無視リストに入れる必要がある。
- **API Gateway REST + VPC Link V2**: `aws_api_gateway_integration.integration_target`（ALB/NLB ARN）は 6.x で追加。`public-api-gateway` の経路に直結するが attachment 型ではない。
- **End User Messaging**: `aws_pinpointsmsvoicev2_*` は名前が旧 Pinpoint 由来のまま。resource 名は改名されうる。

## 隣接して気づいたこと

要件 3.1 の検査 3 は「usecase 層」の規則として書かれているが、検査 1 で usecase 直下に resource が無い以上、検査 3 が実際に走る対象は `internal/` と `_shared/` の module である。要件の文言をそのように直すか、検査 3 の適用範囲を明記した方がよい。

作業ファイル: `/tmp/tabp/awsdocs/repo/website/docs/r/`（v6.65.0 の docs 全 1718 ファイル）、抽出スクリプト `/tmp/tabp/awsdocs/args.sh`。
