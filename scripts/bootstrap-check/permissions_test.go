package main

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// permissions の desc を、表と独立に書いたもの（ADR-0702 決定15）。$ で始まる記号は permExpand が展開する。
// permExpand は引数の順に照合するので、長い記号を先に置く。
var (
	permS3Descs = []string{
		"plan $b: 15 件が allowed",
		"apply-identity $b: 17 件が allowed",
		"apply-identity $key: 1 件が allowed",
		"apply-identity $role/plan: 2 件が allowed",
		"plan $pb: 15 件が allowed",
		"plan $pb/pr/1/x: 1 件が allowed",
		"apply-identity $pb: 16 件が allowed",
		"apply-identity $pb/apply/1/x: 1 件が allowed",
		"plan $ab: 16 件が allowed",
		"apply-identity $ab: 19 件が allowed",
		"apply-identity $ab: 1 件が implicitDeny",
		"plan $ab: 2 件が implicitDeny",
		"apply $b: 2 件が allowed",
		"plan $key: 1 件が implicitDeny",
		"plan $role/plan: 1 件が implicitDeny",
		"plan $pb/pr/1/x: 2 件が implicitDeny",
		"plan $pb/x: 1 件が implicitDeny",
		"apply-identity $pb/pr/1/x: 2 件が implicitDeny",
		"apply-identity $pb/apply/1/x: 2 件が implicitDeny",
		"apply $role/apply-identity: 3 件が explicitDeny",
		"apply $b: 5 件が explicitDeny",
		"apply $key: 2 件が explicitDeny",
		"apply $pb: 3 件が explicitDeny",
		"apply $pb/pr/1/x: 2 件が explicitDeny",
		"apply $ab/x: 1 件が allowed",
		"apply $ab/x: 4 件が explicitDeny",
		"apply $ab: 5 件が explicitDeny",
	}
	permKeyDescs = []string{
		"apply-identity $sk: 6 件が allowed",
		"apply-identity *: 1 件が allowed",
		"plan $sk: 4 件が allowed",
		"plan *: 1 件が allowed",
		"apply *: 1 件が allowed",
		"apply-identity *: 1 件が implicitDeny",
		"apply-identity $sk: 8 件が implicitDeny",
		"apply-identity $other: 2 件が implicitDeny",
		"plan $sk: 3 件が implicitDeny",
		"plan $other: 1 件が implicitDeny",
		"apply $sk: 2 件が implicitDeny",
		"apply $sk: 21 件が explicitDeny",
		"apply-identity *: 1 件が explicitDeny",
	}
	permGovernanceDescs = []string{
		"apply-identity $policy/apply-identity-governance: 7 件が allowed",
		"apply-identity $iam:policy/other: 3 件が implicitDeny",
		"apply-identity $role/apply-identity: 1 件が allowed",
		"apply-identity $role/apply-identity: 1 件が implicitDeny",
		"apply-identity $policy/apply-identity-manage: 7 件が allowed",
		"apply-identity $role/apply-identity: 1 件が allowed",
		"apply-identity $role/apply: 1 件が implicitDeny",
		"plan $policy/apply-identity-manage: 2 件が allowed",
		"apply $policy/apply-identity-manage: 1 件が explicitDeny",
		"plan *: 5 件が allowed",
		"plan $policy/apply-identity-governance: 2 件が allowed",
		"plan *: 2 件が implicitDeny",
		"apply *: 11 件が explicitDeny",
		"apply *: 3 件が implicitDeny",
		"apply $bg: 2 件が explicitDeny",
		"apply $policy/apply-identity-governance: 1 件が explicitDeny",
	}
	permGovernanceGatedDescs = []string{
		"apply-identity *: 3 件が allowed",
		"apply-identity $bg: 1 件が allowed",
		"apply-identity *: 1 件が implicitDeny",
		"apply-identity $bg: 1 件が implicitDeny",
	}
	permSLRDescs = []string{
		"apply-identity $slr: 1 件が allowed",
		"apply-identity $slr: 1 件が implicitDeny",
		"apply-identity $slr: 2 件が allowed",
		"apply-identity $slr: 5 件が implicitDeny",
		"plan $slr: 1 件が allowed",
		"plan $slr: 2 件が implicitDeny",
	}
	permWorkloadDescs = []string{
		"apply-identity $policy/apply-workload: 7 件が allowed",
		"apply-identity $policy/apply-workload-read: 7 件が allowed",
		"apply-identity $policy/apply-workload-2: 7 件が allowed",
		"apply-identity $policy/boundary-ops-notification: 7 件が allowed",
		"apply-identity $role/apply: 1 件が allowed",
		"apply-identity $role/apply: 1 件が allowed",
		"apply-identity $role/apply: 1 件が allowed",
		"apply-identity $role/apply: 1 件が implicitDeny",
		"apply-identity $role/plan: 1 件が allowed",
		"apply-identity $role/plan: 1 件が implicitDeny",
		"apply-identity $role/plan: 1 件が implicitDeny",
		"plan $policy/apply-workload: 2 件が allowed",
		"plan $policy/apply-workload-read: 2 件が allowed",
		"plan arn:aws:sns:us-east-1:123456789012:tabp-admin-ops-notification-us-east-1: 2 件が allowed",
		"plan arn:aws:sns:us-west-2:123456789012:tabp-admin-ops-notification: 1 件が implicitDeny",
		"plan $topic: 2 件が allowed",
		"plan $topic: 1 件が implicitDeny",
		"plan $wk: 1 件が allowed",
		"plan $wk: 1 件が implicitDeny",
		"apply $policy/apply-workload: 2 件が explicitDeny",
		"apply $policy/apply-workload-read: 2 件が explicitDeny",
		"apply $policy/boundary-ops-notification: 2 件が explicitDeny",
		"apply $role/other: 2 件が explicitDeny",
		"apply *: 1 件が explicitDeny",
		"apply *: 2 件が explicitDeny",
	}
	permWorkloadGatedDescs = []string{
		"apply $iam:role/tabp-admin-ops-heartbeat: 3 件が allowed",
		"apply $iam:role/tabp-admin-ops-heartbeat: 2 件が implicitDeny",
		"apply $iam:role/tabp-admin-OPS-heartbeat: 1 件が implicitDeny",
		"apply $iam:role/tabp-admin-ops-heartbeat: 1 件が allowed",
		"apply $iam:role/tabp-admin-ops-heartbeat: 1 件が implicitDeny",
		"apply $topic: 2 件が allowed",
		"apply $topic: 2 件が implicitDeny",
		"apply $wk: 1 件が implicitDeny",
		"apply $slr: 1 件が allowed",
	}
	permBudgetsDescs = []string{
		"plan $budget: 2 件が allowed",
		"plan $monitor: 2 件が allowed",
		"plan $subscription: 2 件が allowed",
		"plan $budget: 1 件が implicitDeny",
		"plan *: 2 件が implicitDeny",
		"plan $monitor: 2 件が implicitDeny",
		"plan $subscription: 2 件が implicitDeny",
		"apply $monitor: 3 件が explicitDeny",
		"apply $subscription: 3 件が explicitDeny",
	}
	permBudgetsGatedDescs = []string{
		"apply $budget: 1 件が allowed",
		"apply *: 2 件が allowed",
		"apply $monitor: 2 件が allowed",
		"apply $subscription: 2 件が allowed",
	}
	permUnlockIdentityDescs = []string{
		"unlock-identity $b: 1 件が allowed",
		"unlock-identity $key.tflock: 2 件が allowed",
		"unlock-identity $b: 1 件が implicitDeny",
		"unlock-identity $key.tflock: 2 件が implicitDeny",
		"unlock-identity $key: 3 件が implicitDeny",
		"unlock-identity $sk: 1 件が implicitDeny",
	}
	permUnlockDescs = []string{
		"unlock $b: 1 件が allowed",
		"unlock $wkey.tflock: 2 件が allowed",
		"unlock $b: 1 件が implicitDeny",
		"unlock $wkey.tflock: 2 件が implicitDeny",
		"unlock $key.tflock: 2 件が implicitDeny",
		"unlock $wkey: 3 件が implicitDeny",
		"unlock $sk: 1 件が implicitDeny",
	}
	outputPolicyPlanDescs = []string{
		"plan $pb/pr/1/x（Bucket Policy を含む）: 1 件が allowed",
		"plan $pb/branch/1/x（Bucket Policy を含む）: 1 件が allowed",
		"apply-identity $pb/apply/1/x（Bucket Policy を含む）: 1 件が allowed",
		"plan $pb/apply/1/x（Bucket Policy を含む）: 1 件が explicitDeny",
		"apply-identity $pb/pr/1/x（Bucket Policy を含む）: 1 件が explicitDeny",
		"apply $pb/pr/1/x（Bucket Policy を含む）: 1 件が explicitDeny",
		"plan $pb/pr/1/x（Bucket Policy を含む）: 2 件が explicitDeny",
		"apply-identity $pb/apply/1/x（Bucket Policy を含む）: 2 件が explicitDeny",
	}
	outputPolicyApplyDescs = []string{
		"apply $ab/x（Bucket Policy を含む）: 1 件が allowed",
		"apply-identity $ab/x（Bucket Policy を含む）: 1 件が explicitDeny",
		"apply $ab/x（Bucket Policy を含む）: 2 件が explicitDeny",
	}
)

