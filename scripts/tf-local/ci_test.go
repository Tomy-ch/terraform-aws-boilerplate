package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
)

const (
	account   = "123456789012"
	immutable = "repo:octo-org@123456/octo-repo@456789:environment:admin-plan"
)

func jwtWith(payload string) string {
	enc := base64.RawURLEncoding

	return enc.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + enc.EncodeToString([]byte(payload)) + ".c2ln"
}

func fileWith(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func planWith(changes, outputs string) string {
	return `{"format_version":"1.2","resource_changes":[` + changes + `],"output_changes":{` + outputs + `}}`
}

func change(address, actions string) string {
	return `{"address":"` + address + `","change":{"actions":[` + actions + `]}}`
}

func Test_roleARN(t *testing.T) {
	t.Parallel()

	for _, role := range cientry.Roles() {
		t.Run(role+" の役割の ARN を組む", func(t *testing.T) {
			t.Parallel()

			got, err := roleARN("tabp", "admin", role, account)
			require.NoError(t, err)
			assert.Equal(t, "arn:aws:iam::123456789012:role/tabp-admin-ci-"+role, got)
		})
	}

	// 写しと正本のずれを検出する検査は、正本の実物を読んでよい（ADR-0702 決定17）。
	t.Run("usecases/deployment-identity の role 名の規則と一致する", func(t *testing.T) {
		t.Parallel()

		uc, err := os.ReadFile(filepath.Join("..", "..", "usecases", "deployment-identity", "main.tf"))
		require.NoError(t, err)
		assert.Regexp(t, `name_prefix\s*=\s*"\$\{var\.project\}-\$\{var\.env\}"`, string(uc))

		// 行の role と name の対応、および role の集合と登録簿の一致は lib/cientry の CheckTerraformRoles が見る。
		rows, err := cientry.TerraformRolesRepo(filepath.Join("..", ".."))
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for _, row := range rows {
			got, err := roleARN("tabp", "admin", row.Role, account)
			require.NoError(t, err, row.Role)
			name := strings.Replace(row.Name, cientry.TerraformNamePrefix, "tabp-admin", 1)
			assert.Equal(t, "arn:aws:iam::123456789012:role/"+name, got, row.Role)
		}
	})

	cases := map[string][4]string{
		"知らない役割は失敗する":           {"tabp", "admin", "admin", account},
		"下線の役割名は失敗する":           {"tabp", "admin", "apply_identity", account},
		"11桁のアカウント ID は失敗する":    {"tabp", "admin", "plan", "12345678901"},
		"13桁のアカウント ID は失敗する":    {"tabp", "admin", "plan", "1234567890123"},
		"空のアカウント ID は失敗する":      {"tabp", "admin", "plan", ""},
		"ハイフンを含む project は失敗する": {"ta-bp", "admin", "plan", account},
		"空の env は失敗する":          {"tabp", "", "plan", account},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := roleARN(c[0], c[1], c[2], c[3])
			require.ErrorIs(t, err, errRole)
		})
	}

	t.Run("エラーの文にアカウント ID を入れない", func(t *testing.T) {
		t.Parallel()

		_, err := roleARN("tabp", "admin", "admin", account)
		require.ErrorIs(t, err, errRole)
		assert.NotContains(t, err.Error(), account)
	})
}

const (
	deniedStderr = "\naws: [ERROR]: An error occurred (AccessDenied) when calling the AssumeRoleWithWebIdentity operation: Not authorized to perform sts:AssumeRoleWithWebIdentity\n"
	invalidToken = "\naws: [ERROR]: An error occurred (InvalidIdentityToken) when calling the AssumeRoleWithWebIdentity operation: Incorrect token audience\n"
)

