package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

const (
	allowed      = "allowed"
	implicitDeny = "implicitDeny"
	explicitDeny = "explicitDeny"

	// referenceRoot は、make tf-policy の reference の root のうち、出力の Bucket を持つもの。
	referenceRoot = "state-backend"
	// referenceAccount は、reference の plan のアカウント ID。実際の ID に置き換えて評価する。
	referenceAccount = "123456789012"
)

// simRow は、role の policy を resource に対して評価する simulate-principal-policy の1回。actions の判定が
// すべて want であれば ok。bucketPolicy なら、doc を resource の Bucket Policy として含めて評価する。
// unresolved が空でなければ、aws を呼ばずにそれを理由とする NG にする。
type simRow struct {
	want         string
	role         string
	resource     string
	context      []string
	actions      []string
	bucketPolicy bool
	doc          string
	unresolved   string
}

func ctxEntry(name, value, typ string) string {
	return "ContextKeyName=" + name + ",ContextKeyValues=" + value + ",ContextKeyType=" + typ
}

// ctxList は、値の一覧を受ける条件キーの --context-entries の1つ。値は AWS CLI の shorthand の一覧として `,` で区切る。
func ctxList(name, first string, rest ...string) string {
	return ctxEntry(name, strings.Join(append([]string{first}, rest...), ","), "stringList")
}

// ctxSummary は、ctxEntry の形の entry を `<key>=<values>` に縮めたもの。その形でなければ entry をそのまま返す。
func ctxSummary(entry string) string {
	rest, ok := strings.CutPrefix(entry, "ContextKeyName=")
	if !ok {
		return entry
	}

	name, rest, ok := strings.Cut(rest, ",ContextKeyValues=")
	if !ok {
		return entry
	}

	i := strings.LastIndex(rest, ",ContextKeyType=")
	if i < 0 {
		return entry
	}

	return name + "=" + rest[:i]
}

func sim(want, role, resource string, actions ...string) simRow {
	return simRow{want: want, role: role, resource: resource, actions: actions}
}

func simCtx(want, role, resource string, context []string, actions ...string) simRow {
	return simRow{want: want, role: role, resource: resource, context: context, actions: actions}
}

// desc は、行の説明。context を与える行は、同じ role と resource の行と区別できるように context を `[...]` で添える。
func (r simRow) desc() string {
	subject := r.role + " " + r.resource
	if r.bucketPolicy {
		subject += "（Bucket Policy を含む）"
	}

	if len(r.context) > 0 {
		parts := make([]string, 0, len(r.context))
		for _, c := range r.context {
			parts = append(parts, ctxSummary(c))
		}

		subject += " [" + strings.Join(parts, "; ") + "]"
	}

	return fmt.Sprintf("%s: %d 件が %s", subject, len(r.actions), r.want)
}

func (r simRow) args(n names) []string {
	role := n.roleARN(r.role)
	args := []string{"iam", "simulate-principal-policy", "--policy-source-arn", role}

	if r.bucketPolicy {
		// Bucket Policy は aws:PrincipalArn で書き手を比べるので、その値を role の ARN にする。
		return append(append(args, "--caller-arn", role, "--resource-arns", r.resource, "--resource-policy", r.doc,
			"--resource-owner", n.account, "--context-entries",
			ctxEntry("aws:PrincipalArn", role, "string"), ctxEntry("aws:SecureTransport", "true", "boolean"),
			"--action-names"), r.actions...)
	}

	args = append(args, "--resource-arns", r.resource)
	if len(r.context) > 0 {
		args = append(append(args, "--context-entries"), r.context...)
	}

	return append(append(args, "--action-names"), r.actions...)
}

// simCheck は、行の評価の結果が、要求した actions と同じ action を同じ数だけ返し（多重集合として一致）、すべて want
// であれば ok とする確認。context を与えた行（context か Bucket Policy を持つ行）は、どの結果にも足りない context が
// 無いことも求める。与えない行は、足りない context を NG の理由に添えるだけで、判定には使わない。
func simCheck(r simRow, n names) check {
	return check{desc: r.desc(), eval: func(q *query) (verdict, error) {
		if r.unresolved != "" {
			return verdict{reason: r.unresolved}, nil
		}

		v, reason, err := q.object(r.args(n)...)
		if err != nil || reason != "" {
			return verdict{reason: reason}, err
		}

		results, ok := items(get(v, "EvaluationResults"))
		if !ok {
			return verdict{reason: "応答を読めない"}, nil
		}

		var got, bad, missing []string

		for _, e := range results {
			action, ok1 := at(e, "EvalActionName").(string)
			decision, ok2 := at(e, "EvalDecision").(string)

			m, ok3 := get(e, "MissingContextValues")
			list, isArray := m.([]any)
			keys, ok4 := strs(list)

			if m != nil && !isArray {
				ok4 = false
			}

			if !ok1 || !ok2 || !ok3 || !ok4 {
				return verdict{reason: "応答を読めない"}, nil
			}

			got = append(got, action)
			missing = append(missing, keys...)

			if decision != r.want {
				bad = append(bad, action+"="+decision)
			}
		}

		returned := strings.Join(got, " ")
		if returned == "" {
			returned = "なし"
		}

		givesContext := len(r.context) > 0 || r.bucketPolicy
		note := ""

		if !givesContext && len(missing) > 0 {
			note = "、足りない context: " + strings.Join(missing, " ")
		}

		switch {
		case givesContext && len(missing) > 0:
			return verdict{reason: "評価に足りない context: " + strings.Join(missing, " ")}, nil
		case !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(r.actions))):
			return verdict{reason: fmt.Sprintf("返った action が要求と一致しない（返った件数 %d / %d: %s）", len(got), len(r.actions), returned) + note}, nil
		case len(bad) > 0:
			return verdict{reason: "違う判定: " + strings.Join(bad, " ") + note}, nil
		}

		return verdict{ok: true}, nil
	}}
}

// gate は、条件つきの確認が前提とする managed policy の付き方。
type gate int

const (
	gateNone gate = iota
	gateGovernance
	gateDelegatedAdmin
	gateWorkload
)

type gateState int

const (
	gateAttached gateState = iota + 1
	gateAbsent
	// gateUnknown は、読めないか、想定しない付き方であること。NG として数える。
	gateUnknown
)

type gateResult struct {
	state gateState
	ng    check
}

// section は、permissions の出力の1区分。gated は gate の policy が付いているときだけ評価する。付いていなければ
// absentNote を出し、付き方を決められなければ、reportGate の区分では gate の NG を、それ以外では unknownNote を出す。
type section struct {
	heading     string
	lead        []step
	rows        []simRow
	gate        gate
	gated       []simRow
	absentNote  string
	unknownNote string
	reportGate  bool
}