const (
	// permissions の件数。条件つきの確認の有無で変わる。
	permissionsBase     = 109
	governanceGated     = 4
	workloadGatedCount  = 9
	budgetsGatedCount   = 4
	outputPolicyCount   = 13
	governanceAbsent    = "  統制の policy がまだ付いていないので、作成と設定の権限は確かめない（統制の policy を作って付ける apply の後にもう一度走らせる）"
	workloadAbsent      = "  workload の2つの policy がまだ付いていないので、apply の role の workload の権限は確かめない（2つの policy を作って付ける apply の後にもう一度走らせる）"
	budgetsAbsent       = "  workload の2つの policy がまだ付いていないので、apply の role の budgets と Cost Anomaly Detection の作成と変更の権限は確かめない（2つの policy を作って付ける apply の後にもう一度走らせる）"
	budgetsUnknown      = "  apply の role の managed policy を確かめられないので（上の NG）、apply の role の budgets と Cost Anomaly Detection の作成と変更の権限は確かめない"
	attachedIdentityKey = "iam list-attached-role-policies --role-name tabp-admin-ci-apply-identity --output json --region " + testRegion
	attachedApplyKey    = "iam list-attached-role-policies --role-name tabp-admin-ci-apply --output json --region " + testRegion
	governanceAttached  = `{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-manage"},{"PolicyName":"tabp-admin-ci-apply-identity-governance"}]}`
	workloadAttached    = `{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload-read"},{"PolicyName":"tabp-admin-ci-apply-workload"}]}`
	noneAttached        = `{"AttachedPolicies":[]}`
)

var permExpand = strings.NewReplacer(
	"$reads", "s3:GetBucketAcl s3:GetBucketCORS s3:GetBucketWebsite s3:GetAccelerateConfiguration s3:GetBucketRequestPayment "+
		"s3:GetBucketLogging s3:GetLifecycleConfiguration s3:GetReplicationConfiguration s3:GetBucketObjectLockConfiguration "+
		"s3:GetBucketTagging s3:GetBucketPolicy s3:GetBucketVersioning s3:GetEncryptionConfiguration "+
		"s3:GetBucketPublicAccessBlock s3:GetBucketOwnershipControls",
	"$manage", "iam:CreatePolicy iam:CreatePolicyVersion iam:DeletePolicyVersion iam:SetDefaultPolicyVersion iam:GetPolicy iam:GetPolicyVersion iam:ListPolicyVersions",
	"$bg", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass",
	"$budget", "arn:aws:budgets::123456789012:budget/tabp-admin-x",
	"$b", "arn:aws:s3:::"+testBucket,
	"$key", "arn:aws:s3:::"+testBucket+"/identity/terraform.tfstate",
	"$wkey", "arn:aws:s3:::"+testBucket+"/workload/terraform.tfstate",
	"$pb", "arn:aws:s3:::"+testPlanBucket,
	"$ab", "arn:aws:s3:::"+testApplyBucket,
	"$sk", testKeyARN,
	"$other", "arn:aws:kms:ap-northeast-1:123456789012:key/00000000-0000-0000-0000-0000000000a0",
	"$wk", "arn:aws:kms:ap-northeast-1:123456789012:key/00000000-0000-0000-0000-0000000000a1",
	"$role/", "arn:aws:iam::123456789012:role/tabp-admin-ci-",
	"$policy/", "arn:aws:iam::123456789012:policy/tabp-admin-ci-",
	"$iam", "arn:aws:iam::123456789012",
	"$topic", "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-ops-notification",
	"$slr", "arn:aws:iam::123456789012:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot",
	"$monitor", "arn:aws:ce::123456789012:anomalymonitor/x",
	"$subscription", "arn:aws:ce::123456789012:anomalysubscription/x",
)

