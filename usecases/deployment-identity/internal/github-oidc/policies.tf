# Action の一覧の根拠（Provider の各 resource が呼ぶ API）は usecases/deployment-identity/docs/adr/0201 が持つ。
locals {
  identity_state_key = "${var.state_bucket_arn}/identity/terraform.tfstate"
  workload_state_key = "${var.state_bucket_arn}/workload/terraform.tfstate"
  # pr/ は Pull Request の plan、branch/ は保護ブランチの plan（deployment-identity/ADR-0201 決定8）。
  plan_output_keys = ["${var.plan_output_bucket_arn}/pr/*", "${var.plan_output_bucket_arn}/branch/*"]

  state_key_read_actions = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
  # deployment-identity/ADR-0201 決定5。
  state_key_administration_actions = [
    "kms:CancelKeyDeletion",
    "kms:CreateAlias",
    "kms:DeleteAlias",
    "kms:DescribeKey",
    "kms:DisableKey",
    "kms:DisableKeyRotation",
    "kms:EnableKey",
    "kms:EnableKeyRotation",
    "kms:GetKeyPolicy",
    "kms:GetKeyRotationStatus",
    "kms:ListGrants",
    "kms:ListKeyPolicies",
    "kms:ListResourceTags",
    "kms:PutKeyPolicy",
    "kms:RevokeGrant",
    "kms:RotateKeyOnDemand",
    "kms:ScheduleKeyDeletion",
    "kms:TagResource",
    "kms:UntagResource",
    "kms:UpdateAlias",
    "kms:UpdateKeyDescription",
  ]
  state_key_arn_prefix = "arn:${data.aws_partition.current.partition}:kms:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:key/"

  identity_role_arns = [for k in keys(local.roles) : local.role_arns[k]]

  iam_arn_prefix  = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}"
  ci_arn_patterns = ["${local.iam_arn_prefix}:role/${var.name_prefix}-ci-*", "${local.iam_arn_prefix}:policy/${var.name_prefix}-ci-*"]
  # break-glass の Deny は path の pattern で書く（deployment-identity/ADR-0201 決定5）。入力はこの prefix の下に限る。
  break_glass_arn_prefixes = [for t in ["group", "role", "user"] : "${local.iam_arn_prefix}:${t}/break-glass/"]

  workload_policy_name = "${var.name_prefix}-ci-apply-workload"
  workload_policy_arn  = "${local.iam_arn_prefix}:policy/${local.workload_policy_name}"
  # 読み取りは2つ目の policy に分ける（決定17）。
  workload_read_policy_name = "${var.name_prefix}-ci-apply-workload-read"
  workload_read_policy_arn  = "${local.iam_arn_prefix}:policy/${local.workload_read_policy_name}"
  # account-baseline の管理は3つ目の policy に置く（決定17・21）。
  workload_2_policy_name = "${var.name_prefix}-ci-apply-workload-2"
  workload_2_policy_arn  = "${local.iam_arn_prefix}:policy/${local.workload_2_policy_name}"
  workload_policy_arns   = [local.workload_policy_arn, local.workload_read_policy_arn, local.workload_2_policy_arn]
  boundary_policy_names  = { "ops-notification" = "${var.name_prefix}-ci-boundary-ops-notification" }
  # 決定13。IAM の role と policy を書き込む Allow は、usecase が IAM の名前の先頭に置く小文字の接頭辞に限る。
  usecase_iam_prefixes = { "ops-notification" = "${var.name_prefix}-ops-" }
  boundary_policy_arns = { for k, n in local.boundary_policy_names : k => "${local.iam_arn_prefix}:policy/${n}" }

  bucket_read_actions = [
    "s3:GetAccelerateConfiguration",
    "s3:GetBucketAcl",
    "s3:GetBucketCORS",
    "s3:GetBucketLogging",
    "s3:GetBucketObjectLockConfiguration",
    "s3:GetBucketOwnershipControls",
    "s3:GetBucketPolicy",
    "s3:GetBucketPublicAccessBlock",
    "s3:GetBucketRequestPayment",
    "s3:GetBucketTagging",
    "s3:GetBucketVersioning",
    "s3:GetBucketWebsite",
    "s3:GetEncryptionConfiguration",
    "s3:GetLifecycleConfiguration",
    "s3:GetReplicationConfiguration",
  ]

  bucket_configure_actions = [
    "s3:DeleteBucketPolicy",
    "s3:PutBucketOwnershipControls",
    "s3:PutBucketPolicy",
    "s3:PutBucketPublicAccessBlock",
    "s3:PutBucketVersioning",
    "s3:PutEncryptionConfiguration",
  ]

  # 3つの Bucket に同じ Action を与える文は、Resource を3つにした1つの文にする（inline policy の長さ）。
  bucket_arns = [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn]

  bucket_read_statements = [
    { Sid = "ListBuckets", Effect = "Allow", Action = "s3:ListBucket", Resource = local.bucket_arns },
    { Sid = "ReadBucketConfiguration", Effect = "Allow", Action = local.bucket_read_actions, Resource = local.bucket_arns },
  ]

  identity_read_statements = [
    {
      Sid    = "ReadGitHubProvider"
      Effect = "Allow"
      Action = [
        "iam:GetOpenIDConnectProvider",
        "iam:ListOpenIDConnectProviderTags",
      ]
      Resource = aws_iam_openid_connect_provider.github.arn
    },
    {
      Sid    = "ReadCiRoles"
      Effect = "Allow"
      Action = [
        "iam:GetRole",
        "iam:GetRolePolicy",
        "iam:ListAttachedRolePolicies",
        "iam:ListRolePolicies",
        "iam:ListRoleTags",
      ]
      Resource = local.identity_role_arns
    },
    { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
    {
      Sid      = "ReadCiPolicies"
      Effect   = "Allow"
      Action   = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
      Resource = "${local.iam_arn_prefix}:policy/${var.name_prefix}-ci-*"
    },
  ]

  # 統制の構成物（organization-baseline）は管理アカウントの identity root だけが持つ（deployment-identity/ADR-0201 決定3）。
  management = var.env == "admin"

  # 組織の ID は作る前に決まらないので、Organizations の ARN は pattern で書く（deployment-identity/ADR-0201 決定4）。
  org_arn_prefix = "arn:${data.aws_partition.current.partition}:organizations::${data.aws_caller_identity.current.account_id}"
  org_root_arns  = ["${local.org_arn_prefix}:root/o-*/r-*"]
  org_ou_arns    = ["${local.org_arn_prefix}:ou/o-*/ou-*"]
  org_policy_arn = "${local.org_arn_prefix}:policy/o-*/*/p-*"
  # メンバーアカウントのメールは、organization-baseline が管理アカウントの parameter から読む（organization-baseline/README.md）。
  member_account_emails_parameter_arn = "arn:${data.aws_partition.current.partition}:ssm:${local.regional_arn_suffix}:parameter/${trimsuffix(var.name_prefix, "-${var.env}")}/organization-baseline/member-account-emails"

  break_glass_user_arns  = [for a in var.break_glass_arns : a if strcontains(a, ":user/")]
  break_glass_group_arns = [for a in var.break_glass_arns : a if strcontains(a, ":group/")]
  break_glass_role_arns  = [for a in var.break_glass_arns : a if strcontains(a, ":role/")]
  administrator_access   = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AdministratorAccess"

  # StackSet の名前は break-glass の role と同じで、ARN の末尾の ID は作るまで決まらないので `:*` で書く（deployment-identity/ADR-0201 決定3・4）。
  cfn_arn_prefix              = "arn:${data.aws_partition.current.partition}:cloudformation:${local.regional_arn_suffix}"
  break_glass_stack_set_names = [for a in local.break_glass_role_arns : regex("[^/]+$", a)]
  break_glass_stack_set_arns  = [for n in local.break_glass_stack_set_names : "${local.cfn_arn_prefix}:stackset/${n}:*"]
  # UpdateStackSet と stack instance の操作は、stackset-target と type の resource 型も要る（Service Authorization Reference）。type の ARN はアカウントを `*` にする（deployment-identity/ADR-0201 決定4）。
  # stackset-target は region を空、type は region を `*` にする（同 決定4）。
  break_glass_stack_instance_arns = concat(
    local.break_glass_stack_set_arns,
    [for n in local.break_glass_stack_set_names : "arn:${data.aws_partition.current.partition}:cloudformation::${data.aws_caller_identity.current.account_id}:stackset-target/${n}:*"],
  )
  break_glass_stack_set_type_arn     = "arn:${data.aws_partition.current.partition}:cloudformation:*:*:type/resource/AWS-IAM-Role"
  break_glass_stack_set_write_arns   = concat(local.break_glass_stack_instance_arns, [local.break_glass_stack_set_type_arn])
  break_glass_stack_instance_actions = ["cloudformation:CreateStackInstances", "cloudformation:UpdateStackInstances"]

  governance_policy_name     = "${var.name_prefix}-ci-apply-identity-governance"
  governance_policy_arn      = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:policy/${local.governance_policy_name}"
  governance_policy_arns     = [for a in [local.governance_policy_arn] : a if local.management]
  manage_policy_name         = "${var.name_prefix}-ci-apply-identity-manage"
  manage_policy_arn          = "${local.iam_arn_prefix}:policy/${local.manage_policy_name}"
  apply_identity_policy_arns = concat([local.manage_policy_arn], local.governance_policy_arns)
  managed_policy_arns        = { apply_identity = local.apply_identity_policy_arns, apply = local.workload_policy_arns, plan = [local.workload_read_policy_arn] }
  ci_policy_arns             = concat(local.apply_identity_policy_arns, local.workload_policy_arns, values(local.boundary_policy_arns))

  regional_arn_suffix = "${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}"
  # workload root の usecase の構成物は、名前を `<name_prefix>-` で始める pattern で限る（決定13）。
  # 置かれる region は provider の region と us-east-1（repository-wide/ADR-0208 決定4・5）。
  workload_regions      = distinct(concat([data.aws_region.current.region], data.aws_partition.current.partition == "aws" ? ["us-east-1"] : []))
  workload_arn_suffixes = [for r in local.workload_regions : "${r}:${data.aws_caller_identity.current.account_id}"]
  workload_key_arns     = [for x in local.workload_arn_suffixes : "arn:${data.aws_partition.current.partition}:kms:${x}:key/*"]
  workload_topic_arns   = [for x in local.workload_arn_suffixes : "arn:${data.aws_partition.current.partition}:sns:${x}:${var.name_prefix}-*"]
  workload_schedule_arns = flatten([for x in local.workload_arn_suffixes : [
    "arn:${data.aws_partition.current.partition}:scheduler:${x}:schedule-group/${var.name_prefix}-*",
    "arn:${data.aws_partition.current.partition}:scheduler:${x}:schedule/${var.name_prefix}-*/*",
  ]])
  workload_key_conditions = {
    StringEquals = { "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT", "kms:KeyOrigin" = "AWS_KMS" }
    Bool         = { "kms:MultiRegion" = "false" }
  }
  # monitor と subscription の ARN は名前を含まないので、名前で限れない（決定13）。
  workload_budget_arn    = "arn:${data.aws_partition.current.partition}:budgets::${data.aws_caller_identity.current.account_id}:budget/${var.name_prefix}-*"
  anomaly_detection_arns = [for t in ["anomalymonitor", "anomalysubscription"] : "arn:${data.aws_partition.current.partition}:ce::${data.aws_caller_identity.current.account_id}:${t}/*"]
  # project は英小文字と数字だけなので（usecases/deployment-identity の variables.tf）、name_prefix の最初の - の前が project である。
  bootstrap_tag_key = "${split("-", var.name_prefix)[0]}:bootstrap"
  # アカウントの前提の構成物（deployment-identity/ADR-0201 決定3）。aws 以外の partition にはこの role が無いので、文を置かない。
  chatbot_slr_arn = "${local.iam_arn_prefix}:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
  chatbot_slr_read_statements = [for s in [
    { Sid = "ReadChatbotServiceLinkedRole", Effect = "Allow", Action = "iam:GetRole", Resource = local.chatbot_slr_arn },
  ] : s if data.aws_partition.current.partition == "aws"]
  # account-baseline の構成物（決定13）。リージョンごとの構成物は repository-wide/ADR-0209 の集合のすべてに在り、ARN の region を
  # `*` にして、DenyBaselineOutsideRegions で集合に限る（ADR-0209 決定4、repository-wide/ADR-0301 決定11 の但し書き）。
  # 集合を持たない partition では account-baseline を plan できないので、リージョンごとの文と Deny を置かない（account-baseline/ADR-0101 決定2）。
  baseline_regions   = sort(tolist(lookup(module.default_regions.by_partition, data.aws_partition.current.partition, toset([]))))
  baseline_supported = length(local.baseline_regions) > 0
  account_arn        = "arn:${data.aws_partition.current.partition}:account::${data.aws_caller_identity.current.account_id}:account"
  config_slr_arn     = "${local.iam_arn_prefix}:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig"
  # CreateAnalyzer は、Access Analyzer の SLR が無ければ呼び手の権限で作る（Service Authorization Reference の依存の Action）。
  analyzer_slr_arn         = "${local.iam_arn_prefix}:role/aws-service-role/access-analyzer.amazonaws.com/AWSServiceRoleForAccessAnalyzer"
  baseline_arn_suffix      = "*:${data.aws_caller_identity.current.account_id}"
  config_recorder_arn      = "arn:${data.aws_partition.current.partition}:config:${local.baseline_arn_suffix}:configuration-recorder/${var.name_prefix}/*"
  analyzer_arn             = "arn:${data.aws_partition.current.partition}:access-analyzer:${local.baseline_arn_suffix}:analyzer/${var.name_prefix}-external"
  ssm_service_setting_arns = [for id in ["documents/console/public-sharing-permission", "automation/customer-script-log-destination"] : "arn:${data.aws_partition.current.partition}:ssm:${local.baseline_arn_suffix}:servicesetting/ssm/${id}"]
  ec2_region_condition     = { StringEquals = { "ec2:Region" = local.baseline_regions } }
  # アカウントに1つの設定と global のサービス（account・IAM・S3 の Public Access Block）は、リージョンで限らない。
  baseline_account_read_statements = [
    { Sid = "ReadAlternateContact", Effect = "Allow", Action = "account:GetAlternateContact", Resource = local.account_arn },
    { Sid = "ReadConfigServiceLinkedRole", Effect = "Allow", Action = "iam:GetRole", Resource = local.config_slr_arn },
    { Sid = "ReadAccountPublicAccessBlock", Effect = "Allow", Action = "s3:GetAccountPublicAccessBlock", Resource = "*" },
    { Sid = "ReadPasswordPolicy", Effect = "Allow", Action = "iam:GetAccountPasswordPolicy", Resource = "*" },
  ]
  baseline_regional_read_statements = [
    { Sid = "ReadConfigRecorders", Effect = "Allow", Action = ["config:DescribeConfigurationRecorderStatus", "config:DescribeConfigurationRecorders"], Resource = local.config_recorder_arn },
    { Sid = "ReadAnalyzers", Effect = "Allow", Action = "access-analyzer:GetAnalyzer", Resource = local.analyzer_arn },
    { Sid = "ReadSsmServiceSettings", Effect = "Allow", Action = "ssm:GetServiceSetting", Resource = local.ssm_service_setting_arns },
    { Sid = "ReadDeliveryChannels", Effect = "Allow", Action = "config:DescribeDeliveryChannels", Resource = "*" },
    { Sid = "ReadRetentionConfigurations", Effect = "Allow", Action = "config:DescribeRetentionConfigurations", Resource = "*" },
    { Sid = "ReadEbsEncryptionByDefault", Effect = "Allow", Action = "ec2:GetEbsEncryptionByDefault", Resource = "*", Condition = local.ec2_region_condition },
    { Sid = "DescribeRegions", Effect = "Allow", Action = "ec2:DescribeRegions", Resource = "*", Condition = local.ec2_region_condition },
    { Sid = "ReadVpcBlockPublicAccess", Effect = "Allow", Action = "ec2:DescribeVpcBlockPublicAccessOptions", Resource = "*", Condition = local.ec2_region_condition },
    { Sid = "ReadEmrBlockPublicAccess", Effect = "Allow", Action = "elasticmapreduce:GetBlockPublicAccessConfiguration", Resource = "*" },
  ]
  baseline_read_statements = concat(local.baseline_account_read_statements, [for s in local.baseline_regional_read_statements : s if local.baseline_supported])
  baseline_account_manage_statements = concat([
    { Sid = "PutAlternateContact", Effect = "Allow", Action = "account:PutAlternateContact", Resource = local.account_arn, Condition = { StringEquals = { "account:AlternateContactTypes" = "SECURITY" } } },
    { Sid = "CreateConfigServiceLinkedRole", Effect = "Allow", Action = "iam:CreateServiceLinkedRole", Resource = local.config_slr_arn, Condition = { StringEquals = { "iam:AWSServiceName" = "config.amazonaws.com" } } },
    { Sid = "CreateAnalyzerServiceLinkedRole", Effect = "Allow", Action = "iam:CreateServiceLinkedRole", Resource = local.analyzer_slr_arn, Condition = { StringEquals = { "iam:AWSServiceName" = "access-analyzer.amazonaws.com" } } },
    # PutConfigurationRecorder は、recorder の role を渡す PassRole を要する（Service Authorization Reference の依存の Action）。
    { Sid = "PassConfigServiceLinkedRole", Effect = "Allow", Action = "iam:PassRole", Resource = local.config_slr_arn, Condition = { StringEquals = { "iam:PassedToService" = "config.amazonaws.com" } } },
    { Sid = "PutAccountPublicAccessBlock", Effect = "Allow", Action = "s3:PutAccountPublicAccessBlock", Resource = "*" },
    ], local.management ? [] : [
    { Sid = "UpdatePasswordPolicy", Effect = "Allow", Action = "iam:UpdateAccountPasswordPolicy", Resource = "*" },
  ])
  baseline_regional_manage_statements = [
    { Sid = "ManageConfigRecorders", Effect = "Allow", Action = ["config:PutConfigurationRecorder", "config:StartConfigurationRecorder"], Resource = local.config_recorder_arn },
    { Sid = "CreateAnalyzers", Effect = "Allow", Action = "access-analyzer:CreateAnalyzer", Resource = local.analyzer_arn },
    { Sid = "UpdateSsmServiceSettings", Effect = "Allow", Action = "ssm:UpdateServiceSetting", Resource = local.ssm_service_setting_arns },
    { Sid = "PutDeliveryChannels", Effect = "Allow", Action = "config:PutDeliveryChannel", Resource = "*" },
    { Sid = "PutRetentionConfigurations", Effect = "Allow", Action = "config:PutRetentionConfiguration", Resource = "*" },
    { Sid = "EnableEbsEncryptionByDefault", Effect = "Allow", Action = "ec2:EnableEbsEncryptionByDefault", Resource = "*", Condition = local.ec2_region_condition },
    { Sid = "ModifyVpcBlockPublicAccess", Effect = "Allow", Action = "ec2:ModifyVpcBlockPublicAccessOptions", Resource = "*", Condition = local.ec2_region_condition },
    { Sid = "PutEmrBlockPublicAccess", Effect = "Allow", Action = "elasticmapreduce:PutBlockPublicAccessConfiguration", Resource = "*" },
  ]
  baseline_manage_statements = concat(local.baseline_account_manage_statements, [for s in local.baseline_regional_manage_statements : s if local.baseline_supported])
  # aws:RequestedRegion は要求が呼んだ endpoint の region で、常に要求の文脈に在る（IAM User Guide）。StringNotEquals は key が
  # 無い要求も拒む。
  baseline_region_deny = { for role, statements in {
    plan  = local.baseline_regional_read_statements
    apply = concat(local.baseline_regional_read_statements, local.baseline_regional_manage_statements)
    } : role => [for s in [{
      Sid       = "DenyBaselineOutsideRegions"
      Effect    = "Deny"
      Action    = sort(distinct(flatten([for st in statements : st.Action])))
      Resource  = "*"
      Condition = { StringNotEquals = { "aws:RequestedRegion" = local.baseline_regions } }
  }] : s if local.baseline_supported] }
  workload_read_statements = concat([
    {
      Sid      = "ReadWorkloadTopics"
      Effect   = "Allow"
      Action   = ["sns:GetTopicAttributes", "sns:ListTagsForResource"]
      Resource = local.workload_topic_arns
    },
    {
      Sid       = "ReadWorkloadKeys"
      Effect    = "Allow"
      Action    = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
      Resource  = local.workload_key_arns
      Condition = local.workload_key_conditions
    },
    {
      Sid      = "ReadWorkloadSchedules"
      Effect   = "Allow"
      Action   = ["scheduler:GetSchedule", "scheduler:GetScheduleGroup", "scheduler:ListTagsForResource"]
      Resource = local.workload_schedule_arns
    },
    { Sid = "DescribeChatConfigurations", Effect = "Allow", Action = "chatbot:DescribeSlackChannelConfigurations", Resource = "*" },
    {
      Sid      = "ReadWorkloadRoles"
      Effect   = "Allow"
      Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies"]
      Resource = "${local.iam_arn_prefix}:role/${var.name_prefix}-*"
    },
    {
      Sid      = "ReadWorkloadPolicies"
      Effect   = "Allow"
      Action   = ["iam:GetPolicy", "iam:GetPolicyVersion"]
      Resource = "${local.iam_arn_prefix}:policy/${var.name_prefix}-*"
    },
    # Provider は tag を付けなくても Read のたびに ListTagsForResource を呼ぶ。
    { Sid = "ReadWorkloadBudgets", Effect = "Allow", Action = ["budgets:ListTagsForResource", "budgets:ViewBudget"], Resource = local.workload_budget_arn },
    { Sid = "ReadAnomalyDetection", Effect = "Allow", Action = ["ce:GetAnomalyMonitors", "ce:GetAnomalySubscriptions", "ce:ListTagsForResource"], Resource = local.anomaly_detection_arns },
  ], local.chatbot_slr_read_statements, local.baseline_read_statements)

  workload_role_write_arns   = [for p in values(local.usecase_iam_prefixes) : "${local.iam_arn_prefix}:role/${p}*"]
  workload_policy_write_arns = [for p in values(local.usecase_iam_prefixes) : "${local.iam_arn_prefix}:policy/${p}*"]
  boundary_condition         = { StringEquals = { "iam:PermissionsBoundary" = values(local.boundary_policy_arns) } }

  # apply の role の workload の管理（決定13・14・18）。鍵と Topic の削除は与えない。
  workload_manage_statements = [
    {
      Sid      = "CreateWorkloadKeys"
      Effect   = "Allow"
      Action   = "kms:CreateKey"
      Resource = "*"
      Condition = {
        StringEquals         = { "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT" }
        StringEqualsIfExists = { "kms:KeyOrigin" = "AWS_KMS" }
        BoolIfExists         = { "kms:MultiRegion" = "false" }
      }
    },
    {
      Sid       = "ManageWorkloadKeys"
      Effect    = "Allow"
      Action    = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"]
      Resource  = local.workload_key_arns
      Condition = local.workload_key_conditions
    },
    {
      Sid      = "ManageWorkloadKeyAliases"
      Effect   = "Allow"
      Action   = ["kms:CreateAlias", "kms:DeleteAlias", "kms:UpdateAlias"]
      Resource = concat([for x in local.workload_arn_suffixes : "arn:${data.aws_partition.current.partition}:kms:${x}:alias/${var.name_prefix}-*"], local.workload_key_arns)
    },
    {
      Sid      = "ManageWorkloadTopics"
      Effect   = "Allow"
      Action   = ["sns:CreateTopic", "sns:SetTopicAttributes"]
      Resource = local.workload_topic_arns
    },
    {
      Sid      = "ManageWorkloadSchedules"
      Effect   = "Allow"
      Action   = ["scheduler:CreateSchedule", "scheduler:CreateScheduleGroup", "scheduler:DeleteSchedule", "scheduler:DeleteScheduleGroup", "scheduler:UpdateSchedule"]
      Resource = local.workload_schedule_arns
    },
    { Sid = "CreateChatConfigurations", Effect = "Allow", Action = "chatbot:CreateSlackChannelConfiguration", Resource = "*" },
    {
      Sid      = "ManageChatConfigurations"
      Effect   = "Allow"
      Action   = ["chatbot:DeleteSlackChannelConfiguration", "chatbot:UpdateSlackChannelConfiguration"]
      Resource = "arn:${data.aws_partition.current.partition}:chatbot::${data.aws_caller_identity.current.account_id}:chat-configuration/slack-channel/${var.name_prefix}-*"
    },
    {
      Sid    = "ManageBoundedRoles"
      Effect = "Allow"
      Action = [
        "iam:CreateRole",
        "iam:DeleteRole",
        "iam:DeleteRolePolicy",
        "iam:DetachRolePolicy",
        "iam:PutRolePermissionsBoundary",
        "iam:PutRolePolicy",
        "iam:UpdateAssumeRolePolicy",
        "iam:UpdateRole",
        "iam:UpdateRoleDescription",
      ]
      Resource  = local.workload_role_write_arns
      Condition = local.boundary_condition
    },
    {
      Sid       = "AttachWorkloadPolicies"
      Effect    = "Allow"
      Action    = "iam:AttachRolePolicy"
      Resource  = local.workload_role_write_arns
      Condition = merge(local.boundary_condition, { ArnLike = { "iam:PolicyARN" = local.workload_policy_write_arns } })
    },
    # iam:PermissionsBoundary を受けないので、条件つきの文に入れない。
    { Sid = "ListWorkloadRoleInstanceProfiles", Effect = "Allow", Action = "iam:ListInstanceProfilesForRole", Resource = "${local.iam_arn_prefix}:role/${var.name_prefix}-*" },
    {
      Sid    = "ManageWorkloadPolicies"
      Effect = "Allow"
      Action = [
        "iam:CreatePolicy",
        "iam:CreatePolicyVersion",
        "iam:DeletePolicy",
        "iam:DeletePolicyVersion",
        "iam:ListPolicyVersions",
        "iam:SetDefaultPolicyVersion",
      ]
      Resource = local.workload_policy_write_arns
    },
    {
      Sid       = "PassWorkloadRoles"
      Effect    = "Allow"
      Action    = "iam:PassRole"
      Resource  = local.workload_role_write_arns
      Condition = { StringEquals = { "iam:PassedToService" = ["chatbot.amazonaws.com", "scheduler.amazonaws.com"] } }
    },
    {
      Sid       = "CreateChatbotServiceLinkedRole"
      Effect    = "Allow"
      Action    = "iam:CreateServiceLinkedRole"
      Resource  = local.chatbot_slr_arn
      Condition = { StringEquals = { "iam:AWSServiceName" = "management.chatbot.amazonaws.com" } }
    },
    # budgets:ModifyBudget は予算と通知の作成・更新・削除を1つの Action で許す（決定13・18）。
    { Sid = "ManageWorkloadBudgets", Effect = "Allow", Action = "budgets:ModifyBudget", Resource = local.workload_budget_arn },
    { Sid = "CreateAnomalyMonitors", Effect = "Allow", Action = "ce:CreateAnomalyMonitor", Resource = "*" },
    { Sid = "CreateAnomalySubscriptions", Effect = "Allow", Action = "ce:CreateAnomalySubscription", Resource = "*" },
    {
      Sid      = "ManageAnomalyDetection"
      Effect   = "Allow"
      Action   = ["ce:DeleteAnomalyMonitor", "ce:DeleteAnomalySubscription", "ce:UpdateAnomalyMonitor", "ce:UpdateAnomalySubscription"]
      Resource = local.anomaly_detection_arns
    },
  ]

  # role を作る usecase ごとの boundary の Allow（決定15・16）。Deny は bootstrap_deny_statements を足す。
  boundary_allow_statements = {
    "ops-notification" = concat(
      [
        { Sid = "PublishTopics", Effect = "Allow", Action = "sns:Publish", Resource = [for x in local.workload_arn_suffixes : "arn:${data.aws_partition.current.partition}:sns:${x}:${local.usecase_iam_prefixes["ops-notification"]}*"] },
        {
          Sid       = "UseKeysViaSns"
          Effect    = "Allow"
          Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
          Resource  = local.workload_key_arns
          Condition = { StringEquals = { "kms:ViaService" = [for r in local.workload_regions : "sns.${r}.amazonaws.com"] } }
        },
        {
          Sid      = "ReadAlarms"
          Effect   = "Allow"
          Action   = ["cloudwatch:DescribeAlarmHistory", "cloudwatch:DescribeAlarms", "cloudwatch:ListTagsForResource"]
          Resource = [for x in local.workload_arn_suffixes : "arn:${data.aws_partition.current.partition}:cloudwatch:${x}:alarm:*"]
        },
      ],
      # boundary は 6,144 文字が上限なので、Sid に service の名前を入れない。
      [for a in ["cloudwatch:DescribeAlarmsForMetric", "cloudwatch:GetMetricData", "cloudwatch:GetMetricStatistics", "cloudwatch:GetMetricWidgetImage", "cloudwatch:ListMetrics"] : { Sid = title(split(":", a)[1]), Effect = "Allow", Action = a, Resource = "*" }],
    )
  }

  # Resource "*" でしか与えられない Action は、Action ごとの別の文にする（deployment-identity/ADR-0201 決定1）。
  governance_wildcard_read_actions = [
    "iam:GetAccountPasswordPolicy",
    "iam:ListOrganizationsFeatures",
    "organizations:DescribeOrganization",
    "organizations:ListAWSServiceAccessForOrganization",
    "organizations:ListAccounts",
    "organizations:ListRoots",
  ]
  governance_read_statements = [for s in concat(
    [for a in local.governance_wildcard_read_actions : { Sid = replace(title(replace(a, ":", " ")), " ", ""), Effect = "Allow", Action = a, Resource = "*" }],
    [
      {
        Sid      = "ReadOrganizationalUnits"
        Effect   = "Allow"
        Action   = ["organizations:DescribeOrganizationalUnit", "organizations:ListAccountsForParent", "organizations:ListParents", "organizations:ListTagsForResource"]
        Resource = local.org_ou_arns
      },
      {
        Sid      = "ReadOrganizationPolicies"
        Effect   = "Allow"
        Action   = ["organizations:DescribePolicy", "organizations:ListTagsForResource", "organizations:ListTargetsForPolicy"]
        Resource = local.org_policy_arn
      },
      { Sid = "ReadMemberAccountEmails", Effect = "Allow", Action = "ssm:GetParameter", Resource = local.member_account_emails_parameter_arn },
      { Sid = "ReadBreakGlassUsers", Effect = "Allow", Action = ["iam:GetUser", "iam:ListUserTags"], Resource = local.break_glass_user_arns },
      { Sid = "ReadBreakGlassGroup", Effect = "Allow", Action = ["iam:GetGroup", "iam:GetGroupPolicy"], Resource = local.break_glass_group_arns },
      {
        Sid      = "ReadBreakGlassRole"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = local.break_glass_role_arns
      },
      {
        Sid    = "ReadBreakGlassStackSet"
        Effect = "Allow"
        Action = [
          "cloudformation:DescribeStackInstance",
          "cloudformation:DescribeStackSet",
          "cloudformation:DescribeStackSetOperation",
          "cloudformation:ListStackInstances",
          "cloudformation:ListStackSetOperationResults",
        ]
        Resource = local.break_glass_stack_set_arns
      },
    ],
  ) : s if local.management]

  governance_wildcard_write_actions = [
    "iam:DeleteAccountPasswordPolicy",
    "iam:DisableOrganizationsRootCredentialsManagement",
    "iam:DisableOrganizationsRootSessions",
    "iam:EnableOrganizationsRootCredentialsManagement",
    "iam:EnableOrganizationsRootSessions",
    "iam:UpdateAccountPasswordPolicy",
    "organizations:CreateOrganization",
  ]

  # 組織の削除・メンバーの閉鎖・組織からの除外と、prevent_destroy を持つ構成物の削除は与えない（deployment-identity/ADR-0201 決定3）。
  governance_manage_statements = concat(
    [for a in local.governance_wildcard_write_actions : { Sid = replace(title(replace(a, ":", " ")), " ", ""), Effect = "Allow", Action = a, Resource = "*" }],
    # StackSet に tag を付けないので、tag を伴う作成を拒む。
    [{ Sid = "CloudformationCreateStackSet", Effect = "Allow", Action = "cloudformation:CreateStackSet", Resource = "*", Condition = { Null = { "aws:TagKeys" = "true" } } }],
    [for a in ["organizations:DisableAWSServiceAccess", "organizations:EnableAWSServiceAccess"] : {
      Sid       = replace(title(replace(a, ":", " ")), " ", "")
      Effect    = "Allow"
      Action    = a
      Resource  = "*"
      Condition = { StringEquals = { "organizations:ServicePrincipal" = "iam.amazonaws.com" } }
    }],
    [
      {
        Sid       = "CreateOrganizationsServiceLinkedRole"
        Effect    = "Allow"
        Action    = "iam:CreateServiceLinkedRole"
        Resource  = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/aws-service-role/organizations.amazonaws.com/AWSServiceRoleForOrganizations"
        Condition = { StringEquals = { "iam:AWSServiceName" = "organizations.amazonaws.com" } }
      },
      { Sid = "ManagePolicyTypes", Effect = "Allow", Action = ["organizations:DisablePolicyType", "organizations:EnablePolicyType"], Resource = local.org_root_arns },
      { Sid = "CreateOrganizationalUnits", Effect = "Allow", Action = "organizations:CreateOrganizationalUnit", Resource = concat(local.org_root_arns, local.org_ou_arns) },
      { Sid = "UpdateOrganizationalUnits", Effect = "Allow", Action = "organizations:UpdateOrganizationalUnit", Resource = local.org_ou_arns },
      {
        Sid      = "ManageOrganizationPolicies"
        Effect   = "Allow"
        Action   = ["organizations:CreatePolicy", "organizations:DeletePolicy", "organizations:UpdatePolicy"]
        Resource = local.org_policy_arn
      },
      {
        Sid      = "AttachOrganizationPolicies"
        Effect   = "Allow"
        Action   = ["organizations:AttachPolicy", "organizations:DetachPolicy"]
        Resource = concat(local.org_root_arns, local.org_ou_arns, [local.org_policy_arn])
      },
      { Sid = "ManageBreakGlassUsers", Effect = "Allow", Action = ["iam:CreateUser", "iam:UpdateUser"], Resource = local.break_glass_user_arns },
      {
        Sid    = "ManageBreakGlassGroup"
        Effect = "Allow"
        Action = [
          "iam:AddUserToGroup",
          "iam:CreateGroup",
          "iam:DeleteGroup",
          "iam:DeleteGroupPolicy",
          "iam:ListAttachedGroupPolicies",
          "iam:ListGroupPolicies",
          "iam:PutGroupPolicy",
          "iam:RemoveUserFromGroup",
          "iam:UpdateGroup",
        ]
        Resource = local.break_glass_group_arns
      },
      {
        Sid      = "ManageBreakGlassRole"
        Effect   = "Allow"
        Action   = ["iam:CreateRole", "iam:UpdateAssumeRolePolicy", "iam:UpdateRole", "iam:UpdateRoleDescription"]
        Resource = local.break_glass_role_arns
      },
      {
        Sid       = "AttachBreakGlassAdministratorAccess"
        Effect    = "Allow"
        Action    = ["iam:AttachRolePolicy", "iam:DetachRolePolicy"]
        Resource  = local.break_glass_role_arns
        Condition = { ArnEquals = { "iam:PolicyARN" = local.administrator_access } }
      },
      # StackSet と stack instance の削除は与えない。メンバーの break-glass の role が消える（deployment-identity/ADR-0201「与えないもの」）。
      { Sid = "UpdateBreakGlassStackSet", Effect = "Allow", Action = "cloudformation:UpdateStackSet", Resource = local.break_glass_stack_set_write_arns },
      { Sid = "ManageBreakGlassStackInstances", Effect = "Allow", Action = local.break_glass_stack_instance_actions, Resource = local.break_glass_stack_instance_arns },
      # TargetRegion は type の評価にだけ付くので、条件はこの文に置く。ForAllValues は空の一覧にも成り立つので、Null を外さない（deployment-identity/ADR-0201 決定4）。
      {
        Sid      = "LimitBreakGlassStackInstanceRegions"
        Effect   = "Allow"
        Action   = local.break_glass_stack_instance_actions
        Resource = [local.break_glass_stack_set_type_arn]
        Condition = {
          "ForAllValues:StringEquals" = { "cloudformation:TargetRegion" = data.aws_region.current.region }
          Null                        = { "cloudformation:TargetRegion" = "false" }
        }
      },
    ],
  )

  ci_policy_management_statements = [
    {
      Sid      = "ManageCiPolicies"
      Effect   = "Allow"
      Action   = ["iam:CreatePolicy", "iam:CreatePolicyVersion", "iam:DeletePolicy", "iam:DeletePolicyVersion", "iam:SetDefaultPolicyVersion"]
      Resource = local.ci_policy_arns
    },
    {
      Sid       = "AttachApplyWorkloadPolicy"
      Effect    = "Allow"
      Action    = "iam:AttachRolePolicy"
      Resource  = local.role_arns.apply
      Condition = { ArnEquals = { "iam:PolicyARN" = local.workload_policy_arns } }
    },
    # plan の role は workload の読み取りを apply の role と同じ policy で受ける（決定17）。
    {
      Sid       = "AttachPlanReadPolicy"
      Effect    = "Allow"
      Action    = "iam:AttachRolePolicy"
      Resource  = local.role_arns.plan
      Condition = { ArnEquals = { "iam:PolicyARN" = local.workload_read_policy_arn } }
    },
    {
      Sid       = "AttachApplyIdentityPolicies"
      Effect    = "Allow"
      Action    = "iam:AttachRolePolicy"
      Resource  = local.role_arns.apply_identity
      Condition = { ArnEquals = { "iam:PolicyARN" = local.apply_identity_policy_arns } }
    },
  ]

  # apply-identity の role の、核の外の書き込み（deployment-identity/ADR-0201 決定21）。
  manage_statements = concat([
    # s3:PutLifecycleConfiguration は DeleteBucketLifecycle の権限も兼ねる。
    { Sid = "ConfigureBuckets", Effect = "Allow", Action = concat(local.bucket_configure_actions, ["s3:PutLifecycleConfiguration"]), Resource = local.bucket_arns },
    { Sid = "CreateApplyOutputBucket", Effect = "Allow", Action = "s3:CreateBucket", Resource = var.apply_output_bucket_arn },
    {
      Sid    = "ManageGitHubProvider"
      Effect = "Allow"
      Action = [
        "iam:AddClientIDToOpenIDConnectProvider",
        "iam:CreateOpenIDConnectProvider",
        "iam:DeleteOpenIDConnectProvider",
        "iam:RemoveClientIDFromOpenIDConnectProvider",
        "iam:UpdateOpenIDConnectProviderThumbprint",
      ]
      Resource = aws_iam_openid_connect_provider.github.arn
    },
    # 鍵と alias を作る権限は与えない（deployment-identity/ADR-0201 決定3）。
    { Sid = "ManageStateKey", Effect = "Allow", Action = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"], Resource = var.state_key_arn },
    ], [for s in [
      {
        Sid       = "CreateChatbotServiceLinkedRole"
        Effect    = "Allow"
        Action    = "iam:CreateServiceLinkedRole"
        Resource  = local.chatbot_slr_arn
        Condition = { StringEquals = { "iam:AWSServiceName" = "management.chatbot.amazonaws.com" } }
      },
      { Sid = "UpdateChatbotServiceLinkedRole", Effect = "Allow", Action = "iam:UpdateRole", Resource = local.chatbot_slr_arn },
  ] : s if data.aws_partition.current.partition == "aws"])

  # 決定1。apply-identity と apply の role に同じ文で置く。
  lockout_bypass_deny_statement = { Sid = "DenyLockoutSafetyBypass", Effect = "Deny", Action = ["kms:CreateKey", "kms:PutKeyPolicy"], Resource = "*", Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } } }

  # apply の role の統制の Deny（deployment-identity/ADR-0201 決定5）。Resource "*" で、条件を付けない。
  governance_change_actions = concat(
    [for v in ["Accept", "Attach", "Cancel", "Close", "Create", "Decline", "Delete", "Deregister", "Detach", "Disable", "Enable", "Invite", "Leave", "Move", "Put", "Register", "Remove", "Tag", "Terminate", "Untag", "Update"] : "organizations:${v}*"],
    ["account:CloseAccount"],
    ["iam:DeleteAccountPasswordPolicy", "iam:DisableOrganizationsRoot*", "iam:EnableOrganizationsRoot*"],
    # メンバーの password policy は account-baseline が apply の role で持つ（決定5）。
    local.management ? ["iam:UpdateAccountPasswordPolicy"] : [],
    ["sso:*AccountAssignment", "sso:*FromPermissionSet", "sso:*ToPermissionSet"],
    [for a in ["CreatePermissionSet", "DeletePermissionSet", "ProvisionPermissionSet", "PutPermissionsPolicy", "UpdatePermissionSet"] : "sso:${a}"],
    [for a in ["ActivateOrganizationsAccess", "CreateStackInstances", "CreateStackSet", "DeactivateOrganizationsAccess", "DeleteStackInstances", "DeleteStackSet", "ImportStacksToStackSet", "StopStackSetOperation", "UpdateStackInstances", "UpdateStackSet"] : "cloudformation:${a}"],
    ["sts:AssumeRoot"],
  )

  # apply の role の Deny（deployment-identity/ADR-0201 決定5）。
  bootstrap_deny_statements = [
    { Sid = "DenyIdentityChange", Effect = "Deny", Action = "*", Resource = concat(local.ci_arn_patterns, [aws_iam_openid_connect_provider.github.arn]) },
    { Sid = "DenyIdentityStateChange", Effect = "Deny", Action = "*", Resource = [local.identity_state_key, "${local.identity_state_key}.tflock"] },
    { Sid = "DenyStateBucketChange", Effect = "Deny", NotAction = ["s3:GetEncryptionConfiguration", "s3:ListBucket"], Resource = var.state_bucket_arn },
    { Sid = "DenyStateVersionDeletion", Effect = "Deny", Action = "s3:DeleteObjectVersion", Resource = "${var.state_bucket_arn}/*" },
    { Sid = "DenyPlanOutputChange", Effect = "Deny", Action = "*", Resource = [var.plan_output_bucket_arn, "${var.plan_output_bucket_arn}/*"] },
    { Sid = "DenyApplyOutputBucketChange", Effect = "Deny", Action = "*", Resource = var.apply_output_bucket_arn },
    { Sid = "DenyApplyOutputChange", Effect = "Deny", NotAction = ["s3:PutObject"], Resource = "${var.apply_output_bucket_arn}/*" },
    # kms:* にすると、Key Policy が role を名指して許す暗号操作まで塞ぐ。
    { Sid = "DenyStateKeyChange", Effect = "Deny", Action = local.state_key_administration_actions, Resource = var.state_key_arn },
    { Sid = "DenyGovernanceChange", Effect = "Deny", Action = local.governance_change_actions, Resource = "*" },
    { Sid = "DenyBreakGlassChange", Effect = "Deny", Action = "*", Resource = [for p in local.break_glass_arn_prefixes : "${p}*"] },
  ]

  statements = {
    plan = concat(
      local.bucket_read_statements,
      [
        { Sid = "ReadState", Effect = "Allow", Action = "s3:GetObject", Resource = [local.identity_state_key, local.workload_state_key] },
        { Sid = "WritePlanOutput", Effect = "Allow", Action = "s3:PutObject", Resource = local.plan_output_keys },
        { Sid = "ReadStateKey", Effect = "Allow", Action = local.state_key_read_actions, Resource = var.state_key_arn },
      ],
      local.identity_read_statements,
      local.governance_read_statements,
      local.baseline_region_deny.plan,
    )

    apply = concat([
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
      { Sid = "ReadStateBucketEncryption", Effect = "Allow", Action = "s3:GetEncryptionConfiguration", Resource = var.state_bucket_arn },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.workload_state_key },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.workload_state_key}.tflock" },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
      { Sid = "WriteWorkloadApplyOutput", Effect = "Allow", Action = "s3:PutObject", Resource = "${var.apply_output_bucket_arn}/*" },
      ], local.bootstrap_deny_statements, [
      { Sid = "DenyBoundaryRemoval", Effect = "Deny", Action = "iam:DeleteRolePermissionsBoundary", Resource = "*" },
      local.lockout_bypass_deny_statement,
      # 人が tag を付けた、管理アカウントの bootstrap の前提の異常検知を守る（決定13）。boundary へ写さない。
      {
        Sid       = "DenyBootstrapAnomalyDetectionChange"
        Effect    = "Deny"
        Action    = ["ce:DeleteAnomalyMonitor", "ce:DeleteAnomalySubscription", "ce:UntagResource", "ce:UpdateAnomalyMonitor", "ce:UpdateAnomalySubscription"]
        Resource  = local.anomaly_detection_arns
        Condition = { Null = { "aws:ResourceTag/${local.bootstrap_tag_key}" = "false" } }
      },
    ], local.baseline_region_deny.apply)

    apply_identity = concat(
      local.bucket_read_statements,
      [
        { Sid = "WriteIdentityState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.identity_state_key },
        { Sid = "LockIdentityState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.identity_state_key}.tflock" },
        {
          Sid    = "ManageCiRoles"
          Effect = "Allow"
          Action = [
            "iam:CreateRole",
            "iam:DeleteRole",
            "iam:DeleteRolePolicy",
            "iam:DetachRolePolicy",
            "iam:ListInstanceProfilesForRole",
            "iam:PutRolePolicy",
            "iam:UpdateAssumeRolePolicy",
            "iam:UpdateRole",
            "iam:UpdateRoleDescription",
          ]
          Resource = local.identity_role_arns
        },
        { Sid = "ReadStateKey", Effect = "Allow", Action = local.state_key_read_actions, Resource = var.state_key_arn },
        local.lockout_bypass_deny_statement,
      ],
      local.identity_read_statements,
      [{ Sid = "WriteApplyOutput", Effect = "Allow", Action = "s3:PutObject", Resource = "${var.plan_output_bucket_arn}/apply/*" }],
      local.governance_read_statements,
      local.ci_policy_management_statements,
      local.chatbot_slr_read_statements,
    )

    # 範囲と、各 Action が要る理由は deployment-identity/ADR-0201 決定9（unlock は決定11）。
    unlock_identity = [
      { Sid = "ListIdentityLock", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn, Condition = { StringEquals = { "s3:prefix" = "identity/terraform.tfstate.tflock" } } },
      { Sid = "RemoveIdentityLock", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject"], Resource = "${local.identity_state_key}.tflock" },
    ]

    unlock = [
      { Sid = "ListWorkloadLock", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn, Condition = { StringEquals = { "s3:prefix" = "workload/terraform.tfstate.tflock" } } },
      { Sid = "RemoveWorkloadLock", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject"], Resource = "${local.workload_state_key}.tflock" },
    ]
  }
}

resource "aws_iam_role_policy" "apply_identity" {
  name = "apply_identity"
  role = aws_iam_role.apply_identity.name
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.statements.apply_identity
  })

  depends_on = [aws_iam_role_policy_attachment.manage]

  lifecycle {
    # 別のアカウントやリージョンの鍵を渡すと、apply-identity は自分の state の鍵を管理できなくなる。他の role は
    # この policy の後に作るので、ここで落ちれば他の role の policy にも届かない。
    precondition {
      condition     = startswith(var.state_key_arn, local.state_key_arn_prefix)
      error_message = "state_key_arn が、role と同じ partition・リージョン・アカウントの鍵ではありません。"
    }

    # apply の role の DenyBreakGlassChange は path の pattern なので、その外の ARN には効かない。
    precondition {
      condition     = length(var.break_glass_arns) > 0 && alltrue([for a in var.break_glass_arns : anytrue([for p in local.break_glass_arn_prefixes : startswith(a, p)])])
      error_message = "break_glass_arns が空か、role と同じ partition・アカウントの、path が /break-glass/ の IAM user・group・role の ARN ではありません。"
    }

    # 管理アカウントでは種類ごとの文の Resource になる。空の Resource は IAM が拒む。
    precondition {
      condition     = !local.management || (length(local.break_glass_user_arns) > 0 && length(local.break_glass_group_arns) > 0 && length(local.break_glass_role_arns) > 0)
      error_message = "管理アカウント（env が admin）の break_glass_arns は、break-glass の IAM user・group・role をそれぞれ1つ以上含めてください。"
    }

    # role の名前は StackSet の名前にもなる。IAM が許して StackSet が許さない名前では、Allow がどの StackSet にも当たらない。
    precondition {
      condition     = !local.management || alltrue([for n in local.break_glass_stack_set_names : can(regex("^[A-Za-z][-A-Za-z0-9]*$", n))])
      error_message = "管理アカウント（env が admin）の break-glass の role の名前は、英字で始まり英数字とハイフンだけの名前にしてください（StackSet の名前の制約）。"
    }
  }
}

moved {
  from = aws_iam_role_policy.this["apply_identity"]
  to   = aws_iam_role_policy.apply_identity
}

resource "aws_iam_role_policy" "this" {
  for_each = local.ci_roles

  name = each.key
  role = aws_iam_role.this[each.key].name
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.statements[each.key]
  })
}