// attachedNames は、role に付く managed policy の名前（並べ替えたもの）。読めなければ理由を返す。
func attachedNames(q *query, roleName string) ([]string, string, error) {
	v, reason, err := q.object("iam", "list-attached-role-policies", "--role-name", roleName)
	if err != nil || reason != "" {
		return nil, reason, err
	}

	got, ok := policyNames(v, "AttachedPolicies")
	if !ok {
		return nil, "応答を読めない", nil
	}

	return got, "", nil
}

// governanceGate は、apply-identity の role に統制の policy・メンバーの role の policy と、委任管理者の policy が付いているか。
// 1回の読み取りから2つを決める。読めなければどちらも unknown で、NG は統制の側だけが持つ。
func governanceGate(q *query, n names) (gateResult, gateResult, error) {
	got, reason, err := attachedNames(q, n.roleName(n.applyIdentity))
	if err != nil {
		return gateResult{}, gateResult{}, err
	}

	if reason != "" {
		unknown := gateResult{state: gateUnknown, ng: fixedCheck(n.applyIdentity+": list-attached-role-policies を読めないので、統制の policy とメンバーの role の policy が付いているかを決められない", verdict{reason: reason})}

		return unknown, gateResult{state: gateUnknown}, nil
	}

	delegated := gateResult{state: gateAbsent}
	if slices.Contains(got, n.roleName(n.applyIdentity+"-governance-2")) {
		delegated = gateResult{state: gateAttached}
	}

	// StackSet の行は members の policy に、ほかの行は統制の policy に依る。片方だけなら、どちらの行も確かめられない。
	governance := slices.Contains(got, n.roleName(n.applyIdentity+"-governance"))
	members := slices.Contains(got, n.roleName(n.applyIdentity+"-members"))

	switch {
	case governance && members:
		return gateResult{state: gateAttached}, delegated, nil
	case !governance && !members:
		return gateResult{state: gateAbsent}, delegated, nil
	default:
		return gateResult{state: gateUnknown, ng: fixedCheck(n.applyIdentity+" に、統制の policy とメンバーの role の policy の片方だけが付いている", verdict{reason: strings.Join(got, "、")})}, delegated, nil
	}
}

func workloadGate(q *query, n names) (gateResult, error) {
	got, reason, err := attachedNames(q, n.roleName(n.apply))
	if err != nil {
		return gateResult{}, err
	}

	want := []string{n.roleName(n.apply + "-workload"), n.roleName(n.apply + "-workload-2"), n.roleName(n.apply + "-workload-read")}

	switch {
	case reason != "":
		return gateResult{state: gateUnknown, ng: fixedCheck(n.apply+": list-attached-role-policies を読めないので、workload の3つの policy が付いているかを決められない", verdict{reason: reason})}, nil
	case slices.Equal(got, want):
		return gateResult{state: gateAttached}, nil
	case len(got) == 0:
		return gateResult{state: gateAbsent}, nil
	default:
		return gateResult{state: gateUnknown, ng: fixedCheck("apply の role に付く managed policy が workload の3つの policy ではない", verdict{reason: strings.Join(got, "、")})}, nil
	}
}