// permRowActions は、permissions の表の各行（区分ごとに行、条件つきの行の順）の action と context を、表と独立に
// 書いたもの。"<action ...> ; <key> = <value> (<type>) ; ..." の形で、$ の記号は permExpand が展開する。
var permRowActions = []string{
	// S3 と IAM
	"$reads",
	"$reads s3:PutBucketPolicy s3:PutLifecycleConfiguration",
	"s3:PutObject",
	"iam:PutRolePolicy iam:UpdateAssumeRolePolicy",
	"$reads",
	"s3:PutObject",
	"$reads s3:PutLifecycleConfiguration",
	"s3:PutObject",
	"s3:ListBucket $reads",
	"s3:CreateBucket s3:ListBucket $reads s3:PutBucketPolicy s3:PutLifecycleConfiguration",
	"s3:DeleteBucket",
	"s3:CreateBucket s3:PutBucketPolicy",
	"s3:ListBucket s3:GetEncryptionConfiguration",
	"s3:PutObject",
	"iam:PutRolePolicy",
	"s3:DeleteObject s3:DeleteObjectVersion",
	"s3:PutObject",
	"s3:PutObject s3:DeleteObject",
	"s3:DeleteObject s3:DeleteObjectVersion",
	"iam:PutRolePolicy iam:UpdateAssumeRolePolicy sts:AssumeRole",
	"s3:PutBucketPolicy s3:DeleteBucketPolicy s3:PutLifecycleConfiguration s3:PutBucketTagging s3:TagResource",
	"s3:PutObject s3:DeleteObject",
	"s3:PutBucketPolicy s3:PutLifecycleConfiguration s3:GetBucketPolicy",
	"s3:PutObject s3:DeleteObjectVersion",
	"s3:PutObject",
	"s3:GetObject s3:DeleteObject s3:DeleteObjectVersion s3:PutObjectAcl",
	"s3:ListBucket s3:GetBucketPolicy s3:PutBucketPolicy s3:PutLifecycleConfiguration s3:DeleteBucket",
	// state の鍵
	"kms:DescribeKey kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListResourceTags kms:EnableKeyRotation kms:PutKeyPolicy",
	"kms:ListAliases",
	"kms:DescribeKey kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListResourceTags",
	"kms:ListAliases",
	"kms:ListAliases",
	"kms:CreateKey",
	"kms:CreateAlias kms:ScheduleKeyDeletion kms:DisableKey kms:DisableKeyRotation kms:EnableKey kms:UpdateKeyDescription kms:TagResource kms:Decrypt",
	"kms:DescribeKey kms:PutKeyPolicy",
	"kms:PutKeyPolicy kms:Decrypt kms:GenerateDataKey",
	"kms:DescribeKey",
	"kms:Decrypt kms:GenerateDataKey",
	"kms:CancelKeyDeletion kms:CreateAlias kms:DeleteAlias kms:DescribeKey kms:DisableKey kms:DisableKeyRotation kms:EnableKey kms:EnableKeyRotation " +
		"kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListGrants kms:ListKeyPolicies kms:ListResourceTags kms:PutKeyPolicy kms:RevokeGrant " +
		"kms:RotateKeyOnDemand kms:ScheduleKeyDeletion kms:TagResource kms:UntagResource kms:UpdateAlias kms:UpdateKeyDescription",
	"kms:PutKeyPolicy ; kms:BypassPolicyLockoutSafetyCheck = true (boolean)",
	// 統制の構成物
	"$manage",
	"iam:CreatePolicy iam:CreatePolicyVersion iam:GetPolicy",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-identity-governance (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = arn:aws:iam::aws:policy/AdministratorAccess (arn)",
	"$manage",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-identity-manage (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-identity-manage (arn)",
	"iam:GetPolicy iam:GetPolicyVersion",
	"iam:CreatePolicyVersion",
	"organizations:DescribeOrganization organizations:ListRoots organizations:ListAccounts iam:ListOrganizationsFeatures iam:GetAccountPasswordPolicy",
	"iam:GetPolicy iam:GetPolicyVersion",
	"organizations:CreateOrganization iam:UpdateAccountPasswordPolicy",
	"organizations:CreateOrganization organizations:AttachPolicy organizations:LeaveOrganization account:CloseAccount iam:EnableOrganizationsRootSessions " +
		"iam:UpdateAccountPasswordPolicy sso:CreatePermissionSet sso:AttachManagedPolicyToPermissionSet sso:DeleteAccountAssignment sso:PutPermissionsPolicy sts:AssumeRoot",
	"sso:DescribePermissionSet sso:GetPermissionSet sso:ListAccountAssignments",
	"iam:UpdateAssumeRolePolicy iam:AttachRolePolicy",
	"iam:CreatePolicyVersion",
	"organizations:CreateOrganization organizations:DescribeOrganization iam:UpdateAccountPasswordPolicy",
	"iam:AttachRolePolicy ; iam:PolicyARN = arn:aws:iam::aws:policy/AdministratorAccess (arn)",
	"organizations:DeleteOrganization",
	"iam:DeleteRole",
	// Chatbot の service-linked role
	"iam:CreateServiceLinkedRole ; iam:AWSServiceName = management.chatbot.amazonaws.com (string)",
	"iam:CreateServiceLinkedRole ; iam:AWSServiceName = config.amazonaws.com (string)",
	"iam:GetRole iam:UpdateRole",
	"iam:DeleteServiceLinkedRole iam:TagRole iam:UntagRole iam:UpdateRoleDescription iam:ListRoleTags",
	"iam:GetRole",
	"iam:UpdateRole iam:CreateServiceLinkedRole",
	// workload の権限の policy と permissions boundary
	"$manage",
	"$manage",
	"$manage",
	"$manage",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-workload (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-workload-read (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-workload-2 (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/boundary-ops-notification (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-workload-read (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-workload (arn)",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-workload-2 (arn)",
	"iam:GetPolicy iam:GetPolicyVersion",
	"iam:GetPolicy iam:GetPolicyVersion",
	"sns:GetTopicAttributes sns:ListTagsForResource",
	"sns:GetTopicAttributes",
	"sns:GetTopicAttributes sns:ListTagsForResource",
	"sns:SetTopicAttributes",
	"kms:DescribeKey ; kms:KeySpec = SYMMETRIC_DEFAULT (string) ; kms:KeyUsage = ENCRYPT_DECRYPT (string) ; kms:KeyOrigin = AWS_KMS (string) ; kms:MultiRegion = false (boolean)",
	"kms:DescribeKey ; kms:KeySpec = RSA_2048 (string) ; kms:KeyUsage = SIGN_VERIFY (string) ; kms:KeyOrigin = AWS_KMS (string) ; kms:MultiRegion = false (boolean)",
	"iam:CreatePolicyVersion iam:DeletePolicy",
	"iam:CreatePolicyVersion iam:DeletePolicy",
	"iam:CreatePolicyVersion iam:DeletePolicy",
	"iam:CreateRole iam:PassRole",
	"iam:DeleteRolePermissionsBoundary",
	"kms:CreateKey kms:PutKeyPolicy ; kms:BypassPolicyLockoutSafetyCheck = true (boolean)",
	"iam:CreateRole iam:PutRolePolicy iam:DeleteRole ; iam:PermissionsBoundary = $policy/boundary-ops-notification (arn)",
	"iam:CreateRole iam:PutRolePolicy",
	"iam:CreateRole ; iam:PermissionsBoundary = $policy/boundary-ops-notification (arn)",
	"iam:PassRole ; iam:PassedToService = scheduler.amazonaws.com (string)",
	"iam:PassRole ; iam:PassedToService = lambda.amazonaws.com (string)",
	"sns:CreateTopic sns:SetTopicAttributes",
	"sns:DeleteTopic sns:Subscribe",
	"kms:ScheduleKeyDeletion",
	"iam:GetRole",
	// budgets と Cost Anomaly Detection
	"budgets:ViewBudget budgets:ListTagsForResource",
	"ce:GetAnomalyMonitors ce:ListTagsForResource",
	"ce:GetAnomalySubscriptions ce:ListTagsForResource",
	"budgets:ModifyBudget",
	"ce:CreateAnomalyMonitor ce:CreateAnomalySubscription",
	"ce:UpdateAnomalyMonitor ce:DeleteAnomalyMonitor",
	"ce:UpdateAnomalySubscription ce:DeleteAnomalySubscription",
	"ce:UpdateAnomalyMonitor ce:DeleteAnomalyMonitor ce:UntagResource ; aws:ResourceTag/tabp:bootstrap = true (string)",
	"ce:UpdateAnomalySubscription ce:DeleteAnomalySubscription ce:UntagResource ; aws:ResourceTag/tabp:bootstrap = true (string)",
	"budgets:ModifyBudget",
	"ce:CreateAnomalyMonitor ce:CreateAnomalySubscription",
	"ce:UpdateAnomalyMonitor ce:DeleteAnomalyMonitor",
	"ce:UpdateAnomalySubscription ce:DeleteAnomalySubscription",
	// unlock-identity の role
	"s3:ListBucket ; s3:prefix = identity/terraform.tfstate.tflock (string)",
	"s3:GetObject s3:DeleteObject",
	"s3:ListBucket ; s3:prefix = identity/terraform.tfstate (string)",
	"s3:PutObject s3:DeleteObjectVersion",
	"s3:GetObject s3:PutObject s3:DeleteObject",
	"kms:Decrypt",
	// unlock の role
	"s3:ListBucket ; s3:prefix = workload/terraform.tfstate.tflock (string)",
	"s3:GetObject s3:DeleteObject",
	"s3:ListBucket ; s3:prefix = identity/terraform.tfstate.tflock (string)",
	"s3:PutObject s3:DeleteObjectVersion",
	"s3:GetObject s3:DeleteObject",
	"s3:GetObject s3:PutObject s3:DeleteObject",
	"kms:Decrypt",
}

// parseRowActions は、permRowActions の1件を action の並びと context entry に分ける。
func parseRowActions(t *testing.T, entry string) ([]string, []string) {
	t.Helper()

	parts := strings.Split(entry, " ; ")
	actions := strings.Fields(permExpand.Replace(parts[0]))

	var context []string

	for _, c := range parts[1:] {
		key, rest, ok := strings.Cut(c, " = ")
		require.True(t, ok, c)

		value, typ, ok := strings.Cut(rest, " (")
		require.True(t, ok, c)

		context = append(context, "ContextKeyName="+key+",ContextKeyValues="+permExpand.Replace(value)+",ContextKeyType="+strings.TrimSuffix(typ, ")"))
	}

	return actions, context
}

func expand(descs []string, prefix string) []string {
	out := make([]string, 0, len(descs))
	for _, d := range descs {
		out = append(out, prefix+permExpand.Replace(d))
	}

	return out
}

// simActions は、simulate-principal-policy の引数列の --action-names の後ろ（--output の前まで）。
func simActions(args []string) []string {
	i := slices.Index(args, "--action-names")
	j := slices.Index(args, "--output")

	if i < 0 || j < i {
		return nil
	}

	return args[i+1 : j]
}

func simResult(actions []string, decision string) awsResult {
	results := make([]map[string]string, 0, len(actions))
	for _, a := range actions {
		results = append(results, map[string]string{"EvalActionName": a, "EvalDecision": decision, "EvalResourceName": "r"})
	}

	b, _ := json.Marshal(map[string]any{"EvaluationResults": results})

	return okResult(string(b))
}

func tableHandler(n names, rows []simRow) func(args []string) (awsResult, bool) {
	wants := map[string]string{}
	for _, r := range rows {
		wants[strings.Join(r.args(n), " ")] = r.want
	}

	return func(args []string) (awsResult, bool) {
		want, ok := wants[strings.Join(args[:len(args)-4], " ")]
		if !ok {
			return awsResult{}, false
		}

		return simResult(simActions(args), want), true
	}
}

func allRows(n names, sk string) []simRow {
	var rows []simRow
	for _, s := range permissionSections(n, sk, "") {
		rows = append(append(rows, s.rows...), s.gated...)
	}

	return rows
}

func permFake(t *testing.T, identity, apply awsResult) *fakeAWS {
	t.Helper()

	n := testNames("")

	return &fakeAWS{t: t, responses: map[string]awsResult{
		jsonKey("kms", "describe-key", "--key-id", testAlias): okResult(describeKeyBody),
		attachedIdentityKey: identity,
		attachedApplyKey:    apply,
	}, handler: tableHandler(n, allRows(n, testKeyARN))}
}

// permExpected は、permissions の出力の行。governance と workload は条件つきの確認の区分の末尾の行（nil なら
// 条件つきの確認をすべて ok とする）。
func permExpected(governance, workload, budgets []string) []string {
	out := []string{"# S3 と IAM"}
	out = append(out, expand(permS3Descs, "ok  ")...)
	out = append(append(out, "# state の鍵"), expand(permKeyDescs, "ok  ")...)
	out = append(append(out, "# 統制の構成物と apply-identity の policy（deployment-identity/ADR-0201 決定3・5・7）"), expand(permGovernanceDescs, "ok  ")...)

	if governance == nil {
		governance = expand(permGovernanceGatedDescs, "ok  ")
	}

	out = append(append(out, governance...), "# Chatbot の service-linked role（deployment-identity/ADR-0201 決定3）")
	out = append(append(out, expand(permSLRDescs, "ok  ")...), "# workload の権限の policy と permissions boundary（deployment-identity/ADR-0201 決定5・13-17）")
	out = append(out, expand(permWorkloadDescs, "ok  ")...)

	if workload == nil {
		workload = expand(permWorkloadGatedDescs, "ok  ")
	}

	out = append(append(out, workload...), "# budgets と Cost Anomaly Detection（deployment-identity/ADR-0201 決定13）")
	out = append(out, expand(permBudgetsDescs, "ok  ")...)

	if budgets == nil {
		budgets = expand(permBudgetsGatedDescs, "ok  ")
	}

	out = append(append(out, budgets...), "# unlock-identity の role")
	out = append(append(out, expand(permUnlockIdentityDescs, "ok  ")...), "# unlock の role")

	return append(out, expand(permUnlockDescs, "ok  ")...)
}

func Test_ctxEntry(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ContextKeyName=aws:SecureTransport,ContextKeyValues=true,ContextKeyType=boolean", ctxEntry("aws:SecureTransport", "true", "boolean"))
}

func Test_sim(t *testing.T) {
	t.Parallel()

	assert.Equal(t, simRow{want: allowed, role: "plan", resource: "*", actions: []string{"a", "b"}}, sim(allowed, "plan", "*", "a", "b"))
}

func Test_simCtx(t *testing.T) {
	t.Parallel()

	assert.Equal(t, simRow{want: explicitDeny, role: "apply", resource: "*", context: []string{"c"}, actions: []string{"a"}},
		simCtx(explicitDeny, "apply", "*", []string{"c"}, "a"))
}

func Test_simRow_desc(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "plan arn:x: 2 件が allowed", sim(allowed, "plan", "arn:x", "a", "b").desc())

	r := sim(explicitDeny, "apply", "arn:x", "a")
	r.bucketPolicy = true
	assert.Equal(t, "apply arn:x（Bucket Policy を含む）: 1 件が explicitDeny", r.desc())
}

func Test_simRow_args(t *testing.T) {
	t.Parallel()

	n := testNames("")
	role := "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"

	assert.Equal(t, []string{"iam", "simulate-principal-policy", "--policy-source-arn", role, "--resource-arns", "arn:x", "--action-names", "a", "b"},
		sim(allowed, "plan", "arn:x", "a", "b").args(n))

	assert.Equal(t, []string{"iam", "simulate-principal-policy", "--policy-source-arn", role, "--resource-arns", "arn:x", "--context-entries", "c1", "c2", "--action-names", "a"},
		simCtx(allowed, "plan", "arn:x", []string{"c1", "c2"}, "a").args(n), "context entry を1つの --context-entries に並べる")

	r := sim(allowed, "plan", "arn:x", "a")
	r.bucketPolicy, r.doc = true, `{"Statement":[]}`

	assert.Equal(t, []string{"iam", "simulate-principal-policy", "--policy-source-arn", role, "--caller-arn", role, "--resource-arns", "arn:x",
		"--resource-policy", `{"Statement":[]}`, "--resource-owner", testAccount, "--context-entries",
		"ContextKeyName=aws:PrincipalArn,ContextKeyValues=" + role + ",ContextKeyType=string",
		"ContextKeyName=aws:SecureTransport,ContextKeyValues=true,ContextKeyType=boolean", "--action-names", "a"}, r.args(n))
}

func Test_simCheck(t *testing.T) {
	t.Parallel()

	n := testNames("")
	row := sim(allowed, "plan", "arn:x", "s3:A", "s3:B")
	key := jsonKey(row.args(n)...)

	for name, tc := range map[string]struct {
		r    awsResult
		want verdict
	}{
		"すべて want なら ok": {simResult([]string{"s3:A", "s3:B"}, allowed), verdict{ok: true}},
		"順序が違っても ok":     {simResult([]string{"s3:B", "s3:A"}, allowed), verdict{ok: true}},
		"違う判定を並べる":       {simResult([]string{"s3:A", "s3:B"}, implicitDeny), verdict{reason: "違う判定: s3:A=implicitDeny s3:B=implicitDeny"}},
		"件数が足りない":        {simResult([]string{"s3:A"}, allowed), verdict{reason: "返った action が要求と一致しない（返った件数 1 / 2: s3:A）"}},
		"件数が多い":          {simResult([]string{"s3:A", "s3:B", "s3:C"}, allowed), verdict{reason: "返った action が要求と一致しない（返った件数 3 / 2: s3:A s3:B s3:C）"}},
		"重複して欠落":         {simResult([]string{"s3:A", "s3:A"}, allowed), verdict{reason: "返った action が要求と一致しない（返った件数 2 / 2: s3:A s3:A）"}},
		"別名":             {simResult([]string{"s3:A", "s3:b"}, allowed), verdict{reason: "返った action が要求と一致しない（返った件数 2 / 2: s3:A s3:b）"}},
		"0件":             {okResult(`{"EvaluationResults":[]}`), verdict{reason: "返った action が要求と一致しない（返った件数 0 / 2: なし）"}},
		"context を与えない行は、足りない context を判定に使わない": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":["s3:prefix"]},` +
			`{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`), verdict{ok: true}},
		"context を与えない行は、NG の理由に足りない context を添える": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"implicitDeny","MissingContextValues":["s3:prefix"]},` +
			`{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`), verdict{reason: "違う判定: s3:A=implicitDeny、足りない context: s3:prefix"}},
		"action の不一致にも足りない context を添える": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":["aws:x"]}]}`),
			verdict{reason: "返った action が要求と一致しない（返った件数 1 / 2: s3:A）、足りない context: aws:x"}},
		"足りない context が object": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":{"k":"v"}},` +
			`{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`), verdict{reason: "応答を読めない"}},
		"足りない context が文字列": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":"s3:prefix"},` +
			`{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`), verdict{reason: "応答を読めない"}},
		"足りない context が空・null なら ok": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":[]},` +
			`{"EvalActionName":"s3:B","EvalDecision":"allowed","MissingContextValues":null}]}`), verdict{ok: true}},
		"足りない context が文字列でない": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":[1]},` +
			`{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`), verdict{reason: "応答を読めない"}},
		"結果の配列が無い":       {okResult(`{}`), verdict{reason: "応答を読めない"}},
		"結果が object でない": {okResult(`{"EvaluationResults":["x"]}`), verdict{reason: "応答を読めない"}},
		"判定が文字列でない": {okResult(`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":1},{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`),
			verdict{reason: "応答を読めない"}},
		"空の応答":    {okResult(""), verdict{reason: "応答を読めない"}},
		"呼び出しの失敗": {failed("NoSuchEntity"), verdict{reason: "呼び出しが失敗: NoSuchEntity"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{key: tc.r}, nil)

			v, err := simCheck(row, n).eval(newQuery(f.fn, testRegion))

			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
		})
	}

	t.Run("unresolved なら aws を呼ばずに、それを理由とする NG", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, nil, nil)
		r := row
		r.unresolved = "読めない"

		v, err := simCheck(r, n).eval(newQuery(f.fn, testRegion))

		require.NoError(t, err)
		assert.Equal(t, verdict{reason: "読めない"}, v)
		assert.Empty(t, f.calls)
	})

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, err := simCheck(row, n).eval(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion))

		require.ErrorIs(t, err, errAWSStart)
	})

	for name, r := range map[string]simRow{
		"context を与えた行":      simCtx(allowed, "plan", "arn:x", []string{"c"}, "s3:A", "s3:B"),
		"Bucket Policy を含む行": {want: allowed, role: "plan", resource: "arn:x", actions: []string{"s3:A", "s3:B"}, bucketPolicy: true, doc: `{"Statement":[]}`},
	} {
		for cname, tc := range map[string]struct {
			body string
			want verdict
		}{
			"足りない context があれば NG": {`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":["s3:prefix"]},` +
				`{"EvalActionName":"s3:B","EvalDecision":"allowed","MissingContextValues":["aws:x","aws:y"]}]}`, verdict{reason: "評価に足りない context: s3:prefix aws:x aws:y"}},
			"足りない context と違う判定が同時なら、足りない context を先に報告する": {`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"implicitDeny","MissingContextValues":["aws:x"]},` +
				`{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`, verdict{reason: "評価に足りない context: aws:x"}},
			"足りない context が空なら ok": {`{"EvaluationResults":[{"EvalActionName":"s3:A","EvalDecision":"allowed","MissingContextValues":[]},` +
				`{"EvalActionName":"s3:B","EvalDecision":"allowed"}]}`, verdict{ok: true}},
		} {
			t.Run(name+"で、"+cname, func(t *testing.T) {
				t.Parallel()

				f := verifyFake(t, map[string]awsResult{jsonKey(r.args(n)...): okResult(tc.body)}, nil)

				v, err := simCheck(r, n).eval(newQuery(f.fn, testRegion))

				require.NoError(t, err)
				assert.Equal(t, tc.want, v)
			})
		}
	}
}

