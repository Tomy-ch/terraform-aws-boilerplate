package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
)

func parsed(t *testing.T, file, src string) wfDoc {
	t.Helper()
	d, f := parseWorkflow(Source{File: file, Source: src})
	require.Nil(t, f)
	return d
}

func yamlNode(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(src), &doc))
	return doc.Content[0]
}

func messages(findings []lintreport.Finding) string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Message)
	}
	return strings.Join(out, "\n")
}

func boolPtr(v bool) *bool { return &v }

func declCheck(name, workflow, scope string, req bool) checksdecl.Check {
	return checksdecl.Check{Name: name, Workflow: workflow, Paths: []string{}, Scope: scope, Required: boolPtr(req)}
}

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ YAML / シェルの字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const prGateSrc = `on:
  pull_request:
jobs:
  changes:
    runs-on: x
  call-a:
    uses: ./.github/workflows/a.yaml
  call-b:
    uses: ./.github/workflows/b.yaml
  gate:
    needs: [changes, call-a, call-b]
    if: always()
    runs-on: x
`

// editorconfig-checker-enable

func gateSpecAB() gateSpec {
	return gateSpec{
		file: prGateFile, job: gateJob, fixed: []string{changesJob},
		checks: []checksdecl.Check{declCheck("a", "a.yaml", checksdecl.ScopeTree, true), declCheck("b", "b.yaml", checksdecl.ScopeChange, true)},
	}
}

func Test_gateFindings(t *testing.T) {
	t.Parallel()

	decl := checksdecl.Decl{Checks: []checksdecl.Check{declCheck("a", "a.yaml", checksdecl.ScopeTree, true), declCheck("b", "b.yaml", checksdecl.ScopeChange, true)}}
	valid := func() []Source {
		return []Source{
			{File: "wf/pr-gate.yaml", Source: prGateSrc},
			{File: "wf/line-verify.yaml", Source: "on:\n  push:\njobs:\n  call-a:\n    uses: ./.github/workflows/a.yaml\n  line-gate:\n    needs: call-a\n    if: always()\n    runs-on: x\n"},
			{File: "wf/a.yaml", Source: "on:\n  workflow_call:\njobs:\n  a:\n    runs-on: x\n"},
			{File: "wf/b.yaml", Source: "on:\n  workflow_call:\njobs:\n  b:\n    runs-on: x\n"},
		}
	}
	in := gateInput{
		contexts: []requiredchecks.Context{{Name: "gate", File: "r/lines.json"}, {Name: "line-gate", File: "r/deploy.json"}},
		decl:     decl, checksPath: "checks.json", workflowsDir: "wf",
	}

	t.Run("宣言と workflow が一致すれば違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, gateFindings(in, valid()))
	})

	t.Run("解釈できない workflow は違反1件にし、gate の workflow が無いとは報告しない", func(t *testing.T) {
		t.Parallel()
		srcs := valid()
		srcs[0].Source = "on: [\n"
		got := gateFindings(in, srcs)
		msgs := messages(got)
		assert.Contains(t, msgs, "workflow を解釈できません")
		assert.NotContains(t, msgs, "pr-gate.yaml がありません")
		// 読めない pr-gate.yaml の job は数えられないので、gate の報告 job が0件として現れる。
		assert.Contains(t, msgs, "required context `gate` を報告する job は 1 件必要です（実際: 0）")
	})

	t.Run("gate と線の検証の workflow が無ければそれぞれ報告する", func(t *testing.T) {
		t.Parallel()
		got := gateFindings(in, valid()[2:])
		msgs := messages(got)
		assert.Contains(t, msgs, "pr-gate.yaml がありません")
		assert.Contains(t, msgs, "line-verify.yaml がありません")
		files := map[string]bool{}
		for _, f := range got {
			files[f.File] = true
		}
		assert.True(t, files[filepath.Join("wf", prGateFile)])
		assert.True(t, files[filepath.Join("wf", lineVerifyFile)])
	})

	t.Run("古い宣言の context と同じ名前の call-* も衝突にする", func(t *testing.T) {
		t.Parallel()
		withLegacy := in
		withLegacy.legacy = []string{"call-a"}
		assert.Contains(t, messages(gateFindings(withLegacy, valid())), "job の id `call-a` が ruleset の required context と同じ名前です")
	})

	t.Run("古い宣言が在れば、呼ばれる workflow の pull_request を違反にしない", func(t *testing.T) {
		t.Parallel()
		srcs := valid()
		srcs[2].Source = "on:\n  pull_request:\n  workflow_call:\njobs:\n  a:\n    runs-on: x\n"
		assert.NotEmpty(t, gateFindings(in, srcs))
		withLegacy := in
		withLegacy.legacy = []string{"a"}
		assert.Empty(t, gateFindings(withLegacy, srcs))
	})
}

