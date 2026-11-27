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
    steps:
      - uses: actions/checkout@x
      - id: changes
        run: make ci-changes RUNNER_MODE=host
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
    if: always()
    runs-on: x
    steps:
      - continue-on-error: true
        run: sleep 1
      - env:
          NEEDS: ${{ toJSON(needs) }}
        run: make ci-gate RUNNER_MODE=host
`

const lineVerifySrc = `on:
  push:
jobs:
  call-a:
    uses: ./.github/workflows/a.yaml
  line-gate:
    needs: call-a
    if: always()
    runs-on: x
    steps:
      - env:
          NEEDS: ${{ toJSON(needs) }}
        run: make ci-line-gate RUNNER_MODE=host
`

// editorconfig-checker-enable

// jobOf は、fixture の src から job id の区間（次の job の手前まで）を返す。
func jobOf(src, id string) string {
	start := strings.Index(src, "\n  "+id+":\n")
	if start < 0 {
		return ""
	}
	rest := src[start+1:]
	next := len(rest)
	for i := 1; i < len(rest); i++ {
		if rest[i-1] == '\n' && strings.HasPrefix(rest[i:], "  ") && !strings.HasPrefix(rest[i:], "   ") {
			next = i
			break
		}
	}
	return rest[:next]
}

// withoutJob は、fixture の src から job id の区間を除く。
func withoutJob(src, id string) string { return strings.Replace(src, jobOf(src, id), "", 1) }

func gateSpecAB() gateSpec {
	return gateSpec{
		file: prGateFile, job: gateJob, run: gateRun, fixed: []string{changesJob}, gated: true,
		checks: []checksdecl.Check{declCheck("a", "a.yaml", checksdecl.ScopeTree, true), declCheck("b", "b.yaml", checksdecl.ScopeChange, true)},
	}
}

func Test_gateFindings(t *testing.T) {
	t.Parallel()

	decl := checksdecl.Decl{Checks: []checksdecl.Check{declCheck("a", "a.yaml", checksdecl.ScopeTree, true), declCheck("b", "b.yaml", checksdecl.ScopeChange, true)}}
	valid := func() []Source {
		return []Source{
			{File: "wf/pr-gate.yaml", Source: prGateSrc},
			{File: "wf/line-verify.yaml", Source: lineVerifySrc},
			{File: "wf/a.yaml", Source: "on:\n  workflow_call:\njobs:\n  a:\n    runs-on: x\n"},
			{File: "wf/b.yaml", Source: "on:\n  workflow_call:\njobs:\n  b:\n    runs-on: x\n"},
		}
	}
	in := gateInput{
		contexts: []requiredchecks.Context{{Name: "gate", File: "r/lines.json"}, {Name: "line-gate", File: "r/deploy-admin.json"}},
		decl:     decl, checksPath: "checks.json", workflowsDir: "wf", rulesetsDir: "r",
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

	t.Run("線の ruleset が gate のほかの context を required とすれば違反", func(t *testing.T) {
		t.Parallel()
		extra := in
		extra.contexts = append([]requiredchecks.Context{{Name: "build", File: "r/lines.json"}}, in.contexts...)
		srcs := append(valid(), Source{File: "wf/ci.yaml", Source: "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n"})
		got := gateFindings(extra, srcs)
		require.Len(t, got, 1)
		assert.Equal(t, filepath.Join("r", linesRulesetFile), got[0].File)
		assert.Contains(t, got[0].Message, "（実際: [build, gate]）")
	})

	t.Run("線の検証の workflow が push のほかのイベントでも起動すれば違反", func(t *testing.T) {
		t.Parallel()
		srcs := valid()
		srcs[1].Source = strings.Replace(lineVerifySrc, "on:\n  push:\n", "on:\n  push:\n  pull_request:\n", 1)
		got := gateFindings(in, srcs)
		require.Len(t, got, 1, messages(got))
		assert.Equal(t, "wf/line-verify.yaml", got[0].File)
		assert.Contains(t, got[0].Message, "push のほかのイベント（pull_request）")
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
		"マッピングにマージキーがある":   "<<: {push: }\npull_request:\n",
	} {
		t.Run(name+"ならエラー", func(t *testing.T) {
			t.Parallel()
			_, err := readEvents(yamlNode(t, src))
			require.ErrorIs(t, err, errEvents)
		})
	}

	t.Run("単一値・配列・マッピングのどれでもなければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := readEvents(&yaml.Node{Kind: yaml.AliasNode})
		require.ErrorIs(t, err, errEvents)
	})
}

func Test_opaqueEntry(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		src  string
		want int
	}{
		"スカラーのキーとふつうの値だけなら0":   {"paths: [x]\nbranches:\n  - a\n", 0},
		"マージキーの行を返す":           {"a: 1\n<<: {paths: [x]}\n", 2},
		"値が別名の項目の行を返す":         {"a: &x [b]\nbranches: *x\n", 2},
		"キーがスカラーでない項目の行を返す":    {"? [a]\n: x\n", 1},
		"引用符付きの << はマージキーではない": {"'<<': x\n", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, opaqueEntry(yamlNode(t, c.src)))
		})
	}
}

func Test_emptyEvent(t *testing.T) {
	t.Parallel()

	t.Run("配列や単一値の記法で値の node が無ければ空", func(t *testing.T) {
		t.Parallel()
		assert.True(t, emptyEvent(nil))
	})

	for name, c := range map[string]struct {
		src  string
		want bool
	}{
		"空の値は空":      {"e:\n", true},
		"~ は空":       {"e: ~\n", true},
		"null は空":    {"e: null\n", true},
		"マッピングは空でない": {"e: {}\n", false},
		"空でないスカラーは空でない":    {"e: x\n", false},
		"別名は中身を読めないので空でない": {"a: &x {}\ne: *x\n", false},
		"配列は空でない":          {"e: []\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, v := lookup(yamlNode(t, c.src), "e")
			require.NotNil(t, v)
			assert.Equal(t, c.want, emptyEvent(v))
		})
	}
}

func Test_wfDoc_event(t *testing.T) {
	t.Parallel()

	d := parsed(t, "a.yaml", "on:\n  push:\n  pull_request:\njobs:\n  a:\n    runs-on: x\n")

	t.Run("在るイベントはその行とともに返す", func(t *testing.T) {
		t.Parallel()
		e, ok := d.event("pull_request")
		require.True(t, ok)
		assert.Equal(t, 3, e.line)
	})

	t.Run("無いイベントは偽を返す", func(t *testing.T) {
		t.Parallel()
		_, ok := d.event("workflow_call")
		assert.False(t, ok)
	})
}

func Test_wfJob_get(t *testing.T) {
	t.Parallel()

	t.Run("job の直下の key と値の node を返す", func(t *testing.T) {
		t.Parallel()
		j := parsed(t, "a.yaml", "on: push\njobs:\n  a:\n    if: always()\n    steps:\n      - if: x\n").jobs[0]
		k, v := j.get("if")
		require.NotNil(t, k)
		assert.Equal(t, 4, k.Line)
		assert.Equal(t, "always()", v.Value)
	})

	t.Run("step の key は job の直下の key として返さない", func(t *testing.T) {
		t.Parallel()
		k, _ := parsed(t, "a.yaml", "on: push\njobs:\n  a:\n    steps:\n      - if: x\n").jobs[0].get("if")
		assert.Nil(t, k)
	})
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

	t.Run("scope が tree で required の検査を宣言の順に返し、plan_env を持つものは外す", func(t *testing.T) {
		t.Parallel()
		got := treeChecks(d)
		require.Len(t, got, 2)
		assert.Equal(t, "t1", got[0].Name)
		assert.Equal(t, "t2", got[1].Name)
	})

	t.Run("検査の無い宣言からは何も返さない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, treeChecks(checksdecl.Decl{}))
	})
}

func Test_directFindings(t *testing.T) {
	t.Parallel()

	ok := parsed(t, "wf/ci.yaml", "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n")

	t.Run("報告する job がちょうど1つなら違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, directFindings([]requiredchecks.Context{{Name: "build", File: "r.json"}}, nil, []wfDoc{ok}))
	})

	t.Run("複数の ruleset が名指す context は1度だけ数える", func(t *testing.T) {
		t.Parallel()
		got := directFindings([]requiredchecks.Context{{Name: "build", File: "a.json"}, {Name: "build", File: "b.json"}}, nil, []wfDoc{ok})
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
			got := directFindings([]requiredchecks.Context{{Name: "build", File: "a.json"}, {Name: "build", File: "b.json"}}, nil, c.docs)
			require.Len(t, got, 1)
			assert.Equal(t, "a.json", got[0].File)
			assert.Contains(t, got[0].Message, c.want)
		})
	}

	gated := parsed(t, "wf/ci.yaml", "on:\n  pull_request:\n    branches: # branch-set: ruleset\n      - 'a'\njobs:\n  build:\n    runs-on: x\n")

	t.Run("線の ruleset の context は印 gate を求める", func(t *testing.T) {
		t.Parallel()
		got := directFindings([]requiredchecks.Context{{Name: "build", File: "r/lines.json"}}, nil, []wfDoc{gated})
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "`# branch-set: gate` にします")
	})

	t.Run("デプロイブランチの ruleset の context は印 deploy-env を求める", func(t *testing.T) {
		t.Parallel()
		got := directFindings([]requiredchecks.Context{{Name: "build", File: "r/deploy-admin.json"}, {Name: "build", File: "r/deploy-security.json"}}, nil, []wfDoc{gated})
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "`# branch-set: deploy-env` にします")
	})

	t.Run("古い宣言にも在る context には印の集合を課さない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, directFindings([]requiredchecks.Context{{Name: "build", File: "r/deploy-admin.json"}}, []string{"build"}, []wfDoc{gated}))
	})

	t.Run("対象の集合が違う ruleset が同じ context を required とすれば違反", func(t *testing.T) {
		t.Parallel()
		got := directFindings([]requiredchecks.Context{{Name: "build", File: "r/lines.json"}, {Name: "build", File: "r/deploy-admin.json"}}, nil, []wfDoc{ok})
		require.Len(t, got, 1)
		assert.Equal(t, "r/lines.json", got[0].File)
		assert.Contains(t, got[0].Message, "対象の集合が違う ruleset")
	})

	t.Run("報告する job 自身の違反も返す", func(t *testing.T) {
		t.Parallel()
		d := parsed(t, "wf/ci.yaml", "on:\n  pull_request:\njobs:\n  build:\n    name: Build\n    runs-on: x\n")
		got := directFindings([]requiredchecks.Context{{Name: "build", File: "r.json"}}, nil, []wfDoc{d})
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "name: を置かないでください")
	})
}