func Test_attachedNames(t *testing.T) {
	t.Parallel()

	key := jsonKey("iam", "list-attached-role-policies", "--role-name", "r")

	for name, tc := range map[string]struct {
		r          awsResult
		want       []string
		wantReason string
	}{
		"名前を並べ替えて返す": {okResult(`{"AttachedPolicies":[{"PolicyName":"b"},{"PolicyName":"a"}]}`), []string{"a", "b"}, ""},
		"空":          {okResult(noneAttached), []string{}, ""},
		"呼び出しの失敗":    {failed("denied"), nil, "呼び出しが失敗: denied"},
		"配列が無い":      {okResult(`{}`), nil, "応答を読めない"},
		"空の応答":       {okResult(""), nil, "応答を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{key: tc.r}, nil)

			got, reason, err := attachedNames(newQuery(f.fn, testRegion), "r")

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantReason, reason)
		})
	}
}

func Test_governanceGate(t *testing.T) {
	t.Parallel()

	n := testNames("")

	for name, tc := range map[string]struct {
		r     awsResult
		state gateState
	}{
		"統制の policy が付いていれば attached": {okResult(governanceAttached), gateAttached},
		"統制の policy だけでも attached":    {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-governance"}]}`), gateAttached},
		"付いていなければ absent":             {okResult(noneAttached), gateAbsent},
		"manage だけなら absent":          {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-manage"}]}`), gateAbsent},
		"一覧が配列でなければ unknown":          {okResult(`{"AttachedPolicies":{"PolicyName":"tabp-admin-ci-apply-identity-governance"}}`), gateUnknown},
		"空の応答なら unknown":              {okResult(""), gateUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, err := governanceGate(newQuery(verifyFake(t, map[string]awsResult{attachedIdentityKey: tc.r}, nil).fn, testRegion), n)

			require.NoError(t, err)
			assert.Equal(t, tc.state, g.state)
		})
	}

	t.Run("読めなければ unknown で、NG の確認を持つ", func(t *testing.T) {
		t.Parallel()

		g, err := governanceGate(newQuery(verifyFake(t, map[string]awsResult{attachedIdentityKey: failed("denied")}, nil).fn, testRegion), n)

		require.NoError(t, err)
		assert.Equal(t, gateUnknown, g.state)

		v, _ := g.ng.eval(nil)
		assert.Equal(t, "apply-identity: list-attached-role-policies を読めないので、統制の policy が付いているかを決められない", g.ng.desc)
		assert.Equal(t, verdict{reason: "呼び出しが失敗: denied"}, v)
	})

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, err := governanceGate(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion), n)

		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_workloadGate(t *testing.T) {
	t.Parallel()

	n := testNames("")

	for name, tc := range map[string]struct {
		r     awsResult
		state gateState
	}{
		"2つちょうどなら attached": {okResult(workloadAttached), gateAttached},
		"付いていなければ absent":   {okResult(noneAttached), gateAbsent},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, err := workloadGate(newQuery(verifyFake(t, map[string]awsResult{attachedApplyKey: tc.r}, nil).fn, testRegion), n)

			require.NoError(t, err)
			assert.Equal(t, tc.state, g.state)
		})
	}

	for name, tc := range map[string]struct {
		r          awsResult
		wantDesc   string
		wantReason string
	}{
		"読めなければ unknown":    {failed("denied"), "apply: list-attached-role-policies を読めないので、workload の2つの policy が付いているかを決められない", "呼び出しが失敗: denied"},
		"応答が読めなければ unknown": {okResult(`{"AttachedPolicies":"x"}`), "apply: list-attached-role-policies を読めないので、workload の2つの policy が付いているかを決められない", "応答を読めない"},
		"1つだけなら unknown": {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload"}]}`),
			"apply の role に付く managed policy が workload の2つの policy ではない", "tabp-admin-ci-apply-workload"},
		"余分があれば unknown": {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload"},{"PolicyName":"x"},{"PolicyName":"tabp-admin-ci-apply-workload-read"}]}`),
			"apply の role に付く managed policy が workload の2つの policy ではない", "tabp-admin-ci-apply-workload、tabp-admin-ci-apply-workload-read、x"},
	} {
		t.Run(name+"で、NG の確認を持つ", func(t *testing.T) {
			t.Parallel()

			g, err := workloadGate(newQuery(verifyFake(t, map[string]awsResult{attachedApplyKey: tc.r}, nil).fn, testRegion), n)

			require.NoError(t, err)
			assert.Equal(t, gateUnknown, g.state)

			v, _ := g.ng.eval(nil)
			assert.Equal(t, tc.wantDesc, g.ng.desc)
			assert.Equal(t, verdict{reason: tc.wantReason}, v)
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, err := workloadGate(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion), n)

		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_permissionSections(t *testing.T) {
	t.Parallel()

	n := testNames("")
	sections := permissionSections(n, testKeyARN, "")

	t.Run("どの行も action を1つ以上持ち、判定・role・resource が決まっている", func(t *testing.T) {
		t.Parallel()

		rows := allRows(n, testKeyARN)

		require.Len(t, rows, permissionsBase+governanceGated+workloadGatedCount+budgetsGatedCount)

		for _, r := range rows {
			assert.NotEmpty(t, r.actions, r.desc())
			assert.Contains(t, []string{allowed, implicitDeny, explicitDeny}, r.want, r.desc())
			assert.Contains(t, n.ciRoles(), r.role, r.desc())
			assert.NotEmpty(t, r.resource, r.desc())
		}
	})

	t.Run("ダミーの鍵の ARN は、アカウント ID の伏せ字の後も2種類を区別できる", func(t *testing.T) {
		t.Parallel()

		var masked []string

		for _, r := range allRows(n, testKeyARN) {
			if strings.Contains(r.resource, ":key/00000000-") {
				masked = append(masked, maskAllAccounts(r.resource))
			}
		}

		a0 := "arn:aws:kms:ap-northeast-1:<account>:key/00000000-0000-0000-0000-0000000000a0"
		a1 := "arn:aws:kms:ap-northeast-1:<account>:key/00000000-0000-0000-0000-0000000000a1"

		require.Len(t, masked, 5)
		assert.Equal(t, []string{a0, a0, a1, a1, a1}, masked)
	})

	t.Run("各行の role・resource・判定・action の数が、表と独立に書いた desc の一覧と一致する", func(t *testing.T) {
		t.Parallel()

		var descs []string
		for _, l := range [][]string{
			permS3Descs, permKeyDescs, permGovernanceDescs, permGovernanceGatedDescs, permSLRDescs, permWorkloadDescs, permWorkloadGatedDescs,
			permBudgetsDescs, permBudgetsGatedDescs, permUnlockIdentityDescs, permUnlockDescs,
		} {
			descs = append(descs, expand(l, "")...)
		}

		rows := allRows(n, testKeyARN)

		require.Len(t, descs, len(rows))

		for i, r := range rows {
			head, tail, ok := strings.Cut(descs[i], ": ")
			require.True(t, ok, descs[i])

			role, resource, ok := strings.Cut(head, " ")
			require.True(t, ok, descs[i])

			count, want, ok := strings.Cut(tail, " 件が ")
			require.True(t, ok, descs[i])

			assert.Equal(t, role, r.role, "%d 行目", i)
			assert.Equal(t, resource, r.resource, "%d 行目", i)
			assert.Equal(t, want, r.want, "%d 行目", i)
			assert.Equal(t, count, strconv.Itoa(len(r.actions)), "%d 行目", i)
		}
	})

	t.Run("各行の action の並びと context が、表と独立に書いた一覧と一致する", func(t *testing.T) {
		t.Parallel()

		rows := allRows(n, testKeyARN)

		require.Len(t, permRowActions, len(rows))

		for i, r := range rows {
			actions, context := parseRowActions(t, permRowActions[i])

			assert.Equal(t, actions, r.actions, "%d 行目 %s", i, r.desc())
			assert.Equal(t, context, r.context, "%d 行目 %s", i, r.desc())
		}
	})

	t.Run("条件つきの区分は、行と注記を持つ", func(t *testing.T) {
		t.Parallel()

		for _, s := range sections {
			if s.gate == gateNone {
				assert.Empty(t, s.gated, s.heading)

				continue
			}

			assert.NotEmpty(t, s.gated, s.heading)
			assert.NotEmpty(t, s.absentNote, s.heading)
			assert.True(t, s.reportGate || s.unknownNote != "", s.heading)
		}
	})

	t.Run("state の鍵を読めなければ、鍵の区分の先頭にその NG を置く", func(t *testing.T) {
		t.Parallel()

		got := permissionSections(n, "arn:unknown", "呼び出しが失敗: x")

		require.Len(t, got[1].lead, 1)
		assert.Equal(t, "state の鍵の ARN（alias/tabp-admin-tfstate）を読めない", got[1].lead[0].check.desc)
		assert.Empty(t, sections[1].lead)
	})

	t.Run("region が us-west-2 なら、及ばない region に eu-west-1 を使う", func(t *testing.T) {
		t.Parallel()

		w := newNames(verifyOptions{project: testProject, region: "us-west-2"}, testAccount)

		assert.True(t, slices.ContainsFunc(permissionSections(w, testKeyARN, "")[4].rows, func(r simRow) bool {
			return r.want == implicitDeny && r.resource == "arn:aws:sns:eu-west-1:123456789012:tabp-admin-ops-notification"
		}))
	})
}

func Test_permissionsSteps(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, f *fakeAWS) ([]string, error) {
		t.Helper()

		q := newQuery(f.fn, testRegion)

		steps, err := permissionsSteps(q, testNames(""))
		require.NoError(t, err)

		return runStepLines(t, f, steps)
	}

	for name, tc := range map[string]struct {
		identity, apply             string
		governance, workload, budgt []string
		count                       int
		result                      string
	}{
		"条件つきの確認がすべて走る": {governanceAttached, workloadAttached, nil, nil, nil, permissionsBase + governanceGated + workloadGatedCount + budgetsGatedCount,
			"# 結果: 126 件のうち NG 0 件"},
		"統制の policy が付いていない": {noneAttached, workloadAttached, []string{governanceAbsent}, nil, nil, permissionsBase + workloadGatedCount + budgetsGatedCount,
			"# 結果: 122 件のうち NG 0 件（確かめていない区分 1）"},
		"workload の policy が付いていない": {governanceAttached, noneAttached, nil, []string{workloadAbsent}, []string{budgetsAbsent},
			permissionsBase + governanceGated, "# 結果: 113 件のうち NG 0 件（確かめていない区分 2）"},
		"どちらも付いていない": {noneAttached, noneAttached, []string{governanceAbsent}, []string{workloadAbsent}, []string{budgetsAbsent}, permissionsBase,
			"# 結果: 109 件のうち NG 0 件（確かめていない区分 3）"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			lines, err := run(t, permFake(t, okResult(tc.identity), okResult(tc.apply)))

			require.NoError(t, err)
			assert.Equal(t, append(permExpected(tc.governance, tc.workload, tc.budgt), tc.result), lines)
			assert.Contains(t, tc.result, "# 結果: "+strconv.Itoa(tc.count)+" 件", "件数は表と独立の定数から")
		})
	}

	t.Run("managed policy を読めなければ、条件つきの確認を飛ばさずに NG として数える", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, failed("denied"), failed("denied")))

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, append(permExpected(
			[]string{"NG  apply-identity: list-attached-role-policies を読めないので、統制の policy が付いているかを決められない（呼び出しが失敗: denied）"},
			[]string{"NG  apply: list-attached-role-policies を読めないので、workload の2つの policy が付いているかを決められない（呼び出しが失敗: denied）"},
			[]string{budgetsUnknown},
		), "# 結果: "+strconv.Itoa(permissionsBase+2)+" 件のうち NG 2 件（確かめていない区分 1）"), lines)
	})

	t.Run("統制の policy を読めなければ NG 1 件で、確かめていない区分には数えない", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, failed("denied"), okResult(workloadAttached)))

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+workloadGatedCount+budgetsGatedCount+1)+" 件のうち NG 1 件", lines[len(lines)-1])
	})

	t.Run("workload の policy を読めなければ NG 1 件で、budgets の注記だけを確かめていない区分に数える", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, okResult(governanceAttached), failed("denied")))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, budgetsUnknown)
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+governanceGated+1)+" 件のうち NG 1 件（確かめていない区分 1）", lines[len(lines)-1])
	})

	t.Run("apply の managed policy が想定と違えば、名前を添えて NG", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, okResult(governanceAttached), okResult(`{"AttachedPolicies":[{"PolicyName":"x"}]}`)))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  apply の role に付く managed policy が workload の2つの policy ではない（x）")
		assert.Contains(t, lines, budgetsUnknown)
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+governanceGated+1)+" 件のうち NG 1 件（確かめていない区分 1）", lines[len(lines)-1])
	})

	t.Run("state の鍵の ARN を読めなければ NG を数え、その鍵の行は aws を呼ばずに NG", func(t *testing.T) {
		t.Parallel()

		f := permFake(t, okResult(governanceAttached), okResult(workloadAttached))
		f.responses[jsonKey("kms", "describe-key", "--key-id", testAlias)] = failed("NotFoundException")

		lines, err := run(t, f)

		require.ErrorIs(t, err, errMismatch)

		i := slices.Index(lines, "# state の鍵")
		require.GreaterOrEqual(t, i, 0)
		assert.Equal(t, "NG  state の鍵の ARN（alias/tabp-admin-tfstate）を読めない（呼び出しが失敗: NotFoundException）", lines[i+1])
		assert.Equal(t, "NG  apply-identity arn:aws:kms:ap-northeast-1:123456789012:key/unknown: 6 件が allowed（state の鍵の ARN を読めない）", lines[i+2])
		// 鍵を読めない NG 1 と、鍵を使う行（鍵の区分 6、unlock-identity と unlock の各 1）。
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+governanceGated+workloadGatedCount+budgetsGatedCount+1)+" 件のうち NG 9 件", lines[len(lines)-1])

		for _, c := range f.calls {
			assert.NotContains(t, c, "key/unknown")
		}
	})

	t.Run("表のどの行も、want と違う判定が返れば NG", func(t *testing.T) {
		t.Parallel()

		n := testNames("")

		for _, r := range allRows(n, testKeyARN) {
			f := &fakeAWS{t: t, handler: func(args []string) (awsResult, bool) { return simResult(simActions(args), "x"), true }}

			v, err := simCheck(r, n).eval(newQuery(f.fn, testRegion))

			require.NoError(t, err)
			assert.False(t, v.ok, r.desc())
			assert.Contains(t, v.reason, "違う判定: "+r.actions[0]+"=x", r.desc())
		}
	})

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		for _, failing := range []string{"describe-key", "--role-name tabp-admin-ci-apply-identity ", "--role-name tabp-admin-ci-apply "} {
			f := permFake(t, okResult(governanceAttached), okResult(workloadAttached))
			q := newQuery(func(args ...string) awsResult {
				if strings.Contains(strings.Join(args, " "), failing) {
					return awsResult{err: os.ErrNotExist}
				}

				return f.fn(args...)
			}, testRegion)

			_, err := permissionsSteps(q, testNames(""))

			require.ErrorIs(t, err, errAWSStart, failing)
		}
	})
}

