package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPins は健全な宣言です。**孤児を含めません** —— 参照されない対を「健全」の定義へ
// 入れると、孤児を検出しない実装がそのまま正しいものとして固定されます。
const testPins = `# comment
"ubuntu-latest" = "ubuntu-24.04"
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

var (
	// errWD は、作業ディレクトリの取得が失敗したことを表すテスト側のセンチネルです。
	// **require.Error では、run が別の理由で落ちても通ってしまいます。**
	errWD = xerrors.New("getwd failed")
	// errWrite は、出力の書き込みが失敗したことを表すテスト側のセンチネルです。
	errWrite = xerrors.New("write failed")
)

// failWriter は、書き込みが必ず失敗する io.Writer です。
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errWrite }

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
			assert.Contains(t, out.String(), "workflow 1 件の runs-on 1 件が宣言通りに固定されています")
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
			wd := func() (string, error) { return "", errWD }
			require.ErrorIs(t, run([]string{"check"}, wd, &strings.Builder{}), errWD)
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

		t.Run("報告する件数はファイル数と runs-on 件数の両方である", func(t *testing.T) {
			t.Parallel()
			// 1ファイルに runs-on 2件。両者が一致するフィクスチャでは、どちらを数えて
			// 報告しているかを区別できない（ADR-0702 決定15）。
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = "name: T\njobs:\n  one:\n    runs-on: ubuntu-24.04\n  two:\n    runs-on: ubuntu-24.04\n"
			var out strings.Builder
			require.NoError(t, applyOrCheck(fixture(t, files), true, &out))
			assert.Equal(t, "✅ pin-runners: workflow 1 件の runs-on 2 件が宣言通りに固定されています\n", out.String())
		})

		t.Run("ずれたファイルだけを名前順に列挙する", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = floatingYAML
			files[workflowDir+"/b.yaml"] = pinnedYAML
			files[workflowDir+"/c.yaml"] = floatingYAML
			err := applyOrCheck(fixture(t, files), true, &strings.Builder{})
			require.ErrorIs(t, err, errRunnerDrift)
			assert.Contains(t, err.Error(), "a.yaml, "+workflowDir+"/c.yaml")
			assert.NotContains(t, err.Error(), "b.yaml")
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
			require.ErrorIs(t, applyOrCheck(fixture(t, files), true, &strings.Builder{}), os.ErrNotExist)
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
			require.ErrorIs(t, applyOrCheck(root, true, &strings.Builder{}), os.ErrNotExist)
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

		declCases := map[string]string{
			"固定先がキーと同じ宣言を弾く":          "\"ubuntu-latest\" = \"ubuntu-latest\"\n",
			"固定先が別のキーの宣言を弾く":          "\"ubuntu-latest\" = \"ubuntu-24.04\"\n\"ubuntu-24.04\" = \"ubuntu-22.04\"\n",
			"固定先が別の浮動 label である宣言を弾く": "\"ubuntu-latest\" = \"ubuntu-24.04\"\n\"macos-latest\" = \"ubuntu-latest\"\n",
		}
		for name, pins := range declCases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				files := soundRepo()
				files[pinFile] = pins
				require.ErrorIs(t, applyOrCheck(fixture(t, files), true, &strings.Builder{}), errPinNotTerminal)
			})
		}

		t.Run("どの runs-on にも当たらない宣言を弾く", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[pinFile] = testPins + "\"macos-latest\" = \"macos-14\"\n"
			err := applyOrCheck(fixture(t, files), true, &strings.Builder{})
			require.ErrorIs(t, err, errPinOrphan)
			assert.Contains(t, err.Error(), "macos-latest")
		})

		t.Run("workflow が1件も無ければ番兵を返す", func(t *testing.T) {
			t.Parallel()
			root := fixture(t, map[string]string{pinFile: testPins, workflowDir + "/README.md": ""})
			require.ErrorIs(t, applyOrCheck(root, true, &strings.Builder{}), errNoRunsOn)
		})

		t.Run("後のファイルに読めない label があれば、先のファイルも書き換えない", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = floatingYAML
			files[workflowDir+"/b.yaml"] = "name: T\njobs:\n  one:\n    runs-on: macos-14\n"
			root := fixture(t, files)
			require.ErrorIs(t, applyOrCheck(root, false, &strings.Builder{}), errUnknownRunner)

			got, err := os.ReadFile(filepath.Join(root, workflowDir, "a.yaml"))
			require.NoError(t, err)
			assert.Equal(t, floatingYAML, string(got), "失敗した実行が書き換えを残している")
		})

		t.Run("check はずれを番兵で返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[workflowDir+"/a.yaml"] = floatingYAML
			root := fixture(t, files)
			err := applyOrCheck(root, true, &strings.Builder{})
			require.ErrorIs(t, err, errRunnerDrift)
			assert.Contains(t, err.Error(), "a.yaml")

			got, readErr := os.ReadFile(filepath.Join(root, workflowDir, "a.yaml"))
			require.NoError(t, readErr)
			assert.Equal(t, floatingYAML, string(got), "check が作業ツリーを書き換えている")
		})

		t.Run("読めない workflow があれば弾く", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "読み取り権限を落としても root は読めてしまう")
			files := soundRepo()
			root := fixture(t, files)
			require.NoError(t, os.Chmod(filepath.Join(root, workflowDir, "a.yaml"), 0o000))
			err := applyOrCheck(root, true, &strings.Builder{})
			require.ErrorIs(t, err, os.ErrPermission)
			assert.Contains(t, err.Error(), "a.yaml")
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

			require.ErrorIs(t, applyOrCheck(root, false, &strings.Builder{}), os.ErrPermission)

			got, err := os.ReadFile(filepath.Join(dir, "a.yaml"))
			require.NoError(t, err)
			assert.Equal(t, floatingYAML, string(got), "失敗した実行が書き換えを残している")
		})

		t.Run("apply の報告が書けなければエラー", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, applyOrCheck(fixture(t, soundRepo()), false, failWriter{}), errWrite)
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
				workflowDir + "/X.YAML":        "",
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
			require.ErrorIs(t, err, os.ErrNotExist)
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
			"ブロックスカラーを抜けた後の行は再び走査対象に戻る": {
				source:   "      - run: |\n          echo\n    runs-on: ubuntu-latest\n",
				want:     "      - run: |\n          echo\n    runs-on: ubuntu-24.04\n",
				wantSeen: 1,
			},
			"末尾が | のコメント行をヘッダと見なさない": {
				source:   "  # see also: |\n    runs-on: ubuntu-latest\n",
				want:     "  # see also: |\n    runs-on: ubuntu-24.04\n",
				wantSeen: 1,
			},
			"行末の注記の中の | もヘッダと見なさない": {
				source:   "    name: x # todo: |\n    runs-on: ubuntu-latest\n",
				want:     "    name: x # todo: |\n    runs-on: ubuntu-24.04\n",
				wantSeen: 1,
			},
			"複数の runs-on を全部数え、固定済みと浮動を混ぜて扱う": {
				source:   "    runs-on: ubuntu-24.04\n    runs-on: ubuntu-latest\n",
				want:     "    runs-on: ubuntu-24.04\n    runs-on: ubuntu-24.04\n",
				wantSeen: 2,
			},
		}
		for name, tc := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, labels, err := rewrite(tc.source, pins, pinned)
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
				assert.Len(t, labels, tc.wantSeen)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string]string{
			"宣言にも固定先にも無い label":          "    runs-on: macos-14\n",
			"列の形は読めないものとして弾く":            "    runs-on: [self-hosted, linux]\n",
			"式の形は読めないものとして弾く":            "    runs-on: ${{ matrix.os }}\n",
			"値が次の行にある形も弾く":               "    runs-on:\n      group: big\n",
			"コロンの前に空白があるキーも読めないものとして弾く":  "    runs-on : ubuntu-latest\n",
			"二重引用符のキーも読めないものとして弾く":       "    \"runs-on\": ubuntu-latest\n",
			"一重引用符のキーも読めないものとして弾く":       "    'runs-on': ubuntu-latest\n",
			"フロー写像の中のキーも読めないものとして弾く":     "  one: {runs-on: ubuntu-latest}\n",
			"空白を挟まない # は label の一部として弾く": "    runs-on: ubuntu-24.04#x\n",
			"CRLF の行は読めないものとして弾く":        "    runs-on: ubuntu-24.04\r\n",
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
			require.NoError(t, report(&out, nil, 3, 5))
			assert.Equal(t, "✅ pin-runners: workflow 3 件の runs-on 5 件が宣言通りに固定されています\n", out.String())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ずれがあれば番兵と直し方を返す", func(t *testing.T) {
			t.Parallel()
			err := report(&strings.Builder{}, []string{"x.yaml"}, 1, 1)
			require.ErrorIs(t, err, errRunnerDrift)
			assert.Contains(t, err.Error(), "make pin-runners-apply")
		})

		t.Run("書けなければエラー", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, report(failWriter{}, nil, 1, 1), errWrite)
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

func Test_checkTerminal(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("固定先がどのキーでもなければ通す", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, checkTerminal(map[string]string{"ubuntu-latest": "ubuntu-24.04"}))
		})

		t.Run("宣言が空なら通す", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, checkTerminal(map[string]string{}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string]map[string]string{
			"自己写像を弾く":      {"ubuntu-latest": "ubuntu-latest"},
			"連鎖を弾く":        {"ubuntu-latest": "ubuntu-24.04", "ubuntu-24.04": "ubuntu-22.04"},
			"固定先が別のキーなら弾く": {"ubuntu-latest": "ubuntu-24.04", "macos-latest": "ubuntu-latest"},
		}
		for name, pins := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, checkTerminal(pins), errPinNotTerminal)
			})
		}
	})
}

func Test_checkOrphan(t *testing.T) {
	t.Parallel()

	pins := map[string]string{"ubuntu-latest": "ubuntu-24.04"}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("キーとして当たっていれば通す", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, checkOrphan(pins, map[string]bool{"ubuntu-latest": true}))
		})

		t.Run("固定先として当たっていれば通す", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, checkOrphan(pins, map[string]bool{"ubuntu-24.04": true}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("どちらでも当たらなければ弾き、どの対かを述べる", func(t *testing.T) {
			t.Parallel()
			err := checkOrphan(pins, map[string]bool{"macos-14": true})
			require.ErrorIs(t, err, errPinOrphan)
			assert.Contains(t, err.Error(), "ubuntu-latest")
		})
	})
}

func Test_sortedKeys(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("キーを昇順で返す", func(t *testing.T) {
			t.Parallel()
			got := sortedKeys(map[string]string{"b": "1", "a": "2", "c": "3"})
			assert.Equal(t, []string{"a", "b", "c"}, got)
		})

		t.Run("空なら空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, sortedKeys(map[string]string{}))
		})
	})
}