func Test_parseWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("イベントと job を宣言の順に読む", func(t *testing.T) {
		t.Parallel()
		d := parsed(t, "a.yaml", "on:\n  push:\n  pull_request:\njobs:\n  x:\n    runs-on: a\n  y:\n    runs-on: b\n")
		require.Len(t, d.events, 2)
		assert.Equal(t, "push", d.events[0].name)
		assert.Equal(t, 2, d.events[0].line)
		require.Len(t, d.jobs, 2)
		assert.Equal(t, "y", d.jobs[1].id)
		assert.Equal(t, 7, d.jobs[1].line)
	})

	for name, c := range map[string]struct {
		src  string
		line int
		want string
	}{
		"YAML でない":            {"on: [\n", 1, "workflow を解釈できません"},
		"空の文書":                {"", 1, "最上位が mapping ではありません"},
		"最上位が配列":              {"- a\n", 1, "最上位が mapping ではありません"},
		"on: が無い":             {"jobs:\n  a:\n    runs-on: x\n", 1, "on: がありません"},
		"on: の値が読めない":         {"on:\n  - [a]\njobs:\n  a:\n    runs-on: x\n", 1, "イベント名ではありません"},
		"jobs: が無い":           {"on: push\n", 1, "jobs: がありません"},
		"jobs: が mapping でない": {"on: push\njobs: []\n", 2, "jobs: が mapping ではありません"},
		"job の値が mapping でない": {"on: push\njobs:\n  a: x\n", 3, "「id: mapping」の形ではありません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, f := parseWorkflow(Source{File: "a.yaml", Source: c.src})
			require.NotNil(t, f)
			assert.Equal(t, "a.yaml", f.File)
			assert.Equal(t, c.line, f.Line)
			assert.Contains(t, f.Message, c.want)
		})
	}
}

func Test_lookup(t *testing.T) {
	t.Parallel()

	m := yamlNode(t, "a: 1\nb:\n  c: 2\n")

	t.Run("key と値の node を返す", func(t *testing.T) {
		t.Parallel()
		k, v := lookup(m, "b")
		require.NotNil(t, k)
		assert.Equal(t, 2, k.Line)
		assert.Equal(t, yaml.MappingNode, v.Kind)
	})

	t.Run("無い key は両方 nil", func(t *testing.T) {
		t.Parallel()
		k, v := lookup(m, "c")
		assert.Nil(t, k)
		assert.Nil(t, v)
	})

	t.Run("mapping でなければ両方 nil", func(t *testing.T) {
		t.Parallel()
		k, v := lookup(yamlNode(t, "- a\n"), "a")
		assert.Nil(t, k)
		assert.Nil(t, v)
		k, v = lookup(nil, "a")
		assert.Nil(t, k)
		assert.Nil(t, v)
	})
}

