package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	// identityMain は、state の版の保持日数の正本を持つ identity root（リポジトリの直下からのパス）。
	identityMain = "env/admin/identity/main.tf"
	oidcHost     = "token.actions.githubusercontent.com"
)

var (
	retentionDaysLine = regexp.MustCompile(`(?m)^[ \t]*state_noncurrent_version_retention_days[ \t]*=([^\n]*)$`)
	// retentionDaysValue は代入の右辺。数字の後ろは空白とコメントだけ。
	retentionDaysValue = regexp.MustCompile(`^[ \t]*([0-9]+)[ \t]*(?:(?:#|//).*)?\r?$`)
)

// stateKeyARN は、state の鍵の ARN。読めなければ理由を返す。
func stateKeyARN(q *query, n names) (string, string, error) {
	v, reason, err := q.object("kms", "describe-key", "--key-id", n.keyAlias)
	if err != nil || reason != "" {
		return "", reason, err
	}

	arn, _ := at(v, "KeyMetadata", "Arn").(string)
	if arn == "" {
		return "", "応答を読めない", nil
	}

	return arn, "", nil
}

// expectWithKey は、state の鍵の ARN と aws の応答が pred を満たせば ok とする確認。
func expectWithKey(desc string, n names, pred func(v map[string]any, key string) bool, args ...string) check {
	return check{desc: desc, eval: func(q *query) (verdict, error) {
		key, reason, err := stateKeyARN(q, n)
		if err != nil || reason != "" {
			return verdict{reason: "state の鍵の ARN を読めない: " + reason}, err
		}

		v, reason, err := q.object(args...)
		if err != nil || reason != "" {
			return verdict{reason: reason}, err
		}

		return verdict{ok: pred(v, key)}, nil
	}}
}

// singleRule は、規則がちょうど1つの配列ならその規則。
func singleRule(v any, keys ...string) (any, bool) {
	rules, ok := items(get(v, keys...))
	if !ok || len(rules) != 1 {
		return nil, false
	}

	return rules[0], true
}

func isSSEC(s string) bool { return s == "SSE-C" }

// kmsEncryption は、既定の暗号化が規則1つで、鍵 key の SSE-KMS、Bucket Key なし、SSE-C を拒むこと。
func kmsEncryption(v map[string]any, key string) bool {
	rule, ok := singleRule(v, "ServerSideEncryptionConfiguration", "Rules")
	if !ok {
		return false
	}

	m, ok := allConds(rule,
		fieldEq("aws:kms", "ApplyServerSideEncryptionByDefault", "SSEAlgorithm"),
		fieldEq(key, "ApplyServerSideEncryptionByDefault", "KMSMasterKeyID"),
		func(s any) (bool, bool) {
			b, ok := get(s, "BucketKeyEnabled")

			return b == nil || b == false, ok
		},
	)

	return ok && m && anyString(rule, isSSEC)
}

func kmsEncryptionSteps(n names) []step {
	buckets := []string{n.bucket, n.planBucket, n.applyBucket}
	steps := make([]step, 0, len(buckets))

	for _, b := range buckets {
		steps = append(steps, checkStep(expectWithKey(b+" の既定の暗号化が state の鍵の SSE-KMS（Bucket Key なし、SSE-C を拒否）",
			n, kmsEncryption, n.s3("get-bucket-encryption", b)...)))
	}

	return steps
}

func stage1Steps(n names) []step {
	return []step{
		checkStep(expectJSON("Public Access Block の4項目が true", func(v map[string]any) bool {
			c, ok := get(v, "PublicAccessBlockConfiguration")

			for _, k := range []string{"BlockPublicAcls", "IgnorePublicAcls", "BlockPublicPolicy", "RestrictPublicBuckets"} {
				x, ok2 := get(c, k)
				if !ok || !ok2 || !truthy(x) {
					return false
				}
			}

			return true
		}, n.s3("get-public-access-block", n.bucket)...)),
		checkStep(expectJSON("所有権が BucketOwnerEnforced", func(v map[string]any) bool {
			return jsonEqual(at(v, "OwnershipControls", "Rules"), `[{"ObjectOwnership":"BucketOwnerEnforced"}]`)
		}, n.s3("get-bucket-ownership-controls", n.bucket)...)),
		checkStep(expectJSON("版管理が Enabled", func(v map[string]any) bool {
			return v["Status"] == "Enabled"
		}, n.s3("get-bucket-versioning", n.bucket)...)),
		checkStep(expectJSON("既定の暗号化が AES256 で SSE-C を拒否（違えば段2 へ進まず PR に記録して止まる）", func(v map[string]any) bool {
			rule, ok := singleRule(v, "ServerSideEncryptionConfiguration", "Rules")
			if !ok {
				return false
			}

			m, ok := fieldEq("AES256", "ApplyServerSideEncryptionByDefault", "SSEAlgorithm")(rule)

			return ok && m && anyString(rule, isSSEC)
		}, n.s3("get-bucket-encryption", n.bucket)...)),
	}
}