func Test_assumeDenied(t *testing.T) {
	t.Parallel()

	for name, stderr := range map[string]string{
		"CLI の接頭辞つきの AccessDenied": deniedStderr,
		"接頭辞の無い AccessDenied":      "An error occurred (AccessDenied) when calling the AssumeRoleWithWebIdentity operation: Not authorized\n",
	} {
		t.Run(name+"を拒否として受ける", func(t *testing.T) {
			t.Parallel()

			got, err := assumeDenied("unlock-identity", []byte(stderr))
			require.NoError(t, err)
			assert.Equal(t, "✅ unlock-identity の role の assume は拒否されました（AccessDenied）", got)
		})
	}

	for name, code := range map[string]string{
		"token の不正":                "InvalidIdentityToken",
		"token の期限切れ":              "ExpiredToken",
		"IdP との通信の失敗":              "IDPCommunicationError",
		"throttling":               "Throttling",
		"AccessDenied で始まる別の Code": "AccessDeniedException",
	} {
		t.Run(name+"は拒否ではないとして失敗し、Code を文に入れる", func(t *testing.T) {
			t.Parallel()

			stderr := "An error occurred (" + code + ") when calling the AssumeRoleWithWebIdentity operation: x"
			_, err := assumeDenied("apply", []byte(stderr))
			require.ErrorIs(t, err, errAssumeNotDenied)
			assert.Contains(t, err.Error(), "（"+code+"）")
		})
	}

	for name, stderr := range map[string]string{
		"空の stderr":    "",
		"接続の失敗":        "\nCould not connect to the endpoint URL: \"https://sts.ap-northeast-1.amazonaws.com/\"\n",
		"別の operation": "An error occurred (AccessDenied) when calling the AssumeRole operation: x",
		"エラーの行が2つ":     deniedStderr + deniedStderr,
		"Code に記号を含む":  "An error occurred (Access-Denied) when calling the AssumeRoleWithWebIdentity operation: x",
	} {
		t.Run(name+"は STS のエラーを読めないとして失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := assumeDenied("apply", []byte(stderr))
			require.ErrorIs(t, err, errAssumeUnreadable)
		})
	}

	t.Run("エラーの文に stderr の本文とアカウント ID を入れない", func(t *testing.T) {
		t.Parallel()

		stderr := "An error occurred (InvalidIdentityToken) when calling the AssumeRoleWithWebIdentity operation: arn:aws:iam::" + account + ":role/x"
		_, err := assumeDenied("apply", []byte(stderr))
		require.ErrorIs(t, err, errAssumeNotDenied)
		assert.NotContains(t, err.Error(), account)
		assert.NotContains(t, err.Error(), "arn:aws")
	})
}

func Test_oidcSub(t *testing.T) {
	t.Parallel()

	t.Run("payload の sub を返す", func(t *testing.T) {
		t.Parallel()

		got, err := oidcSub(jwtWith(`{"sub":"`+immutable+`","aud":"sts.amazonaws.com"}`) + "\n")
		require.NoError(t, err)
		assert.Equal(t, immutable, got)
	})

	t.Run("padding の付いた payload も読む", func(t *testing.T) {
		t.Parallel()

		parts := strings.Split(jwtWith(`{"sub":"`+immutable+`"}`), ".")
		padded := base64.URLEncoding.EncodeToString([]byte(`{"sub":"` + immutable + `"}`))
		got, err := oidcSub(parts[0] + "." + padded + "." + parts[2])
		require.NoError(t, err)
		assert.Equal(t, immutable, got)
	})

	// sized は、3つに分かれた長さ n の token を返す。大きさの分岐を、後ろの JWT の形の分岐と分けて見るため。
	sized := func(n int) string { return "a." + strings.Repeat("b", n-4) + ".c" }

	cases := map[string]struct{ token, why string }{
		"空の token は失敗する":                {"", "空か、大きすぎます"},
		"3つに分かれない token は失敗する":          {"a.b", "JWT の形ではありません"},
		"base64url でない payload は失敗する":   {"a.!!!.c", "base64url ではありません"},
		"JSON でない payload は失敗する":        {jwtWith("not json"), "JSON ではありません"},
		"sub の無い payload は失敗する":         {jwtWith(`{"aud":"sts.amazonaws.com"}`), "environment 形式ではありません"},
		"ref 形式の sub は失敗する":             {jwtWith(`{"sub":"repo:o/r:ref:refs/heads/main"}`), "environment 形式ではありません"},
		"pull_request 形式の sub は失敗する":    {jwtWith(`{"sub":"repo:o/r:pull_request"}`), "environment 形式ではありません"},
		"environment 名に空白を含む sub は失敗する": {jwtWith(`{"sub":"repo:o/r:environment:admin plan"}`), "environment 形式ではありません"},
		"上限を1つ超える token は大きさで失敗する":      {sized(maxTokenSize + 1), "空か、大きすぎます"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := oidcSub(c.token)
			require.ErrorIs(t, err, errOIDCToken)
			assert.Contains(t, err.Error(), c.why)
		})
	}

	t.Run("上限ちょうどの token は大きさでは失敗しない", func(t *testing.T) {
		t.Parallel()

		_, err := oidcSub(sized(maxTokenSize))
		require.ErrorIs(t, err, errOIDCToken)
		assert.NotContains(t, err.Error(), "大きすぎます")
	})

	t.Run("エラーの文に token を入れない", func(t *testing.T) {
		t.Parallel()

		token := jwtWith(`{"sub":"secret-looking-value"}`)
		_, err := oidcSub(token)
		require.ErrorIs(t, err, errOIDCToken)
		assert.NotContains(t, err.Error(), "secret-looking-value")
		assert.NotContains(t, err.Error(), strings.Split(token, ".")[1])
	})
}

func Test_planCounts_changed(t *testing.T) {
	t.Parallel()

	t.Run("すべて0なら変更なし", func(t *testing.T) {
		t.Parallel()

		assert.False(t, planCounts{}.changed())
	})

	cases := map[string]planCounts{
		"取り込みがあれば変更あり":       {importing: 1},
		"追加があれば変更あり":         {add: 1},
		"変更があれば変更あり":         {change: 1},
		"破棄があれば変更あり":         {destroy: 1},
		"読み取りがあれば変更あり":       {read: 1},
		"忘却があれば変更あり":         {forget: 1},
		"移動があれば変更あり":         {move: 1},
		"output の変更があれば変更あり": {outputs: 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.True(t, c.changed())
		})
	}
}

