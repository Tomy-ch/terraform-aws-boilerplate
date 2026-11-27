package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// plan-permissions の role ごとの文書は plan の resource から導き、address の表を持たない（planDocuments）。
// 出力に ARN と context の値を出さない理由は ADR-0602 決定13。

const (
	typeRolePolicy          = "aws_iam_role_policy"
	typeRolePolicies        = "aws_iam_role_policies_exclusive"
	typeRoleAttachments     = "aws_iam_role_policy_attachments_exclusive"
	typeManagedPolicy       = "aws_iam_policy"
	canaryTargetRegionKey   = "cloudformation:TargetRegion"
	planPermissionsHeadline = "# plan-permissions"
)

var (
	errPlanPermInput  = xerrors.New("plan のファイルを読めません。権限を評価できません")
	errPlanPermJSON   = xerrors.New("plan の JSON の形が想定と違います。権限を評価できません")
	errPlanPermEmpty  = xerrors.New("plan に resource_changes がありません。権限を評価できません")
	errPlanPermNoDocs = xerrors.New("plan から文書を決められた CI の role が1つもありません。権限を評価できません")
	// errCanary は、落ちるべき文書が NG にならないこと。評価器が文書を見ていないので、本走査をしない（ADR-0702 決定25）。
	errCanary = xerrors.New("落ちるべき文書が simulate で NG になりません。権限を評価できません")
)

type planPermOptions struct {
	plan    string
	project string
	region  string
}

func parsePlanPermArgs(args []string) (planPermOptions, error) {
	fs := flag.NewFlagSet("plan-permissions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var o planPermOptions

	fs.StringVar(&o.plan, "plan", "", "")
	fs.StringVar(&o.project, "project", "", "")
	fs.StringVar(&o.region, "region", "", "")

	if err := fs.Parse(args); err != nil {
		return planPermOptions{}, xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 0 || o.plan == "" || o.project == "" || o.region == "" {
		return planPermOptions{}, errUsage
	}

	return o, nil
}

// planChange は、plan の resource_changes の1件のうち、文書を導くのに使う分。after は削除なら nil。unknown は
// change.after_unknown（全体が未知なら true）。
type planChange struct {
	address string
	typ     string
	after   map[string]any
	before  map[string]any
	unknown any
}

// readPlanChanges は、`terraform show -json` の plan の resource_changes を読む。resource_changes が無いか空なら
// errPlanPermEmpty、JSON の object でないか、要素の address と type が文字列でなければ errPlanPermJSON。
func readPlanChanges(data []byte) ([]planChange, error) {
	top, ok := decodeObject(data)
	if !ok {
		return nil, errPlanPermJSON
	}

	raw, present := top["resource_changes"]
	if !present || raw == nil {
		return nil, errPlanPermEmpty
	}

	list, ok := raw.([]any)
	if !ok {
		return nil, xerrors.Wrap(errPlanPermJSON, "resource_changes が配列ではない")
	}

	if len(list) == 0 {
		return nil, errPlanPermEmpty
	}

	out := make([]planChange, 0, len(list))

	for i, e := range list {
		address, ok1 := at(e, "address").(string)
		typ, ok2 := at(e, "type").(string)

		if !ok1 || !ok2 || address == "" {
			return nil, xerrors.Wrapf(errPlanPermJSON, "resource_changes の %d 番目に address か type が無い", i)
		}

		after, _ := at(e, "change", "after").(map[string]any)
		before, _ := at(e, "change", "before").(map[string]any)
		out = append(out, planChange{address: address, typ: typ, after: after, before: before, unknown: at(e, "change", "after_unknown")})
	}

	return out, nil
}

// unknownAt は、after の key の値（その要素を含む）が plan で決まらないこと。
func (c planChange) unknownAt(key string) bool {
	if c.unknown == true {
		return true
	}

	return anyTrue(at(c.unknown, key))
}

// anyTrue は、v かその要素のどれかが true であること。
func anyTrue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case []any:
		return slices.ContainsFunc(x, anyTrue)
	case map[string]any:
		for _, e := range x {
			if anyTrue(e) {
				return true
			}
		}
	}

	return false
}

// owner は、after の key の文字列。削除（after が nil）なら before の値。決まらなければ空。
func (c planChange) owner(key string) string {
	src := c.after
	if src == nil {
		src = c.before
	} else if c.unknownAt(key) {
		return ""
	}

	s, _ := src[key].(string)

	return s
}

