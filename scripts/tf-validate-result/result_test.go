package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cleanJSON = `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}`

const awsWarningJSON = `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":2,"diagnostics":[
{"severity":"warning","summary":"Argument is deprecated","detail":"managed_policy_arns is deprecated. Use the aws_iam_role_policy_attachment resource instead.","address":"aws_iam_role.this",
"range":{"filename":"main.tf","start":{"line":14,"column":25,"byte":262},"end":{"line":14,"column":27,"byte":264}},
"snippet":{"context":"resource \"aws_iam_role\" \"this\"","code":"  managed_policy_arns = []","start_line":14,"highlight_start_offset":24,"highlight_end_offset":26,"values":[]}},
{"severity":"warning","summary":"Argument is deprecated","detail":"inline_policy is deprecated. Use the aws_iam_role_policy resource instead.","address":"aws_iam_role.this",
"range":{"filename":"main.tf","start":{"line":11,"column":33,"byte":200},"end":{"line":11,"column":34,"byte":201}},
"snippet":{"context":"resource \"aws_iam_role\" \"this\"","code":"resource \"aws_iam_role\" \"this\" {","start_line":11,"highlight_start_offset":32,"highlight_end_offset":33,"values":[]}}]}`

const errorJSON = `{"format_version":"1.0","valid":false,"error_count":1,"warning_count":0,"diagnostics":[
{"severity":"error","summary":"Reference to undeclared input variable","detail":"An input variable with the name \"missing\" has not been declared.",
"range":{"filename":"main.tf","start":{"line":2,"column":11,"byte":23},"end":{"line":2,"column":22,"byte":34}},
"snippet":{"context":"output \"x\"","code":"  value = var.missing","start_line":2,"highlight_start_offset":10,"highlight_end_offset":21,"values":[]}}]}`

// 子 module の警告は、呼び出し元の validate に `../` で始まるパスで現れる。
func childWarningJSON(filename string) string {
	return `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":1,"diagnostics":[
{"severity":"warning","summary":"Deprecated variable got a value","detail":"use new instead",
"range":{"filename":"` + filename + `","start":{"line":3,"column":12,"byte":40},"end":{"line":3,"column":15,"byte":43}},
"snippet":{"context":"module \"i\"","code":"  old    = \"x\"","start_line":3,"highlight_start_offset":11,"highlight_end_offset":14,"values":[]}}]}`
}

const noRangeWarningJSON = `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":1,"diagnostics":[
{"severity":"warning","summary":"Provider development overrides are in effect","detail":"The following provider development overrides are set."}]}`

func Test_diagnostic_location(t *testing.T) {
	t.Parallel()
	t.Run("位置を持つ diagnostic はディレクトリからのパス・行・列で表す", func(t *testing.T) {
		t.Parallel()
		r, err := parseResult("modules/a", 1, []byte(errorJSON))
		require.NoError(t, err)
		require.Len(t, r.errors, 1)
		assert.Equal(t, "modules/a/main.tf:2:11", r.errors[0].location("modules/a"))
	})
	t.Run("位置を持たない diagnostic はディレクトリと位置なしで表す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "modules/a（位置なし）", diagnostic{}.location("modules/a"))
	})
}

func Test_warning_location(t *testing.T) {
	t.Parallel()
	t.Run("位置を持つ警告はパス・行・列で表す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "modules/a/main.tf:3:12", warning{path: "modules/a/main.tf", line: 3, column: 12}.location())
	})
	t.Run("位置を持たない警告は位置なしで表す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "（位置なし）", warning{}.location())
	})
}

