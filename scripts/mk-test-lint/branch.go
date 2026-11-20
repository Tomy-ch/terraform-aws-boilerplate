package main

import (
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// srcLine は `.mk` の1行と、その行番号。
type srcLine struct {
	n    int
	text string
}

// bootstrap は、運用機構を起動する前の層の変数。Go を起動する前に要るので、本体を検査で辿らない
// （ADR-0702 決定25 の例外）。
var bootstrap = map[string]struct{}{"COMPOSE_RUN": {}, "RUN_SCRIPT": {}, "RUN_SCRIPT_HOST": {}}

var (
	// コマンドの位置に来たら分岐・繰り返し・制御になる語（決定25 の閉じた一覧）。
	commandWords = map[string]string{
		"if": "if", "case": "case", "for": "for", "while": "while", "until": "until",
		"trap": "trap", "wait": "wait", "exit": "exit", "return": "return",
		"eval": "eval", "source": "source", ".": "source", "test": "test", "[": "[ ]", "[[": "[[ ]]",
	}
	andOrRe      = regexp.MustCompile(`&&|\|\|`)
	backgroundRe = regexp.MustCompile(`(^|[^&>|0-9])&([^&>]|$)`)
	paramCondRe  = regexp.MustCompile(`\$\$\{[A-Za-z_][A-Za-z0-9_]*:?[-=+?]`)
	cmdSubstRe   = regexp.MustCompile("\\$\\$\\(|`")
	funcDefRe    = regexp.MustCompile(`(^|[\s;{(])[A-Za-z_][A-Za-z0-9_]*\s*\(\)\s*\{`)
	makeFuncRe   = regexp.MustCompile(`\$[({](if|or|and|filter|filter-out|findstring|foreach|eval|shell)[\s,]`)
	directiveRe  = regexp.MustCompile(`^\s*(ifeq|ifneq|ifdef|ifndef|else|endif)\b`)
	commandSepRe = regexp.MustCompile(`;|\||&|\(|\)|\{|\}|\bthen\b|\bdo\b|\belse\b`)
	makeRefRe    = regexp.MustCompile(`\$[({][^$(){}]*[)}]`)
	varRefRe     = regexp.MustCompile(`\$[({](?:call\s+)?([A-Za-z_][A-Za-z0-9_]*)[\s,)}]`)
)

// constructs は1行が持つ、決定25 の閉じた一覧に当たる書き方を返す。
func constructs(line string) []string {
	var out []string
	add := func(s string) {
		for _, x := range out {
			if x == s {
				return
			}
		}
		out = append(out, s)
	}
	if directiveRe.MatchString(line) {
		add("条件ディレクティブ")
	}
	if m := makeFuncRe.FindStringSubmatch(line); m != nil {
		add("$(" + m[1] + ")")
	}
	for _, m := range andOrRe.FindAllString(line, -1) {
		add(m)
	}
	if backgroundRe.MatchString(line) {
		add("&（背景実行）")
	}
	if paramCondRe.MatchString(line) {
		add("条件のパラメータ展開")
	}
	if cmdSubstRe.MatchString(line) {
		add("コマンド置換")
	}
	if funcDefRe.MatchString(line) {
		add("関数定義")
	}
	// make の変数と関数の参照は、中の `(` で区切りを誤らないよう1語に潰してから、コマンドの位置を見る。
	flat := line
	for {
		next := makeRefRe.ReplaceAllString(flat, "MAKEREF")
		if next == flat {
			break
		}
		flat = next
	}
	for _, seg := range commandSepRe.Split(flat, -1) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		word := strings.TrimLeft(fields[0], "@-+")
		if word == "" && len(fields) > 1 {
			word = fields[1]
		}
		if word == "." && len(fields) < 2 {
			continue // `.` だけの値（作業ディレクトリ）は source ではない
		}
		if c, ok := commandWords[word]; ok {
			add(c)
		}
	}
	return out
}

// varRefs は1行が参照する変数の名前を返す。make の関数の名前は除く。
func varRefs(line string) []string {
	var out []string
	for _, m := range varRefRe.FindAllStringSubmatch(line, -1) {
		if _, fn := makeFunctions[m[1]]; !fn {
			out = append(out, m[1])
		}
	}
	return out
}

var makeFunctions = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, f := range strings.Fields("if or and filter filter-out findstring foreach eval shell call wildcard patsubst subst strip sort word words wordlist firstword lastword dir notdir suffix basename addsuffix addprefix join realpath abspath error warning info origin flavor value file let intcmp") {
		m[f] = struct{}{}
	}
	return m
}()

// varDef は変数か define の本体と、それを定義したファイル。
type varDef struct {
	file string
	body []srcLine
}

// recipeFindings は、範囲の `.mk` が定義するレシピと、そこから参照される変数の本体を検査する。
func recipeFindings(file string, mk mkFile, vars map[string][]varDef) []finding {
	var out []finding
	seen := map[string]struct{}{}
	var visit func(name string, stack []string)
	visit = func(name string, stack []string) {
		if _, skip := bootstrap[name]; skip {
			return
		}
		for _, s := range stack {
			if s == name {
				defs := vars[name]
				if len(defs) > 0 && len(defs[0].body) > 0 {
					out = append(out, finding{file: defs[0].file, line: defs[0].body[0].n, what: "変数の再帰", where: "変数 " + name + " の本体"})
				}
				return
			}
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		for _, d := range vars[name] {
			for _, l := range d.body {
				for _, c := range constructs(l.text) {
					out = append(out, finding{file: d.file, line: l.n, what: c, where: "変数 " + name + " の本体"})
				}
				for _, ref := range varRefs(l.text) {
					visit(ref, append(append([]string{}, stack...), name))
				}
			}
		}
	}
	for _, name := range keys(mk.recipeSrc) {
		for _, l := range mk.recipeSrc[name] {
			for _, c := range constructs(l.text) {
				out = append(out, finding{file: file, line: l.n, what: c, where: "ターゲット " + name + " のレシピ"})
			}
			for _, ref := range varRefs(l.text) {
				visit(ref, nil)
			}
		}
	}
	return out
}

// finding はレシピの検査の違反1件。
type finding struct {
	file  string
	line  int
	what  string
	where string
}

var scopeEntryRe = regexp.MustCompile(`^"([^"]+)",?$`)

// parseScope は、分岐の検査を掛ける `.mk` の範囲の宣言を読む。`files = [ ... ]` の1行1パスの形だけを受ける。
func parseScope(raw []byte) ([]string, error) {
	var out []string
	seen := map[string]struct{}{}
	inList := false
	closed := false
	for i, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "" || strings.HasPrefix(t, "#"):
		case !inList && !closed && t == "files = [":
			inList = true
		case inList && t == "]":
			inList, closed = false, true
		case inList:
			m := scopeEntryRe.FindStringSubmatch(t)
			if m == nil {
				return nil, xerrors.Wrapf(errSyntax, "%d 行目: %q", i+1, t)
			}
			if _, dup := seen[m[1]]; dup {
				return nil, xerrors.Wrapf(errSyntax, "%d 行目: %s が重複しています", i+1, m[1])
			}
			seen[m[1]] = struct{}{}
			out = append(out, m[1])
		default:
			return nil, xerrors.Wrapf(errSyntax, "%d 行目: %q", i+1, t)
		}
	}
	if inList || !closed {
		return nil, xerrors.Wrap(errSyntax, "files = [ ... ] が閉じていません")
	}
	if len(out) == 0 {
		return nil, xerrors.Wrap(errNoTargets, "範囲の宣言に `.mk` が1つもありません")
	}
	return out, nil
}
