package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/harden"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
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

	// 判定と changes の step の run:。scripts/ci-gate を起動する make の入口（.makefiles/github/setting/repo-settings.mk）。
	gateRun    = "make ci-gate RUNNER_MODE=host"
	lineRun    = "make ci-line-gate RUNNER_MODE=host"
	changesRun = "make ci-changes RUNNER_MODE=host"
	// needsEnv は、判定の step が needs を受け取る env の値。
	needsEnv = "${{ toJSON(needs) }}"

	linesRulesetFile  = "lines.json"
	deployRulesetStem = "deploy-"
	// 直接 required の context を報告する workflow の pull_request の branches に置く印の集合（repo-settings が
	// branches.json から展開する）。線の ruleset の対象は gate、env を持つデプロイブランチの ruleset の対象を合わせた
	// ものは deploy-env である（決定16・35）。
	setGate      = "gate"
	setDeployEnv = "deploy-env"
)

var (
	// branchSetMarker は、repo-settings が branches.json から生成する行の印（行末のコメント）。
	branchSetMarker = regexp.MustCompile(`#\s*branch-set:\s*(\S+)\s*$`)
	// callerWorkflowRef は、呼ばれた workflow の中では caller の workflow を指す context。Actions の式は大文字小文字を
	// 区別せず、property は索引の記法（github['workflow']）でも書ける。
	callerWorkflowRef = regexp.MustCompile(`(?i)\bgithub\s*(?:\.\s*workflow(?:_ref)?\b|\[\s*['"]workflow(?:_ref)?['"]\s*\])`)
	// makeTarget は、run: のうち make の入口を起動する部分。判定の step を数えるのに使う。
	makeTarget = regexp.MustCompile(`\bmake\s+(ci-gate|ci-line-gate|ci-changes)\b`)
	// secretRef は、call-* の job の secrets: に置ける値。呼ぶ側の secret を1つ名指す。
	secretRef = regexp.MustCompile(`^\$\{\{\s*secrets\.[A-Za-z_][A-Za-z0-9_]*\s*\}\}$`)

	// errEvents は、on: の値からイベントを読めないことを表す。
	errEvents = xerrors.New("on: のイベントを読めません")

	// 判定と changes の step の形。env は、make の入口が読む変数だけを置ける。ほかの変数（MAKEFLAGS など）は make の
	// 振る舞いを替える。changes の step の id は、job の outputs が steps.changes を読むために要る。
	baseEnv     = []string{"BASE_SHA", "HEAD_SHA", "BASE_REF"}
	changesStep = stepSpec{run: changesRun, keys: []string{"name", "id", "env", "run"}, env: baseEnv}
	gateStep    = stepSpec{run: gateRun, keys: []string{"name", "env", "run"}, env: append([]string{"NEEDS"}, baseEnv...), needs: true}
	lineStep    = stepSpec{run: lineRun, keys: []string{"name", "env", "run"}, env: []string{"NEEDS"}, needs: true}
	// prGateTypes は、gate の workflow の pull_request の types に要るもの。gate の判定は base に依るので、base の
	// 付け替え（edited）でも走り直す。
	prGateTypes = []string{"opened", "synchronize", "reopened", "edited"}
)

// gateInput は、gate と線の検証の検査の入力。どれも宣言から読んだもので、検査が独自に持つ一覧は無い（決定20）。
type gateInput struct {
	contexts     []requiredchecks.Context
	legacy       []string
	decl         checksdecl.Decl
	checksPath   string
	workflowsDir string
	rulesetsDir  string
}

type wfDoc struct {
	file   string
	lines  []string
	root   *yaml.Node
	events []wfEvent
	jobs   []wfJob
}

// wfEvent は on: のイベント1つ。value はマッピング記法のときだけ入り、ほかの記法では nil。
type wfEvent struct {
	name  string
	line  int
	value *yaml.Node
}

type wfJob struct {
	id   string
	line int
	node *yaml.Node
}

// gateSpec は、宣言の検査を call-<name> の job で呼び、1つの job で判定する workflow の形。step は判定の step の形。
// gated は、call-* の job が changes の job の出力で走るかを決める（gate の workflow）ことを表す。types は、
// pull_request の types に要るもの。
type gateSpec struct {
	file   string
	job    string
	step   stepSpec
	fixed  []string
	checks []checksdecl.Check
	gated  bool
	types  []string
}

