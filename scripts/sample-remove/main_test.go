package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/markers"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const fixtureLock = `# header
"actions/checkout@v7.0.1" = "aaaa"
"acme/scan@v1" = "bbbb"
"acme/shared@v2" = "cccc"
`

// fixtureDomains は、テスト用の manifest。scan は自分の workflow でだけ使う pin を、
// shared は残る workflow も参照する pin を持つ。
var fixtureDomains = []domain{
	{name: "scan", paths: []string{".github/workflows/scan.yaml", "scan.properties"}, pins: []string{"acme/scan@v1", "acme/shared@v2"}},
	{name: "tool", paths: []string{"tool"}},
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(c), 0o644))
	}
}

func fixtureTree(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		pinFile:                       fixtureLock,
		".github/workflows/scan.yaml": "steps:\n  - uses: acme/scan@bbbb # v1\n  - uses: acme/shared@cccc # v2\n",
		".github/workflows/keep.yaml": "steps:\n  - uses: acme/shared@cccc # v2\n  - uses: actions/checkout@aaaa # v7.0.1\n",
		"scan.properties":             "k=v\n",
		"tool/main.go":                "// sample:bogus は消える側なので走査しない\n",
		"README.md":                   "a\n<!-- sample:begin -->\nscan の説明\n<!-- sample:end -->\nb\n",
		".github/egress.toml":         "[x]\n# sample:begin\n[job.\"scan.yaml:scan\"]\n# sample:end\n",
	}
	writeTree(t, root, files)
	list := make([]string, 0, len(files))
	for p := range files {
		list = append(list, p)
	}
	sort.Strings(list)
	return root, list
}

func Test_gitLsFiles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("追跡されているファイルだけを返す", func(t *testing.T) {
			t.Parallel()
			_, err := exec.LookPath("git")
			require.NoError(t, err, "git が要ります")
			root := t.TempDir()
			writeTree(t, root, map[string]string{"a.txt": "a", "dir/b.txt": "b", "untracked.txt": "u"})
			for _, args := range [][]string{{"init", "-q"}, {"add", "a.txt", "dir/b.txt"}} {
				require.NoError(t, exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).Run())
			}
			got, err := gitLsFiles(root)
			require.NoError(t, err)
			assert.Equal(t, []string{"a.txt", "dir/b.txt"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリでなければエラーにする", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			_, err := gitLsFiles(root)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "git ls-files")
		})
	})
}

func Test_run(t *testing.T) {
	t.Parallel()
	noList := func(string) ([]string, error) { return nil, nil }

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("check は件数を書き、何も書き換えない", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			var w bytes.Buffer
			require.NoError(t, run([]string{"check"}, root, fixtureDomains, func(string) ([]string, error) { return list, nil }, &w))
			assert.Equal(t, "✅ sample-remove check: 印 4 個（2 ファイル）、manifest のパス 3 件、pin 2 件を確認しました\n", w.String())
			assert.FileExists(t, filepath.Join(root, "scan.properties"))
		})

		t.Run("apply --dry-run は計画を書き、何も書き換えない", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			var w bytes.Buffer
			require.NoError(t, run([]string{"apply", "--dry-run"}, root, fixtureDomains, func(string) ([]string, error) { return list, nil }, &w))
			assert.Contains(t, w.String(), "消すパス（3 件）:")
			assert.True(t, strings.HasSuffix(w.String(), "（--dry-run のため何も書き換えていません）\n"))
			b, err := os.ReadFile(filepath.Join(root, "README.md"))
			require.NoError(t, err)
			assert.Contains(t, string(b), "sample:begin")
			assert.FileExists(t, filepath.Join(root, "scan.properties"))
		})

		t.Run("apply は計画を書いてから反映する", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			var w bytes.Buffer
			require.NoError(t, run([]string{"apply"}, root, fixtureDomains, func(string) ([]string, error) { return list, nil }, &w))
			assert.Contains(t, w.String(), "外す pin（1 件）:")
			assert.NoFileExists(t, filepath.Join(root, "scan.properties"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, args := range [][]string{nil, {"bogus"}, {"check", "x"}, {"apply", "--force"}, {"apply", "--dry-run", "x"}} {
			t.Run("引数 "+strings.Join(args, " ")+" は使い方のエラーにする", func(t *testing.T) {
				t.Parallel()
				err := run(args, t.TempDir(), fixtureDomains, noList, &bytes.Buffer{})
				require.ErrorIs(t, err, errUsage)
			})
		}

		t.Run("apply は manifest のパスが無ければ何も書かずに落ちる", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{"README.md": "<!-- sample:line -->\n"})
			err := run([]string{"apply"}, root, fixtureDomains, func(string) ([]string, error) { return []string{"README.md"}, nil }, &bytes.Buffer{})
			require.ErrorIs(t, err, errMissingPath)
			b, err := os.ReadFile(filepath.Join(root, "README.md"))
			require.NoError(t, err)
			assert.Equal(t, "<!-- sample:line -->\n", string(b))
		})
	})
}

func Test_countPins(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("domain をまたいで pin を数える", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, 2, countPins(fixtureDomains))
		})
	})
}

func Test_build(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("消すパス・印・外す pin を決め、何も書き換えない", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p, err := build(root, fixtureDomains, func(string) ([]string, error) { return list, nil })
			require.NoError(t, err)
			assert.Equal(t, []string{".github/workflows/scan.yaml", "scan.properties", "tool"}, p.deletes)
			assert.Equal(t, 4, p.markerCount)
			assert.ElementsMatch(t, []string{"README.md", ".github/egress.toml"}, p.markedFiles)
			assert.Equal(t, "a\nb\n", p.writes["README.md"])
			assert.Equal(t, []string{"acme/scan@v1"}, p.droppedPins)
			b, err := os.ReadFile(filepath.Join(root, "README.md"))
			require.NoError(t, err)
			assert.Contains(t, string(b), "sample:begin")
		})

		// tool/main.go は解釈できない印を持つ。消すパスの中まで走査すると build が落ちる。
		t.Run("消すパスの中の印は解釈しない", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p, err := build(root, fixtureDomains, func(string) ([]string, error) { return list, nil })
			require.NoError(t, err)
			assert.NotContains(t, p.markedFiles, "tool/main.go")
		})

		t.Run("消すパスが0件でも印があれば通す", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{"a.md": "<!-- sample:line -->\n", pinFile: fixtureLock})
			p, err := build(root, nil, func(string) ([]string, error) { return []string{"a.md"}, nil })
			require.NoError(t, err)
			assert.Equal(t, 1, p.markerCount)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("manifest のパスが無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			require.NoError(t, os.Remove(filepath.Join(root, "scan.properties")))
			_, err := build(root, fixtureDomains, func(string) ([]string, error) { return list, nil })
			require.ErrorIs(t, err, errMissingPath)
			assert.Contains(t, err.Error(), "scan.properties")
		})

		t.Run("追跡ファイルが0件ならエラーにする", func(t *testing.T) {
			t.Parallel()
			root, _ := fixtureTree(t)
			_, err := build(root, fixtureDomains, func(string) ([]string, error) { return nil, nil })
			require.ErrorIs(t, err, errNoFiles)
		})

		t.Run("ファイルの一覧を取れなければエラーを返す", func(t *testing.T) {
			t.Parallel()
			root, _ := fixtureTree(t)
			sentinel := xerrors.New("ls")
			_, err := build(root, fixtureDomains, func(string) ([]string, error) { return nil, sentinel })
			require.ErrorIs(t, err, sentinel)
		})

		// manifest のパスだけで通すと、印の解釈が壊れて何も拾わなくなっても緑になる。
		t.Run("消すパスがあっても印が0件ならエラーにする", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{"a.md": "a\n", "gone.txt": "x", pinFile: fixtureLock})
			_, err := build(root, []domain{{name: "x", paths: []string{"gone.txt"}}}, func(string) ([]string, error) { return []string{"a.md", "gone.txt"}, nil })
			require.ErrorIs(t, err, errNoTargets)
		})

		t.Run("印も消すパスも無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{"a.md": "a\n", pinFile: fixtureLock})
			_, err := build(root, nil, func(string) ([]string, error) { return []string{"a.md"}, nil })
			require.ErrorIs(t, err, errNoTargets)
		})

		t.Run("解釈できない印があればファイル名を添えてエラーにする", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			writeTree(t, root, map[string]string{"README.md": "<!-- sample:begin -->\n"})
			_, err := build(root, fixtureDomains, func(string) ([]string, error) { return list, nil })
			require.ErrorIs(t, err, errBadMarker)
			require.ErrorIs(t, err, markers.ErrUnbalanced)
			assert.Contains(t, err.Error(), "README.md")
		})

		t.Run("綴りを誤った印はエラーにする", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			writeTree(t, root, map[string]string{"README.md": "<!-- sample:lin -->\n"})
			_, err := build(root, fixtureDomains, func(string) ([]string, error) { return list, nil })
			require.ErrorIs(t, err, markers.ErrUnknownMarker)
		})

		t.Run("pin の不備を build のエラーとして返す", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			ds := append([]domain{{name: "x", pins: []string{"acme/none@v9"}}}, fixtureDomains...)
			_, err := build(root, ds, func(string) ([]string, error) { return list, nil })
			require.ErrorIs(t, err, errMissingPin)
		})

		t.Run("context の不備を build のエラーとして返す", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			writeTree(t, root, map[string]string{protectionFile: fixtureProtection})
			ds := append([]domain{{name: "x", contexts: []string{"none"}}}, fixtureDomains...)
			_, err := build(root, ds, func(string) ([]string, error) { return list, nil })
			require.ErrorIs(t, err, errMissingContext)
		})

		t.Run("追跡ファイルを読めなければエラーにする", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			_, err := build(root, fixtureDomains, func(string) ([]string, error) { return append(list, "gone.md"), nil })
			require.ErrorIs(t, err, errRead)
			assert.Contains(t, err.Error(), "gone.md")
		})
	})
}

func Test_prunePins(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("残る workflow が参照しない pin だけを外す", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p := plan{writes: map[string]string{}, deletes: []string{".github/workflows/scan.yaml"}}
			require.NoError(t, prunePins(root, fixtureDomains, list, &p))
			assert.Equal(t, []string{"acme/scan@v1"}, p.droppedPins)
			assert.Equal(t, "# header\n\"actions/checkout@v7.0.1\" = \"aaaa\"\n\"acme/shared@v2\" = \"cccc\"\n", p.writes[pinFile])
		})

		t.Run("外す pin が無ければ lockfile を書き換えない", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p := plan{writes: map[string]string{}}
			require.NoError(t, prunePins(root, fixtureDomains, list, &p))
			assert.Empty(t, p.droppedPins)
			assert.NotContains(t, p.writes, pinFile)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("manifest の pin が lockfile に無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			ds := []domain{{name: "x", pins: []string{"acme/none@v9"}}}
			err := prunePins(root, ds, list, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errMissingPin)
			assert.Contains(t, err.Error(), "acme/none@v9")
		})

		t.Run("lockfile を読めなければエラーにする", func(t *testing.T) {
			t.Parallel()
			err := prunePins(t.TempDir(), fixtureDomains, nil, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errRead)
		})
	})
}

func Test_pinReferenced(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("残る workflow の uses: が参照していれば true を返す", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p := plan{writes: map[string]string{}, deletes: []string{".github/workflows/scan.yaml"}}
			got, err := pinReferenced(root, "acme/shared@v2", list, &p)
			require.NoError(t, err)
			assert.True(t, got)
		})

		t.Run("消す workflow だけが参照していれば false を返す", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p := plan{writes: map[string]string{}, deletes: []string{".github/workflows/scan.yaml"}}
			got, err := pinReferenced(root, "acme/scan@v1", list, &p)
			require.NoError(t, err)
			assert.False(t, got)
		})

		// 印で uses: の行だけを取り除いた workflow は、書き換え後の内容で見なければならない。
		t.Run("書き換え後の内容で参照を判定する", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p := plan{writes: map[string]string{".github/workflows/keep.yaml": "steps: []\n"}, deletes: []string{".github/workflows/scan.yaml"}}
			got, err := pinReferenced(root, "acme/shared@v2", list, &p)
			require.NoError(t, err)
			assert.False(t, got)
		})

		t.Run("action の下の path を指す参照も数える", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{".github/workflows/w.yaml": "    - uses: acme/scan/sub@bbbb\n"})
			got, err := pinReferenced(root, "acme/scan@v1", []string{".github/workflows/w.yaml"}, &plan{writes: map[string]string{}})
			require.NoError(t, err)
			assert.True(t, got)
		})

		t.Run("composite action の参照も数える", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{".github/actions/x/action.yaml": "    - uses: \"acme/scan@bbbb\"\n"})
			got, err := pinReferenced(root, "acme/scan@v1", []string{".github/actions/x/action.yaml"}, &plan{writes: map[string]string{}})
			require.NoError(t, err)
			assert.True(t, got)
		})

		t.Run("workflow 以外の言及は参照として数えない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{"README.md": "uses: acme/scan@v1\n"})
			got, err := pinReferenced(root, "acme/scan@v1", []string{"README.md"}, &plan{writes: map[string]string{}})
			require.NoError(t, err)
			assert.False(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("@ の無いキーはエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := pinReferenced(t.TempDir(), "acme/scan", nil, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errMissingPin)
		})

		t.Run("workflow を読めなければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := pinReferenced(t.TempDir(), "acme/scan@v1", []string{".github/workflows/gone.yaml"}, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errRead)
		})
	})
}

// editorconfig-checker-disable

const fixtureProtection = `{
  "required_status_checks": [
    {
      "context": "a"
    },
    {
      "context": "scan"
    },
    {
      "context": "z"
    }
  ]
}
`

// editorconfig-checker-enable

func Test_dropContexts(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("途中の context を3行ごと外す", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{protectionFile: fixtureProtection})
			p := plan{writes: map[string]string{}}
			require.NoError(t, dropContexts(root, []domain{{name: "x", contexts: []string{"scan"}}}, &p))
			assert.Equal(t, []string{"scan"}, p.droppedContexts)
			assert.Equal(t, "{\n  \"required_status_checks\": [\n    {\n      \"context\": \"a\"\n    },\n    {\n      \"context\": \"z\"\n    }\n  ]\n}\n", p.writes[protectionFile])
		})

		// 末尾の要素を外したときに1つ前の `},` を残すと、JSON として読めなくなる。
		t.Run("末尾の context を外すと1つ前の要素の , を外す", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{protectionFile: fixtureProtection})
			p := plan{writes: map[string]string{}}
			require.NoError(t, dropContexts(root, []domain{{name: "x", contexts: []string{"z"}}}, &p))
			var v map[string]any
			require.NoError(t, json.Unmarshal([]byte(p.writes[protectionFile]), &v))
			assert.Len(t, v["required_status_checks"], 2)
		})

		t.Run("context が無ければ宣言を読まない", func(t *testing.T) {
			t.Parallel()
			p := plan{writes: map[string]string{}}
			require.NoError(t, dropContexts(t.TempDir(), fixtureDomains, &p))
			assert.Empty(t, p.writes)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言に無い context はエラーにする", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{protectionFile: fixtureProtection})
			err := dropContexts(root, []domain{{name: "x", contexts: []string{"none"}}}, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errMissingContext)
		})

		t.Run("宣言に2回ある context はエラーにする", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{protectionFile: strings.Replace(fixtureProtection, `"context": "a"`, `"context": "scan"`, 1)})
			err := dropContexts(root, []domain{{name: "x", contexts: []string{"scan"}}}, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errMissingContext)
		})

		t.Run("3行の形でない context はエラーにする", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeTree(t, root, map[string]string{protectionFile: `[{"context": "scan"}]`})
			err := dropContexts(root, []domain{{name: "x", contexts: []string{"scan"}}}, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errMissingContext)
		})

		t.Run("宣言を読めなければエラーにする", func(t *testing.T) {
			t.Parallel()
			err := dropContexts(t.TempDir(), []domain{{name: "x", contexts: []string{"scan"}}}, &plan{writes: map[string]string{}})
			require.ErrorIs(t, err, errRead)
		})
	})
}

func Test_under(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			path string
			want bool
		}{
			{name: "同じパスは当たる", path: "tool", want: true},
			{name: "配下のパスは当たる", path: "tool/main.go", want: true},
			{name: "名前が前方一致するだけのパスは当たらない", path: "toolbox/main.go", want: false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, under(tt.path, []string{"tool"}))
			})
		}
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("消すパス・印のファイル・外す pin を件数とともに書く", func(t *testing.T) {
			t.Parallel()
			var w bytes.Buffer
			report(&w, plan{deletes: []string{"a"}, markedFiles: []string{"z.md", "b.md"}, markerCount: 3, droppedPins: []string{"acme/scan@v1"}, droppedContexts: []string{"scan"}})
			assert.Equal(t, "消すパス（1 件）:\n  - a\n印を取り除くファイル（2 件、印 3 個）:\n  - b.md\n  - z.md\n外す pin（1 件）:\n  - acme/scan@v1\n必須検査から外す context（1 件）:\n  - scan\n", w.String())
		})

		t.Run("空の plan でも各見出しを0件で書く", func(t *testing.T) {
			t.Parallel()
			var w bytes.Buffer
			report(&w, plan{})
			assert.Equal(t, "消すパス（0 件）:\n印を取り除くファイル（0 件、印 0 個）:\n外す pin（0 件）:\n必須検査から外す context（0 件）:\n", w.String())
		})
	})
}

func Test_apply(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("書き換えてからパスを消す", func(t *testing.T) {
			t.Parallel()
			root, list := fixtureTree(t)
			p, err := build(root, fixtureDomains, func(string) ([]string, error) { return list, nil })
			require.NoError(t, err)
			require.NoError(t, apply(root, p))
			b, err := os.ReadFile(filepath.Join(root, "README.md"))
			require.NoError(t, err)
			assert.Equal(t, "a\nb\n", string(b))
			for _, gone := range []string{"tool", "scan.properties", ".github/workflows/scan.yaml"} {
				assert.NoFileExists(t, filepath.Join(root, gone))
			}
			lock, err := os.ReadFile(filepath.Join(root, pinFile))
			require.NoError(t, err)
			assert.NotContains(t, string(lock), "acme/scan@v1")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("書き換えに失敗したらパスを消さない", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "読み取り専用のディレクトリへの書き込みを失敗させる")
			root, list := fixtureTree(t)
			p, err := build(root, fixtureDomains, func(string) ([]string, error) { return list, nil })
			require.NoError(t, err)
			dir := filepath.Join(root, ".github")
			require.NoError(t, os.Chmod(dir, 0o555))
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			require.Error(t, apply(root, p))
			assert.FileExists(t, filepath.Join(root, "scan.properties"))
			assert.FileExists(t, filepath.Join(root, ".github/workflows/scan.yaml"))
			assert.DirExists(t, filepath.Join(root, "tool"))
			for path, want := range map[string]string{
				"README.md":           "a\n<!-- sample:begin -->\nscan の説明\n<!-- sample:end -->\nb\n",
				pinFile:               fixtureLock,
				".github/egress.toml": "[x]\n# sample:begin\n[job.\"scan.yaml:scan\"]\n# sample:end\n",
			} {
				b, err := os.ReadFile(filepath.Join(root, path))
				require.NoError(t, err)
				assert.Equal(t, want, string(b), path)
			}
		})
	})
}
