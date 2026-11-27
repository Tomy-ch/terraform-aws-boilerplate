package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.RunGitIsolated(m))
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}
	return root
}

// errListFixture は、木のファイルの一覧を得られないことを模すエラー。
var errListFixture = xerrors.New("一覧を得られません")

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ YAML / シェルの字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const rulesetOneContext = `{
  "rules": [
    {"type": "required_status_checks",
     "parameters": {"required_status_checks": [{"context": "build"}]}}
  ]
}`

// validTree は、すべての不変条件を満たす最小の木。ケースは、ここから1か所だけを差し替える。
func validTree() map[string]string {
	return map[string]string{
		"rulesets/common.json":       `{"rules": [{"type": "deletion"}]}`,
		"rulesets/lines.json":        `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "gate"}]}}]}`,
		"rulesets/deploy-admin.json": `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "deploy-source-check"}, {"context": "line-gate"}]}}]}`,
		"checks.json": `{"checks": [
  {"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true},
  {"name": "b", "workflow": "b.yaml", "paths": ["x/**"], "scope": "change", "required": true}
]}`,
		"wf/pr-gate.yaml": `on:
  pull_request:
    types: [opened, synchronize, reopened, edited]
    branches: # branch-set: gate
      - 'main'
jobs:
  changes:
    runs-on: x
    steps:
      - run: make ci-changes RUNNER_MODE=host
  call-a:
    needs: changes
    if: ${{ !cancelled() && (needs.changes.result != 'success' || contains(fromJSON(needs.changes.outputs.run), 'a')) }}
    uses: ./.github/workflows/a.yaml
  call-b:
    needs: changes
    if: ${{ !cancelled() && (needs.changes.result != 'success' || contains(fromJSON(needs.changes.outputs.run), 'b')) }}
    uses: ./.github/workflows/b.yaml
  gate:
    needs: [changes, call-a, call-b]
    if: ${{ always() }}
    runs-on: x
    steps:
      - env:
          NEEDS: ${{ toJSON(needs) }}
        run: make ci-gate RUNNER_MODE=host
`,
		"wf/line-verify.yaml": `on:
  push:
    branches: # branch-set: lines
      - 'main'
jobs:
  call-a:
    uses: ./.github/workflows/a.yaml
  line-gate:
    needs: [call-a]
    if: always()
    runs-on: x
    steps:
      - env:
          NEEDS: ${{ toJSON(needs) }}
        run: make ci-line-gate RUNNER_MODE=host
`,
		"wf/deploy-gate.yaml": `on:
  pull_request:
    branches: # branch-set: deploy-env
      - 'admin'
jobs:
  deploy-source-check:
    runs-on: x
`,
		"wf/a.yaml": "on:\n  workflow_call:\njobs:\n  a:\n    runs-on: x\n",
		"wf/b.yaml": "on:\n  workflow_call:\njobs:\n  b:\n    runs-on: x\n",
	}
}

// editorconfig-checker-enable

// treeWith は validTree の写しに files を重ねる。値が空のファイルは消す。
func treeWith(files map[string]string) map[string]string {
	tree := validTree()
	for k, v := range files {
		if v == "" {
			delete(tree, k)
			continue
		}
		tree[k] = v
	}
	return tree
}

// treeFiles は、validTree の検査の paths に当たるリポジトリの木のファイルの一覧を返す。
func treeFiles() ([]string, error) { return []string{"x/a.go", "y.md"}, nil }