// stepSpec は、make の入口を起動する step の形。keys は step に置ける key、env は env に置ける key。needs が真なら、
// env の NEEDS に needs を渡すことを課す。
type stepSpec struct {
	run   string
	keys  []string
	env   []string
	needs bool
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

	findings = append(findings, directFindings(in.contexts, in.legacy, docs)...)
	findings = append(findings, linesRulesetFindings(in)...)

	for _, g := range []gateSpec{
		{file: prGateFile, job: gateJob, step: gateStep, fixed: []string{changesJob}, checks: in.decl.Checks, gated: true, types: prGateTypes},
		{file: lineVerifyFile, job: lineGateJob, step: lineStep, checks: treeChecks(in.decl)},
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
			findings = append(findings, gateWorkflowFindings(g, d, in.checksPath, byName)...)
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

// parseWorkflow は workflow を YAML として読みます。読めないときと、文書のどこかにマージキー・別名・スカラーでない
// キーがあるときは違反1件を返します。取り込まれた先のキーは、mapping を1段ずつ読む検査からは見えない。
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
	if line, what := opaqueNode(root); line != 0 {
		return fail(line, "%sは使えません", what)
	}

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

func readEvents(on *yaml.Node) ([]wfEvent, error) {
	switch on.Kind {
	case yaml.ScalarNode:
		return []wfEvent{{name: on.Value, line: on.Line}}, nil
	case yaml.SequenceNode:
		out := make([]wfEvent, 0, len(on.Content))
		for _, e := range on.Content {
			if e.Kind != yaml.ScalarNode {
				return nil, xerrors.Wrapf(errEvents, "on: の配列の %d 行目がイベント名ではありません", e.Line)
			}
			out = append(out, wfEvent{name: e.Value, line: e.Line})
		}
		return out, nil
	case yaml.MappingNode:
		out := make([]wfEvent, 0, len(on.Content)/2)
		for i := 0; i+1 < len(on.Content); i += 2 {
			k := on.Content[i]
			if k.Kind != yaml.ScalarNode {
				return nil, xerrors.Wrapf(errEvents, "on: の %d 行目のキーがイベント名ではありません", k.Line)
			}
			out = append(out, wfEvent{name: k.Value, line: k.Line, value: on.Content[i+1]})
		}
		return out, nil
	}
	return nil, xerrors.Wrap(errEvents, "on: の値が単一値・配列・マッピングのどれでもありません")
}

// emptyEvent は、イベントの値が無い（配列や単一値の記法か、値が空か null）かを返します。マッピングでもなく空でも
// ない値は中身を読めないので、呼び出し側は違反にする。
func emptyEvent(v *yaml.Node) bool {
	return v == nil || (v.Kind == yaml.ScalarNode && (v.Value == "" || v.ShortTag() == "!!null"))
}

// mergeTag は、yaml のマージキー（<<）の解決後の tag です。
const mergeTag = "!!merge"

// opaqueNode は、n の下を文書の順に辿り、中身を読めない最初の node（マージキー、別名、スカラーでない mapping の
// キー）の行とその種類を返します。無ければ行は 0 です。
func opaqueNode(n *yaml.Node) (int, string) {
	if n.Kind == yaml.AliasNode {
		return n.Line, "別名（*）"
	}
	for i, c := range n.Content {
		if n.Kind == yaml.MappingNode && i%2 == 0 {
			switch {
			case c.Kind != yaml.ScalarNode:
				return c.Line, "スカラーでないキー"
			case c.ShortTag() == mergeTag:
				return c.Line, "マージキー（<<）"
			}
		}
		if line, what := opaqueNode(c); line != 0 {
			return line, what
		}
	}
	return 0, ""
}

func (d wfDoc) event(name string) (wfEvent, bool) {
	for _, e := range d.events {
		if e.name == name {
			return e, true
		}
	}
	return wfEvent{}, false
}

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
// 名指すときは、最初に名指したファイルへ報告する。legacy は、役割ごとの ruleset と並べて適用する単一の ruleset の
// context で、そこに在る context の起動条件は [check] がフィルタ無しを求めるので、印の集合を課さない。
func directFindings(contexts []requiredchecks.Context, legacy []string, docs []wfDoc) []lintreport.Finding {
	var findings []lintreport.Finding
	inLegacy := map[string]bool{}
	for _, c := range legacy {
		inLegacy[c] = true
	}
	first := map[string]requiredchecks.Context{}
	sets := map[string]map[string]bool{}
	var order []string
	for _, c := range contexts {
		if _, ok := first[c.Name]; !ok {
			first[c.Name] = c
			sets[c.Name] = map[string]bool{}
			order = append(order, c.Name)
		}
		sets[c.Name][rulesetSet(c.File)] = true
	}
	for _, name := range order {
		c := first[name]
		want, conflict := "", false
		switch {
		case inLegacy[name]:
			// 並行期間は古い宣言が同じ context を required とし、[check] がフィルタ無しを求める。deploy-gate.yaml の
			// context はここに当たり、印の集合を課さない（決定40）。
			want = setAny
		case len(sets[name]) > 1:
			conflict = true
		default:
			for set := range sets[name] {
				want = set
			}
		}
		if conflict {
			findings = append(findings, lintreport.Finding{
				File: c.File, Line: 1,
				Message: fmt.Sprintf("required context `%s` を、対象の集合が違う ruleset が required としています。報告する workflow の branches を1つの集合に揃えられません", name),
			})
			want = setAny
		}

		n := 0
		for _, d := range docs {
			for _, j := range d.jobs {
				if j.id != name {
					continue
				}
				n++
				findings = append(findings, directJobFindings(name, want, d, j)...)
			}
		}
		if n != 1 {
			findings = append(findings, lintreport.Finding{
				File: c.File, Line: 1,
				Message: fmt.Sprintf("required context `%s` を報告する job は 1 件必要です（実際: %d）", name, n),
			})
		}
	}
	return findings
}

// setNone と setAny は、[rulesetSet] と [directJobFindings] が使う、印の集合の代わりの値。setNone は branches を
// 置けないこと、setAny は印さえあれば集合を問わないことを表す。
const (
	setNone = ""
	setAny  = "*"
)

// rulesetSet は、ruleset の宣言 file が required とする context の workflow が、pull_request の branches に置ける印の
// 集合を返します。線の ruleset は gate、env を持つデプロイブランチの ruleset は deploy-env で、ほかは branches を
// 置けない。
func rulesetSet(file string) string {
	base := filepath.Base(file)
	switch {
	case base == linesRulesetFile:
		return setGate
	case strings.HasPrefix(base, deployRulesetStem):
		return setDeployEnv
	}
	return setNone
}

// linesRulesetFindings は、線の ruleset の required の context がちょうど gate の1つであることを検査します
// （決定35）。gate のほかの context は、gate が呼ぶ検査と二重に required になるか、gate の workflow の外で報告される。
func linesRulesetFindings(in gateInput) []lintreport.Finding {
	var got []string
	for _, c := range in.contexts {
		if filepath.Base(c.File) == linesRulesetFile {
			got = append(got, c.Name)
		}
	}
	if len(got) == 1 && got[0] == gateJob {
		return nil
	}
	return []lintreport.Finding{{
		File: filepath.Join(in.rulesetsDir, linesRulesetFile), Line: 1,
		Message: fmt.Sprintf("線の ruleset の required context は `%s` の1つだけにします（実際: [%s]）", gateJob, strings.Join(got, ", ")),
	}}
}

// directJobFindings は、直接 required の context を報告する job 1つを検査します。set は、その workflow の
// pull_request の branches に置ける印の集合（[rulesetSet]）。線の検証の workflow の起動条件は
// [lineVerifyTriggerFindings] が見る。
func directJobFindings(context, set string, d wfDoc, j wfJob) []lintreport.Finding {
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
	if emptyEvent(pr.value) {
		return findings
	}
	if pr.value.Kind != yaml.MappingNode {
		add(pr.line, "の workflow の pull_request の値を読めません。フィルタを置くならマッピングで書きます")
		return findings
	}
	for i := 0; i+1 < len(pr.value.Content); i += 2 {
		k := pr.value.Content[i]
		switch k.Value {
		case "paths", "paths-ignore", "branches-ignore":
			add(k.Line, "の pull_request に `%s` が残っています（起動しなかった check は未報告のままマージを止めます）", k.Value)
		case "branches":
			var m []string
			if k.Line >= 1 && k.Line <= len(d.lines) {
				m = branchSetMarker.FindStringSubmatch(d.lines[k.Line-1])
			}
			switch {
			case m == nil:
				add(k.Line, "の pull_request の branches が、印 `# branch-set: <集合>` で branches.json から生成した一覧ではありません")
			case set == setNone:
				add(k.Line, "の pull_request に branches があります。この context を required とする ruleset の対象は、印の集合で表せません")
			case set != setAny && m[1] != set:
				add(k.Line, "の pull_request の branches の印は `# branch-set: %s` にします（実際: %s）。context を required とする ruleset の対象と同じ集合です", set, m[1])
			}
		}
	}
	return findings
}

// gateWorkflowFindings は、gate の workflow の call-* の job の集合が宣言の検査と両方向で一致し、判定の job が
// それらをすべて受け取り、取り消されても走る形であることを検査します（決定17-19・37）。byName は、call-* が呼ぶ
// workflow を secrets: の検査のために引く。
func gateWorkflowFindings(s gateSpec, d wfDoc, checksPath string, byName map[string]wfDoc) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: d.file, Line: line, Message: fmt.Sprintf(format, args...)})
	}

	for _, key := range []string{"env", "defaults"} {
		if k, _ := lookup(d.root, key); k != nil {
			add(k.Line, "%s の最上位に %s: を置けません。判定の step の環境・shell・場所を替えます", s.file, key)
		}
	}
	if len(s.types) > 0 {
		findings = append(findings, typesFindings(s, d)...)
	}

	want := make(map[string]checksdecl.Check, len(s.checks))
	wantIDs := make([]string, 0, len(s.checks))
	for _, c := range s.checks {
		id := checksdecl.CallerPrefix + c.Name
		want[id] = c
		wantIDs = append(wantIDs, id)
	}

	got := map[string]bool{}
	callers := 0
	var judges, changes []wfJob
	for _, j := range d.jobs {
		switch {
		case j.id == s.job:
			judges = append(judges, j)
		case s.gated && j.id == changesJob:
			changes = append(changes, j)
		case strings.HasPrefix(j.id, checksdecl.CallerPrefix):
			callers++
			c, ok := want[j.id]
			if !ok {
				add(j.line, "job `%s` は %s に無い検査を呼んでいます", j.id, checksPath)
				continue
			}
			got[j.id] = true
			_, uses := j.get("uses")
			if exp := localWorkflowPrefix + c.Workflow; uses == nil || uses.Kind != yaml.ScalarNode || uses.Value != exp {
				add(j.line, "job `%s` の uses: は `%s` にします", j.id, exp)
			}
			findings = append(findings, callerFindings(s, d.file, j, c, byName[c.Workflow])...)
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

	if s.gated {
		if len(changes) != 1 {
			add(1, "job `%s` は 1 件必要です（実際: %d）", changesJob, len(changes))
		} else {
			findings = append(findings, stepFindings(changesJob, d, changes[0], changesStep)...)
		}
	}

	if len(judges) != 1 {
		add(1, "job `%s` は 1 件必要です（実際: %d）", s.job, len(judges))
		return findings
	}
	findings = append(findings, judgeFindings(s.job, d.file, judges[0], append(append([]string{}, s.fixed...), wantIDs...))...)
	return append(findings, stepFindings(s.job, d, judges[0], s.step)...)
}

// typesFindings は、workflow の pull_request の types が s.types をすべて含むことを検査します。types を置かないと
// GitHub の既定（opened・synchronize・reopened）になる。
func typesFindings(s gateSpec, d wfDoc) []lintreport.Finding {
	line := 1
	got := map[string]bool{}
	if pr, ok := d.event("pull_request"); ok {
		line = pr.line
		if k, v := lookup(pr.value, "types"); k != nil {
			line = k.Line
			switch v.Kind {
			case yaml.ScalarNode:
				got[v.Value] = true
			case yaml.SequenceNode:
				for _, e := range v.Content {
					got[e.Value] = true
				}
			}
		}
	}
	var missing []string
	for _, t := range s.types {
		if !got[t] {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []lintreport.Finding{{
		File: d.file, Line: line,
		Message: fmt.Sprintf("%s の pull_request の types は %s を含めます（無いもの: %s）。判定は base に依るので、base の付け替え（edited）でも走り直します",
			s.file, strings.Join(s.types, ", "), strings.Join(missing, ", ")),
	}}
}

// callerIf は、gate の workflow の call-<name> の job の if: の式（${{ }} の中身）。changes が success でなければ
// fail open で走り、判定は gate が失敗にする。
func callerIf(name string) string {
	return "!cancelled() && (needs.changes.result != 'success' || contains(fromJSON(needs.changes.outputs.run), '" + name + "'))"
}

// callerFindings は、call-* の job 1つが、宣言の検査をそのまま呼ぶ形であることを検査します。with: は呼ばれる側の
// 入力を変え、secrets: inherit は呼ばれる側へすべての secret を渡す。secrets: は、呼ばれる側 called が
// workflow_call.secrets で宣言した名前へ secret を1つずつ渡す形だけを許す。gate の workflow では、needs: が changes
// だけで、if: が [callerIf] であることも課す。線の検証は path で絞らないので、if: を置けない。
func callerFindings(s gateSpec, file string, j wfJob, c checksdecl.Check, called wfDoc) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf("job `%s` ", j.id) + fmt.Sprintf(format, args...)})
	}

	if k, _ := j.get("with"); k != nil {
		add(k.Line, "に with: を置けません。検査は宣言の workflow をそのまま呼びます")
	}
	if k, v := j.get("secrets"); k != nil {
		findings = append(findings, secretsFindings(j.id, file, k, v, called)...)
	}

	k, v := j.get("if")
	if !s.gated {
		if k != nil {
			add(k.Line, "に if: を置けません。線の検証は検査を path で絞らず、すべて走らせます")
		}
		return findings
	}
	switch {
	case k == nil:
		add(j.line, "に if: がありません。`${{ %s }}` にします", callerIf(c.Name))
	case v.Kind != yaml.ScalarNode || unwrapExpr(v.Value) != callerIf(c.Name):
		add(k.Line, "の if: は `${{ %s }}` にします", callerIf(c.Name))
	}
	nk, nv := j.get("needs")
	if nk == nil {
		add(j.line, "に needs: がありません。`needs: %s` にします", changesJob)
	} else if needs, ok := needsOf(nv); !ok || len(needs) != 1 || needs[0] != changesJob {
		add(nk.Line, "の needs: は `%s` だけにします", changesJob)
	}
	return findings
}

