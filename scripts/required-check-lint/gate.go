package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// gate と線の検証の workflow の形（ADR-0603 決定17・19・37・38）。job の id は scripts/ci-gate が needs の key として
// 読む名前で、caller の接頭辞は checksdecl.CallerPrefix が持つ。
const (
	prGateFile          = "pr-gate.yaml"
	lineVerifyFile      = "line-verify.yaml"
	changesJob          = "changes"
	gateJob             = "gate"
	lineGateJob         = "line-gate"
	localWorkflowPrefix = "./.github/workflows/"
)

var (
	// branchSetMarker は、repo-settings が branches.json から生成する行の印（行末のコメント）。
	branchSetMarker = regexp.MustCompile(`#\s*branch-set:\s*\S+\s*$`)
	// callerWorkflowRef は、呼ばれた workflow の中では caller の workflow を指す context。Actions の式は大文字小文字を
	// 区別しない。
	callerWorkflowRef = regexp.MustCompile(`(?i)\bgithub\.workflow(?:_ref)?\b`)
)

// gateInput は、gate と線の検証の検査の入力。どれも宣言から読んだもので、検査が独自に持つ一覧は無い（決定20）。
type gateInput struct {
	contexts     []requiredchecks.Context
	legacy       []string
	decl         checksdecl.Decl
	checksPath   string
	workflowsDir string
}

// wfDoc は、YAML として読んだ workflow 1件。
type wfDoc struct {
	file   string
	lines  []string
	root   *yaml.Node
	events []wfEvent
	jobs   []wfJob
}

// wfEvent は on: のイベント1つ。value はマッピング記法のときのイベントの値で、ほかの記法では nil。
type wfEvent struct {
	name  string
	line  int
	value *yaml.Node
}

// wfJob は jobs: の直下の job 1つ。
type wfJob struct {
	id   string
	line int
	node *yaml.Node
}

// gateSpec は、宣言の検査を call-<name> の job で呼び、1つの job で判定する workflow の形。
type gateSpec struct {
	file   string
	job    string
	fixed  []string
	checks []checksdecl.Check
}

// gateFindings は、ruleset の宣言・検査の宣言・workflow を突き合わせ、決定16・19・38 の不変条件の違反を返します。
// 解釈できない workflow は違反として返し、そこから先の検査の対象にしない。
func gateFindings(in gateInput, sources []Source) []lintreport.Finding {
	var (
		findings []lintreport.Finding
		docs     []wfDoc
	)
	byName := map[string]wfDoc{}
	failed := map[string]bool{}
	for _, s := range sources {
		d, f := parseWorkflow(s)
		if f != nil {
			findings = append(findings, *f)
			failed[filepath.Base(s.File)] = true
			continue
		}
		docs = append(docs, d)
		byName[filepath.Base(d.file)] = d
	}

	findings = append(findings, directFindings(in.contexts, docs)...)

	for _, g := range []gateSpec{
		{file: prGateFile, job: gateJob, fixed: []string{changesJob}, checks: in.decl.Checks},
		{file: lineVerifyFile, job: lineGateJob, checks: treeChecks(in.decl)},
	} {
		d, ok := byName[g.file]
		switch {
		case failed[g.file]:
		case !ok:
			findings = append(findings, lintreport.Finding{
				File: filepath.Join(in.workflowsDir, g.file), Line: 1,
				Message: fmt.Sprintf("%s がありません（job `%s` が %s の検査を呼びます）", g.file, g.job, in.checksPath),
			})
		default:
			findings = append(findings, gateWorkflowFindings(g, d, in.checksPath)...)
		}
	}
	if d, ok := byName[lineVerifyFile]; ok {
		findings = append(findings, lineVerifyTriggerFindings(d)...)
	}

	findings = append(findings, calledFindings(in, byName, failed)...)

	contexts := map[string]bool{}
	for _, c := range in.contexts {
		contexts[c.Name] = true
	}
	for _, c := range in.legacy {
		contexts[c] = true
	}
	findings = append(findings, collisionFindings(docs, contexts)...)

	return findings
}