// noneMatch は、配列 key の各要素の field が match を満たさないこと。要素の field が文字列でなければ偽。
func noneMatch(v map[string]any, key, field string, match func(string) bool) bool {
	list, ok := items(get(v, key))
	if !ok {
		return false
	}

	for _, e := range list {
		f, ok := get(e, field)
		if !ok {
			return false
		}

		s, isString := f.(string)
		if !isString || match(s) {
			return false
		}
	}

	return true
}

func beforeApplySteps(n names) []step {
	steps := []step{checkStep(expectJSON("OIDC provider（"+oidcHost+"）がまだ無い", func(v map[string]any) bool {
		return noneMatch(v, "OpenIDConnectProviderList", "Arn", func(s string) bool { return strings.HasSuffix(s, "/"+oidcHost) })
	}, "iam", "list-open-id-connect-providers"))}

	for _, r := range n.ciRoles() {
		steps = append(steps, checkStep(expectError("role "+n.roleName(r)+" がまだ無い", "NoSuchEntity", "iam", "get-role", "--role-name", n.roleName(r))))
	}

	steps = append(steps,
		checkStep(expectError("state Bucket に Bucket Policy がまだ無い", "NoSuchBucketPolicy", n.s3("get-bucket-policy", n.bucket)...)),
		checkStep(expectError("state Bucket に lifecycle がまだ無い", "NoSuchLifecycleConfiguration", n.s3("get-bucket-lifecycle-configuration", n.bucket)...)),
		// 在ると、段4 の apply は鍵を作った後の alias の作成で落ち、alias の無い鍵が残る。
		checkStep(expectJSON("state の鍵の alias（"+n.keyAlias+"）がまだ無い", func(v map[string]any) bool {
			list, ok := items(get(v, "Aliases"))
			if !ok {
				return false
			}

			for _, e := range list {
				a, ok := get(e, "AliasName")
				if !ok || a == n.keyAlias {
					return false
				}
			}

			return true
		}, "kms", "list-aliases")),
		// 既に組織を持つ管理アカウントは organization-baseline の unsupported。
		checkStep(expectError("組織がまだ無い", "AWSOrganizationsNotInUseException", "organizations", "describe-organization")),
	)

	for _, u := range []string{"break-glass-1", "break-glass-2"} {
		steps = append(steps, checkStep(expectError("IAM user "+u+" がまだ無い", "NoSuchEntity", "iam", "get-user", "--user-name", u)))
	}

	breakGlass := n.project + "-break-glass"

	steps = append(steps,
		checkStep(expectError("IAM group break-glass がまだ無い", "NoSuchEntity", "iam", "get-group", "--group-name", "break-glass")),
		checkStep(expectError("role "+breakGlass+" がまだ無い", "NoSuchEntity", "iam", "get-role", "--role-name", breakGlass)),
		checkStep(expectError("StackSet "+breakGlass+" がまだ無い", "StackSetNotFoundException", "cloudformation", "describe-stack-set", "--stack-set-name", breakGlass)),
	)

	for _, p := range []string{n.applyIdentity + "-governance", n.applyIdentity + "-governance-2", n.applyIdentity + "-manage", n.apply + "-workload", n.apply + "-workload-read", "boundary-ops-notification"} {
		steps = append(steps, checkStep(expectError("policy "+n.roleName(p)+" がまだ無い", "NoSuchEntity", "iam", "get-policy", "--policy-arn", n.policyARN(p))))
	}

	// 在ると、段4 の apply は import せずに上書きする。
	return append(steps, checkStep(expectError("account password policy がまだ無い", "NoSuchEntity", "iam", "get-account-password-policy")))
}