// referencePlanJSON は、reference の plan の、2つの出力の Bucket の Bucket Policy を持つもの。
func referencePlanJSON(t *testing.T) string {
	t.Helper()

	b, err := json.Marshal(map[string]any{"resource_changes": []any{
		map[string]any{"address": "module.state_backend.module.plan_output.aws_s3_bucket_policy.this",
			"change": map[string]any{"after": map[string]any{"policy": `{"Statement":[{"Effect":"Deny","Principal":"arn:aws:iam::123456789012:root"}]}`}}},
		map[string]any{"address": "module.state_backend.module.apply_output.aws_s3_bucket_policy.this",
			"change": map[string]any{"after": map[string]any{"policy": `{"Statement":[{"Sid":"apply"}]}`}}},
		map[string]any{"address": "module.other", "change": map[string]any{"after": nil}},
	}})
	require.NoError(t, err)

	return string(b)
}

func Test_referencePolicy(t *testing.T) {
	t.Parallel()

	read := func(s string) func(string) ([]byte, error) {
		return func(p string) ([]byte, error) {
			if p != referencePlan {
				return nil, os.ErrNotExist
			}

			return []byte(s), nil
		}
	}

	t.Run("module の文書を読み、アカウント ID を置き換える", func(t *testing.T) {
		t.Parallel()

		doc, reason := referencePolicy(read(referencePlanJSON(t)), "plan_output", "999999999999")

		assert.Empty(t, reason)
		assert.Equal(t, `{"Statement":[{"Effect":"Deny","Principal":"arn:aws:iam::999999999999:root"}]}`, doc)
	})

	dup := strings.Replace(referencePlanJSON(t), `{"address":"module.other"`,
		`{"address":"module.state_backend.module.apply_output.aws_s3_bucket_policy.this","change":{"after":{"policy":"{}"}}},{"address":"module.other"`, 1)

	for name, tc := range map[string]struct {
		content string
		module  string
		want    string
	}{
		"JSON でない":             {"x", "plan_output", referencePlan + " を JSON の object として読めない"},
		"null":                 {"null", "plan_output", referencePlan + " を JSON の object として読めない"},
		"resource_changes が無い": {`{}`, "plan_output", referencePlan + " に resource_changes の配列が無い"},
		"address が無い": {referencePlanJSON(t), "none",
			referencePlan + " に module.state_backend.module.none.aws_s3_bucket_policy.this の policy が1つではない（0 個）"},
		"address が2つ": {dup, "apply_output",
			referencePlan + " に module.state_backend.module.apply_output.aws_s3_bucket_policy.this の policy が1つではない（2 個）"},
		"policy が null": {`{"resource_changes":[{"address":"module.state_backend.module.plan_output.aws_s3_bucket_policy.this","change":{"after":{"policy":null}}}]}`, "plan_output",
			referencePlan + " に module.state_backend.module.plan_output.aws_s3_bucket_policy.this の policy が1つではない（1 個）"},
	} {
		t.Run(name+"なら理由を返す", func(t *testing.T) {
			t.Parallel()

			doc, reason := referencePolicy(read(tc.content), tc.module, testAccount)

			assert.Empty(t, doc)
			assert.Equal(t, tc.want, reason)
		})
	}

	t.Run("読めなければ理由を返す", func(t *testing.T) {
		t.Parallel()

		_, reason := referencePolicy(read(""), "plan_output", testAccount)
		assert.Equal(t, referencePlan+" を JSON の object として読めない", reason)

		_, reason = referencePolicy(func(string) ([]byte, error) { return nil, os.ErrNotExist }, "plan_output", testAccount)
		assert.Equal(t, referencePlan+" を読めない: "+os.ErrNotExist.Error(), reason)
	})
}