// parseWorkflow は workflow を YAML として読みます。読めないときは違反1件を返します。
func parseWorkflow(src Source) (wfDoc, *lintreport.Finding) {
	fail := func(line int, format string, args ...any) (wfDoc, *lintreport.Finding) {
		return wfDoc{}, &lintreport.Finding{File: src.File, Line: line, Message: "workflow を解釈できません: " + fmt.Sprintf(format, args...)}
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src.Source), &doc); err != nil {
		return fail(1, "%v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fail(1, "最上位が mapping ではありません")
	}
	root := doc.Content[0]

	onKey, on := lookup(root, "on")
	if on == nil {
		return fail(1, "on: がありません")
	}
	events, err := readEvents(on)
	if err != nil {
		return fail(onKey.Line, "%v", err)
	}

	jobsKey, jobs := lookup(root, "jobs")
	if jobs == nil {
		return fail(1, "jobs: がありません")
	}
	if jobs.Kind != yaml.MappingNode {
		return fail(jobsKey.Line, "jobs: が mapping ではありません")
	}
	var out []wfJob
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		k, v := jobs.Content[i], jobs.Content[i+1]
		if k.Kind != yaml.ScalarNode || v.Kind != yaml.MappingNode {
			return fail(k.Line, "job が「id: mapping」の形ではありません")
		}
		out = append(out, wfJob{id: k.Value, line: k.Line, node: v})
	}

	return wfDoc{file: src.File, lines: strings.Split(src.Source, "\n"), root: root, events: events, jobs: out}, nil
}

// lookup は mapping の key の node と値の node を返します。無ければ両方 nil です。
func lookup(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k := m.Content[i]; k.Kind == yaml.ScalarNode && k.Value == key {
			return k, m.Content[i+1]
		}
	}
	return nil, nil
}

// readEvents は on: の単一値・配列・マッピングの3つの記法を読みます。
func readEvents(on *yaml.Node) ([]wfEvent, error) {
	switch on.Kind {
	case yaml.ScalarNode:
		return []wfEvent{{name: on.Value, line: on.Line}}, nil
	case yaml.SequenceNode:
		out := make([]wfEvent, 0, len(on.Content))
		for _, e := range on.Content {
			if e.Kind != yaml.ScalarNode {
				return nil, xerrors.Newf("on: の配列の %d 行目がイベント名ではありません", e.Line)
			}
			out = append(out, wfEvent{name: e.Value, line: e.Line})
		}
		return out, nil
	case yaml.MappingNode:
		out := make([]wfEvent, 0, len(on.Content)/2)
		for i := 0; i+1 < len(on.Content); i += 2 {
			k := on.Content[i]
			if k.Kind != yaml.ScalarNode {
				return nil, xerrors.Newf("on: の %d 行目のキーがイベント名ではありません", k.Line)
			}
			out = append(out, wfEvent{name: k.Value, line: k.Line, value: on.Content[i+1]})
		}
		return out, nil
	}
	return nil, xerrors.New("on: の値が単一値・配列・マッピングのどれでもありません")
}

// event は、名前が name のイベントを返します。
func (d wfDoc) event(name string) (wfEvent, bool) {
	for _, e := range d.events {
		if e.name == name {
			return e, true
		}
	}
	return wfEvent{}, false
}

// get は、job の直下の key の node と値の node を返します。
func (j wfJob) get(key string) (*yaml.Node, *yaml.Node) { return lookup(j.node, key) }

