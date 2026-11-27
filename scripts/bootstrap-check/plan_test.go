package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_checkPlan(t *testing.T) {
	t.Parallel()

	t.Run("入力を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		o := options{headerFile: write(t, dir, "h", headerClean), input: filepath.Join(dir, "missing")}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errInputUnreadable)
	})

	t.Run("期待値を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", filepath.Join(dir, "missing"))
		o := options{headerFile: write(t, dir, "h", headerClean), input: write(t, dir, "p", planJSON(matchingChanges()...))}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errExpectedUnreadable)
	})

	t.Run("期待値が空なら plan が一致していても失敗する", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", "# だけ\n"))
		o := options{headerFile: write(t, dir, "h", headerClean), input: write(t, dir, "p", planJSON(matchingChanges()...))}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errExpectedEmpty)
	})

	t.Run("header を読めなければ、何も出さずに失敗する", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		o := options{headerFile: write(t, dir, "h", "HEAD"), input: write(t, dir, "p", planJSON(matchingChanges()...))}
		require.ErrorIs(t, checkPlan(o, &out, d), errHeader)
		assert.Empty(t, out.String())
	})

	t.Run("plan が0件なら --partial でも失敗する", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		o := options{partial: true, headerFile: write(t, dir, "h", headerClean), input: write(t, dir, "p", planJSON())}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errPlanEmpty)
	})

	t.Run("--partial は apply 済みを済みとする", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", "import b.y\nadd a.x\nadd z.z\n"))
		o := options{partial: true, headerFile: write(t, dir, "h", headerClean), input: write(t, dir, "p", planJSON(matchingChanges()...))}

		var out bytes.Buffer

		require.NoError(t, checkPlan(o, &out, d))
		assert.Contains(t, out.String(), "済み  add z.z\n# 結果（--partial）: 済み 1 件、余剰 0 件\n")
	})

	t.Run("--partial でも余剰があれば差分にする", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", "import b.y\n"))
		o := options{partial: true, headerFile: write(t, dir, "h", headerClean), input: write(t, dir, "p", planJSON(matchingChanges()...))}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errMismatch)
	})

	t.Run("mock は header を読まずに一致を判定する", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		o := options{mock: true, testExitFile: write(t, dir, "x", "0\n"), input: write(t, dir, "t", passingTestOutput(matchingChanges()...))}
		require.NoError(t, checkPlan(o, &bytes.Buffer{}, d))
	})

	t.Run("mock は立ち上げの期待値を足して判定し、メンバーの resource が足りなければ差分にする", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		d.bootstrapping = write(t, dir, "b", "# 立ち上げ\nadd m.x\n")
		member := rc("m.x", `["create"]`, false)

		o := options{mock: true, testExitFile: write(t, dir, "x", "0\n"), input: write(t, dir, "t", passingTestOutput(append(matchingChanges(), member)...))}
		require.NoError(t, checkPlan(o, &bytes.Buffer{}, d))

		o.input = write(t, dir, "t2", passingTestOutput(matchingChanges()...))

		var out bytes.Buffer

		require.ErrorIs(t, checkPlan(o, &out, d), errMismatch)
		assert.Contains(t, out.String(), "NG  不足  add m.x")
	})

	t.Run("mock は立ち上げの期待値が無ければ、メンバーの resource を余剰にする", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		d.bootstrapping = filepath.Join(dir, "missing")
		member := rc("m.x", `["create"]`, false)
		o := options{mock: true, testExitFile: write(t, dir, "x", "0\n"), input: write(t, dir, "t", passingTestOutput(append(matchingChanges(), member)...))}

		var out bytes.Buffer

		require.ErrorIs(t, checkPlan(o, &out, d), errMismatch)
		assert.Contains(t, out.String(), "NG  余剰  add m.x")
	})

	t.Run("mock は立ち上げの期待値を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		d.bootstrapping = dir
		o := options{mock: true, testExitFile: write(t, dir, "x", "0\n"), input: write(t, dir, "t", passingTestOutput(matchingChanges()...))}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errExpectedUnreadable)
	})

	t.Run("mock は2つの期待値にまたがる重複を拒む", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		d.bootstrapping = write(t, dir, "b", "add a.x\n")
		o := options{mock: true, testExitFile: write(t, dir, "x", "0\n"), input: write(t, dir, "t", passingTestOutput(matchingChanges()...))}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errExpectedDuplicate)
	})

	t.Run("実 plan は立ち上げの期待値を読まない", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		d.bootstrapping = write(t, dir, "b", "add m.x\n")
		o := options{headerFile: write(t, dir, "h", headerClean), input: write(t, dir, "p", planJSON(matchingChanges()...))}
		require.NoError(t, checkPlan(o, &bytes.Buffer{}, d))
	})

	t.Run("mock は terraform test の失敗を差分にも一致にもしない", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", write(t, dir, "e", expectedBody))
		o := options{mock: true, testExitFile: write(t, dir, "x", "1\n"), input: write(t, dir, "t", passingTestOutput(matchingChanges()...))}
		require.ErrorIs(t, checkPlan(o, &bytes.Buffer{}, d), errTestFailed)
	})
}