// secretsFindings は、call-* の job の secrets: が、呼ばれる側の workflow_call.secrets の名前へ ${{ secrets.<名前> }}
// を渡す mapping であることを検査します。
func secretsFindings(id, file string, key, value *yaml.Node, called wfDoc) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf("job `%s` の secrets: ", id) + fmt.Sprintf(format, args...)})
	}
	if value.Kind != yaml.MappingNode {
		add(key.Line, "は、呼ばれる側が workflow_call.secrets で宣言した名前へ1つずつ渡す mapping にします（inherit は使えません）")
		return findings
	}
	declared := map[string]bool{}
	if e, ok := called.event("workflow_call"); ok {
		_, sec := lookup(e.value, "secrets")
		if sec != nil && sec.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(sec.Content); i += 2 {
				declared[sec.Content[i].Value] = true
			}
		}
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		k, v := value.Content[i], value.Content[i+1]
		if !declared[k.Value] {
			add(k.Line, "の `%s` は、呼ばれる workflow の workflow_call.secrets に宣言がありません", k.Value)
		}
		if v.Kind != yaml.ScalarNode || !secretRef.MatchString(strings.TrimSpace(v.Value)) {
			add(k.Line, "の `%s` の値は `${{ secrets.<名前> }}` にします", k.Value)
		}
	}
	return findings
}

