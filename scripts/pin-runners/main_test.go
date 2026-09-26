package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPins = `# comment
"ubuntu-latest" = "ubuntu-24.04"
"windows-latest" = "windows-2022"
`

// floatingYAML は、まだ固定されていない workflow。
const floatingYAML = `name: T
jobs:
  one:
    runs-on: ubuntu-latest
`

// pinnedYAML は、既に宣言どおりに固定されている workflow。
const pinnedYAML = `name: T
jobs:
  one:
    runs-on: ubuntu-24.04
`

// fixture は t.TempDir() の下へツリーを建てて root を返します。実物のツリーは読みません
// （ADR-0702 決定17）。
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		full := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	}
	return root
}

// soundRepo は、ずれを1件も持たない最小のリポジトリです。
func soundRepo() map[string]string {
	return map[string]string{
		pinFile:                 testPins,
		workflowDir + "/a.yaml": pinnedYAML,
	}
}

// failWriter は、書き込みが必ず失敗する io.Writer です。
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("書けない") }

func Test_run(t *testing.T) {
	t.Parallel()

	fixedWd := func(root string) func() (string, error) {
		return func() (string, error) { return root, nil }
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("check は固定済みのツリーを通す", func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			require.NoError(t, run([]string{"check"}, fixedWd(fixture(t, soundRepo())), &out))
			assert.Contains(t, out.String(), "workflow 1 件の runs-on が宣言通りに固定されています")
		})

		t.Run("apply は浮動の label を書き換える", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = floatingYAML
			root := fixture(t, files)
			var out strings.Builder
			require.NoError(t, run([]string{"apply"}, fixedWd(root), &out))

			got, err := os.ReadFile(filepath.Join(root, workflowDir, "a.yaml"))
			require.NoError(t, err)
			assert.Equal(t, pinnedYAML, string(got))
			assert.Contains(t, out.String(), "1 ファイルへ反映しました")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string][]string{
			"引数が無ければ usage":     {},
			"引数が2つなら usage":     {"apply", "check"},
			"未知のサブコマンドなら usage": {"resolve"},
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, run(args, fixedWd(fixture(t, soundRepo())), &strings.Builder{}), errUsage)
			})
		}

		t.Run("作業ディレクトリが取れなければエラー", func(t *testing.T) {
			t.Parallel()
			wd := func() (string, error) { return "", errors.New("取れない") }
			require.Error(t, run([]string{"check"}, wd, &strings.Builder{}))
		})
	})
}

func Test_applyOrCheck(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("apply するずれが無ければその旨を報せる", func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			require.NoError(t, applyOrCheck(fixture(t, soundRepo()), false, &out))
			assert.Contains(t, out.String(), "反映するずれはありませんでした")
		})

		t.Run("ブロックスカラーの中の runs-on は書き換えない", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = pinnedYAML + `    steps:
      - run: |
          echo "runs-on: ubuntu-latest"
`
			root := fixture(t, files)
			require.NoError(t, applyOrCheck(root, true, &strings.Builder{}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言が読めなければエラー", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			delete(files, pinFile)
			require.Error(t, applyOrCheck(fixture(t, files), true, &strings.Builder{}))
		})

		t.Run("宣言が0件なら成功で返さず番兵を返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[pinFile] = "# 宣言が1件も無い\n"
			require.ErrorIs(t, applyOrCheck(fixture(t, files), true, &strings.Builder{}), errNoPins)
		})

		t.Run("workflow ディレクトリが無ければエラー", func(t *testing.T) {
			t.Parallel()
			root := fixture(t, map[string]string{pinFile: testPins})
			require.Error(t, applyOrCheck(root, true, &strings.Builder{}))
		})

		t.Run("runs-on が0件なら成功で返さず番兵を返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = "name: T\njobs: {}\n"
			require.ErrorIs(t, applyOrCheck(fixture(t, files), true, &strings.Builder{}), errNoRunsOn)
		})

		t.Run("宣言に無い label があればファイル名を添えて弾く", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = "name: T\njobs:\n  one:\n    runs-on: macos-14\n"
			err := applyOrCheck(fixture(t, files), true, &strings.Builder{})
			require.ErrorIs(t, err, errUnknownRunner)
			assert.Contains(t, err.Error(), "a.yaml")
		})

		t.Run("check はずれを番兵で返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = floatingYAML
			err := applyOrCheck(fixture(t, files), true, &strings.Builder{})
			require.ErrorIs(t, err, errRunnerDrift)
			assert.Contains(t, err.Error(), "a.yaml")
		})

		t.Run("読めない workflow があれば弾く", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "読み取り権限を落としても root は読めてしまう")
			files := soundRepo()
			root := fixture(t, files)
			require.NoError(t, os.Chmod(filepath.Join(root, workflowDir, "a.yaml"), 0o000))
			require.Error(t, applyOrCheck(root, true, &strings.Builder{}))
		})

		t.Run("書けなければエラーを返し、作業ツリーを変えない", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "書き込み権限を落としても root は書けてしまう")
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = floatingYAML
			root := fixture(t, files)
			dir := filepath.Join(root, workflowDir)
			// rename はファイルの権限を見ないので、親ディレクトリを読み取り専用にする。
			require.NoError(t, os.Chmod(dir, 0o500))
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

			require.Error(t, applyOrCheck(root, false, &strings.Builder{}))

			got, err := os.ReadFile(filepath.Join(dir, "a.yaml"))
			require.NoError(t, err)
			assert.Equal(t, floatingYAML, string(got), "失敗した実行が書き換えを残している")
		})

		t.Run("apply の報告が書けなければエラー", func(t *testing.T) {
			t.Parallel()
			require.Error(t, applyOrCheck(fixture(t, soundRepo()), false, failWriter{}))
		})
	})
}