func Test_readInput(t *testing.T) {
	t.Parallel()

	t.Run("パスのファイルを読む", func(t *testing.T) {
		t.Parallel()

		got, err := readInput(write(t, t.TempDir(), "p", "x"), osDeps("stdin"))
		require.NoError(t, err)
		assert.Equal(t, "x", string(got))
	})

	t.Run("- は標準入力を読む", func(t *testing.T) {
		t.Parallel()

		got, err := readInput("-", osDeps("stdin"))
		require.NoError(t, err)
		assert.Equal(t, "stdin", string(got))
	})

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := readInput(filepath.Join(t.TempDir(), "missing"), osDeps(""))
		require.ErrorIs(t, err, errInputUnreadable)
	})

	t.Run("標準入力を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := readInput("-", deps{stdin: errReader{}})
		require.ErrorIs(t, err, errInputUnreadable)
	})
}

func Test_readHeader(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, content := range map[string]string{
			"clean":      headerClean + "\n",
			"改行が無くても読む":  headerClean,
			"未コミットの注記つき": headerDirty + "\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := readHeader(write(t, t.TempDir(), "h", content), osDeps(""))
				require.NoError(t, err)
				assert.Equal(t, strings.TrimSpace(content), got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, content := range map[string]string{
			"空":           "",
			"commit でない":  "HEAD\n",
			"sha が3桁":     "commit: abc\n",
			"sha が39桁":    "commit: " + commitSHA[:39] + "\n",
			"sha が41桁":    "commit: " + commitSHA + "0\n",
			"sha が16進でない": "commit: " + strings.Repeat("g", 40) + "\n",
			"大文字の sha":    "commit: " + strings.ToUpper(commitSHA) + "\n",
			"2行":          headerClean + "\n" + headerClean + "\n",
			"sha の後が空白だけで注記が無い（後ろに別の行）": headerClean + "   \n" + "x",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := readHeader(write(t, t.TempDir(), "h", content), osDeps(""))
				require.ErrorIs(t, err, errHeader)
			})
		}

		t.Run("ファイルが無い", func(t *testing.T) {
			t.Parallel()

			_, err := readHeader(filepath.Join(t.TempDir(), "missing"), osDeps(""))
			require.ErrorIs(t, err, errHeader)
		})
	})
}

