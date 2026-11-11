// Package main は bullfrog の通信制限（with: ブロック）を SSOT から生成・検証するツール。
//
//	apply: .github/egress.toml を元に、各 job の先頭にある bullfrog step の with: を書き換える。
//	check: apply と同じ判定を書き換えなしで行い、ドリフトがあれば非ゼロ終了する（CI / hook 用）。
//
// 詳細は .github/workflows/README.md の「ランナーの封じ込め」節を参照。
package main

import (
	"fmt"
	"log"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// scalarKeys はスカラーで書くキー。それ以外のキーは配列で書く。
var scalarKeys = map[string]bool{"egress_policy": true}

// elementMark は SSOT の本文に現れない文字で、配列の要素の位置を示す。
const elementMark = "\x00"

const (
	ssotFile = ".github/egress.toml"
	filePerm = 0o644
	// policyAudit は許可リストを持たない唯一の egress-policy。
	policyAudit = "audit"
	policyBlock = "block"
	// bullfrogAction は step を見つける錨。SHA は pin-actions が書くので、ここでは名前だけを見る。
	bullfrogAction = "bullfrogsec/bullfrog"
	// withIndent / itemIndent は workflow.SplitSteps が前提とする桁（step は6桁の "- "）から決まる。
	withIndent = "        "
	keyIndent  = "          "
	itemIndent = "            "
)

var (
	sectionRe   = regexp.MustCompile(`^\[(class|job|root|type)\.(?:"([^"]+)"|([A-Za-z0-9_-]+))\]$`)
	scalarRe    = regexp.MustCompile(`^(\w+)\s*=\s*"([^"]*)"$`)
	arrayHeadRe = regexp.MustCompile(`^(\w+)\s*=\s*\[(.*)$`)
	quotedRe    = regexp.MustCompile(`"([^"]*)"`)
	// domainRe は bullfrog の allowed-domains が受け付ける形のうち、先頭の `*.` だけをワイルドカードとして
	// 許す。道具は途中や単独の `*` も通すが、意図より広い範囲を開けるので宣言で拒む。
	domainRe = regexp.MustCompile(`^(?:\*\.)?[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+$`)
	// typeNameRe は resource / data source の型名。typePrefixRe は `_` で終わる接頭辞で、provider 名だけの
	// 接頭辞（`aws_`）はサービスをまたいで当たるので、少なくとも 2 要素を要る。
	typeNameRe   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	typePrefixRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)+_$`)
	stepNameRe   = regexp.MustCompile(`^ {6}- name: \S`)
	stepUsesRe   = regexp.MustCompile(`^ {8}uses: ` + regexp.QuoteMeta(bullfrogAction) + `@[0-9A-Za-z._-]+(?: # \S+)?$`)
	stepWithRe   = regexp.MustCompile(`^ {8}with:$`)
	commentRe    = regexp.MustCompile(`^\s*#`)
	// reusableRe は reusable workflow を呼ぶジョブ（runner を持たない）の `uses:` 行。
	reusableRe = regexp.MustCompile(`^ {4}uses:\s*\S`)
	// elementListRe は、配列の `[` と `]` の間から引用符付きの要素を elementMark に置き換えた残りが取れる形。
	elementListRe = regexp.MustCompile(`^\s*(?:\x00\s*,\s*)*(?:\x00\s*,?\s*)?$`)
	// shallowLineRe は job の本文に現れてはならない、job の見出しより浅い（または桁0以外の
	// 見出しと同じ桁の）行。切り出しが読めなかった見出しは、直前の job の本文にこの形で残る。
	shallowLineRe = regexp.MustCompile(`^(?: {1,3}[^\s#]|\t)`)
	// loopbackPrefix は allowed-ips に書ける範囲。allowed-ips はドメインの判定を経ずに通すので、
	// runner 自身の中（ローカルの DNS 転送役）より外を開けさせない。
	loopbackPrefix = netip.MustParsePrefix("127.0.0.0/8")
)

var (
	errUsage      = xerrors.New("usage: egress <apply|check>")
	errSSOTSyntax = xerrors.New("SSOT に解釈できない行があります")
	// errSSOTDuplicate は、SSOT にセクション / キー / 配列要素の重複があった場合のエラー。
	errSSOTDuplicate    = xerrors.New("SSOT に重複があります")
	errSSOTUnknownClass = xerrors.New("SSOT が未定義のクラスを参照しています")
	errSSOTClassCycle   = xerrors.New("SSOT のクラス継承が循環しています")
	errSSOTBaseClass    = xerrors.New("base クラスは全ジョブへ暗黙に適用されるため classes に書けません")
	// errSSOTPolicy は、egress_policy の値が不正、または audit ジョブが許可リストを持つ場合のエラー。
	errSSOTPolicy = xerrors.New("SSOT の egress_policy が不正です")
	// errSSOTDomain は、ポート付き・途中のワイルドカードなど、ドメイン名として書けない宛先のエラー。
	errSSOTDomain     = xerrors.New("SSOT の宛先がドメイン名の形をしていません")
	errSSOTIP         = xerrors.New("SSOT の ips が 127.0.0.0/8 内の IPv4 アドレスまたは CIDR ではありません")
	errSSOTIPScope    = xerrors.New("ips は base クラスだけが持てます")
	errSSOTUnknownJob = xerrors.New("SSOT の root が未定義のジョブを参照しています")
	errSSOTRootPath   = xerrors.New("SSOT の root がリポジトリ内の正規化された相対パスではありません")
	errSSOTTypeName   = xerrors.New("SSOT の type が resource / data source の型の接頭辞の形をしていません")
	// errSSOTTypeHosts は、宛先の無い接頭辞の type のエラー。API を呼ばない型は型名そのもので宣言する。
	errSSOTTypeHosts  = xerrors.New("SSOT の接頭辞の type が宛先を持ちません")
	errWorkflowNoJobs = xerrors.New("jobs: を持たない workflow があります")
	// errJobUnparsed は、reusable workflow の呼び出しでもないのに steps を読み取れないジョブのエラー。
	// 読める形は scripts/lib/workflow の切り出しが決める。
	errJobUnparsed   = xerrors.New("steps を読み取れないジョブがあります")
	errStepMissing   = xerrors.New("steps を持つのに bullfrog の step が無いジョブがあります")
	errStepNotFirst  = xerrors.New("bullfrog の step がジョブの最初の step ではありません")
	errStepDuplicate = xerrors.New("1 つのジョブに bullfrog の step が複数あります")
	// errStepMalformed は、bullfrog の step が生成器の書ける形（name / uses / with だけ）を外れているか、
	// step の外で bullfrog が名指しされている場合のエラー。apply では直らない。
	errStepMalformed = xerrors.New("bullfrog の step が想定外の形をしています")
	errJobMissing    = xerrors.New("SSOT に未登録のジョブがあります")
	errJobOrphan     = xerrors.New("SSOT に対応する workflow が無いジョブがあります")
	errNoTargets     = xerrors.New("bullfrog の step が 1 件も見つかりません")
	errEgressDrift   = xerrors.New("bullfrog の with: が SSOT からずれています")
)

type class struct {
	includes []string
	hosts    []string
	ips      []string
}

// jobSpec は 1 ジョブの宣言。key は `<workflow ファイル名>:<job id>`。
type jobSpec struct {
	classes []string
	extra   []string
	policy  string
}

// rootSpec は CI が Terraform を実行する root 1 件の宣言。jobs はその root を実行するジョブ。
type rootSpec struct {
	jobs []string
}

// typeSpec は resource / data source の型の接頭辞 1 件が呼ぶ宛先。
type typeSpec struct {
	hosts []string
}

// ssot は .github/egress.toml の内容。*Order は各セクションの宣言順。
type ssot struct {
	classes   map[string]*class
	jobs      map[string]*jobSpec
	jobOrder  []string
	roots     map[string]*rootSpec
	rootOrder []string
	types     map[string]*typeSpec
	typeOrder []string
	baseHosts []string
	baseIPs   []string
}

// section は解析中のセクション。どれか 1 つだけが nil でない。
type section struct {
	class *class
	job   *jobSpec
	root  *rootSpec
	typ   *typeSpec
}

// orderedHosts は初出順を保ったまま重複を落とすホスト集合。
type orderedHosts struct {
	list []string
	seen map[string]bool
}

// target は workflow 内の bullfrog step 1 件のうち、生成器が書く with: の範囲。
type target struct {
	jobKey string
	// start / end は差し替える行の範囲（0 起点・end は排他）。with: が無い step では start == end で、
	// uses: の直後へ挿入する。
	start int
	end   int
}

func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Getwd); err != nil {
		log.Fatalf("❌ %v", err)
	}
}