// stringsOf は、after の key の文字列の配列。決まらないか、文字列の配列でなければ false。
func (c planChange) stringsOf(key string) ([]string, bool) {
	if c.after == nil || c.unknownAt(key) {
		return nil, false
	}

	return stringsAt(c.after[key], "")
}

// planDocument は、after の policy の文書。削除・plan で決まらない・空・JSON の object でなければ、address を名指す理由を返す。
func planDocument(c planChange) (string, string) {
	if c.after == nil {
		return "", c.address + " が削除される"
	}

	if c.unknownAt("policy") {
		return "", c.address + " の文書が plan で決まらない"
	}

	doc, _ := c.after["policy"].(string)
	if doc == "" {
		return "", c.address + " に文書が無い"
	}

	if _, ok := decodeObject([]byte(doc)); !ok {
		return "", c.address + " の文書を JSON の object として読めない"
	}

	return doc, ""
}

// policyNameOf は、managed policy の ARN の名前。policy の ARN でなければ空。
func policyNameOf(arn string) string {
	_, path, ok := strings.Cut(arn, ":policy/")
	if !ok {
		return ""
	}

	return path[strings.LastIndex(path, "/")+1:]
}

// managedPolicyARN は、aws_iam_policy の ARN。plan で決まっていれば arn、決まらなければ account・path・name から組む。
// name が決まらなければ空。
func managedPolicyARN(c planChange, account string) string {
	if arn := c.owner("arn"); arn != "" {
		return arn
	}

	name := c.owner("name")
	if name == "" {
		return ""
	}

	path := c.owner("path")
	if path == "" {
		path = "/"
	}

	return "arn:aws:iam::" + account + ":policy" + path + name
}

// rolePolicies は、plan から導いた1つの role の policy の文書と、付く managed policy の名前（並べ替えたもの）。
// unresolved が空でなければ、文書を決められなかった理由。
type rolePolicies struct {
	docs       []string
	managed    []string
	unresolved string
}

func planDocuments(changes []planChange, n names) map[string]rolePolicies {
	out := map[string]rolePolicies{}
	for _, role := range n.ciRoles() {
		out[role] = roleDocuments(changes, n, n.roleName(role))
	}

	return out
}

func only(changes []planChange, typ, key, roleName string) []planChange {
	var out []planChange

	for _, c := range changes {
		if c.typ == typ && c.owner(key) == roleName {
			out = append(out, c)
		}
	}

	return out
}

// roleDocuments は、roleName の role の文書。inline は aws_iam_role_policy の文書、managed は
// aws_iam_role_policy_attachments_exclusive の policy_arns の ARN に当たる aws_iam_policy（managedPolicyARN）の文書である。inline の数は
// aws_iam_role_policies_exclusive の policy_names の数と、managed の数は policy_arns の数と突き合わせ、合わなければ
// 決められない（ADR-0702 決定15）。
func roleDocuments(changes []planChange, n names, roleName string) rolePolicies {
	exclusive := only(changes, typeRolePolicies, "role_name", roleName)
	attachments := only(changes, typeRoleAttachments, "role_name", roleName)
	inline := only(changes, typeRolePolicy, "role", roleName)

	unresolved := func(format string, a ...any) rolePolicies {
		return rolePolicies{unresolved: fmt.Sprintf(format, a...)}
	}

	if len(exclusive) != 1 || len(attachments) != 1 {
		return unresolved("plan に %s の %s が %d 個、%s が %d 個（1個ずつを期待）", roleName, typeRolePolicies, len(exclusive), typeRoleAttachments, len(attachments))
	}

	var docs []string

	for _, c := range []planChange{exclusive[0], attachments[0]} {
		if c.after == nil {
			return unresolved("%s が削除される", c.address)
		}
	}

	policyNames, ok := exclusive[0].stringsOf("policy_names")
	if !ok {
		return unresolved("%s の policy_names が plan で決まらない", exclusive[0].address)
	}

	for _, c := range inline {
		doc, reason := planDocument(c)
		if reason != "" {
			return unresolved("%s", reason)
		}

		docs = append(docs, doc)
	}

	if len(docs) != len(policyNames) {
		return unresolved("inline policy の文書が %d 個で、%s の policy_names の %d 個と合わない", len(docs), exclusive[0].address, len(policyNames))
	}

	arns, ok := attachments[0].stringsOf("policy_arns")
	if !ok {
		return unresolved("%s の policy_arns が plan で決まらない", attachments[0].address)
	}

	var managed []string

	seen := map[string]bool{}

	for _, arn := range arns {
		if seen[arn] {
			return unresolved("%s の policy_arns に同じ policy が2つある", attachments[0].address)
		}

		seen[arn] = true

		found := slices.DeleteFunc(slices.Clone(changes), func(c planChange) bool {
			return c.typ != typeManagedPolicy || managedPolicyARN(c, n.account) != arn
		})
		if len(found) != 1 {
			continue
		}

		doc, reason := planDocument(found[0])
		if reason != "" {
			return unresolved("%s", reason)
		}

		docs = append(docs, doc)
		managed = append(managed, policyNameOf(arn))
	}

	if len(managed) != len(arns) {
		return unresolved("managed policy の文書が %d 個で、%s の policy_arns の %d 個と合わない", len(managed), attachments[0].address, len(arns))
	}

	if len(docs) == 0 {
		return unresolved("plan に %s の文書が1つも無い", roleName)
	}

	slices.Sort(managed)

	return rolePolicies{docs: docs, managed: managed}
}

