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

func Test_runAssumeDenied(t *testing.T) {
	t.Parallel()

	t.Run("終了コードが非0で AccessDenied なら拒否の行を出す", func(t *testing.T) {
		t.Parallel()

		got, err := runAssumeDenied([]string{"-role", "apply", "-exitcode", "254", fileWith(t, deniedStderr)}, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, "✅ apply の role の assume は拒否されました（AccessDenied）", got)
	})

	t.Run("終了コード 0 は stderr が AccessDenied でも assume が通ったとして失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runAssumeDenied([]string{"-role", "apply-identity", "-exitcode", "0", fileWith(t, deniedStderr)}, os.ReadFile)
		require.ErrorIs(t, err, errAssumeSucceeded)
	})

	t.Run("終了コード 0 は stderr のファイルが無くても失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runAssumeDenied([]string{"-role", "apply", "-exitcode", "0", filepath.Join(t.TempDir(), "none")}, os.ReadFile)
		require.ErrorIs(t, err, errAssumeSucceeded)
	})

	t.Run("終了コードが非0で AccessDenied でなければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runAssumeDenied([]string{"-role", "apply", "-exitcode", "254", fileWith(t, invalidToken)}, os.ReadFile)
		require.ErrorIs(t, err, errAssumeNotDenied)
	})

	t.Run("STS のエラーを読めなければ aws の終了コードを文に入れて失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runAssumeDenied([]string{"-role", "apply", "-exitcode", "255", fileWith(t, "Could not connect to the endpoint URL\n")}, os.ReadFile)
		require.ErrorIs(t, err, errAssumeUnreadable)
		assert.Contains(t, err.Error(), "aws の終了コード 255")
	})

	t.Run("stderr のファイルを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runAssumeDenied([]string{"-role", "apply", "-exitcode", "254", filepath.Join(t.TempDir(), "none")}, os.ReadFile)
		require.ErrorIs(t, err, errAssumeUnreadable)
	})

	for name, args := range map[string][]string{
		"知らない役割は失敗する":           {"-role", "admin", "-exitcode", "254", "f"},
		"役割が無ければ失敗する":           {"-exitcode", "254", "f"},
		"終了コードが無ければ失敗する":        {"-role", "apply", "f"},
		"数でない終了コードは失敗する":        {"-role", "apply", "-exitcode", "x", "f"},
		"負の終了コードは失敗する":          {"-role", "apply", "-exitcode", "-1", "f"},
		"256 以上の終了コードは失敗する":     {"-role", "apply", "-exitcode", "256", "f"},
		"stderr のファイルが無ければ失敗する": {"-role", "apply", "-exitcode", "254"},
		"余分な引数は失敗する":            {"-role", "apply", "-exitcode", "254", "f", "g"},
		"知らないフラグは失敗する":          {"-rc", "254", "f"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runAssumeDenied(args, os.ReadFile)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

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

func Test_runApplyKey(t *testing.T) {
	t.Parallel()

	t.Run("フラグから key を組む", func(t *testing.T) {
		t.Parallel()

		got, err := runApplyKey([]string{"-run", "345", "-attempt", "1", "-root", "admin-identity"})
		require.NoError(t, err)
		assert.Equal(t, "apply/345-1-admin-identity.txt", got)
	})

	t.Run("知らないフラグは失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runApplyKey([]string{"-pr", "1", "-run", "1", "-attempt", "1", "-root", "a"})
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("余分な引数は失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runApplyKey([]string{"-run", "1", "-attempt", "1", "-root", "a", "x"})
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_applyKey(t *testing.T) {
	t.Parallel()

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

func Test_runApplyResult(t *testing.T) {
	t.Parallel()

	t.Run("終了コード 0 なら成功の行を出す", func(t *testing.T) {
		t.Parallel()

		got, err := runApplyResult([]string{"-exitcode", "0"})
		require.NoError(t, err)
		assert.Equal(t, "✅ apply が終わりました", got)
	})

	t.Run("終了コード 124 は打ち切りとして失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runApplyResult([]string{"-exitcode", "124"})
		require.ErrorIs(t, err, errApplyTimeout)
		assert.NotErrorIs(t, err, errApplyFailed)
	})

	for _, code := range []string{"1", "2", "123", "125", "130", "137", "255"} {
		t.Run("終了コード "+code+" は apply の失敗として失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := runApplyResult([]string{"-exitcode", code})
			require.ErrorIs(t, err, errApplyFailed)
			assert.NotErrorIs(t, err, errApplyTimeout)
		})
	}

	for name, args := range map[string][]string{
		"終了コードが無ければ失敗する":    {},
		"空の終了コードは失敗する":      {"-exitcode", ""},
		"数でない終了コードは失敗する":    {"-exitcode", "x"},
		"負の終了コードは失敗する":      {"-exitcode", "-1"},
		"256 以上の終了コードは失敗する": {"-exitcode", "256"},
		"余分な引数は失敗する":        {"-exitcode", "0", "x"},
		"知らないフラグは失敗する":      {"-rc", "0"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runApplyResult(args)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_runFailureKind(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{
		"lock.txt":  []byte("plan\n\nError: Error acquiring the state lock\n\nLock Info:\n"),
		"plan.txt":  []byte("plan\n\nError: Invalid reference\n"),
		"empty.txt": {},
	}
	readFile := func(path string) ([]byte, error) {
		if b, ok := files[path]; ok {
			return b, nil
		}
		return nil, os.ErrNotExist
	}

	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"plan が lock の取得で落ちれば lock":        {[]string{"-step", "plan", "-exitcode", "1", "lock.txt"}, "lock"},
		"plan がほかの理由で落ちれば plan":            {[]string{"-step", "plan", "-exitcode", "1", "plan.txt"}, "plan"},
		"全文が空でも、読めれば plan":                 {[]string{"-step", "plan", "-exitcode", "1", "empty.txt"}, "plan"},
		"plan が通った後の失敗は、全文を読まずに plan":      {[]string{"-step", "plan", "-exitcode", "2", "missing.txt"}, "plan"},
		"終了コード 0 の plan の後の失敗も plan":       {[]string{"-step", "plan", "-exitcode", "0", "lock.txt"}, "plan"},
		"apply が lock の取得で落ちれば lock":       {[]string{"-step", "apply", "-exitcode", "1", "lock.txt"}, "lock"},
		"apply がほかの理由で落ちれば apply":          {[]string{"-step", "apply", "-exitcode", "1", "plan.txt"}, "apply"},
		"apply の打ち切りは、全文を読まずに timeout":     {[]string{"-step", "apply", "-exitcode", "124", "missing.txt"}, "timeout"},
		"apply の打ち切りの1つ手前は apply":          {[]string{"-step", "apply", "-exitcode", "123", "lock.txt"}, "apply"},
		"apply の打ち切りの1つ後は apply":           {[]string{"-step", "apply", "-exitcode", "125", "lock.txt"}, "apply"},
		"apply が終わった後の失敗は、全文を読まずに applied": {[]string{"-step", "apply", "-exitcode", "0", "missing.txt"}, "applied"},
		"apply の終了コードの上限 255 は apply":      {[]string{"-step", "apply", "-exitcode", "255", "missing.txt"}, "apply"},
		"plan の 124 は timeout にせず plan":    {[]string{"-step", "plan", "-exitcode", "124", "missing.txt"}, "plan"},
		"apply の 2 は、全文を読まずに apply":        {[]string{"-step", "apply", "-exitcode", "2", "missing.txt"}, "apply"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := runFailureKind(c.args, readFile)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	for _, step := range []string{"plan", "apply"} {
		t.Run(step+" が落ちたのに全文を読めなければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := runFailureKind([]string{"-step", step, "-exitcode", "1", "missing.txt"}, readFile)
			require.ErrorIs(t, err, errFailureKind)
		})
	}

	for name, args := range map[string][]string{
		"段が無い":       {"-exitcode", "1", "plan.txt"},
		"知らない段":      {"-step", "init", "-exitcode", "1", "plan.txt"},
		"終了コードが無い":   {"-step", "apply", "plan.txt"},
		"終了コードが範囲の外": {"-step", "apply", "-exitcode", "256", "plan.txt"},
		"終了コードが負":    {"-step", "apply", "-exitcode", "-1", "plan.txt"},
		"終了コードが数でない": {"-step", "apply", "-exitcode", "x", "plan.txt"},
		"全文が無い":      {"-step", "apply", "-exitcode", "1"},
		"余分な引数がある":   {"-step", "plan", "-exitcode", "1", "plan.txt", "x"},
		"知らない flag":  {"-x"},
	} {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := runFailureKind(args, readFile)
			require.ErrorIs(t, err, errUsage)
		})
	}
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

func Test_countRules(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		rego string
		want int
	}{
		"行頭の deny contains を数える":    {"package p\n\ndeny contains msg if {\n}\n\ndeny contains msg if {\n}\n", 2},
		"字下げした deny contains は数えない": {"package p\n\n  deny contains msg if {\n}\n", 0},
		"コメントの deny contains は数えない": {"package p\n# deny contains msg\n", 0},
		"規則が無ければ0":                  {"package p\n", 0},
		"空なら0":                      {"", 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, countRules([]byte(c.rego)))
		})
	}
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
		got, err := oidcToken(fileWith(t, `{"value":"`+token+`"}`), os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, token, got)
	})

	t.Run("応答の value を返す", func(t *testing.T) {
		t.Parallel()

		got, err := oidcToken(fileWith(t, `{"count":1,"value":"a.b.c"}`), os.ReadFile)
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

			_, err := oidcToken(fileWith(t, body), os.ReadFile)
			require.ErrorIs(t, err, errOIDCResponse)
			assert.NotContains(t, err.Error(), "a.b")
		})
	}

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := oidcToken(filepath.Join(t.TempDir(), "missing"), os.ReadFile)
		require.ErrorIs(t, err, errOIDCResponse)
	})
}

