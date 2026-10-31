// secret-suppression-lint は、秘密の走査（gitleaks・trufflehog・Trivy の secret scanning）の抑止が
// fingerprint か path の単位であり、各エントリが理由と撤回条件を伴うことを検査します
// （ADR-0302 決定9・10）。
//
// 解いている問題:
//
// **抑止は検出を黙らせる口であり、広げても何も赤くならない。** path の正規表現がディレクトリへ
// 広がった日も、理由の無い fingerprint が積まれた日も、走査は緑のままです。単位と理由を検める
// 検査が無ければ、ADR の「理由を伴う path 単位の抑止」は書いた人の自制だけで保たれます。
//
// 見られるのは、理由の欄が在り撤回条件の語を含むことまでです。理由が正しいかは見られません。
//
// 対象のファイルは固定で持ちます。呼び出し側が列挙すると、列挙し忘れた抑止が黙って検査の外に
// 残ります。どのファイルも、知らない書き方の行はエラーにします（ADR-0702 決定14）。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 秘密の走査が読む抑止のファイルを足したときは、ここにも足す。
const (
	gitleaksIgnore   = ".gitleaksignore"
	gitleaksConfig   = ".gitleaks.toml"
	trufflehogPaths  = ".trufflehog-exclude-paths.txt"
	trufflehogHist   = ".trufflehog-exclude-paths-history.txt"
	trivyIgnore      = ".trivyignore.yaml"
	revocationMarker = "撤回"
)

var (
	errUnreadable = xerrors.New("検査対象を読み取れません")
	errNoTracked  = xerrors.New("追跡ファイルの一覧が0件です")
	errNoCommits  = xerrors.New("commit の一覧が0件です")
	errViolations = xerrors.New("違反がある")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Printf("❌ secret-suppression-lint: %v", err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errUnreadable), xerrors.Is(err, errNoTracked), xerrors.Is(err, errNoCommits):
		return 2
	default:
		return 1
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("secret-suppression-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", ".", "抑止のファイルを置くリポジトリ直下")
	list := fs.String("tracked", "", "追跡ファイルの一覧（1行1パス。git ls-files の出力）")
	commitList := fs.String("commits", "", "commit の一覧（1行1つ。git rev-list --all の出力）")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	tracked, err := readSet(*list, errNoTracked)
	if err != nil {
		return err
	}
	commits, err := readSet(*commitList, errNoCommits)
	if err != nil {
		return err
	}

	read := func(name string) (string, error) {
		raw, err := os.ReadFile(filepath.Clean(filepath.Join(*root, name)))
		if err != nil {
			return "", xerrors.Wrapf(errUnreadable, "%s", name)
		}
		return string(raw), nil
	}

	var findings []lintreport.Finding
	entries := 0
	checks := []struct {
		name  string
		check func(name, body string) ([]lintreport.Finding, int)
	}{
		{gitleaksIgnore, func(n, b string) ([]lintreport.Finding, int) { return checkGitleaksIgnore(n, b, commits) }},
		{gitleaksConfig, func(n, b string) ([]lintreport.Finding, int) { return checkGitleaksConfig(n, b, tracked) }},
		{trufflehogPaths, func(n, b string) ([]lintreport.Finding, int) { return checkPathRegexes(n, b, tracked) }},
		{trufflehogHist, func(n, b string) ([]lintreport.Finding, int) { return checkPathRegexes(n, b, nil) }},
		{trivyIgnore, func(n, b string) ([]lintreport.Finding, int) { return checkTrivySecrets(n, b, tracked) }},
	}
	for _, c := range checks {
		body, err := read(c.name)
		if err != nil {
			return err
		}
		f, n := c.check(c.name, body)
		findings = append(findings, f...)
		entries += n
	}

	if len(findings) > 0 {
		lintreport.Sort(findings)
		fmt.Fprintf(out, "❌ secret-suppression-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}

	fmt.Fprintf(out, "✅ secret-suppression-lint: %d ファイル / 抑止 %d 件がすべて理由を伴う fingerprint・path 単位です\n",
		len(checks), entries)
	return nil
}

// readSet は1行1つの一覧を集合にします。0件なら empty を返します —— 0件の一覧では、実在の検査が
// すべてのエントリを「無い」と言い、違反と不全の区別がつかなくなります。
func readSet(path string, empty error) (map[string]bool, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(errUnreadable, "一覧 %q", path)
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			set[line] = true
		}
	}
	if len(set) == 0 {
		return nil, xerrors.Wrapf(empty, "一覧 %q", path)
	}
	return set, nil
}

// reasonAbove は n 行目（0始まり）の直前に空行を挟まず続くコメント行が、撤回条件を含むかを返します。
func reasonAbove(lines []string, n int) bool {
	var block []string
	for i := n - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "#") {
			break
		}
		block = append(block, l)
	}
	return strings.Contains(strings.Join(block, "\n"), revocationMarker)
}