func Test_readEvents(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		src  string
		want []string
	}{
		"単一値":   {"pull_request", []string{"pull_request"}},
		"配列":    {"[push, workflow_call]", []string{"push", "workflow_call"}},
		"マッピング": {"push:\n  branches: [a]\nworkflow_call:\n", []string{"push", "workflow_call"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := readEvents(yamlNode(t, c.src))
			require.NoError(t, err)
			names := make([]string, 0, len(got))
			for _, e := range got {
				names = append(names, e.name)
			}
			assert.Equal(t, c.want, names)
		})
	}

	t.Run("マッピング記法だけがイベントの値を持つ", func(t *testing.T) {
		t.Parallel()
		got, err := readEvents(yamlNode(t, "push:\n  branches: [a]\n"))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, yaml.MappingNode, got[0].value.Kind)
		got, err = readEvents(yamlNode(t, "[push]"))
		require.NoError(t, err)
		assert.Nil(t, got[0].value)
	})

	for name, src := range map[string]string{
		"配列の要素がスカラーでない":    "[[push]]",
		"マッピングのキーがスカラーでない": "? [push]\n: x\n",
	} {
		t.Run(name+"ならエラー", func(t *testing.T) {
			t.Parallel()
			_, err := readEvents(yamlNode(t, src))
			require.Error(t, err)
		})
	}

	t.Run("単一値・配列・マッピングのどれでもなければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := readEvents(&yaml.Node{Kind: yaml.AliasNode})
		require.Error(t, err)
	})
}

func Test_wfDoc_event(t *testing.T) {
	t.Parallel()

	d := parsed(t, "a.yaml", "on:\n  push:\n  pull_request:\njobs:\n  a:\n    runs-on: x\n")

	e, ok := d.event("pull_request")
	require.True(t, ok)
	assert.Equal(t, 3, e.line)

	_, ok = d.event("workflow_call")
	assert.False(t, ok)
}

func Test_wfJob_get(t *testing.T) {
	t.Parallel()

	j := parsed(t, "a.yaml", "on: push\njobs:\n  a:\n    if: always()\n    steps:\n      - if: x\n").jobs[0]

	k, v := j.get("if")
	require.NotNil(t, k)
	assert.Equal(t, 4, k.Line)
	assert.Equal(t, "always()", v.Value)

	// step の if: は job の直下ではない。
	k, _ = parsed(t, "a.yaml", "on: push\njobs:\n  a:\n    steps:\n      - if: x\n").jobs[0].get("if")
	assert.Nil(t, k)
}

func Test_treeChecks(t *testing.T) {
	t.Parallel()

	d := checksdecl.Decl{Checks: []checksdecl.Check{
		declCheck("t1", "t1.yaml", checksdecl.ScopeTree, true),
		declCheck("c", "c.yaml", checksdecl.ScopeChange, true),
		declCheck("opt", "opt.yaml", checksdecl.ScopeTree, false),
		declCheck("t2", "t2.yaml", checksdecl.ScopeTree, true),
	}}
	plan := declCheck("plan", "plan.yaml", checksdecl.ScopeTree, true)
	plan.PlanEnv = "admin"
	d.Checks = append(d.Checks, plan)
	got := treeChecks(d)
	require.Len(t, got, 2)
	assert.Equal(t, "t1", got[0].Name)
	assert.Equal(t, "t2", got[1].Name)

	assert.Empty(t, treeChecks(checksdecl.Decl{}))
}

func Test_directFindings(t *testing.T) {
	t.Parallel()

	ok := parsed(t, "wf/ci.yaml", "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n")

	t.Run("報告する job がちょうど1つなら違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, directFindings([]requiredchecks.Context{{Name: "build", File: "r.json"}}, []wfDoc{ok}))
	})

	t.Run("複数の ruleset が名指す context は1度だけ数える", func(t *testing.T) {
		t.Parallel()
		got := directFindings([]requiredchecks.Context{{Name: "build", File: "a.json"}, {Name: "build", File: "b.json"}}, []wfDoc{ok})
		assert.Empty(t, got)
	})

	for name, c := range map[string]struct {
		docs []wfDoc
		want string
	}{
		"報告する job が無い":       {nil, "（実際: 0）"},
		"2つの workflow が報告する": {[]wfDoc{ok, parsed(t, "wf/other.yaml", "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n")}, "（実際: 2）"},
	} {
		t.Run(name+"なら、最初に名指した ruleset へ報告する", func(t *testing.T) {
			t.Parallel()
			got := directFindings([]requiredchecks.Context{{Name: "build", File: "a.json"}, {Name: "build", File: "b.json"}}, c.docs)
			require.Len(t, got, 1)
			assert.Equal(t, "a.json", got[0].File)
			assert.Contains(t, got[0].Message, c.want)
		})
	}

	t.Run("報告する job 自身の違反も返す", func(t *testing.T) {
		t.Parallel()
		d := parsed(t, "wf/ci.yaml", "on:\n  pull_request:\njobs:\n  build:\n    name: Build\n    runs-on: x\n")
		got := directFindings([]requiredchecks.Context{{Name: "build", File: "r.json"}}, []wfDoc{d})
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "name: を置かないでください")
	})
}

