// mk-test-lint は、`.mk` のレシピにテストのターゲットが無いことを検査します（ADR-0702 決定25）。
//
// 解いている問題:
//
// **レシピの中で書いたテストには、決定13-16 の規律を当てる手段がありません。** fixture を
// `printf` で組み、終了コードを数えるテストは、センチネルでの判定も、ケースごとの失敗の報告も
// 持たず、検査器を検査する手段が検査されないまま残ります。テストは運用機構の Go のテストへ
// 置きます。
//
// テストのターゲットは名前で判定します。名前が `-test` で終わるか `-canary` を含むものです。
// `go test` か `terraform test` を起動するだけの runner（runners）は除きます。除外の前提が
// 崩れていないこと（名前が実在し、レシピがその道具を起動すること）も確かめます。
//
// 取り出しが黙って縮まないよう、2つの独立な経路を突き合わせます（決定15）。
//
//   - `Makefile` が `include` する `.mk` の集合と、`.makefiles/` に実在する `.mk` の集合
//   - `.mk` を読んで取り出したターゲットと、make が読んだ規則の集合（`make -p` の出力）
//
// 移行中の除外は、決定18 の形の宣言（期限・上限3ヶ月・対象の実在）だけが持ちます。
package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUnreadable = xerrors.New("入力を読み取れません")
	errNoTargets  = xerrors.New("検査対象が0件です")
	errMismatch   = xerrors.New("独立な経路の件数が食い違います")
	errViolations = xerrors.New("違反がある")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout, time.Now().UTC()); err != nil {
		log.Printf("❌ mk-test-lint: %v", err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errViolations):
		return 1
	default:
		return 2
	}
}

func run(args []string, out io.Writer, now time.Time) error {
	fs := flag.NewFlagSet("mk-test-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	makefile := fs.String("makefile", "Makefile", "include を読む Makefile")
	dir := fs.String("dir", ".makefiles", "`.mk` を置くディレクトリ")
	dbPath := fs.String("db", "", "make -pRrq の出力")
	dbExit := fs.Int("db-exit", -1, "make -pRrq の終了コード")
	bypassPath := fs.String("bypass", "", "移行中の除外の宣言")
	today := fs.String("today", "", "期限の判定に使う日付（YYYY-MM-DD。既定は今日）")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}
	day := now.Truncate(24 * time.Hour)
	if *today != "" {
		d, err := time.Parse(time.DateOnly, *today)
		if err != nil {
			return xerrors.Wrapf(errUnreadable, "-today %q", *today)
		}
		day = d
	}

	in, err := load(*makefile, *dir, *dbPath, *dbExit, *bypassPath)
	if err != nil {
		return err
	}
	res, err := check(in, day)
	if err != nil {
		return err
	}

	if len(res.findings) > 0 {
		lintreport.Sort(res.findings)
		fmt.Fprintf(out, "❌ mk-test-lint: %d 件の違反\n\n", len(res.findings))
		fmt.Fprintln(out, lintreport.Format(res.findings))
		fmt.Fprintln(out, "\nテストは運用機構の Go のテストへ置く（ADR-0702 決定25、.makefiles/README.md）。")
		return xerrors.Wrapf(errViolations, "%d 件", len(res.findings))
	}
	fmt.Fprintf(out, "✅ mk-test-lint: %d ファイル / %d ターゲット / 移行中 %d 件\n", res.files, res.targets, res.bypassed)
	return nil
}

// input は検査が読むものすべて。読み取りと判定を分け、判定を純粋に保つ。
type input struct {
	makefilePath string
	makefile     []byte
	mk           map[string][]byte // 実在する `.mk`（Makefile の置き場からの相対パス）
	db           []byte
	dbExit       int
	bypassPath   string
	bypass       []byte
}

