// Package cientry は CI の入口の種別と種別ごとの属性の登録簿と、.makefiles/terraform/operation/ci.mk の
// 入口の読み取りです。
//
// 入口は `ci-<種別>-<env>-<root>` のターゲットで、その振る舞いは ci.mk の `define CI_<種別>` が持ちます。
// 種別を足すときは、ci.mk の define と入口に加えて、登録簿に1件足します。ci.mk に在って登録簿に無い種別と
// role のずれは [Parse] が、登録簿に在って ci.mk に入口が無い種別は [CheckDeclared] が落とします。
//
// 登録簿の role と、deployment-identity が作る role（[TerraformRolesFile] の `local.roles`）との過不足は
// [CheckTerraformRoles] が落とします。
package cientry

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// Makefile は CI の入口を持つ Makefile の、リポジトリの直下からのパスです。
const Makefile = ".makefiles/terraform/operation/ci.mk"

// 種別の名前。
const (
	Plan   = "plan"
	Apply  = "apply"
	Unlock = "unlock"
	Hold   = "hold"
)

// 種別の起動として許すイベント。
const (
	EventPullRequest      = "pull_request"
	EventPush             = "push"
	EventWorkflowDispatch = "workflow_dispatch"
)

// Root は入口が触れる root の種類です（ADR-0604 決定5）。
type Root string

// root の種類。
const (
	RootIdentity Root = "identity"
	RootWorkload Root = "workload"
)

// Dir は、env の root のディレクトリを、リポジトリの直下からの相対パスで返します（ADR-0604 決定5）。
func (r Root) Dir(env string) string {
	if r == RootIdentity {
		return path.Join("env", env, "identity")
	}

	return path.Join("env", env)
}

// Lock は入口が state の lock をどう扱うかです。
type Lock int

// lock の扱い。
const (
	LockSkip    Lock = iota + 1 // lock を取らない（-lock=false）
	LockRelease                 // terraform が lock を取り、終わりに外す
	LockKeep                    // terraform に lock を取らせ、外さずに残す
	LockRemove                  // 残った lock を外す。terraform を呼ばない
)

// Kind は入口の種別1件と、その属性です。
type Kind struct {
	// Name は `[a-z]+` の1語。入口は ci-<Name>-<env>-<root>、define は CI_<NAME>。
	Name      string
	Terraform bool
	Lock      Lock
	// Events は起動として許すイベント。入口を走らせる workflow の起動は、この空でない部分集合に限る。
	Events []string
	// IdentityRole と WorkloadRole は、identity root と workload root の入口が取る role
	// （ADR-0604 決定3・4・8）。
	IdentityRole string
	WorkloadRole string
}

// Role は root の種類 r の入口が取る role を返します。知らない root の種類には空文字を返します。
func (k Kind) Role(r Root) string {
	switch r {
	case RootIdentity:
		return k.IdentityRole
	case RootWorkload:
		return k.WorkloadRole
	}
	return ""
}

// Entry は ci.mk の入口1件です。
type Entry struct {
	// Target は ci-<種別>-<env>-<root>。
	Target string
	Kind   Kind
	Env    string
	Root   Root
	// Role は入口の TF_LOCAL_PROFILE が名指す role。
	Role string
	// Line はルール行の行番号（1始まり）。
	Line int
}

var (
	// ErrParse は ci.mk の入口の形を読めない場合のエラー。
	ErrParse = xerrors.New("ci.mk の CI の入口を解釈できません")
	// ErrKindUnknown は ci.mk の入口の種別が登録簿に無い場合のエラー。
	ErrKindUnknown = xerrors.New("ci.mk の入口の種別が登録簿にありません")
	// ErrRole は ci.mk の入口の role、または拒否を確かめる role の一覧が登録簿と一致しない場合のエラー。
	ErrRole = xerrors.New("ci.mk の入口の role が登録簿と一致しません")
	// ErrKindAbsent は登録簿の種別の入口が ci.mk に1件も無い場合のエラー。
	ErrKindAbsent = xerrors.New("登録簿の種別の入口が ci.mk にありません")

	errRegistry = xerrors.New("CI の入口の種別の登録簿が不正です")
)