// treeChecks は、線の検証が呼ぶ検査を宣言の順に返します。どれを呼ぶかは checksdecl の TreeChecks が決めます。
func treeChecks(d checksdecl.Decl) []checksdecl.Check {
	names := map[string]bool{}
	for _, n := range d.TreeChecks() {
		names[n] = true
	}
	var out []checksdecl.Check
	for _, c := range d.Checks {
		if names[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

// directFindings は、ruleset が直接 required とする各 context について、報告する job がちょうど1つ在り、その job と
// workflow が context を取りこぼさない形であることを検査します（決定16・19）。同じ context を複数の ruleset が
// 名指すときは、最初に名指したファイルへ報告する。
func directFindings(contexts []requiredchecks.Context, docs []wfDoc) []lintreport.Finding {
	var findings []lintreport.Finding
	seen := map[string]bool{}
	for _, c := range contexts {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true

		n := 0
		for _, d := range docs {
			for _, j := range d.jobs {
				if j.id != c.Name {
					continue
				}
				n++
				findings = append(findings, directJobFindings(c.Name, d, j)...)
			}
		}
		if n != 1 {
			findings = append(findings, lintreport.Finding{
				File: c.File, Line: 1,
				Message: fmt.Sprintf("required context `%s` を報告する job は 1 件必要です（実際: %d）", c.Name, n),
			})
		}
	}
	return findings
}

// directJobFindings は、直接 required の context を報告する job 1つを検査します。線の検証の workflow の起動条件は
// [lineVerifyTriggerFindings] が見る。
func directJobFindings(context string, d wfDoc, j wfJob) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{
			File: d.file, Line: line,
			Message: fmt.Sprintf("required context `%s` ", context) + fmt.Sprintf(format, args...),
		})
	}

	if k, _ := j.get("uses"); k != nil {
		add(k.Line, "を報告する job が reusable workflow を呼んでいます。報告される context が `<caller> / <job>` になります")
	}
	if e, ok := d.event("workflow_call"); ok {
		add(e.line, "の workflow が workflow_call を持っています。呼ばれた job は caller の名前を前に付けて報告されます")
	}
	if k, _ := j.get("name"); k != nil {
		add(k.Line, "を報告する job に name: を置かないでください。報告される context が job の ID と違う名前になります")
	}

	if filepath.Base(d.file) == lineVerifyFile {
		return findings
	}
	pr, ok := d.event("pull_request")
	if !ok {
		add(1, "の workflow に pull_request トリガーがありません")
		return findings
	}
	if pr.value == nil || pr.value.Kind != yaml.MappingNode {
		return findings
	}
	for i := 0; i+1 < len(pr.value.Content); i += 2 {
		k := pr.value.Content[i]
		switch k.Value {
		case "paths", "paths-ignore", "branches-ignore":
			add(k.Line, "の pull_request に `%s` が残っています（起動しなかった check は未報告のままマージを止めます）", k.Value)
		case "branches":
			if k.Line < 1 || k.Line > len(d.lines) || !branchSetMarker.MatchString(d.lines[k.Line-1]) {
				add(k.Line, "の pull_request の branches が、印 `# branch-set: <集合>` で branches.json から生成した一覧ではありません")
			}
		}
	}
	return findings
}

// gateWorkflowFindings は、gate の workflow の call-* の job の集合が宣言の検査と両方向で一致し、判定の job が
// それらをすべて受け取り、取り消されても走る形であることを検査します（決定17-19・37）。
func gateWorkflowFindings(s gateSpec, d wfDoc, checksPath string) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: d.file, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	want := make(map[string]string, len(s.checks))
	wantIDs := make([]string, 0, len(s.checks))
	for _, c := range s.checks {
		id := checksdecl.CallerPrefix + c.Name
		want[id] = c.Workflow
		wantIDs = append(wantIDs, id)
	}

	got := map[string]bool{}
	callers := 0
	var judges []wfJob
	for _, j := range d.jobs {
		switch {
		case j.id == s.job:
			judges = append(judges, j)
		case strings.HasPrefix(j.id, checksdecl.CallerPrefix):
			callers++
			wf, ok := want[j.id]
			if !ok {
				add(j.line, "job `%s` は %s に無い検査を呼んでいます", j.id, checksPath)
				continue
			}
			got[j.id] = true
			_, uses := j.get("uses")
			if exp := localWorkflowPrefix + wf; uses == nil || uses.Kind != yaml.ScalarNode || uses.Value != exp {
				add(j.line, "job `%s` の uses: は `%s` にします", j.id, exp)
			}
		}
	}
	for _, id := range wantIDs {
		if !got[id] {
			add(1, "%s の検査 `%s` を呼ぶ job `%s` がありません", checksPath, strings.TrimPrefix(id, checksdecl.CallerPrefix), id)
		}
	}
	if callers != len(wantIDs) {
		add(1, "%s* の job は %d 件、%s の検査は %d 件です", checksdecl.CallerPrefix, callers, checksPath, len(wantIDs))
	}

	if len(judges) != 1 {
		add(1, "job `%s` は 1 件必要です（実際: %d）", s.job, len(judges))
		return findings
	}
	return append(findings, judgeFindings(s.job, d.file, judges[0], append(append([]string{}, s.fixed...), wantIDs...))...)
}

