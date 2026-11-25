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
		"apply-identity * [kms:BypassPolicyLockoutSafetyCheck=true]: 1 件が explicitDeny",
	}
	permGovernanceDescs = []string{
		"apply-identity $policy/apply-identity-governance: 7 件が allowed",
		"apply-identity $iam:policy/other: 3 件が implicitDeny",
		"apply-identity $role/apply-identity [iam:PolicyARN=$policy/apply-identity-governance]: 1 件が allowed",
		"apply-identity $role/apply-identity [iam:PolicyARN=arn:aws:iam::aws:policy/AdministratorAccess]: 1 件が implicitDeny",
		"apply-identity $policy/apply-identity-manage: 7 件が allowed",
		"apply-identity $role/apply-identity [iam:PolicyARN=$policy/apply-identity-manage]: 1 件が allowed",
		"apply-identity $role/apply [iam:PolicyARN=$policy/apply-identity-manage]: 1 件が implicitDeny",
		"plan $policy/apply-identity-manage: 2 件が allowed",
		"apply $policy/apply-identity-manage: 1 件が explicitDeny",
		"plan *: 5 件が allowed",
		"plan $policy/apply-identity-governance: 2 件が allowed",
		"plan *: 2 件が implicitDeny",
		"apply *: 11 件が explicitDeny",
		"apply *: 3 件が implicitDeny",
		"apply $bg: 2 件が explicitDeny",
		"apply $policy/apply-identity-governance: 1 件が explicitDeny",
		"apply $ss: 3 件が explicitDeny",
	}
	permGovernanceGatedDescs = []string{
		"apply-identity *: 3 件が allowed",
		"apply-identity $bg [iam:PolicyARN=arn:aws:iam::aws:policy/AdministratorAccess]: 1 件が allowed",
		"apply-identity *: 1 件が implicitDeny",
		"apply-identity $bg: 1 件が implicitDeny",
		"apply-identity $ss: 2 件が allowed",
		"apply-identity $st: 2 件が allowed",
		"apply-identity $ty [cloudformation:TargetRegion=ap-northeast-1]: 2 件が allowed",
		"apply-identity $ty [cloudformation:TargetRegion=ap-northeast-1,ap-northeast-1]: 2 件が allowed",
		"apply-identity $ty [cloudformation:TargetRegion=ap-northeast-1,us-west-2]: 2 件が implicitDeny",
		"apply-identity $ty [cloudformation:TargetRegion=us-west-2]: 2 件が implicitDeny",
		"apply-identity $ty: 2 件が implicitDeny",
		"apply-identity $ss: 1 件が allowed",
		"apply-identity $st: 1 件が allowed",
		"apply-identity $ty: 1 件が allowed",
		"apply-identity $ss: 1 件が implicitDeny",
		"apply-identity $xss: 2 件が implicitDeny",
		"apply-identity $xst: 2 件が implicitDeny",
		"apply-identity $yss: 2 件が implicitDeny",
		"apply-identity $yst: 2 件が implicitDeny",
		"apply-identity $xss: 1 件が implicitDeny",
		"apply-identity $xst: 1 件が implicitDeny",
		"apply-identity $yss: 1 件が implicitDeny",
		"apply-identity $yst: 1 件が implicitDeny",
	}
	permDelegatedDescs = []string{
		"apply-identity $policy/apply-identity-governance-2: 7 件が allowed",
		"apply-identity $role/apply-identity [iam:PolicyARN=$policy/apply-identity-governance-2]: 1 件が allowed",
		"plan *: 1 件が allowed",
		"plan * [aws:RequestedRegion=ap-northeast-1]: 2 件が allowed",
		"plan * [aws:RequestedRegion=af-south-1]: 2 件が explicitDeny",
		"plan arn:aws:securityhub:ap-northeast-1:123456789012:hub/default [aws:RequestedRegion=ap-northeast-1]: 1 件が allowed",
		"plan arn:aws:securityhub:af-south-1:123456789012:hub/default [aws:RequestedRegion=af-south-1]: 1 件が explicitDeny",
		"plan * [aws:RequestedRegion=ap-northeast-1]: 2 件が implicitDeny",
		"apply arn:aws:organizations::123456789012:account/o-exampleorg1/111111111111: 2 件が explicitDeny",
		"apply $policy/apply-identity-governance-2: 1 件が explicitDeny",
	}
	permDelegatedGatedDescs = []string{
		"apply-identity arn:aws:organizations::123456789012:account/o-exampleorg1/111111111111 [organizations:ServicePrincipal=guardduty.amazonaws.com]: 2 件が allowed",
		"apply-identity arn:aws:organizations::123456789012:account/o-exampleorg1/111111111111 [organizations:ServicePrincipal=config.amazonaws.com]: 1 件が allowed",
		"apply-identity arn:aws:organizations::123456789012:account/o-exampleorg1/111111111111 [organizations:ServicePrincipal=sso.amazonaws.com]: 1 件が implicitDeny",
		"apply-identity * [organizations:ServicePrincipal=inspector2.amazonaws.com]: 1 件が allowed",
		"apply-identity * [organizations:ServicePrincipal=config.amazonaws.com]: 2 件が implicitDeny",
		"apply-identity * [aws:RequestedRegion=ap-northeast-1]: 4 件が allowed",
		"apply-identity * [aws:RequestedRegion=af-south-1]: 4 件が explicitDeny",
		"apply-identity arn:aws:securityhub:ap-northeast-1:123456789012:hub/default [aws:RequestedRegion=ap-northeast-1]: 2 件が allowed",
		"apply-identity arn:aws:securityhub:af-south-1:123456789012:hub/default [aws:RequestedRegion=af-south-1]: 1 件が explicitDeny",
		"apply-identity * [organizations:ServicePrincipal=securityhub.amazonaws.com]: 1 件が allowed",
		"apply-identity *: 2 件が allowed",
		"apply-identity $iam:role/aws-service-role/cloudtrail.amazonaws.com/AWSServiceRoleForCloudTrail [iam:AWSServiceName=cloudtrail.amazonaws.com]: 1 件が allowed",
		"apply-identity $iam:role/aws-service-role/cloudtrail.amazonaws.com/AWSServiceRoleForCloudTrail [iam:AWSServiceName=guardduty.amazonaws.com]: 1 件が implicitDeny",
	}
	permSLRDescs = []string{
		"apply-identity $slr [iam:AWSServiceName=management.chatbot.amazonaws.com]: 1 件が allowed",
		"apply-identity $slr [iam:AWSServiceName=config.amazonaws.com]: 1 件が implicitDeny",
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
		"apply-identity $role/apply [iam:PolicyARN=$policy/apply-workload]: 1 件が allowed",
		"apply-identity $role/apply [iam:PolicyARN=$policy/apply-workload-read]: 1 件が allowed",
		"apply-identity $role/apply [iam:PolicyARN=$policy/apply-workload-2]: 1 件が allowed",
		"apply-identity $role/apply [iam:PolicyARN=$policy/boundary-ops-notification]: 1 件が implicitDeny",
		"apply-identity $role/plan [iam:PolicyARN=$policy/apply-workload-read]: 1 件が allowed",
		"apply-identity $role/plan [iam:PolicyARN=$policy/apply-workload]: 1 件が implicitDeny",
		"apply-identity $role/plan [iam:PolicyARN=$policy/apply-workload-2]: 1 件が implicitDeny",
		"plan $policy/apply-workload: 2 件が allowed",
		"plan $policy/apply-workload-read: 2 件が allowed",
		"plan arn:aws:sns:us-east-1:123456789012:tabp-admin-ops-notification-us-east-1: 2 件が allowed",
		"plan arn:aws:sns:us-west-2:123456789012:tabp-admin-ops-notification: 1 件が implicitDeny",
		"plan $topic: 2 件が allowed",
		"plan $topic: 1 件が implicitDeny",
		"plan $wk [kms:KeySpec=SYMMETRIC_DEFAULT; kms:KeyUsage=ENCRYPT_DECRYPT; kms:KeyOrigin=AWS_KMS; kms:MultiRegion=false]: 1 件が allowed",
		"plan $wk [kms:KeySpec=RSA_2048; kms:KeyUsage=SIGN_VERIFY; kms:KeyOrigin=AWS_KMS; kms:MultiRegion=false]: 1 件が implicitDeny",
		"apply $policy/apply-workload: 2 件が explicitDeny",
		"apply $policy/apply-workload-read: 2 件が explicitDeny",
		"apply $policy/boundary-ops-notification: 2 件が explicitDeny",
		"apply $role/other: 2 件が explicitDeny",
		"apply *: 1 件が explicitDeny",
		"apply * [kms:BypassPolicyLockoutSafetyCheck=true]: 2 件が explicitDeny",
	}
	permWorkloadGatedDescs = []string{
		"apply $iam:role/tabp-admin-ops-heartbeat [iam:PermissionsBoundary=$policy/boundary-ops-notification]: 3 件が allowed",
		"apply $iam:role/tabp-admin-ops-heartbeat: 2 件が implicitDeny",
		"apply $iam:role/tabp-admin-OPS-heartbeat [iam:PermissionsBoundary=$policy/boundary-ops-notification]: 1 件が implicitDeny",
		"apply $iam:role/tabp-admin-ops-heartbeat [iam:PassedToService=scheduler.amazonaws.com]: 1 件が allowed",
		"apply $iam:role/tabp-admin-ops-heartbeat [iam:PassedToService=lambda.amazonaws.com]: 1 件が implicitDeny",
		"apply $topic: 2 件が allowed",
		"apply $topic: 2 件が implicitDeny",
		"apply $wk: 1 件が implicitDeny",
		"apply $slr: 1 件が allowed",
		"apply arn:aws:config:ap-northeast-1:123456789012:configuration-recorder/tabp-admin/abc [aws:RequestedRegion=ap-northeast-1]: 3 件が allowed",
		"apply arn:aws:config:af-south-1:123456789012:configuration-recorder/tabp-admin/abc [aws:RequestedRegion=af-south-1]: 2 件が explicitDeny",
		"apply arn:aws:config:ap-northeast-1:123456789012:configuration-recorder/tabp-admin/abc [aws:RequestedRegion=ap-northeast-1]: 2 件が implicitDeny",
		"apply arn:aws:access-analyzer:ap-northeast-1:123456789012:analyzer/tabp-admin-external [aws:RequestedRegion=ap-northeast-1]: 2 件が allowed",
		"apply arn:aws:access-analyzer:ap-northeast-1:123456789012:analyzer/tabp-admin-external [aws:RequestedRegion=ap-northeast-1]: 1 件が implicitDeny",
		"apply $iam:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig [iam:PassedToService=config.amazonaws.com]: 1 件が allowed",
		"apply $iam:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig [iam:AWSServiceName=config.amazonaws.com]: 1 件が allowed",
		"apply $iam:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig: 1 件が implicitDeny",
		"apply * [ec2:Region=ap-northeast-1; aws:RequestedRegion=ap-northeast-1]: 2 件が allowed",
		"apply * [ec2:Region=ap-northeast-1; aws:RequestedRegion=ap-northeast-1]: 1 件が implicitDeny",
		"apply * [ec2:Region=af-south-1; aws:RequestedRegion=af-south-1]: 1 件が explicitDeny",
		"apply * [aws:RequestedRegion=ap-northeast-1]: 3 件が allowed",
		"apply * [aws:RequestedRegion=af-south-1]: 2 件が explicitDeny",
		"apply *: 1 件が allowed",
		"apply * [aws:RequestedRegion=ap-northeast-1]: 2 件が implicitDeny",
		"plan arn:aws:config:ap-northeast-1:123456789012:configuration-recorder/tabp-admin/abc [aws:RequestedRegion=ap-northeast-1]: 2 件が allowed",
		"plan arn:aws:config:af-south-1:123456789012:configuration-recorder/tabp-admin/abc [aws:RequestedRegion=af-south-1]: 1 件が explicitDeny",
		"plan arn:aws:access-analyzer:ap-northeast-1:123456789012:analyzer/tabp-admin-external [aws:RequestedRegion=ap-northeast-1]: 1 件が allowed",
		"plan arn:aws:access-analyzer:ap-northeast-1:123456789012:analyzer/tabp-admin-external [aws:RequestedRegion=ap-northeast-1]: 1 件が implicitDeny",
	}
	permBudgetsDescs = []string{
		"plan $budget: 2 件が allowed",
		"plan $monitor: 2 件が allowed",
		"plan $subscription: 2 件が allowed",
		"plan $budget: 1 件が implicitDeny",
		"plan *: 2 件が implicitDeny",
		"plan $monitor: 2 件が implicitDeny",
		"plan $subscription: 2 件が implicitDeny",
		"apply $monitor [aws:ResourceTag/tabp:bootstrap=true]: 3 件が explicitDeny",
		"apply $subscription [aws:ResourceTag/tabp:bootstrap=true]: 3 件が explicitDeny",
	}
	permBudgetsGatedDescs = []string{
		"apply $budget: 1 件が allowed",
		"apply *: 2 件が allowed",
		"apply $monitor: 2 件が allowed",
		"apply $subscription: 2 件が allowed",
	}
	permUnlockIdentityDescs = []string{
		"unlock-identity $b [s3:prefix=identity/terraform.tfstate.tflock]: 1 件が allowed",
		"unlock-identity $key.tflock: 2 件が allowed",
		"unlock-identity $b [s3:prefix=identity/terraform.tfstate]: 1 件が implicitDeny",
		"unlock-identity $key.tflock: 2 件が implicitDeny",
		"unlock-identity $key: 3 件が implicitDeny",
		"unlock-identity $sk: 1 件が implicitDeny",
	}
	permUnlockDescs = []string{
		"unlock $b [s3:prefix=workload/terraform.tfstate.tflock]: 1 件が allowed",
		"unlock $wkey.tflock: 2 件が allowed",
		"unlock $b [s3:prefix=identity/terraform.tfstate.tflock]: 1 件が implicitDeny",
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
	permissionsBase     = 120
	governanceGated     = 23
	delegatedGated      = 13
	delegatedAbsent     = "  委任管理者の policy がまだ付いていないので、指定と解除の権限は確かめない（委任管理者の policy を作って付ける apply の後にもう一度走らせる）"
	delegatedUnknown    = "  apply-identity の role の managed policy を確かめられないので（上の NG）、委任管理者の指定と解除の権限は確かめない"
	workloadGatedCount  = 28
	budgetsGatedCount   = 4
	outputPolicyCount   = 13
	governanceAbsent    = "  統制の policy がまだ付いていないので、作成と設定の権限は確かめない（統制の policy を作って付ける apply の後にもう一度走らせる）"
	workloadAbsent      = "  workload の3つの policy がまだ付いていないので、apply の role の workload の権限は確かめない（3つの policy を作って付ける apply の後にもう一度走らせる）"
	budgetsAbsent       = "  workload の3つの policy がまだ付いていないので、apply の role の budgets と Cost Anomaly Detection の作成と変更の権限は確かめない（3つの policy を作って付ける apply の後にもう一度走らせる）"
	budgetsUnknown      = "  apply の role の managed policy を確かめられないので（上の NG）、apply の role の budgets と Cost Anomaly Detection の作成と変更の権限は確かめない"
	attachedIdentityKey = "iam list-attached-role-policies --role-name tabp-admin-ci-apply-identity --output json --region " + testRegion
	attachedApplyKey    = "iam list-attached-role-policies --role-name tabp-admin-ci-apply --output json --region " + testRegion
	governanceAttached  = `{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-manage"},{"PolicyName":"tabp-admin-ci-apply-identity-governance"},{"PolicyName":"tabp-admin-ci-apply-identity-governance-2"}]}`
	workloadAttached    = `{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload-read"},{"PolicyName":"tabp-admin-ci-apply-workload-2"},{"PolicyName":"tabp-admin-ci-apply-workload"}]}`
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
	"$ss", "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass:00000000-0000-0000-0000-0000000000b0",
	"$st", "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass:00000000-0000-0000-0000-0000000000b0/r-abcd",
	"$ty", "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role",
	"$xss", "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-other:00000000-0000-0000-0000-0000000000b0",
	"$xst", "arn:aws:cloudformation::123456789012:stackset-target/tabp-other:00000000-0000-0000-0000-0000000000b0/r-abcd",
	"$yss", "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass-x:00000000-0000-0000-0000-0000000000b0",
	"$yst", "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass-x:00000000-0000-0000-0000-0000000000b0/r-abcd",
	"$inst", "cloudformation:CreateStackInstances cloudformation:UpdateStackInstances",
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
	"$inst cloudformation:DeleteStackInstances",
	"organizations:CreateOrganization organizations:DescribeOrganization iam:UpdateAccountPasswordPolicy",
	"iam:AttachRolePolicy ; iam:PolicyARN = arn:aws:iam::aws:policy/AdministratorAccess (arn)",
	"organizations:DeleteOrganization",
	"iam:DeleteRole",
	"$inst",
	"$inst",
	"$inst ; cloudformation:TargetRegion = ap-northeast-1 (stringList)",
	"$inst ; cloudformation:TargetRegion = ap-northeast-1,ap-northeast-1 (stringList)",
	"$inst ; cloudformation:TargetRegion = ap-northeast-1,us-west-2 (stringList)",
	"$inst ; cloudformation:TargetRegion = us-west-2 (stringList)",
	"$inst",
	"cloudformation:UpdateStackSet",
	"cloudformation:UpdateStackSet",
	"cloudformation:UpdateStackSet",
	"cloudformation:DeleteStackInstances",
	"$inst",
	"$inst",
	"$inst",
	"$inst",
	"cloudformation:UpdateStackSet",
	"cloudformation:UpdateStackSet",
	"cloudformation:UpdateStackSet",
	"cloudformation:UpdateStackSet",
	// 委任管理者の指定
	"$manage",
	"iam:AttachRolePolicy ; iam:PolicyARN = $policy/apply-identity-governance-2 (arn)",
	"organizations:ListDelegatedAdministrators",
	"guardduty:ListOrganizationAdminAccounts inspector2:ListDelegatedAdminAccounts ; aws:RequestedRegion = ap-northeast-1 (string)",
	"guardduty:ListOrganizationAdminAccounts inspector2:ListDelegatedAdminAccounts ; aws:RequestedRegion = af-south-1 (string)",
	"securityhub:ListOrganizationAdminAccounts ; aws:RequestedRegion = ap-northeast-1 (string)",
	"securityhub:ListOrganizationAdminAccounts ; aws:RequestedRegion = af-south-1 (string)",
	"guardduty:EnableOrganizationAdminAccount inspector2:EnableDelegatedAdminAccount ; aws:RequestedRegion = ap-northeast-1 (string)",
	"organizations:RegisterDelegatedAdministrator organizations:DeregisterDelegatedAdministrator",
	"iam:CreatePolicyVersion",
	"organizations:RegisterDelegatedAdministrator organizations:DeregisterDelegatedAdministrator ; organizations:ServicePrincipal = guardduty.amazonaws.com (string)",
	"organizations:RegisterDelegatedAdministrator ; organizations:ServicePrincipal = config.amazonaws.com (string)",
	"organizations:RegisterDelegatedAdministrator ; organizations:ServicePrincipal = sso.amazonaws.com (string)",
	"organizations:EnableAWSServiceAccess ; organizations:ServicePrincipal = inspector2.amazonaws.com (string)",
	"organizations:EnableAWSServiceAccess organizations:DisableAWSServiceAccess ; organizations:ServicePrincipal = config.amazonaws.com (string)",
	"guardduty:EnableOrganizationAdminAccount guardduty:DisableOrganizationAdminAccount inspector2:EnableDelegatedAdminAccount inspector2:DisableDelegatedAdminAccount ; aws:RequestedRegion = ap-northeast-1 (string)",
	"guardduty:EnableOrganizationAdminAccount guardduty:DisableOrganizationAdminAccount inspector2:EnableDelegatedAdminAccount inspector2:DisableDelegatedAdminAccount ; aws:RequestedRegion = af-south-1 (string)",
	"securityhub:EnableOrganizationAdminAccount securityhub:DisableOrganizationAdminAccount ; aws:RequestedRegion = ap-northeast-1 (string)",
	"securityhub:EnableOrganizationAdminAccount ; aws:RequestedRegion = af-south-1 (string)",
	"organizations:EnableAWSServiceAccess ; organizations:ServicePrincipal = securityhub.amazonaws.com (string)",
	"cloudtrail:RegisterOrganizationDelegatedAdmin cloudtrail:DeregisterOrganizationDelegatedAdmin",
	"iam:CreateServiceLinkedRole ; iam:AWSServiceName = cloudtrail.amazonaws.com (string)",
	"iam:CreateServiceLinkedRole ; iam:AWSServiceName = guardduty.amazonaws.com (string)",
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
	"config:PutConfigurationRecorder config:StartConfigurationRecorder config:DescribeConfigurationRecorders ; aws:RequestedRegion = ap-northeast-1 (string)",
	"config:PutConfigurationRecorder config:DescribeConfigurationRecorders ; aws:RequestedRegion = af-south-1 (string)",
	"config:DeleteConfigurationRecorder config:StopConfigurationRecorder ; aws:RequestedRegion = ap-northeast-1 (string)",
	"access-analyzer:CreateAnalyzer access-analyzer:GetAnalyzer ; aws:RequestedRegion = ap-northeast-1 (string)",
	"access-analyzer:DeleteAnalyzer ; aws:RequestedRegion = ap-northeast-1 (string)",
	"iam:PassRole ; iam:PassedToService = config.amazonaws.com (string)",
	"iam:CreateServiceLinkedRole ; iam:AWSServiceName = config.amazonaws.com (string)",
	"iam:DeleteServiceLinkedRole",
	"ec2:EnableEbsEncryptionByDefault ec2:ModifyVpcBlockPublicAccessOptions ; ec2:Region = ap-northeast-1 (string) ; aws:RequestedRegion = ap-northeast-1 (string)",
	"ec2:DisableEbsEncryptionByDefault ; ec2:Region = ap-northeast-1 (string) ; aws:RequestedRegion = ap-northeast-1 (string)",
	"ec2:EnableEbsEncryptionByDefault ; ec2:Region = af-south-1 (string) ; aws:RequestedRegion = af-south-1 (string)",
	"config:PutDeliveryChannel config:PutRetentionConfiguration elasticmapreduce:PutBlockPublicAccessConfiguration ; aws:RequestedRegion = ap-northeast-1 (string)",
	"config:PutDeliveryChannel elasticmapreduce:PutBlockPublicAccessConfiguration ; aws:RequestedRegion = af-south-1 (string)",
	"s3:PutAccountPublicAccessBlock",
	"config:DeleteDeliveryChannel config:DeleteRetentionConfiguration ; aws:RequestedRegion = ap-northeast-1 (string)",
	"config:DescribeConfigurationRecorders config:DescribeConfigurationRecorderStatus ; aws:RequestedRegion = ap-northeast-1 (string)",
	"config:DescribeConfigurationRecorders ; aws:RequestedRegion = af-south-1 (string)",
	"access-analyzer:GetAnalyzer ; aws:RequestedRegion = ap-northeast-1 (string)",
	"access-analyzer:CreateAnalyzer ; aws:RequestedRegion = ap-northeast-1 (string)",
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

// permExpected は、permissions の出力の行。governance・delegated・workload・budgets は条件つきの確認の区分の末尾の
// 行（nil なら条件つきの確認をすべて ok とする）。
func permExpected(governance, delegated, workload, budgets []string) []string {
	out := []string{"# S3 と IAM"}
	out = append(out, expand(permS3Descs, "ok  ")...)
	out = append(append(out, "# state の鍵"), expand(permKeyDescs, "ok  ")...)
	out = append(append(out, "# 統制の構成物と apply-identity の policy（deployment-identity/ADR-0201 決定3・5・7）"), expand(permGovernanceDescs, "ok  ")...)

	if governance == nil {
		governance = expand(permGovernanceGatedDescs, "ok  ")
	}

	out = append(append(out, governance...), "# 委任管理者の指定（deployment-identity/ADR-0201 決定7・21、repository-wide/ADR-0209 決定4）")
	out = append(out, expand(permDelegatedDescs, "ok  ")...)

	if delegated == nil {
		delegated = expand(permDelegatedGatedDescs, "ok  ")
	}

	out = append(append(out, delegated...), "# Chatbot の service-linked role（deployment-identity/ADR-0201 決定3）")
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

func Test_ctxList(t *testing.T) {
	t.Parallel()

	t.Run("値が1つなら、その値だけを ContextKeyValues に持つ stringList の entry", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "ContextKeyName=cloudformation:TargetRegion,ContextKeyValues=ap-northeast-1,ContextKeyType=stringList",
			ctxList("cloudformation:TargetRegion", "ap-northeast-1"))
	})

	t.Run("値が2つなら、`,` で区切って1つの ContextKeyValues に並べる", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "ContextKeyName=cloudformation:TargetRegion,ContextKeyValues=ap-northeast-1,us-west-2,ContextKeyType=stringList",
			ctxList("cloudformation:TargetRegion", "ap-northeast-1", "us-west-2"))
	})
}

