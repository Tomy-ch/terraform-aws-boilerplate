// editorconfig-directive-lint は、editorconfig-checker の無効化の印が、理由と撤回条件を持つ
// 閉じた範囲としてだけ置かれていることを検査します。
//
// 解いている問題:
//
// **editorconfig-checker は印を行の部分文字列として読み、閉じない範囲を誤りにしません。** 範囲を
// 開く印の後に閉じる印が無ければ、そのファイルの残りは黙って検査の外に出ます。散文が印の文字列を
// 引用しただけでも同じことが起き、ゲートは緑のままです。
//
// 印の綴りはこのソース自身にも書けません。書けば、このファイルの残りが検査の外に出ます。そのため
// 文字列は連結で組み立てます。
//
//	editorconfig-directive-lint [-files <一覧>]
//
// -files を省くと、`git -c core.quotepath=false ls-files -z` の一覧のうち、作業ツリーに在るものを見ます。git には
// 外側のリポジトリから切り離した env（lib/gitenv）を渡します。在るかどうかで外すのは、作業ツリーから消えた追跡
// ファイル（ErrNotExist）だけで、外した後の件数が0件なら落ちます。違反は終了コード 1、使い方の誤り・読めない
// 入力・一覧を得られない・対象が0件は 2 です。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const prefix = "editorconfig-" + "checker-"

var (
	errUsage      = xerrors.New("引数が正しくありません")
	errUnreadable = xerrors.New("検査対象を読み取れません")
	errNoTargets  = xerrors.New("検査対象のファイルが0件です")
	errViolations = xerrors.New("違反がある")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout, func() ([]string, error) { return gitFiles(".", gitEnv) }); err != nil {
		log.Printf("❌ editorconfig-directive-lint: %v", err)
		os.Exit(exitCode(err))
	}
}

func gitEnv() ([]string, error) { return gitEnvFrom(os.Environ()) }

func gitEnvFrom(environ []string) ([]string, error) {
	return gitenv.Env(environ)
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errUsage), xerrors.Is(err, errUnreadable), xerrors.Is(err, errNoTargets):
		return 2
	default:
		return 1
	}
}

// listFunc は、-files を省いたときの検査対象の候補（git の追跡ファイル）を返します。テストは差し替えます。
type listFunc func() ([]string, error)

func run(args []string, out io.Writer, lister listFunc) error {
	fset := flag.NewFlagSet("editorconfig-directive-lint", flag.ContinueOnError)
	fset.SetOutput(out)
	list := fset.String("files", "", "検査するファイルの一覧（1行1パス）。省くと git の追跡ファイル")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if fset.NArg() > 0 {
		return xerrors.Wrapf(errUsage, "余分な引数 %q", fset.Args())
	}

	var paths []string
	var err error
	if *list != "" {
		paths, err = readList(*list)
	} else {
		paths, err = existingFiles(lister)
	}
	if err != nil {
		return err
	}

	var findings []lintreport.Finding
	directives := 0
	for _, p := range paths {
		raw, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return xerrors.Wrapf(errUnreadable, "%s", p)
		}
		fileFindings, n := check(p, raw)
		findings = append(findings, fileFindings...)
		directives += n
	}

	if len(findings) > 0 {
		lintreport.Sort(findings)
		fmt.Fprintf(out, "❌ editorconfig-directive-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		fmt.Fprintf(out, "\n印は `%s` に続けて範囲を開く語・閉じる語を書いた形だけを使い、開く印に ` -- <理由>。撤回: <条件>` を添える。"+
			"散文で印に触れるときは綴りを書かない（.makefiles/hygiene/editorconfig.mk 冒頭）。\n", prefix)
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}

	fmt.Fprintf(out, "✅ editorconfig-directive-lint: %d ファイル / 印 %d 行がすべて閉じた範囲です\n", len(paths), directives)
	return nil
}

func readList(path string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(errUnreadable, "一覧 %q", path)
	}
	var paths []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		return nil, xerrors.Wrapf(errNoTargets, "一覧 %q", path)
	}
	return paths, nil
}