// judgeFindings は、判定の job の needs が want と両方向で一致し、if: が always() であることを検査します。
func judgeFindings(id, file string, j wfJob, want []string) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf("job `%s` ", id) + fmt.Sprintf(format, args...)})
	}

	if k, v := j.get("if"); k == nil {
		add(j.line, "に if: がありません。取り消されても走るよう always() にします")
	} else if !isAlways(v) {
		add(k.Line, "の if: は always() にします。ほかの条件は、取り消しや失敗のときに判定を skip させます")
	}

	k, v := j.get("needs")
	if k == nil {
		add(j.line, "に needs: がありません")
		return findings
	}
	needs, ok := needsOf(v)
	if !ok {
		add(k.Line, "の needs: を job の id の一覧として読めません")
		return findings
	}
	have := map[string]bool{}
	for _, n := range needs {
		have[n] = true
	}
	wanted := map[string]bool{}
	for _, w := range want {
		wanted[w] = true
		if !have[w] {
			add(k.Line, "の needs: に `%s` がありません", w)
		}
	}
	for _, n := range needs {
		if !wanted[n] {
			add(k.Line, "の needs: の `%s` は宣言から導かれない job です", n)
		}
	}
	if len(needs) != len(want) {
		add(k.Line, "の needs: は %d 件、宣言から導かれる job は %d 件です", len(needs), len(want))
	}
	return findings
}

// isAlways は、if: の値が always() か ${{ always() }} であるかを返します。
func isAlways(v *yaml.Node) bool {
	if v == nil || v.Kind != yaml.ScalarNode {
		return false
	}
	s := strings.TrimSpace(v.Value)
	if strings.HasPrefix(s, "${{") && strings.HasSuffix(s, "}}") {
		s = strings.TrimSpace(s[3 : len(s)-2])
	}
	return s == "always()"
}

// needsOf は needs: の単一値と配列を読みます。どちらでもないか、要素がスカラーでなければ false を返します。
func needsOf(v *yaml.Node) ([]string, bool) {
	switch v.Kind {
	case yaml.ScalarNode:
		return []string{v.Value}, true
	case yaml.SequenceNode:
		out := make([]string, 0, len(v.Content))
		for _, e := range v.Content {
			if e.Kind != yaml.ScalarNode {
				return nil, false
			}
			out = append(out, e.Value)
		}
		return out, true
	}
	return nil, false
}

// lineVerifyTriggerFindings は、線の検証の workflow が push だけで起動し、push を path で絞らないことを検査します
// （決定37・38）。ほかのイベントで起動すると、skip された判定の job の check が同じ commit の成功として数えられる。
func lineVerifyTriggerFindings(d wfDoc) []lintreport.Finding {
	var findings []lintreport.Finding
	push := false
	for _, e := range d.events {
		if e.name != "push" {
			findings = append(findings, lintreport.Finding{
				File: d.file, Line: e.line,
				Message: fmt.Sprintf("%s は push のほかのイベント（%s）で起動できません。job `%s` の名前が別のイベントの run に現れます", lineVerifyFile, e.name, lineGateJob),
			})
			continue
		}
		push = true
		if e.value == nil || e.value.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(e.value.Content); i += 2 {
			if k := e.value.Content[i]; k.Value == "paths" || k.Value == "paths-ignore" {
				findings = append(findings, lintreport.Finding{
					File: d.file, Line: k.Line,
					Message: fmt.Sprintf("%s の push に `%s` があります。線の検証は path で絞りません", lineVerifyFile, k.Value),
				})
			}
		}
	}
	if !push {
		findings = append(findings, lintreport.Finding{File: d.file, Line: 1, Message: lineVerifyFile + " に push トリガーがありません"})
	}
	return findings
}