// permissionSections は、permissions の確認の表。sk は state の鍵の ARN で、skReason が空でなければ読めなかった理由。
func permissionSections(n names, sk, skReason string) []section {
	reads := []string{"s3:GetBucketAcl", "s3:GetBucketCORS", "s3:GetBucketWebsite", "s3:GetAccelerateConfiguration", "s3:GetBucketRequestPayment",
		"s3:GetBucketLogging", "s3:GetLifecycleConfiguration", "s3:GetReplicationConfiguration", "s3:GetBucketObjectLockConfiguration",
		"s3:GetBucketTagging", "s3:GetBucketPolicy", "s3:GetBucketVersioning", "s3:GetEncryptionConfiguration",
		"s3:GetBucketPublicAccessBlock", "s3:GetBucketOwnershipControls"}
	with := func(a ...string) []string { return append(slices.Clone(reads), a...) }
	plus := func(head []string, a ...string) []string { return append(append(head, reads...), a...) }

	plan, apply, ai, ui, unlock := n.plan, n.apply, n.applyIdentity, n.unlockIdentity, n.unlock
	b := "arn:aws:s3:::" + n.bucket
	key := b + "/identity/terraform.tfstate"
	pb := "arn:aws:s3:::" + n.planBucket
	ab := "arn:aws:s3:::" + n.applyBucket
	iam := "arn:aws:iam::" + n.account
	other := "arn:aws:kms:" + n.region + ":" + n.account + ":key/00000000-0000-0000-0000-0000000000a0"
	policyARN := func(arn string) []string { return []string{ctxEntry("iam:PolicyARN", arn, "arn")} }
	bypass := []string{ctxEntry("kms:BypassPolicyLockoutSafetyCheck", "true", "boolean")}
	manage := []string{"iam:CreatePolicy", "iam:CreatePolicyVersion", "iam:DeletePolicyVersion", "iam:SetDefaultPolicyVersion", "iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyVersions"}

	var lead []step
	if skReason != "" {
		lead = []step{checkStep(fixedCheck("state の鍵の ARN（"+n.keyAlias+"）を読めない", verdict{reason: skReason}))}
	}

	gp := n.policyARN(ai + "-governance")
	gp2 := n.policyARN(ai + "-governance-2")
	orgAccount := "arn:aws:organizations::" + n.account + ":account/o-exampleorg1/111111111111"
	principal := func(svc string) []string { return []string{ctxEntry("organizations:ServicePrincipal", svc, "string")} }
	ctslr := iam + ":role/aws-service-role/cloudtrail.amazonaws.com/AWSServiceRoleForCloudTrail"
	delegatedRegional := []string{"guardduty:EnableOrganizationAdminAccount", "guardduty:DisableOrganizationAdminAccount", "inspector2:EnableDelegatedAdminAccount", "inspector2:DisableDelegatedAdminAccount"}
	delegatedLists := []string{"guardduty:ListOrganizationAdminAccounts", "inspector2:ListDelegatedAdminAccounts"}
	hub := func(r string) string { return "arn:aws:securityhub:" + r + ":" + n.account + ":hub/default" }
	mp := n.policyARN(ai + "-manage")
	mbp := n.policyARN(ai + "-members")
	// 管理アカウントの OrganizationAccountAccessRole の ARN。引き受けの Resource がアカウントの wildcard へ広がったことを見つける。
	// 立ち上げの中のメンバーの引き受けは、アカウントを入力に持たないので確かめない（Policy Test の PLAN-ASSUME-* が見る）。
	oaar := iam + ":role/OrganizationAccountAccessRole"
	bg := iam + ":role/break-glass/" + n.project + "-break-glass"
	admin := "arn:aws:iam::aws:policy/AdministratorAccess"
	slr := iam + ":role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
	serviceName := func(svc string) []string { return []string{ctxEntry("iam:AWSServiceName", svc, "string")} }
	ss, _, _ := stackSetARNs(n, n.project+"-break-glass")

	wp := n.policyARN(apply + "-workload")
	wrp := n.policyARN(apply + "-workload-read")
	w2p := n.policyARN(apply + "-workload-2")
	bp := n.policyARN("boundary-ops-notification")
	topic := "arn:aws:sns:" + n.region + ":" + n.account + ":" + n.prefix + "-ops-notification"
	rec := "arn:aws:config:" + n.region + ":" + n.account + ":configuration-recorder/" + n.prefix + "/abc"
	an := "arn:aws:access-analyzer:" + n.region + ":" + n.account + ":analyzer/" + n.prefix + "-external"
	cslr := iam + ":role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig"
	// リージョンごとの構成物は repository-wide/ADR-0209 の集合に限る。集合の外は opt-in の af-south-1 で見る。
	optIn := "af-south-1"
	recOut := "arn:aws:config:" + optIn + ":" + n.account + ":configuration-recorder/" + n.prefix + "/abc"
	inCtx := []string{ctxEntry("aws:RequestedRegion", n.region, "string")}
	outCtx := []string{ctxEntry("aws:RequestedRegion", optIn, "string")}
	ec2Ctx := func(r string) []string {
		return []string{ctxEntry("ec2:Region", r, "string"), ctxEntry("aws:RequestedRegion", r, "string")}
	}

	wk := "arn:aws:kms:" + n.region + ":" + n.account + ":key/00000000-0000-0000-0000-0000000000a1"
	keyCtx := func(spec, usage string) []string {
		return []string{
			ctxEntry("kms:KeySpec", spec, "string"), ctxEntry("kms:KeyUsage", usage, "string"),
			ctxEntry("kms:KeyOrigin", "AWS_KMS", "string"), ctxEntry("kms:MultiRegion", "false", "boolean"),
		}
	}
	or := iam + ":role/" + n.prefix + "-ops-heartbeat"
	boundary := []string{ctxEntry("iam:PermissionsBoundary", bp, "arn")}
	passed := func(svc string) []string { return []string{ctxEntry("iam:PassedToService", svc, "string")} }

	budget := "arn:aws:budgets::" + n.account + ":budget/" + n.prefix + "-x"
	monitor := "arn:aws:ce::" + n.account + ":anomalymonitor/x"
	subscription := "arn:aws:ce::" + n.account + ":anomalysubscription/x"
	bootstrapTag := []string{ctxEntry("aws:ResourceTag/"+n.project+":bootstrap", "true", "string")}

	lock := key + ".tflock"
	wkey := b + "/workload/terraform.tfstate"
	wlock := wkey + ".tflock"
	prefix := func(p string) []string { return []string{ctxEntry("s3:prefix", p, "string")} }

	return []section{
		{heading: "# S3 と IAM", rows: []simRow{
			sim(allowed, plan, b, reads...),
			sim(allowed, ai, b, with("s3:PutBucketPolicy", "s3:PutLifecycleConfiguration")...),
			sim(allowed, ai, key, "s3:PutObject"),
			sim(allowed, ai, n.roleARN(plan), "iam:PutRolePolicy", "iam:UpdateAssumeRolePolicy"),
			sim(allowed, plan, pb, reads...),
			sim(allowed, plan, pb+"/pr/1/x", "s3:PutObject"),
			sim(allowed, ai, pb, with("s3:PutLifecycleConfiguration")...),
			sim(allowed, ai, pb+"/apply/1/x", "s3:PutObject"),
			sim(allowed, plan, ab, plus([]string{"s3:ListBucket"})...),
			sim(allowed, ai, ab, plus([]string{"s3:CreateBucket", "s3:ListBucket"}, "s3:PutBucketPolicy", "s3:PutLifecycleConfiguration")...),
			sim(implicitDeny, ai, ab, "s3:DeleteBucket"),
			sim(implicitDeny, plan, ab, "s3:CreateBucket", "s3:PutBucketPolicy"),
			sim(allowed, apply, b, "s3:ListBucket", "s3:GetEncryptionConfiguration"),
			sim(implicitDeny, plan, key, "s3:PutObject"),
			sim(implicitDeny, plan, n.roleARN(plan), "iam:PutRolePolicy"),
			sim(implicitDeny, plan, pb+"/pr/1/x", "s3:DeleteObject", "s3:DeleteObjectVersion"),
			sim(implicitDeny, plan, pb+"/x", "s3:PutObject"),
			sim(implicitDeny, ai, pb+"/pr/1/x", "s3:PutObject", "s3:DeleteObject"),
			sim(implicitDeny, ai, pb+"/apply/1/x", "s3:DeleteObject", "s3:DeleteObjectVersion"),
			sim(explicitDeny, apply, n.roleARN(ai), "iam:PutRolePolicy", "iam:UpdateAssumeRolePolicy", "sts:AssumeRole"),
			sim(explicitDeny, apply, b, "s3:PutBucketPolicy", "s3:DeleteBucketPolicy", "s3:PutLifecycleConfiguration", "s3:PutBucketTagging", "s3:TagResource"),
			sim(explicitDeny, apply, key, "s3:PutObject", "s3:DeleteObject"),
			sim(explicitDeny, apply, pb, "s3:PutBucketPolicy", "s3:PutLifecycleConfiguration", "s3:GetBucketPolicy"),
			sim(explicitDeny, apply, pb+"/pr/1/x", "s3:PutObject", "s3:DeleteObjectVersion"),
			sim(allowed, apply, ab+"/x", "s3:PutObject"),
			sim(explicitDeny, apply, ab+"/x", "s3:GetObject", "s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObjectAcl"),
			sim(explicitDeny, apply, ab, "s3:ListBucket", "s3:GetBucketPolicy", "s3:PutBucketPolicy", "s3:PutLifecycleConfiguration", "s3:DeleteBucket"),
		}},
		{heading: "# state の鍵", lead: lead, rows: []simRow{
			sim(allowed, ai, sk, "kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags", "kms:EnableKeyRotation", "kms:PutKeyPolicy"),
			sim(allowed, ai, "*", "kms:ListAliases"),
			sim(allowed, plan, sk, "kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"),
			sim(allowed, plan, "*", "kms:ListAliases"),
			sim(allowed, apply, "*", "kms:ListAliases"),
			sim(implicitDeny, ai, "*", "kms:CreateKey"),
			sim(implicitDeny, ai, sk, "kms:CreateAlias", "kms:ScheduleKeyDeletion", "kms:DisableKey", "kms:DisableKeyRotation", "kms:EnableKey", "kms:UpdateKeyDescription", "kms:TagResource", "kms:Decrypt"),
			sim(implicitDeny, ai, other, "kms:DescribeKey", "kms:PutKeyPolicy"),
			sim(implicitDeny, plan, sk, "kms:PutKeyPolicy", "kms:Decrypt", "kms:GenerateDataKey"),
			sim(implicitDeny, plan, other, "kms:DescribeKey"),
			sim(implicitDeny, apply, sk, "kms:Decrypt", "kms:GenerateDataKey"),
			sim(explicitDeny, apply, sk, "kms:CancelKeyDeletion", "kms:CreateAlias", "kms:DeleteAlias", "kms:DescribeKey", "kms:DisableKey", "kms:DisableKeyRotation",
				"kms:EnableKey", "kms:EnableKeyRotation", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListGrants", "kms:ListKeyPolicies", "kms:ListResourceTags",
				"kms:PutKeyPolicy", "kms:RevokeGrant", "kms:RotateKeyOnDemand", "kms:ScheduleKeyDeletion", "kms:TagResource", "kms:UntagResource", "kms:UpdateAlias", "kms:UpdateKeyDescription"),
			simCtx(explicitDeny, ai, "*", bypass, "kms:PutKeyPolicy"),
		}},
		{
			heading: "# 統制の構成物と " + ai + " の policy（deployment-identity/ADR-0201 決定3・5・7・22）",
			rows: []simRow{
				sim(allowed, ai, gp, manage...),
				sim(implicitDeny, ai, iam+":policy/other", "iam:CreatePolicy", "iam:CreatePolicyVersion", "iam:GetPolicy"),
				simCtx(allowed, ai, n.roleARN(ai), policyARN(gp), "iam:AttachRolePolicy"),
				simCtx(implicitDeny, ai, n.roleARN(ai), policyARN(admin), "iam:AttachRolePolicy"),
				sim(allowed, ai, mp, manage...),
				simCtx(allowed, ai, n.roleARN(ai), policyARN(mp), "iam:AttachRolePolicy"),
				simCtx(implicitDeny, ai, n.roleARN(apply), policyARN(mp), "iam:AttachRolePolicy"),
				sim(allowed, plan, mp, "iam:GetPolicy", "iam:GetPolicyVersion"),
				sim(explicitDeny, apply, mp, "iam:CreatePolicyVersion"),
				sim(allowed, ai, mbp, manage...),
				simCtx(allowed, ai, n.roleARN(ai), policyARN(mbp), "iam:AttachRolePolicy"),
				sim(allowed, plan, mbp, "iam:GetPolicy", "iam:GetPolicyVersion"),
				sim(explicitDeny, apply, mbp, "iam:CreatePolicyVersion"),
				// plan の role は立ち上げの読み取りの role だけを引き受ける。apply の role は引き受けない（deployment-identity/ADR-0201 決定22）。
				sim(implicitDeny, plan, oaar, "sts:AssumeRole"),
				sim(implicitDeny, apply, oaar, "sts:AssumeRole"),
				sim(allowed, plan, "*", "organizations:DescribeOrganization", "organizations:ListRoots", "organizations:ListAccounts", "iam:ListOrganizationsFeatures", "iam:GetAccountPasswordPolicy"),
				sim(allowed, plan, gp, "iam:GetPolicy", "iam:GetPolicyVersion"),
				sim(implicitDeny, plan, "*", "organizations:CreateOrganization", "iam:UpdateAccountPasswordPolicy"),
				sim(explicitDeny, apply, "*", "organizations:CreateOrganization", "organizations:AttachPolicy", "organizations:LeaveOrganization", "account:CloseAccount",
					"iam:EnableOrganizationsRootSessions", "iam:UpdateAccountPasswordPolicy", "sso:CreatePermissionSet", "sso:AttachManagedPolicyToPermissionSet",
					"sso:DeleteAccountAssignment", "sso:PutPermissionsPolicy", "sts:AssumeRoot"),
				sim(implicitDeny, apply, "*", "sso:DescribePermissionSet", "sso:GetPermissionSet", "sso:ListAccountAssignments"),
				sim(explicitDeny, apply, bg, "iam:UpdateAssumeRolePolicy", "iam:AttachRolePolicy"),
				sim(explicitDeny, apply, gp, "iam:CreatePolicyVersion"),
				sim(explicitDeny, apply, ss, "cloudformation:CreateStackInstances", "cloudformation:UpdateStackInstances", "cloudformation:DeleteStackInstances"),
			},
			gate: gateGovernance,
			gated: append([]simRow{
				sim(allowed, ai, "*", "organizations:CreateOrganization", "organizations:DescribeOrganization", "iam:UpdateAccountPasswordPolicy"),
				simCtx(allowed, ai, bg, policyARN(admin), "iam:AttachRolePolicy"),
				sim(implicitDeny, ai, "*", "organizations:DeleteOrganization"),
				sim(implicitDeny, ai, bg, "iam:DeleteRole"),
			}, stackInstanceRows(n, otherRegion(n))...),
			absentNote: "  統制の policy とメンバーの role の policy がまだ付いていないので、作成と設定の権限は確かめない（2つの policy を作って付ける apply の後にもう一度走らせる）",
			reportGate: true,
		},
		{
			heading: "# 委任管理者の指定（deployment-identity/ADR-0201 決定7・21、repository-wide/ADR-0209 決定4）",
			rows: []simRow{
				sim(allowed, ai, gp2, manage...),
				simCtx(allowed, ai, n.roleARN(ai), policyARN(gp2), "iam:AttachRolePolicy"),
				sim(allowed, plan, "*", "organizations:ListDelegatedAdministrators"),
				simCtx(allowed, plan, "*", inCtx, delegatedLists...),
				simCtx(explicitDeny, plan, "*", outCtx, delegatedLists...),
				simCtx(allowed, plan, hub(n.region), inCtx, "securityhub:ListOrganizationAdminAccounts"),
				simCtx(explicitDeny, plan, hub(optIn), outCtx, "securityhub:ListOrganizationAdminAccounts"),
				simCtx(implicitDeny, plan, "*", inCtx, "guardduty:EnableOrganizationAdminAccount", "inspector2:EnableDelegatedAdminAccount"),
				sim(explicitDeny, apply, orgAccount, "organizations:RegisterDelegatedAdministrator", "organizations:DeregisterDelegatedAdministrator"),
				sim(explicitDeny, apply, gp2, "iam:CreatePolicyVersion"),
			},
			gate: gateDelegatedAdmin,
			gated: []simRow{
				simCtx(allowed, ai, orgAccount, principal("guardduty.amazonaws.com"), "organizations:RegisterDelegatedAdministrator", "organizations:DeregisterDelegatedAdministrator"),
				simCtx(allowed, ai, orgAccount, principal("config.amazonaws.com"), "organizations:RegisterDelegatedAdministrator"),
				simCtx(implicitDeny, ai, orgAccount, principal("sso.amazonaws.com"), "organizations:RegisterDelegatedAdministrator"),
				simCtx(allowed, ai, "*", principal("inspector2.amazonaws.com"), "organizations:EnableAWSServiceAccess"),
				simCtx(implicitDeny, ai, "*", principal("config.amazonaws.com"), "organizations:EnableAWSServiceAccess", "organizations:DisableAWSServiceAccess"),
				simCtx(allowed, ai, "*", inCtx, delegatedRegional...),
				simCtx(explicitDeny, ai, "*", outCtx, delegatedRegional...),
				simCtx(allowed, ai, hub(n.region), inCtx, "securityhub:EnableOrganizationAdminAccount", "securityhub:DisableOrganizationAdminAccount"),
				simCtx(explicitDeny, ai, hub(optIn), outCtx, "securityhub:EnableOrganizationAdminAccount"),
				simCtx(allowed, ai, "*", principal("securityhub.amazonaws.com"), "organizations:EnableAWSServiceAccess"),
				sim(allowed, ai, "*", "cloudtrail:RegisterOrganizationDelegatedAdmin", "cloudtrail:DeregisterOrganizationDelegatedAdmin"),
				simCtx(allowed, ai, ctslr, serviceName("cloudtrail.amazonaws.com"), "iam:CreateServiceLinkedRole"),
				simCtx(implicitDeny, ai, ctslr, serviceName("guardduty.amazonaws.com"), "iam:CreateServiceLinkedRole"),
			},
			absentNote:  "  委任管理者の policy がまだ付いていないので、指定と解除の権限は確かめない（委任管理者の policy を作って付ける apply の後にもう一度走らせる）",
			unknownNote: "  apply-identity の role の managed policy を確かめられないので（上の NG）、委任管理者の指定と解除の権限は確かめない",
		},
		{heading: "# Chatbot の service-linked role（deployment-identity/ADR-0201 決定3）", rows: []simRow{
			simCtx(allowed, ai, slr, serviceName("management.chatbot.amazonaws.com"), "iam:CreateServiceLinkedRole"),
			simCtx(implicitDeny, ai, slr, serviceName("config.amazonaws.com"), "iam:CreateServiceLinkedRole"),
			sim(allowed, ai, slr, "iam:GetRole", "iam:UpdateRole"),
			sim(implicitDeny, ai, slr, "iam:DeleteServiceLinkedRole", "iam:TagRole", "iam:UntagRole", "iam:UpdateRoleDescription", "iam:ListRoleTags"),
			sim(allowed, plan, slr, "iam:GetRole"),
			sim(implicitDeny, plan, slr, "iam:UpdateRole", "iam:CreateServiceLinkedRole"),
		}},
		{
			heading: "# workload の権限の policy と permissions boundary（deployment-identity/ADR-0201 決定5・13-17）",
			rows: []simRow{
				sim(allowed, ai, wp, manage...),
				sim(allowed, ai, wrp, manage...),
				sim(allowed, ai, w2p, manage...),
				sim(allowed, ai, bp, manage...),
				simCtx(allowed, ai, n.roleARN(apply), policyARN(wp), "iam:AttachRolePolicy"),
				simCtx(allowed, ai, n.roleARN(apply), policyARN(wrp), "iam:AttachRolePolicy"),
				simCtx(allowed, ai, n.roleARN(apply), policyARN(w2p), "iam:AttachRolePolicy"),
				simCtx(implicitDeny, ai, n.roleARN(apply), policyARN(bp), "iam:AttachRolePolicy"),
				// plan の role に付けてよいのは読み取りの policy だけ。
				simCtx(allowed, ai, n.roleARN(plan), policyARN(wrp), "iam:AttachRolePolicy"),
				simCtx(implicitDeny, ai, n.roleARN(plan), policyARN(wp), "iam:AttachRolePolicy"),
				simCtx(implicitDeny, ai, n.roleARN(plan), policyARN(w2p), "iam:AttachRolePolicy"),
				sim(allowed, plan, wp, "iam:GetPolicy", "iam:GetPolicyVersion"),
				sim(allowed, plan, wrp, "iam:GetPolicy", "iam:GetPolicyVersion"),
				sim(allowed, plan, "arn:aws:sns:us-east-1:"+n.account+":"+n.prefix+"-ops-notification-us-east-1", "sns:GetTopicAttributes", "sns:ListTagsForResource"),
				sim(implicitDeny, plan, "arn:aws:sns:"+otherRegion(n)+":"+n.account+":"+n.prefix+"-ops-notification", "sns:GetTopicAttributes"),
				sim(allowed, plan, topic, "sns:GetTopicAttributes", "sns:ListTagsForResource"),
				sim(implicitDeny, plan, topic, "sns:SetTopicAttributes"),
				simCtx(allowed, plan, wk, keyCtx("SYMMETRIC_DEFAULT", "ENCRYPT_DECRYPT"), "kms:DescribeKey"),
				simCtx(implicitDeny, plan, wk, keyCtx("RSA_2048", "SIGN_VERIFY"), "kms:DescribeKey"),
				sim(explicitDeny, apply, wp, "iam:CreatePolicyVersion", "iam:DeletePolicy"),
				sim(explicitDeny, apply, wrp, "iam:CreatePolicyVersion", "iam:DeletePolicy"),
				sim(explicitDeny, apply, bp, "iam:CreatePolicyVersion", "iam:DeletePolicy"),
				sim(explicitDeny, apply, n.roleARN("other"), "iam:CreateRole", "iam:PassRole"),
				sim(explicitDeny, apply, "*", "iam:DeleteRolePermissionsBoundary"),
				simCtx(explicitDeny, apply, "*", bypass, "kms:CreateKey", "kms:PutKeyPolicy"),
			},
			gate: gateWorkload,
			gated: []simRow{
				simCtx(allowed, apply, or, boundary, "iam:CreateRole", "iam:PutRolePolicy", "iam:DeleteRole"),
				sim(implicitDeny, apply, or, "iam:CreateRole", "iam:PutRolePolicy"),
				simCtx(implicitDeny, apply, iam+":role/"+n.prefix+"-OPS-heartbeat", boundary, "iam:CreateRole"),
				simCtx(allowed, apply, or, passed("scheduler.amazonaws.com"), "iam:PassRole"),
				simCtx(implicitDeny, apply, or, passed("lambda.amazonaws.com"), "iam:PassRole"),
				sim(allowed, apply, topic, "sns:CreateTopic", "sns:SetTopicAttributes"),
				sim(implicitDeny, apply, topic, "sns:DeleteTopic", "sns:Subscribe"),
				sim(implicitDeny, apply, wk, "kms:ScheduleKeyDeletion"),
				sim(allowed, apply, slr, "iam:GetRole"),
				// account-baseline の構成物（決定13・17）。削除と停止は与えず、password policy の更新は admin で拒む（決定5）。
				simCtx(allowed, apply, rec, inCtx, "config:PutConfigurationRecorder", "config:StartConfigurationRecorder", "config:DescribeConfigurationRecorders"),
				simCtx(explicitDeny, apply, recOut, outCtx, "config:PutConfigurationRecorder", "config:DescribeConfigurationRecorders"),
				simCtx(implicitDeny, apply, rec, inCtx, "config:DeleteConfigurationRecorder", "config:StopConfigurationRecorder"),
				simCtx(allowed, apply, an, inCtx, "access-analyzer:CreateAnalyzer", "access-analyzer:GetAnalyzer"),
				simCtx(implicitDeny, apply, an, inCtx, "access-analyzer:DeleteAnalyzer"),
				simCtx(allowed, apply, cslr, passed("config.amazonaws.com"), "iam:PassRole"),
				simCtx(allowed, apply, cslr, serviceName("config.amazonaws.com"), "iam:CreateServiceLinkedRole"),
				sim(implicitDeny, apply, cslr, "iam:DeleteServiceLinkedRole"),
				simCtx(allowed, apply, "*", ec2Ctx(n.region), "ec2:EnableEbsEncryptionByDefault", "ec2:ModifyVpcBlockPublicAccessOptions"),
				simCtx(implicitDeny, apply, "*", ec2Ctx(n.region), "ec2:DisableEbsEncryptionByDefault"),
				simCtx(explicitDeny, apply, "*", ec2Ctx(optIn), "ec2:EnableEbsEncryptionByDefault"),
				simCtx(allowed, apply, "*", inCtx, "config:PutDeliveryChannel", "config:PutRetentionConfiguration", "elasticmapreduce:PutBlockPublicAccessConfiguration"),
				simCtx(explicitDeny, apply, "*", outCtx, "config:PutDeliveryChannel", "elasticmapreduce:PutBlockPublicAccessConfiguration"),
				sim(allowed, apply, "*", "s3:PutAccountPublicAccessBlock"),
				simCtx(implicitDeny, apply, "*", inCtx, "config:DeleteDeliveryChannel", "config:DeleteRetentionConfiguration"),
				simCtx(allowed, plan, rec, inCtx, "config:DescribeConfigurationRecorders", "config:DescribeConfigurationRecorderStatus"),
				simCtx(explicitDeny, plan, recOut, outCtx, "config:DescribeConfigurationRecorders"),
				simCtx(allowed, plan, an, inCtx, "access-analyzer:GetAnalyzer"),
				simCtx(implicitDeny, plan, an, inCtx, "access-analyzer:CreateAnalyzer"),
			},
			absentNote: "  workload の3つの policy がまだ付いていないので、apply の role の workload の権限は確かめない（3つの policy を作って付ける apply の後にもう一度走らせる）",
			reportGate: true,
		},
		{
			heading: "# budgets と Cost Anomaly Detection（deployment-identity/ADR-0201 決定13）",
			rows: []simRow{
				sim(allowed, plan, budget, "budgets:ViewBudget", "budgets:ListTagsForResource"),
				sim(allowed, plan, monitor, "ce:GetAnomalyMonitors", "ce:ListTagsForResource"),
				sim(allowed, plan, subscription, "ce:GetAnomalySubscriptions", "ce:ListTagsForResource"),
				sim(implicitDeny, plan, budget, "budgets:ModifyBudget"),
				sim(implicitDeny, plan, "*", "ce:CreateAnomalyMonitor", "ce:CreateAnomalySubscription"),
				sim(implicitDeny, plan, monitor, "ce:UpdateAnomalyMonitor", "ce:DeleteAnomalyMonitor"),
				sim(implicitDeny, plan, subscription, "ce:UpdateAnomalySubscription", "ce:DeleteAnomalySubscription"),
				// 人が tag を付けた、bootstrap の前提の異常検知は apply の role から変えられない。
				simCtx(explicitDeny, apply, monitor, bootstrapTag, "ce:UpdateAnomalyMonitor", "ce:DeleteAnomalyMonitor", "ce:UntagResource"),
				simCtx(explicitDeny, apply, subscription, bootstrapTag, "ce:UpdateAnomalySubscription", "ce:DeleteAnomalySubscription", "ce:UntagResource"),
			},
			gate: gateWorkload,
			gated: []simRow{
				sim(allowed, apply, budget, "budgets:ModifyBudget"),
				sim(allowed, apply, "*", "ce:CreateAnomalyMonitor", "ce:CreateAnomalySubscription"),
				sim(allowed, apply, monitor, "ce:UpdateAnomalyMonitor", "ce:DeleteAnomalyMonitor"),
				sim(allowed, apply, subscription, "ce:UpdateAnomalySubscription", "ce:DeleteAnomalySubscription"),
			},
			absentNote:  "  workload の3つの policy がまだ付いていないので、apply の role の budgets と Cost Anomaly Detection の作成と変更の権限は確かめない（3つの policy を作って付ける apply の後にもう一度走らせる）",
			unknownNote: "  apply の role の managed policy を確かめられないので（上の NG）、apply の role の budgets と Cost Anomaly Detection の作成と変更の権限は確かめない",
		},
		{heading: "# " + ui + " の role", rows: []simRow{
			simCtx(allowed, ui, b, prefix("identity/terraform.tfstate.tflock"), "s3:ListBucket"),
			sim(allowed, ui, lock, "s3:GetObject", "s3:DeleteObject"),
			simCtx(implicitDeny, ui, b, prefix("identity/terraform.tfstate"), "s3:ListBucket"),
			sim(implicitDeny, ui, lock, "s3:PutObject", "s3:DeleteObjectVersion"),
			sim(implicitDeny, ui, key, "s3:GetObject", "s3:PutObject", "s3:DeleteObject"),
			sim(implicitDeny, ui, sk, "kms:Decrypt"),
		}},
		{heading: "# " + unlock + " の role", rows: []simRow{
			simCtx(allowed, unlock, b, prefix("workload/terraform.tfstate.tflock"), "s3:ListBucket"),
			sim(allowed, unlock, wlock, "s3:GetObject", "s3:DeleteObject"),
			simCtx(implicitDeny, unlock, b, prefix("identity/terraform.tfstate.tflock"), "s3:ListBucket"),
			sim(implicitDeny, unlock, wlock, "s3:PutObject", "s3:DeleteObjectVersion"),
			sim(implicitDeny, unlock, lock, "s3:GetObject", "s3:DeleteObject"),
			sim(implicitDeny, unlock, wkey, "s3:GetObject", "s3:PutObject", "s3:DeleteObject"),
			sim(implicitDeny, unlock, sk, "kms:Decrypt"),
		}},
	}
}