func runArgs(root string) []string {
	return []string{
		"-rulesets", filepath.Join(root, "rulesets"),
		"-legacy-ruleset", filepath.Join(root, "branch-protection.json"),
		"-checks", filepath.Join(root, "checks.json"),
		"-workflows", filepath.Join(root, "wf"),
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言と workflow が一致すれば件数を出して通る", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, validTree())
			var out bytes.Buffer
			require.NoError(t, run(runArgs(root), treeFiles, &out))
			assert.Contains(t, out.String(), "ruleset の required context 3 件、古い宣言の required context 0 件、検査 2 件と workflow 5 件")
		})

		t.Run("古い宣言が在る間は、呼ばれる workflow の pull_request と if: を許し、古い context の報告 job を検査する", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, treeWith(map[string]string{
				"branch-protection.json": `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "a"}]}}]}`,
				"wf/a.yaml":              "on:\n  pull_request:\n  workflow_call:\njobs:\n  a:\n    if: github.event_name == 'pull_request'\n    runs-on: x\n",
			}))
			var out bytes.Buffer
			require.NoError(t, run(runArgs(root), treeFiles, &out))
			assert.Contains(t, out.String(), "古い宣言の required context 1 件")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("違反は件数とともに、ファイルの順に並べて落ちる", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, treeWith(map[string]string{
				"wf/b.yaml": "on:\n  pull_request:\n  workflow_call:\njobs:\n  b:\n    runs-on: x\n",
				"wf/a.yaml": "on:\n  pull_request:\n  workflow_call:\njobs:\n  a:\n    runs-on: x\n",
			}))
			var out bytes.Buffer
			err := run(runArgs(root), treeFiles, &out)
			require.Error(t, err)
			assert.Contains(t, out.String(), "2 件の不整合")
			assert.Less(t, strings.Index(out.String(), "a.yaml"), strings.Index(out.String(), "b.yaml"))
		})

		t.Run("古い宣言の context の pull_request にフィルタが残っていれば落ちる", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, treeWith(map[string]string{
				"branch-protection.json": rulesetOneContext,
				"wf/ci.yaml":             "on:\n  pull_request:\n    paths:\n      - 'src/**'\n\njobs:\n  build:\n    runs-on: x\n",
			}))
			var out bytes.Buffer
			require.Error(t, run(runArgs(root), treeFiles, &out))
			assert.Contains(t, out.String(), "`paths` が残っています")
		})

		t.Run("pr-gate.yaml が無ければ落ちる", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, treeWith(map[string]string{"wf/pr-gate.yaml": ""}))
			var out bytes.Buffer
			require.Error(t, run(runArgs(root), treeFiles, &out))
			assert.Contains(t, out.String(), "pr-gate.yaml がありません")
		})

		t.Run("線の ruleset の違反は、-rulesets のディレクトリの lines.json へ報告する", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, treeWith(map[string]string{
				"rulesets/lines.json": `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "gate"}, {"context": "build"}]}}]}`,
				"wf/ci.yaml":          "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n",
			}))
			var out bytes.Buffer
			require.Error(t, run(runArgs(root), treeFiles, &out))
			assert.Contains(t, out.String(), "1 件の不整合")
			assert.Contains(t, out.String(), "  "+filepath.Join(root, "rulesets", "lines.json")+"\n")
			assert.Contains(t, out.String(), "（実際: [gate, build]）")
		})

		t.Run("検査の paths の pattern が木のどのファイルにも当たらなければ落ちる", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, validTree())
			var out bytes.Buffer
			require.Error(t, run(runArgs(root), func() ([]string, error) { return []string{"y.md"}, nil }, &out))
			assert.Contains(t, out.String(), "1 件の不整合")
			assert.Contains(t, out.String(), "検査 `b` の paths の `x/**` が、リポジトリの木のどのファイルにも当たりません")
		})

		t.Run("木のファイルの一覧を得られなければ落ちる", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, validTree())
			require.ErrorIs(t, run(runArgs(root), func() ([]string, error) { return nil, errListFixture }, &bytes.Buffer{}), errListFixture)
		})

		t.Run("解釈できない workflow は違反として報告する", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, treeWith(map[string]string{"wf/broken.yaml": "on: [\n"}))
			var out bytes.Buffer
			require.Error(t, run(runArgs(root), treeFiles, &out))
			assert.Contains(t, out.String(), "workflow を解釈できません")
		})
	})

	t.Run("退化した入力", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			files map[string]string
			want  error
		}{
			"rulesets/ が無ければ読めない":       {map[string]string{"rulesets/common.json": "", "rulesets/lines.json": "", "rulesets/deploy-admin.json": ""}, requiredchecks.ErrRead},
			"rulesets/ が空なら0件":          {map[string]string{"rulesets/lines.json": "", "rulesets/deploy-admin.json": "", "rulesets/README.md": "x", "rulesets/common.json": ""}, requiredchecks.ErrNone},
			"checks.json が無ければ読めない":     {map[string]string{"checks.json": ""}, checksdecl.ErrDecl},
			"checks.json を解釈できなければ読めない": {map[string]string{"checks.json": "nope"}, checksdecl.ErrDecl},
			"検査が0件なら0件":                 {map[string]string{"checks.json": `{"checks": []}`}, checksdecl.ErrNoChecks},
			"線の検証の検査が0件なら0件":            {map[string]string{"checks.json": `{"checks": [{"name": "b", "workflow": "b.yaml", "paths": [], "scope": "change", "required": true}]}`}, checksdecl.ErrNoChecks},
			"古い宣言が在るのに0件なら0件":           {map[string]string{"branch-protection.json": `{"rules": [{"type": "deletion"}]}`}, requiredchecks.ErrNone},
			"古い宣言を解釈できなければ読めない":         {map[string]string{"branch-protection.json": "nope"}, requiredchecks.ErrRead},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				root := writeFixture(t, treeWith(c.files))
				require.ErrorIs(t, run(runArgs(root), treeFiles, &bytes.Buffer{}), c.want)
			})
		}

		t.Run("木のファイルが0件なら0件", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, validTree())
			require.ErrorIs(t, run(runArgs(root), func() ([]string, error) { return nil, nil }, &bytes.Buffer{}), errNoFiles)
		})

		t.Run("workflow のディレクトリが無ければ落ちる", func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, validTree())
			args := runArgs(root)
			args[len(args)-1] = filepath.Join(root, "missing")
			require.Error(t, run(args, treeFiles, &bytes.Buffer{}))
		})
	})
}

