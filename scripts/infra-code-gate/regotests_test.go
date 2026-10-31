package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const policyOK = "package infra_code\n\ndeny contains msg if {\n\tmsg := \"IC010 [未分類の型] x\"\n}\n"

const policyTestOK = "package infra_code\n\ntest_IC010_deny_unknown if { true }\n\ntest_IC010_allow_known if { true }\n"

const reportOK = "conftest/hcl/a_test.rego:3:\ndata.infra_code.test_IC010_deny_unknown: PASS (1ms)\ndata.infra_code.test_IC010_allow_known: PASS (1ms)\n"

func Test_runRegoTests(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, policy, test, report string) (string, string) {
		t.Helper()
		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, "conftest/hcl/a.rego"), policy)
		writeFile(t, filepath.Join(repo, "conftest/hcl/a_test.rego"), test)
		p := filepath.Join(t.TempDir(), "report.txt")
		writeFile(t, p, report)
		return repo, p
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		repo, p := setup(t, policyOK, policyTestOK, reportOK)
		var out bytes.Buffer
		require.NoError(t, runRegoTests([]string{"-report", p, "-namespaces", "infra_code"}, repo, &out))
		assert.Contains(t, out.String(), "ID 1 件")
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			policy, test, report string
			noReport             bool
			want                 error
			msg                  string
		}{
			"報告の指定が無ければ使い方のエラー":  {policy: policyOK, test: policyTestOK, noReport: true, want: errUsage},
			"Rego の置き場所が無ければエラー": {policy: "", test: "", report: reportOK, want: os.ErrNotExist},
			"違反のテストの対が欠ければ違反": {
				policy: policyOK, test: "package infra_code\n\ntest_IC010_allow_known if { true }\n",
				report: "data.infra_code.test_IC010_allow_known: PASS (1ms)\n",
				want:   errViolations, msg: "ID IC010 に test_IC010_deny_* のテストがありません",
			},
			"外界に依存する組み込み関数を使えば違反": {
				policy: policyOK + "\nx := http.send({})\n", test: policyTestOK, report: reportOK,
				want: errViolations, msg: "判定を外界に依存させる組み込み関数 http.send",
			},
			"ID を持たない規則があれば違反": {
				policy: policyOK + "\nwarn contains \"no id\" if { true }\n", test: policyTestOK, report: reportOK,
				want: errViolations, msg: "規則 2 個に対し、メッセージの先頭の ID が 1 個です",
			},
			"検出が評価しない名前空間の規則は違反": {
				policy: strings.Replace(policyOK, "package infra_code", "package infra_code_typo", 1),
				test:   strings.Replace(policyTestOK, "package infra_code", "package infra_code_typo", 1),
				report: strings.ReplaceAll(reportOK, "data.infra_code.", "data.infra_code_typo."),
				want:   errViolations, msg: "package infra_code_typo の規則は、検出が評価する名前空間（infra_code）にありません",
			},
			"ID を持つ deny が無ければ0件のエラー": {
				policy: "package infra_code\n", test: policyTestOK, report: reportOK, want: errNoInput,
			},
			"テストが1件も走っていなければ0件のエラー": {
				policy: policyOK, test: policyTestOK, report: "0 tests, 0 passed\n", want: errNoInput,
			},
			"走っていないテストがあれば違反": {
				policy: policyOK, test: policyTestOK + "\ntest_extra if { true }\n", report: reportOK,
				want: errViolations, msg: "テスト data.infra_code.test_extra が走っていません",
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo, p := setup(t, tt.policy, tt.test, tt.report)
				if tt.policy == "" {
					require.NoError(t, os.RemoveAll(filepath.Join(repo, "conftest")))
				}
				args := []string{"-report", p, "-namespaces", "infra_code"}
				if tt.noReport {
					args = []string{"-namespaces", "infra_code"}
				}
				var out bytes.Buffer
				err := runRegoTests(args, repo, &out)
				require.ErrorIs(t, err, tt.want)
				if tt.msg != "" {
					assert.Contains(t, out.String(), tt.msg)
				}
			})
		}
	})
}