// stepFindings は、job j の steps に make の入口 s.run をちょうど1つ起動する step が在り、job と step が判定の
// 失敗を成功へ変えず、make の振る舞いを替えない形であることを検査します。job は continue-on-error・env・defaults・
// container を持たず、判定の step は s.keys の key だけを持ち、env は s.env の key だけを持つ。判定の step より前の
// step は、egress が生成した bullfrog の三つ組（harden.GeneratedLines）のほかに run: を持たない —— run: は
// $GITHUB_ENV へ書いて判定の step の環境を替えられる。run: は字面で一致させるので、`||` や `;` の後続は一致しない。
func stepFindings(id string, d wfDoc, j wfJob, s stepSpec) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: d.file, Line: line, Message: fmt.Sprintf("job `%s` ", id) + fmt.Sprintf(format, args...)})
	}

	if k, _ := j.get("continue-on-error"); k != nil {
		add(k.Line, "に continue-on-error を置けません。判定の失敗が job の成功になります")
	}
	for _, key := range []string{"env", "defaults", "container"} {
		if k, _ := j.get(key); k != nil {
			add(k.Line, "に %s: を置けません。判定の step の環境・shell・場所を替えます", key)
		}
	}
	_, steps := j.get("steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		add(j.line, "に steps の一覧がありません。`%s` を起動する step が要ります", s.run)
		return findings
	}
	at := -1
	n := 0
	for i, st := range steps.Content {
		if _, r := lookup(st, "run"); r != nil && r.Kind == yaml.ScalarNode && makeTarget.MatchString(r.Value) {
			at = i
			n++
		}
	}
	if n != 1 {
		add(j.line, "の steps に make の入口を起動する step は 1 件必要です（実際: %d）", n)
		return findings
	}
	generated := hardenLines(d, j.id)
	for _, st := range steps.Content[:at] {
		if k, _ := lookup(st, "run"); k != nil && !generated[k.Line] {
			add(k.Line, "の判定の step より前の step に run: を置けません。$GITHUB_ENV などで判定の step の環境を替えます")
		}
	}
	st := steps.Content[at]
	if _, r := lookup(st, "run"); strings.TrimSpace(r.Value) != s.run {
		add(r.Line, "の判定の step の run: は `%s` にします。後続のコマンドや別の入口は、判定の結果を変えます", s.run)
	}
	for i := 0; i+1 < len(st.Content); i += 2 {
		if k := st.Content[i]; !slices.Contains(s.keys, k.Value) {
			add(k.Line, "の判定の step に %s: を置けません。置ける key は %s です", k.Value, strings.Join(s.keys, ", "))
		}
	}
	envKey, env := lookup(st, "env")
	switch {
	case env == nil:
	case env.Kind != yaml.MappingNode:
		add(envKey.Line, "の判定の step の env: を読めません")
	default:
		for i := 0; i+1 < len(env.Content); i += 2 {
			if k := env.Content[i]; !slices.Contains(s.env, k.Value) {
				add(k.Line, "の判定の step の env に `%s` を置けません。置ける key は %s です", k.Value, strings.Join(s.env, ", "))
			}
		}
	}
	if !s.needs {
		return findings
	}
	_, v := lookup(env, "NEEDS")
	if v == nil || v.Kind != yaml.ScalarNode || strings.TrimSpace(v.Value) != needsEnv {
		add(st.Line, "の判定の step の env の NEEDS は `%s` にします", needsEnv)
	}
	return findings
}