// run はサブコマンドを解釈して apply / check へ振り分ける。wd は走査の基点の取得手段。
func run(args []string, wd func() (string, error)) error {
	if len(args) != 1 {
		return errUsage
	}
	root, err := wd()
	if err != nil {
		return xerrors.Wrap(err, "getwd")
	}
	switch args[0] {
	case "apply":
		return applyOrCheck(root, false)
	case "check":
		return applyOrCheck(root, true)
	default:
		return errUsage
	}
}

func parseSSOT(data string) (*ssot, error) {
	s := &ssot{
		classes: map[string]*class{}, jobs: map[string]*jobSpec{},
		roots: map[string]*rootSpec{}, types: map[string]*typeSpec{},
	}
	var cur section
	var seen map[string]bool

	lines := strings.Split(data, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(stripComment(lines[i]))
		if line == "" {
			continue
		}
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			opened, err := s.openSection(m[1], m[2]+m[3])
			if err != nil {
				return nil, err
			}
			cur = opened
			seen = map[string]bool{}
			continue
		}
		if cur == (section{}) {
			return nil, xerrors.Wrap(errSSOTSyntax, fmt.Sprintf("%d 行目: %q（セクションの外に値があります）", i+1, line))
		}
		key, values, next, err := parseAssignment(lines, i)
		if err != nil {
			return nil, err
		}
		i = next
		if seen[key] {
			return nil, xerrors.Wrap(errSSOTDuplicate, fmt.Sprintf("%d 行目: キー %q", i+1, key))
		}
		seen[key] = true
		if err := assign(cur, key, values); err != nil {
			return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目", i+1))
		}
	}
	return s, s.validate()
}