func Test_gitLsFiles(t *testing.T) {
	t.Parallel()

	t.Run("追跡されているファイルだけを root からの path で返す", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{"a.txt": "a", "dir/b.txt": "b", "untracked.txt": "u"})
		for _, args := range [][]string{{"init", "-q"}, {"add", "a.txt", "dir/b.txt"}} {
			require.NoError(t, exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).Run())
		}
		got, err := gitLsFiles(root)
		require.NoError(t, err)
		assert.Equal(t, []string{"a.txt", "dir/b.txt"}, got)
	})

	t.Run("リポジトリでなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		_, err := gitLsFiles(t.TempDir())
		require.ErrorIs(t, err, errLsFiles)
	})
}

func Test_pathFindings(t *testing.T) {
	t.Parallel()

	check := func(name string, paths ...string) checksdecl.Check { return checksdecl.Check{Name: name, Paths: paths} }
	files := []string{"scripts/a/b.go", "mise.toml"}

	t.Run("どの pattern もどれかのファイルに当たり、paths が空の検査は見なければ違反0件", func(t *testing.T) {
		t.Parallel()
		decl := checksdecl.Decl{Checks: []checksdecl.Check{check("a", "scripts/**", "**/*.go", "mise.toml"), check("always")}}
		assert.Empty(t, pathFindings(decl, "checks.json", files))
		assert.Empty(t, pathFindings(checksdecl.Decl{Checks: []checksdecl.Check{check("always")}}, "checks.json", nil))
	})

	t.Run("当たらない pattern ごとに、検査と pattern を名指して宣言へ報告する", func(t *testing.T) {
		t.Parallel()
		decl := checksdecl.Decl{Checks: []checksdecl.Check{check("a", "mise.toml", "env/**"), check("b", "docs/*.md")}}
		got := pathFindings(decl, "checks.json", files)
		require.Len(t, got, 2)
		assert.Equal(t, "checks.json", got[0].File)
		assert.Contains(t, got[0].Message, "検査 `a` の paths の `env/**`")
		assert.Contains(t, got[1].Message, "検査 `b` の paths の `docs/*.md`")
	})
}