func Test_planCounts_String(t *testing.T) {
	t.Parallel()

	t.Run("変更が無ければ No changes. を返す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "No changes.", planCounts{}.String())
	})

	t.Run("terraform と同じ順で件数を返す", func(t *testing.T) {
		t.Parallel()

		got := planCounts{importing: 5, add: 21, change: 1, destroy: 2}.String()
		assert.Equal(t, "Plan: 5 to import, 21 to add, 1 to change, 2 to destroy.", got)
	})

	t.Run("0でない付帯の件数だけを足す", func(t *testing.T) {
		t.Parallel()

		got := planCounts{read: 1, outputs: 2}.String()
		assert.Equal(t, "Plan: 0 to import, 0 to add, 0 to change, 0 to destroy. 1 to read. 2 output changes.", got)
	})
}

func Test_planSummary(t *testing.T) {
	t.Parallel()

	t.Run("変更が無く終了コードが0なら No changes.", func(t *testing.T) {
		t.Parallel()

		got, err := planSummary("0", []byte(planWith(change("a.b", `"no-op"`), `"x":{"actions":["no-op"]}`)))
		require.NoError(t, err)
		assert.Equal(t, "No changes.", got)
	})

	t.Run("変更があり終了コードが2なら件数を返す", func(t *testing.T) {
		t.Parallel()

		changes := strings.Join([]string{
			change("a.create", `"create"`),
			change("a.update", `"update"`),
			change("a.delete", `"delete"`),
			change("a.replace", `"delete","create"`),
			change("a.replace2", `"create","delete"`),
			`{"address":"a.imported","change":{"actions":["no-op"],"importing":{"id":"x"}}}`,
		}, ",")
		got, err := planSummary("2", []byte(planWith(changes, "")))
		require.NoError(t, err)
		assert.Equal(t, "Plan: 1 to import, 3 to add, 1 to change, 3 to destroy.", got)
	})

	t.Run("output の変更だけでも変更ありと数える", func(t *testing.T) {
		t.Parallel()

		got, err := planSummary("2", []byte(planWith(change("a.b", `"no-op"`), `"x":{"actions":["create"]}`)))
		require.NoError(t, err)
		assert.Equal(t, "Plan: 0 to import, 0 to add, 0 to change, 0 to destroy. 1 output changes.", got)
	})

	t.Run("終了コード0で変更があれば失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("0", []byte(planWith(change("a.b", `"create"`), "")))
		require.ErrorIs(t, err, errPlanMismatch)
	})

	t.Run("終了コード2で変更が無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("2", []byte(planWith(change("a.b", `"no-op"`), "")))
		require.ErrorIs(t, err, errPlanMismatch)
	})

	for _, code := range []string{"1", "", "3"} {
		t.Run("終了コード "+code+" は失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := planSummary(code, []byte(planWith(change("a.b", `"no-op"`), "")))
			require.ErrorIs(t, err, errPlanExitCode)
		})
	}

	t.Run("resource の変更が0件なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("0", []byte(planWith("", "")))
		require.ErrorIs(t, err, errPlanEmpty)
	})

	t.Run("resource_changes が無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("0", []byte(`{"format_version":"1.2"}`))
		require.ErrorIs(t, err, errPlanEmpty)
	})

	t.Run("errored の plan は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("2", []byte(`{"format_version":"1.2","errored":true,"resource_changes":[`+change("a.b", `"create"`)+`]}`))
		require.ErrorIs(t, err, errPlanErrored)
	})

	t.Run("JSON でなければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("0", []byte("No changes."))
		require.ErrorIs(t, err, errPlanJSON)
	})

	t.Run("format_version が無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("0", []byte(`{"resource_changes":[`+change("a.b", `"no-op"`)+`]}`))
		require.ErrorIs(t, err, errPlanJSON)
	})

	t.Run("知らない actions は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planSummary("2", []byte(planWith(change("a.b", `"explode"`), "")))
		require.ErrorIs(t, err, errPlanJSON)
	})
}

