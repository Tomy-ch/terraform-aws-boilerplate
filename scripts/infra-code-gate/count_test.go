package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func result(name string, successes, failures int) conftestResult {
	r := conftestResult{Filename: name, Namespace: "infra_code_input", Successes: successes}
	for range failures {
		r.Failures = append(r.Failures, []byte(`{}`))
	}
	return r
}

// git は、追跡しているファイルの列挙が検査の入力なので、差し替えずに実物を使う。
func git(t *testing.T, repo string, args ...string) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	require.NoError(t, err, "git が要ります: %s", out)
}

func Test_runCount(t *testing.T) {
	t.Parallel()

	const healthyCombined = `[{"filename": "Combined", "namespace": "infra_code", "successes": 15}]`
	const healthyResults = `[{"filename": "modules/a/main.tf", "namespace": "n", "successes": 3}, {"filename": "env/dev/main.tf", "namespace": "n", "successes": 3}]`

	// setup は、3つの root を持ち、構成ファイルを追跡しているリポジトリを建てる。extra は root の外などへ足すファイル。
	setup := func(t *testing.T, results, combined string, extra ...string) (repo, resultsPath, combinedPath string) {
		t.Helper()
		repo = t.TempDir()
		git(t, repo, "init", "-q")
		writeFile(t, filepath.Join(repo, "modules/a/main.tf"), "x")
		writeFile(t, filepath.Join(repo, "env/dev/main.tf"), "x")
		require.NoError(t, os.MkdirAll(filepath.Join(repo, "usecases"), 0o750))
		for _, f := range extra {
			writeFile(t, filepath.Join(repo, f), "x")
		}
		git(t, repo, "add", "-A", "-f")
		dir := t.TempDir()
		resultsPath = filepath.Join(dir, "results.json")
		combinedPath = filepath.Join(dir, "combined.json")
		writeFile(t, resultsPath, results)
		writeFile(t, combinedPath, combined)
		return repo, resultsPath, combinedPath
	}
	roots := []string{"usecases", "modules", "env"}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		repo, r, c := setup(t, healthyResults, healthyCombined)
		var out bytes.Buffer
		require.NoError(t, runCount(append([]string{"-results", r, "-combined", c}, roots...), repo, &out))
		assert.Contains(t, out.String(), "2 件が、独立の走査の 2 件と一致しました")
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			results, combined string
			extra             []string
			args              func(r, c string) []string
			untracked         bool
			want              error
			msg               string
		}{
			"結果の指定が無ければ使い方のエラー": {
				args: func(_, _ string) []string { return roots }, want: errUsage,
			},
			"検出の結果の指定が無ければ使い方のエラー": {
				args: func(r, _ string) []string { return append([]string{"-results", r}, roots...) }, want: errUsage,
			},
			"root の指定が無ければ使い方のエラー": {
				args: func(r, c string) []string { return []string{"-results", r, "-combined", c} }, want: errUsage,
			},
			"root が1つでも無ければエラー": {
				args: func(r, c string) []string { return []string{"-results", r, "-combined", c, "modules", "no-such"} }, want: errMissingRoot,
			},
			"走査の対象が0件なら、結果を読む前に0件のエラー": {
				results: `not json`,
				args:    func(r, c string) []string { return []string{"-results", r, "-combined", c, "usecases"} },
				want:    errNoInput,
			},
			"結果を解釈できなければエラー": {results: `not json`, want: errResults},
			"結果のファイルが無ければエラー": {
				args: func(_, c string) []string { return append([]string{"-results", "/no/such", "-combined", c}, roots...) }, want: os.ErrNotExist,
			},
			"検出の結果を解釈できなければエラー":     {combined: `{}`, want: errResults},
			"追跡しているファイルが0件なら0件のエラー": {untracked: true, want: errNoInput},
			"Conftest が読んでいないファイルがあれば違反": {
				results: `[{"filename": "modules/a/main.tf", "namespace": "n", "successes": 1}]`,
				want:    errViolations, msg: "Conftest がこのファイルを読んでいません",
			},
			"検出の結果が空なら違反": {combined: `[]`, want: errViolations, msg: "検出の結果が1件もありません"},
			"検出の規則が評価されていなければ違反": {
				combined: `[{"filename": "Combined", "namespace": "no_such"}]`,
				want:     errViolations, msg: `検出の名前空間 "no_such" の規則が1つも評価されていません`,
			},
			"root の外に追跡している構成ファイルがあれば違反": {
				extra: []string{"docs/x.tf"}, want: errViolations, msg: "走査の root（usecases modules env）の外に在ります",
			},
			".terraform の下に追跡しているファイルがあれば違反": {
				extra: []string{"modules/a/.terraform/evil/main.tf"}, want: errViolations, msg: "`.terraform` の下のファイルを追跡しています",
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				results, combined := tt.results, tt.combined
				if results == "" {
					results = healthyResults
				}
				if combined == "" {
					combined = healthyCombined
				}
				repo, r, c := setup(t, results, combined, tt.extra...)
				if tt.untracked {
					git(t, repo, "rm", "-q", "-r", "--cached", ".")
				}
				args := append([]string{"-results", r, "-combined", c}, roots...)
				if tt.args != nil {
					args = tt.args(r, c)
				}
				var out bytes.Buffer
				err := runCount(args, repo, &out)
				require.ErrorIs(t, err, tt.want)
				if tt.msg != "" {
					assert.Contains(t, out.String(), tt.msg)
				}
			})
		}
	})
}