func Test_pinnedSet(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("固定先だけを集める", func(t *testing.T) {
			t.Parallel()
			got := pinnedSet(map[string]string{"ubuntu-latest": "ubuntu-24.04"})
			assert.True(t, got["ubuntu-24.04"])
			assert.False(t, got["ubuntu-latest"])
		})

		t.Run("宣言が空なら空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, pinnedSet(map[string]string{}))
		})
	})
}

func Test_workflowFiles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("yaml と yml を名前順に相対パスで返す", func(t *testing.T) {
			t.Parallel()
			root := fixture(t, map[string]string{
				workflowDir + "/b.yaml":        "",
				workflowDir + "/a.yml":         "",
				workflowDir + "/README.md":     "",
				workflowDir + "/nested/c.yaml": "",
			})
			got, err := workflowFiles(root)
			require.NoError(t, err)
			assert.Equal(t, []string{workflowDir + "/a.yml", workflowDir + "/b.yaml"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, err := workflowFiles(t.TempDir())
			require.Error(t, err)
		})
	})
}

func Test_rewrite(t *testing.T) {
	t.Parallel()

	pins := map[string]string{"ubuntu-latest": "ubuntu-24.04"}
	pinned := pinnedSet(pins)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string]struct {
			source   string
			want     string
			wantSeen int
		}{
			"浮動の label を固定先へ書き換える": {
				source: "    runs-on: ubuntu-latest\n", want: "    runs-on: ubuntu-24.04\n", wantSeen: 1,
			},
			"固定済みはそのまま数える": {
				source: "    runs-on: ubuntu-24.04\n", want: "    runs-on: ubuntu-24.04\n", wantSeen: 1,
			},
			"runs-on が無ければ0件": {
				source: "name: T\n", want: "name: T\n", wantSeen: 0,
			},
			"行末の注記を保ったまま書き換える": {
				source: "    runs-on: ubuntu-latest  # 注記\n", want: "    runs-on: ubuntu-24.04  # 注記\n", wantSeen: 1,
			},
			"ブロックスカラーの中は見ない": {
				source:   "      - run: |\n          runs-on: ubuntu-latest\n",
				want:     "      - run: |\n          runs-on: ubuntu-latest\n",
				wantSeen: 0,
			},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, seen, err := rewrite(tc.source, pins, pinned)
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
				assert.Equal(t, tc.wantSeen, seen)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string]string{
			"宣言にも固定先にも無い label": "    runs-on: macos-14\n",
			"列の形は読めないものとして弾く":   "    runs-on: [self-hosted, linux]\n",
			"式の形は読めないものとして弾く":   "    runs-on: ${{ matrix.os }}\n",
			"値が次の行にある形も弾く":      "    runs-on:\n      group: big\n",
		}
		for name, source := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, _, err := rewrite(source, pins, pinned)
				require.ErrorIs(t, err, errUnknownRunner)
			})
		}
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ずれが無ければ件数を添えて成功する", func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			require.NoError(t, report(&out, nil, 3))
			assert.Contains(t, out.String(), "workflow 3 件")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ずれがあれば番兵と直し方を返す", func(t *testing.T) {
			t.Parallel()
			err := report(&strings.Builder{}, []string{"x.yaml"}, 1)
			require.ErrorIs(t, err, errRunnerDrift)
			assert.Contains(t, err.Error(), "make pin-runners-apply")
		})

		t.Run("書けなければエラー", func(t *testing.T) {
			t.Parallel()
			require.Error(t, report(failWriter{}, nil, 1))
		})
	})
}

func Test_formatApplied(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ずれが無ければその旨を述べる", func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, formatApplied(nil), "反映するずれはありませんでした")
		})

		t.Run("ずれた件数とファイル名を並べる", func(t *testing.T) {
			t.Parallel()
			got := formatApplied([]string{"x.yaml", "y.yaml"})
			assert.Contains(t, got, "2 ファイルへ反映しました")
			assert.Contains(t, got, "x.yaml, y.yaml")
		})
	})
}