func Test_parseExpected(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("コメントと空行を読み飛ばし、整列して返す", func(t *testing.T) {
			t.Parallel()

			got, err := parseExpected([]byte("# c\n\n  # indented\nimport b.y\n\t\nadd a.x\nadd m[\"k\"].x\n"), "e")
			require.NoError(t, err)
			assert.Equal(t, []string{"add a.x", `add m["k"].x`, "import b.y"}, got)
		})

		t.Run("同じ address でも種類が違えば別の行", func(t *testing.T) {
			t.Parallel()

			got, err := parseExpected([]byte("import a.x\nadd a.x\n"), "e")
			require.NoError(t, err)
			assert.Len(t, got, 2)
		})

		t.Run("address に空白を含んでも読む（for_each のキー）", func(t *testing.T) {
			t.Parallel()

			got, err := parseExpected([]byte(`add a.x["k 1"]`+"\n"), "e")
			require.NoError(t, err)
			assert.Equal(t, []string{`add a.x["k 1"]`}, got)
		})

		t.Run("address の途中の連続する空白は読む", func(t *testing.T) {
			t.Parallel()

			got, err := parseExpected([]byte(`add a.x["k  1"]`+"\n"), "e")
			require.NoError(t, err)
			assert.Equal(t, []string{`add a.x["k  1"]`}, got)
		})

		t.Run("最後の行に改行が無くても読む", func(t *testing.T) {
			t.Parallel()

			got, err := parseExpected([]byte("add a.x"), "e")
			require.NoError(t, err)
			assert.Equal(t, []string{"add a.x"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, data := range map[string]string{
			"空のファイル": "",
			"コメントだけ": "# a\n# b\n",
			"空白の行だけ": "  \n\t\n",
		} {
			t.Run(name+"は空として失敗する", func(t *testing.T) {
				t.Parallel()

				_, err := parseExpected([]byte(data), "e")
				require.ErrorIs(t, err, errExpectedEmpty)
			})
		}

		for name, data := range map[string]string{
			"種類が無い":           "a.x\n",
			"知らない種類":          "remove a.x\n",
			"address が無い":     "add\n",
			"address が空":      "add \n",
			"区切りがタブ":          "add\ta.x\n",
			"address の途中にタブ":  "add a\t.x\n",
			"address の途中に CR": "add a\r.x\n",
			"address の後ろにタブ":  "add a.x\t\n",
			"区切りが2つの空白":       "add  a.x\n",
			"address の後ろに空白":  "add a.x \n",
			"行頭に空白":           " add a.x\n",
			"CRLF":            "add a.x\r\n",
			"種類が大文字":          "ADD a.x\n",
		} {
			t.Run(name+"は形の崩れとして失敗する", func(t *testing.T) {
				t.Parallel()

				_, err := parseExpected([]byte(data), "e")
				require.ErrorIs(t, err, errExpectedMalformed)
			})
		}

		t.Run("崩れた行の番号と内容を示す", func(t *testing.T) {
			t.Parallel()

			_, err := parseExpected([]byte("# c\nadd a.x\nbogus line\n"), "exp.txt")
			require.ErrorIs(t, err, errExpectedMalformed)
			assert.Contains(t, err.Error(), `exp.txt の 3 行目 "bogus line"`)
		})

		t.Run("同じ行の重複は、2つ目の行番号を示して失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := parseExpected([]byte("add a.x\nimport a.x\nadd a.x\n"), "exp.txt")
			require.ErrorIs(t, err, errExpectedDuplicate)
			assert.Contains(t, err.Error(), `exp.txt の 3 行目 "add a.x"`)
		})
	})
}

func Test_parsePlan(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("complete と errored が無くても読む", func(t *testing.T) {
			t.Parallel()

			got, err := parsePlan([]byte(`{"format_version":"1.2","resource_changes":[` + rc("a.x", `["create"]`, false) + `]}`))
			require.NoError(t, err)
			assert.Equal(t, []string{"add a.x"}, got)
		})

		t.Run("errored が false で complete が true なら読む", func(t *testing.T) {
			t.Parallel()

			got, err := parsePlan([]byte(planJSON(rc("a.x", `["create"]`, false))))
			require.NoError(t, err)
			assert.Equal(t, []string{"add a.x"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		change := rc("a.x", `["create"]`, false)

		for name, tc := range map[string]struct {
			data string
			want error
		}{
			"JSON でない":                          {`not json`, errPlanJSON},
			"空":                                 {``, errPlanJSON},
			"null":                              {`null`, errPlanJSON},
			"オブジェクトでない":                         {`[]`, errPlanJSON},
			"format_version が無い":                {`{"resource_changes":[` + change + `]}`, errPlanJSON},
			"format_version が文字列でない":            {`{"format_version":1,"resource_changes":[` + change + `]}`, errPlanJSON},
			"format_version が空":                 {`{"format_version":"","resource_changes":[` + change + `]}`, errMockVersion},
			"format_version が 2.0":              {`{"format_version":"2.0","resource_changes":[` + change + `]}`, errMockVersion},
			"errored が null":                    {`{"format_version":"1.2","errored":null,"resource_changes":[` + change + `]}`, errPlanShape},
			"errored が文字列":                      {`{"format_version":"1.2","errored":"true","resource_changes":[` + change + `]}`, errPlanShape},
			"errored が数値":                       {`{"format_version":"1.2","errored":1,"resource_changes":[` + change + `]}`, errPlanShape},
			"complete が null":                   {`{"format_version":"1.2","complete":null,"resource_changes":[` + change + `]}`, errPlanShape},
			"complete が文字列":                     {`{"format_version":"1.2","complete":"false","resource_changes":[` + change + `]}`, errPlanShape},
			"complete が数値":                      {`{"format_version":"1.2","complete":0,"resource_changes":[` + change + `]}`, errPlanShape},
			"errored が true で complete が false": {`{"format_version":"1.2","errored":true,"complete":false,"resource_changes":[` + change + `]}`, errPlanErrored},
			"errored が true":                    {`{"format_version":"1.2","errored":true,"resource_changes":[` + change + `]}`, errPlanErrored},
			"complete が false":                  {`{"format_version":"1.2","complete":false,"resource_changes":[` + change + `]}`, errPlanIncomplete},
			"resource_changes が無い":              {`{"format_version":"1.2"}`, errPlanEmpty},
			"resource_changes が空":               {`{"format_version":"1.2","resource_changes":[]}`, errPlanEmpty},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := parsePlan([]byte(tc.data))
				require.ErrorIs(t, err, tc.want)
			})
		}
	})
}

func Test_boolOr(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		top  string
		def  bool
		want bool
	}{
		"true":      {`{"k":true}`, false, true},
		"false":     {`{"k":false}`, true, false},
		"前後に空白の真偽値": {`{"k": true }`, false, true},
		"キーが無ければ既定": {`{}`, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var top map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(tc.top), &top))

			got, err := boolOr(top, "k", tc.def)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	for name, raw := range map[string]string{
		"null": `null`, "文字列": `"true"`, "数値": `1`, "配列": `[]`,
	} {
		t.Run("真偽値でない（"+name+"）は形の誤りとして失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := boolOr(map[string]json.RawMessage{"k": json.RawMessage(raw)}, "k", false)
			require.ErrorIs(t, err, errPlanShape)
		})
	}
}