func Test_readLegacy(t *testing.T) {
	t.Parallel()

	t.Run("ファイルが無ければ nil を返す", func(t *testing.T) {
		t.Parallel()
		got, err := readLegacy(filepath.Join(t.TempDir(), "missing.json"))
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("在れば required の context を返す", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{"r.json": rulesetOneContext})
		got, err := readLegacy(filepath.Join(root, "r.json"))
		require.NoError(t, err)
		assert.Equal(t, []string{"build"}, got)
	})

	t.Run("在るのに0件なら番兵で返す", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{"r.json": `{"rules": []}`})
		_, err := readLegacy(filepath.Join(root, "r.json"))
		require.ErrorIs(t, err, requiredchecks.ErrNone)
	})

	t.Run("在るのに読めなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{"r.json": "nope"})
		_, err := readLegacy(filepath.Join(root, "r.json"))
		require.ErrorIs(t, err, requiredchecks.ErrRead)
	})
}

func Test_findPullRequestTrigger(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source    string
		wantOK    bool
		wantHits  int
		wantFirst string
	}{
		"フィルタ無し":           {source: "on:\n  pull_request:\n  push:\n", wantOK: true},
		"paths":            {source: "on:\n  pull_request:\n    paths:\n      - x\n", wantOK: true, wantHits: 1, wantFirst: "paths"},
		"paths-ignore":     {source: "on:\n  pull_request:\n    paths-ignore:\n      - x\n", wantOK: true, wantHits: 1, wantFirst: "paths-ignore"},
		"branches-ignore":  {source: "on:\n  pull_request:\n    branches-ignore:\n      - x\n", wantOK: true, wantHits: 1, wantFirst: "branches-ignore"},
		"次のイベントで打ち切る":      {source: "on:\n  pull_request:\n  push:\n    paths:\n      - x\n", wantOK: true},
		"pull_request が無い": {source: "on:\n  push:\n", wantOK: false},
		"on: が無い":          {source: "jobs:\n  a:\n", wantOK: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hits, ok := findPullRequestTrigger(tt.source)
			assert.Equal(t, tt.wantOK, ok)
			assert.Len(t, hits, tt.wantHits)
			if tt.wantFirst != "" {
				assert.Equal(t, tt.wantFirst, hits[0].key)
			}
		})
	}
}

// 記法の差で検出が外れると、フィルタが残ったまま検査が沈黙する。GitHub が受け付ける形をすべて固定する。
func Test_findPullRequestTrigger_記法の差(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source   string
		wantOK   bool
		wantHits int
	}{
		"配列記法":                   {source: "on: [pull_request]\njobs:\n", wantOK: true},
		"配列記法に複数のイベント":           {source: "on: [push, pull_request]\njobs:\n", wantOK: true},
		"配列記法に pull_request が無い": {source: "on: [push]\njobs:\n", wantOK: false},
		"単一値記法":                  {source: "on: pull_request\njobs:\n", wantOK: true},
		"単一値記法の別イベント":            {source: "on: push\njobs:\n", wantOK: false},
		"二重引用符のキー":               {source: "on:\n  \"pull_request\":\njobs:\n", wantOK: true},
		"単引用符のキー":                {source: "on:\n  'pull_request':\njobs:\n", wantOK: true},
		"キーに行末コメント":              {source: "on:\n  pull_request: # なぜ\njobs:\n", wantOK: true},
		"引用符付きのフィルタキー":           {source: "on:\n  pull_request:\n    \"paths\":\n      - x\njobs:\n", wantOK: true, wantHits: 1},
		"引用符付きの後続イベントで打ち切る":      {source: "on:\n  pull_request:\n  \"push\":\n    paths:\n      - x\njobs:\n", wantOK: true},
		"桁の違うキーはフィルタとみなさない":      {source: "on:\n  pull_request:\n      paths:\n        - x\njobs:\n", wantOK: true},
		"フィルタが2つ":                {source: "on:\n  pull_request:\n    paths:\n      - x\n    branches:\n      - main\njobs:\n", wantOK: true, wantHits: 2},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hits, ok := findPullRequestTrigger(tt.source)
			assert.Equal(t, tt.wantOK, ok)
			assert.Len(t, hits, tt.wantHits)
		})
	}
}