// stripComment は行末コメントを落とす。値のクオート内に `#` は現れない前提。
func stripComment(line string) string {
	if before, _, ok := strings.Cut(line, "#"); ok {
		return before
	}
	return line
}

// openSection は kind のセクション name を登録し、以降の代入の宛先を返す。
func (s *ssot) openSection(kind, name string) (section, error) {
	dup := func() (section, error) { return section{}, xerrors.Wrap(errSSOTDuplicate, kind+" "+name) }
	switch kind {
	case "class":
		if _, ok := s.classes[name]; ok {
			return dup()
		}
		s.classes[name] = &class{}
		return section{class: s.classes[name]}, nil
	case "job":
		if _, ok := s.jobs[name]; ok {
			return dup()
		}
		s.jobs[name] = &jobSpec{}
		s.jobOrder = append(s.jobOrder, name)
		return section{job: s.jobs[name]}, nil
	case "root":
		if _, ok := s.roots[name]; ok {
			return dup()
		}
		s.roots[name] = &rootSpec{}
		s.rootOrder = append(s.rootOrder, name)
		return section{root: s.roots[name]}, nil
	default:
		if _, ok := s.types[name]; ok {
			return dup()
		}
		s.types[name] = &typeSpec{}
		s.typeOrder = append(s.typeOrder, name)
		return section{typ: s.types[name]}, nil
	}
}