func Test_hasMajorOne(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		raw  string
		want bool
	}{
		"1.2":  {`"1.2"`, true},
		"1.":   {`"1."`, true},
		"2.0":  {`"2.0"`, false},
		"10.0": {`"10.0"`, false},
		"空":    {`""`, false},
		"1":    {`"1"`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, hasMajorOne(json.RawMessage(tc.raw)))
		})
	}
}

func Test_isString(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		raw  string
		want bool
	}{
		"文字列":       {`"1.2"`, true},
		"前に空白の文字列":  {` "1.2"`, true},
		"数値":        {`1.2`, false},
		"null":      {`null`, false},
		"空":         {``, false},
		"配列":        {`["a"]`, false},
		"空白だけ":      {" \t\n", false},
		"true":      {`true`, false},
		"オブジェクト":    {`{}`, false},
		"閉じていない文字列": {`"1.2`, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, isString(json.RawMessage(tc.raw)))
		})
	}
}

func Test_changeLines(t *testing.T) {
	t.Parallel()

	lines := func(t *testing.T, changes ...string) []string {
		t.Helper()

		got, err := changeLines(json.RawMessage("[" + strings.Join(changes, ",") + "]"))
		require.NoError(t, err)

		return got
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("no-op の取り込みは import だけを出す", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"import a.x"}, lines(t, rc("a.x", `["no-op"]`, true)))
		})

		t.Run("create の取り込みは add と import を出す", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"add a.x", "import a.x"}, lines(t, rc("a.x", `["create"]`, true)))
		})

		t.Run("importing の キーが無い変更は取り込みに数えない", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"add a.x"}, lines(t, `{"address":"a.x","mode":"managed","change":{"actions":["create"]}}`))
		})

		t.Run("importing が null の変更は取り込みに数えない", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"add a.x"}, lines(t, rc("a.x", `["create"]`, false)))
		})

		t.Run("no-op は何も出さない（ほかの resource が在れば0件でも検査した）", func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, lines(t, rc("a.x", `["no-op"]`, false)))
		})

		t.Run("data source は数えない", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"add a.x"}, lines(t, rc("a.x", `["create"]`, false), dataRC("data.c")))
		})

		t.Run("address が空の data source は読み飛ばす", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"add a.x"}, lines(t, rc("a.x", `["create"]`, false),
				`{"address":"","mode":"data","change":{"actions":["read"]}}`))
		})

		t.Run("行は整列して返す", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{"add a", "add b"}, lines(t, rc("b", `["create"]`, false), rc("a", `["create"]`, false)))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, raw := range map[string]string{
			"null":           `null`,
			"欠けている":          ``,
			"空の配列":           `[]`,
			"data source だけ": `[` + dataRC("data.c") + `]`,
		} {
			t.Run("resource が0件（"+name+"）は失敗する", func(t *testing.T) {
				t.Parallel()

				_, err := changeLines(json.RawMessage(raw))
				require.ErrorIs(t, err, errPlanEmpty)
			})
		}

		for name, raw := range map[string]string{
			"配列でない":                      `{}`,
			"actions が文字列":               `[{"address":"a","mode":"managed","change":{"actions":"create"}}]`,
			"address が無い":                `[{"mode":"managed","change":{"actions":["create"]}}]`,
			"address が空":                 `[{"address":"","mode":"managed","change":{"actions":["create"]}}]`,
			"mode が数値":                   `[{"address":"a","mode":1,"change":{"actions":["create"]}}]`,
			"mode が無い":                   `[{"address":"a","change":{"actions":["create"]}}]`,
			"mode が空":                    `[{"address":"a","mode":"","change":{"actions":["create"]}}]`,
			"data source の actions が文字列": `[{"address":"data.d","mode":"data","change":{"actions":"read"}},` + rc("a.x", `["create"]`, false) + `]`,
			"mode が大文字始まり":               `[{"address":"a","mode":"Managed","change":{"actions":["create"]}}]`,
			"mode が知らない値":                `[{"address":"a","mode":"ephemeral","change":{"actions":["create"]}}]`,
		} {
			t.Run("形が違う（"+name+"）は失敗する", func(t *testing.T) {
				t.Parallel()

				_, err := changeLines(json.RawMessage(raw))
				require.ErrorIs(t, err, errPlanShape)
			})
		}
	})
}