// planGates は、plan の managed policy の名前から決めた、条件つきの区分の前提の付き方。role の文書を決められなければ
// unknown で、NG は統制と workload の側が持つ。
func planGates(n names, policies map[string]rolePolicies) map[gate]gateResult {
	gates := map[gate]gateResult{gateNone: {state: gateAttached}}

	if ai := policies[n.applyIdentity]; ai.unresolved != "" {
		gates[gateGovernance] = gateResult{state: gateUnknown, ng: fixedCheck(n.applyIdentity+": plan で managed policy を決められないので、統制の policy とメンバーの role の policy が付いているかを決められない", verdict{reason: ai.unresolved})}
		gates[gateDelegatedAdmin] = gateResult{state: gateUnknown}
	} else {
		gates[gateGovernance], gates[gateDelegatedAdmin] = governanceGateOf(n, ai.managed)
	}

	if ap := policies[n.apply]; ap.unresolved != "" {
		gates[gateWorkload] = gateResult{state: gateUnknown, ng: fixedCheck(n.apply+": plan で managed policy を決められないので、workload の3つの policy が付いているかを決められない", verdict{reason: ap.unresolved})}
	} else {
		gates[gateWorkload] = workloadGateOf(n, ap.managed)
	}

	return gates
}

type planItem struct {
	check check
	// row は、表の行から作った確認のときだけ持つ（それ以外は nil）。
	row *simRow
}

type planGroup struct {
	heading string
	items   []planItem
}

// planPermissionGroups は、permissions の表の行に plan の文書を与えた確認の表。条件つきの区分は、前提の policy が
// plan で付いていなければ、注記で通さずに NG を1件置く。sk は state の鍵の ARN で、skReason が空でなければ読めなかった理由。
func planPermissionGroups(n names, policies map[string]rolePolicies, sk, skReason string) []planGroup {
	gates := planGates(n, policies)
	sections := permissionSections(n, sk, skReason)
	groups := make([]planGroup, 0, len(sections))

	for _, s := range sections {
		g := planGroup{heading: s.heading}

		for _, l := range s.lead {
			g.items = append(g.items, planItem{check: l.check})
		}

		addRows := func(rows []simRow) {
			for _, r := range rows {
				p := policies[r.role]
				r.docs, r.unresolved = p.docs, p.unresolved

				if skReason != "" && r.resource == sk {
					r.unresolved = "state の鍵の ARN を読めない"
				}

				g.items = append(g.items, planItem{check: simCheck(r, n), row: &r})
			}
		}

		addRows(s.rows)

		switch gr := gates[s.gate]; {
		case s.gate == gateNone:
		case gr.state == gateAttached:
			addRows(s.gated)
		case gr.state == gateAbsent:
			g.items = append(g.items, planItem{check: fixedCheck(fmt.Sprintf("この区分の前提の managed policy が plan で付いていない（条件つきの %d 行を確かめない）", len(s.gated)), verdict{})})
		case gr.ng.eval != nil:
			g.items = append(g.items, planItem{check: gr.ng})
		default:
			g.items = append(g.items, planItem{check: fixedCheck(fmt.Sprintf("この区分の前提の managed policy の付き方を決められない（上の区分の NG。条件つきの %d 行を確かめない）", len(s.gated)), verdict{})})
		}

		groups = append(groups, g)
	}

	return groups
}