func Test_parseResult(t *testing.T) {
	t.Parallel()
	t.Run("警告もエラーも無い出力は valid で diagnostic を持たない", func(t *testing.T) {
		t.Parallel()
		r, err := parseResult("modules/a", 0, []byte(cleanJSON))
		require.NoError(t, err)
		assert.True(t, r.valid)
		assert.Empty(t, r.errors)
		assert.Empty(t, r.warnings)
	})
	t.Run("警告は終了コード 0 のまま warnings に入る", func(t *testing.T) {
		t.Parallel()
		r, err := parseResult("modules/a", 0, []byte(awsWarningJSON))
		require.NoError(t, err)
		assert.True(t, r.valid)
		require.Len(t, r.warnings, 2)
		assert.Equal(t, "Argument is deprecated", r.warnings[0].Summary)
	})
	t.Run("エラーは valid=false と終了コード 1 で errors に入る", func(t *testing.T) {
		t.Parallel()
		r, err := parseResult("modules/a", 1, []byte(errorJSON))
		require.NoError(t, err)
		assert.False(t, r.valid)
		require.Len(t, r.errors, 1)
		assert.Equal(t, "Reference to undeclared input variable", r.errors[0].Summary)
	})
	t.Run("terraform が付ける末尾の改行は受け入れる", func(t *testing.T) {
		t.Parallel()
		r, err := parseResult("modules/a", 0, []byte(cleanJSON+"\n"))
		require.NoError(t, err)
		assert.True(t, r.valid)
	})
	t.Run("format_version が 1. で始まれば 1.0 以外も受け入れる", func(t *testing.T) {
		t.Parallel()
		_, err := parseResult("modules/a", 0, []byte(`{"format_version":"1.1","valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}`))
		require.NoError(t, err)
	})
	t.Run("同じ警告が並んでもまとめずに数える", func(t *testing.T) {
		t.Parallel()
		data := `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":2,"diagnostics":[
{"severity":"warning","summary":"s","detail":"d"},{"severity":"warning","summary":"s","detail":"d"}]}`
		r, err := parseResult("d", 0, []byte(data))
		require.NoError(t, err)
		assert.Len(t, r.warnings, 2)
	})

	broken := []struct {
		name string
		code int
		data string
		want error
	}{
		{"空の出力", 0, ``, errUnreadable},
		{"JSON でない出力", 1, `Error: Failed to read`, errUnreadable},
		{"JSON の後ろに余計な出力", 0, cleanJSON + "\nnext", errUnreadable},
		{"JSON が2つ続く", 0, cleanJSON + cleanJSON, errUnreadable},
		{"warning_count の型が違う", 0, `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":"0","diagnostics":[]}`, errUnreadable},
		{"JSON が null", 0, `null`, errMalformed},
		{"JSON が配列", 0, `[]`, errUnreadable},
		{"JSON が空のオブジェクト", 0, `{}`, errMalformed},
		{"diagnostics が null", 0, `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":0,"diagnostics":null}`, errMalformed},
		{"diagnostics が無い", 0, `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":0}`, errMalformed},
		{"warning_count が無い", 0, `{"format_version":"1.0","valid":true,"error_count":0,"diagnostics":[]}`, errMalformed},
		{"error_count が無い", 0, `{"format_version":"1.0","valid":true,"warning_count":0,"diagnostics":[]}`, errMalformed},
		{"valid が無い", 0, `{"format_version":"1.0","error_count":0,"warning_count":0,"diagnostics":[]}`, errMalformed},
		{"format_version が無い", 0, `{"valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}`, errMalformed},
		{"format_version が 1 だけ", 0, `{"format_version":"1","valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}`, errMalformed},
		{"format_version が空", 0, `{"format_version":"","valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}`, errMalformed},
		{"format_version が 1. で始まらない", 0, `{"format_version":"2.0","valid":true,"error_count":0,"warning_count":0,"diagnostics":[]}`, errMalformed},
		{"知らない severity", 0, `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":0,"diagnostics":[{"severity":"info","summary":"s","detail":"d"}]}`, errMalformed},
		{"warning_count が diagnostic より多い", 0, `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":1,"diagnostics":[]}`, errCountMismatch},
		{"warning_count が diagnostic より少ない", 0, `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":0,"diagnostics":[{"severity":"warning","summary":"s","detail":"d"}]}`, errCountMismatch},
		{"error_count が diagnostic と合わない", 1, `{"format_version":"1.0","valid":false,"error_count":2,"warning_count":0,"diagnostics":[{"severity":"error","summary":"s","detail":"d"}]}`, errCountMismatch},
		{"終了コード 0 なのに valid=false", 0, errorJSON, errExitMismatch},
		{"終了コードが非0 なのに valid=true", 1, cleanJSON, errExitMismatch},
		{"警告だけで終了コードが非0", 1, awsWarningJSON, errExitMismatch},
		{"valid=true なのにエラーがある", 0, `{"format_version":"1.0","valid":true,"error_count":1,"warning_count":0,"diagnostics":[{"severity":"error","summary":"s","detail":"d"}]}`, errExitMismatch},
		{"valid=false なのにエラーが0件", 1, `{"format_version":"1.0","valid":false,"error_count":0,"warning_count":0,"diagnostics":[]}`, errExitMismatch},
	}
	for _, c := range broken {
		t.Run(c.name+"はエラー", func(t *testing.T) {
			t.Parallel()
			_, err := parseResult("modules/a", c.code, []byte(c.data))
			require.ErrorIs(t, err, c.want)
		})
	}
}