func Test_actionLines(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		actions []string
		want    []string
	}{
		"no-op は出さない":      {[]string{"no-op"}, nil},
		"create は add":     {[]string{"create"}, []string{"add a"}},
		"update は change":  {[]string{"update"}, []string{"change a"}},
		"delete は destroy": {[]string{"delete"}, []string{"destroy a"}},
		"delete,create の置き換えは add と destroy": {[]string{"delete", "create"}, []string{"add a", "destroy a"}},
		"create,delete の置き換えは add と destroy": {[]string{"create", "delete"}, []string{"add a", "destroy a"}},
		"read は知らない actions として行にする":         {[]string{"read"}, []string{"unknown(read) a"}},
		"forget は知らない actions として行にする":       {[]string{"forget"}, []string{"unknown(forget) a"}},
		"空の actions は知らない actions":           {[]string{}, []string{"unknown() a"}},
		"actions が無い（null）は知らない actions":     {nil, []string{"unknown() a"}},
		"update と create の組は知らない actions":    {[]string{"update", "create"}, []string{"unknown(update,create) a"}},
		"create が2つは知らない actions":            {[]string{"create", "create"}, []string{"unknown(create,create) a"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, actionLines(tc.actions, "a"))
		})
	}
}

func Test_readTestOutput(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("run が pass した回数と plan を数える", func(t *testing.T) {
			t.Parallel()

			got, err := readTestOutput([]byte(passingTestOutput(matchingChanges()...)))
			require.NoError(t, err)
			assert.Equal(t, 1, got.passed)
			assert.False(t, got.failed)
			assert.Len(t, got.plans, 1)
			assert.Empty(t, got.errors)
		})

		t.Run("別の run と別のファイルの pass と plan は数えない", func(t *testing.T) {
			t.Parallel()

			got, err := readTestOutput([]byte(testOutputOf(
				runEvent(mockFile, "other", "pass"), runEvent("tests/other.tftest.hcl", mockRun, "pass"),
				planEvent("tests/other.tftest.hcl", mockRun, `"1.2"`))))
			require.NoError(t, err)
			assert.Zero(t, got.passed)
			assert.Empty(t, got.plans)
		})

		t.Run("どの run でも fail と error は失敗として数える", func(t *testing.T) {
			t.Parallel()

			for _, status := range []string{"fail", "error"} {
				got, err := readTestOutput([]byte(testOutputOf(runEvent("tests/other.tftest.hcl", "x", status))))
				require.NoError(t, err)
				assert.True(t, got.failed, status)
			}
		})

		t.Run("skip は失敗に数えない", func(t *testing.T) {
			t.Parallel()

			got, err := readTestOutput([]byte(testOutputOf(runEvent(mockFile, mockRun, "skip"))))
			require.NoError(t, err)
			assert.False(t, got.failed)
		})

		t.Run("error の level の文を順に集める", func(t *testing.T) {
			t.Parallel()

			got, err := readTestOutput([]byte(testOutputOf(errorEvent("a"), `{"type":"log","@level":"info","@message":"i"}`, errorEvent("b"))))
			require.NoError(t, err)
			assert.Equal(t, []string{"a", "b"}, got.errors)
		})

		t.Run("空行と空のファイルは0件として読む", func(t *testing.T) {
			t.Parallel()

			got, err := readTestOutput([]byte("\n\n"))
			require.NoError(t, err)
			assert.Zero(t, got.passed)

			got, err = readTestOutput(nil)
			require.NoError(t, err)
			assert.Empty(t, got.plans)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, data := range map[string]string{
			"JSON でない行":     "not json\n",
			"途中から壊れる":       runEvent(mockFile, mockRun, "pass") + "\n{\n",
			"オブジェクトでない行":    "1\n",
			"test_run が文字列": `{"type":"test_run","test_run":"x"}` + "\n",
			"test_plan が配列": `{"type":"test_plan","test_plan":[]}` + "\n",
		} {
			t.Run(name+"は失敗する", func(t *testing.T) {
				t.Parallel()

				_, err := readTestOutput([]byte(data))
				require.ErrorIs(t, err, errMockUnreadable)
			})
		}
	})
}