// parseAssignment は lines[i] から始まる代入を読み、キー・値・消費した最終行を返す。
// 配列は `]` が現れるまで後続行を取り込む。要素は二重引用符の文字列をカンマで区切ったものに限る。
func parseAssignment(lines []string, i int) (string, []string, int, error) {
	start := i
	line := strings.TrimSpace(stripComment(lines[i]))
	if m := scalarRe.FindStringSubmatch(line); m != nil {
		if !scalarKeys[m[1]] {
			return "", nil, 0, xerrors.Wrap(errSSOTSyntax, fmt.Sprintf("%d 行目: %s は配列で書く", i+1, m[1]))
		}
		return m[1], []string{m[2]}, i, nil
	}
	m := arrayHeadRe.FindStringSubmatch(line)
	if m == nil {
		return "", nil, 0, xerrors.Wrap(errSSOTSyntax, fmt.Sprintf("%d 行目: %q", i+1, line))
	}
	key, body := m[1], m[2]
	if scalarKeys[key] {
		return "", nil, 0, xerrors.Wrap(errSSOTSyntax, fmt.Sprintf("%d 行目: %s はスカラーで書く", i+1, key))
	}
	for !strings.Contains(body, "]") {
		i++
		if i >= len(lines) {
			return "", nil, 0, xerrors.Wrap(errSSOTSyntax, "配列が閉じられていません: "+key)
		}
		body += "\n" + stripComment(lines[i])
	}
	inner, after, _ := strings.Cut(body, "]")
	if strings.TrimSpace(after) != "" || !elementListRe.MatchString(quotedRe.ReplaceAllString(inner, elementMark)) {
		return "", nil, 0, xerrors.Wrap(errSSOTSyntax, fmt.Sprintf("%d-%d 行目: %s（要素は二重引用符の文字列をカンマで区切る）", start+1, i+1, key))
	}
	var values []string
	for _, q := range quotedRe.FindAllStringSubmatch(inner, -1) {
		values = append(values, q[1])
	}
	return key, values, i, nil
}

func assign(cur section, key string, values []string) error {
	if err := requireUnique(values); err != nil {
		return xerrors.Wrap(err, key)
	}
	c, j := cur.class, cur.job
	switch {
	case c != nil && key == "includes":
		c.includes = values
	case c != nil && key == "hosts":
		c.hosts = values
	case c != nil && key == "ips":
		c.ips = values
	case j != nil && key == "classes":
		j.classes = values
	case j != nil && key == "extra":
		j.extra = values
	case j != nil && key == "egress_policy" && len(values) == 1:
		j.policy = values[0]
	case cur.root != nil && key == "jobs":
		cur.root.jobs = values
	case cur.typ != nil && key == "hosts":
		cur.typ.hosts = values
	default:
		return xerrors.Wrap(errSSOTSyntax, "未知のキー "+key)
	}
	return nil
}

// requireUnique は同一リスト内の重複を弾く。重複は「どちらを直せばよいか」が読めなくなる。
func requireUnique(values []string) error {
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			return xerrors.Wrap(errSSOTDuplicate, v)
		}
		seen[v] = true
	}
	return nil
}

// validateDomain は bullfrog の allowed-domains へ書ける宛先かを判定する。
func validateDomain(d string) error {
	if !domainRe.MatchString(d) {
		return xerrors.Wrap(errSSOTDomain, d)
	}
	return nil
}

// validateIP は loopbackPrefix 内の IPv4 アドレスまたは IPv4 CIDR かを判定する。
func validateIP(v string) error {
	if a, err := netip.ParseAddr(v); err == nil && a.Is4() && loopbackPrefix.Contains(a) {
		return nil
	}
	if p, err := netip.ParsePrefix(v); err == nil && p.Addr().Is4() &&
		p.Bits() >= loopbackPrefix.Bits() && loopbackPrefix.Contains(p.Addr()) {
		return nil
	}
	return xerrors.Wrap(errSSOTIP, v)
}

