// test-mapping は、関数・メソッドとテストが 1:1 で対応していることを検査します。
//
// 規約の正本は .claude/skills/scaffold-test/SKILL.md であり、ここが持つのはその写しではなく
// **判定**です。期待する名前は次の形です。
//
//	公開された関数 Foo   -> TestFoo / Test_Foo
//	非公開の関数 foo     -> Test_foo
//	メソッド (T) Bar    -> TestT_Bar / Test_T_Bar
//
// 関数は正本が公開・非公開それぞれの形を述べているので、その形だけを受けます。**メソッドは
// 正本がどちらを正とするか決めていない**ため、レシーバの型が公開かどうかに依らず両方受けます
// —— 述べられていない場合に道具が決めないようにしてあります。ただし**両方在るのは違反**です
// —— 同じ対象に2つの枠があることは、対象が2つに割れたことを意味します。
//
// 検証できない対象の逃げ道は allowlist ではなく、**規約どおりの名前の TestXxx を宣言し、
// その中で t.Skip("<なぜ検証できないか>") を書くこと**です。理由の無い skip と、他のテストを
// 名指しした skip は違反として扱います —— 後者は、名指しされたテストが縮んだ日も緑のまま
// 残るためです。
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	toolName    = "test-mapping"
	defaultRoot = "scripts"
	testSuffix  = "_test.go"
)

// skip の理由が別のテストを名指ししている形（免除としない理由は
// .claude/skills/scaffold-test/SKILL.md の規則2）。
var coveringTest = regexp.MustCompile(`Test[A-Z_]`)

// 番兵（ADR-0702 決定13）。検査対象0件を成功として終えない。
var errNoSubject = xerrors.New("検査対象の関数・メソッドが1件も見つかりません")

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoSubject) {
			log.Printf("❌ %s: %v", toolName, err)
			os.Exit(2)
		}
		log.Fatalf("❌ %s: %v", toolName, err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet(toolName, flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", defaultRoot, "走査するディレクトリ")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	subjects, tests, findings, err := scan(*root)
	if err != nil {
		return err
	}
	// 数えるのはディレクトリではなく対象そのものである。ディレクトリ数を代わりに使うと、
	// 報告される件数が対象の数でなくなり、番兵も「対象が在る」と誤って答える。
	count := 0
	for _, s := range subjects {
		count += len(s)
	}
	if count == 0 {
		return errNoSubject
	}

	findings = append(findings, matchSubjects(subjects, tests)...)

	if len(findings) > 0 {
		lintreport.Sort(findings)
		fmt.Fprintf(out, "❌ %s: %d 件の不整合\n\n", toolName, len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の不整合", len(findings))
	}

	fmt.Fprintf(out, "✅ %s: 対象 %d 件が、それぞれ 1 つの TestXxx を持つことを確認しました\n", toolName, count)
	return nil
}

// subject は、テストを持つべき関数・メソッド1件です。
type subject struct {
	dir        string
	file       string
	line       int
	name       string   // 報告に使う名前。メソッドは (T).Bar の形。
	candidates []string // 規約が認める TestXxx の名前
}

// testFunc は、テストファイルが宣言している TestXxx 1件です。
type testFunc struct {
	file string
	line int
}

// scan は root を走査し、対象と TestXxx、および skip の書き方の違反を集めます。
func scan(root string) (map[string][]subject, map[string]map[string]testFunc, []lintreport.Finding, error) {
	subjects := map[string][]subject{}
	tests := map[string]map[string]testFunc{}
	var findings []lintreport.Finding

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return xerrors.Wrapf(err, "%s の解析", path)
		}
		rel := filepath.ToSlash(path)
		dir := filepath.ToSlash(filepath.Dir(path))

		if strings.HasSuffix(path, testSuffix) {
			if tests[dir] == nil {
				tests[dir] = map[string]testFunc{}
			}
			findings = append(findings, indexTestFile(fset, file, rel, dir, tests)...)
			return nil
		}
		subjects[dir] = append(subjects[dir], collectSubjects(fset, file, rel, dir)...)
		return nil
	})
	if err != nil {
		return nil, nil, nil, xerrors.Wrapf(err, "%s の走査", root)
	}
	return subjects, tests, findings, nil
}

// indexTestFile は、テストファイルの TestXxx を控え、skip の書き方を検査します。
func indexTestFile(fset *token.FileSet, file *ast.File, rel, dir string, tests map[string]map[string]testFunc) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") {
			continue
		}
		line := fset.Position(fn.Pos()).Line
		tests[dir][fn.Name.Name] = testFunc{file: rel, line: line}
		findings = append(findings, checkSkip(fset, fn, rel)...)
	}
	return findings
}