// policyNames は、応答の配列 key の各要素の PolicyName を並べ替えたもの。読めなければ false。
func policyNames(v map[string]any, key string) ([]string, bool) {
	list, ok := items(get(v, key))
	if !ok {
		return nil, false
	}

	out := make([]any, 0, len(list))

	for _, e := range list {
		p, ok := get(e, "PolicyName")
		if !ok {
			return nil, false
		}

		out = append(out, p)
	}

	names, ok := strs(out)
	if !ok {
		return nil, false
	}

	slices.Sort(names)

	return names, true
}

// tlsDenyPolicy は、Bucket Policy の文がすべて Deny で、TLS でない要求を拒む文が在ること。
func tlsDenyPolicy(v map[string]any) bool {
	stmts, ok := items(get(v, "Statement"))

	return ok && everyStatement(stmts, fieldEq("Deny", "Effect")) &&
		anyStatement(stmts, fieldEq("false", "Condition", "Bool", "aws:SecureTransport"))
}

// executorException は、Bucket Policy の DenyObjectWriteByOthers の aws:PrincipalArn の例外に、suffix で終わる
// ARN が在ること。
func executorException(v map[string]any, suffix string) bool {
	stmts, ok := items(get(v, "Statement"))
	if !ok {
		return false
	}

	found := false

	for _, s := range stmts {
		sid, ok := get(s, "Sid")
		if !ok {
			return false
		}

		if sid != "DenyObjectWriteByOthers" {
			continue
		}

		arns, ok := get(s, "Condition", "ArnNotEquals", "aws:PrincipalArn")
		if !ok {
			return false
		}

		list, ok := strs(arrOf(arns))
		if !ok {
			return false
		}

		found = found || slices.ContainsFunc(list, func(a string) bool { return strings.HasSuffix(a, suffix) })
	}

	return found
}

// noUser は、文書の文字列のどれも IAM user（:user/）を含まないこと。
// 文が無い文書は偽。
func noUser(v map[string]any) bool {
	return hasStatements(v) && !anyString(v, func(s string) bool { return strings.Contains(s, ":user/") })
}

// retentionDays は、identity root の state_noncurrent_version_retention_days の値。ちょうど1か所でなければ理由を返す。
func retentionDays(readFile func(string) ([]byte, error)) (int, string) {
	b, err := readFile(identityMain)
	if err != nil {
		return 0, identityMain + " を読めない: " + err.Error()
	}

	m := retentionDaysLine.FindAllSubmatch(b, -1)
	if len(m) != 1 {
		return 0, fmt.Sprintf("%s の state_noncurrent_version_retention_days の代入が1か所ではない（%d か所）", identityMain, len(m))
	}

	v := retentionDaysValue.FindSubmatch(m[0][1])
	if v == nil {
		return 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"
	}

	days, err := strconv.Atoi(string(v[1]))
	if err != nil {
		return 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"
	}

	return days, ""
}

// lifecycleRule は、lifecycle が規則1つで、全体に掛かり、版の保持 days 日、delete marker の掃除、未完了の
// multipart の中止 1 日であること。
func lifecycleRule(v map[string]any, days int) bool {
	rule, ok := singleRule(v, "Rules")
	if !ok {
		return false
	}

	m, ok := allConds(rule,
		fieldEq("Enabled", "Status"),
		func(s any) (bool, bool) {
			f, ok := get(s, "Filter")
			if !truthy(f) {
				f = map[string]any{}
			}

			return jsonEqual(f, `{}`) || jsonEqual(f, `{"Prefix":""}`), ok
		},
		func(s any) (bool, bool) {
			e, ok := get(s, "Expiration")

			return jsonEqual(e, `{"ExpiredObjectDeleteMarker":true}`), ok
		},
		fieldEq(float64(days), "NoncurrentVersionExpiration", "NoncurrentDays"),
		fieldEq(float64(1), "AbortIncompleteMultipartUpload", "DaysAfterInitiation"),
	)

	return ok && m
}