func Test_checkMock(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, output, exitContent string) ([]string, string, error) {
		t.Helper()

		if exitContent == "" {
			exitContent = "0\n"
		}

		o := options{mock: true, testExitFile: write(t, t.TempDir(), "x", exitContent)}

		var out bytes.Buffer

		got, err := checkMock(o, []byte(output), &out, osDeps(""))

		return got, out.String(), err
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("pass した run の plan から行を返す", func(t *testing.T) {
			t.Parallel()

			got, out, err := check(t, passingTestOutput(matchingChanges()...), "0\n")
			require.NoError(t, err)
			assert.Equal(t, []string{"add a.x", "import b.y"}, got)
			assert.Empty(t, out)
		})

		t.Run("plan_format_version が 1. で始まれば版が違っても読む", func(t *testing.T) {
			t.Parallel()

			output := testOutputOf(runEvent(mockFile, mockRun, "pass"), planEvent(mockFile, mockRun, `"1.0"`, rc("a.x", `["create"]`, false)))
			_, _, err := check(t, output, "")
			require.NoError(t, err)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		pass := runEvent(mockFile, mockRun, "pass")
		plan := planEvent(mockFile, mockRun, `"1.2"`, rc("a.x", `["create"]`, false))

		for name, tc := range map[string]struct {
			output string
			want   error
		}{
			"run が fail":     {testOutputOf(runEvent(mockFile, mockRun, "fail"), plan), errTestFailed},
			"別の run が error": {testOutputOf(pass, plan, runEvent(mockFile, "other", "error")), errTestFailed},
			"出力が空":           {"", errMockNotPassed},
			"pass の行が無い":     {testOutputOf(plan), errMockNotPassed},
			"pass が2回":       {testOutputOf(pass, pass, plan), errMockNotPassed},
			"別のファイルの run だけが pass": {testOutputOf(runEvent("tests/other.tftest.hcl", mockRun, "pass"), plan), errMockNotPassed},
			"plan が0件":                   {testOutputOf(pass), errMockPlanCount},
			"plan が2件":                   {testOutputOf(pass, plan, plan), errMockPlanCount},
			"別のファイルの plan だけ":            {testOutputOf(pass, planEvent("tests/other.tftest.hcl", mockRun, `"1.2"`)), errMockPlanCount},
			"plan_format_version が 2.0":  {testOutputOf(pass, planEvent(mockFile, mockRun, `"2.0"`, rc("a.x", `["create"]`, false))), errMockVersion},
			"plan_format_version が数値":    {testOutputOf(pass, planEvent(mockFile, mockRun, `1.2`, rc("a.x", `["create"]`, false))), errMockVersion},
			"plan_format_version が null": {testOutputOf(pass, planEvent(mockFile, mockRun, `null`, rc("a.x", `["create"]`, false))), errMockVersion},
			"plan_format_version が 10.0": {testOutputOf(pass, planEvent(mockFile, mockRun, `"10.0"`, rc("a.x", `["create"]`, false))), errMockVersion},
			"plan に resource が0件":        {testOutputOf(pass, planEvent(mockFile, mockRun, `"1.2"`)), errPlanEmpty},
			"出力が JSON でない":               {"not json\n", errMockUnreadable},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, _, err := check(t, tc.output, "")
				require.ErrorIs(t, err, tc.want)
			})
		}

		t.Run("terraform test の終了コードが 0 以外なら、出力が pass でも失敗する", func(t *testing.T) {
			t.Parallel()

			_, _, err := check(t, passingTestOutput(matchingChanges()...), "1\n")
			require.ErrorIs(t, err, errTestFailed)
		})

		t.Run("終了コードのファイルを読めなければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, _, err := check(t, passingTestOutput(matchingChanges()...), "x\n")
			require.ErrorIs(t, err, errInputUnreadable)
		})

		t.Run("失敗のときは出力の error の文を出す", func(t *testing.T) {
			t.Parallel()

			_, out, err := check(t, testOutputOf(errorEvent("Error: a"), errorEvent("Error: b"), runEvent(mockFile, mockRun, "fail")), "")
			require.ErrorIs(t, err, errTestFailed)
			assert.Equal(t, "Error: a\nError: b\n", out)
		})

		t.Run("pass で終わっていないときも出力の error の文を出す", func(t *testing.T) {
			t.Parallel()

			_, out, err := check(t, testOutputOf(errorEvent("Error: init")), "")
			require.ErrorIs(t, err, errMockNotPassed)
			assert.Equal(t, "Error: init\n", out)
		})

		t.Run("終了コードが 0 以外で出力が空なら、pass が無い失敗より terraform test の失敗が先に立つ", func(t *testing.T) {
			t.Parallel()

			_, _, err := check(t, "", "1\n")
			require.ErrorIs(t, err, errTestFailed)
		})

		t.Run("plan の件数の違いは件数を述べる", func(t *testing.T) {
			t.Parallel()

			_, _, err := check(t, testOutputOf(pass), "")
			require.ErrorIs(t, err, errMockPlanCount)
			assert.Contains(t, err.Error(), "0 件")
		})
	})
}

