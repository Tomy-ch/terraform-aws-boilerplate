package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// writeFixture はリポジトリの実物ではなく一時ディレクトリへ検査対象を組み立てます。
// 実物を読むテストは、リポジトリの今日の内容で通ったり落ちたりするようになり、
// ツールについてのテストであることをやめます。
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

const rulesetOneContext = `{
  "rules": [
    {"type": "required_status_checks",
     "parameters": {"required_status_checks": [{"context": "build"}]}}
  ]
}`

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("required context を報告する job が在り、フィルタが無ければ通る", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"ruleset.json": rulesetOneContext,
			"wf/ci.yaml":   "on:\n  pull_request:\n  workflow_dispatch:\n\njobs:\n  build:\n    runs-on: x\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.NoError(t, err)
		assert.Contains(t, out.String(), "required context 1 件")
	})

	t.Run("pull_request に paths が残っていれば落ちる", func(t *testing.T) {
		t.Parallel()
		// 除外された Pull Request では run が1件も起動せず、GitHub は報告の不在を待ち続ける。
		// その Pull Request は恒久的にマージ可能にならない。
		root := writeFixture(t, map[string]string{
			"ruleset.json": rulesetOneContext,
			"wf/ci.yaml":   "on:\n  pull_request:\n    paths:\n      - 'src/**'\n\njobs:\n  build:\n    runs-on: x\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "`paths` が残っています")
		assert.Contains(t, out.String(), ":3")
	})

	t.Run("branches フィルタも同じく落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"ruleset.json": rulesetOneContext,
			"wf/ci.yaml":   "on:\n  pull_request:\n    branches:\n      - main\n\njobs:\n  build:\n    runs-on: x\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "`branches` が残っています")
	})

	t.Run("報告する job が存在しなければ落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"ruleset.json": rulesetOneContext,
			"wf/ci.yaml":   "on:\n  pull_request:\n\njobs:\n  other:\n    runs-on: x\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "実際: 0")
	})

	t.Run("同じ context を2つの job が報告すれば落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"ruleset.json": rulesetOneContext,
			"wf/a.yaml":    "on:\n  pull_request:\n\njobs:\n  build:\n    runs-on: x\n",
			"wf/b.yaml":    "on:\n  pull_request:\n\njobs:\n  build:\n    runs-on: x\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "実際: 2")
	})

	t.Run("pull_request トリガーが無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"ruleset.json": rulesetOneContext,
			"wf/ci.yaml":   "on:\n  push:\n\njobs:\n  build:\n    runs-on: x\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "pull_request トリガーがありません")
	})

	t.Run("jobs: を読めない workflow は取り違えとして報告する", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"ruleset.json": rulesetOneContext,
			"wf/ci.yaml":   "on:\n  pull_request:\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "jobs: が見つかりません")
	})

	// 退化した入力の pin。ゲートは「何も検査せず緑を報告する」方向へ壊れる。
	t.Run("required status check が0件なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{
			"ruleset.json": `{"rules": [{"type": "deletion"}]}`,
			"wf/ci.yaml":   "on:\n  pull_request:\n\njobs:\n  build:\n    runs-on: x\n",
		})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "ruleset.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
		// 違反（終了コード1）とは別に扱えるよう、番兵で識別できること。
		assert.True(t, xerrors.Is(err, errNoRequiredContext))
	})

	t.Run("宣言を読めなければ落ちる", func(t *testing.T) {
		t.Parallel()
		root := writeFixture(t, map[string]string{"wf/ci.yaml": "jobs:\n  build:\n    runs-on: x\n"})
		var out bytes.Buffer
		err := run([]string{"-ruleset", filepath.Join(root, "missing.json"), "-workflows", filepath.Join(root, "wf")}, &out)
		require.Error(t, err)
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

	t.Run("違反の報告先は宣言側のパスにする", func(t *testing.T) {
		t.Parallel()
		got := check([]string{"missing"}, []Source{{File: "a.yaml", Source: ok}}, "custom/path.json")
		require.Len(t, got, 1)
		assert.Equal(t, "custom/path.json", got[0].File)
	})
}

func Test_readRequiredContexts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want []string
		err  bool
	}{
		"1件":                        {body: rulesetOneContext, want: []string{"build"}},
		"複数件":                       {body: `{"rules":[{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"a"},{"context":"b"}]}}]}`, want: []string{"a", "b"}},
		"別の型の rule は無視":             {body: `{"rules":[{"type":"deletion"},{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"a"}]}}]}`, want: []string{"a"}},
		"rules が空":                  {body: `{"rules":[]}`, want: nil},
		"required_status_checks が空": {body: `{"rules":[{"type":"required_status_checks","parameters":{"required_status_checks":[]}}]}`, want: nil},
		"壊れた JSON":                  {body: `{`, err: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := writeFixture(t, map[string]string{"r.json": tt.body})
			got, err := readRequiredContexts(filepath.Join(root, "r.json"))
			if tt.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
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

// ここから下は輸入した検査項目。

func Test_check_輸入したケース(t *testing.T) {
	t.Parallel()

	t.Run("push 側のフィルタは残っていてよい", func(t *testing.T) {
		t.Parallel()
		// 検査しているのは pull_request の起動条件だけである。push は required check の
		// 報告契機ではないため、そこにフィルタが在っても Pull Request は止まらない。
		src := "on:\n  push:\n    paths:\n      - 'src/**'\n  pull_request:\n\njobs:\n  build:\n    runs-on: x\n"
		assert.Empty(t, check([]string{"build"}, []Source{{File: "a.yaml", Source: src}}, "r.json"))
	})

	t.Run("required でない job は起動条件を問わない", func(t *testing.T) {
		t.Parallel()
		// required でない check は、報告されなくてもマージを止めない。
		src := "on:\n  pull_request:\n    paths:\n      - x\n\njobs:\n  other:\n    runs-on: x\n  build:\n    runs-on: x\n"
		got := check([]string{"other"}, []Source{{File: "a.yaml", Source: src}}, "r.json")
		// other は required なのでフィルタが違反になる。build は required でないので問われない。
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "`other`")
	})

	t.Run("pull_request の後続イベントに付いたフィルタは拾わない", func(t *testing.T) {
		t.Parallel()
		src := "on:\n  pull_request:\n  push:\n    paths:\n      - x\n\njobs:\n  build:\n    runs-on: x\n"
		assert.Empty(t, check([]string{"build"}, []Source{{File: "a.yaml", Source: src}}, "r.json"))
	})
}