// otherRegion は、provider の region でない region。workload の権限は provider の region と us-east-1 に及び、ほかの
// region には及ばない（ADR-0208 決定4）。
func otherRegion(n names) string {
	if n.region == "us-west-2" {
		return "eu-west-1"
	}

	return "us-west-2"
}

// stackSetARNs は、name の StackSet の stack instance の操作が当たる3つの resource（stackset・stackset-target・type）の
// ARN。stackset-target は region を持たず、type は region が `*` でアカウントを持たない（deployment-identity/ADR-0201 決定4）。
// 末尾の ID は作るまで決まらないので、ダミーにする。
func stackSetARNs(n names, name string) (string, string, string) {
	id := name + ":00000000-0000-0000-0000-0000000000b0"

	return "arn:aws:cloudformation:" + n.region + ":" + n.account + ":stackset/" + id,
		"arn:aws:cloudformation::" + n.account + ":stackset-target/" + id + "/r-abcd",
		"arn:aws:cloudformation:*::type/resource/AWS-IAM-Role"
}

// stackInstanceRows は、apply-identity が break-glass と立ち上げの読み取りの stack instance を provider の region にだけ作り・更新でき、削除
// できず、別の名前の StackSet には及ばないことの行（名前の prefix を共有する名前は、Resource を名前の prefix へ広げた policy を
// 見つける）。1つの要求が3つの resource に当たり、simCheck は返った action を数えるので、resource ごとに行を分ける。
func stackInstanceRows(n names, other string) []simRow {
	ss, st, ty := stackSetARNs(n, n.project+"-break-glass")
	// policy の type の Resource はアカウントが `*` で StackSet の名前を含まないので、type には別の名前の行を持たない。
	xss, xst, _ := stackSetARNs(n, n.project+"-other")
	yss, yst, _ := stackSetARNs(n, n.project+"-break-glass-x")
	// 立ち上げの読み取りの role の StackSet も、同じ文で作り・更新でき、削除できない（organization-baseline/ADR-0203）。
	rss, rst, _ := stackSetARNs(n, n.project+"-bootstrap-read")
	zss, zst, _ := stackSetARNs(n, n.project+"-bootstrap-read-x")
	ai := n.applyIdentity
	inst := []string{"cloudformation:CreateStackInstances", "cloudformation:UpdateStackInstances"}
	// TargetRegion は type の評価にだけ付き、stackset と stackset-target の評価には付かない（実際の要求での観測。
	// Service Authorization Reference は3つとも受けると書く）。付かないキーを context に与える行は、IAM が見ないものを確かめる。
	// type の条件は一覧を受けるキー（ArrayOfString）を provider の region に限る（deployment-identity/ADR-0201 決定4）。
	// provider の region だけの一覧は、条件が StringEquals なら implicitDeny になるので、StringEquals で書いた条件を見つける。
	// 同じ region を2つ並べた一覧は、`,` を含む1つの値として渡ったときに allowed にならない。
	tr := func(first string, rest ...string) []string {
		return []string{ctxList("cloudformation:TargetRegion", first, rest...)}
	}

	upd := "cloudformation:UpdateStackSet"

	return []simRow{
		// キーの無い stackset と stackset-target の行は、条件がこの2つへ戻ったときに implicitDeny になる。
		sim(allowed, ai, ss, inst...),
		sim(allowed, ai, st, inst...),
		simCtx(allowed, ai, ty, tr(n.region), inst...),
		simCtx(allowed, ai, ty, tr(n.region, n.region), inst...),
		simCtx(implicitDeny, ai, ty, tr(n.region, other), inst...),
		simCtx(implicitDeny, ai, ty, tr(other), inst...),
		sim(implicitDeny, ai, ty, inst...),
		sim(allowed, ai, ss, upd),
		sim(allowed, ai, st, upd),
		sim(allowed, ai, ty, upd),
		sim(implicitDeny, ai, ss, "cloudformation:DeleteStackInstances"),
		sim(implicitDeny, ai, xss, inst...),
		sim(implicitDeny, ai, xst, inst...),
		sim(implicitDeny, ai, yss, inst...),
		sim(implicitDeny, ai, yst, inst...),
		sim(implicitDeny, ai, xss, upd),
		sim(implicitDeny, ai, xst, upd),
		sim(implicitDeny, ai, yss, upd),
		sim(implicitDeny, ai, yst, upd),
		sim(allowed, ai, rss, inst...),
		sim(allowed, ai, rst, inst...),
		sim(allowed, ai, rss, upd),
		sim(implicitDeny, ai, rss, "cloudformation:DeleteStackInstances"),
		sim(implicitDeny, ai, zss, inst...),
		sim(implicitDeny, ai, zst, inst...),
		sim(implicitDeny, ai, zss, upd),
		sim(implicitDeny, ai, zst, upd),
	}
}

