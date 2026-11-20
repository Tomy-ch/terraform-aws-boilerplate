package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const maxBypassMonths = 3

var (
	errSyntax = xerrors.New("解釈できない構文です")

	// runners は、テストの名前を持つが `go test` / `terraform test` を起動するだけのターゲットと、
	// そのレシピが起動しなければならない道具。テストの本体は `.mk` の外に在る。
	runners = map[string]*regexp.Regexp{
		"go-test": regexp.MustCompile(`\bgo -C scripts test\b`),
		"tf-test": regexp.MustCompile(`\$\(TF\) [^;]*\btest\b`),
	}

	includeRe = regexp.MustCompile(`^include\s+(\S+)\s*(#.*)?$`)
	dbFromRe  = regexp.MustCompile("^#\\s+(?:recipe|commands) to execute \\(from [`']([^']+)', line \\d+\\):$")
	bypassRe  = regexp.MustCompile(
		`^"([^"]+)"\s*=\s*\{\s*expires\s*=\s*(\d{4}-\d{2}-\d{2})\s*,\s*issue\s*=\s*(\d+)\s*,\s*reason\s*=\s*"([^"]+)"\s*\}$`,
	)
)

// parseIncludes は Makefile の include 行が名指す `.mk` を返す。展開を要する形は読めない入力とする。
func parseIncludes(raw []byte) ([]string, error) {
	var out []string
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "-include", "sinclude":
			return nil, xerrors.Wrapf(errSyntax, "%d 行目: 無くても黙る include は数えられません: %q", i+1, trimmed)
		case "include":
		default:
			continue
		}
		m := includeRe.FindStringSubmatch(trimmed)
		if m == nil || strings.Contains(m[1], "$") {
			return nil, xerrors.Wrapf(errSyntax, "%d 行目: include は1行に1つのリテラルのパスで書きます: %q", i+1, trimmed)
		}
		out = append(out, m[1])
	}
	if len(out) == 0 {
		return nil, xerrors.Wrap(errNoTargets, "include が1つもありません")
	}
	return out, nil
}

// mkFile は1つの `.mk` から取り出した規則。
type mkFile struct {
	literal    map[string]int      // リテラルの名前を持つ規則のターゲット → 最初の行
	withRecipe map[string]struct{} // レシピを持つ規則のターゲット（リテラルの名前のもの）
	recipes    map[string]string   // ターゲット → レシピの本文
	unresolved int                 // 名前に変数かパターンを含み、レシピを持つ規則のターゲットの数
}

func (m mkFile) line(name string) int { return m.literal[name] }

// parseMk は `.mk` の規則の行を取り出す。define の中・レシピ・代入・条件分岐の行は規則としない。
// 閉じない define と、続きの行の途中で終わるファイルは、読めない入力とする（決定14）。
func parseMk(raw []byte) (mkFile, error) {
	mk := mkFile{literal: map[string]int{}, withRecipe: map[string]struct{}{}, recipes: map[string]string{}}
	lines := strings.Split(string(raw), "\n")
	depth := 0
	var current []string // レシピを集めている規則のターゲット
	inRecipe := false
	continued := false

	finish := func() {
		current, inRecipe = nil, false
	}

	for i := 0; i < len(lines); i++ {
		n := i + 1
		line := lines[i]
		wasContinued := continued
		continued = strings.HasSuffix(line, "\\")

		if wasContinued {
			if inRecipe {
				for _, name := range current {
					mk.recipes[name] += "\n" + line
				}
			}
			continue
		}
		head := strings.Fields(line)
		if depth > 0 {
			switch {
			case len(head) > 0 && head[0] == "endef":
				depth--
			case len(head) > 0 && head[0] == "define":
				depth++
			}
			continue
		}
		if strings.HasPrefix(line, "\t") {
			if inRecipe {
				for _, name := range current {
					mk.recipes[name] += "\n" + line
				}
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if trimmed == "" {
				finish()
			}
			continue
		}
		finish()
		switch head[0] {
		case "define":
			depth++
			continue
		case "endef":
			return mk, xerrors.Wrapf(errSyntax, "%d 行目: 対応する define の無い endef", n)
		case "ifeq", "ifneq", "ifdef", "ifndef", "else", "endif", "export", "unexport", "override", "vpath":
			continue
		case "include", "-include", "sinclude":
			return mk, xerrors.Wrapf(errSyntax, "%d 行目: `.mk` の中の include は数えられません", n)
		}

		colon := ruleColon(trimmed)
		if colon < 0 {
			continue // 代入
		}
		names, recipe := strings.Fields(trimmed[:colon]), ""
		rest := trimmed[colon+1:]
		if semi := strings.Index(rest, ";"); semi >= 0 {
			recipe = rest[semi+1:]
		}
		if len(names) == 0 {
			return mk, xerrors.Wrapf(errSyntax, "%d 行目: ターゲットの無い規則", n)
		}
		hasRecipe := recipe != "" || (i+1 < len(lines) && strings.HasPrefix(lines[i+1], "\t"))
		for _, name := range names {
			if strings.HasPrefix(name, ".") {
				continue // .PHONY などの特別なターゲット
			}
			if strings.ContainsAny(name, "$%") {
				if hasRecipe {
					mk.unresolved++
				}
				continue
			}
			if _, ok := mk.literal[name]; !ok {
				mk.literal[name] = n
			}
			if hasRecipe {
				mk.withRecipe[name] = struct{}{}
				current = append(current, name)
				mk.recipes[name] += recipe
			}
		}
		inRecipe = hasRecipe
	}
	if depth > 0 {
		return mk, xerrors.Wrap(errSyntax, "閉じない define があります")
	}
	if continued {
		return mk, xerrors.Wrap(errSyntax, "最後の行が続きの行の途中で終わります")
	}
	return mk, nil
}

// ruleColon は規則の区切りの `:` の位置を返す。代入の行なら -1。
func ruleColon(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '=':
			return -1
		case ':':
			if strings.HasPrefix(s[i:], ":=") || strings.HasPrefix(s[i:], "::=") {
				return -1
			}
			return i
		case '$':
			// $(...) の中の `:` と `=` は区切りではない
			if i+1 < len(s) && (s[i+1] == '(' || s[i+1] == '{') {
				closer := byte(')')
				if s[i+1] == '{' {
					closer = '}'
				}
				depth := 0
			scan:
				for j := i + 1; j < len(s); j++ {
					switch s[j] {
					case s[i+1]:
						depth++
					case closer:
						depth--
					}
					if depth == 0 {
						i = j
						break scan
					}
				}
			}
		}
	}
	return -1
}