func Test_testExitFailed(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			content string
			want    bool
		}{
			"0":       {"0\n", false},
			"改行が無い 0": {"0", false},
			"1":       {"1\n", true},
			"2":       {"2\n", true},
			"130":     {"130\n", true},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := testExitFailed(write(t, t.TempDir(), "x", tc.content), osDeps(""))
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, content := range map[string]string{
			"空":       "",
			"数字でない":   "x\n",
			"負の数":     "-1\n",
			"2行":      "0\n0\n",
			"桁があふれる数": "99999999999999999999999\n",
		} {
			t.Run(name+"は失敗する", func(t *testing.T) {
				t.Parallel()

				_, err := testExitFailed(write(t, t.TempDir(), "x", content), osDeps(""))
				require.ErrorIs(t, err, errInputUnreadable)
			})
		}

		t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := testExitFailed(filepath.Join(t.TempDir(), "missing"), osDeps(""))
			require.ErrorIs(t, err, errInputUnreadable)
		})
	})
}

func Test_diffLines(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		expected, actual []string
		missing, extra   []string
	}{
		"一致":              {[]string{"a", "b"}, []string{"a", "b"}, nil, nil},
		"期待値にだけ在る行は不足":    {[]string{"a", "b"}, []string{"a"}, []string{"b"}, nil},
		"実際にだけ在る行は余剰":     {[]string{"a"}, []string{"a", "b"}, nil, []string{"b"}},
		"間に差が在っても取りこぼさない": {[]string{"a", "c", "e"}, []string{"b", "c", "d"}, []string{"a", "e"}, []string{"b", "d"}},
		"重複は多重度で比べる":      {[]string{"a"}, []string{"a", "a"}, nil, []string{"a"}},
		"期待値の重複が足りなければ不足": {[]string{"a", "a"}, []string{"a"}, []string{"a"}, nil},
		"実際が空なら全部が不足":     {[]string{"a", "b"}, nil, []string{"a", "b"}, nil},
		"期待値が空なら全部が余剰":    {nil, []string{"a", "b"}, nil, []string{"a", "b"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			missing, extra := diffLines(tc.expected, tc.actual)
			assert.Equal(t, tc.missing, missing)
			assert.Equal(t, tc.extra, extra)
		})
	}
}

func Test_summary(t *testing.T) {
	t.Parallel()

	t.Run("種類ごとに数え、区切りは ` / `", func(t *testing.T) {
		t.Parallel()

		got := summary("plan", []string{"add a", "add b", "change c", "destroy d", "import e", "import f", "import g"})
		assert.Equal(t, "# plan: import 3 / add 2 / change 1 / destroy 1", got)
	})

	t.Run("0件の種類も0と出す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "# 期待値: import 0 / add 0 / change 0 / destroy 0", summary("期待値", nil))
	})

	t.Run("知らない actions の行はどの種類にも数えない", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "# plan: import 0 / add 1 / change 0 / destroy 0", summary("plan", []string{"add a", "unknown(read) b"}))
	})

	t.Run("種類の語が address の前方に在る行を取り違えない", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "# plan: import 0 / add 0 / change 0 / destroy 0", summary("plan", []string{"addx a"}))
	})
}