// permissionsSteps は、CI の role の権限を simulate-principal-policy で評価する確認の表。state の鍵の ARN を読めなければ、
// それを NG として数え、その鍵に対する行は aws を呼ばずに NG にする。
func permissionsSteps(q *query, n names) ([]step, error) {
	sk, skReason, err := stateKeyARN(q, n)
	if err != nil {
		return nil, err
	}

	if skReason != "" {
		sk = "arn:aws:kms:" + n.region + ":" + n.account + ":key/unknown"
	}

	gates := map[gate]gateResult{gateNone: {state: gateAttached}}

	if gates[gateGovernance], gates[gateDelegatedAdmin], err = governanceGate(q, n); err != nil {
		return nil, err
	}

	if gates[gateWorkload], err = workloadGate(q, n); err != nil {
		return nil, err
	}

	var steps []step

	rowSteps := func(rows []simRow) {
		for _, r := range rows {
			if skReason != "" && r.resource == sk {
				r.unresolved = "state の鍵の ARN を読めない"
			}

			steps = append(steps, checkStep(simCheck(r, n)))
		}
	}

	for _, s := range permissionSections(n, sk, skReason) {
		steps = append(append(steps, noteStep(s.heading)), s.lead...)
		rowSteps(s.rows)

		switch g := gates[s.gate]; {
		case s.gate == gateNone:
		case g.state == gateAttached:
			rowSteps(s.gated)
		case g.state == gateAbsent:
			steps = append(steps, skipStep(s.absentNote))
		case s.reportGate:
			steps = append(steps, checkStep(g.ng))
		default:
			steps = append(steps, skipStep(s.unknownNote))
		}
	}

	return steps, nil
}

