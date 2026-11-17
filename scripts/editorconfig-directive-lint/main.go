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
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const prefix = "editorconfig-" + "checker-"

var (
	errUnreadable = xerrors.New("検査対象を読み取れません")
	errNoTargets  = xerrors.New("検査対象のファイルが0件です")
	errViolations = xerrors.New("違反がある")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Printf("❌ editorconfig-directive-lint: %v", err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errUnreadable), xerrors.Is(err, errNoTargets):
		return 2
	default:
		return 1
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("editorconfig-directive-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	list := fs.String("files", "", "検査するファイルの一覧（1行1パス。git ls-files の出力）")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	paths, err := readList(*list)
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

// check は1ファイルの印を、editorconfig-checker と同じ部分文字列の判定で分類します。
// 返す数は、接頭辞を含む行の数です。
func check(path string, raw []byte) ([]lintreport.Finding, int) {
	var findings []lintreport.Finding
	add := func(line int, msg string) {
		findings = append(findings, lintreport.Finding{File: path, Line: line, Message: msg})
	}

	open := 0
	count := 0
	for i, line := range strings.Split(string(raw), "\n") {
		n := i + 1
		idx := strings.Index(line, prefix)
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
	if total := strings.Count(string(raw), prefix); total != count {
		add(1, fmt.Sprintf("印の出現 %d 件のうち %d 件しか分類できていない。1行に印を2つ書かない", total, count))
	}
	return findings, count
}

func hasReason(tail string) bool {
	_, reason, ok := strings.Cut(tail, " -- ")
	return ok && strings.TrimSpace(reason) != "" && strings.Contains(reason, "撤回")
}
