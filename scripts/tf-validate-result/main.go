// tf-validate-result は、`make tf-validate` がディレクトリごとに走らせた `terraform validate -json`
// の結果を読み、エラーと警告の両方で失敗を判定します（ADR-0501 決定4・13）。
//
// 解いている問題:
//
// **`terraform validate` は警告を出しても 0 で終わる。** 終了コードだけを見るゲートは、Provider が
// 非推奨とした引数を書いた構成を通します。警告を数えるには `-json` の出力を読む必要があり、読む側が
// 壊れると、ゲートは何も数えずに緑を返します。そのため、出力が読めない、件数の欄が diagnostic の数と
// 食い違う、終了コードと `valid` が食い違う、知らない severity が在る、対象が0件のときは、
// いずれも成功させません（ADR-0702 決定13-15）。
//
// 子 module の警告は、それを呼ぶディレクトリの validate にも現れます。走査の基点からのパス・行・列・
// summary・detail が同じ警告は1件に数えます。
//
// 抑止は宣言（既定は `.tf-validate-ignore.yaml`）が持ちます。Terraform の警告は検出 ID も inline の
// 抑止も持たないので、summary を検出 ID とし、パス・ブロック・detail で対象を名指します
// （ADR-0501 決定13）。宣言の件数が、当たった警告の件数と一致しなければ失敗します。
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage         = xerrors.New("引数が正しくありません")
	errManifest      = xerrors.New("結果の一覧を解釈できません")
	errNoTargets     = xerrors.New("validate の対象ディレクトリが0件です")
	errUnreadable    = xerrors.New("validate の出力を読み取れません")
	errMalformed     = xerrors.New("validate の出力が想定の形ではありません")
	errCountMismatch = xerrors.New("validate の件数の欄が diagnostic の数と食い違います")
	errExitMismatch  = xerrors.New("validate の終了コードが valid と食い違います")
	errOutside       = xerrors.New("警告のパスが走査の基点の外を指します")
	errIgnore        = xerrors.New("抑止の宣言を解釈できません")
	errFailed        = xerrors.New("validate が落ちた")
)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout), os.Stderr))
}

// report は run のエラーを1行で w に書き、終了コードを返します。エラーの文言はパスや validate の
// 出力を含むので、lineGuard を通します。
func report(err error, w io.Writer) int {
	if err != nil {
		g := &lineGuard{w: w}
		fmt.Fprintf(g, "❌ tf-validate-result: %v\n", err)
		_ = g.flush()
	}
	return exitCode(err)
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errFailed):
		return 1
	default:
		return 2
	}
}

func run(args []string, w io.Writer) error {
	out := &lineGuard{w: w}
	defer func() { _ = out.flush() }()
	fs := flag.NewFlagSet("tf-validate-result", flag.ContinueOnError)
	fs.SetOutput(out)
	base := fs.String("base", "", "走査の基点（TF_BASE）")
	manifest := fs.String("manifest", "", "結果の一覧（1行に ディレクトリ・終了コード・JSON・stderr をタブで区切る）")
	ignorePath := fs.String("ignore", "", "抑止の宣言")
	if err := fs.Parse(args); err != nil {
		// usage は flag が既に出力している。
		if xerrors.Is(err, flag.ErrHelp) {
			return nil
		}
		return xerrors.Wrapf(errUsage, "引数の解釈: %v", err)
	}
	if *base == "" || *manifest == "" || *ignorePath == "" {
		return xerrors.Wrap(errUsage, "-base・-manifest・-ignore はすべて要ります")
	}

	entries, err := readManifest(*manifest)
	if err != nil {
		return err
	}
	ignores, err := loadIgnore(*ignorePath)
	if err != nil {
		return err
	}

	var results []dirResult
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Clean(e.json))
		if err != nil {
			return xerrors.Wrapf(errUnreadable, "%s: %s", e.dir, e.json)
		}
		stderr, err := os.ReadFile(filepath.Clean(e.stderr))
		if err != nil {
			fmt.Fprintf(out, "%s の validate の stderr を読めません（判定は JSON で続けます）: %v\n", e.dir, err)
		}
		if s := strings.TrimSpace(string(stderr)); s != "" {
			fmt.Fprintf(out, "%s の validate の stderr:\n%s\n", e.dir, quote(s))
		}
		r, err := parseResult(e.dir, e.code, data)
		if err != nil {
			return err
		}
		results = append(results, r)
	}

	warnings, err := collectWarnings(*base, results)
	if err != nil {
		return err
	}
	remaining, suppressed, mismatches := applyIgnore(ignores, warnings)

	var failedDirs []string
	for _, r := range results {
		if r.valid {
			continue
		}
		failedDirs = append(failedDirs, r.dir)
		for _, d := range r.errors {
			fmt.Fprintf(out, "%s: %s\n%s\n", d.location(r.dir), oneLine(d.Summary), quote(d.Detail))
		}
	}
	for _, w := range remaining {
		fmt.Fprintf(out, "%s: [warning] %s（%s）\n%s\n  現れたディレクトリ: %s\n",
			w.location(), oneLine(w.summary), oneLine(w.block), quote(w.detail), strings.Join(w.dirs, " "))
	}
	for _, m := range mismatches {
		fmt.Fprintf(out, "抑止の宣言 %s の %q（%s）: 当たった警告 %d 件、宣言 %d 件\n",
			*ignorePath, m.entry.Summary, m.entry.Path, m.matched, m.entry.Occurrences)
	}

	var problems []string
	if len(failedDirs) > 0 {
		fmt.Fprintf(out, "❌ tf-validate が落ちたディレクトリ: %s\n", strings.Join(failedDirs, " "))
		problems = append(problems, fmt.Sprintf("エラーのあるディレクトリ %d 件", len(failedDirs)))
	}
	if len(remaining) > 0 {
		fmt.Fprintf(out, "❌ tf-validate: 抑止されない警告が %d 件あります（抑止は %s。ADR-0501 決定13）\n", len(remaining), *ignorePath)
		problems = append(problems, fmt.Sprintf("警告 %d 件", len(remaining)))
	}
	if len(mismatches) > 0 {
		fmt.Fprintf(out, "❌ tf-validate: 件数の合わない抑止の宣言が %d 件あります\n", len(mismatches))
		problems = append(problems, fmt.Sprintf("件数の合わない抑止 %d 件", len(mismatches)))
	}
	if len(problems) > 0 {
		return xerrors.Wrap(errFailed, strings.Join(problems, "、"))
	}

	fmt.Fprintf(out, "✅ tf-validate: %d ディレクトリを validate し、エラーと抑止されない警告はありません（抑止 %d 件）\n",
		len(results), suppressed)
	return nil
}