func Test_directJobFindings(t *testing.T) {
	t.Parallel()

	findSet := func(t *testing.T, file, set, src string) []lintreport.Finding {
		t.Helper()
		d := parsed(t, file, src)
		for _, j := range d.jobs {
			if j.id == "build" {
				return directJobFindings("build", set, d, j)
			}
		}
		require.FailNow(t, "job build がありません")
		return nil
	}
	find := func(t *testing.T, file, src string) []lintreport.Finding {
		t.Helper()
		return findSet(t, file, setAny, src)
	}
	marked := func(set string) string {
		return "on:\n  pull_request:\n    branches: # branch-set: " + set + "\n      - 'admin'\njobs:\n  build:\n    runs-on: x\n"
	}

	for name, src := range map[string]string{
		"フィルタ無し":            "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n",
		"配列記法":              "on: [pull_request]\njobs:\n  build:\n    runs-on: x\n",
		"印の付いた branches":    marked("deploy"),
		"types は絞り込みとみなさない": "on:\n  pull_request:\n    types: [opened]\njobs:\n  build:\n    runs-on: x\n",
		"値が null":           "on:\n  pull_request: ~\njobs:\n  build:\n    runs-on: x\n",
		"値が空のマッピング":         "on:\n  pull_request: {}\njobs:\n  build:\n    runs-on: x\n",
	} {
		t.Run(name+"なら違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, find(t, "ci.yaml", src))
		})
	}

	t.Run("印の集合が ruleset の対象の集合と一致すれば違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, findSet(t, "ci.yaml", setGate, marked(setGate)))
		assert.Empty(t, findSet(t, "ci.yaml", setDeployEnv, marked(setDeployEnv)))
	})

	for name, set := range map[string]string{
		"印の集合で表せない ruleset": setNone,
		"線の ruleset":        setGate,
		"デプロイブランチの ruleset": setDeployEnv,
	} {
		t.Run("branches が無ければ、"+name+"の context でも違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, findSet(t, "ci.yaml", set, "on:\n  pull_request:\njobs:\n  build:\n    runs-on: x\n"))
		})
	}

	for name, c := range map[string]struct {
		set, src, want string
	}{
		"線の ruleset の context の印が ruleset": {setGate, marked("ruleset"), "`# branch-set: gate` にします（実際: ruleset）"},
		"デプロイブランチの context の印が deploy":     {setDeployEnv, marked("deploy"), "`# branch-set: deploy-env` にします（実際: deploy）"},
		"集合で表せない ruleset の context の印":     {setNone, marked(setGate), "印の集合で表せません"},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := findSet(t, "ci.yaml", c.set, c.src)
			require.Len(t, got, 1)
			assert.Equal(t, 3, got[0].Line)
			assert.Contains(t, got[0].Message, c.want)
		})
	}

	for name, c := range map[string]struct {
		src  string
		line int
		want string
	}{
		"reusable workflow を呼ぶ job":     {"on:\n  pull_request:\njobs:\n  build:\n    uses: ./.github/workflows/x.yaml\n", 5, "reusable workflow を呼んでいます"},
		"workflow_call を持つ workflow":    {"on:\n  pull_request:\n  workflow_call:\njobs:\n  build:\n    runs-on: x\n", 3, "workflow_call を持っています"},
		"job の name:":                   {"on:\n  pull_request:\njobs:\n  build:\n    name: Build\n    runs-on: x\n", 5, "name: を置かないでください"},
		"pull_request が無い":              {"on:\n  push:\njobs:\n  build:\n    runs-on: x\n", 1, "pull_request トリガーがありません"},
		"paths":                         {"on:\n  pull_request:\n    paths:\n      - x\njobs:\n  build:\n    runs-on: x\n", 3, "`paths` が残っています"},
		"paths-ignore":                  {"on:\n  pull_request:\n    paths-ignore:\n      - x\njobs:\n  build:\n    runs-on: x\n", 3, "`paths-ignore` が残っています"},
		"branches-ignore は印があっても":       {"on:\n  pull_request:\n    branches-ignore: # branch-set: deploy\n      - x\njobs:\n  build:\n    runs-on: x\n", 3, "`branches-ignore` が残っています"},
		"印の無い branches":                 {"on:\n  pull_request:\n    branches:\n      - main\njobs:\n  build:\n    runs-on: x\n", 3, "印 `# branch-set: <集合>`"},
		"印がほかの行にある branches":            {"on:\n  pull_request:\n    branches:\n      - main # branch-set: deploy\njobs:\n  build:\n    runs-on: x\n", 3, "印 `# branch-set: <集合>`"},
		"pull_request の値が別名":            {"x-pr: &pr\n  paths: [x]\non:\n  pull_request: *pr\njobs:\n  build:\n    runs-on: x\n", 4, "pull_request の値を読めません"},
		"pull_request の値が空でないスカラー":      {"on:\n  pull_request: x\njobs:\n  build:\n    runs-on: x\n", 2, "pull_request の値を読めません"},
		"pull_request のマージキー":           {"x-pr: &pr\n  paths: [x]\non:\n  pull_request:\n    <<: *pr\njobs:\n  build:\n    runs-on: x\n", 5, "マージキーと別名は使えません"},
		"pull_request の branches の値が別名": {"x-b: &b\n  - main\non:\n  pull_request:\n    branches: *b # branch-set: deploy\njobs:\n  build:\n    runs-on: x\n", 5, "マージキーと別名は使えません"},
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
		require.Len(t, got, 2)
		assert.Contains(t, got[0].Message, "reusable workflow を呼んでいます")
		assert.Contains(t, got[1].Message, "name: を置かないでください")
	})
}