// parseDB は make -pRrq の出力から、レシピを定義したファイルごとのターゲットを返す。
// make の終了コードは 0（最新）か 1（更新が要る）だけを受ける。2 は Makefile の誤り。
func parseDB(raw []byte, exit int) (map[string]map[string]struct{}, error) {
	if exit != 0 && exit != 1 {
		return nil, xerrors.Wrapf(errUnreadable, "make -p が終了コード %d で終わりました", exit)
	}
	text := string(raw)
	if !strings.Contains(text, "# Make data base") || !strings.Contains(text, "# Finished Make data base") {
		return nil, xerrors.Wrap(errUnreadable, "make -p の出力の始まりか終わりがありません")
	}
	out := map[string]map[string]struct{}{}
	inFiles := false
	notTarget := false
	var pending []string
	count := 0
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "# Files":
			inFiles = true
			continue
		case strings.HasPrefix(line, "# files hash-table stats"), strings.HasPrefix(line, "# VPATH Search Paths"):
			inFiles = false
			continue
		}
		if !inFiles {
			continue
		}
		switch {
		case line == "# Not a target:":
			notTarget = true
		case line == "":
			pending, notTarget = nil, false
		case strings.HasPrefix(line, "#"):
			if m := dbFromRe.FindStringSubmatch(line); m != nil && len(pending) > 0 && !notTarget {
				if out[m[1]] == nil {
					out[m[1]] = map[string]struct{}{}
				}
				for _, name := range pending {
					out[m[1]][name] = struct{}{}
					count++
				}
			}
		case strings.HasPrefix(line, "\t"):
		default:
			colon := strings.Index(line, ":")
			if colon <= 0 {
				continue
			}
			name := line[:colon]
			if strings.HasPrefix(name, ".") {
				continue
			}
			pending = []string{name}
		}
	}
	if count == 0 {
		return nil, xerrors.Wrap(errNoTargets, "make -p の出力にレシピを持つ規則がありません")
	}
	return out, nil
}

// bypass は移行中の除外 1 件（ADR-0702 決定18）。
type bypass struct {
	expires time.Time
	issue   int
	line    int
}

func (b bypass) valid(today time.Time) bool {
	return !b.expires.Before(today) && !b.expires.After(today.AddDate(0, maxBypassMonths, 0))
}

// parseBypass は `"<ターゲット>" = { expires = YYYY-MM-DD, issue = <N>, reason = "..." }` の行を読む。
func parseBypass(raw []byte) (map[string]bypass, error) {
	out := map[string]bypass{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := bypassRe.FindStringSubmatch(line)
		if m == nil {
			return nil, xerrors.Wrapf(errSyntax, "%d 行目: %q", i+1, line)
		}
		expires, err := time.Parse(time.DateOnly, m[2])
		if err != nil {
			return nil, xerrors.Wrapf(errSyntax, "%d 行目: 日付 %q", i+1, m[2])
		}
		issue, err := strconv.Atoi(m[3])
		if err != nil {
			return nil, xerrors.Wrapf(errSyntax, "%d 行目: issue %q", i+1, m[3])
		}
		if _, dup := out[m[1]]; dup {
			return nil, xerrors.Wrapf(errSyntax, "%d 行目: %s が重複しています", i+1, m[1])
		}
		out[m[1]] = bypass{expires: expires, issue: issue, line: i + 1}
	}
	return out, nil
}