func Test_directJobFindings(t *testing.T) {
	t.Parallel()

	find := func(t *testing.T, file, src string) []lintreport.Finding {
		t.Helper()
		d := parsed(t, file, src)
		for _, j := range d.jobs {
			if j.id == "build" {
				return directJobFindings("build", d, j)
			}
		}
		require.FailNow(t, "job build がありません")
		return nil
	}

	for name, src := range map[string]string{
		"フィルタ無し":            "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n",
		"配列記法":              "on: [pull_request]\njobs:\n  build:\n    runs-on: x\n",
		"印の付いた branches":    "on:\n  pull_request:\n    branches: # branch-set: deploy\n      - 'admin'\njobs:\n  build:\n    runs-on: x\n",
		"types は絞り込みとみなさない": "on:\n  pull_request:\n    types: [opened]\njobs:\n  build:\n    runs-on: x\n",
	} {
		t.Run(name+"なら違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, find(t, "ci.yaml", src))
		})
	}

	for name, c := range map[string]struct {
		src  string
		line int
		want string
	}{
		"reusable workflow を呼ぶ job":  {"on:\n  pull_request:\njobs:\n  build:\n    uses: ./.github/workflows/x.yaml\n", 5, "reusable workflow を呼んでいます"},
		"workflow_call を持つ workflow": {"on:\n  pull_request:\n  workflow_call:\njobs:\n  build:\n    runs-on: x\n", 3, "workflow_call を持っています"},
		"job の name:":                {"on:\n  pull_request:\njobs:\n  build:\n    name: Build\n    runs-on: x\n", 5, "name: を置かないでください"},
		"pull_request が無い":           {"on:\n  push:\njobs:\n  build:\n    runs-on: x\n", 1, "pull_request トリガーがありません"},
		"paths":                      {"on:\n  pull_request:\n    paths:\n      - x\njobs:\n  build:\n    runs-on: x\n", 3, "`paths` が残っています"},
		"paths-ignore":               {"on:\n  pull_request:\n    paths-ignore:\n      - x\njobs:\n  build:\n    runs-on: x\n", 3, "`paths-ignore` が残っています"},
		"branches-ignore は印があっても":    {"on:\n  pull_request:\n    branches-ignore: # branch-set: deploy\n      - x\njobs:\n  build:\n    runs-on: x\n", 3, "`branches-ignore` が残っています"},
		"印の無い branches":              {"on:\n  pull_request:\n    branches:\n      - main\njobs:\n  build:\n    runs-on: x\n", 3, "印 `# branch-set: <集合>`"},
		"印がほかの行にある branches":         {"on:\n  pull_request:\n    branches:\n      - main # branch-set: deploy\njobs:\n  build:\n    runs-on: x\n", 3, "印 `# branch-set: <集合>`"},
	} {
		t.Run(name+"は違反", func(t *testing.T) {
			t.Parallel()
			got := find(t, "ci.yaml", c.src)
			require.Len(t, got, 1)
			assert.Equal(t, "ci.yaml", got[0].File)
			assert.Equal(t, c.line, got[0].Line)
			assert.Contains(t, got[0].Message, "required context `build`")
			assert.Contains(t, got[0].Message, c.want)
		})
	}

	t.Run("線の検証の workflow は pull_request を求めない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, find(t, "wf/"+lineVerifyFile, "on:\n  push:\njobs:\n  build:\n    runs-on: x\n"))
	})

	t.Run("線の検証の workflow でも uses: と name: は違反", func(t *testing.T) {
		t.Parallel()
		got := find(t, "wf/"+lineVerifyFile, "on:\n  push:\njobs:\n  build:\n    name: B\n    uses: ./x.yaml\n")
		assert.Len(t, got, 2)
	})
}

