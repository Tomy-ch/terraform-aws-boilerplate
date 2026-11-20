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
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
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
	if err := run(os.Args[1:], os.Stdout, time.Now().UTC(), makeDatabase); err != nil {
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

// makeDatabase は make が読んだ規則の一覧（`make -pRrq`）と、その終了コードを返す。make を起動
// できなかったときだけエラーを返す。終了コードの判定は parseDB が持つ。
func makeDatabase(makefile string) ([]byte, int, error) {
	cmd := exec.CommandContext(context.Background(), "make", "--no-print-directory", "-pRrq", "-f", filepath.Base(makefile), "help") //nolint:gosec // 引数は固定と、Makefile のパスだけ
	cmd.Dir = filepath.Dir(makefile)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.Bytes(), 0, nil
	case errors.As(err, &exitErr):
		return stdout.Bytes(), exitErr.ExitCode(), nil
	default:
		return nil, -1, xerrors.Wrapf(errUnreadable, "make を起動できません: %v", err)
	}
}

func run(args []string, out io.Writer, now time.Time, database func(string) ([]byte, int, error)) error {
	fs := flag.NewFlagSet("mk-test-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	makefile := fs.String("makefile", "Makefile", "include を読む Makefile")
	dir := fs.String("dir", ".makefiles", "`.mk` を置くディレクトリ")
	bypassPath := fs.String("bypass", "", "移行中の除外の宣言")
	scopePath := fs.String("scope", "", "分岐の検査を掛ける `.mk` の範囲の宣言")
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

	in, err := load(*makefile, *dir, *bypassPath, *scopePath)
	if err != nil {
		return err
	}
	if in.db, in.dbExit, err = database(*makefile); err != nil {
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
		fmt.Fprintln(out, "\nレシピは運用機構と道具を順に起動するだけにし、判定・分岐・繰り返しとテストは運用機構の Go が持つ（ADR-0702 決定25）。")
		return xerrors.Wrapf(errViolations, "%d 件", len(res.findings))
	}
	fmt.Fprintf(out, "✅ mk-test-lint: %d ファイル / %d ターゲット / 移行中 %d 件 / 分岐の検査の範囲 %d ファイル\n", res.files, res.targets, res.bypassed, res.scoped)
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
	scopePath    string
	scope        []byte
}

func load(makefile, dir, bypassPath, scopePath string) (input, error) {
	in := input{makefilePath: filepath.Base(makefile), bypassPath: bypassPath, scopePath: scopePath, mk: map[string][]byte{}}
	var err error
	if in.makefile, err = os.ReadFile(filepath.Clean(makefile)); err != nil {
		return in, xerrors.Wrapf(errUnreadable, "%s", makefile)
	}
	if in.scope, err = os.ReadFile(filepath.Clean(scopePath)); err != nil {
		return in, xerrors.Wrapf(errUnreadable, "範囲の宣言 %q", scopePath)
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
	scoped   int
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
	// include を Makefile から辿る。`.mk` が別の `.mk` を include してもよい。
	parsed := map[string]mkFile{}
	order := []string{}
	seen := map[string]struct{}{}
	push := func(paths []string) {
		for _, p := range paths {
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				order = append(order, p)
			}
		}
	}
	push(includes)
	for i := 0; i < len(order); i++ {
		raw, ok := in.mk[order[i]]
		if !ok {
			continue // 実在しない include は下の突き合わせが落とす
		}
		mk, err := parseMk(raw)
		if err != nil {
			return res, xerrors.Wrapf(err, "%s", order[i])
		}
		parsed[order[i]] = mk
		push(mk.includes)
	}
	if err := sameSet("include", order, keys(in.mk)); err != nil {
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
	for _, path := range order {
		mk := parsed[path]
		names, err := reconcile(path, mk, db[path])
		if err != nil {
			return res, err
		}
		for _, name := range names {
			keep(defined, name, target{file: path, line: mk.line(name), recipe: mk.recipes[name]})
		}
	}
	// Makefile 自身の規則も同じ突き合わせに掛ける。
	topMk, err := parseMk(in.makefile)
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
	for _, p := range order {
		known[p] = struct{}{}
	}
	for _, file := range keys(db) {
		if _, ok := known[file]; !ok {
			return res, xerrors.Wrapf(errMismatch, "make が %s から規則を読みましたが、Makefile にも include にもありません", file)
		}
	}
	// 範囲の `.mk` のレシピと、そこから参照される変数の本体に、決定25 の閉じた一覧に当たる書き方が無いこと。
	scope, err := parseScope(in.scope)
	if err != nil {
		return res, xerrors.Wrapf(err, "%s", in.scopePath)
	}
	vars := map[string][]varDef{}
	all := map[string]mkFile{in.makefilePath: topMk}
	for p, mk := range parsed {
		all[p] = mk
	}
	for _, p := range keys(all) {
		for name, body := range all[p].vars {
			vars[name] = append(vars[name], varDef{file: p, body: body})
		}
	}
	reported := map[finding]struct{}{} // 同じ変数を範囲の複数のファイルが参照しても、1度だけ報告する
	for _, p := range scope {
		mk, ok := all[p]
		if !ok {
			return res, xerrors.Wrapf(errMismatch, "%s: 範囲の宣言の %s は Makefile にも include にもありません", in.scopePath, p)
		}
		for _, f := range recipeFindings(p, mk, vars) {
			if _, dup := reported[f]; dup {
				continue
			}
			reported[f] = struct{}{}
			add(f.file, f.line, "%s を、%sが持ちます", f.what, f.where)
		}
	}
	res.scoped = len(scope)
	if len(defined) == 0 {
		return res, xerrors.Wrap(errNoTargets, "ターゲットを1つも取り出せません")
	}
	res.files = len(order)
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