func Test_gateWorkflowFindings(t *testing.T) {
	t.Parallel()

	t.Run("call-* と判定の job が宣言と一致すれば違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", prGateSrc), "checks.json", nil))
	})

	for name, c := range map[string]struct {
		src  string
		want []string
	}{
		"call-* が0件": {
			withoutJob(withoutJob(prGateSrc, "call-a"), "call-b"),
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
			withoutJob(prGateSrc, "gate"),
			[]string{"job `gate` は 1 件必要です（実際: 0）"},
		},
		"判定の job が2件": {
			prGateSrc + jobOf(prGateSrc, "gate"),
			[]string{"job `gate` は 1 件必要です（実際: 2）"},
		},
		"changes の job が無い": {
			withoutJob(prGateSrc, "changes"),
			[]string{"job `changes` は 1 件必要です（実際: 0）"},
		},
		"changes の job が2件": {
			strings.Replace(prGateSrc, "  call-a:\n", jobOf(prGateSrc, "changes")+"  call-a:\n", 1),
			[]string{"job `changes` は 1 件必要です（実際: 2）"},
		},
		"changes の step の run: が違う": {
			strings.Replace(prGateSrc, "run: make ci-changes RUNNER_MODE=host", "run: make ci-changes RUNNER_MODE=host || true", 1),
			[]string{"job `changes` の判定の step の run: は `make ci-changes RUNNER_MODE=host` にします"},
		},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", c.src), "checks.json", nil)
			require.Len(t, got, len(c.want))
			for i, w := range c.want {
				assert.Contains(t, got[i].Message, w)
			}
		})
	}

	t.Run("同じ id の call-* が2つあれば件数の差として現れる", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(prGateSrc, "  gate:\n", jobOf(prGateSrc, "call-a")+"  gate:\n", 1)
		got := gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", src), "checks.json", nil)
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "call-* の job は 3 件、checks.json の検査は 2 件です")
	})

	t.Run("判定の job の needs は fixed と call-* を合わせたものと突き合わせる", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(prGateSrc, "[changes, call-a, call-b]", "[call-a, call-b]", 1)
		got := gateWorkflowFindings(gateSpecAB(), parsed(t, "pr-gate.yaml", src), "checks.json", nil)
		assert.Contains(t, messages(got), "needs: に `changes` がありません")
	})
}