// newlines は、GitHub Actions の runner が行の終わりと読む `\r\n` と `\r` を `\n` に揃えます。
var newlines = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// quote は Terraform と Provider が出した文字列の各行に `│` を前置し、見出しの行と区別します。
func quote(s string) string {
	return "  │ " + strings.ReplaceAll(newlines.Replace(s), "\n", "\n  │ ")
}

// oneLine は、見出しの行に入れる文字列の改行を空白に置き換えます。
func oneLine(s string) string {
	return strings.ReplaceAll(newlines.Replace(s), "\n", " ")
}

// lineGuard は、書き込まれた出力を runner と同じ規則（`\n`・`\r`・`\r\n`）で行に分け、空白を除いて
// `::` で始まる行に印を前置します。出力にはパスや Terraform の文字列が混ざり、行頭の `::` は GitHub
// Actions が workflow command と読むためです。行の途中で終わった書き込みは、次の改行か flush まで持ちます。
type lineGuard struct {
	w   io.Writer
	buf []byte
}

func (g *lineGuard) Write(p []byte) (int, error) {
	g.buf = append(g.buf, p...)
	for {
		i := bytes.IndexAny(g.buf, "\r\n")
		if i < 0 || (g.buf[i] == '\r' && i == len(g.buf)-1) {
			return len(p), nil
		}
		next := i + 1
		if g.buf[i] == '\r' && g.buf[next] == '\n' {
			next++
		}
		if _, err := g.w.Write(guardLine(g.buf[:i])); err != nil {
			return 0, err
		}
		g.buf = g.buf[next:]
	}
}

// flush は、まだ書いていない残りを Write と同じ規則で行に分けて書きます。改行で終わらない最後の行も
// 1行として書きます。
func (g *lineGuard) flush() error {
	if len(g.buf) == 0 {
		return nil
	}
	rest := bytes.TrimSuffix(g.buf, []byte("\r"))
	if !bytes.HasSuffix(rest, []byte("\n")) {
		rest = append(rest, '\n')
	}
	g.buf = nil
	_, err := g.Write(rest)
	return err
}

// guardLine は、改行を除いた1行に改行を付けて返します。空白を除いて `::` で始まる行には印を前置します。
func guardLine(line []byte) []byte {
	out := make([]byte, 0, len(line)+len("│ \n"))
	if bytes.HasPrefix(bytes.TrimLeftFunc(line, unicode.IsSpace), []byte("::")) {
		out = append(out, "│ "...)
	}
	out = append(out, line...)
	return append(out, '\n')
}

type manifestEntry struct {
	dir    string
	code   int
	json   string
	stderr string
}

// readManifest は結果の一覧を読みます。0件の一覧は、対象を見ないまま成功する形なのでエラーにします。
func readManifest(path string) ([]manifestEntry, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(errManifest, "%s を開けません", path)
	}
	defer func() { _ = f.Close() }()

	var entries []manifestEntry
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || fields[0] == "" || fields[2] == "" || fields[3] == "" {
			return nil, xerrors.Wrapf(errManifest, "%s:%d: 4つの欄を持ちません", path, n)
		}
		code, err := strconv.Atoi(fields[1])
		if err != nil || code < 0 {
			return nil, xerrors.Wrapf(errManifest, "%s:%d: 終了コード %q", path, n, fields[1])
		}
		entries = append(entries, manifestEntry{dir: fields[0], code: code, json: fields[2], stderr: fields[3]})
	}
	if err := sc.Err(); err != nil {
		return nil, xerrors.Wrapf(errManifest, "%s を読めません", path)
	}
	if len(entries) == 0 {
		return nil, xerrors.Wrapf(errNoTargets, "%s", path)
	}
	return entries, nil
}