func isSkippable(line string) bool {
	l := strings.TrimSpace(line)
	return l == "" || strings.HasPrefix(l, "#")
}

// lineEntry は1行1エントリのファイルの行を分類します。読み飛ばすのは行頭の `#` と空行だけで、
// 消費側（gitleaks・trufflehog、trufflehog.mk の sed）と同じ読み方をします。前後に空白を持つ行は
// 違反として返します —— 字下げしたコメントや空白だけの行は、lint には読み飛ばして見え、消費側では
// 正規表現として効きます。
func lineEntry(line string) (entry bool, msg string) {
	switch {
	case line == "" || strings.HasPrefix(line, "#"):
		return false, ""
	case strings.TrimSpace(line) != line:
		return true, "行の前後に空白がある。コメントは行頭の `#` で始め、エントリは空白を付けずに書く"
	}
	return true, ""
}

var fingerprint = regexp.MustCompile(`^([0-9a-f]{40}):(.+):([A-Za-z0-9_-]+):([0-9]+)$`)

// checkGitleaksIgnore は fingerprint が commit を含み、その commit が履歴に在ることを見ます。
// commit を含まない形は作業ツリーに残る検出にも当たり、履歴を書き換えて消えた commit を指す形は
// 何にも当たらないまま残ります。
func checkGitleaksIgnore(name, body string, commits map[string]bool) ([]lintreport.Finding, int) {
	var findings []lintreport.Finding
	lines := strings.Split(body, "\n")
	n := 0
	for i, line := range lines {
		entry, msg := lineEntry(line)
		if !entry {
			continue
		}
		n++
		if msg != "" {
			findings = append(findings, finding(name, i, msg))
		}
		m := fingerprint.FindStringSubmatch(line)
		switch {
		case m == nil:
			findings = append(findings, finding(name, i, "fingerprint（<commit>:<path>:<rule>:<line>）の形ではない"))
		case !commits[m[1]]:
			findings = append(findings, finding(name, i, fmt.Sprintf("commit %s は履歴に無い", m[1])))
		}
		if !reasonAbove(lines, i) {
			findings = append(findings, finding(name, i, "直前のコメントに理由と撤回条件が無い"))
		}
	}
	return findings, n
}

// checkPathRegexes の tracked が nil のときは、path が追跡ファイルに在るかを見ない。
func checkPathRegexes(name, body string, tracked map[string]bool) ([]lintreport.Finding, int) {
	var findings []lintreport.Finding
	lines := strings.Split(body, "\n")
	n := 0
	for i, line := range lines {
		entry, msg := lineEntry(line)
		if !entry {
			continue
		}
		n++
		if msg != "" {
			findings = append(findings, finding(name, i, msg))
		}
		findings = append(findings, checkPathRegex(name, i, line, tracked)...)
		if !reasonAbove(lines, i) {
			findings = append(findings, finding(name, i, "直前のコメントに理由と撤回条件が無い"))
		}
	}
	return findings, n
}

func checkPathRegex(name string, i int, expr string, tracked map[string]bool) []lintreport.Finding {
	if _, err := regexp.Compile(expr); err != nil {
		return []lintreport.Finding{finding(name, i, fmt.Sprintf("正規表現として解釈できない: %v", err))}
	}
	path, ok := literalPath(expr)
	if !ok {
		return []lintreport.Finding{finding(name, i,
			fmt.Sprintf("`%s` は1つの path を名指していない。`^` と `$` で囲み、メタ文字はエスケープする", expr))}
	}
	if tracked != nil && !tracked[path] {
		return []lintreport.Finding{finding(name, i, fmt.Sprintf("`%s` は追跡ファイルに無い", path))}
	}
	return nil
}

// literalPath は `^...$` で囲まれ、メタ文字をすべてエスケープした正規表現から path を取り出します。
func literalPath(expr string) (string, bool) {
	if len(expr) < 3 || expr[0] != '^' || expr[len(expr)-1] != '$' {
		return "", false
	}
	inner := expr[1 : len(expr)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case c == '\\':
			if i+1 >= len(inner) || !strings.ContainsRune(`.+*?()[]{}|^$\-/`, rune(inner[i+1])) {
				return "", false
			}
			i++
			b.WriteByte(inner[i])
		case strings.ContainsRune(`.+*?()[]{}|^$`, rune(c)):
			return "", false
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), true
}

func finding(name string, i int, msg string) lintreport.Finding {
	return lintreport.Finding{File: name, Line: i + 1, Message: msg}
}

