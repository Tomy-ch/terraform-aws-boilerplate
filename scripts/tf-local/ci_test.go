package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func Test_runRoleARN(t *testing.T) {
	t.Parallel()

	t.Run("フラグから role の ARN を組む", func(t *testing.T) {
		t.Parallel()

		got, err := runRoleARN([]string{"-project", "tabp", "-env", "admin", "-role", "apply-identity", "-account", account})
		require.NoError(t, err)
		assert.Equal(t, "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", got)
	})

	t.Run("知らないフラグは失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runRoleARN([]string{"-region", "x"})
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("余分な引数は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runRoleARN([]string{"-project", "tabp", "-env", "admin", "-role", "plan", "-account", account, "x"})
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_roleARN(t *testing.T) {
	t.Parallel()

	for _, role := range []string{"plan", "apply", "apply-identity"} {
		t.Run(role+" の役割の ARN を組む", func(t *testing.T) {
			t.Parallel()

			got, err := roleARN("tabp", "admin", role, account)
			require.NoError(t, err)
			assert.Equal(t, "arn:aws:iam::123456789012:role/tabp-admin-ci-"+role, got)
		})
	}

	t.Run("usecases/deployment-identity の role 名の規則と一致する", func(t *testing.T) {
		t.Parallel()

		uc, err := os.ReadFile(filepath.Join("..", "..", "usecases", "deployment-identity", "main.tf"))
		require.NoError(t, err)
		assert.Regexp(t, `name_prefix\s*=\s*"\$\{var\.project\}-\$\{var\.env\}"`, string(uc))

		roles, err := os.ReadFile(filepath.Join("..", "..", "usecases", "deployment-identity", "internal", "github-oidc", "main.tf"))
		require.NoError(t, err)
		for _, role := range roleNames {
			assert.Contains(t, string(roles), `"${var.name_prefix}-ci-`+role+`"`)
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

func Test_oidcSub(t *testing.T) {
	t.Parallel()

	t.Run("payload の sub を返す", func(t *testing.T) {
		t.Parallel()

		got, err := oidcSub(fileWith(t, jwtWith(`{"sub":"`+immutable+`","aud":"sts.amazonaws.com"}`)+"\n"), os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, immutable, got)
	})

	t.Run("padding の付いた payload も読む", func(t *testing.T) {
		t.Parallel()

		parts := strings.Split(jwtWith(`{"sub":"`+immutable+`"}`), ".")
		padded := base64.URLEncoding.EncodeToString([]byte(`{"sub":"` + immutable + `"}`))
		got, err := oidcSub(fileWith(t, parts[0]+"."+padded+"."+parts[2]), os.ReadFile)
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

			_, err := oidcSub(fileWith(t, c.token), os.ReadFile)
			require.ErrorIs(t, err, errOIDCToken)
			assert.Contains(t, err.Error(), c.why)
		})
	}

	t.Run("上限ちょうどの token は大きさでは失敗しない", func(t *testing.T) {
		t.Parallel()

		_, err := oidcSub(fileWith(t, sized(maxTokenSize)), os.ReadFile)
		require.ErrorIs(t, err, errOIDCToken)
		assert.NotContains(t, err.Error(), "大きすぎます")
	})

	t.Run("token のファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := oidcSub(filepath.Join(t.TempDir(), "missing"), os.ReadFile)
		require.ErrorIs(t, err, errOIDCToken)
	})

	t.Run("エラーの文に token を入れない", func(t *testing.T) {
		t.Parallel()

		token := jwtWith(`{"sub":"secret-looking-value"}`)
		_, err := oidcSub(fileWith(t, token), os.ReadFile)
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

func Test_runPlanSummary(t *testing.T) {
	t.Parallel()

	t.Run("ファイルの JSON を要約する", func(t *testing.T) {
		t.Parallel()

		got, err := runPlanSummary([]string{"-exitcode", "0", fileWith(t, planWith(change("a.b", `"no-op"`), ""))}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "No changes.", got)
	})

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPlanSummary([]string{"-exitcode", "0", filepath.Join(t.TempDir(), "missing")}, os.ReadFile)
		require.ErrorIs(t, err, errPlanJSON)
	})

	t.Run("終了コードが1なら、ファイルを読まずに plan の失敗として落ちる", func(t *testing.T) {
		t.Parallel()

		_, err := runPlanSummary([]string{"-exitcode", "1", filepath.Join(t.TempDir(), "missing")}, os.ReadFile)
		require.ErrorIs(t, err, errPlanFailed)
	})

	t.Run("終了コードが1なら、JSON が読めても plan の失敗として落ちる", func(t *testing.T) {
		t.Parallel()

		_, err := runPlanSummary([]string{"-exitcode", "1", fileWith(t, planWith(change("a.b", `"no-op"`), ""))}, os.ReadFile)
		require.ErrorIs(t, err, errPlanFailed)
	})

	t.Run("ファイルが1つでなければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPlanSummary([]string{"-exitcode", "0"}, os.ReadFile)
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("知らないフラグは失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPlanSummary([]string{"-x", "0", "f"}, os.ReadFile)
		require.ErrorIs(t, err, errUsage)
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

func Test_runPlanKey(t *testing.T) {
	t.Parallel()

	t.Run("フラグから key を組む", func(t *testing.T) {
		t.Parallel()

		got, err := runPlanKey([]string{"-pr", "12", "-run", "345", "-attempt", "1", "-root", "admin-identity"})
		require.NoError(t, err)
		assert.Equal(t, "pr/12/345-1-admin-identity.txt", got)
	})

	t.Run("知らないフラグは失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPlanKey([]string{"-sha", "x"})
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("余分な引数は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPlanKey([]string{"-pr", "1", "-run", "1", "-attempt", "1", "-root", "a", "x"})
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_planKey(t *testing.T) {
	t.Parallel()

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

		got, err := runPolicyResult([]string{"-namespace", "plan.p", "-rules", "3", fileWith(t, `[{"namespace":"plan.p","successes":3}]`)}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rules", "3", filepath.Join(t.TempDir(), "missing")}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyJSON)
	})

	t.Run("0 バイトのファイル（conftest が何も書かなかった）は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runPolicyResult([]string{"-namespace", "plan.p", "-rules", "3", fileWith(t, "")}, os.ReadFile)
		require.ErrorIs(t, err, errPolicyJSON)
	})

	cases := map[string][]string{
		"名前空間が無ければ失敗する":    {"-rules", "3", "f"},
		"規則の数が無ければ失敗する":    {"-namespace", "plan.p", "f"},
		"規則の数が0なら失敗する":     {"-namespace", "plan.p", "-rules", "0", "f"},
		"規則の数が数でなければ失敗する":  {"-namespace", "plan.p", "-rules", "x", "f"},
		"ファイルが1つでなければ失敗する": {"-namespace", "plan.p", "-rules", "3"},
		"知らないフラグは失敗する":     {"-x", "plan.p", "f"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runPolicyResult(args, os.ReadFile)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_policyResult(t *testing.T) {
	t.Parallel()

	t.Run("宣言した数の規則が評価され違反が無ければ件数を出す", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult("plan.p", 3, []byte(`[{"filename":"Combined","namespace":"plan.p","successes":2,"warnings":[{"msg":"w"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("同じ名前空間の複数の結果を合算する", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult("plan.p", 3, []byte(`[{"namespace":"plan.p","successes":1},{"namespace":"plan.p","successes":2}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("例外も評価として数える", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult("plan.p", 2, []byte(`[{"namespace":"plan.p","successes":1,"exceptions":[{"msg":"e"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 2 件を評価し、違反はありません", got)
	})

	t.Run("違反があれば、その文を持って失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(`[{"namespace":"plan.p","successes":2,"failures":[{"msg":"[PLAN-STATE-01] x"}]}]`))
		require.ErrorIs(t, err, errPolicyViolation)
		assert.Contains(t, err.Error(), "[PLAN-STATE-01] x")
	})

	t.Run("違反だけの結果は、0件の評価ではなく違反として失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(`[{"namespace":"plan.p","successes":0,"failures":[{"msg":"a"}]}]`))
		require.ErrorIs(t, err, errPolicyViolation)
	})

	t.Run("評価された規則が宣言より少なければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(`[{"namespace":"plan.p","successes":1}]`))
		require.ErrorIs(t, err, errPolicyCount)
	})

	t.Run("評価された規則が宣言より多ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(`[{"namespace":"plan.p","successes":4}]`))
		require.ErrorIs(t, err, errPolicyCount)
	})

	t.Run("名前空間の規則が0件の評価なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(`[{"namespace":"plan.p","successes":0}]`))
		require.ErrorIs(t, err, errPolicyEmpty)
	})

	t.Run("別の名前空間の評価は数えない", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(`[{"namespace":"plan.q","successes":3}]`))
		require.ErrorIs(t, err, errPolicyEmpty)
	})

	t.Run("結果が空の配列なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(`[]`))
		require.ErrorIs(t, err, errPolicyEmpty)
	})

	t.Run("空の入力なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte(""))
		require.ErrorIs(t, err, errPolicyJSON)
	})

	t.Run("JSON でなければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult("plan.p", 3, []byte("FAIL - x"))
		require.ErrorIs(t, err, errPolicyJSON)
	})
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
