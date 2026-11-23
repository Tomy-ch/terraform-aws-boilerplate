package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// missingShown は、走査の対象から外れた追跡ファイルを表示する上限です。
const missingShown = 20

// expectedExcludes は、本走査の対象から外れてよい追跡ファイルのパターン（gitignore の形）です。.typos.toml の
// [files] の extend-exclude と同じ集合に保つ独立の宣言で、一致しなければ落ちます（package doc）。
var expectedExcludes = []string{".git/", "**/.terraform.lock.hcl", "scripts/go.sum"}

// runCoverage は、設定 cfg の extend-exclude が expectedExcludes と集合として一致し、本走査の対象（typos --files）が、
// git の追跡ファイルから expectedExcludes に当たるものと設定 config 自身を除いたものを覆っていることを確かめます。
// config はリポジトリ直下からの相対パスです。
func runCoverage(config string, cfg []byte, w io.Writer, tool toolFunc, env envFunc) error {
	declared, err := extendExcludes(string(cfg))
	if err != nil {
		return err
	}
	if err := sameExcludes(declared, expectedExcludes); err != nil {
		return err
	}
	patterns := expectedExcludes
	gitEnv, err := env()
	if err != nil {
		return xerrors.Wrapf(errTool, "%v", err)
	}

	scanned, err := runTool(tool(".", os.Environ(), stepFiles))
	if err != nil {
		return err
	}
	if scanned.code != 0 {
		return xerrors.Wrapf(errTool, "typos --files が rc=%d で終わりました\n%s", scanned.code, scanned.combined)
	}
	listed, err := runTool(tool(".", gitEnv, stepLsFiles))
	if err != nil {
		return err
	}
	if listed.code != 0 {
		return xerrors.Wrapf(errTool, "git ls-files が rc=%d で終わりました\n%s", listed.code, listed.combined)
	}

	self := filepath.ToSlash(filepath.Clean(config))
	var want []string
	for _, p := range strings.Split(listed.stdout, "\x00") {
		if p == "" || p == self {
			continue
		}
		hit, err := excluded(p, patterns)
		if err != nil {
			return err
		}
		if !hit {
			want = append(want, p)
		}
	}
	if len(want) == 0 {
		return xerrors.Wrap(errNoInput, "追跡ファイルの一覧が0件です")
	}
	missing := missingFiles(scanned.stdout, want)
	if len(missing) > 0 {
		fmt.Fprintln(w, "❌ typos: 追跡ファイルが走査対象から外れています:")
		for _, p := range missing[:min(len(missing), missingShown)] {
			fmt.Fprintln(w, p)
		}

		return xerrors.Wrapf(errUncovered, "%d 件", len(missing))
	}
	fmt.Fprintf(w, "✅ typos coverage: 追跡ファイル %d 件がすべて走査の対象です\n", len(want))

	return nil
}

// sameExcludes は、declared と expected が重複を除いて同じ集合なら nil、そうでなければ両方を名指した errExcludes です。
func sameExcludes(declared, expected []string) error {
	a, b := slices.Clone(declared), slices.Clone(expected)
	slices.Sort(a)
	slices.Sort(b)
	if slices.Equal(slices.Compact(a), slices.Compact(b)) {
		return nil
	}

	return xerrors.Wrapf(errExcludes, ".typos.toml: %q / typos-gate: %q", a, b)
}

// missingFiles は、want のうち、typos --files の出力（1行1パス、`./` 始まり）に無いものを want の順で返します。
func missingFiles(scanned string, want []string) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(scanned, "\n") {
		if line != "" {
			seen[strings.TrimPrefix(line, "./")] = true
		}
	}
	var missing []string
	for _, p := range want {
		if !seen[p] {
			missing = append(missing, p)
		}
	}

	return missing
}

var (
	sectionRe = regexp.MustCompile(`^\[\s*([^\]]+?)\s*\]$`)
	excludeRe = regexp.MustCompile(`^(files\.)?extend-exclude\s*=(.*)$`)
)