// keyPolicy は、state の鍵の Key Policy が7文で、アカウントへの委任と、role ごとに限った S3 経由の暗号操作だけで
// あること。
func keyPolicy(v map[string]any, n names) bool {
	stmts, ok := items(get(v, "Statement"))
	if !ok || len(stmts) != 7 {
		return false
	}

	via := "s3." + n.region + ".amazonaws.com"
	sb, pb, ab := "arn:aws:s3:::"+n.bucket, "arn:aws:s3:::"+n.planBucket, "arn:aws:s3:::"+n.applyBucket
	grant := func(role string, actions []string, op string, objects ...string) bool {
		return anyStatement(stmts,
			fieldEq("Allow", "Effect"),
			arrEq([]string{n.roleARN(role)}, false, "Principal", "AWS"),
			arrEq(actions, len(actions) > 1, "Action"),
			arrEq(objects, true, "Condition", op, "kms:EncryptionContext:aws:s3:arn"),
			fieldEq(via, "Condition", "StringEquals", "kms:ViaService"),
		)
	}
	rw := []string{"kms:Decrypt", "kms:GenerateDataKey"}
	rd := []string{"kms:Decrypt"}

	return anyStatement(stmts, fieldEq("Allow", "Effect"), fieldEq("arn:aws:iam::"+n.account+":root", "Principal", "AWS")) &&
		grant(n.plan, rw, "StringLike", pb+"/pr/*", pb+"/branch/*") &&
		grant(n.plan, rd, "StringEquals", sb+"/identity/terraform.tfstate", sb+"/workload/terraform.tfstate") &&
		grant(n.applyIdentity, rw, "StringLike", sb+"/identity/terraform.tfstate", sb+"/identity/terraform.tfstate.tflock", pb+"/apply/*") &&
		grant(n.apply, rw, "StringLike", sb+"/workload/terraform.tfstate", sb+"/workload/terraform.tfstate.tflock", ab+"/*") &&
		grant(n.unlockIdentity, rd, "StringEquals", sb+"/identity/terraform.tfstate.tflock") &&
		grant(n.unlock, rd, "StringEquals", sb+"/workload/terraform.tfstate.tflock")
}