var (
	tomlHeader = regexp.MustCompile(`^\[\[?([A-Za-z]+)\]\]?$`)
	tomlKey    = regexp.MustCompile(`^([A-Za-z]+)\s*=\s*(.*)$`)
	tomlString = regexp.MustCompile(`^(?:"([^"\\]*)"|'([^']*)')$`)
	tomlArray  = regexp.MustCompile(`^\[(.*)\]$`)
	tomlLit    = regexp.MustCompile(`'''(.*?)'''|'([^']*)'`)
)

type allowlist struct {
	line                         int
	description, condition, rtgt string
	paths, regexes               []string
	hasPaths, hasRegexes         bool
}

// checkGitleaksConfig は .gitleaks.toml を行単位で読み、抑止がファイルの単位（paths）を持ち、
// 値の形（regexes）で狭めるときは AND で効くことを見ます。
//
// 受け付けるのは [extend] の useDefault = true と、allowlist の description / condition /
// regexTarget / paths / regexes だけで、1行に収まる値に限ります。規則の無効化や置き換えを含め、
// それ以外の書き方はエラーにします。
func checkGitleaksConfig(name, body string, tracked map[string]bool) ([]lintreport.Finding, int) {
	var findings []lintreport.Finding
	add := func(i int, msg string) { findings = append(findings, finding(name, i, msg)) }
	lines := strings.Split(body, "\n")

	var lists []*allowlist
	var cur *allowlist
	section := ""
	for i, raw := range lines {
		if isSkippable(raw) {
			continue
		}
		line := strings.TrimSpace(raw)
		if tomlHeader.MatchString(line) {
			switch line {
			case "[extend]":
				section, cur = "extend", nil
			case "[allowlist]", "[[allowlists]]":
				section = "allowlist"
				cur = &allowlist{line: i}
				lists = append(lists, cur)
				if !reasonAbove(lines, i) {
					add(i, "直前のコメントに理由と撤回条件が無い")
				}
			default:
				section, cur = "", nil
				add(i, fmt.Sprintf("`%s` は受け付けない。規則の追加・置き換えは抑止の範囲を超える", line))
			}
			continue
		}
		m := tomlKey.FindStringSubmatch(line)
		if m == nil {
			add(i, "行として解釈できない（値は1行に収める）")
			continue
		}
		key, value := m[1], strings.TrimSpace(m[2])
		switch section {
		case "extend":
			if key != "useDefault" || value != "true" {
				add(i, fmt.Sprintf("[extend] は useDefault = true だけを受け付ける（`%s`）", line))
			}
		case "allowlist":
			setAllowlistKey(cur, key, value, func(msg string) { add(i, msg) })
		default:
			add(i, fmt.Sprintf("テーブルの外のキー `%s` は受け付けない", key))
		}
	}

	for _, l := range lists {
		at := func(msg string) { add(l.line, msg) }
		if l.description == "" {
			at("description が無い")
		}
		if !l.hasPaths || len(l.paths) == 0 {
			at("paths が無い。抑止はファイルの単位で置く")
		}
		if l.hasRegexes && l.condition != "AND" {
			at("regexes を置くなら condition = \"AND\" にする。OR では regexes がどのファイルにも当たる")
		}
		for _, p := range l.paths {
			for _, f := range checkPathRegex(name, l.line, p, tracked) {
				at(f.Message)
			}
		}
	}
	return findings, len(lists)
}

func setAllowlistKey(l *allowlist, key, value string, add func(string)) {
	str := func() (string, bool) {
		m := tomlString.FindStringSubmatch(value)
		if m == nil {
			add(key + " の値を1行の文字列として解釈できない")
			return "", false
		}
		return m[1] + m[2], true
	}
	switch key {
	case "description":
		if s, ok := str(); ok {
			l.description = strings.TrimSpace(s)
		}
	case "condition":
		if s, ok := str(); ok {
			l.condition = s
		}
	case "regexTarget":
		if s, ok := str(); ok {
			l.rtgt = s
			if s != "match" && s != "secret" && s != "line" {
				add(fmt.Sprintf("regexTarget `%s` は gitleaks の値ではない", s))
			}
		}
	case "paths", "regexes":
		items, ok := literalArray(value)
		if !ok {
			add(key + " は1行のリテラル文字列（'...' か '''...'''）の配列で書く")
			return
		}
		for _, it := range items {
			if _, err := regexp.Compile(it); err != nil {
				add(fmt.Sprintf("%s の `%s` を正規表現として解釈できない", key, it))
			}
		}
		if key == "paths" {
			l.paths, l.hasPaths = items, true
		} else {
			l.regexes, l.hasRegexes = items, true
		}
		if len(items) == 0 {
			add(key + " が空である")
		}
	default:
		add(fmt.Sprintf("allowlist のキー `%s` は受け付けない", key))
	}
}