func Test_ctxSummary(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ in, want string }{
		"string の entry はキーと値": {"ContextKeyName=ec2:Region,ContextKeyValues=us-west-2,ContextKeyType=string", "ec2:Region=us-west-2"},
		"stringList の entry は値の `,` を残す": {"ContextKeyName=cloudformation:TargetRegion,ContextKeyValues=ap-northeast-1,us-west-2,ContextKeyType=stringList",
			"cloudformation:TargetRegion=ap-northeast-1,us-west-2"},
		"ContextKeyName で始まらなければそのまま": {"c1", "c1"},
		"ContextKeyValues が無ければそのまま":  {"ContextKeyName=k,ContextKeyType=string", "ContextKeyName=k,ContextKeyType=string"},
		"ContextKeyType が無ければそのまま":    {"ContextKeyName=k,ContextKeyValues=v", "ContextKeyName=k,ContextKeyValues=v"},
		"空文字列はそのまま":                   {"", ""},
		"値が `,ContextKeyType=` を含めば、最後の `,ContextKeyType=` で切る": {"ContextKeyName=k,ContextKeyValues=a,ContextKeyType=b,ContextKeyType=string",
			"k=a,ContextKeyType=b"},
		"キーが空なら、空のキーと値": {"ContextKeyName=,ContextKeyValues=v,ContextKeyType=string", "=v"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ctxSummary(tc.in))
		})
	}
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

	t.Run("context ありなら、context を `[...]` で添える", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "plan arn:x [ec2:Region=us-west-2]: 1 件が implicitDeny",
			simCtx(implicitDeny, "plan", "arn:x", []string{ctxEntry("ec2:Region", "us-west-2", "string")}, "a").desc())
	})

	t.Run("複数 context と読めない entry は、`; ` で区切り、読めない entry はそのまま並べる", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "plan arn:x [k1=a,b; k2=true; c3]: 1 件が allowed",
			simCtx(allowed, "plan", "arn:x", []string{ctxList("k1", "a", "b"), ctxEntry("k2", "true", "boolean"), "c3"}, "a").desc())
	})
}