func Test_gateWorkflowFindings(t *testing.T) {
	t.Parallel()

	t.Run("call-* と判定の job が宣言と一致すれば違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", prGateSrc), "checks.json"))
	})

	for name, c := range map[string]struct {
		src  string
		want []string
	}{
		"call-* が0件": {
			"on:\n  pull_request:\njobs:\n  changes:\n    runs-on: x\n  gate:\n    needs: [changes, call-a, call-b]\n    if: always()\n",
			[]string{"検査 `a` を呼ぶ job `call-a` がありません", "検査 `b` を呼ぶ job `call-b` がありません", "call-* の job は 0 件、checks.json の検査は 2 件です"},
		},
		"宣言に無い検査を呼ぶ": {
			strings.Replace(prGateSrc, "  gate:\n", "  call-z:\n    uses: ./.github/workflows/z.yaml\n  gate:\n", 1),
			[]string{"job `call-z` は checks.json に無い検査を呼んでいます", "call-* の job は 3 件、checks.json の検査は 2 件です"},
		},
		"uses が別の workflow": {
			strings.Replace(prGateSrc, "./.github/workflows/b.yaml", "./.github/workflows/a.yaml", 1),
			[]string{"job `call-b` の uses: は `./.github/workflows/b.yaml` にします"},
		},
		"uses が無い": {
			strings.Replace(prGateSrc, "    uses: ./.github/workflows/b.yaml\n", "    runs-on: x\n", 1),
			[]string{"job `call-b` の uses: は `./.github/workflows/b.yaml` にします"},
		},
		"判定の job が無い": {
			"on:\n  pull_request:\njobs:\n  call-a:\n    uses: ./.github/workflows/a.yaml\n  call-b:\n    uses: ./.github/workflows/b.yaml\n",
			[]string{"job `gate` は 1 件必要です（実際: 0）"},
		},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", c.src), "checks.json")
			require.Len(t, got, len(c.want))
			for i, w := range c.want {
				assert.Contains(t, got[i].Message, w)
			}
		})
	}

	t.Run("同じ id の call-* が2つあれば件数の差として現れる", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(prGateSrc, "  gate:\n", "  call-a:\n    uses: ./.github/workflows/a.yaml\n  gate:\n", 1)
		got := gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", src), "checks.json")
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "call-* の job は 3 件、checks.json の検査は 2 件です")
	})

	t.Run("判定の job の needs は fixed と call-* を合わせたものと突き合わせる", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(prGateSrc, "[changes, call-a, call-b]", "[call-a, call-b]", 1)
		got := gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", src), "checks.json")
		assert.Contains(t, messages(got), "needs: に `changes` がありません")
	})
}