// literalArray は1行の配列を要素へ分けます。要素はリテラル文字列（単引用か三重の単引用）に限り、
// 要素の間にはカンマを1つ要求します。基本文字列（"..."）はエスケープで正規表現の意味が変わるので
// 受け付けません。
func literalArray(value string) ([]string, bool) {
	m := tomlArray.FindStringSubmatch(value)
	if m == nil {
		return nil, false
	}
	rest := strings.TrimSpace(m[1])
	items := []string{}
	for rest != "" {
		loc := tomlLit.FindStringSubmatchIndex(rest)
		if loc == nil || loc[0] != 0 {
			return nil, false
		}
		sm := tomlLit.FindStringSubmatch(rest)
		items = append(items, sm[1]+sm[2])
		rest = strings.TrimSpace(rest[loc[1]:])
		if rest == "" {
			break
		}
		if !strings.HasPrefix(rest, ",") {
			return nil, false
		}
		rest = strings.TrimSpace(rest[1:])
	}
	return items, true
}

var trivyTopLevel = map[string]bool{"vulnerabilities": true, "misconfigurations": true, "secrets": true, "licenses": true}

type trivySecret struct {
	ID        string   `yaml:"id"`
	Paths     []string `yaml:"paths"`
	Statement string   `yaml:"statement"`
	ExpiredAt string   `yaml:"expired_at"`
}

// checkTrivySecrets は .trivyignore.yaml の secrets 節の各エントリを見ます。トップレベルのキーも
// 閉じます —— 綴りを誤った secrets は、キーが無いのと同じく 0件として読まれます。
func checkTrivySecrets(name, body string, tracked map[string]bool) ([]lintreport.Finding, int) {
	dec := yaml.NewDecoder(strings.NewReader(body))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil && !xerrors.Is(err, io.EOF) {
		return []lintreport.Finding{{File: name, Line: 1, Message: fmt.Sprintf("YAML として解釈できない: %v", err)}}, 0
	}
	// 2つ目以降の文書は読まれないまま残る。
	var next yaml.Node
	if err := dec.Decode(&next); !xerrors.Is(err, io.EOF) {
		return []lintreport.Finding{{File: name, Line: max(next.Line, 1), Message: "YAML の文書が2つ以上ある。1つにまとめる"}}, 0
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return []lintreport.Finding{{File: name, Line: 1, Message: "トップレベルがマッピングではない"}}, 0
	}

	var findings []lintreport.Finding
	top := doc.Content[0]
	n := 0
	for i := 0; i+1 < len(top.Content); i += 2 {
		k, v := top.Content[i], top.Content[i+1]
		if !trivyTopLevel[k.Value] {
			findings = append(findings, lintreport.Finding{File: name, Line: k.Line,
				Message: fmt.Sprintf("トップレベルのキー `%s` は Trivy の節ではない", k.Value)})
			continue
		}
		if k.Value != "secrets" {
			continue
		}
		var entries []yaml.Node
		if err := v.Decode(&entries); err != nil {
			findings = append(findings, lintreport.Finding{File: name, Line: v.Line, Message: "secrets が列ではない"})
			continue
		}
		for _, e := range entries {
			n++
			findings = append(findings, checkTrivySecret(name, e, tracked)...)
		}
	}
	return findings, n
}

func checkTrivySecret(name string, e yaml.Node, tracked map[string]bool) []lintreport.Finding {
	at := func(msg string) lintreport.Finding { return lintreport.Finding{File: name, Line: e.Line, Message: msg} }
	var s trivySecret
	if err := e.Decode(&s); err != nil {
		return []lintreport.Finding{at(fmt.Sprintf("secrets のエントリとして解釈できない: %v", err))}
	}
	var findings []lintreport.Finding
	for i := 0; i+1 < len(e.Content); i += 2 {
		switch e.Content[i].Value {
		case "id", "paths", "statement", "expired_at":
		default:
			findings = append(findings, at(fmt.Sprintf("secrets のキー `%s` は受け付けない", e.Content[i].Value)))
		}
	}
	if s.ID == "" {
		findings = append(findings, at("id が無い"))
	}
	if len(s.Paths) == 0 {
		findings = append(findings, at("paths が無い。抑止はファイルの単位で置く"))
	}
	for _, p := range s.Paths {
		switch {
		case strings.ContainsAny(p, "*?[{"):
			findings = append(findings, at(fmt.Sprintf("`%s` は1つの path を名指していない", p)))
		case !tracked[p]:
			findings = append(findings, at(fmt.Sprintf("`%s` は追跡ファイルに無い", p)))
		}
	}
	if !strings.Contains(s.Statement, revocationMarker) {
		findings = append(findings, at("statement に理由と撤回条件が無い"))
	}
	return findings
}