func Test_scanRego(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.rego"), policyOK+"deny contains \"IC011 [許可リストに無い] y\" if { false }\n")
	writeFile(t, filepath.Join(dir, "lib/b.rego"), "package lib\n\nx := http.send({})\n")
	writeFile(t, filepath.Join(dir, "a_test.rego"), policyTestOK+"# \"IC099 x\" はテストの中では ID として数えない\n")
	writeFile(t, filepath.Join(dir, "nopkg_test.rego"), "test_x if { true }\n")
	writeFile(t, filepath.Join(dir, "README.md"), "\"IC077 x\"")
	writeFile(t, filepath.Join(dir, "noid.rego"), "package infra_code\n\ndeny contains msg if {\n\tmsg := \"no id\"\n}\n")
	writeFile(t, filepath.Join(dir, "forms.rego"), "package infra_code_input\n\ndeny[msg] if { msg := \"x\" }\n\nviolation contains \"y\" if { true }\n\nwarn contains \"IC012 z\" if { true }\n\ndeny_extra contains \"w\" if { true }\n\ndenies(x) := x\n")
	writeFile(t, filepath.Join(dir, "other.rego"), "package elsewhere\n\ndeny contains \"IC013 a\" if { true }\n")

	suite, err := scanRego(dir, []string{"infra_code", "infra_code_input"})
	require.NoError(t, err)
	assert.Equal(t, []string{"IC010", "IC011", "IC012", "IC013"}, sortedKeys(suite.ids))
	assert.Equal(t, []string{"data.infra_code.test_IC010_allow_known", "data.infra_code.test_IC010_deny_unknown"}, sortedKeys(suite.tests))
	var msgs []string
	for _, f := range suite.findings {
		msgs = append(msgs, f.Message)
	}
	assert.ElementsMatch(t, []string{
		"判定を外界に依存させる組み込み関数 http.send を使っています",
		"package 宣言がありません",
		"規則 1 個に対し、メッセージの先頭の ID が 0 個です。各規則のメッセージを ID で始める",
		"規則 4 個に対し、メッセージの先頭の ID が 1 個です。各規則のメッセージを ID で始める",
		"package elsewhere の規則は、検出が評価する名前空間（infra_code, infra_code_input）にありません",
	}, msgs)

	_, err = scanRego(filepath.Join(dir, "none"), nil)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func Test_checkPairs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ids   map[string]string
		tests []string
		want  []string
	}{
		"両方揃えば違反なし": {
			ids:   map[string]string{"IC010": "a.rego"},
			tests: []string{"data.p.test_IC010_deny_x", "data.p.test_IC010_allow_y", "data.p.test_helper"},
		},
		"違反のテストが欠ける": {
			ids:   map[string]string{"IC010": "a.rego"},
			tests: []string{"data.p.test_IC010_allow_y"},
			want:  []string{"ID IC010 に test_IC010_deny_* のテストがありません"},
		},
		"非違反のテストが欠ける": {
			ids:   map[string]string{"IC010": "a.rego"},
			tests: []string{"data.p.test_IC010_deny_y"},
			want:  []string{"ID IC010 に test_IC010_allow_* のテストがありません"},
		},
		"存在しない ID を名乗るテスト": {
			ids:   map[string]string{"IC010": "a.rego"},
			tests: []string{"data.p.test_IC010_deny_x", "data.p.test_IC010_allow_y", "data.p.test_IC099_deny_z"},
			want:  []string{"テスト test_IC099_deny_z の ID IC099 を出す deny がありません"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			suite := &regoSuite{ids: tt.ids, tests: map[string]string{}}
			for _, n := range tt.tests {
				suite.tests[n] = "a_test.rego"
			}
			got := checkPairs(suite)
			require.Len(t, got, len(tt.want), "%v", got)
			for i, w := range tt.want {
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func Test_readReport(t *testing.T) {
	t.Parallel()

	t.Run("テストごとの結果を読む。同じ名前が FAIL を含めば FAIL を残す", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "r.txt")
		writeFile(t, p, "x.rego:1:\ndata.p.test_a: PASS (1ms)\ndata.p.test_b: FAIL (1ms)\n----\nFAILURES\ndata.p.test_b: FAIL (1ms)\ndata.p.test_c: ERROR (1ms)\nPASS: 1/3\n")
		got, err := readReport(p)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"data.p.test_a": "PASS", "data.p.test_b": "FAIL", "data.p.test_c": "ERROR"}, got)
	})

	t.Run("FAIL の後の PASS でも FAIL を残し、PASS の後の FAIL は FAIL にする", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "r.txt")
		writeFile(t, p, "data.p.test_a: FAIL (1ms)\ndata.p.test_a: PASS (1ms)\ndata.p.test_b: PASS (1ms)\ndata.p.test_b: ERROR (1ms)\n")
		got, err := readReport(p)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"data.p.test_a": "FAIL", "data.p.test_b": "ERROR"}, got)
	})

	t.Run("読めなければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := readReport(filepath.Join(t.TempDir(), "none"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_checkRan(t *testing.T) {
	t.Parallel()

	suite := &regoSuite{tests: map[string]string{"data.p.test_a": "a_test.rego", "data.p.test_b": "a_test.rego", "data.p.test_c": "a_test.rego"}}
	got := checkRan(suite, map[string]string{"data.p.test_a": "PASS", "data.p.test_b": "FAIL", "data.q.test_d": "ERROR", "data.q.test_e": "PASS"})
	require.Len(t, got, 3)
	assert.Equal(t, "テスト data.p.test_b が FAIL です", got[0].Message)
	assert.Equal(t, "テスト data.p.test_c が走っていません", got[1].Message)
	assert.Equal(t, "テスト data.q.test_d が ERROR です", got[2].Message)
}