func Test_judgeFindings(t *testing.T) {
	t.Parallel()

	judge := func(t *testing.T, body string) wfJob {
		t.Helper()
		return parsed(t, "g.yaml", "on: push\njobs:\n  gate:\n"+body).jobs[0]
	}
	want := []string{"changes", "call-a"}

	for name, body := range map[string]string{
		"配列の needs と always()":       "    needs: [changes, call-a]\n    if: always()\n",
		"${{ always() }} も always()": "    needs: [call-a, changes]\n    if: ${{ always() }}\n",
	} {
		t.Run(name+"なら違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, judgeFindings("gate", "g.yaml", judge(t, body), want))
		})
	}

	t.Run("単一値の needs を読む", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, judgeFindings("gate", "g.yaml", judge(t, "    needs: call-a\n    if: always()\n"), []string{"call-a"}))
	})

	for name, c := range map[string]struct {
		body string
		want []string
	}{
		"if: が無い":            {"    needs: [changes, call-a]\n", []string{"if: がありません"}},
		"if: が always() でない": {"    needs: [changes, call-a]\n    if: success()\n", []string{"if: は always() にします"}},
		"always() にほかの条件を足す": {"    needs: [changes, call-a]\n    if: always() && github.event_name == 'push'\n", []string{"if: は always() にします"}},
		"needs が無い":          {"    if: always()\n", []string{"needs: がありません"}},
		"needs が読めない":        {"    needs: {a: b}\n    if: always()\n", []string{"needs: を job の id の一覧として読めません"}},
		"needs が欠ける":         {"    needs: [changes]\n    if: always()\n", []string{"needs: に `call-a` がありません", "needs: は 1 件、宣言から導かれる job は 2 件です"}},
		"needs が余る":          {"    needs: [changes, call-a, call-z]\n    if: always()\n", []string{"`call-z` は宣言から導かれない job です", "needs: は 3 件、宣言から導かれる job は 2 件です"}},
		"needs が重複する":        {"    needs: [changes, call-a, call-a]\n    if: always()\n", []string{"needs: は 3 件、宣言から導かれる job は 2 件です"}},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := judgeFindings("gate", "g.yaml", judge(t, c.body), want)
			require.Len(t, got, len(c.want))
			for i, w := range c.want {
				assert.Equal(t, "g.yaml", got[i].File)
				assert.Contains(t, got[i].Message, "job `gate` ")
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func Test_isAlways(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]bool{
		"always()":                     true,
		"' always() '":                 true,
		"${{ always() }}":              true,
		"${{always()}}":                true,
		"success()":                    false,
		"always() && true":             false,
		"${{ always() }} && true":      false,
		"[always()]":                   false,
		"${{ !cancelled() }}":          false,
		"\"${{ always() }} \"":         true,
		"${{ always() || failure() }}": false,
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isAlways(yamlNode(t, "v: "+src).Content[1]))
		})
	}

	t.Run("nil は always() ではない", func(t *testing.T) {
		t.Parallel()
		assert.False(t, isAlways(nil))
	})
}