const envWord = `[a-z0-9]+(?:-[a-z0-9]+)*`

var (
	kindNameRe = regexp.MustCompile(`^[a-z]+$`)
	roleRe     = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)
	ruleRe     = regexp.MustCompile(`^(ci-[a-z0-9-]+):$`)
	callRe     = regexp.MustCompile(`^\t@\$\(call CI_([A-Z]+),(` + envWord + `),(identity|workload)\)$`)
	// callWordsRe は種別の名前が1語でない呼び出し。
	callWordsRe = regexp.MustCompile(`^\t@\$\(call CI_[A-Z]+_[A-Z_]+,`)
	profileRe   = regexp.MustCompile(`^(ci-[a-z0-9-]+): TF_LOCAL_PROFILE := \$\(call CI_PROFILE,(` + envWord + `),([a-z-]+)\)$`)
	defineRe    = regexp.MustCompile(`^define CI_([A-Z_]+)$`)
)

// callPrefix は入口の呼び出しの行の字面。読めた入口の件数と突き合わせる（ADR-0702 決定15）。
const callPrefix = "\t@$(call CI_"

// nonEntries は ci- で始まるが入口ではないターゲット。root を持たない。
var nonEntries = []string{"ci-aws-credentials", "ci-assume-denied"}

type registry []Kind

var std = func() registry {
	r, err := newRegistry(
		Kind{Name: Plan, Terraform: true, Lock: LockSkip, Events: []string{EventPullRequest},
			IdentityRole: "plan", WorkloadRole: "plan"},
		Kind{Name: Apply, Terraform: true, Lock: LockRelease, Events: []string{EventPush, EventWorkflowDispatch},
			IdentityRole: "apply-identity", WorkloadRole: "apply"},
		Kind{Name: Unlock, Terraform: false, Lock: LockRemove, Events: []string{EventWorkflowDispatch},
			IdentityRole: "unlock-identity", WorkloadRole: "unlock"},
		Kind{Name: Hold, Terraform: true, Lock: LockKeep, Events: []string{EventWorkflowDispatch},
			IdentityRole: "apply-identity", WorkloadRole: "apply"},
	)
	if err != nil {
		panic(err)
	}
	return r
}()