func Test_outputDocument(t *testing.T) {
	t.Parallel()

	env := func(t *testing.T, source string, r awsResult) verifyEnv {
		t.Helper()

		f := verifyFake(t, map[string]awsResult{s3Key("get-bucket-policy", testPlanBucket): r}, nil)

		return verifyEnv{q: newQuery(f.fn, testRegion), n: testNames(""), source: source,
			readFile: func(string) ([]byte, error) { return []byte(referencePlanJSON(t)), nil }}
	}

	for name, tc := range map[string]struct {
		source     string
		r          awsResult
		want       string
		wantReason string
	}{
		"live は Bucket に付いている文書":   {sourceLive, okResult(`{"Policy":"{\"a\":1}"}`), `{"a":1}`, ""},
		"live の呼び出しの失敗":            {sourceLive, failed("NoSuchBucketPolicy"), "", "呼び出しが失敗: NoSuchBucketPolicy"},
		"live の Policy が無い":        {sourceLive, okResult(`{}`), "", "文書を JSON の object として読めない"},
		"live の Policy が JSON でない": {sourceLive, okResult(`{"Policy":"[1]"}`), "", "文書を JSON の object として読めない"},
		"reference は plan の文書":     {sourceReference, okResult(""), `{"Statement":[{"Effect":"Deny","Principal":"arn:aws:iam::123456789012:root"}]}`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			doc, reason, err := outputDocument(env(t, tc.source, tc.r), testPlanBucket, "plan_output")

			require.NoError(t, err)
			assert.Equal(t, tc.want, doc)
			assert.Equal(t, tc.wantReason, reason)
		})
	}

	t.Run("live で aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		e := env(t, sourceLive, okResult(""))
		e.q = newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion)

		_, _, err := outputDocument(e, testPlanBucket, "plan_output")

		require.ErrorIs(t, err, errAWSStart)
	})

	for _, policy := range []string{"x", "[1]"} {
		t.Run("reference の policy が "+policy+" なら読めない理由を返す", func(t *testing.T) {
			t.Parallel()

			b, err := json.Marshal(map[string]any{"resource_changes": []any{map[string]any{
				"address": "module.state_backend.module.plan_output.aws_s3_bucket_policy.this",
				"change":  map[string]any{"after": map[string]any{"policy": policy}},
			}}})
			require.NoError(t, err)

			e := env(t, sourceReference, okResult(""))
			e.readFile = func(string) ([]byte, error) { return b, nil }

			doc, reason, err := outputDocument(e, testPlanBucket, "plan_output")

			require.NoError(t, err)
			assert.Empty(t, doc)
			assert.Equal(t, "文書を JSON の object として読めない", reason)
		})
	}
}