func Test_rulesetSet(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		file, want string
	}{
		"線の ruleset は gate":              {"r/lines.json", setGate},
		"デプロイブランチの ruleset は deploy-env": {"r/deploy-admin.json", setDeployEnv},
		"common の ruleset は集合を持たない":      {"r/common.json", setNone},
		"古い単一の ruleset は集合を持たない":         {"branch-protection.json", setNone},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, rulesetSet(c.file))
		})
	}
}

func Test_linesRulesetFindings(t *testing.T) {
	t.Parallel()

	in := func(contexts ...requiredchecks.Context) gateInput {
		return gateInput{contexts: contexts, rulesetsDir: "r"}
	}

	t.Run("線の ruleset の required が gate だけなら違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, linesRulesetFindings(in(requiredchecks.Context{Name: "gate", File: "r/lines.json"}, requiredchecks.Context{Name: "x", File: "r/deploy-a.json"})))
	})

	for name, contexts := range map[string][]requiredchecks.Context{
		"線の ruleset に required が無い": {{Name: "x", File: "r/deploy-a.json"}},
		"gate でない context":          {{Name: "line-gate", File: "r/lines.json"}},
		"gate のほかに context がある":     {{Name: "gate", File: "r/lines.json"}, {Name: "x", File: "r/lines.json"}},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := linesRulesetFindings(in(contexts...))
			require.Len(t, got, 1)
			assert.Equal(t, filepath.Join("r", linesRulesetFile), got[0].File)
		})
	}
}