func Test_simRow_args(t *testing.T) {
	t.Parallel()

	n := testNames("")
	role := "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"

	assert.Equal(t, []string{"iam", "simulate-principal-policy", "--policy-source-arn", role, "--resource-arns", "arn:x", "--action-names", "a", "b"},
		sim(allowed, "plan", "arn:x", "a", "b").args(n))

	assert.Equal(t, []string{"iam", "simulate-principal-policy", "--policy-source-arn", role, "--resource-arns", "arn:x", "--context-entries", "c1", "c2", "--action-names", "a"},
		simCtx(allowed, "plan", "arn:x", []string{"c1", "c2"}, "a").args(n), "context entry を1つの --context-entries に並べる")

	assert.Equal(t, []string{"iam", "simulate-principal-policy", "--policy-source-arn", role, "--resource-arns", "arn:x", "--context-entries",
		"ContextKeyName=k,ContextKeyValues=v1,v2,ContextKeyType=stringList", "--action-names", "a"},
		simCtx(allowed, "plan", "arn:x", []string{ctxList("k", "v1", "v2")}, "a").args(n), "stringList の entry は分けずに1つの引数として --context-entries に渡す")

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
		r                awsResult
		state, delegated gateState
	}{
		"両方が付いていれば attached":           {okResult(governanceAttached), gateAttached, gateAttached},
		"統制の policy だけなら委任管理者は absent": {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-governance"}]}`), gateAttached, gateAbsent},
		"委任管理者の policy だけなら統制は absent": {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-governance-2"}]}`), gateAbsent, gateAttached},
		"付いていなければ absent":              {okResult(noneAttached), gateAbsent, gateAbsent},
		"manage だけなら absent":           {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-manage"}]}`), gateAbsent, gateAbsent},
		"一覧が配列でなければ unknown":           {okResult(`{"AttachedPolicies":{"PolicyName":"tabp-admin-ci-apply-identity-governance"}}`), gateUnknown, gateUnknown},
		"空の応答なら unknown":               {okResult(""), gateUnknown, gateUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, d, err := governanceGate(newQuery(verifyFake(t, map[string]awsResult{attachedIdentityKey: tc.r}, nil).fn, testRegion), n)

			require.NoError(t, err)
			assert.Equal(t, tc.state, g.state)
			assert.Equal(t, tc.delegated, d.state)
		})
	}

	t.Run("読めなければ unknown で、NG の確認を持つ", func(t *testing.T) {
		t.Parallel()

		g, d, err := governanceGate(newQuery(verifyFake(t, map[string]awsResult{attachedIdentityKey: failed("denied")}, nil).fn, testRegion), n)

		require.NoError(t, err)
		assert.Equal(t, gateUnknown, g.state)
		assert.Equal(t, gateUnknown, d.state)
		assert.Nil(t, d.ng.eval, "NG は統制の側だけが持つ")

		v, _ := g.ng.eval(nil)
		assert.Equal(t, "apply-identity: list-attached-role-policies を読めないので、統制の policy が付いているかを決められない", g.ng.desc)
		assert.Equal(t, verdict{reason: "呼び出しが失敗: denied"}, v)
	})

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, _, err := governanceGate(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion), n)

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
		"3つちょうどなら attached": {okResult(workloadAttached), gateAttached},
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
		"読めなければ unknown":    {failed("denied"), "apply: list-attached-role-policies を読めないので、workload の3つの policy が付いているかを決められない", "呼び出しが失敗: denied"},
		"応答が読めなければ unknown": {okResult(`{"AttachedPolicies":"x"}`), "apply: list-attached-role-policies を読めないので、workload の3つの policy が付いているかを決められない", "応答を読めない"},
		"1つだけなら unknown": {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload"}]}`),
			"apply の role に付く managed policy が workload の3つの policy ではない", "tabp-admin-ci-apply-workload"},
		"workload-2 が無ければ unknown": {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload"},{"PolicyName":"tabp-admin-ci-apply-workload-read"}]}`),
			"apply の role に付く managed policy が workload の3つの policy ではない", "tabp-admin-ci-apply-workload、tabp-admin-ci-apply-workload-read"},
		"余分があれば unknown": {okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload"},{"PolicyName":"x"},{"PolicyName":"tabp-admin-ci-apply-workload-read"}]}`),
			"apply の role に付く managed policy が workload の3つの policy ではない", "tabp-admin-ci-apply-workload、tabp-admin-ci-apply-workload-read、x"},
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

		require.Len(t, rows, permissionsBase+governanceGated+delegatedGated+workloadGatedCount+budgetsGatedCount)

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

	t.Run("各行の role・resource・context・判定・action の数が、表と独立に書いた desc の一覧と一致する", func(t *testing.T) {
		t.Parallel()

		var descs []string
		for _, l := range [][]string{
			permS3Descs, permKeyDescs, permGovernanceDescs, permGovernanceGatedDescs, permDelegatedDescs, permDelegatedGatedDescs, permSLRDescs, permWorkloadDescs, permWorkloadGatedDescs,
			permBudgetsDescs, permBudgetsGatedDescs, permUnlockIdentityDescs, permUnlockDescs,
		} {
			descs = append(descs, expand(l, "")...)
		}

		rows := allRows(n, testKeyARN)

		require.Len(t, descs, len(rows))

		for i, r := range rows {
			assert.Equal(t, descs[i], r.desc(), "%d 行目", i)
		}
	})

	t.Run("区分の中で desc が重ならず、NG の行がどの行かを区別できる", func(t *testing.T) {
		t.Parallel()

		require.NotEmpty(t, sections)

		for _, s := range sections {
			var descs []string
			for _, r := range append(slices.Clone(s.rows), s.gated...) {
				descs = append(descs, r.desc())
			}

			require.NotEmpty(t, descs, s.heading)

			sorted := slices.Sorted(slices.Values(descs))
			assert.Len(t, slices.Compact(sorted), len(descs), s.heading)
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

	t.Run("region が us-west-2 なら", func(t *testing.T) {
		t.Parallel()

		w := newNames(verifyOptions{project: testProject, region: "us-west-2"}, testAccount)
		got := permissionSections(w, testKeyARN, "")
		byHeading := func(t *testing.T, prefix string) section {
			t.Helper()

			i := slices.IndexFunc(got, func(s section) bool { return strings.HasPrefix(s.heading, prefix) })
			require.GreaterOrEqual(t, i, 0, prefix)

			return got[i]
		}

		t.Run("及ばない region の SNS の行に eu-west-1 を使う", func(t *testing.T) {
			t.Parallel()

			workload := byHeading(t, "# workload の権限の policy と permissions boundary")

			assert.True(t, slices.ContainsFunc(workload.rows, func(r simRow) bool {
				return r.want == implicitDeny && r.resource == "arn:aws:sns:eu-west-1:123456789012:tabp-admin-ops-notification"
			}))
		})

		t.Run("stack instance の行は、TargetRegion の一覧を us-west-2 と eu-west-1 で組み、stackset の ARN に us-west-2 を持つ", func(t *testing.T) {
			t.Parallel()

			type row struct{ want, resource, regions string }

			ss := "arn:aws:cloudformation:us-west-2:123456789012:stackset/tabp-break-glass:00000000-0000-0000-0000-0000000000b0"
			st := "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass:00000000-0000-0000-0000-0000000000b0/r-abcd"
			ty := "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role"
			xss := "arn:aws:cloudformation:us-west-2:123456789012:stackset/tabp-other:00000000-0000-0000-0000-0000000000b0"
			xst := "arn:aws:cloudformation::123456789012:stackset-target/tabp-other:00000000-0000-0000-0000-0000000000b0/r-abcd"
			yss := "arn:aws:cloudformation:us-west-2:123456789012:stackset/tabp-break-glass-x:00000000-0000-0000-0000-0000000000b0"
			yst := "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass-x:00000000-0000-0000-0000-0000000000b0/r-abcd"

			var rows []row

			for _, r := range byHeading(t, "# 統制の構成物と ").gated {
				if !slices.Contains(r.actions, "cloudformation:CreateStackInstances") {
					continue
				}

				regions := ""

				for _, c := range r.context {
					if v, ok := strings.CutPrefix(c, "ContextKeyName=cloudformation:TargetRegion,ContextKeyValues="); ok {
						regions = strings.TrimSuffix(v, ",ContextKeyType=stringList")
					}
				}

				rows = append(rows, row{r.want, r.resource, regions})
			}

			assert.Equal(t, []row{
				{allowed, ss, ""}, {allowed, st, ""},
				{allowed, ty, "us-west-2"}, {allowed, ty, "us-west-2,us-west-2"},
				{implicitDeny, ty, "us-west-2,eu-west-1"}, {implicitDeny, ty, "eu-west-1"}, {implicitDeny, ty, ""},
				{implicitDeny, xss, ""}, {implicitDeny, xst, ""}, {implicitDeny, yss, ""}, {implicitDeny, yst, ""},
			}, rows)
		})
	})
}

func Test_otherRegion(t *testing.T) {
	t.Parallel()

	for region, want := range map[string]string{
		"ap-northeast-1": "us-west-2",
		"us-east-1":      "us-west-2",
		"eu-west-1":      "us-west-2",
		"us-west-2":      "eu-west-1",
	} {
		t.Run("provider の region が "+region+" なら、それとも us-east-1 とも違う "+want, func(t *testing.T) {
			t.Parallel()

			got := otherRegion(newNames(verifyOptions{project: testProject, region: region}, testAccount))

			assert.Equal(t, want, got)
			assert.NotEqual(t, region, got)
			assert.NotEqual(t, "us-east-1", got)
		})
	}
}

func Test_stackSetARNs(t *testing.T) {
	t.Parallel()

	t.Run("stackset は provider の region を持ち、stackset-target は region を持たず、type は region が * でアカウントを持たない", func(t *testing.T) {
		t.Parallel()

		ss, st, ty := stackSetARNs(testNames(""), "tabp-break-glass")

		assert.Equal(t, "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass:00000000-0000-0000-0000-0000000000b0", ss)
		assert.Equal(t, "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass:00000000-0000-0000-0000-0000000000b0/r-abcd", st)
		assert.Equal(t, "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role", ty)
	})

	t.Run("region が us-west-2 なら、stackset だけが us-west-2 を持つ", func(t *testing.T) {
		t.Parallel()

		ss, st, ty := stackSetARNs(newNames(verifyOptions{project: testProject, region: "us-west-2"}, testAccount), "tabp-break-glass")

		assert.Equal(t, "arn:aws:cloudformation:us-west-2:123456789012:stackset/tabp-break-glass:00000000-0000-0000-0000-0000000000b0", ss)
		assert.Equal(t, "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass:00000000-0000-0000-0000-0000000000b0/r-abcd", st)
		assert.Equal(t, "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role", ty)
	})

	t.Run("名前は stackset と stackset-target にだけ入り、type には入らない", func(t *testing.T) {
		t.Parallel()

		ss, st, ty := stackSetARNs(testNames(""), "tabp-other")

		assert.Equal(t, "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-other:00000000-0000-0000-0000-0000000000b0", ss)
		assert.Equal(t, "arn:aws:cloudformation::123456789012:stackset-target/tabp-other:00000000-0000-0000-0000-0000000000b0/r-abcd", st)
		assert.Equal(t, "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role", ty)
	})
}

func Test_stackInstanceRows(t *testing.T) {
	t.Parallel()

	n := testNames("")
	rows := stackInstanceRows(n, "us-west-2")

	ss := "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass:00000000-0000-0000-0000-0000000000b0"
	st := "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass:00000000-0000-0000-0000-0000000000b0/r-abcd"
	ty := "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role"
	xss := "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-other:00000000-0000-0000-0000-0000000000b0"
	xst := "arn:aws:cloudformation::123456789012:stackset-target/tabp-other:00000000-0000-0000-0000-0000000000b0/r-abcd"
	yss := "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass-x:00000000-0000-0000-0000-0000000000b0"
	yst := "arn:aws:cloudformation::123456789012:stackset-target/tabp-break-glass-x:00000000-0000-0000-0000-0000000000b0/r-abcd"
	inst := []string{"cloudformation:CreateStackInstances", "cloudformation:UpdateStackInstances"}
	tr := func(v string) []string {
		return []string{"ContextKeyName=cloudformation:TargetRegion,ContextKeyValues=" + v + ",ContextKeyType=stringList"}
	}
	row := func(want, resource string, context []string, actions ...string) simRow {
		return simRow{want: want, role: "apply-identity", resource: resource, context: context, actions: actions}
	}

	t.Run("作成と更新は stackset と stackset-target でキー無しで allowed、type で provider の region だけの一覧で allowed・"+
		"別の region を含む一覧・別の region だけの一覧・キー無しで implicitDeny、UpdateStackSet は allowed、"+
		"DeleteStackInstances と別の名前・名前の prefix を共有する名前の StackSet は implicitDeny", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []simRow{
			row(allowed, ss, nil, inst...),
			row(allowed, st, nil, inst...),
			row(allowed, ty, tr("ap-northeast-1"), inst...),
			row(allowed, ty, tr("ap-northeast-1,ap-northeast-1"), inst...),
			row(implicitDeny, ty, tr("ap-northeast-1,us-west-2"), inst...),
			row(implicitDeny, ty, tr("us-west-2"), inst...),
			row(implicitDeny, ty, nil, inst...),
			row(allowed, ss, nil, "cloudformation:UpdateStackSet"),
			row(allowed, st, nil, "cloudformation:UpdateStackSet"),
			row(allowed, ty, nil, "cloudformation:UpdateStackSet"),
			row(implicitDeny, ss, nil, "cloudformation:DeleteStackInstances"),
			row(implicitDeny, xss, nil, inst...),
			row(implicitDeny, xst, nil, inst...),
			row(implicitDeny, yss, nil, inst...),
			row(implicitDeny, yst, nil, inst...),
			row(implicitDeny, xss, nil, "cloudformation:UpdateStackSet"),
			row(implicitDeny, xst, nil, "cloudformation:UpdateStackSet"),
			row(implicitDeny, yss, nil, "cloudformation:UpdateStackSet"),
			row(implicitDeny, yst, nil, "cloudformation:UpdateStackSet"),
		}, rows)
	})

	t.Run("TargetRegion の context は type の行にだけ与え、stackset と stackset-target の行には与えない", func(t *testing.T) {
		t.Parallel()

		require.NotEmpty(t, rows)

		for _, r := range rows {
			if r.resource != ty {
				assert.Empty(t, r.context, r.resource)
			}
		}
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
		identity, apply                        string
		governance, delegated, workload, budgt []string
		count                                  int
		result                                 string
	}{
		"条件つきの確認がすべて走る": {governanceAttached, workloadAttached, nil, nil, nil, nil, permissionsBase + governanceGated + delegatedGated + workloadGatedCount + budgetsGatedCount,
			"# 結果: 188 件のうち NG 0 件"},
		"統制の policy が付いていない": {noneAttached, workloadAttached, []string{governanceAbsent}, []string{delegatedAbsent}, nil, nil, permissionsBase + workloadGatedCount + budgetsGatedCount,
			"# 結果: 152 件のうち NG 0 件（確かめていない区分 2）"},
		"委任管理者の policy だけが付いていない": {`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-manage"},{"PolicyName":"tabp-admin-ci-apply-identity-governance"}]}`, workloadAttached,
			nil, []string{delegatedAbsent}, nil, nil, permissionsBase + governanceGated + workloadGatedCount + budgetsGatedCount, "# 結果: 175 件のうち NG 0 件（確かめていない区分 1）"},
		"workload の policy が付いていない": {governanceAttached, noneAttached, nil, nil, []string{workloadAbsent}, []string{budgetsAbsent},
			permissionsBase + governanceGated + delegatedGated, "# 結果: 156 件のうち NG 0 件（確かめていない区分 2）"},
		"どちらも付いていない": {noneAttached, noneAttached, []string{governanceAbsent}, []string{delegatedAbsent}, []string{workloadAbsent}, []string{budgetsAbsent}, permissionsBase,
			"# 結果: 120 件のうち NG 0 件（確かめていない区分 4）"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			lines, err := run(t, permFake(t, okResult(tc.identity), okResult(tc.apply)))

			require.NoError(t, err)
			assert.Equal(t, append(permExpected(tc.governance, tc.delegated, tc.workload, tc.budgt), tc.result), lines)
			assert.Contains(t, tc.result, "# 結果: "+strconv.Itoa(tc.count)+" 件", "件数は表と独立の定数から")
		})
	}

	t.Run("managed policy を読めなければ、条件つきの確認を飛ばさずに NG として数える", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, failed("denied"), failed("denied")))

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, append(permExpected(
			[]string{"NG  apply-identity: list-attached-role-policies を読めないので、統制の policy が付いているかを決められない（呼び出しが失敗: denied）"},
			[]string{delegatedUnknown},
			[]string{"NG  apply: list-attached-role-policies を読めないので、workload の3つの policy が付いているかを決められない（呼び出しが失敗: denied）"},
			[]string{budgetsUnknown},
		), "# 結果: "+strconv.Itoa(permissionsBase+2)+" 件のうち NG 2 件（確かめていない区分 2）"), lines)
	})

	t.Run("統制の policy を読めなければ NG 1 件で、委任管理者の区分だけを確かめていない区分に数える", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, failed("denied"), okResult(workloadAttached)))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, delegatedUnknown)
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+workloadGatedCount+budgetsGatedCount+1)+" 件のうち NG 1 件（確かめていない区分 1）", lines[len(lines)-1])
	})

	t.Run("workload の policy を読めなければ NG 1 件で、budgets の注記だけを確かめていない区分に数える", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, okResult(governanceAttached), failed("denied")))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, budgetsUnknown)
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+governanceGated+delegatedGated+1)+" 件のうち NG 1 件（確かめていない区分 1）", lines[len(lines)-1])
	})

	t.Run("apply の managed policy が想定と違えば、名前を添えて NG", func(t *testing.T) {
		t.Parallel()

		lines, err := run(t, permFake(t, okResult(governanceAttached), okResult(`{"AttachedPolicies":[{"PolicyName":"x"}]}`)))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  apply の role に付く managed policy が workload の3つの policy ではない（x）")
		assert.Contains(t, lines, budgetsUnknown)
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+governanceGated+delegatedGated+1)+" 件のうち NG 1 件（確かめていない区分 1）", lines[len(lines)-1])
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
		assert.Equal(t, "# 結果: "+strconv.Itoa(permissionsBase+governanceGated+delegatedGated+workloadGatedCount+budgetsGatedCount+1)+" 件のうち NG 9 件", lines[len(lines)-1])

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