func Test_check(t *testing.T) {
	t.Parallel()

	const ok = "on:\n  pull_request:\n\njobs:\n  build:\n    runs-on: x\n"

	t.Run("複数の context をそれぞれ数える", func(t *testing.T) {
		t.Parallel()
		wfs := []Source{
			{File: "a.yaml", Source: ok},
			{File: "b.yaml", Source: "on:\n  pull_request:\n\njobs:\n  test:\n    runs-on: x\n"},
		}
		got := check([]string{"build", "test"}, wfs, "r.json")
		assert.Empty(t, got)
	})

	t.Run("required でない job は数えない", func(t *testing.T) {
		t.Parallel()
		wfs := []Source{{File: "a.yaml", Source: ok + "  other:\n    runs-on: x\n"}}
		got := check([]string{"build"}, wfs, "r.json")
		assert.Empty(t, got)
	})

	t.Run("context が0件なら違反も0件", func(t *testing.T) {
		t.Parallel()
		// 0件であること自体は run() が番兵で弾く。check は「与えられた分だけ見る」に徹する。
		assert.Empty(t, check(nil, []Source{{File: "a.yaml", Source: ok}}, "r.json"))
	})

	// jobs: を読めない workflow を飛ばして黙ると、そこに在る報告 job を数えないまま0件と報告する。
	t.Run("jobs: が見つからない workflow は違反にし、その job を数えない", func(t *testing.T) {
		t.Parallel()
		got := check([]string{"build"}, []Source{{File: "a.yaml", Source: "on:\n  pull_request:\n"}}, "r.json")
		require.Len(t, got, 2)
		assert.Equal(t, "a.yaml", got[0].File)
		assert.Equal(t, 1, got[0].Line)
		assert.Equal(t, "jobs: が見つかりません", got[0].Message)
		assert.Equal(t, "r.json", got[1].File)
		assert.Contains(t, got[1].Message, "required context `build` を報告する job は 1 件必要です（実際: 0）")
	})

	t.Run("違反の報告先は宣言側のパスにする", func(t *testing.T) {
		t.Parallel()
		got := check([]string{"missing"}, []Source{{File: "a.yaml", Source: ok}}, "custom/path.json")
		require.Len(t, got, 1)
		assert.Equal(t, "custom/path.json", got[0].File)
	})
}

func Test_readWorkflows(t *testing.T) {
	t.Parallel()

	t.Run("yaml と yml を拾い、それ以外を落とす", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"wf/a.yaml": "x", "wf/b.yml": "y", "wf/README.md": "z",
		})
		got, err := readWorkflows(filepath.Join(root, "wf"))
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Contains(t, got[0].File, "a.yaml")
	})

	t.Run("サブディレクトリは対象にしない", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{"wf/a.yaml": "x", "wf/sub/b.yaml": "y"})
		got, err := readWorkflows(filepath.Join(root, "wf"))
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := readWorkflows(filepath.Join(t.TempDir(), "missing"))
		require.Error(t, err)
	})
}

func Test_check_検査の対象外(t *testing.T) {
	t.Parallel()

	t.Run("push 側のフィルタは残っていてよい", func(t *testing.T) {
		t.Parallel()
		// push は context を報告しないので絞ってよい（.github/workflows/README.md「必須検査はすべての Pull Request で報告される」）。
		src := "on:\n  push:\n    paths:\n      - 'src/**'\n  pull_request:\n\njobs:\n  build:\n    runs-on: x\n"
		assert.Empty(t, check([]string{"build"}, []Source{{File: "a.yaml", Source: src}}, "r.json"))
	})

	t.Run("required でない job は起動条件を問わない", func(t *testing.T) {
		t.Parallel()
		// required でない check は、報告されなくてもマージを止めない。
		src := "on:\n  pull_request:\n    paths:\n      - x\n\njobs:\n  other:\n    runs-on: x\n  build:\n    runs-on: x\n"
		got := check([]string{"other"}, []Source{{File: "a.yaml", Source: src}}, "r.json")
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "`other`")
	})

	t.Run("pull_request の後続イベントに付いたフィルタは拾わない", func(t *testing.T) {
		t.Parallel()
		src := "on:\n  pull_request:\n  push:\n    paths:\n      - x\n\njobs:\n  build:\n    runs-on: x\n"
		assert.Empty(t, check([]string{"build"}, []Source{{File: "a.yaml", Source: src}}, "r.json"))
	})
}