// calledFindings は、検査の宣言が名指す各 workflow を、宣言の順に1度ずつ検査します。古い単一の ruleset の宣言が
// 在る間は、その context を報告するために pull_request で起動し if: で skip する形が残るので、その2つを課さない。
func calledFindings(in gateInput, byName map[string]wfDoc, failed map[string]bool) []lintreport.Finding {
	var findings []lintreport.Finding
	final := in.legacy == nil
	seen := map[string]bool{}
	for _, c := range in.decl.Checks {
		if seen[c.Workflow] || failed[c.Workflow] {
			continue
		}
		seen[c.Workflow] = true
		d, ok := byName[c.Workflow]
		if !ok {
			findings = append(findings, lintreport.Finding{
				File: in.checksPath, Line: 1,
				Message: fmt.Sprintf("検査 `%s` の workflow `%s` が %s にありません", c.Name, c.Workflow, in.workflowsDir),
			})
			continue
		}
		findings = append(findings, calledWorkflowFindings(d, final)...)
	}
	return findings
}

// calledWorkflowFindings は、gate が呼ぶ workflow 1つを検査します（決定16・17・19）。final は古い単一の ruleset の
// 宣言が無いことで、そのとき pull_request での起動と job の if: も違反にする。
func calledWorkflowFindings(d wfDoc, final bool) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: d.file, Line: line, Message: "gate が呼ぶ workflow " + fmt.Sprintf(format, args...)})
	}

	if _, ok := d.event("workflow_call"); !ok {
		add(1, "に workflow_call トリガーがありません")
	}
	if k, v := lookup(d.root, "concurrency"); k != nil {
		findings = append(findings, concurrencyFindings(d.file, k, v, "workflow")...)
	}
	for _, j := range d.jobs {
		if k, v := j.get("concurrency"); k != nil {
			findings = append(findings, concurrencyFindings(d.file, k, v, fmt.Sprintf("job `%s`", j.id))...)
		}
	}

	if !final {
		return findings
	}
	if e, ok := d.event("pull_request"); ok {
		add(e.line, "が pull_request で起動します。走らせるかどうかは gate だけが決めます")
	}
	for _, j := range d.jobs {
		if k, _ := j.get("if"); k != nil {
			add(k.Line, "の job `%s` が if: を持っています。gate が呼んだ検査は自身の条件で skip しません", j.id)
		}
	}
	return findings
}

// concurrencyFindings は、呼ばれる workflow の concurrency の group が caller の workflow を指す context を含まないことを
// 検査します。呼ばれた workflow の中では github.workflow が caller の名前になり、同じ gate が呼ぶ検査どうしが
// 同じ group で互いを取り消す。where は報告に使う場所の名前。
func concurrencyFindings(file string, key, value *yaml.Node, where string) []lintreport.Finding {
	group := value
	if value.Kind == yaml.MappingNode {
		_, group = lookup(value, "group")
	}
	switch {
	case group == nil:
		return nil
	case group.Kind != yaml.ScalarNode:
		return []lintreport.Finding{{File: file, Line: key.Line, Message: where + " の concurrency の group を読めません"}}
	case callerWorkflowRef.MatchString(group.Value):
		return []lintreport.Finding{{
			File: file, Line: group.Line,
			Message: where + " の concurrency の group が github.workflow を含みます。呼ばれると caller の名前になり、同じ gate が呼ぶ検査どうしが互いを取り消します",
		}}
	}
	return nil
}

// collisionFindings は、gate と線の検証の job の id がそれぞれの workflow の外に無く、call-* の job の id がどの
// ruleset の context とも一致しないことを検査します（決定38）。skip された job の check は、同じ名前の required の
// context を成功として満たす。
func collisionFindings(docs []wfDoc, contexts map[string]bool) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, d := range docs {
		base := filepath.Base(d.file)
		for _, j := range d.jobs {
			var msg string
			switch {
			case j.id == gateJob && base != prGateFile:
				msg = fmt.Sprintf("job の id `%s` は %s の外に置けません", gateJob, prGateFile)
			case j.id == lineGateJob && base != lineVerifyFile:
				msg = fmt.Sprintf("job の id `%s` は %s の外に置けません", lineGateJob, lineVerifyFile)
			case strings.HasPrefix(j.id, checksdecl.CallerPrefix) && contexts[j.id]:
				msg = fmt.Sprintf("job の id `%s` が ruleset の required context と同じ名前です", j.id)
			default:
				continue
			}
			findings = append(findings, lintreport.Finding{File: d.file, Line: j.line, Message: msg})
		}
	}
	return findings
}