// extendExcludes は、typos の設定の [files] の extend-exclude を読みます。文字列の配列として解釈できない値、
// [files] の外の extend-exclude、重複は errRead です（ADR-0702 決定14）。
func extendExcludes(cfg string) ([]string, error) {
	section := ""
	var value strings.Builder
	found, open := false, false
	for i, line := range strings.Split(cfg, "\n") {
		text := strings.TrimSpace(stripComment(line))
		if open {
			value.WriteString(" " + text)
			open = !strings.HasSuffix(text, "]")

			continue
		}
		if m := sectionRe.FindStringSubmatch(text); m != nil {
			section = m[1]

			continue
		}
		m := excludeRe.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		if (m[1] == "" && section != "files") || (m[1] != "" && section != "") {
			return nil, xerrors.Wrapf(errRead, "%d 行目: [files] の外の extend-exclude は解釈しません", i+1)
		}
		if found {
			return nil, xerrors.Wrapf(errRead, "%d 行目: extend-exclude が重複しています", i+1)
		}
		found = true
		rhs := strings.TrimSpace(m[2])
		value.WriteString(rhs)
		open = !strings.HasSuffix(rhs, "]")
	}
	if open {
		return nil, xerrors.Wrap(errRead, "extend-exclude の配列が閉じていません")
	}
	if !found {
		return nil, nil
	}

	return parseStringArray(value.String())
}

func stripComment(line string) string {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote = r
		case quote == 0 && r == '#':
			return line[:i]
		}
	}

	return line
}

var arrayItemRe = regexp.MustCompile(`^\s*(?:"([^"\\]*)"|'([^']*)')\s*(,|$)`)

// parseStringArray は、`[ "a", 'b', ]` の形の TOML の文字列の配列を読みます。エスケープを含む文字列と、
// 文字列でない要素は errRead です。
func parseStringArray(v string) ([]string, error) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		return nil, xerrors.Wrapf(errRead, "extend-exclude が配列ではありません: %s", v)
	}
	body := strings.TrimSpace(v[1 : len(v)-1])
	var items []string
	for body != "" {
		m := arrayItemRe.FindStringSubmatch(body)
		if m == nil {
			return nil, xerrors.Wrapf(errRead, "extend-exclude の要素を解釈できません: %s", body)
		}
		items = append(items, m[1]+m[2])
		body = strings.TrimSpace(body[len(m[0]):])
	}

	return items, nil
}

// excluded は、パス p（`/` 区切り、リポジトリ直下から）が gitignore の形のパターンのどれかに当たれば true です。
func excluded(p string, patterns []string) (bool, error) {
	for _, pat := range patterns {
		hit, err := matchGlob(pat, p)
		if err != nil {
			return false, err
		}
		if hit {
			return true, nil
		}
	}

	return false, nil
}

// matchGlob は、gitignore の形のパターン pat が p に当たるかを返します。読むのは、末尾の `/`（ディレクトリだけに
// 当たる）、先頭の `/` と途中の `/`（直下に固定）、先頭の `**/`（どの深さにも当たる）、`*` `?` `[]` です。
// それ以外の形（否定の `!`、途中の `**`、エスケープ）は、当たりを取り違えないよう errRead です。
func matchGlob(pat, p string) (bool, error) {
	if pat == "" || strings.HasPrefix(pat, "!") || strings.Contains(pat, `\`) {
		return false, xerrors.Wrapf(errRead, "解釈しない除外のパターン %q", pat)
	}
	dirOnly := strings.HasSuffix(pat, "/")
	pat = strings.TrimSuffix(pat, "/")
	anyDepth := strings.HasPrefix(pat, "**/")
	pat = strings.TrimPrefix(pat, "**/")
	anchored := strings.HasPrefix(pat, "/")
	pat = strings.TrimPrefix(pat, "/")
	if pat == "" || strings.Contains(pat, "**") {
		return false, xerrors.Wrapf(errRead, "解釈しない除外のパターン %q", pat)
	}
	if _, err := path.Match(pat, ""); err != nil {
		return false, xerrors.Wrapf(errRead, "除外のパターン %q: %v", pat, err)
	}
	if strings.Contains(pat, "/") {
		anchored = !anyDepth
	}
	segs := strings.Split(p, "/")
	k := len(strings.Split(pat, "/"))
	for start := 0; start+k <= len(segs); start++ {
		if anchored && start > 0 {
			break
		}
		// ディレクトリに当たれば、その下のすべてが外れる。ファイルに当たるのは最後の段だけ。
		if dirOnly && start+k == len(segs) {
			continue
		}
		if ok, _ := path.Match(pat, strings.Join(segs[start:start+k], "/")); ok {
			return true, nil
		}
	}

	return false, nil
}