func Test_countChanges(t *testing.T) {
	t.Parallel()

	t.Run("actions ごとに数える", func(t *testing.T) {
		t.Parallel()

		p := planJSON{ResourceChanges: []resourceChange{
			{Address: "a"}, {Address: "b"}, {Address: "c"},
		}}
		p.ResourceChanges[0].Change.Actions = []string{"read"}
		p.ResourceChanges[1].Change.Actions = []string{"forget"}
		p.ResourceChanges[2].Change.Actions = []string{"no-op"}
		p.ResourceChanges[2].PreviousAddress = "old"

		got, err := countChanges(p)
		require.NoError(t, err)
		assert.Equal(t, planCounts{read: 1, forget: 1, move: 1}, got)
	})

	t.Run("同じ address の previous_address は移動と数えない", func(t *testing.T) {
		t.Parallel()

		p := planJSON{ResourceChanges: []resourceChange{{Address: "a", PreviousAddress: "a"}}}
		p.ResourceChanges[0].Change.Actions = []string{"no-op"}

		got, err := countChanges(p)
		require.NoError(t, err)
		assert.Equal(t, planCounts{}, got)
	})

	t.Run("importing が null なら取り込みと数えない", func(t *testing.T) {
		t.Parallel()

		p := planJSON{ResourceChanges: []resourceChange{{Address: "a"}}}
		p.ResourceChanges[0].Change.Actions = []string{"no-op"}
		p.ResourceChanges[0].Change.Importing = []byte("null")

		got, err := countChanges(p)
		require.NoError(t, err)
		assert.Equal(t, planCounts{}, got)
	})

	actionCases := map[string]struct {
		actions []string
		want    planCounts
	}{
		"create は追加":           {[]string{"create"}, planCounts{add: 1}},
		"update は変更":           {[]string{"update"}, planCounts{change: 1}},
		"delete は破棄":           {[]string{"delete"}, planCounts{destroy: 1}},
		"delete,create は追加と破棄": {[]string{"delete", "create"}, planCounts{add: 1, destroy: 1}},
		"create,delete は追加と破棄": {[]string{"create", "delete"}, planCounts{add: 1, destroy: 1}},
	}
	for name, c := range actionCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := planJSON{ResourceChanges: []resourceChange{{Address: "a"}}}
			p.ResourceChanges[0].Change.Actions = c.actions

			got, err := countChanges(p)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	t.Run("importing があれば取り込みと数え、no-op でない output を数える", func(t *testing.T) {
		t.Parallel()

		var p planJSON
		require.NoError(t, json.Unmarshal([]byte(`{"resource_changes":[{"address":"a","change":{"actions":["no-op"],"importing":{"id":"x"}}}],"output_changes":{"x":{"actions":["create"]},"y":{"actions":["no-op"]}}}`), &p))

		got, err := countChanges(p)
		require.NoError(t, err)
		assert.Equal(t, planCounts{importing: 1, outputs: 1}, got)
	})

	t.Run("actions が空なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := countChanges(planJSON{ResourceChanges: []resourceChange{{Address: "a"}}})
		require.ErrorIs(t, err, errPlanJSON)
	})
}

// policyObjectPatterns は、deployment-identity の inline policy の宣言の、line に当たるただ1つの行から、
// Bucket の変数 bucketVar の object の Resource の key の pattern（`pr/*` など）を取り出します。
func policyObjectPatterns(t *testing.T, line *regexp.Regexp, bucketVar string) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "usecases", "deployment-identity", "internal", "github-oidc", "policies.tf"))
	require.NoError(t, err)

	var hits []string
	for l := range strings.Lines(string(raw)) {
		if line.MatchString(l) {
			hits = append(hits, l)
		}
	}
	require.Len(t, hits, 1, "policies.tf の %s に当たる行", line)

	var patterns []string
	for _, m := range regexp.MustCompile(`"\$\{var\.`+regexp.QuoteMeta(bucketVar)+`\}/([^"]*)"`).FindAllStringSubmatch(hits[0], -1) {
		patterns = append(patterns, m[1])
	}
	require.NotEmpty(t, patterns, "policies.tf の %q から %s の object の Resource を読めない", hits[0], bucketVar)

	return patterns
}

// iamMatches は、IAM の Resource の pattern（* は / も越える）が key に当たるかを返します。
func iamMatches(patterns []string, key string) bool {
	for _, p := range patterns {
		if regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(p), `\*`, ".*") + "$").MatchString(key) {
			return true
		}
	}

	return false
}

func Test_applyKey(t *testing.T) {
	t.Parallel()

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		got, err := applyKey("1", "1", "admin-identity")
		require.NoError(t, err)

		identity := policyObjectPatterns(t, regexp.MustCompile(`Sid = "WriteApplyOutput"`), "plan_output_bucket_arn")
		assert.True(t, iamMatches(identity, got), "apply の全文の key %s が、apply-identity の role が plan の出力の Bucket に書ける Resource %v に当たる", got, identity)

		workload := policyObjectPatterns(t, regexp.MustCompile(`Sid = "WriteWorkloadApplyOutput"`), "apply_output_bucket_arn")
		assert.True(t, iamMatches(workload, got), "apply の全文の key %s が、apply の role が apply の出力の Bucket に書ける Resource %v に当たる", got, workload)

		plan := policyObjectPatterns(t, regexp.MustCompile(`^\s*plan_output_keys\s*=`), "plan_output_bucket_arn")
		assert.False(t, iamMatches(plan, got), "apply の全文の key %s が、plan の role の Resource %v に当たらない", got, plan)
	})

	t.Run("apply の全文は apply/ の下に置く", func(t *testing.T) {
		t.Parallel()

		got, err := applyKey("99", "2", "admin-identity")
		require.NoError(t, err)
		assert.Equal(t, "apply/99-2-admin-identity.txt", got)
	})

	t.Run("20桁の run は通す", func(t *testing.T) {
		t.Parallel()

		got, err := applyKey("12345678901234567890", "1", "a")
		require.NoError(t, err)
		assert.Equal(t, "apply/12345678901234567890-1-a.txt", got)
	})

	cases := map[string][3]string{
		"空の run は失敗する":      {"", "1", "a"},
		"21桁の run は失敗する":    {"123456789012345678901", "1", "a"},
		"0 の attempt は失敗する": {"1", "0", "a"},
		"空の attempt は失敗する":  {"1", "", "a"},
		"空の root は失敗する":     {"1", "1", ""},
		"大文字を含む root は失敗する": {"1", "1", "Admin"},
		"/ を含む root は失敗する":  {"1", "1", "admin/identity"},
		".. を含む run は失敗する":  {"../1", "1", "a"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := applyKey(c[0], c[1], c[2])
			require.ErrorIs(t, err, errApplyKey)
		})
	}
}