func Test_policyResult(t *testing.T) {
	t.Parallel()

	entries := func(t *testing.T, data string) []policyEntry {
		t.Helper()

		got, err := parsePolicyResult([]byte(data))
		require.NoError(t, err)

		return got
	}

	t.Run("宣言した数の規則が評価され違反が無ければ件数を出す", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult(map[string]int{"plan.p": 3}, entries(t, `[{"filename":"Combined","namespace":"plan.p","successes":2,"warnings":[{"msg":"w"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("同じ名前空間の複数の結果を合算する", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.p","successes":2}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("例外も評価として数える", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult(map[string]int{"plan.p": 2}, entries(t, `[{"namespace":"plan.p","successes":1,"exceptions":[{"msg":"e"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 2 件を評価し、違反はありません", got)
	})

	t.Run("複数の名前空間がすべて一致すれば、名前空間ごとに1行を名前の順で出す", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult(map[string]int{"plan.q": 1, "plan.p": 2}, entries(t, `[{"namespace":"plan.q","successes":1},{"namespace":"plan.p","successes":2}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 2 件を評価し、違反はありません\n✅ plan.q: 規則 1 件を評価し、違反はありません", got)
	})

	t.Run("宣言も評価も0件の名前空間（テストの package）は数えない", func(t *testing.T) {
		t.Parallel()

		got, err := policyResult(map[string]int{"plan.p": 1}, entries(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.p_test","successes":0}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 1 件を評価し、違反はありません", got)
	})

	t.Run("違反があれば、その文を持って失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 3, "plan.q": 1}, entries(t, `[{"namespace":"plan.p","successes":1,"failures":[{"msg":"[PLAN-STATE-01] x"}]}]`))
		require.ErrorIs(t, err, errPolicyViolation)
		assert.Contains(t, err.Error(), "[PLAN-STATE-01] x")
		assert.NotErrorIs(t, err, errPolicyCount, "違反があるときは件数を突き合わせない")
		assert.NotErrorIs(t, err, errPolicyEmpty, "違反があるときは評価の欠けを報告しない")
	})

	t.Run("複数の名前空間の違反は、すべての文を持って失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 1, "plan.q": 1}, entries(t, `[{"namespace":"plan.p","failures":[{"msg":"a"}]},{"namespace":"plan.q","failures":[{"msg":"b"}]}]`))
		require.ErrorIs(t, err, errPolicyViolation)
		assert.Contains(t, err.Error(), "a\nb")
	})

	t.Run("違反だけの結果は、0件の評価ではなく違反として失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"a"}]}]`))
		require.ErrorIs(t, err, errPolicyViolation)
	})

	t.Run("評価された規則が宣言より少なければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":1}]`))
		require.ErrorIs(t, err, errPolicyCount)
		assert.Contains(t, err.Error(), "plan.p: 評価 1 件、宣言 3 件")
	})

	t.Run("評価された規則が宣言より多ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":4}]`))
		require.ErrorIs(t, err, errPolicyCount)
	})

	t.Run("宣言の無い名前空間を評価していれば失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 1}, entries(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.q","successes":2}]`))
		require.ErrorIs(t, err, errPolicyCount)
		assert.Contains(t, err.Error(), "plan.q: 評価 2 件、宣言 0 件")
	})

	t.Run("名前空間の規則が0件の評価なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":0}]`))
		require.ErrorIs(t, err, errPolicyEmpty)
	})

	t.Run("宣言した名前空間が結果に無ければ（package が無い）失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 1, "plan.q": 2}, entries(t, `[{"namespace":"plan.p","successes":1}]`))
		require.ErrorIs(t, err, errPolicyEmpty)
		assert.Contains(t, err.Error(), "plan.q")
	})

	t.Run("評価されない名前空間と件数の食い違いが混ざれば、両方を返す", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 1, "plan.q": 2}, entries(t, `[{"namespace":"plan.p","successes":3}]`))
		require.ErrorIs(t, err, errPolicyEmpty)
		require.ErrorIs(t, err, errPolicyCount)
	})

	t.Run("結果が空の配列なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 3}, entries(t, `[]`))
		require.ErrorIs(t, err, errPolicyEmpty)
	})

	t.Run("結果が null なら、評価されていないとして失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{"plan.p": 1}, entries(t, `null`))
		require.ErrorIs(t, err, errPolicyEmpty)
	})

	t.Run("宣言が空なら、結果を見ずに失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyResult(map[string]int{}, entries(t, `[]`))
		require.ErrorIs(t, err, errPolicyRules)
	})
}