// validate は SSOT 単体で判定できる不整合を返す。
func (s *ssot) validate() error {
	if base, ok := s.classes["base"]; ok {
		s.baseHosts = base.hosts
		s.baseIPs = base.ips
	}
	for name, c := range s.classes {
		if len(c.ips) > 0 && name != "base" {
			return xerrors.Wrap(errSSOTIPScope, name)
		}
		for _, ip := range c.ips {
			if err := validateIP(ip); err != nil {
				return xerrors.Wrap(err, name)
			}
		}
		for _, h := range c.hosts {
			if err := validateDomain(h); err != nil {
				return xerrors.Wrap(err, name)
			}
		}
		for _, inc := range c.includes {
			if _, ok := s.classes[inc]; !ok {
				return xerrors.Wrap(errSSOTUnknownClass, name+" includes "+inc)
			}
		}
	}
	for _, key := range s.jobOrder {
		if err := s.validateJob(key, s.jobs[key]); err != nil {
			return err
		}
	}
	for _, name := range s.rootOrder {
		if err := s.validateRoot(name, s.roots[name]); err != nil {
			return err
		}
	}
	for _, name := range s.typeOrder {
		prefix := typePrefixRe.MatchString(name)
		if !prefix && !typeNameRe.MatchString(name) {
			return xerrors.Wrap(errSSOTTypeName, name)
		}
		if prefix && len(s.types[name].hosts) == 0 {
			return xerrors.Wrap(errSSOTTypeHosts, name)
		}
		for _, h := range s.types[name].hosts {
			if err := validateDomain(h); err != nil {
				return xerrors.Wrap(err, "type "+name)
			}
		}
	}
	return nil
}