func Test_planKey(t *testing.T) {
	t.Parallel()

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		pr, err := planKey("1", "", "1", "1", "admin-identity")
		require.NoError(t, err)
		branch, err := planKey("", "release/v0.1.0", "1", "1", "admin-identity")
		require.NoError(t, err)

		patterns := policyObjectPatterns(t, regexp.MustCompile(`^\s*plan_output_keys\s*=`), "plan_output_bucket_arn")
		for _, key := range []string{pr, branch} {
			assert.True(t, iamMatches(patterns, key), "plan の全文の key %s が、plan の role が書ける Resource %v に当たる", key, patterns)
		}
		for _, p := range patterns {
			assert.True(t, iamMatches([]string{p}, pr) || iamMatches([]string{p}, branch), "plan の role の Resource %s に当たる plan の全文の key が無い", p)
		}
	})

	t.Run("Pull Request の plan は pr/ の下に置く", func(t *testing.T) {
		t.Parallel()

		got, err := planKey("7", "", "99", "2", "admin-identity")
		require.NoError(t, err)
		assert.Equal(t, "pr/7/99-2-admin-identity.txt", got)
	})

	t.Run("20桁の run は通す", func(t *testing.T) {
		t.Parallel()

		got, err := planKey("1", "", "12345678901234567890", "1", "a")
		require.NoError(t, err)
		assert.Equal(t, "pr/1/12345678901234567890-1-a.txt", got)
	})

	t.Run("保護ブランチの plan は branch/ の下に置く", func(t *testing.T) {
		t.Parallel()

		got, err := planKey("", "release/v0.1.0", "99", "1", "admin-identity")
		require.NoError(t, err)
		assert.Equal(t, "branch/release/v0.1.0/99-1-admin-identity.txt", got)
	})

	cases := map[string][5]string{
		"-pr と -branch の両方は失敗する":  {"1", "main", "1", "1", "a"},
		"-pr も -branch も無ければ失敗する": {"", "", "1", "1", "a"},
		"0 の Pull Request は失敗する":  {"0", "", "1", "1", "a"},
		"数でない Pull Request は失敗する": {"1a", "", "1", "1", "a"},
		"親を指すブランチ名は失敗する":          {"", "release/../x", "1", "1", "a"},
		"現在を指すブランチ名は失敗する":         {"", "./x", "1", "1", "a"},
		"先頭が / のブランチ名は失敗する":       {"", "/x", "1", "1", "a"},
		"空白を含むブランチ名は失敗する":         {"", "a b", "1", "1", "a"},
		"0 の attempt は失敗する":       {"1", "", "1", "0", "a"},
		"空の run は失敗する":            {"1", "", "", "1", "a"},
		"21桁の run は失敗する":          {"1", "", "123456789012345678901", "1", "a"},
		"末尾が / のブランチ名は失敗する":       {"", "x/", "1", "1", "a"},
		"大文字を含む root は失敗する":       {"1", "", "1", "1", "Admin"},
		"/ を含む root は失敗する":        {"1", "", "1", "1", "admin/identity"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := planKey(c[0], c[1], c[2], c[3], c[4])
			require.ErrorIs(t, err, errPlanKey)
		})
	}
}