// planNGLine は、NG の1行。表の行は role・action の一覧・期待・理由だけを出し、resource と context を出さない。
func planNGLine(it planItem, v verdict) string {
	line := "NG  " + it.check.desc
	if it.row != nil {
		line = fmt.Sprintf("NG  %s: %s が %s", it.row.role, strings.Join(it.row.actions, " "), it.row.want)
	}

	if v.reason != "" {
		line += "（" + v.reason + "）"
	}

	return line
}

// runPlanChecks は、区分ごとに ok と NG の件数と NG の行を出し、結果の行を出す。NG があれば errMismatch。確認が1件も
// 無ければ errNoChecks。aws を起動できなくなれば、結果の行を出さずにそのエラーを返す。
func runPlanChecks(q *query, out io.Writer, groups []planGroup) error {
	total, fails := 0, 0

	for _, g := range groups {
		var lines []string

		for _, it := range g.items {
			v, err := it.check.eval(q)
			if err != nil {
				return err
			}

			total++

			if !v.ok {
				lines = append(lines, planNGLine(it, v))
			}
		}

		fails += len(lines)

		fmt.Fprintf(out, "%s: ok %d 件、NG %d 件\n", g.heading, len(g.items)-len(lines), len(lines))

		for _, l := range lines {
			fmt.Fprintln(out, l)
		}
	}

	if total == 0 {
		return errNoChecks
	}

	fmt.Fprintf(out, "# 結果: %d 件のうち NG %d 件\n", total, fails)

	if fails > 0 {
		return errMismatch
	}

	return nil
}

// canaryRow は、rows（stackInstanceRows の行）のうち、type に provider の region だけの TargetRegion の一覧で allowed を
// 期待する行。
func canaryRow(n names, rows []simRow) (simRow, bool) {
	_, _, ty := stackSetARNs(n, n.project+"-break-glass")
	want := []string{ctxList(canaryTargetRegionKey, n.region)}

	i := slices.IndexFunc(rows, func(r simRow) bool {
		return r.want == allowed && r.resource == ty && slices.Equal(r.context, want)
	})
	if i < 0 {
		return simRow{}, false
	}

	return rows[i], true
}

// canaryDocument は、r の action を r の resource に、TargetRegion を StringEquals で provider の region に限って許す文書。
// TargetRegion の一覧に StringEquals は当たらないので、r は NG になるはずである。
func canaryDocument(r simRow, region string) string {
	b, _ := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []any{map[string]any{
			"Effect": "Allow", "Action": r.actions, "Resource": r.resource,
			"Condition": map[string]any{"StringEquals": map[string]any{canaryTargetRegionKey: region}},
		}},
	})

	return string(b)
}

// runCanary は、落ちるべき文書を canaryRow の行で評価する。評価が返り、要求した action がすべて implicitDeny で返った
// ときだけ合格で、allowed・呼び出しの失敗・読めない応答・足りない context・action の数の食い違いは errCanary。
func runCanary(q *query, n names, rows []simRow) error {
	r, ok := canaryRow(n, rows)
	if !ok {
		return xerrors.Wrap(errCanary, "表に、type の provider の region だけの一覧で allowed を期待する行が無い")
	}

	r.docs = []string{canaryDocument(r, n.region)}
	r.want = implicitDeny

	v, err := simCheck(r, n).eval(q)
	if err != nil {
		return err
	}

	if !v.ok {
		return xerrors.Wrap(errCanary, v.reason)
	}

	return nil
}