func Test_outputPolicySteps(t *testing.T) {
	t.Parallel()

	n := testNames("")
	pdoc := `{"Statement":[{"Sid":"plan"}]}`
	adoc := `{"Statement":[{"Sid":"apply"}]}`

	handlerFor := func(nn names, docs map[string]string) func(args []string) (awsResult, bool) {
		wants := map[string]string{}
		planRows, applyRows := outputPolicyRows(nn)

		for doc, rows := range map[string][]simRow{docs["plan"]: planRows, docs["apply"]: applyRows} {
			for _, r := range rows {
				r.bucketPolicy, r.doc = true, doc
				wants[strings.Join(r.args(nn), " ")] = r.want
			}
		}

		return func(args []string) (awsResult, bool) {
			want, ok := wants[strings.Join(args[:len(args)-4], " ")]
			if !ok {
				return awsResult{}, false
			}

			return simResult(simActions(args), want), true
		}
	}
	handler := func(docs map[string]string) func(args []string) (awsResult, bool) { return handlerFor(n, docs) }
	expected := func(source string, readable []string) []string {
		out := append([]string{"# 文書の出所: " + source}, readable...)
		out = append(append(out, "# plan の出力の Bucket"), expand(outputPolicyPlanDescs, "ok  ")...)
		out = append(append(out, "# apply の出力の Bucket"), expand(outputPolicyApplyDescs, "ok  ")...)

		return append(out, "# 結果: 13 件のうち NG 0 件")
	}
	readableOK := []string{"ok  plan の出力の Bucket の Bucket Policy の文書を読めた", "ok  apply の出力の Bucket の Bucket Policy の文書を読めた"}

	run := func(t *testing.T, e verifyEnv, f *fakeAWS) ([]string, error) {
		t.Helper()

		steps, err := outputPolicySteps(e)
		require.NoError(t, err)

		return runStepLines(t, f, steps)
	}

	t.Run("live の文書を含めて評価する", func(t *testing.T) {
		t.Parallel()

		f := &fakeAWS{t: t, responses: map[string]awsResult{
			s3Key("get-bucket-policy", testPlanBucket):  policyResult(t, pdoc),
			s3Key("get-bucket-policy", testApplyBucket): policyResult(t, adoc),
		}, handler: handler(map[string]string{"plan": pdoc, "apply": adoc})}

		lines, err := run(t, verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceLive}, f)

		require.NoError(t, err)
		assert.Equal(t, expected("live", readableOK), lines)
	})

	t.Run("reference の文書を、アカウント ID を置き換えて評価する", func(t *testing.T) {
		t.Parallel()

		f := &fakeAWS{t: t, handler: handler(map[string]string{
			"plan":  `{"Statement":[{"Effect":"Deny","Principal":"arn:aws:iam::123456789012:root"}]}`,
			"apply": `{"Statement":[{"Sid":"apply"}]}`,
		})}
		e := verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceReference,
			readFile: func(string) ([]byte, error) { return []byte(referencePlanJSON(t)), nil }}

		lines, err := run(t, e, f)

		require.NoError(t, err)
		assert.Equal(t, expected("reference", readableOK), lines)
	})

	t.Run("文書を読めなければ、その Bucket の行は aws を呼ばずにすべて NG", func(t *testing.T) {
		t.Parallel()

		f := &fakeAWS{t: t, responses: map[string]awsResult{
			s3Key("get-bucket-policy", testPlanBucket):  awsError("NoSuchBucketPolicy"),
			s3Key("get-bucket-policy", testApplyBucket): policyResult(t, adoc),
		}, handler: handler(map[string]string{"apply": adoc})}

		lines, err := run(t, verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceLive}, f)

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  plan の出力の Bucket の Bucket Policy の文書を読めた（呼び出しが失敗: An error occurred (NoSuchBucketPolicy) when calling the X operation: detail）")

		for _, d := range expand(outputPolicyPlanDescs, "NG  ") {
			assert.Contains(t, lines, d+"（Bucket Policy の文書を読めない）")
		}

		assert.Equal(t, "# 結果: 13 件のうち NG 9 件", lines[len(lines)-1])
	})

	t.Run("reference の文書は、実際のアカウント ID に置き換えて評価する", func(t *testing.T) {
		t.Parallel()

		other := newNames(verifyOptions{project: testProject, region: testRegion}, "210987654321")
		f := &fakeAWS{t: t, handler: handlerFor(other, map[string]string{
			"plan":  `{"Statement":[{"Effect":"Deny","Principal":"arn:aws:iam::210987654321:root"}]}`,
			"apply": `{"Statement":[{"Sid":"apply"}]}`,
		})}
		e := verifyEnv{q: newQuery(f.fn, testRegion), n: other, source: sourceReference,
			readFile: func(string) ([]byte, error) { return []byte(referencePlanJSON(t)), nil }}

		lines, err := run(t, e, f)

		require.NoError(t, err)
		assert.Equal(t, "# 結果: 13 件のうち NG 0 件", lines[len(lines)-1])
		assert.Len(t, f.calls, 11)

		for _, c := range f.calls {
			assert.NotContains(t, c, testAccount)
		}
	})

	t.Run("apply の文書だけ読めなければ、apply の側だけ NG", func(t *testing.T) {
		t.Parallel()

		f := &fakeAWS{t: t, responses: map[string]awsResult{
			s3Key("get-bucket-policy", testPlanBucket):  policyResult(t, pdoc),
			s3Key("get-bucket-policy", testApplyBucket): failed("denied"),
		}, handler: handler(map[string]string{"plan": pdoc})}

		lines, err := run(t, verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceLive}, f)

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "ok  plan の出力の Bucket の Bucket Policy の文書を読めた", lines[1])
		assert.Equal(t, "NG  apply の出力の Bucket の Bucket Policy の文書を読めた（呼び出しが失敗: denied）", lines[2])
		assert.Equal(t, "# 結果: 13 件のうち NG 4 件", lines[len(lines)-1])
	})

	t.Run("Policy が JSON でなければ、読めた確認とその Bucket の行が NG", func(t *testing.T) {
		t.Parallel()

		f := &fakeAWS{t: t, responses: map[string]awsResult{
			s3Key("get-bucket-policy", testPlanBucket):  okResult(`{"Policy":"x"}`),
			s3Key("get-bucket-policy", testApplyBucket): policyResult(t, adoc),
		}, handler: handler(map[string]string{"apply": adoc})}

		lines, err := run(t, verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceLive}, f)

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "NG  plan の出力の Bucket の Bucket Policy の文書を読めた（文書を JSON の object として読めない）", lines[1])
		assert.Equal(t, "# 結果: 13 件のうち NG 9 件", lines[len(lines)-1])
	})

	for name, tc := range map[string]struct {
		plan   func(t *testing.T) ([]byte, error)
		result string
	}{
		"reference の plan が無い": {func(*testing.T) ([]byte, error) { return nil, os.ErrNotExist }, "# 結果: 13 件のうち NG 13 件"},
		"reference の plan に apply_output だけ無い": {func(t *testing.T) ([]byte, error) {
			return []byte(strings.Replace(referencePlanJSON(t), "module.state_backend.module.apply_output", "module.x", 1)), nil
		}, "# 結果: 13 件のうち NG 4 件"},
	} {
		t.Run(name+"なら、読めない側が NG", func(t *testing.T) {
			t.Parallel()

			f := &fakeAWS{t: t, handler: handler(map[string]string{
				"plan": `{"Statement":[{"Effect":"Deny","Principal":"arn:aws:iam::123456789012:root"}]}`,
			})}
			e := verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceReference, readFile: func(string) ([]byte, error) { return tc.plan(t) }}

			lines, err := run(t, e, f)

			require.ErrorIs(t, err, errMismatch)
			assert.Equal(t, tc.result, lines[len(lines)-1])
		})
	}

	t.Run("文書の Statement が1文の object なら読めた", func(t *testing.T) {
		t.Parallel()

		doc := `{"Statement":{"Effect":"Deny"}}`
		f := &fakeAWS{t: t, responses: map[string]awsResult{
			s3Key("get-bucket-policy", testPlanBucket):  policyResult(t, doc),
			s3Key("get-bucket-policy", testApplyBucket): policyResult(t, adoc),
		}, handler: handler(map[string]string{"plan": doc, "apply": adoc})}

		lines, err := run(t, verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceLive}, f)

		require.NoError(t, err)
		assert.Equal(t, "ok  plan の出力の Bucket の Bucket Policy の文書を読めた", lines[1])
	})

	for name, doc := range map[string]string{
		"文が無い":           `{"Statement":[]}`,
		"1文の object が空":  `{"Statement":{}}`,
		"Statement が無い":  `{}`,
		"Statement が真偽値": `{"Statement":true}`,
	} {
		t.Run("文書の"+name+"なら、読めた確認が NG", func(t *testing.T) {
			t.Parallel()

			f := &fakeAWS{t: t, responses: map[string]awsResult{
				s3Key("get-bucket-policy", testPlanBucket):  policyResult(t, doc),
				s3Key("get-bucket-policy", testApplyBucket): policyResult(t, adoc),
			}, handler: handler(map[string]string{"plan": doc, "apply": adoc})}

			lines, err := run(t, verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceLive}, f)

			require.ErrorIs(t, err, errMismatch)
			assert.Equal(t, "NG  plan の出力の Bucket の Bucket Policy の文書を読めた", lines[1])
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		for _, bucket := range []string{testPlanBucket, testApplyBucket} {
			f := &fakeAWS{t: t, responses: map[string]awsResult{
				s3Key("get-bucket-policy", testPlanBucket):  policyResult(t, pdoc),
				s3Key("get-bucket-policy", testApplyBucket): policyResult(t, adoc),
			}}
			q := newQuery(func(args ...string) awsResult {
				if slices.Contains(args, bucket) {
					return awsResult{err: os.ErrNotExist}
				}

				return f.fn(args...)
			}, testRegion)

			_, err := outputPolicySteps(verifyEnv{q: q, n: n, source: sourceLive})

			require.ErrorIs(t, err, errAWSStart, bucket)
		}
	})
}

func Test_outputPolicyRows(t *testing.T) {
	t.Parallel()

	planRows, applyRows := outputPolicyRows(testNames(""))

	require.Len(t, planRows, len(outputPolicyPlanDescs))
	require.Len(t, applyRows, len(outputPolicyApplyDescs))
	assert.Equal(t, outputPolicyCount, len(planRows)+len(applyRows)+2, "文書を読めた確認の2件を足して13件")

	for _, r := range append(planRows, applyRows...) {
		assert.NotEmpty(t, r.actions, r.desc())
		assert.Contains(t, []string{allowed, explicitDeny}, r.want, r.desc())
		assert.Empty(t, r.context, "Bucket Policy の行の context は args が組む")
	}
}