func load(makefile, dir, dbPath string, dbExit int, bypassPath string) (input, error) {
	in := input{makefilePath: makefile, dbExit: dbExit, bypassPath: bypassPath, mk: map[string][]byte{}}
	var err error
	if in.makefile, err = os.ReadFile(filepath.Clean(makefile)); err != nil {
		return in, xerrors.Wrapf(errUnreadable, "%s", makefile)
	}
	if in.db, err = os.ReadFile(filepath.Clean(dbPath)); err != nil {
		return in, xerrors.Wrapf(errUnreadable, "make の出力 %q", dbPath)
	}
	if in.bypass, err = os.ReadFile(filepath.Clean(bypassPath)); err != nil {
		return in, xerrors.Wrapf(errUnreadable, "宣言 %q", bypassPath)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return in, xerrors.Wrapf(errUnreadable, "%s", dir)
	}
	defer func() { _ = root.Close() }()
	base, err := filepath.Rel(filepath.Dir(makefile), dir)
	if err != nil {
		return in, xerrors.Wrapf(errUnreadable, "%s: %v", dir, err)
	}
	walkErr := fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".mk" {
			return nil
		}
		raw, err := root.ReadFile(p)
		if err != nil {
			return err
		}
		in.mk[path.Join(filepath.ToSlash(base), p)] = raw
		return nil
	})
	if walkErr != nil {
		return in, xerrors.Wrapf(errUnreadable, "%s: %v", dir, walkErr)
	}
	return in, nil
}

type result struct {
	findings []lintreport.Finding
	files    int
	targets  int
	bypassed int
}

// check は、include と実在、`.mk` と make の規則を突き合わせ、テストのターゲットを判定する。
func check(in input, today time.Time) (result, error) {
	var res result
	if len(in.mk) == 0 {
		return res, xerrors.Wrap(errNoTargets, "`.mk` が1つもありません")
	}
	includes, err := parseIncludes(in.makefile)
	if err != nil {
		return res, xerrors.Wrapf(err, "%s", in.makefilePath)
	}
	if err := sameSet("include", includes, keys(in.mk)); err != nil {
		return res, err
	}
	db, err := parseDB(in.db, in.dbExit)
	if err != nil {
		return res, err
	}
	bypasses, err := parseBypass(in.bypass)
	if err != nil {
		return res, xerrors.Wrapf(err, "%s", in.bypassPath)
	}

	add := func(file string, line int, format string, a ...any) {
		res.findings = append(res.findings, lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf(format, a...)})
	}

	defined := map[string]target{}
	for _, path := range includes {
		mk, err := parseMk(in.mk[path])
		if err != nil {
			return res, xerrors.Wrapf(err, "%s", path)
		}
		names, err := reconcile(path, mk, db[path])
		if err != nil {
			return res, err
		}
		for _, name := range names {
			keep(defined, name, target{file: path, line: mk.line(name), recipe: mk.recipes[name]})
		}
	}
	// Makefile 自身の規則も同じ突き合わせに掛ける。include 行は上で読んだので外す。
	var top []string
	for _, line := range strings.Split(string(in.makefile), "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] == "include" {
			line = ""
		}
		top = append(top, line)
	}
	topMk, err := parseMk([]byte(strings.Join(top, "\n")))
	if err != nil {
		return res, xerrors.Wrapf(err, "%s", in.makefilePath)
	}
	topNames, err := reconcile(in.makefilePath, topMk, db[in.makefilePath])
	if err != nil {
		return res, err
	}
	for _, name := range topNames {
		keep(defined, name, target{file: in.makefilePath, line: topMk.line(name), recipe: topMk.recipes[name]})
	}
	// make が読んだ規則のうち、Makefile と include した `.mk` のどれにも帰さないものは数えられない。
	known := map[string]struct{}{in.makefilePath: {}}
	for _, p := range includes {
		known[p] = struct{}{}
	}
	for _, file := range keys(db) {
		if _, ok := known[file]; !ok {
			return res, xerrors.Wrapf(errMismatch, "make が %s から規則を読みましたが、Makefile にも include にもありません", file)
		}
	}
	if len(defined) == 0 {
		return res, xerrors.Wrap(errNoTargets, "ターゲットを1つも取り出せません")
	}
	res.files = len(includes)
	res.targets = len(defined)

	for _, name := range keys(runners) {
		if _, ok := defined[name]; !ok {
			add(in.makefilePath, 0, "除外する runner %s が定義されていません。除外の一覧（runners）を直してください", name)
		}
	}

	for _, name := range keys(defined) {
		t := defined[name]
		if !isTestName(name) || isRunner(name, t.recipe) {
			continue
		}
		if b, ok := bypasses[name]; ok && b.valid(today) {
			res.bypassed++
			continue
		}
		add(t.file, t.line, "%s はテストのターゲットです。テストは運用機構の Go のテストへ置きます", name)
	}

	limit := addMonths(today, maxBypassMonths)
	for _, name := range keys(bypasses) {
		b := bypasses[name]
		t, ok := defined[name]
		switch {
		case b.expires.Before(today):
			add(in.bypassPath, b.line, "%s の期限 %s が切れています。移すか、延長の判断を #%d に記録してください", name, b.expires.Format(time.DateOnly), b.issue)
		case b.expires.After(limit):
			add(in.bypassPath, b.line, "%s の期限 %s が上限（%s から %d ヶ月 = %s）を越えています", name, b.expires.Format(time.DateOnly), today.Format(time.DateOnly), maxBypassMonths, limit.Format(time.DateOnly))
		case !ok:
			add(in.bypassPath, b.line, "%s は定義されていません。移したならエントリを消してください", name)
		case !isTestName(name):
			add(in.bypassPath, b.line, "%s はテストの名前に当たりません。不要なエントリです", name)
		case isRunner(name, t.recipe):
			add(t.file, t.line, "%s は道具を起動するだけの runner として除外済みです。宣言のエントリは不要です", name)
		}
	}
	return res, nil
}