// referencePolicy は、make tf-policy が dir に書いた reference の plan の、module の出力の Bucket の Bucket Policy の
// 文書。アカウント ID を account に置き換える。読めなければ理由を返す。
// dir の plans.txt に referenceRoot の plan が無ければ、inputs に残っているのは今回の出力ではないので読まない。
func referencePolicy(readFile func(string) ([]byte, error), dir, module, account string) (string, string) {
	list := filepath.Join(dir, "plans.txt")

	lines, err := readFile(list)
	if err != nil {
		return "", list + " を読めない（make tf-policy を走らせてください）: " + err.Error()
	}

	if !slices.ContainsFunc(strings.Split(string(lines), "\n"), func(l string) bool {
		return filepath.Base(strings.TrimSpace(l)) == referenceRoot+".jsonl"
	}) {
		return "", list + " に " + referenceRoot + " の plan が無い（make tf-policy で " + referenceRoot + " の plan が落ちた）"
	}

	referencePlan := filepath.Join(dir, "inputs", referenceRoot+".json")

	b, err := readFile(referencePlan)
	if err != nil {
		return "", referencePlan + " を読めない: " + err.Error()
	}

	var plan map[string]any
	if json.Unmarshal(b, &plan) != nil || plan == nil {
		return "", referencePlan + " を JSON の object として読めない"
	}

	changes, ok := items(get(plan, "resource_changes"))
	if !ok {
		return "", referencePlan + " に resource_changes の配列が無い"
	}

	address := "module.state_backend.module." + module + ".aws_s3_bucket_policy.this"

	var docs []string

	for _, c := range changes {
		if at(c, "address") == address {
			doc, _ := at(c, "change", "after", "policy").(string)
			docs = append(docs, doc)
		}
	}

	if len(docs) != 1 || docs[0] == "" {
		return "", fmt.Sprintf("%s に %s の policy が1つではない（%d 個）", referencePlan, address, len(docs))
	}

	return strings.ReplaceAll(docs[0], referenceAccount, account), ""
}