func Test_needsOf(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		src    string
		want   []string
		wantOK bool
	}{
		"単一値":      {"a", []string{"a"}, true},
		"配列":       {"[a, b]", []string{"a", "b"}, true},
		"空の配列":     {"[]", []string{}, true},
		"配列の要素が配列": {"[[a]]", nil, false},
		"マッピング":    {"{a: b}", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := needsOf(yamlNode(t, "v: "+c.src).Content[1])
			assert.Equal(t, c.wantOK, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func Test_lineVerifyTriggerFindings(t *testing.T) {
	t.Parallel()

	find := func(t *testing.T, on string) []lintreport.Finding {
		t.Helper()
		return lineVerifyTriggerFindings(parsed(t, lineVerifyFile, on+"jobs:\n  line-gate:\n    runs-on: x\n"))
	}

	for name, on := range map[string]string{
		"push だけ":        "on:\n  push:\n",
		"印の付いた branches": "on:\n  push:\n    branches: # branch-set: lines\n      - 'main'\n",
		"単一値の push":      "on: push\n",
	} {
		t.Run(name+"なら違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, find(t, on))
		})
	}

	for name, c := range map[string]struct {
		on   string
		want []string
	}{
		"pull_request でも起動する":     {"on:\n  push:\n  pull_request:\n", []string{"push のほかのイベント（pull_request）"}},
		"push が無い":                {"on:\n  workflow_dispatch:\n", []string{"push のほかのイベント（workflow_dispatch）", "push トリガーがありません"}},
		"push を paths で絞る":        {"on:\n  push:\n    paths:\n      - x\n", []string{"`paths` があります"}},
		"push を paths-ignore で絞る": {"on:\n  push:\n    paths-ignore:\n      - x\n", []string{"`paths-ignore` があります"}},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := find(t, c.on)
			require.Len(t, got, len(c.want))
			for i, w := range c.want {
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func Test_calledFindings(t *testing.T) {
	t.Parallel()

	ok := parsed(t, "wf/a.yaml", "on:\n  workflow_call:\njobs:\n  a:\n    runs-on: x\n")
	withPR := parsed(t, "wf/a.yaml", "on:\n  pull_request:\n  workflow_call:\njobs:\n  a:\n    runs-on: x\n")
	in := gateInput{
		decl:       checksdecl.Decl{Checks: []checksdecl.Check{declCheck("a", "a.yaml", checksdecl.ScopeTree, true), declCheck("a2", "a.yaml", checksdecl.ScopeChange, true)}},
		checksPath: "checks.json", workflowsDir: "wf",
	}

	t.Run("同じ workflow を名指す検査が2つあっても1度だけ検査する", func(t *testing.T) {
		t.Parallel()
		got := calledFindings(in, map[string]wfDoc{"a.yaml": withPR}, nil)
		assert.Len(t, got, 1)
	})

	t.Run("workflow が無ければ宣言の側へ報告する", func(t *testing.T) {
		t.Parallel()
		got := calledFindings(in, map[string]wfDoc{}, nil)
		require.Len(t, got, 1)
		assert.Equal(t, "checks.json", got[0].File)
		assert.Contains(t, got[0].Message, "検査 `a` の workflow `a.yaml` が wf にありません")
	})

	t.Run("解釈できなかった workflow は無いとは報告しない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, calledFindings(in, map[string]wfDoc{}, map[string]bool{"a.yaml": true}))
	})

	t.Run("古い宣言の有無で最終の形を課すかを決める", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, calledFindings(in, map[string]wfDoc{"a.yaml": ok}, nil))
		withLegacy := in
		withLegacy.legacy = []string{"a"}
		assert.Empty(t, calledFindings(withLegacy, map[string]wfDoc{"a.yaml": withPR}, nil))
	})
}

func Test_calledWorkflowFindings(t *testing.T) {
	t.Parallel()

	const job = "jobs:\n  a:\n    runs-on: x\n"

	t.Run("workflow_call だけで起動し、if: と caller の名前の group を持たなければ違反0件", func(t *testing.T) {
		t.Parallel()
		src := "on:\n  workflow_call:\nconcurrency:\n  group: a-${{ github.ref }}\njobs:\n  a:\n    concurrency: x-${{ github.job }}\n    runs-on: x\n    steps:\n      - if: always()\n"
		assert.Empty(t, calledWorkflowFindings(parsed(t, "a.yaml", src), true))
	})

	for name, c := range map[string]struct {
		src   string
		final bool
		line  int
		want  string
	}{
		"workflow_call が無い":                            {"on:\n  push:\n" + job, false, 1, "workflow_call トリガーがありません"},
		"最終の形で pull_request で起動する":                     {"on:\n  workflow_call:\n  pull_request:\n" + job, true, 3, "pull_request で起動します"},
		"最終の形で job が if: を持つ":                          {"on: workflow_call\njobs:\n  a:\n    if: x\n    runs-on: x\n", true, 4, "job `a` が if: を持っています"},
		"workflow の concurrency が github.workflow を含む": {"on: workflow_call\nconcurrency:\n  group: ${{ github.workflow }}-${{ github.ref }}\n" + job, false, 3, "workflow の concurrency の group が github.workflow を含みます"},
		"job の concurrency が github.workflow を含む":      {"on: workflow_call\njobs:\n  a:\n    concurrency: ${{ github.workflow }}\n    runs-on: x\n", false, 4, "job `a` の concurrency"},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := calledWorkflowFindings(parsed(t, "a.yaml", c.src), c.final)
			require.Len(t, got, 1)
			assert.Equal(t, c.line, got[0].Line)
			assert.Contains(t, got[0].Message, c.want)
		})
	}

	t.Run("並行期間は pull_request と job の if: を違反にしない", func(t *testing.T) {
		t.Parallel()
		src := "on:\n  workflow_call:\n  pull_request:\njobs:\n  a:\n    if: x\n    runs-on: x\n"
		assert.Empty(t, calledWorkflowFindings(parsed(t, "a.yaml", src), false))
		assert.Len(t, calledWorkflowFindings(parsed(t, "a.yaml", src), true), 2)
	})
}