func Test_callerIf(t *testing.T) {
	t.Parallel()

	t.Run("取り消しでなければ、changes が success でないか run に名前が在るときに走る式を返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "!cancelled() && (needs.changes.result != 'success' || contains(fromJSON(needs.changes.outputs.run), 'go-lint'))", callerIf("go-lint"))
	})
}

func Test_unwrapExpr(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		in, want string
	}{
		"前後の空白を除く":         {"  always()  ", "always()"},
		"全体を囲む ${{ }} を除く": {"${{ always() }}", "always()"},
		"囲みの内側の空白が無くても除く":  {" ${{a}} ", "a"},
		"空の値は空のまま":         {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, unwrapExpr(c.in))
		})
	}

	t.Run("2つの式を繋いだ値は、どちらか一方の式に縮めない", func(t *testing.T) {
		t.Parallel()
		got := unwrapExpr("${{ always() }} && ${{ failure() }}")
		assert.NotEqual(t, "always()", got)
		assert.NotEqual(t, "failure()", got)
	})
}

func Test_callerFindings(t *testing.T) {
	t.Parallel()

	gate := gateSpecAB()
	line := gateSpec{file: lineVerifyFile, job: lineGateJob, run: lineRun}
	c := declCheck("a", "a.yaml", checksdecl.ScopeTree, true)
	called := parsed(t, "a.yaml", "on:\n  workflow_call:\n    secrets:\n      TOKEN:\n        required: false\njobs:\n  a:\n    runs-on: x\n")
	ifA := "    if: ${{ !cancelled() && (needs.changes.result != 'success' || contains(fromJSON(needs.changes.outputs.run), 'a')) }}\n"
	job := func(t *testing.T, body string) wfJob {
		t.Helper()
		return parsed(t, "g.yaml", "on: push\njobs:\n  call-a:\n    uses: ./.github/workflows/a.yaml\n"+body).jobs[0]
	}

	for name, cs := range map[string]struct {
		spec gateSpec
		body string
	}{
		"gate の形":                 {gate, "    needs: changes\n" + ifA},
		"needs が配列の1要素":           {gate, "    needs: [changes]\n" + ifA},
		"宣言された secret を名指して渡す":    {gate, "    needs: changes\n" + ifA + "    secrets:\n      TOKEN: ${{ secrets.TOKEN }}\n"},
		"線の検証は needs も if: も持たない": {line, ""},
	} {
		t.Run(name+"なら違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, callerFindings(cs.spec, "g.yaml", job(t, cs.body), c, called))
		})
	}

	for name, cs := range map[string]struct {
		spec gateSpec
		body string
		want []string
	}{
		"with: を持つ":                {gate, "    needs: changes\n" + ifA + "    with:\n      x: y\n", []string{"with: を置けません"}},
		"secrets: inherit":         {gate, "    needs: changes\n" + ifA + "    secrets: inherit\n", []string{"inherit は使えません"}},
		"宣言の無い secret を渡す":         {gate, "    needs: changes\n" + ifA + "    secrets:\n      OTHER: ${{ secrets.OTHER }}\n", []string{"`OTHER` は、呼ばれる workflow の workflow_call.secrets に宣言がありません"}},
		"secret に式でない値を渡す":         {gate, "    needs: changes\n" + ifA + "    secrets:\n      TOKEN: literal\n", []string{"`TOKEN` の値は `${{ secrets.<名前> }}` にします"}},
		"secret に加工した値を渡す":         {gate, "    needs: changes\n" + ifA + "    secrets:\n      TOKEN: ${{ secrets.TOKEN || 'x' }}\n", []string{"`TOKEN` の値は"}},
		"if: が無い":                  {gate, "    needs: changes\n", []string{"if: がありません"}},
		"if: が別の検査の名前を見る":          {gate, "    needs: changes\n" + strings.Replace(ifA, "'a'", "'b'", 1), []string{"if: は `${{ !cancelled()"}},
		"if: が fail open しない":      {gate, "    needs: changes\n    if: ${{ contains(fromJSON(needs.changes.outputs.run), 'a') }}\n", []string{"if: は `${{ !cancelled()"}},
		"if: に条件を足す":               {gate, "    needs: changes\n" + strings.Replace(ifA, " }}", " && false }}", 1), []string{"if: は `${{ !cancelled()"}},
		"needs が無い":                {gate, ifA, []string{"needs: がありません"}},
		"needs がほかの job も持つ":       {gate, "    needs: [changes, call-b]\n" + ifA, []string{"needs: は `changes` だけにします"}},
		"needs が別の job":            {gate, "    needs: call-b\n" + ifA, []string{"needs: は `changes` だけにします"}},
		"線の検証の call-* が if: を持つ":   {line, "    if: always()\n", []string{"線の検証は検査を path で絞らず"}},
		"線の検証の call-* が with: を持つ": {line, "    with:\n      x: y\n", []string{"with: を置けません"}},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := callerFindings(cs.spec, "g.yaml", job(t, cs.body), c, called)
			require.Len(t, got, len(cs.want))
			for i, w := range cs.want {
				assert.Equal(t, "g.yaml", got[i].File)
				assert.Contains(t, got[i].Message, "job `call-a` ")
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func Test_secretsFindings(t *testing.T) {
	t.Parallel()

	called := parsed(t, "a.yaml", "on:\n  workflow_call:\n    secrets:\n      TOKEN:\n        required: false\njobs:\n  a:\n    runs-on: x\n")
	find := func(t *testing.T, value string, called wfDoc) []lintreport.Finding {
		t.Helper()
		m := yamlNode(t, "secrets: "+value)
		return secretsFindings("call-a", "g.yaml", m.Content[0], m.Content[1], called)
	}

	t.Run("宣言された名前へ secret を1つずつ渡すなら違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, find(t, "{TOKEN: '${{ secrets.TOKEN }}'}", called))
		assert.Empty(t, find(t, "{TOKEN: '${{ secrets.OTHER_NAME }}'}", called))
		assert.Empty(t, find(t, "{}", called))
	})

	for name, c := range map[string]struct {
		value  string
		called wfDoc
		want   []string
	}{
		"inherit": {"inherit", called, []string{"inherit は使えません"}},
		"配列":      {"[TOKEN]", called, []string{"mapping にします"}},
		"呼ばれる側に workflow_call.secrets が無い": {"{TOKEN: '${{ secrets.TOKEN }}'}", parsed(t, "b.yaml", "on:\n  workflow_call:\njobs:\n  b:\n    runs-on: x\n"), []string{"宣言がありません"}},
		"呼ばれる workflow が無い":                {"{TOKEN: '${{ secrets.TOKEN }}'}", wfDoc{}, []string{"宣言がありません"}},
		"値が mapping":                       {"{TOKEN: {a: b}}", called, []string{"の値は"}},
		"値が github の context":              {"{TOKEN: '${{ github.token }}'}", called, []string{"の値は"}},
		"宣言の無い名前と式でない値":                    {"{X: y}", called, []string{"`X` は、呼ばれる workflow", "`X` の値は"}},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := find(t, c.value, c.called)
			require.Len(t, got, len(c.want))
			for i, w := range c.want {
				assert.Contains(t, got[i].Message, "job `call-a` の secrets: ")
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
}

func Test_stepFindings(t *testing.T) {
	t.Parallel()

	judgeStep := "      - env:\n          NEEDS: ${{ toJSON(needs) }}\n        run: make ci-gate RUNNER_MODE=host\n"
	job := func(t *testing.T, body string) wfJob {
		t.Helper()
		return parsed(t, "g.yaml", "on: push\njobs:\n  gate:\n    runs-on: x\n"+body).jobs[0]
	}
	steps := func(extra ...string) string { return "    steps:\n" + strings.Join(extra, "") }

	for name, c := range map[string]struct {
		body  string
		run   string
		needs bool
	}{
		"判定の step が1つ":                      {steps("      - uses: actions/checkout@x\n", judgeStep), gateRun, true},
		"ほかの step は continue-on-error を持てる": {steps("      - continue-on-error: true\n        run: sleep 1\n", judgeStep), gateRun, true},
		"run: の前後の空白は字面の差にしない":              {steps("      - env:\n          NEEDS: ${{ toJSON(needs) }}\n        run: |\n          make ci-gate RUNNER_MODE=host\n"), gateRun, true},
		"needs を課さない changes の step":        {steps("      - id: changes\n        run: make ci-changes RUNNER_MODE=host\n"), changesRun, false},
	} {
		t.Run(name+"なら違反0件", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, stepFindings("gate", "g.yaml", job(t, c.body), c.run, c.needs))
		})
	}

	for name, c := range map[string]struct {
		body string
		want []string
	}{
		"steps が無い":                    {"", []string{"steps の一覧がありません"}},
		"steps が一覧でない":                 {"    steps: x\n", []string{"steps の一覧がありません"}},
		"判定の step が無い":                 {steps("      - run: echo ok\n"), []string{"（実際: 0）"}},
		"判定の step が2つ":                 {steps(judgeStep, judgeStep), []string{"（実際: 2）"}},
		"別の入口を起動する step が並ぶ":           {steps(judgeStep, "      - run: make ci-changes RUNNER_MODE=host\n"), []string{"（実際: 2）"}},
		"|| で失敗を打ち消す":                  {steps(strings.Replace(judgeStep, "RUNNER_MODE=host", "RUNNER_MODE=host || true", 1)), []string{"run: は `make ci-gate RUNNER_MODE=host` にします"}},
		"; で後続を足す":                     {steps(strings.Replace(judgeStep, "RUNNER_MODE=host", "RUNNER_MODE=host; exit 0", 1)), []string{"run: は `make ci-gate RUNNER_MODE=host` にします"}},
		"別の入口を判定に使う":                   {steps(strings.Replace(judgeStep, "ci-gate", "ci-line-gate", 1)), []string{"run: は `make ci-gate RUNNER_MODE=host` にします"}},
		"判定の step が continue-on-error": {steps("      - continue-on-error: true\n        env:\n          NEEDS: ${{ toJSON(needs) }}\n        run: make ci-gate RUNNER_MODE=host\n"), []string{"continue-on-error: を置けません"}},
		"判定の step が if:":               {steps("      - if: false\n        env:\n          NEEDS: ${{ toJSON(needs) }}\n        run: make ci-gate RUNNER_MODE=host\n"), []string{"if: を置けません"}},
		"job が continue-on-error":      {"    continue-on-error: true\n" + steps(judgeStep), []string{"job `gate` に continue-on-error を置けません"}},
		"NEEDS が無い":                    {steps("      - run: make ci-gate RUNNER_MODE=host\n"), []string{"NEEDS は `${{ toJSON(needs) }}` にします"}},
		"NEEDS が別の値":                   {steps(strings.Replace(judgeStep, "toJSON(needs)", "'{}'", 1)), []string{"NEEDS は `${{ toJSON(needs) }}` にします"}},
	} {
		t.Run(name+"なら違反", func(t *testing.T) {
			t.Parallel()
			got := stepFindings("gate", "g.yaml", job(t, c.body), gateRun, true)
			require.Len(t, got, len(c.want), messages(got))
			for i, w := range c.want {
				assert.Equal(t, "g.yaml", got[i].File)
				assert.Contains(t, got[i].Message, w)
			}
		})
	}
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

	for name, c := range map[string]struct {
		src  string
		want bool
	}{
		"always() そのもの":              {"always()", true},
		"引用符の中の前後の空白は問わない":           {"' always() '", true},
		"${{ }} で囲んだ always()":       {"${{ always() }}", true},
		"空白の無い ${{ }} の always()":    {"${{always()}}", true},
		"二重引用符の中の ${{ }} の always()": {"\"${{ always() }} \"", true},
		"別の関数":                      {"success()", false},
		"always() に条件を足す":           {"always() && true", false},
		"${{ always() }} の後ろに条件を足す": {"${{ always() }} && true", false},
		"${{ }} の中で条件を足す":           {"${{ always() || failure() }}", false},
		"配列":                        {"[always()]", false},
		"取り消しだけを除く式":                {"${{ !cancelled() }}", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, isAlways(yamlNode(t, "v: "+c.src).Content[1]))
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
		"push の値が null":  "on:\n  push: ~\n",
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
		"push の値が別名":              {"x-push: &p\n  paths: [x]\non:\n  push: *p\n", []string{"push の値を読めません"}},
		"push の値が空でないスカラー":        {"on:\n  push: x\n", []string{"push の値を読めません"}},
		"push のマージキー":             {"x-push: &p\n  paths: [x]\non:\n  push:\n    <<: *p\n", []string{"push の項目を読めません"}},
		"push の branches の値が別名":   {"x-b: &b\n  - main\non:\n  push:\n    branches: *b\n", []string{"push の項目を読めません"}},
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
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "pull_request で起動します")
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
		"索引記法の workflow_sha":     "${{ github['workflow_sha'] }}",
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
		"単一引用符の索引記法":                        {"${{ github['workflow'] }}", "github.workflow を含みます"},
		"二重引用符の索引記法":                        {"'${{ github[\"workflow\"] }}'", "github.workflow を含みます"},
		"空白を挟む索引記法の workflow_ref":           {"${{ github [ 'workflow_ref' ] }}", "github.workflow を含みます"},
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