// outputDocument は、出力の Bucket の Bucket Policy の文書。source が live ならいま Bucket に付いているもの、reference
// なら reference の plan のもの。JSON の object として読めなければ理由を返す。
func outputDocument(e verifyEnv, bucket, module string) (string, string, error) {
	var doc string

	if e.source == sourceReference {
		var reason string
		if doc, reason = referencePolicy(e.readFile, e.referenceDir, module, e.n.account); reason != "" {
			return "", reason, nil
		}
	} else {
		v, reason, err := e.q.object(e.n.s3("get-bucket-policy", bucket)...)
		if err != nil || reason != "" {
			return "", reason, err
		}

		doc, _ = v["Policy"].(string)
	}

	if _, ok := decodeObject([]byte(doc)); !ok {
		return "", "文書を JSON の object として読めない", nil
	}

	return doc, "", nil
}

// outputPolicySteps は、出力の2つの Bucket の Bucket Policy が書き手を拒まず、書き手の外と削除を拒むことの確認
// （state-backend/ADR-0201 決定8）。
func outputPolicySteps(e verifyEnv) ([]step, error) {
	n := e.n

	pdoc, preason, err := outputDocument(e, n.planBucket, "plan_output")
	if err != nil {
		return nil, err
	}

	adoc, areason, err := outputDocument(e, n.applyBucket, "apply_output")
	if err != nil {
		return nil, err
	}

	readable := func(desc, doc, reason string) step {
		if reason != "" {
			return checkStep(fixedCheck(desc, verdict{reason: reason}))
		}

		v, _ := decodeObject([]byte(doc))

		return checkStep(fixedCheck(desc, verdict{ok: hasStatements(v)}))
	}
	rows := func(doc, reason string, rs []simRow) []step {
		out := make([]step, 0, len(rs))

		for _, r := range rs {
			r.bucketPolicy, r.doc = true, doc
			if reason != "" {
				r.unresolved = "Bucket Policy の文書を読めない"
			}

			out = append(out, checkStep(simCheck(r, n)))
		}

		return out
	}
	planRows, applyRows := outputPolicyRows(n)

	steps := []step{
		noteStep("# 文書の出所: " + e.source),
		readable("plan の出力の Bucket の Bucket Policy の文書を読めた", pdoc, preason),
		readable("apply の出力の Bucket の Bucket Policy の文書を読めた", adoc, areason),
		noteStep("# plan の出力の Bucket"),
	}
	steps = append(steps, rows(pdoc, preason, planRows)...)
	steps = append(steps, noteStep("# apply の出力の Bucket"))

	return append(steps, rows(adoc, areason, applyRows)...), nil
}