func Test_compare(t *testing.T) {
	t.Parallel()

	compareOut := func(expected, actual []string, partial bool) (string, error) {
		var out bytes.Buffer

		err := compare(&out, "plan", expected, actual, partial)

		return out.String(), err
	}

	t.Run("一致", func(t *testing.T) {
		t.Parallel()

		out, err := compareOut([]string{"add a", "import b"}, []string{"add a", "import b"}, false)
		require.NoError(t, err)
		assert.Equal(t, "# 期待値: import 1 / add 1 / change 0 / destroy 0\n"+
			"# plan: import 1 / add 1 / change 0 / destroy 0\n"+
			"# 結果: 不足 0 件、余剰 0 件\n", out)
	})

	t.Run("余剰が先、不足が後で、行は NG と半角2つの空白で区切る", func(t *testing.T) {
		t.Parallel()

		out, err := compareOut([]string{"add a", "add m"}, []string{"add a", "add z", "change y"}, false)
		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "# 期待値: import 0 / add 2 / change 0 / destroy 0\n"+
			"# plan: import 0 / add 2 / change 1 / destroy 0\n"+
			"NG  余剰  add z\nNG  余剰  change y\n"+
			"NG  不足  add m\n"+
			"# 結果: 不足 1 件、余剰 2 件\n", out)
	})

	t.Run("不足だけでも差分", func(t *testing.T) {
		t.Parallel()

		_, err := compareOut([]string{"add a", "add b"}, []string{"add a"}, false)
		require.ErrorIs(t, err, errMismatch)
	})

	t.Run("余剰だけでも差分", func(t *testing.T) {
		t.Parallel()

		_, err := compareOut([]string{"add a"}, []string{"add a", "add b"}, false)
		require.ErrorIs(t, err, errMismatch)
	})

	t.Run("--partial は不足を済みとして出し、余剰が無ければ一致", func(t *testing.T) {
		t.Parallel()

		out, err := compareOut([]string{"add a", "add b"}, []string{"add a"}, true)
		require.NoError(t, err)
		assert.Equal(t, "# 期待値: import 0 / add 2 / change 0 / destroy 0\n"+
			"# plan: import 0 / add 1 / change 0 / destroy 0\n"+
			"済み  add b\n"+
			"# 結果（--partial）: 済み 1 件、余剰 0 件\n", out)
	})

	t.Run("--partial でも余剰があれば差分", func(t *testing.T) {
		t.Parallel()

		out, err := compareOut([]string{"add a"}, []string{"add a", "change b"}, true)
		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, out, "NG  余剰  change b\n# 結果（--partial）: 済み 0 件、余剰 1 件\n")
		assert.NotContains(t, out, "NG  不足")
	})

	t.Run("知らない actions の行は余剰として差分になる", func(t *testing.T) {
		t.Parallel()

		out, err := compareOut([]string{"add a"}, []string{"add a", "unknown(read) b"}, false)
		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, out, "NG  余剰  unknown(read) b\n")
	})
}

func Test_withBootstrapping(t *testing.T) {
	t.Parallel()

	t.Run("足した行を整列して返す", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", filepath.Join(dir, "e"))
		d.bootstrapping = write(t, dir, "b", "# 立ち上げ\nadd c.z\nadd a.a\n")
		got, err := withBootstrapping([]string{"add b.y", "import d.w"}, d)
		require.NoError(t, err)
		assert.Equal(t, []string{"add a.a", "add b.y", "add c.z", "import d.w"}, got)
	})

	t.Run("ファイルが無ければ期待値をそのまま返す", func(t *testing.T) {
		t.Parallel()

		d := osDepsExpected("", "e")
		d.bootstrapping = filepath.Join(t.TempDir(), "missing")
		got, err := withBootstrapping([]string{"add b.y"}, d)
		require.NoError(t, err)
		assert.Equal(t, []string{"add b.y"}, got)
	})

	t.Run("コメントだけなら errExpectedEmpty", func(t *testing.T) {
		t.Parallel()

		d := osDepsExpected("", "e")
		d.bootstrapping = write(t, t.TempDir(), "b", "# だけ\n")
		_, err := withBootstrapping([]string{"add b.y"}, d)
		require.ErrorIs(t, err, errExpectedEmpty)
	})

	t.Run("形の崩れた行は、そのファイルを名指しして errExpectedMalformed", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		d := osDepsExpected("", "e")
		d.bootstrapping = write(t, dir, "b", "create x.y\n")
		_, err := withBootstrapping([]string{"add b.y"}, d)
		require.ErrorIs(t, err, errExpectedMalformed)
		assert.Contains(t, err.Error(), d.bootstrapping+" の 1 行目")
	})

	t.Run("2つにまたがる重複は、両方を名指しして errExpectedDuplicate", func(t *testing.T) {
		t.Parallel()

		d := osDepsExpected("", "e")
		d.bootstrapping = write(t, t.TempDir(), "b", "add b.y\n")
		_, err := withBootstrapping([]string{"add b.y"}, d)
		require.ErrorIs(t, err, errExpectedDuplicate)
		assert.Contains(t, err.Error(), "e と "+d.bootstrapping)
	})
}