func Test_triggerFindings(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("pull_request にフィルタが無ければ違反を返さない", func(t *testing.T) {
			t.Parallel()
			src := "on:\n  pull_request:\n  push:\n\njobs:\n  build:\n    runs-on: x\n"
			assert.Empty(t, triggerFindings("build", Source{File: "a.yaml", Source: src}))
		})

		t.Run("配列記法の pull_request はフィルタを書けないので違反を返さない", func(t *testing.T) {
			t.Parallel()
			src := "on: [push, pull_request]\njobs:\n  build:\n    runs-on: x\n"
			assert.Empty(t, triggerFindings("build", Source{File: "a.yaml", Source: src}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("pull_request トリガーが無ければ workflow の先頭を指して1件返す", func(t *testing.T) {
			t.Parallel()
			src := "on:\n  push:\n\njobs:\n  build:\n    runs-on: x\n"
			got := triggerFindings("build", Source{File: "a.yaml", Source: src})
			require.Len(t, got, 1)
			assert.Equal(t, "a.yaml", got[0].File)
			assert.Equal(t, 1, got[0].Line)
			assert.Contains(t, got[0].Message, "pull_request トリガーがありません")
			assert.Contains(t, got[0].Message, "`build`")
		})

		t.Run("配列記法に pull_request が無ければ違反にする", func(t *testing.T) {
			t.Parallel()
			// 配列記法の肯定側だけを見ていると、含まれているかの判定を外しても緑のまま残る。
			src := "on: [push]\njobs:\n  build:\n    runs-on: x\n"
			got := triggerFindings("build", Source{File: "a.yaml", Source: src})
			require.Len(t, got, 1)
			assert.Equal(t, 1, got[0].Line)
			assert.Contains(t, got[0].Message, "pull_request トリガーがありません")
		})

		t.Run("空の source もトリガー無しとして違反にする", func(t *testing.T) {
			t.Parallel()
			// 読めなかった workflow を「フィルタが無い」側へ倒すと、検査は何も見ずに緑を返す。
			got := triggerFindings("build", Source{File: "a.yaml"})
			require.Len(t, got, 1)
			assert.Contains(t, got[0].Message, "pull_request トリガーがありません")
		})

		t.Run("残ったフィルタをその行番号とともに1つずつ返す", func(t *testing.T) {
			t.Parallel()
			src := "on:\n  pull_request:\n    paths:\n      - x\n    branches:\n      - main\n\njobs:\n  build:\n    runs-on: x\n"
			got := triggerFindings("build", Source{File: "a.yaml", Source: src})
			require.Len(t, got, 2)
			assert.Equal(t, "a.yaml", got[0].File)
			assert.Equal(t, 3, got[0].Line)
			assert.Contains(t, got[0].Message, "`paths`")
			assert.Contains(t, got[0].Message, "`build`")
			assert.Equal(t, 5, got[1].Line)
			assert.Contains(t, got[1].Message, "`branches`")
		})
	})
}

func Test_nameFindings(t *testing.T) {
	t.Parallel()

	jobOf := func(t *testing.T, src string) workflow.Job {
		t.Helper()
		jobs := workflow.SplitJobs(src).Jobs
		require.Len(t, jobs, 1)
		return jobs[0]
	}

	for name, c := range map[string]struct {
		src  string
		want int
	}{
		"name: が無ければ違反0件":       {"jobs:\n  build:\n    runs-on: x\n", 0},
		"step の name: は数えない":    {"jobs:\n  build:\n    runs-on: x\n    steps:\n      - name: Build\n        run: make\n", 0},
		"job の name: は違反":       {"jobs:\n  build:\n    name: Build\n    runs-on: x\n", 1},
		"引用符付きの name: も違反":      {"jobs:\n  build:\n    \"name\": Build\n    runs-on: x\n", 1},
		"コメントの name: は ID と別の行": {"jobs:\n  build:\n    # name: Build\n    runs-on: x\n", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Len(t, nameFindings(jobOf(t, c.src), "ci.yaml"), c.want)
		})
	}
}