// existingFiles は、lister の一覧のうち作業ツリーに在るものを返します。外すのは ErrNotExist だけで、それ以外の
// 理由で調べられないものは残し、読むときに落とします。外した後が0件なら errNoTargets です。
func existingFiles(lister listFunc) ([]string, error) {
	names, err := lister()
	if err != nil {
		return nil, xerrors.Wrapf(errUnreadable, "追跡ファイルの一覧: %v", err)
	}
	var paths []string
	for _, p := range names {
		if _, err := os.Stat(filepath.Clean(p)); xerrors.Is(err, fs.ErrNotExist) {
			continue
		}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return nil, xerrors.Wrapf(errNoTargets, "追跡ファイル %d 件のうち作業ツリーに在るもの", len(names))
	}
	return paths, nil
}

// gitFiles は、dir のリポジトリの追跡ファイルを、isolated が返す（外側のリポジトリから切り離した）env の git で
// 列挙します。
func gitFiles(dir string, isolated func() ([]string, error)) ([]string, error) {
	env, err := isolated()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(context.Background(), "git", "-c", "core.quotepath=false", "ls-files", "-z")
	cmd.Dir, cmd.Env = dir, env
	raw, err := cmd.Output()
	if err != nil {
		return nil, xerrors.Wrapf(err, "git ls-files")
	}
	return splitNUL(string(raw)), nil
}

func splitNUL(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// check は1ファイルの印を、editorconfig-checker と同じ部分文字列の判定で分類します。
// 返す数は、印の候補を含む行の数です。
func check(path string, raw []byte) ([]lintreport.Finding, int) {
	var findings []lintreport.Finding
	add := func(line int, msg string) {
		findings = append(findings, lintreport.Finding{File: path, Line: line, Message: msg})
	}

	open := 0
	count := 0
	for i, line := range strings.Split(string(raw), "\n") {
		n := i + 1
		idx := candidateIndex(line)
		if idx < 0 {
			continue
		}
		count++
		rest := line[idx+len(prefix):]
		switch {
		case strings.HasPrefix(rest, "disable-file"),
			strings.HasPrefix(rest, "disable-next-line"),
			strings.HasPrefix(rest, "disable-line"):
			add(n, "ファイル・行の単位で外す印は使わない。範囲を開く印と閉じる印で囲む")
		case strings.HasPrefix(rest, "disable"):
			if open > 0 {
				add(n, fmt.Sprintf("%d 行目で開いた範囲の中で、また範囲を開いている", open))
				continue
			}
			open = n
			if !hasReason(rest[len("disable"):]) {
				add(n, "範囲を開く印に ` -- <理由>。撤回: <条件>` が無い")
			}
		case strings.HasPrefix(rest, "enable"):
			if open == 0 {
				add(n, "開いていない範囲を閉じている")
				continue
			}
			open = 0
		default:
			add(n, "印として解釈できない。散文で印に触れるときは綴りを書かない")
		}
	}
	if open > 0 {
		add(open, "範囲を開いたまま閉じていない。ファイルの残りが検査の外に出る")
	}
	// 行ごとの分類は各行の最初の印しか見ない。全体の出現数と食い違えば、分類から漏れた印がある。
	if total := len(candidateRe.FindAllStringIndex(string(raw), -1)); total != count {
		add(1, fmt.Sprintf("印の出現 %d 件のうち %d 件しか分類できていない。1行に印を2つ書かない", total, count))
	}
	return findings, count
}

// candidates は、接頭辞の後ろがこれで始まるときだけ印の候補とする綴りです。editorconfig-checker が読む印は
// どれも disable か enable で始まるので、綴りの誤り（disble など）もここに入ります。接頭辞の後ろがほかの語の
// もの（道具の配布物の名前 `<接頭辞>linux-amd64.tar.gz` など）は印にならないので数えません。
var candidates = []string{"dis", "en"}

// candidateRe は印の候補を、candidateIndex とは別の経路で数えるための形です。check は行ごとの分類の数を
// これと突き合わせます。
var candidateRe = regexp.MustCompile(regexp.QuoteMeta(prefix) + `(?:` + strings.Join(candidates, "|") + `)`)

// candidateIndex は、line の中で最初の印の候補の位置を返します。無ければ -1 です。
func candidateIndex(line string) int {
	for from := 0; ; {
		i := strings.Index(line[from:], prefix)
		if i < 0 {
			return -1
		}
		at := from + i
		rest := line[at+len(prefix):]
		for _, c := range candidates {
			if strings.HasPrefix(rest, c) {
				return at
			}
		}
		from = at + len(prefix)
	}
}

func hasReason(tail string) bool {
	_, reason, ok := strings.Cut(tail, " -- ")
	return ok && strings.TrimSpace(reason) != "" && strings.Contains(reason, "撤回")
}