// checkPlanPermissions は、plan の文書を permissions の表で評価する。aws は sts・kms の describe-key と simulate だけを起動する。
func checkPlanPermissions(o planPermOptions, out io.Writer, d deps) error {
	profile, err := awsProfile(d.getenv)
	if err != nil {
		return err
	}

	raw, err := d.readFile(o.plan)
	if err != nil {
		return xerrors.Wrap(errPlanPermInput, o.plan)
	}

	changes, err := readPlanChanges(raw)
	if err != nil {
		return err
	}

	q := newQuery(d.aws, o.region)
	q.terse = true

	r, err := q.call("text", "sts", "get-caller-identity", "--query", "Account")
	if err != nil {
		return err
	}

	if r.failed() {
		return xerrors.Wrapf(errNoAccount, "sts get-caller-identity が失敗: %s", q.failure(r.stderr))
	}

	account := strings.TrimSpace(string(r.stdout))
	if !accountID.MatchString(account) {
		return xerrors.Wrapf(errNoAccount, "sts get-caller-identity の Account: %q", account)
	}

	n := newNames(verifyOptions{project: o.project, region: o.region}, account)
	policies := planDocuments(changes, n)

	if !slices.ContainsFunc(n.ciRoles(), func(role string) bool { return policies[role].unresolved == "" }) {
		var reasons []string
		for _, role := range n.ciRoles() {
			reasons = append(reasons, role+": "+policies[role].unresolved)
		}

		return xerrors.Wrap(errPlanPermNoDocs, strings.Join(reasons, "; "))
	}

	sk, skReason, err := stateKeyARN(q, n)
	if err != nil {
		return err
	}

	if skReason != "" {
		sk = "arn:aws:kms:" + n.region + ":" + n.account + ":key/unknown"
	}

	fmt.Fprintf(out, "%s（profile: %s、region: %s）\n", planPermissionsHeadline, profile, o.region)

	if err := runCanary(q, n, stackInstanceRows(n, otherRegion(n))); err != nil {
		return err
	}

	fmt.Fprintln(out, "# 評価器の確認: 落ちるべき文書が NG になった")

	return runPlanChecks(q, out, planPermissionGroups(n, policies, sk, skReason))
}

// relevantMissing は、keys（simulate-custom-policy が返した足りない context）のうち、docs の文のうち action と
// resource に当たる文（statementApplies）の Condition に出てくるキーだけ。キーは大文字小文字を区別しない。文書を
// 読めなければ keys をそのまま返す。
func relevantMissing(docs []string, action, resource string, keys []string) []string {
	relevant := map[string]bool{}

	for _, d := range docs {
		doc, ok := decodeObject([]byte(d))
		if !ok {
			return keys
		}

		for _, st := range arrOf(doc["Statement"]) {
			if !statementApplies(st, action, resource) {
				continue
			}

			conditions, _ := at(st, "Condition").(map[string]any)
			for _, byKey := range conditions {
				m, _ := byKey.(map[string]any)
				for k := range m {
					relevant[strings.ToLower(k)] = true
				}
			}
		}
	}

	return slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return !relevant[strings.ToLower(k)] })
}

// statementApplies は、文 st が action と resource の要求に当たること。NotAction・NotResource を持つ文、Action や
// Resource を読めない文は当たるとみなす（足りないキーを多く数える側）。resource が `*` の行は、どの Resource にも当たる。
func statementApplies(st any, action, resource string) bool {
	m, ok := st.(map[string]any)
	if !ok {
		return true
	}

	if _, not := m["NotAction"]; !not {
		patterns, ok := strs(arrOf(m["Action"]))
		if ok && !slices.ContainsFunc(patterns, func(p string) bool { return wildcardMatch(p, action) }) {
			return false
		}
	}

	if _, not := m["NotResource"]; !not && resource != "*" {
		patterns, ok := strs(arrOf(m["Resource"]))
		if ok && !slices.ContainsFunc(patterns, func(p string) bool { return wildcardMatch(p, resource) }) {
			return false
		}
	}

	return true
}

// wildcardMatch は、IAM の pattern（`*` は0文字以上、`?` は1文字）が s の全体に当たること。大文字小文字を区別しない。
func wildcardMatch(pattern, s string) bool {
	p, t := []rune(strings.ToLower(pattern)), []rune(strings.ToLower(s))
	star, mark := -1, 0
	i, j := 0, 0

	for j < len(t) {
		switch {
		case i < len(p) && (p[i] == '?' || p[i] == t[j]):
			i++
			j++
		case i < len(p) && p[i] == '*':
			star, mark = i, j
			i++
		case star >= 0:
			i = star + 1
			mark++
			j = mark
		default:
			return false
		}
	}

	for i < len(p) && p[i] == '*' {
		i++
	}

	return i == len(p)
}