// validateRoot は root のパスがリポジトリの外や別名を指さないこと、jobs が宣言済みのジョブであることを見る。
func (s *ssot) validateRoot(name string, r *rootSpec) error {
	if !filepath.IsLocal(name) || path.Clean(name) != name || name == "." || strings.Contains(name, `\`) {
		return xerrors.Wrap(errSSOTRootPath, name)
	}
	for _, key := range r.jobs {
		if _, ok := s.jobs[key]; !ok {
			return xerrors.Wrap(errSSOTUnknownJob, name+" jobs "+key)
		}
	}
	return nil
}

func (s *ssot) validateJob(key string, j *jobSpec) error {
	switch j.policy {
	case "", policyBlock:
		j.policy = policyBlock
	case policyAudit:
		if len(j.classes) > 0 || len(j.extra) > 0 {
			return xerrors.Wrap(errSSOTPolicy, key+": audit は許可リストを持ちません")
		}
	default:
		return xerrors.Wrap(errSSOTPolicy, key+": "+j.policy)
	}
	for _, cn := range j.classes {
		if cn == "base" {
			return xerrors.Wrap(errSSOTBaseClass, key)
		}
		if _, ok := s.classes[cn]; !ok {
			return xerrors.Wrap(errSSOTUnknownClass, key+" classes "+cn)
		}
	}
	for _, h := range j.extra {
		if err := validateDomain(h); err != nil {
			return xerrors.Wrap(err, key)
		}
	}
	_, err := s.hostsFor(key)
	return err
}

// hostsFor は 1 ジョブの許可リストを算出する。base → classes（includes 展開込み）→ extra の順で、
// 重複は初出を残す。並びを SSOT の宣言順に固定しているため、生成物の差分はレビューで読める。
func (s *ssot) hostsFor(key string) ([]string, error) {
	j := s.jobs[key]
	if j.policy == policyAudit {
		return nil, nil
	}
	out := &orderedHosts{seen: map[string]bool{}}
	out.add(s.baseHosts)
	for _, cn := range j.classes {
		if err := s.expandClass(cn, out, map[string]bool{}); err != nil {
			return nil, xerrors.Wrap(err, key)
		}
	}
	out.add(j.extra)
	return out.list, nil
}

// expandClass は includes を先に展開してからクラス自身の hosts を足す。visiting は循環検出用。
func (s *ssot) expandClass(name string, out *orderedHosts, visiting map[string]bool) error {
	if visiting[name] {
		return xerrors.Wrap(errSSOTClassCycle, name)
	}
	visiting[name] = true
	defer delete(visiting, name)

	c, ok := s.classes[name]
	if !ok {
		return xerrors.Wrap(errSSOTUnknownClass, name)
	}
	for _, inc := range c.includes {
		if err := s.expandClass(inc, out, visiting); err != nil {
			return err
		}
	}
	out.add(c.hosts)
	return nil
}

func (o *orderedHosts) add(hosts []string) {
	for _, h := range hosts {
		if o.seen[h] {
			continue
		}
		o.seen[h] = true
		o.list = append(o.list, h)
	}
}

// scanJobs は 1 ファイルを job に割り、steps を持つ job ごとに bullfrog step の with: の範囲を返す。
// reusable workflow の呼び出しは runner を持たないので対象外。それ以外で steps を読み取れない job は、
// bullfrog の有無を判定できないので拒む。
// step の中で見つけた件数を、ファイル全体で名指しされた件数と突き合わせる（ADR-0702 決定15）。
func scanJobs(name, data string) ([]target, error) {
	split := workflow.SplitJobs(data)
	if !split.Found || len(split.Jobs) == 0 {
		return nil, xerrors.Wrap(errWorkflowNoJobs, name)
	}
	if l, ok := unclaimedLine(data, split); ok {
		return nil, xerrors.Wrap(errJobUnparsed, fmt.Sprintf("%s %d 行目: %q", name, l.Number, l.Text))
	}
	var targets []target
	total := mentionsBullfrog(split.Preamble)
	for _, job := range split.Jobs {
		total += mentionsBullfrog(job.Lines)
		key := name + ":" + job.ID
		if i := slices.IndexFunc(job.Lines, func(l workflow.Line) bool { return shallowLineRe.MatchString(l.Text) }); i >= 0 {
			return nil, xerrors.Wrap(errJobUnparsed, fmt.Sprintf("%s %d 行目: %q", key, job.Lines[i].Number, job.Lines[i].Text))
		}
		steps := workflow.SplitSteps(job)
		if len(steps) == 0 {
			if slices.ContainsFunc(job.Lines, func(l workflow.Line) bool { return reusableRe.MatchString(l.Text) }) {
				continue
			}
			return nil, xerrors.Wrap(errJobUnparsed, fmt.Sprintf("%s %d 行目", key, job.Number))
		}
		var found []int
		for i, st := range steps {
			if mentionsBullfrog(st.Lines) > 0 {
				found = append(found, i)
			}
		}
		switch {
		case len(found) == 0:
			return nil, xerrors.Wrap(errStepMissing, key)
		case len(found) > 1:
			return nil, xerrors.Wrap(errStepDuplicate, key)
		case found[0] != 0:
			return nil, xerrors.Wrap(errStepNotFirst, fmt.Sprintf("%s %d 行目", key, steps[found[0]].Number))
		}
		t, err := checkStepShape(steps[0])
		if err != nil {
			return nil, xerrors.Wrap(err, key)
		}
		t.jobKey = key
		targets = append(targets, t)
	}
	if total != len(targets) {
		return nil, xerrors.Wrap(errStepMalformed,
			fmt.Sprintf("%s: step の外で %s が名指しされています（step 内 %d 件 / 全体 %d 件）", name, bullfrogAction, len(targets), total))
	}
	return targets, nil
}

// unclaimedLine は、切り出しがどの job にも preamble にも割り当てなかった行のうち、`jobs:` の行・空行・
// コメント以外のものを返す。最初の job の見出しが読めないと、その本文はどこにも残らない。
func unclaimedLine(data string, split workflow.Split) (workflow.Line, bool) {
	claimed := map[int]bool{}
	for _, l := range split.Preamble {
		claimed[l.Number] = true
	}
	for _, job := range split.Jobs {
		claimed[job.Number] = true
		for _, l := range job.Lines {
			claimed[l.Number] = true
		}
	}
	jobsSeen := false
	for i, text := range strings.Split(data, "\n") {
		if claimed[i+1] || strings.TrimSpace(text) == "" || commentRe.MatchString(text) {
			continue
		}
		if !jobsSeen && strings.HasPrefix(text, "jobs:") {
			jobsSeen = true
			continue
		}
		return workflow.Line{Number: i + 1, Text: text}, true
	}
	return workflow.Line{}, false
}

// mentionsBullfrog はコメント行を除いて bullfrog を名指しする行を数える。
func mentionsBullfrog(lines []workflow.Line) int {
	n := 0
	for _, l := range lines {
		if !commentRe.MatchString(l.Text) && strings.Contains(l.Text, bullfrogAction) {
			n++
		}
	}
	return n
}

// checkStepShape は bullfrog step が `- name:` → `uses:` → 任意の `with:` だけから成ることを確かめ、
// 差し替える with: の範囲を返す。それ以外の形を拒む理由は scripts/README.md の egress 行が持つ。
// step の後ろに続く空行とコメントは次の step のものなので残す。
func checkStepShape(st workflow.Step) (target, error) {
	ls := st.Lines
	malformed := func(l workflow.Line) error {
		return xerrors.Wrap(errStepMalformed, fmt.Sprintf("%d 行目: %q", l.Number, l.Text))
	}
	if len(ls) < 2 || !stepNameRe.MatchString(ls[0].Text) {
		return target{}, malformed(ls[0])
	}
	if !stepUsesRe.MatchString(ls[1].Text) {
		return target{}, malformed(ls[1])
	}
	i := 2
	t := target{start: ls[1].Number, end: ls[1].Number}
	if i < len(ls) && stepWithRe.MatchString(ls[i].Text) {
		i++
		for i < len(ls) && strings.HasPrefix(ls[i].Text, keyIndent) {
			i++
		}
		t.end = ls[i-1].Number
	}
	for ; i < len(ls); i++ {
		if strings.TrimSpace(ls[i].Text) != "" && !commentRe.MatchString(ls[i].Text) {
			return target{}, malformed(ls[i])
		}
	}
	return t, nil
}

// renderWith は 1 ジョブ分の with: ブロックを返す。enable-sudo: false は全ジョブで書く（ADR-0702 決定21）。
// dns-policy は既定値と同じでも書き、上流の既定が変わっても名前解決の制限が黙って外れないようにする。
func renderWith(policy string, ips, hosts []string) []string {
	out := []string{withIndent + "with:", keyIndent + "egress-policy: " + policy}
	if policy == policyBlock {
		out = append(out, keyIndent+"dns-policy: allowed-domains-only")
	}
	out = append(out, keyIndent+"enable-sudo: false")
	if policy == policyAudit {
		return out
	}
	if len(ips) > 0 {
		out = append(out, keyIndent+"allowed-ips: |")
		for _, ip := range ips {
			out = append(out, itemIndent+ip)
		}
	}
	out = append(out, keyIndent+"allowed-domains: |")
	for _, h := range hosts {
		out = append(out, itemIndent+h)
	}
	return out
}

// applyOrCheck は SSOT を正本に bullfrog の with: を書き換える。dryRun=true は書かない。
func applyOrCheck(root string, dryRun bool) error {
	raw, err := os.ReadFile(filepath.Join(root, ssotFile)) //nolint:gosec // path is cwd + literal filename
	if err != nil {
		return xerrors.Wrap(err, "read "+ssotFile)
	}
	s, err := parseSSOT(string(raw))
	if err != nil {
		return err
	}
	if err := s.checkEndpoints(root); err != nil {
		return err
	}
	var files []string
	for _, ext := range []string{"*.yaml", "*.yml"} {
		matched, err := filepath.Glob(filepath.Join(root, ".github", "workflows", ext))
		if err != nil {
			return xerrors.Wrap(err, "glob workflows")
		}
		files = append(files, matched...)
	}
	sort.Strings(files)

	changes, used, err := s.planFiles(files)
	if err != nil {
		return err
	}
	if len(used) == 0 {
		return xerrors.Wrap(errNoTargets, filepath.Join(".github", "workflows"))
	}
	if orphans := orphanJobs(s, used); len(orphans) > 0 {
		return xerrors.Wrap(errJobOrphan, strings.Join(orphans, ", ")+"（"+ssotFile+" の該当エントリを削除してください）")
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	if err := s.checkCIRoots(root, names); err != nil {
		return err
	}
	return writeChanges(root, changes, dryRun)
}

// planFiles は全 workflow を読み切り、書き換え後の内容と参照済みジョブキーを確定させる。
// 書き出す前に全て確定させる方針は scripts/README.md の Test Strategy が持つ。
func (s *ssot) planFiles(files []string) (map[string]string, map[string]bool, error) {
	changes := map[string]string{}
	used := map[string]bool{}
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // path from cwd glob
		if err != nil {
			return nil, nil, xerrors.Wrap(err, "read "+f)
		}
		out, err := s.rewriteFile(filepath.Base(f), string(data), used)
		if err != nil {
			return nil, nil, err
		}
		if out != string(data) {
			changes[f] = out
		}
	}
	return changes, used, nil
}

// rewriteFile は 1 ファイルの bullfrog step の with: を SSOT 由来の内容へ差し替えた本文を返す。
func (s *ssot) rewriteFile(name, data string, used map[string]bool) (string, error) {
	targets, err := scanJobs(name, data)
	if err != nil {
		return "", err
	}
	for _, t := range targets {
		if err := s.checkJob(t.jobKey); err != nil {
			return "", err
		}
		used[t.jobKey] = true
	}
	lines := strings.Split(data, "\n")
	// 後ろから差し替えて、先行する step の行番号を保つ。
	for _, t := range slices.Backward(targets) {
		hosts, err := s.hostsFor(t.jobKey)
		if err != nil {
			return "", err
		}
		block := renderWith(s.jobs[t.jobKey].policy, s.baseIPs, hosts)
		lines = append(lines[:t.start:t.start], append(block, lines[t.end:]...)...)
	}
	return strings.Join(lines, "\n"), nil
}

func (s *ssot) checkJob(key string) error {
	if _, ok := s.jobs[key]; !ok {
		return xerrors.Wrap(errJobMissing, key+"（"+ssotFile+" へ追加してください）")
	}
	return nil
}

// orphanJobs は SSOT にあるがどの workflow にも現れないジョブキーを返す。
// これをエラーにするのは fail-closed の一環（scripts/README.md の egress 行）。
func orphanJobs(s *ssot, used map[string]bool) []string {
	var orphans []string
	for _, key := range s.jobOrder {
		if !used[key] {
			orphans = append(orphans, key)
		}
	}
	sort.Strings(orphans)
	return orphans
}

// writeChanges は確定した書き換えを反映する。dryRun=true は差分の一覧を非ゼロ終了で報告する。
func writeChanges(root string, changes map[string]string, dryRun bool) error {
	paths := make([]string, 0, len(changes))
	for f := range changes {
		paths = append(paths, f)
	}
	sort.Strings(paths)

	if dryRun {
		if len(paths) > 0 {
			rels := make([]string, 0, len(paths))
			for _, p := range paths {
				rels = append(rels, relTo(root, p))
			}
			return xerrors.Wrap(errEgressDrift, "make egress-apply してコミットしてください: "+strings.Join(rels, ", "))
		}
		log.Printf("✅ 全ジョブの bullfrog の with: が %s 通りです", ssotFile)

		return nil
	}
	if err := atomicwrite.Apply(changes, filePerm); err != nil {
		return err
	}

	for _, f := range paths {
		log.Printf("  updated %s", relTo(root, f))
	}
	log.Printf("✅ %d ファイルへ反映しました", len(paths))

	return nil
}

func relTo(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}