func Test_collectWarnings(t *testing.T) {
	t.Parallel()
	parse := func(t *testing.T, dir, data string) dirResult {
		t.Helper()
		r, err := parseResult(dir, 0, []byte(data))
		require.NoError(t, err)
		return r
	}

	t.Run("子 module の警告は呼び出し元の数だけ現れても1件に数える", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		got, err := collectWarnings(base, []dirResult{
			parse(t, filepath.Join(base, "modules/inner"), childWarningJSON("main.tf")),
			parse(t, filepath.Join(base, "modules/mod"), childWarningJSON("../inner/main.tf")),
			parse(t, filepath.Join(base, "usecases/b"), childWarningJSON("../../modules/inner/main.tf")),
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "modules/inner/main.tf", got[0].path)
		assert.Equal(t, 3, got[0].line)
		assert.Equal(t, 12, got[0].column)
		assert.Equal(t, `module "i"`, got[0].block)
		assert.Equal(t, []string{
			filepath.Join(base, "modules/inner"), filepath.Join(base, "modules/mod"), filepath.Join(base, "usecases/b"),
		}, got[0].dirs)
	})
	t.Run("同じ行の警告でも detail が違えば別に数える", func(t *testing.T) {
		t.Parallel()
		data := `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":2,"diagnostics":[
{"severity":"warning","summary":"Argument is deprecated","detail":"a","range":{"filename":"main.tf","start":{"line":1,"column":1}}},
{"severity":"warning","summary":"Argument is deprecated","detail":"b","range":{"filename":"main.tf","start":{"line":1,"column":1}}}]}`
		got, err := collectWarnings("base", []dirResult{parse(t, "base/modules/a", data)})
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})
	t.Run("同じ detail でも列が違えば別に数える", func(t *testing.T) {
		t.Parallel()
		data := `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":2,"diagnostics":[
{"severity":"warning","summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":1}}},
{"severity":"warning","summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":9}}}]}`
		got, err := collectWarnings("base", []dirResult{parse(t, "base/modules/a", data)})
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})
	differ := []struct{ name, a, b string }{
		{"path だけが違えば別に数える",
			`"summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":1}}`,
			`"summary":"s","detail":"d","range":{"filename":"other.tf","start":{"line":1,"column":1}}`},
		{"行だけが違えば別に数える",
			`"summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":1}}`,
			`"summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":2,"column":1}}`},
		{"summary だけが違えば別に数える",
			`"summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":1}}`,
			`"summary":"t","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":1}}`},
	}
	for _, c := range differ {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data := `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":2,"diagnostics":[` +
				`{"severity":"warning",` + c.a + `},{"severity":"warning",` + c.b + `}]}`
			got, err := collectWarnings("base", []dirResult{parse(t, "base/modules/a", data)})
			require.NoError(t, err)
			assert.Len(t, got, 2)
		})
	}
	inside := []struct{ name, dir, filename, want string }{
		{"基点の直下を指す警告", "base/modules", "../x.tf", "x.tf"},
		{".. で始まる名前のディレクトリ", "base/modules/a", "..foo/main.tf", "modules/a/..foo/main.tf"},
		{".terraform で始まる別の名前のディレクトリ", "base/modules/a", ".terraform-x/main.tf", "modules/a/.terraform-x/main.tf"},
	}
	for _, c := range inside {
		t.Run(c.name+"は基点の内側として数える", func(t *testing.T) {
			t.Parallel()
			got, err := collectWarnings("base", []dirResult{parse(t, c.dir, childWarningJSON(c.filename))})
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, c.want, got[0].path)
		})
	}
	t.Run("tests の下の setup module の警告は基点からのパスで数える", func(t *testing.T) {
		t.Parallel()
		got, err := collectWarnings("base", []dirResult{parse(t, "base/modules/a", childWarningJSON("tests/setup/main.tf"))})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "modules/a/tests/setup/main.tf", got[0].path)
	})
	t.Run("位置を持たない警告は summary と detail で1件にまとめ、パスを持たない", func(t *testing.T) {
		t.Parallel()
		got, err := collectWarnings("base", []dirResult{
			parse(t, "base/modules/a", noRangeWarningJSON), parse(t, "base/modules/b", noRangeWarningJSON),
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Empty(t, got[0].path)
		assert.Equal(t, []string{"base/modules/a", "base/modules/b"}, got[0].dirs)
	})
	t.Run("同じディレクトリで同じ警告が2度出てもディレクトリは1度だけ並べる", func(t *testing.T) {
		t.Parallel()
		data := `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":2,"diagnostics":[
{"severity":"warning","summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":1}}},
{"severity":"warning","summary":"s","detail":"d","range":{"filename":"main.tf","start":{"line":1,"column":1}}}]}`
		got, err := collectWarnings("base", []dirResult{parse(t, "base/modules/a", data)})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, []string{"base/modules/a"}, got[0].dirs)
	})
	t.Run("警告の無い結果は0件", func(t *testing.T) {
		t.Parallel()
		got, err := collectWarnings("base", []dirResult{parse(t, "base/modules/a", cleanJSON)})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	outside := []struct{ name, dir, filename string }{
		{"基点の外を指す警告", "base/modules/a", "../../../outside/main.tf"},
		{"基点の親のファイルを指す警告", "base/modules", "../../x.tf"},
		{"基点の親そのものを指す警告", "base/modules", "../.."},
		{".terraform の下を指す警告", "base/modules/a", ".terraform/modules/m/main.tf"},
	}
	for _, c := range outside {
		t.Run(c.name+"はエラー", func(t *testing.T) {
			t.Parallel()
			_, err := collectWarnings("base", []dirResult{parse(t, c.dir, childWarningJSON(c.filename))})
			require.ErrorIs(t, err, errOutside)
		})
	}
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}