// keep は name の定義を記録する。レシピを持つ定義を優先し、複数あれば後のものを採る
// （make は後に読んだレシピで上書きする）。
func keep(defined map[string]target, name string, t target) {
	if _, ok := defined[name]; !ok || t.recipe != "" {
		defined[name] = t
	}
}

type target struct {
	file   string
	line   int
	recipe string
}

// reconcile は `.mk` から取り出したターゲットを、make が同じファイルに帰した規則と突き合わせ、
// そのファイルのターゲットの名前を返す。名前に変数を含む規則は make の展開した名前で数える。
func reconcile(path string, mk mkFile, fromDB map[string]struct{}) ([]string, error) {
	names := map[string]struct{}{}
	for name := range mk.literal {
		names[name] = struct{}{}
	}
	var missing []string
	for name := range mk.withRecipe {
		if _, ok := fromDB[name]; !ok {
			missing = append(missing, name)
		}
	}
	var extra []string
	for name := range fromDB {
		if _, ok := mk.withRecipe[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) != mk.unresolved {
		return nil, xerrors.Wrapf(errMismatch,
			"%s: レシピを持つ規則のうち make に無いもの %v、make にだけあるもの %v（名前に変数を含む規則 %d 件）",
			path, missing, extra, mk.unresolved)
	}
	for _, name := range extra {
		names[name] = struct{}{}
	}
	return keys(names), nil
}

func sameSet(what string, declared, actual []string) error {
	d := map[string]struct{}{}
	for _, p := range declared {
		d[p] = struct{}{}
	}
	a := map[string]struct{}{}
	for _, p := range actual {
		a[p] = struct{}{}
	}
	var notIncluded, notFound []string
	for p := range a {
		if _, ok := d[p]; !ok {
			notIncluded = append(notIncluded, p)
		}
	}
	for p := range d {
		if _, ok := a[p]; !ok {
			notFound = append(notFound, p)
		}
	}
	if len(notIncluded) == 0 && len(notFound) == 0 {
		return nil
	}
	sort.Strings(notIncluded)
	sort.Strings(notFound)
	return xerrors.Wrapf(errMismatch, "%s: include されない `.mk` %v、実在しない include %v", what, notIncluded, notFound)
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isTestName(name string) bool {
	return strings.HasSuffix(name, "-test") || strings.Contains(name, "-canary")
}