func Test_concurrencyFindings(t *testing.T) {
	t.Parallel()

	find := func(t *testing.T, value string) []lintreport.Finding {
		t.Helper()
		m := yamlNode(t, "concurrency: "+value)
		return concurrencyFindings("a.yaml", m.Content[0], m.Content[1], "workflow")
	}

	for name, value := range map[string]string{
		"github.ref だけ":          "${{ github.ref }}",
		"github.workflow_sha":    "${{ github.workflow_sha }}",
		"group の無い mapping":      "{cancel-in-progress: true}",
		"github.workflows は別の名前": "${{ github.workflows }}",
	} {
		t.Run(name+"なら違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, find(t, value))
		})
	}

	for name, c := range map[string]struct {
		value string
		want  string
	}{
		"単一値の github.workflow":              {"${{ github.workflow }}-x", "github.workflow を含みます"},
		"mapping の group の github.workflow": {"{group: \"${{ github.workflow }}\", cancel-in-progress: true}", "github.workflow を含みます"},
		"大文字の GitHub.Workflow":              {"${{ GitHub.Workflow }}", "github.workflow を含みます"},
		"github.workflow_ref":               {"${{ github.workflow_ref }}", "github.workflow を含みます"},
		"group が読めない":                       {"{group: [a]}", "group を読めません"},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := find(t, c.value)
			require.Len(t, got, 1)
			assert.Equal(t, "a.yaml", got[0].File)
			assert.Contains(t, got[0].Message, c.want)
		})
	}
}

func Test_collisionFindings(t *testing.T) {
	t.Parallel()

	jobs := func(t *testing.T, file string, ids ...string) wfDoc {
		t.Helper()
		var src strings.Builder
		src.WriteString("on: push\njobs:\n")
		for _, id := range ids {
			src.WriteString("  " + id + ":\n    runs-on: x\n")
		}
		return parsed(t, file, src.String())
	}
	contexts := map[string]bool{"call-x": true, "gate": true}

	t.Run("gate と line-gate がそれぞれの workflow に在り、call-* が context と重ならなければ違反0件", func(t *testing.T) {
		t.Parallel()
		docs := []wfDoc{jobs(t, "wf/"+prGateFile, "gate", "call-a"), jobs(t, "wf/"+lineVerifyFile, "line-gate", "call-a")}
		assert.Empty(t, collisionFindings(docs, contexts))
	})

	for name, c := range map[string]struct {
		doc  func(t *testing.T) wfDoc
		want string
	}{
		"gate が pr-gate.yaml の外にある":          {func(t *testing.T) wfDoc { return jobs(t, "wf/ci.yaml", "gate") }, "`gate` は pr-gate.yaml の外に置けません"},
		"line-gate が line-verify.yaml の外にある": {func(t *testing.T) wfDoc { return jobs(t, "wf/"+prGateFile, "line-gate") }, "`line-gate` は line-verify.yaml の外に置けません"},
		"call-* が context と同じ名前":             {func(t *testing.T) wfDoc { return jobs(t, "wf/ci.yaml", "call-x") }, "`call-x` が ruleset の required context と同じ名前です"},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := collisionFindings([]wfDoc{c.doc(t)}, contexts)
			require.Len(t, got, 1)
			assert.Equal(t, 3, got[0].Line)
			assert.Contains(t, got[0].Message, c.want)
		})
	}

	t.Run("call- で始まらない job は context と同じ名前でも衝突にしない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, collisionFindings([]wfDoc{jobs(t, "wf/ci.yaml", "x")}, map[string]bool{"x": true}))
	})
}