func Test_runPolicyResult(t *testing.T) {
	t.Parallel()

	t.Run("ファイルの結果を読む", func(t *testing.T) {
		t.Parallel()

		got, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", regoWith(t, 3), fileWith(t, `[{"namespace":"plan.p","successes":3}]`)}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", regoWith(t, 3), filepath.Join(t.TempDir(), "missing")}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyJSON)
	})

	t.Run("0 バイトのファイル（conftest が何も書かなかった）は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", regoWith(t, 3), fileWith(t, "")}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyJSON)
	})

	t.Run("rego が無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", filepath.Join(t.TempDir(), "missing"), fileWith(t, "[]")}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyRules)
	})

	t.Run("rego に規則が1つも無ければ、結果を読まずに失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", regoWith(t, 0), filepath.Join(t.TempDir(), "missing")}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyRules)
	})

	t.Run("rego の規則の数と評価の数が違えば失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", regoWith(t, 2), fileWith(t, `[{"namespace":"plan.p","successes":3}]`)}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyCount)
	})

	t.Run("-namespace のときは、ほかの名前空間の評価を見ない", func(t *testing.T) {
		t.Parallel()

		got, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", regoWith(t, 1), fileWith(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.q","successes":5}]`)}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 1 件を評価し、違反はありません", got)
	})

	t.Run("-policy のときは、走査した全名前空間の件数と突き合わせる", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{"p.rego": "package plan.p\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n"})
		got, err := runPolicyResult([]string{"-policy", dir, fileWith(t, `[{"namespace":"plan.p","successes":1}]`)}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 1 件を評価し、違反はありません", got)
	})

	t.Run("-policy のときに宣言の無い名前空間の評価があれば失敗する", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{"p.rego": "package plan.p\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n"})
		_, err := runPolicyResult([]string{"-policy", dir, fileWith(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.q","successes":1}]`)}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyCount)
	})

	t.Run("-namespace の結果が他の名前空間だけなら、評価されていないとして失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rego", regoWith(t, 1), fileWith(t, `[{"namespace":"plan.q","successes":1}]`)}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyEmpty)
		assert.Contains(t, err.Error(), "plan.p")
	})

	t.Run("-policy のとき、走査した名前空間が結果に無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{
			"p.rego": "package plan.p\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n",
			"q.rego": "package plan.q\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n",
		})
		_, err := runPolicyResult([]string{"-policy", dir, fileWith(t, `[{"namespace":"plan.p","successes":1}]`)}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyEmpty)
		assert.Contains(t, err.Error(), "plan.q")
	})

	t.Run("-policy の下に規則が1つも無ければ、結果を読まずに失敗する", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{"lib/plan.rego": "package plan.lib\n"})
		_, err := runPolicyResult([]string{"-policy", dir, filepath.Join(t.TempDir(), "missing")}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyRules)
	})

	cases := map[string][]string{
		"名前空間が無ければ失敗する":                {"-rego", "r", "f"},
		"rego が無ければ使い方の誤り":             {"-namespace", "plan.p", "f"},
		"ファイルが1つでなければ失敗する":             {"-namespace", "plan.p", "-rego", "r"},
		"知らないフラグは失敗する":                 {"-x", "plan.p", "f"},
		"-policy と -rego は併用できない":      {"-policy", "d", "-rego", "r", "f"},
		"-policy と -namespace は併用できない": {"-policy", "d", "-namespace", "plan.p", "f"},
		"-policy も -rego も無ければ失敗する":    {"f"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runPolicyResult(args, os.ReadFile)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func regoWith(t *testing.T, n int) string {
	t.Helper()

	return fileWith(t, "package plan.p\n\n"+strings.Repeat("deny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n\n", n))
}

func Test_oidcURL(t *testing.T) {
	t.Parallel()

	const (
		request = "ACTIONS_ID_TOKEN_REQUEST_URL=https://run-actions-1-azure-eastus.actions.githubusercontent.com/x?api-version=2.0"
		bearer  = "ACTIONS_ID_TOKEN_REQUEST_TOKEN=secret-bearer"
	)

	t.Run("要求先に STS の audience を足す", func(t *testing.T) {
		t.Parallel()

		got, err := oidcURL([]string{request, bearer, "HOME=/h"})
		require.NoError(t, err)
		assert.Equal(t, "https://run-actions-1-azure-eastus.actions.githubusercontent.com/x?api-version=2.0&audience=sts.amazonaws.com", got)
		assert.NotContains(t, got, "secret-bearer")
	})

	t.Run("audience が既に在れば STS のものに置き換える", func(t *testing.T) {
		t.Parallel()

		got, err := oidcURL([]string{"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.actions.githubusercontent.com/x?audience=other", bearer})
		require.NoError(t, err)
		assert.Equal(t, "https://t.actions.githubusercontent.com/x?audience=sts.amazonaws.com", got)
	})

	t.Run("port 付きの host も受理する", func(t *testing.T) {
		t.Parallel()

		got, err := oidcURL([]string{"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.actions.githubusercontent.com:443/x", bearer})
		require.NoError(t, err)
		assert.Equal(t, "https://t.actions.githubusercontent.com:443/x?audience=sts.amazonaws.com", got)
	})

	t.Run("audience は usecases/deployment-identity の OIDC provider と一致する", func(t *testing.T) {
		t.Parallel()

		tf, err := os.ReadFile(filepath.Join("..", "..", "usecases", "deployment-identity", "internal", "github-oidc", "main.tf"))
		require.NoError(t, err)
		assert.Regexp(t, `client_id_list\s*=\s*\["`+regexp.QuoteMeta(oidcAudience)+`"\]`, string(tf))
	})

	cases := map[string][]string{
		"要求先が無ければ失敗する":           {bearer},
		"要求の token が無ければ失敗する":    {request},
		"空の要求先は失敗する":             {"ACTIONS_ID_TOKEN_REQUEST_URL=", bearer},
		"https でなければ失敗する":        {"ACTIONS_ID_TOKEN_REQUEST_URL=http://t.actions.githubusercontent.com/x", bearer},
		"host が無ければ失敗する":         {"ACTIONS_ID_TOKEN_REQUEST_URL=https:///x", bearer},
		"GitHub の外の host は失敗する":  {"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.example/x", bearer},
		"host の末尾だけが似ていれば失敗する":   {"ACTIONS_ID_TOKEN_REQUEST_URL=https://evil-actions.githubusercontent.com.example/x", bearer},
		"先頭の . の無い似た host は失敗する": {"ACTIONS_ID_TOKEN_REQUEST_URL=https://evilactions.githubusercontent.com/x", bearer},
		"先頭のラベルが無い host は失敗する":   {"ACTIONS_ID_TOKEN_REQUEST_URL=https://.actions.githubusercontent.com/x", bearer},
		"ドメインそのものは失敗する":          {"ACTIONS_ID_TOKEN_REQUEST_URL=https://actions.githubusercontent.com/x", bearer},
		"userinfo を持てば失敗する":      {"ACTIONS_ID_TOKEN_REQUEST_URL=https://u:p@t.actions.githubusercontent.com/x", bearer},
		"URL でなければ失敗する":          {"ACTIONS_ID_TOKEN_REQUEST_URL=://x", bearer},
		"環境が空なら失敗する":             nil,
	}
	for name, environ := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := oidcURL(environ)
			require.ErrorIs(t, err, errOIDCRequest)
		})
	}
}