// hardenLines は、workflow d の job id の先頭で egress が生成した bullfrog の三つ組の行の番号を返します。
func hardenLines(d wfDoc, id string) map[int]bool {
	for _, j := range workflow.SplitJobs(strings.Join(d.lines, "\n")).Jobs {
		if j.ID == id {
			return harden.GeneratedLines(j)
		}
	}
	return nil
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
	return unwrapExpr(v.Value) == "always()"
}

// unwrapExpr は、if: の値の前後の空白と、全体を囲む ${{ }} を除きます。
func unwrapExpr(v string) string {
	s := strings.TrimSpace(v)
	if strings.HasPrefix(s, "${{") && strings.HasSuffix(s, "}}") {
		s = strings.TrimSpace(s[3 : len(s)-2])
	}
	return s
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
		if emptyEvent(e.value) {
			continue
		}
		if e.value.Kind != yaml.MappingNode {
			findings = append(findings, lintreport.Finding{
				File: d.file, Line: e.line,
				Message: lineVerifyFile + " の push の値を読めません。フィルタを置くならマッピングで書きます",
			})
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

// calledFindings は、検査の宣言が名指す各 workflow を、宣言の順に1度ずつ検査します。
// 役割ごとの ruleset と並べて適用する単一の ruleset の宣言（in.legacy）が在るときは、その context を報告するために
// pull_request で起動し if: で skip する形が残るので、その2つの違反を課さない（final が偽）。
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

// calledWorkflowFindings は、gate が呼ぶ workflow 1つを検査します（決定16・17・19）。final のときだけ、
// pull_request での起動と job の if: も違反にする（[calledFindings] が決める）。
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