// checkSkip は、TestXxx が skip するなら、その理由が要件を満たすかを検査します。
func checkSkip(fset *token.FileSet, fn *ast.FuncDecl, rel string) []lintreport.Finding {
	var findings []lintreport.Finding
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		// SkipNow を外すと、理由を書かせる規律を**引数を取らない API で丸ごと迂回できる**。
		if !ok || (sel.Sel.Name != "Skip" && sel.Sel.Name != "Skipf" && sel.Sel.Name != "SkipNow") {
			return true
		}
		line := fset.Position(call.Pos()).Line
		reason, ok := stringArg(call)
		switch {
		case !ok:
			findings = append(findings, lintreport.Finding{
				File: rel, Line: line,
				Message: fn.Name.Name + " の skip に、リテラルの理由がありません",
			})
		case coveringTest.MatchString(reason):
			findings = append(findings, lintreport.Finding{
				File: rel, Line: line,
				Message: fn.Name.Name + " の skip が別のテストを理由にしています（そのテストが縮んでも緑のまま残ります）",
			})
		}
		return true
	})
	return findings
}

// stringArg は、呼び出しの第1引数が空でない文字列リテラルならその中身を返します。
func stringArg(call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// collectSubjects は、production のファイルからテストを持つべき対象を集めます。
//
// 分岐の有無や body の行数では絞りません。単純な getter も包みも契約を持ち得るうえ、
// 「短いから要らない」という線を道具が引くと、その線の内側は永久に検査されなくなります。
func collectSubjects(fset *token.FileSet, file *ast.File, rel, dir string) []subject {
	var subjects []subject
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name, candidates, ok := expected(fn)
		if !ok {
			continue
		}
		subjects = append(subjects, subject{
			dir: dir, file: rel, line: fset.Position(fn.Pos()).Line,
			name: name, candidates: candidates,
		})
	}
	return subjects
}

// expected は、宣言に対する報告名と、規約が認める TestXxx の名前を返します。
func expected(fn *ast.FuncDecl) (name string, candidates []string, ok bool) {
	if fn.Recv == nil {
		// main と init は呼び出し側が居らず、規約の対象ではない。
		if fn.Name.Name == "main" || fn.Name.Name == "init" {
			return "", nil, false
		}
		return fn.Name.Name, funcCandidates(fn.Name.Name), true
	}
	recv, ok := receiverName(fn.Recv)
	if !ok {
		return "", nil, false
	}
	base := recv + "_" + fn.Name.Name
	return "(" + recv + ")." + fn.Name.Name, []string{"Test" + base, "Test_" + base}, true
}

// funcCandidates は、関数名に対して規約が認める TestXxx の名前を返します。
//
// 非公開の名前に対して `Testfoo` を候補へ入れないのは、正本がその形を挙げておらず、
// 期待名として提示すると**規約に無い名前を道具が教える**ことになるためです。
func funcCandidates(name string) []string {
	if ast.IsExported(name) {
		return []string{"Test" + name, "Test_" + name}
	}
	return []string{"Test_" + name}
}

// receiverName は、レシーバの型名を返します。ポインタと型引数は剥がします。
func receiverName(recv *ast.FieldList) (string, bool) {
	if len(recv.List) == 0 {
		return "", false
	}
	expr := recv.List[0].Type
	for {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
		case *ast.IndexExpr: // func (r Box[T]) ...
			expr = t.X
		case *ast.IndexListExpr: // func (r Pair[K, V]) ...
			expr = t.X
		case *ast.Ident:
			return t.Name, true
		default:
			return "", false
		}
	}
}

// matchSubjects は、対象1件ごとに TestXxx がちょうど1つ在ることを検査します。
func matchSubjects(subjects map[string][]subject, tests map[string]map[string]testFunc) []lintreport.Finding {
	var findings []lintreport.Finding
	dirs := make([]string, 0, len(subjects))
	for dir := range subjects {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		for _, s := range subjects[dir] {
			var found []string
			for _, c := range s.candidates {
				if _, ok := tests[dir][c]; ok {
					found = append(found, c)
				}
			}
			switch len(found) {
			case 1:
				continue
			case 0:
				findings = append(findings, lintreport.Finding{
					File: s.file, Line: s.line,
					Message: s.name + " に対応する TestXxx がありません（期待: " + strings.Join(s.candidates, " / ") + "）",
				})
			default:
				findings = append(findings, lintreport.Finding{
					File: s.file, Line: s.line,
					Message: s.name + " に対応する TestXxx が複数あります: " + strings.Join(found, " / "),
				})
			}
		}
	}
	return findings
}