func Test_readResults(t *testing.T) {
	t.Parallel()

	t.Run("要素を読む", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "r.json")
		writeFile(t, p, `[{"filename": "a.tf", "namespace": "n", "successes": 2, "failures": [{"msg": "x"}]}]`)
		got, err := readResults(p)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "a.tf", got[0].Filename)
		assert.Equal(t, 2, got[0].Successes)
		assert.Len(t, got[0].Failures, 1)
	})

	t.Run("配列でなければエラー", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "r.json")
		writeFile(t, p, `{"filename": "a.tf"}`)
		_, err := readResults(p)
		require.ErrorIs(t, err, errResults)
	})
}

func Test_walkConfigs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	for _, f := range []string{
		"modules/a/main.tf", "modules/a/x.tf.json", "modules/a/tests/a.tftest.hcl", "modules/a/tests/a.tftest.json",
		"modules/a/q.tfquery.hcl", "modules/a/.terraform/modules/m/main.tf", "modules/a/terraform.tfvars",
		"modules/a/.terraform.lock.hcl", "env/dev/main.tf", "docs/main.tf",
	} {
		writeFile(t, filepath.Join(repo, f), "x")
	}

	want := []string{
		"env/dev/main.tf", "modules/a/main.tf", "modules/a/q.tfquery.hcl",
		"modules/a/tests/a.tftest.hcl", "modules/a/tests/a.tftest.json", "modules/a/x.tf.json",
	}

	t.Run("構成ファイルだけを、.terraform に降りずに集める", func(t *testing.T) {
		t.Parallel()
		got, err := walkConfigs(repo, []string{"modules", "env"})
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("重なった root でも同じファイルを2回数えない", func(t *testing.T) {
		t.Parallel()
		got, err := walkConfigs(repo, []string{"modules", "modules/a", "env"})
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("無い root はエラー", func(t *testing.T) {
		t.Parallel()
		_, err := walkConfigs(repo, []string{"modules", "usecases"})
		require.ErrorIs(t, err, errMissingRoot)
	})

	t.Run("ディレクトリでない root はエラー", func(t *testing.T) {
		t.Parallel()
		_, err := walkConfigs(repo, []string{"modules/a/main.tf"})
		require.ErrorIs(t, err, errMissingRoot)
	})
}

func Test_compare(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		results []conftestResult
		walked  []string
		want    []string
	}{
		"集合が一致すれば違反なし": {
			results: []conftestResult{result("a.tf", 1, 0), result("b.tftest.hcl", 0, 1)},
			walked:  []string{"a.tf", "b.tftest.hcl"},
		},
		"規則が評価されていない要素は違反": {
			results: []conftestResult{result("a.tf", 0, 0)},
			walked:  []string{"a.tf"},
			want:    []string{`名前空間 "infra_code_input" の規則が1つも評価されていません`},
		},
		"走査にだけ在るファイル": {
			results: []conftestResult{result("a.tf", 1, 0)},
			walked:  []string{"a.tf", "b.tf"},
			want:    []string{"Conftest がこのファイルを読んでいません"},
		},
		"Conftest の結果にだけ在るファイル": {
			results: []conftestResult{result("a.tf", 1, 0), result("./b.tf", 1, 0)},
			walked:  []string{"a.tf"},
			want:    []string{"Conftest が読んだファイルが、独立の走査に現れません"},
		},
		"構成ファイルでない結果は集合に入れない": {
			results: []conftestResult{result("a.tf", 1, 0), result("x.tfvars", 0, 1)},
			walked:  []string{"a.tf"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := compare(tt.results, tt.walked)
			require.Len(t, got, len(tt.want), "%v", got)
			for i, w := range tt.want {
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func Test_evaluated(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		results []conftestResult
		want    []string
	}{
		"規則が評価されていれば違反なし": {results: []conftestResult{{Namespace: "infra_code", Successes: 15}}},
		"違反だけでも評価されている":   {results: []conftestResult{result("Combined", 0, 2)}},
		"結果が無い":     {results: nil, want: []string{"検出の結果が1件もありません"}},
		"規則の無い名前空間": {results: []conftestResult{{Namespace: "no_such"}}, want: []string{`検出の名前空間 "no_such" の規則が1つも評価されていません`}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := evaluated(tt.results, "combined.json")
			require.Len(t, got, len(tt.want), "%v", got)
			for i, w := range tt.want {
				assert.Equal(t, w, got[i].Message)
			}
		})
	}
}

func Test_trackedTerraformDirs(t *testing.T) {
	t.Parallel()

	got := trackedTerraformDirs([]string{"modules/a/main.tf", "modules/a/.terraform/evil/main.tf", ".terraform/x", "modules/a/.terraform.lock.hcl", "a/x.terraform/b"})
	require.Len(t, got, 2)
	assert.Equal(t, "modules/a/.terraform/evil/main.tf", got[0].File)
	assert.Equal(t, ".terraform/x", got[1].File)
}

func Test_outsideRoots(t *testing.T) {
	t.Parallel()

	got := outsideRoots([]string{"modules/a/main.tf", "modulesx/b.tf", "docs/c.tf", "README.md", "env/x.tftest.hcl"}, []string{"modules", "env/"})
	require.Len(t, got, 2)
	assert.Equal(t, "modulesx/b.tf", got[0].File)
	assert.Equal(t, "docs/c.tf", got[1].File)
}

func Test_gitLsFiles(t *testing.T) {
	t.Parallel()

	t.Run("追跡しているファイルだけを返す", func(t *testing.T) {
		t.Parallel()
		repo := t.TempDir()
		require.NoError(t, exec.CommandContext(t.Context(), "git", "-C", repo, "init", "-q").Run())
		writeFile(t, filepath.Join(repo, "a b.tf"), "x")
		writeFile(t, filepath.Join(repo, "untracked.tf"), "x")
		require.NoError(t, exec.CommandContext(t.Context(), "git", "-C", repo, "add", "a b.tf").Run())
		got, err := gitLsFiles(repo)
		require.NoError(t, err)
		assert.Equal(t, []string{"a b.tf"}, got)
	})

	t.Run("リポジトリでなければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := gitLsFiles(filepath.Join(t.TempDir(), "none"))
		require.Error(t, err)
	})
}