// testReferenceDir は、make tf-policy の -out。plans.txt と inputs/ を持つ。
const testReferenceDir = "tmp/tf-policy/run"

var (
	testReferenceList = testReferenceDir + "/plans.txt"
	testReferencePlan = testReferenceDir + "/inputs/state-backend.json"
)

// referenceFiles は、plans.txt に state-backend の plan を挙げ、inputs に plan を置いた tf-policy の出力を読む。
func referenceFiles(plan string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		switch p {
		case testReferenceList:
			return []byte(testReferenceDir + "/runs/finops.jsonl\n" + testReferenceDir + "/runs/state-backend.jsonl\n"), nil
		case testReferencePlan:
			return []byte(plan), nil
		}

		return nil, os.ErrNotExist
	}
}

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

	t.Run("module の文書を読み、アカウント ID を置き換える", func(t *testing.T) {
		t.Parallel()

		doc, reason := referencePolicy(referenceFiles(referencePlanJSON(t)), testReferenceDir, "plan_output", "999999999999")

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
		"JSON でない": {"x", "plan_output", testReferencePlan + " を JSON の object として読めない"},
		"null":     {"null", "plan_output", testReferencePlan + " を JSON の object として読めない"},
		"空":        {"", "plan_output", testReferencePlan + " を JSON の object として読めない"},
		"配列":       {"[{}]", "plan_output", testReferencePlan + " を JSON の object として読めない"},
		"resource_changes が空の配列": {`{"resource_changes":[]}`, "plan_output",
			testReferencePlan + " に module.state_backend.module.plan_output.aws_s3_bucket_policy.this の policy が1つではない（0 個）"},
		"resource_changes が配列でない": {`{"resource_changes":{}}`, "plan_output", testReferencePlan + " に resource_changes の配列が無い"},
		"resource_changes が無い":    {`{}`, "plan_output", testReferencePlan + " に resource_changes の配列が無い"},
		"address が無い": {referencePlanJSON(t), "none",
			testReferencePlan + " に module.state_backend.module.none.aws_s3_bucket_policy.this の policy が1つではない（0 個）"},
		"address が2つ": {dup, "apply_output",
			testReferencePlan + " に module.state_backend.module.apply_output.aws_s3_bucket_policy.this の policy が1つではない（2 個）"},
		"policy が null": {`{"resource_changes":[{"address":"module.state_backend.module.plan_output.aws_s3_bucket_policy.this","change":{"after":{"policy":null}}}]}`, "plan_output",
			testReferencePlan + " に module.state_backend.module.plan_output.aws_s3_bucket_policy.this の policy が1つではない（1 個）"},
		"after が null": {`{"resource_changes":[{"address":"module.state_backend.module.plan_output.aws_s3_bucket_policy.this","change":{"after":null}}]}`, "plan_output",
			testReferencePlan + " に module.state_backend.module.plan_output.aws_s3_bucket_policy.this の policy が1つではない（1 個）"},
		"policy が無い": {`{"resource_changes":[{"address":"module.state_backend.module.plan_output.aws_s3_bucket_policy.this","change":{"after":{}}}]}`, "plan_output",
			testReferencePlan + " に module.state_backend.module.plan_output.aws_s3_bucket_policy.this の policy が1つではない（1 個）"},
		"policy が空": {`{"resource_changes":[{"address":"module.state_backend.module.plan_output.aws_s3_bucket_policy.this","change":{"after":{"policy":""}}}]}`, "plan_output",
			testReferencePlan + " に module.state_backend.module.plan_output.aws_s3_bucket_policy.this の policy が1つではない（1 個）"},
		"policy が文字列でない": {`{"resource_changes":[{"address":"module.state_backend.module.plan_output.aws_s3_bucket_policy.this","change":{"after":{"policy":{"Statement":[]}}}}]}`, "plan_output",
			testReferencePlan + " に module.state_backend.module.plan_output.aws_s3_bucket_policy.this の policy が1つではない（1 個）"},
	} {
		t.Run(name+"なら理由を返す", func(t *testing.T) {
			t.Parallel()

			doc, reason := referencePolicy(referenceFiles(tc.content), testReferenceDir, tc.module, testAccount)

			assert.Empty(t, doc)
			assert.Equal(t, tc.want, reason)
		})
	}

	t.Run("plans.txt の行の CRLF と前後の空白を外して state-backend の plan を見つける", func(t *testing.T) {
		t.Parallel()

		files := referenceFiles(referencePlanJSON(t))

		doc, reason := referencePolicy(func(p string) ([]byte, error) {
			if p == testReferenceList {
				return []byte(testReferenceDir + "/runs/finops.jsonl\r\n  " + testReferenceDir + "/runs/state-backend.jsonl \r\n"), nil
			}

			return files(p)
		}, testReferenceDir, "plan_output", testAccount)

		assert.Empty(t, reason)
		assert.NotEmpty(t, doc)
	})

	t.Run("plans.txt を読めなければ、inputs を読まずに理由を返す", func(t *testing.T) {
		t.Parallel()

		var read []string

		doc, reason := referencePolicy(func(p string) ([]byte, error) {
			read = append(read, p)

			return nil, os.ErrNotExist
		}, testReferenceDir, "plan_output", testAccount)

		assert.Empty(t, doc)
		assert.Equal(t, testReferenceList+" を読めない（make tf-policy を走らせてください）: "+os.ErrNotExist.Error(), reason)
		assert.Equal(t, []string{testReferenceList}, read)
	})

	for name, list := range map[string]string{
		"空":           "",
		"ほかの root だけ": testReferenceDir + "/runs/finops.jsonl\n",
		"空行と空白だけ":     "\n  \r\n\t\n",
		"拡張子が違う":      testReferenceDir + "/runs/state-backend.json\n",
		"名前が state-backend を含むだけ": testReferenceDir + "/runs/state-backend-x.jsonl\n" + testReferenceDir + "/runs/x-state-backend.jsonl\n",
	} {
		t.Run("plans.txt が"+name+"なら、今回の出力ではないので inputs を読まない", func(t *testing.T) {
			t.Parallel()

			doc, reason := referencePolicy(func(p string) ([]byte, error) {
				if p == testReferenceList {
					return []byte(list), nil
				}

				require.Fail(t, "plans.txt のほかを読んだ", p)

				return nil, os.ErrNotExist
			}, testReferenceDir, "plan_output", testAccount)

			assert.Empty(t, doc)
			assert.Equal(t, testReferenceList+" に state-backend の plan が無い（make tf-policy で state-backend の plan が落ちた）", reason)
		})
	}

	t.Run("plans.txt にあっても inputs の plan を読めなければ理由を返す", func(t *testing.T) {
		t.Parallel()

		files := referenceFiles("")

		_, reason := referencePolicy(func(p string) ([]byte, error) {
			if p == testReferencePlan {
				return nil, os.ErrNotExist
			}

			return files(p)
		}, testReferenceDir, "plan_output", testAccount)

		assert.Equal(t, testReferencePlan+" を読めない: "+os.ErrNotExist.Error(), reason)
	})

	t.Run("置き場は渡された dir から組む", func(t *testing.T) {
		t.Parallel()

		var read []string

		_, _ = referencePolicy(func(p string) ([]byte, error) {
			read = append(read, p)

			if p == "x/y/plans.txt" {
				return []byte("x/y/runs/state-backend.jsonl\n"), nil
			}

			return nil, os.ErrNotExist
		}, "x/y", "plan_output", testAccount)

		assert.Equal(t, []string{"x/y/plans.txt", "x/y/inputs/state-backend.json"}, read)
	})
}