func Test_parsePolicyResult(t *testing.T) {
	t.Parallel()

	t.Run("conftest の JSON の項目を読む", func(t *testing.T) {
		t.Parallel()

		got, err := parsePolicyResult([]byte(`[{"namespace":"plan.p","successes":2,"failures":[{"msg":"a"}]}]`))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "plan.p", got[0].Namespace)
		assert.Equal(t, 2, got[0].Successes)
		assert.Equal(t, "a", got[0].Failures[0].Msg)
	})

	cases := map[string]string{
		"空の入力":     "",
		"JSON でない": "FAIL - x",
		"配列でない":    `{"namespace":"plan.p"}`,
		"件数が数でない":  `[{"namespace":"plan.p","successes":"3"}]`,
	}
	for name, data := range cases {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := parsePolicyResult([]byte(data))
			require.ErrorIs(t, err, errPolicyJSON)
		})
	}
}

func Test_policyRules(t *testing.T) {
	t.Parallel()

	rule := "deny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n\n"

	t.Run("package ごとに行頭の deny contains を数え、ファイルをまたいで合算する", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{
			"a.rego":              "package plan.a\n\n" + rule + rule,
			"a_more.rego":         "package plan.a\n\n" + rule,
			"a_test.rego":         "package plan.a_test\n",
			"lib/input.rego":      "package plan.input\n\n" + rule,
			"lib/plan.rego":       "package plan.lib\n",
			"lib/README.md":       "deny contains x\n",
			"lib/input_test.rego": "package plan.input_test\n",
		})
		got, err := policyRules(dir, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"plan.a": 3, "plan.input": 1}, got)
	})

	t.Run("字下げした deny contains は規則として数えない", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{"a.rego": "package plan.a\n\n" + rule + "  " + rule})
		got, err := policyRules(dir, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"plan.a": 1}, got)
	})

	valid := "package plan.a\n\n" + rule
	failures := map[string]struct {
		files map[string]string
		want  string
	}{
		"規則が全体で0件":    {map[string]string{"lib/plan.rego": "package plan.lib\n"}, "deny contains が1つもありません"},
		"rego が1つも無い": {map[string]string{"README.md": "x\n"}, "deny contains が1つもありません"},
		"他の rego が有効でも、package の行を持たない rego": {map[string]string{"a.rego": valid, "b.rego": rule}, "b.rego: package の行がありません"},
		"lib の外で規則を持たない rego":                {map[string]string{"a.rego": valid, "b.rego": "package plan.b\n"}, "b.rego: deny contains を持たない rego は lib/ の下にだけ置けます"},
		"lib の下の lib で規則を持たない rego":          {map[string]string{"a.rego": valid, "sub/lib/b.rego": "package plan.b\n"}, "lib/b.rego: deny contains を持たない"},
		"lib で始まる別のディレクトリで規則を持たない rego":      {map[string]string{"a.rego": valid, "libx/b.rego": "package plan.b\n"}, "libx/b.rego: deny contains を持たない"},
		"直下の lib.rego で規則を持たない":              {map[string]string{"a.rego": valid, "lib.rego": "package plan.lib\n"}, "lib.rego: deny contains を持たない"},
		"テストの対象の package が無い":                {map[string]string{"a.rego": valid, "b_test.rego": "package plan.b_test\n"}, "テストの対象の plan.b に deny contains がありません"},
		"テストの対象の package が規則を持たない":           {map[string]string{"a.rego": valid, "lib/b.rego": "package plan.b\n", "lib/b_test.rego": "package plan.b_test\n"}, "テストの対象の plan.b に deny contains がありません"},
		"テストの package が _test で終わらない":        {map[string]string{"a.rego": valid, "a_test.rego": "package plan.a\n"}, "a_test.rego: テストの package が _test で終わりません"},
	}
	for name, c := range failures {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := policyRules(policyDirWith(t, c.files), os.ReadFile)
			require.ErrorIs(t, err, errPolicyRules)
			assert.Contains(t, err.Error(), c.want)
		})
	}

	t.Run("ディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := policyRules(filepath.Join(t.TempDir(), "missing"), os.ReadFile)
		require.ErrorIs(t, err, errPolicyRules)
		assert.Contains(t, err.Error(), "missing")
	})

	t.Run("他の rego が有効でも、読めない rego があれば失敗する", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{"a.rego": valid, "b.rego": valid})
		_, err := policyRules(dir, func(path string) ([]byte, error) {
			if filepath.Base(path) == "b.rego" {
				return nil, os.ErrPermission
			}

			return os.ReadFile(path)
		})
		require.ErrorIs(t, err, errPolicyRules)
		assert.Contains(t, err.Error(), "b.rego: ファイルを読めません")
	})
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