// outputPolicyRows は、plan と apply の出力の Bucket の Bucket Policy を含めて評価する行。
func outputPolicyRows(n names) ([]simRow, []simRow) {
	pb := "arn:aws:s3:::" + n.planBucket
	ab := "arn:aws:s3:::" + n.applyBucket
	plan, apply, ai := n.plan, n.apply, n.applyIdentity

	return []simRow{
		sim(allowed, plan, pb+"/pr/1/x", "s3:PutObject"),
		sim(allowed, plan, pb+"/branch/1/x", "s3:PutObject"),
		sim(allowed, ai, pb+"/apply/1/x", "s3:PutObject"),
		sim(explicitDeny, plan, pb+"/apply/1/x", "s3:PutObject"),
		sim(explicitDeny, ai, pb+"/pr/1/x", "s3:PutObject"),
		sim(explicitDeny, apply, pb+"/pr/1/x", "s3:PutObject"),
		sim(explicitDeny, plan, pb+"/pr/1/x", "s3:DeleteObject", "s3:DeleteObjectVersion"),
		sim(explicitDeny, ai, pb+"/apply/1/x", "s3:DeleteObject", "s3:DeleteObjectVersion"),
	}, []simRow{
		sim(allowed, apply, ab+"/x", "s3:PutObject"),
		sim(explicitDeny, ai, ab+"/x", "s3:PutObject"),
		sim(explicitDeny, apply, ab+"/x", "s3:DeleteObject", "s3:DeleteObjectVersion"),
	}
}