func Test_outputDocument(t *testing.T) {
	t.Parallel()

	env := func(t *testing.T, source string, r awsResult) verifyEnv {
		t.Helper()

		f := verifyFake(t, map[string]awsResult{s3Key("get-bucket-policy", testPlanBucket): r}, nil)

		return verifyEnv{q: newQuery(f.fn, testRegion), n: testNames(""), source: source, referenceDir: testReferenceDir,
			readFile: referenceFiles(referencePlanJSON(t))}
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
			e.readFile = referenceFiles(string(b))

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
		e := verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceReference, referenceDir: testReferenceDir,
			readFile: referenceFiles(referencePlanJSON(t))}

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
		e := verifyEnv{q: newQuery(f.fn, testRegion), n: other, source: sourceReference, referenceDir: testReferenceDir,
			readFile: referenceFiles(referencePlanJSON(t))}

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
		readFile func(t *testing.T) func(string) ([]byte, error)
		result   string
	}{
		"tf-policy の出力が無い": {func(*testing.T) func(string) ([]byte, error) {
			return func(string) ([]byte, error) { return nil, os.ErrNotExist }
		}, "# 結果: 13 件のうち NG 13 件"},
		"plans.txt はあるが inputs の plan が無い": {func(t *testing.T) func(string) ([]byte, error) {
			files := referenceFiles(referencePlanJSON(t))

			return func(p string) ([]byte, error) {
				if p == testReferencePlan {
					return nil, os.ErrNotExist
				}

				return files(p)
			}
		}, "# 結果: 13 件のうち NG 13 件"},
		"reference の plan に apply_output だけ無い": {func(t *testing.T) func(string) ([]byte, error) {
			return referenceFiles(strings.Replace(referencePlanJSON(t), "module.state_backend.module.apply_output", "module.x", 1))
		}, "# 結果: 13 件のうち NG 4 件"},
	} {
		t.Run(name+"なら、読めない側が NG", func(t *testing.T) {
			t.Parallel()

			f := &fakeAWS{t: t, handler: handler(map[string]string{
				"plan": `{"Statement":[{"Effect":"Deny","Principal":"arn:aws:iam::123456789012:root"}]}`,
			})}
			e := verifyEnv{q: newQuery(f.fn, testRegion), n: n, source: sourceReference, referenceDir: testReferenceDir, readFile: tc.readFile(t)}

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