func stage4Steps(n names, readFile func(string) ([]byte, error)) []step {
	steps := kmsEncryptionSteps(n)

	steps = append(steps, checkStep(expectJSON("OIDC provider の ClientIDList が sts.amazonaws.com だけ", func(v map[string]any) bool {
		return jsonEqual(v["ClientIDList"], `["sts.amazonaws.com"]`)
	}, "iam", "get-open-id-connect-provider", "--open-id-connect-provider-arn", "arn:aws:iam::"+n.account+":oidc-provider/"+oidcHost)))

	for _, r := range n.ciRoles() {
		inline := strings.ReplaceAll(r, "-", "_")
		steps = append(steps, checkStep(expectJSON("role "+n.roleName(r)+" の inline policy が "+inline+" だけ", func(v map[string]any) bool {
			got, ok := v["PolicyNames"].([]any)

			return ok && len(got) == 1 && got[0] == inline
		}, "iam", "list-role-policies", "--role-name", n.roleName(r))))

		desc, want := "role "+n.roleName(r)+" に managed policy が付いていない", []string{}

		switch r {
		case n.applyIdentity:
			desc = "role " + n.roleName(r) + " に付く managed policy が、統制の policy と委任管理者の policy と manage の policy だけ"
			want = []string{n.roleName(n.applyIdentity + "-governance"), n.roleName(n.applyIdentity + "-governance-2"), n.roleName(n.applyIdentity + "-manage")}
		case n.apply:
			desc = "role " + n.roleName(r) + " に付く managed policy が、workload の3つの policy だけ"
			want = []string{n.roleName(n.apply + "-workload"), n.roleName(n.apply + "-workload-2"), n.roleName(n.apply + "-workload-read")}
		case n.plan:
			desc = "role " + n.roleName(r) + " に付く managed policy が、workload の読み取りの policy だけ"
			want = []string{n.roleName(n.apply + "-workload-read")}
		}

		steps = append(steps, checkStep(expectJSON(desc, func(v map[string]any) bool {
			got, ok := policyNames(v, "AttachedPolicies")

			return ok && slices.Equal(got, want)
		}, "iam", "list-attached-role-policies", "--role-name", n.roleName(r))))
	}

	policy := n.s3("get-bucket-policy", n.bucket)

	steps = append(steps, checkStep(expectDocument("state Bucket の Bucket Policy の文がすべて Deny で、TLS でない要求を拒む文が在る", tlsDenyPolicy, policy...)))

	if n.executor != "" {
		steps = append(steps, checkStep(expectDocument("Bucket Policy の書き込みの例外（DenyObjectWriteByOthers）に、段4 の実行者 "+n.executor+" が在る", func(v map[string]any) bool {
			return executorException(v, ":user/"+n.executor)
		}, policy...)))
	} else {
		steps = append(steps, checkStep(expectDocument("Bucket Policy に IAM user が無い（段3 で段4 の実行者を例外に入れていない）", noUser, policy...)))
	}

	days, reason := retentionDays(readFile)
	shown := strconv.Itoa(days)

	if reason != "" {
		shown = "?"
	}

	lifecycle := "lifecycle が規則1つ（版の保持 " + shown + " 日、delete marker の掃除、未完了の multipart の中止 1 日）"
	if reason != "" {
		steps = append(steps, checkStep(fixedCheck(lifecycle, verdict{reason: reason})))
	} else {
		steps = append(steps, checkStep(expectJSON(lifecycle, func(v map[string]any) bool {
			return lifecycleRule(v, days)
		}, n.s3("get-bucket-lifecycle-configuration", n.bucket)...)))
	}

	steps = append(steps,
		checkStep(expectJSON("state の鍵が有効な対称鍵（単一リージョン、AWS_KMS）", func(v map[string]any) bool {
			m, ok := allConds(v["KeyMetadata"],
				fieldEq("Enabled", "KeyState"), fieldEq("SYMMETRIC_DEFAULT", "KeySpec"), fieldEq("ENCRYPT_DECRYPT", "KeyUsage"),
				fieldEq(false, "MultiRegion"), fieldEq("AWS_KMS", "Origin"))

			return ok && m
		}, "kms", "describe-key", "--key-id", n.keyAlias)),
		checkStep(expectJSON("組織の FeatureSet が ALL", func(v map[string]any) bool {
			return at(v, "Organization", "FeatureSet") == "ALL"
		}, "organizations", "describe-organization")),
	)

	// 資格情報は organization-baseline.md の 3.4 で人が設定する。
	for _, u := range []string{"break-glass-1", "break-glass-2"} {
		steps = append(steps, checkStep(expectJSON("IAM user "+u+" がアクセスキーを持たない", func(v map[string]any) bool {
			return arrayLen(v["AccessKeyMetadata"]) == 0
		}, "iam", "list-access-keys", "--user-name", u)))
	}

	breakGlass := n.project + "-break-glass"

	return append(steps,
		checkStep(expectJSON("role "+breakGlass+" の信頼条件が MFA を求める", func(v map[string]any) bool {
			stmts, ok := items(get(v, "Role", "AssumeRolePolicyDocument", "Statement"))

			return ok && len(stmts) > 0 && everyStatement(stmts, fieldEq("true", "Condition", "Bool", "aws:MultiFactorAuthPresent"))
		}, "iam", "get-role", "--role-name", breakGlass)),
		checkStep(expectJSON("state の鍵の自動 rotation が 365 日", func(v map[string]any) bool {
			return v["KeyRotationEnabled"] == true && v["RotationPeriodInDays"] == float64(365)
		}, "kms", "get-key-rotation-status", "--key-id", n.keyAlias)),
		checkStep(expectDocument("Key Policy が、アカウントへの委任・"+n.plan+" と "+n.applyIdentity+" と "+n.apply+
			" の読み書きする object に限った S3 経由の暗号操作・"+n.plan+" の state の復号・"+n.unlockIdentity+" と "+n.unlock+
			" のそれぞれの lock file の復号の7文だけ", func(v map[string]any) bool {
			return keyPolicy(v, n)
		}, "kms", "get-key-policy", "--key-id", n.keyAlias, "--policy-name", "default")),
	)
}

func stage5Steps(n names) []step {
	return append(kmsEncryptionSteps(n),
		checkStep(expectWithKey("identity root の state が state の鍵の SSE-KMS で書かれている", n, func(v map[string]any, key string) bool {
			return v["ServerSideEncryption"] == "aws:kms" && v["SSEKMSKeyId"] == key
		}, n.s3("head-object", n.bucket, "--key", "identity/terraform.tfstate")...)),
		checkStep(expectDocument("Bucket Policy の例外に IAM user（段4 の実行者）が無い", noUser, n.s3("get-bucket-policy", n.bucket)...)),
	)
}