func Test_oidcToken(t *testing.T) {
	t.Parallel()

	t.Run("ちょうど maxTokenSize なら受理する", func(t *testing.T) {
		t.Parallel()

		token := "a.b." + strings.Repeat("c", maxTokenSize-4)
		got, err := oidcToken([]byte(`{"value":"` + token + `"}`))
		require.NoError(t, err)
		assert.Equal(t, token, got)
	})

	t.Run("応答の value を返す", func(t *testing.T) {
		t.Parallel()

		got, err := oidcToken([]byte(`{"count":1,"value":"a.b.c"}`))
		require.NoError(t, err)
		assert.Equal(t, "a.b.c", got)
	})

	cases := map[string]string{
		"value が無ければ失敗する":          `{"count":1}`,
		"value が空なら失敗する":           `{"value":""}`,
		"JWT の形でなければ失敗する":          `{"value":"a.b"}`,
		"空白を含めば失敗する":               `{"value":"a.b.c d"}`,
		"改行を含めば失敗する":               `{"value":"a.b.c\n"}`,
		"JSON でなければ失敗する":           `value=a.b.c`,
		"value が文字列でなければ失敗する":      `{"value":1}`,
		"maxTokenSize を1つ超えれば失敗する": `{"value":"a.b.` + strings.Repeat("c", maxTokenSize-3) + `"}`,
		"区切りだけの value は失敗する":       `{"value":".."}`,
		"空の片を含めば失敗する":              `{"value":"a..c"}`,
		"4片の JWT は失敗する":            `{"value":"a.b.c.d"}`,
		"タブを含めば失敗する":               `{"value":"a.b\tx.c"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := oidcToken([]byte(body))
			require.ErrorIs(t, err, errOIDCResponse)
			assert.NotContains(t, err.Error(), "a.b")
		})
	}
}

func policyDirWith(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}

	return dir
}

func Test_fileSHA256(t *testing.T) {
	t.Parallel()

	t.Run("ファイルの SHA-256 を base64 で返す", func(t *testing.T) {
		t.Parallel()

		got, err := fileSHA256(fileWith(t, "abc"), os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0=", got)
	})

	t.Run("空のファイルも空の本文の SHA-256 を返す", func(t *testing.T) {
		t.Parallel()

		got, err := fileSHA256(fileWith(t, ""), os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=", got)
	})

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := fileSHA256(filepath.Join(t.TempDir(), "missing"), os.ReadFile)
		require.ErrorIs(t, err, errDigest)
	})
}

func Test_planChanges(t *testing.T) {
	t.Parallel()

	t.Run("変わる resource の address と操作だけを、JSON の順に返す", func(t *testing.T) {
		t.Parallel()

		changes := strings.Join([]string{
			change("a.none", `"no-op"`),
			change("a.create", `"create"`),
			change("a.update", `"update"`),
			change("a.delete", `"delete"`),
			change("a.replace", `"delete","create"`),
			change("a.replace2", `"create","delete"`),
			change("data.a.read", `"read"`),
			change("a.forget", `"forget"`),
			`{"address":"a.imported","change":{"actions":["no-op"],"importing":{"id":"x"}}}`,
			`{"address":"a.imported2","change":{"actions":["update"],"importing":{"id":"x"}}}`,
			`{"address":"a.to","previous_address":"a.from","change":{"actions":["no-op"]}}`,
			`{"address":"a.to2","previous_address":"a.from2","change":{"actions":["update"]}}`,
			`{"address":"a.same","previous_address":"a.same","change":{"actions":["no-op"]}}`,
		}, ",")
		got, err := planChanges([]byte(planWith(changes, "")), summaryOf(2, 3, 3, 3))
		require.NoError(t, err)
		assert.Equal(t, []string{
			"create a.create", "update a.update", "delete a.delete", "replace a.replace", "replace a.replace2",
			"read data.a.read", "forget a.forget", "import a.imported", "import+update a.imported2",
			"move a.from -> a.to", "move+update a.from2 -> a.to2",
		}, got)
	})

	t.Run("属性の値は一覧に入らない", func(t *testing.T) {
		t.Parallel()

		data := `{"format_version":"1.2","resource_changes":[{"address":"a.b","change":{"actions":["update"],` +
			`"before":{"v":"BEFORE-VALUE"},"after":{"v":"AFTER-VALUE"},"after_unknown":{"v":"UNKNOWN-VALUE"},` +
			`"before_sensitive":{"v":"SENSITIVE-VALUE"}}}]}`
		got, err := planChanges([]byte(data), summaryOf(0, 0, 1, 0))
		require.NoError(t, err)
		assert.Equal(t, []string{"update a.b"}, got)
		for _, v := range []string{"BEFORE-VALUE", "AFTER-VALUE", "UNKNOWN-VALUE", "SENSITIVE-VALUE"} {
			assert.NotContains(t, strings.Join(got, "\n"), v)
		}
	})

	t.Run("アカウント ID とメールを含む key は、移動元の address でも伏せる", func(t *testing.T) {
		t.Parallel()

		changes := `{"address":"module.m[\"acct-` + account + `\"].a.b[\"x@example.com\"]","previous_address":"a.b[\"` + account + `\"]","change":{"actions":["create"]}}`
		got, err := planChanges([]byte(planWith(changes, "")), summaryOf(0, 1, 0, 0))
		require.NoError(t, err)
		assert.Equal(t, []string{"move+create a.b[<伏せた key>] -> module.m[<伏せた key>].a.b[<伏せた key>]"}, got)
	})

	t.Run("変更が無く change_summary が0件ずつなら空の一覧を返す", func(t *testing.T) {
		t.Parallel()

		got, err := planChanges([]byte(planWith(change("a.b", `"no-op"`), "")), summaryOf(0, 0, 0, 0))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("move と forget だけなら change_summary は0件で一致する", func(t *testing.T) {
		t.Parallel()

		changes := change("a.forget", `"forget"`) + `,{"address":"a.to","previous_address":"a.from","change":{"actions":["no-op"]}}`
		got, err := planChanges([]byte(planWith(changes, "")), summaryOf(0, 0, 0, 0))
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	type mismatch struct {
		name    string
		changes string
		summary streamDiag
	}

	for _, c := range []mismatch{
		{
			name:    "件数が change_summary と違えば失敗する",
			changes: change("a.b", `"create"`),
			summary: summaryOf(0, 2, 0, 0),
		},
		{
			name:    "置き換えを add と remove の両方に数える",
			changes: change("a.b", `"delete","create"`),
			summary: summaryOf(0, 1, 0, 0),
		},
		{
			name:    "import の件数が違えば失敗する",
			changes: change("a.b", `"create"`),
			summary: summaryOf(1, 1, 0, 0),
		},
		{
			name:    "変更が無いのに change_summary に件数があれば失敗する",
			changes: change("a.b", `"no-op"`),
			summary: summaryOf(0, 0, 1, 0),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			_, err := planChanges([]byte(planWith(c.changes, "")), c.summary)
			require.ErrorIs(t, err, errPlanList)
			assert.Equal(t, 2, exitCode(err))
		})
	}

	t.Run("change_summary が無ければ、変更が無くても失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planChanges([]byte(planWith(change("a.b", `"no-op"`), "")), streamDiag{})
		require.ErrorIs(t, err, errChangeSummary)
	})

	t.Run("change_summary が2つなら失敗する", func(t *testing.T) {
		t.Parallel()

		two := streamDiag{Summaries: append(summaryOf(0, 1, 0, 0).Summaries, summaryOf(0, 1, 0, 0).Summaries...)}
		_, err := planChanges([]byte(planWith(change("a.b", `"create"`), "")), two)
		require.ErrorIs(t, err, errChangeSummary)
	})

	t.Run("JSON を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planChanges([]byte("x"), streamDiag{})
		require.ErrorIs(t, err, errPlanJSON)
	})

	t.Run("知らない actions は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := planChanges([]byte(planWith(change("a.b", `"create","update"`), "")), streamDiag{})
		require.ErrorIs(t, err, errPlanJSON)
	})
}

func summaryOf(imports, add, chg, remove int) streamDiag {
	return streamDiag{Summaries: []map[string]any{{
		"import": float64(imports), "add": float64(add), "change": float64(chg), "remove": float64(remove), "operation": "plan",
	}}}
}