// newRegistry は種別の一覧を検証して登録簿にします。
func newRegistry(kinds ...Kind) (registry, error) {
	if len(kinds) == 0 {
		return nil, xerrors.Wrap(errRegistry, "種別が1件もありません")
	}
	var roles []string
	for i, k := range kinds {
		if !kindNameRe.MatchString(k.Name) {
			return nil, xerrors.Wrapf(errRegistry, "種別 %q: 名前は1語（[a-z]+）に限ります", k.Name)
		}
		if slices.ContainsFunc(kinds[:i], func(p Kind) bool { return p.Name == k.Name }) {
			return nil, xerrors.Wrapf(errRegistry, "種別 %s が重複しています", k.Name)
		}
		if k.Lock < LockSkip || k.Lock > LockRemove {
			return nil, xerrors.Wrapf(errRegistry, "種別 %s: lock の扱い %d を知りません", k.Name, k.Lock)
		}
		if len(k.Events) == 0 {
			return nil, xerrors.Wrapf(errRegistry, "種別 %s: 起動のイベントがありません", k.Name)
		}
		for j, e := range k.Events {
			if e != EventPullRequest && e != EventPush && e != EventWorkflowDispatch {
				return nil, xerrors.Wrapf(errRegistry, "種別 %s: 起動のイベント %q を知りません", k.Name, e)
			}
			if slices.Contains(k.Events[:j], e) {
				return nil, xerrors.Wrapf(errRegistry, "種別 %s: 起動のイベント %s が重複しています", k.Name, e)
			}
		}
		for _, role := range []string{k.IdentityRole, k.WorkloadRole} {
			if !roleRe.MatchString(role) {
				return nil, xerrors.Wrapf(errRegistry, "種別 %s: role %q を読めません", k.Name, role)
			}
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	// environment の名前 <env>-<role> を role の接尾辞で切るとき、切り方が role の順序に依存しないための条件。
	for _, a := range roles {
		for _, b := range roles {
			if a != b && strings.HasSuffix("-"+a, "-"+b) {
				return nil, xerrors.Wrapf(errRegistry, "role %s の末尾が role %s と同じです", a, b)
			}
		}
	}
	out := slices.Clone(kinds)
	for i := range out {
		out[i].Events = slices.Clone(out[i].Events)
	}
	return out, nil
}

// Kinds は登録簿の種別を、登録簿の順で返します。返り値を書き換えても登録簿は変わりません。
func Kinds() []Kind {
	out := slices.Clone(std)
	for i := range out {
		out[i].Events = slices.Clone(out[i].Events)
	}
	return out
}

// Lookup は名前が name の種別を返します。無ければ偽を返します。
func Lookup(name string) (Kind, bool) {
	for _, k := range std {
		if k.Name == name {
			k.Events = slices.Clone(k.Events)
			return k, true
		}
	}
	return Kind{}, false
}

// Roles は登録簿の種別が取る role を、重複を除いて返します。順は種別の順に identity、workload です。
func Roles() []string {
	return std.roles()
}

func (r registry) roles() []string {
	var out []string
	for _, k := range r {
		for _, role := range []string{k.IdentityRole, k.WorkloadRole} {
			if !slices.Contains(out, role) {
				out = append(out, role)
			}
		}
	}
	return out
}

// DeniedRoles は、pull_request で起動する種別が取る role を除いた登録簿の role を、重複を除いて登録簿の順で
// 返します。plan の job は、同じ OIDC token でこれらの assume がすべて拒否されることを確かめます
// （.github/workflows/README.md の plan の job）。
func DeniedRoles() []string {
	return std.deniedRoles()
}

func (r registry) deniedRoles() []string {
	var allowed []string
	for _, k := range r {
		if slices.Contains(k.Events, EventPullRequest) {
			allowed = append(allowed, k.IdentityRole, k.WorkloadRole)
		}
	}

	return slices.DeleteFunc(r.roles(), func(role string) bool { return slices.Contains(allowed, role) })
}

// SplitEnvRoot は入口の名前の `<env>-<root>` を、末尾の -identity か -workload で切ります。
// どちらの接尾辞でもないか、env が空なら偽を返します。
func SplitEnvRoot(envRoot string) (string, Root, bool) {
	for _, r := range []Root{RootIdentity, RootWorkload} {
		if env, ok := strings.CutSuffix(envRoot, "-"+string(r)); ok {
			if env == "" {
				return "", "", false
			}
			return env, r, true
		}
	}
	return "", "", false
}

// MustPattern は、kinds の名前のどれかを prefix と suffix で挟んだ正規表現を組みます。kinds が空か、名前が
// 登録簿の種別の名前の形（[a-z]+ の1語）でない種別があれば panic します。引き損ねた種別の零値 Kind{} から、
// 何にも当たらない正規表現を組まないためです。
func MustPattern(prefix, suffix string, kinds ...Kind) *regexp.Regexp {
	if len(kinds) == 0 {
		panic("cientry.MustPattern: 種別がありません")
	}
	names := make([]string, len(kinds))
	for i, k := range kinds {
		if !kindNameRe.MatchString(k.Name) {
			panic(fmt.Sprintf("cientry.MustPattern: 種別の名前 %q は [a-z]+ の1語ではありません", k.Name))
		}
		names[i] = k.Name
	}
	return regexp.MustCompile(prefix + `(?:` + strings.Join(names, "|") + `)` + suffix)
}

// Parse は ci.mk の入口を、書かれた順で返します。
//
// ci- で始まるターゲットは、入口か、入口でないと名指したもの（ci-aws-credentials・ci-assume-denied）の
// どちらかでなければなりません。入口は、`.PHONY` の行・`TF_LOCAL_PROFILE` の行・ルール行を1つずつ持ち、
// ルール行の次の行が `$(call CI_<種別>,<env>,<identity|workload>)` で、その種別の define を持ちます。
// 形を読めなければ [ErrParse]、種別が登録簿に無ければ [ErrKindUnknown]、TF_LOCAL_PROFILE の role が
// 登録簿と一致しなければ [ErrRole] を返します。入口が1件も無ければ [ErrParse] です。
func Parse(data string) ([]Entry, error) {
	lines := strings.Split(data, "\n")
	entries, err := parseRules(lines)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, xerrors.Wrap(ErrParse, "入口が 1 件もありません")
	}
	if err := checkTargets(lines, entries); err != nil {
		return nil, err
	}
	if err := checkDefines(lines, entries); err != nil {
		return nil, err
	}
	calls := 0
	for _, l := range lines {
		if strings.HasPrefix(l, callPrefix) {
			calls++
		}
	}
	if calls != len(entries) {
		return nil, xerrors.Wrap(ErrParse, fmt.Sprintf("呼び出しの行 %d 件 / 読めた入口 %d 件", calls, len(entries)))
	}
	return entries, nil
}

func parseRules(lines []string) ([]Entry, error) {
	var entries []Entry
	for i, l := range lines {
		m := ruleRe.FindStringSubmatch(l)
		if m == nil || slices.Contains(nonEntries, m[1]) {
			continue
		}
		target := m[1]
		if slices.ContainsFunc(entries, func(e Entry) bool { return e.Target == target }) {
			return nil, xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s が重複しています", i+1, target))
		}
		var next string
		if i+1 < len(lines) {
			next = lines[i+1]
		}
		c := callRe.FindStringSubmatch(next)
		if c == nil {
			reason := ""
			if callWordsRe.MatchString(next) {
				reason = "。種別は1語（[a-z]+）に限ります"
			}
			return nil, xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s の次の行が $(call CI_<種別>,<env>,<identity|workload>) ではありません%s", i+1, target, reason))
		}
		name, env, root := strings.ToLower(c[1]), c[2], Root(c[3])
		if want := "ci-" + name + "-" + env + "-" + string(root); target != want {
			return nil, xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s の呼び出しは %s の入口です", i+1, target, want))
		}
		k, ok := Lookup(name)
		if !ok {
			return nil, xerrors.Wrap(ErrKindUnknown, fmt.Sprintf("%d 行目: %s（種別 %s）", i+1, target, name))
		}
		entries = append(entries, Entry{Target: target, Kind: k, Env: env, Root: root, Line: i + 1})
	}
	return entries, nil
}

// checkTargets は ci- で始まるターゲットの行と `.PHONY` の行を入口と突き合わせ、入口ごとの role を埋めます。
func checkTargets(lines []string, entries []Entry) error {
	index := map[string]int{}
	for i, e := range entries {
		index[e.Target] = i
	}
	known := func(target string) bool {
		_, ok := index[target]
		return ok || slices.Contains(nonEntries, target)
	}
	phony := map[string]int{}
	profiles := map[string]int{}
	for n, l := range lines {
		if rest, ok := strings.CutPrefix(l, ".PHONY:"); ok {
			body, _, _ := strings.Cut(rest, " ##")
			for _, f := range strings.Fields(body) {
				if !strings.HasPrefix(f, "ci-") {
					continue
				}
				if !known(f) {
					return xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: .PHONY の %s は入口でも、入口でないと名指したターゲットでもありません", n+1, f))
				}
				phony[f]++
			}
			continue
		}
		if !strings.HasPrefix(l, "ci-") {
			continue
		}
		head, rest, ok := strings.Cut(l, ":")
		if !ok {
			return xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: ci- で始まる行を読めません", n+1))
		}
		target := strings.TrimSpace(head)
		if !known(target) {
			return xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s は入口でも、入口でないと名指したターゲットでもありません", n+1, target))
		}
		if !strings.HasPrefix(strings.TrimSpace(rest), "TF_LOCAL_PROFILE") {
			continue
		}
		i, entry := index[target]
		m := profileRe.FindStringSubmatch(l)
		if !entry || m == nil {
			return xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s の TF_LOCAL_PROFILE は <入口>: TF_LOCAL_PROFILE := $(call CI_PROFILE,<env>,<role>) ではありません", n+1, target))
		}
		e := &entries[i]
		if m[2] != e.Env {
			return xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s の TF_LOCAL_PROFILE の env %s が入口の env %s と違います", n+1, target, m[2], e.Env))
		}
		if want := e.Kind.Role(e.Root); m[3] != want {
			return xerrors.Wrap(ErrRole, fmt.Sprintf("%d 行目: %s の role %s（登録簿は %s）", n+1, target, m[3], want))
		}
		e.Role = m[3]
		profiles[target]++
	}
	for _, e := range entries {
		if phony[e.Target] != 1 {
			return xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s の .PHONY の行が %d 行です（1行を期待）", e.Line, e.Target, phony[e.Target]))
		}
		if profiles[e.Target] != 1 {
			return xerrors.Wrap(ErrParse, fmt.Sprintf("%d 行目: %s の TF_LOCAL_PROFILE の行が %d 行です（1行を期待）", e.Line, e.Target, profiles[e.Target]))
		}
	}
	return nil
}

// checkDefines は、入口が使う種別ごとに define CI_<種別> がちょうど1つ在ることを見ます。
// 無い define の $(call) は空に展開され、入口は何もせずに成功します。
func checkDefines(lines []string, entries []Entry) error {
	defines := map[string]int{}
	for _, l := range lines {
		if m := defineRe.FindStringSubmatch(l); m != nil {
			defines[m[1]]++
		}
	}
	for _, e := range entries {
		if n := defines[strings.ToUpper(e.Kind.Name)]; n != 1 {
			return xerrors.Wrap(ErrParse, fmt.Sprintf("%s の define CI_%s が %d 個です（1個を期待）", e.Target, strings.ToUpper(e.Kind.Name), n))
		}
	}
	return nil
}

// ParseRepo はリポジトリ repo の [Makefile] を読んで [Parse] します。
func ParseRepo(repo string) ([]Entry, error) {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	data, err := r.ReadFile(Makefile)
	if err != nil {
		return nil, xerrors.Wrap(err, "read "+Makefile)
	}
	entries, err := Parse(string(data))
	if err != nil {
		return nil, xerrors.Wrap(err, Makefile)
	}
	return entries, nil
}

// CheckDeclared は、登録簿のすべての種別が entries に入口を1件以上持つことを見ます。欠けた種別があれば
// [ErrKindAbsent] を返します。
func CheckDeclared(entries []Entry) error {
	return std.checkDeclared(entries)
}

func (r registry) checkDeclared(entries []Entry) error {
	if len(r) == 0 {
		return xerrors.Wrap(errRegistry, "種別が1件もありません")
	}
	var missing []string
	for _, k := range r {
		if !slices.ContainsFunc(entries, func(e Entry) bool { return e.Kind.Name == k.Name }) {
			missing = append(missing, k.Name)
		}
	}
	if len(missing) > 0 {
		return xerrors.Wrap(ErrKindAbsent, strings.Join(missing, ", "))
	}
	return nil
}
