package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// parse は、ソースの断片を構文木にします。実物のツリーを読まずに宣言の形だけを与えるためです。
func parse(t *testing.T, src string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", "package p\n"+src, parser.SkipObjectResolution)
	require.NoError(t, err)
	return fset, file
}

func decl(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	_, file := parse(t, src)
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			return fn
		}
	}
	require.FailNow(t, "関数宣言がありません")
	return nil
}

// writeGo は、検査対象になる木を一時ディレクトリへ組み立てます。
func writeGo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}
	return root
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("対象がそれぞれ1つの TestXxx を持てば件数を添えて成功する", func(t *testing.T) {
			t.Parallel()
			root := writeGo(t, map[string]string{
				"a/a.go":      "package a\n\nfunc Foo() {}\n",
				"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n",
			})
			var out bytes.Buffer
			require.NoError(t, run([]string{"-root", root}, &out))
			assert.Contains(t, out.String(), "対象 1 件")
		})

		t.Run("件数はディレクトリではなく対象そのものを数える", func(t *testing.T) {
			t.Parallel()
			root := writeGo(t, map[string]string{
				"a/a.go":      "package a\n\nfunc Foo() {}\nfunc Bar() {}\n",
				"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\nfunc TestBar(t *testing.T) {}\n",
				"b/b.go":      "package b\n\nfunc Baz() {}\n",
				"b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestBaz(t *testing.T) {}\n",
			})
			var out bytes.Buffer
			require.NoError(t, run([]string{"-root", root}, &out))
			assert.Contains(t, out.String(), "対象 3 件", "2ディレクトリ・3対象")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("対応する TestXxx が無ければ違反として返す", func(t *testing.T) {
			t.Parallel()
			root := writeGo(t, map[string]string{"a/a.go": "package a\n\nfunc Foo() {}\n"})
			var out bytes.Buffer
			require.Error(t, run([]string{"-root", root}, &out))
			assert.Contains(t, out.String(), "TestXxx がありません")
		})

		t.Run("宣言を持たない Go しか無ければ合格ではなく番兵で返す", func(t *testing.T) {
			t.Parallel()
			// 空のスライスでもキーは生まれるため、ディレクトリ数を見る番兵はここで素通りする。
			root := writeGo(t, map[string]string{"a/a.go": "package a\n\nvar x = 1\n"})
			var out bytes.Buffer
			err := run([]string{"-root", root}, &out)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoSubject))
		})
		t.Run("対象が1件も無ければ合格ではなく番兵で返す", func(t *testing.T) {
			t.Parallel()
			root := writeGo(t, map[string]string{"a/README.md": "対象の Go が無い\n"})
			var out bytes.Buffer
			err := run([]string{"-root", root}, &out)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoSubject))
		})

		t.Run("走査先が無ければエラーを返す", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.Error(t, run([]string{"-root", filepath.Join(t.TempDir(), "absent")}, &out))
		})

		t.Run("解釈できない引数はエラーを返す", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.Error(t, run([]string{"-no-such-flag"}, &out))
		})
	})
}

func Test_scan(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("production と test を分けて集め、走査しない場所を飛ばす", func(t *testing.T) {
			t.Parallel()
			root := writeGo(t, map[string]string{
				"a/a.go":              "package a\n\nfunc Foo() {}\n",
				"a/a_test.go":         "package a\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\nfunc helper() {}\n",
				"a/testdata/bad.go":   "package a\n\nfunc NotASubject() {}\n",
				"a/node_modules/x.go": "package a\n\nfunc AlsoNot() {}\n",
				"a/notes.md":          "対象ではない\n",
			})
			subjects, tests, findings, err := scan(root)
			require.NoError(t, err)
			assert.Empty(t, findings)

			dir := filepath.ToSlash(filepath.Join(root, "a"))
			require.Len(t, subjects, 1, "testdata と node_modules は対象のディレクトリとして現れない")
			require.Len(t, subjects[dir], 1)
			assert.Equal(t, "Foo", subjects[dir][0].name)
			assert.Contains(t, tests[dir], "TestFoo")
			assert.NotContains(t, tests[dir], "helper", "テストファイルのヘルパは TestXxx ではない")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("解析できない Go があればエラー", func(t *testing.T) {
			t.Parallel()
			root := writeGo(t, map[string]string{"a/a.go": "これは Go ではない\n"})
			_, _, _, err := scan(root)
			require.Error(t, err)
		})

		t.Run("走査先が無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, _, _, err := scan(filepath.Join(t.TempDir(), "absent"))
			require.Error(t, err)
		})
	})
}

func Test_indexTestFile(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("TestXxx だけを控える", func(t *testing.T) {
			t.Parallel()
			fset, file := parse(t, "func TestFoo(t *testing.T) {}\nfunc helper() {}\nfunc Testing() {}\n")
			tests := map[string]map[string]testFunc{"a": {}}
			findings := indexTestFile(fset, file, "a/a_test.go", "a", tests)
			assert.Empty(t, findings)
			assert.Contains(t, tests["a"], "TestFoo")
			assert.Contains(t, tests["a"], "Testing", "Test で始まる名前はすべて控える")
			assert.NotContains(t, tests["a"], "helper")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("skip の書き方の違反をそのまま返す", func(t *testing.T) {
			t.Parallel()
			fset, file := parse(t, "func TestFoo(t *testing.T) { t.Skip() }\n")
			tests := map[string]map[string]testFunc{"a": {}}
			findings := indexTestFile(fset, file, "a/a_test.go", "a", tests)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "リテラルの理由がありません")
		})
	})
}

func Test_checkSkip(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"skip しなければ何も言わない":      "func TestFoo(t *testing.T) { _ = 1 }",
			"理由のある skip は通る":        `func TestFoo(t *testing.T) { t.Skip("失敗経路が呼び出し元を終わらせるため検証できない") }`,
			"入れ子の中の skip も理由があれば通る": `func TestFoo(t *testing.T) { if true { t.Skip("環境が無い") } }`,
		}

		for name, src := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				fset, _ := parse(t, src)
				assert.Empty(t, checkSkip(fset, decl(t, src), "a/a_test.go"))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src         string
			wantMessage string
		}{
			"理由が無い":        {src: "func TestFoo(t *testing.T) { t.Skip() }", wantMessage: "リテラルの理由がありません"},
			"理由が空":         {src: `func TestFoo(t *testing.T) { t.Skip("") }`, wantMessage: "リテラルの理由がありません"},
			"理由が空白だけ":      {src: `func TestFoo(t *testing.T) { t.Skip("   ") }`, wantMessage: "リテラルの理由がありません"},
			"理由が変数":        {src: "func TestFoo(t *testing.T) { t.Skip(reason) }", wantMessage: "リテラルの理由がありません"},
			"理由が他のテストの名指し": {src: `func TestFoo(t *testing.T) { t.Skip("TestBar がカバーしている") }`, wantMessage: "別のテストを理由にしています"},
			"Skipf も同じ":    {src: "func TestFoo(t *testing.T) { t.Skipf() }", wantMessage: "リテラルの理由がありません"},
			"SkipNow も同じ":  {src: "func TestFoo(t *testing.T) { t.SkipNow() }", wantMessage: "リテラルの理由がありません"},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				fset, _ := parse(t, tt.src)
				findings := checkSkip(fset, decl(t, tt.src), "a/a_test.go")
				require.Len(t, findings, 1)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
				assert.Equal(t, "a/a_test.go", findings[0].File)
			})
		}
	})
}

func Test_stringArg(t *testing.T) {
	t.Parallel()

	arg := func(t *testing.T, src string) *ast.CallExpr {
		t.Helper()
		var call *ast.CallExpr
		ast.Inspect(decl(t, "func f() { "+src+" }"), func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && call == nil {
				call = c
			}
			return true
		})
		require.NotNil(t, call)
		return call
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
			ok   bool
		}{
			"空でない文字列リテラル": {src: `g("理由")`, want: "理由", ok: true},
			"引数が無い":       {src: "g()", ok: false},
			"空文字":         {src: `g("")`, ok: false},
			"空白だけ":        {src: `g("  ")`, ok: false},
			"リテラルでない":     {src: "g(reason)", ok: false},
			"文字列でないリテラル":  {src: "g(1)", ok: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, ok := stringArg(arg(t, tt.src))
				assert.Equal(t, tt.ok, ok)
				if tt.ok {
					assert.Equal(t, tt.want, got)
				}
			})
		}
	})
}

func Test_collectSubjects(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言を集め、main と init は対象にしない", func(t *testing.T) {
			t.Parallel()
			src := "func Foo() {}\nfunc bar() {}\nfunc (c Client) Fetch() {}\nfunc main() {}\nfunc init() {}\n"
			fset, file := parse(t, src)
			subjects := collectSubjects(fset, file, "a/a.go", "a")

			var names []string
			for _, s := range subjects {
				names = append(names, s.name)
			}
			assert.Equal(t, []string{"Foo", "bar", "(Client).Fetch"}, names)
			assert.Equal(t, "a/a.go", subjects[0].file)
			assert.Positive(t, subjects[0].line)
		})

		t.Run("分岐が無い関数も対象にする", func(t *testing.T) {
			t.Parallel()
			fset, file := parse(t, "func Name() string { return \"x\" }\n")
			assert.Len(t, collectSubjects(fset, file, "a/a.go", "a"), 1)
		})
	})
}

func Test_expected(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src            string
			wantName       string
			wantCandidates []string
			wantOK         bool
		}{
			"公開された関数": {
				src: "func Foo() {}", wantName: "Foo",
				wantCandidates: []string{"TestFoo", "Test_Foo"}, wantOK: true,
			},
			"非公開の関数": {
				src: "func parseVersion() {}", wantName: "parseVersion",
				wantCandidates: []string{"Test_parseVersion"}, wantOK: true,
			},
			"値レシーバのメソッド": {
				src: "func (c Client) Fetch() {}", wantName: "(Client).Fetch",
				wantCandidates: []string{"TestClient_Fetch", "Test_Client_Fetch"}, wantOK: true,
			},
			"ポインタレシーバのメソッド": {
				src: "func (c *Client) Fetch() {}", wantName: "(Client).Fetch",
				wantCandidates: []string{"TestClient_Fetch", "Test_Client_Fetch"}, wantOK: true,
			},
			"レシーバ名を省いたメソッド": {
				src: "func (*Client) Fetch() {}", wantName: "(Client).Fetch",
				wantCandidates: []string{"TestClient_Fetch", "Test_Client_Fetch"}, wantOK: true,
			},
			"型引数を持つレシーバ": {
				src: "func (b Box[T]) Get() {}", wantName: "(Box).Get",
				wantCandidates: []string{"TestBox_Get", "Test_Box_Get"}, wantOK: true,
			},
			"型引数を持つ関数": {
				src: "func Map[T any]() {}", wantName: "Map",
				wantCandidates: []string{"TestMap", "Test_Map"}, wantOK: true,
			},
			"main は対象でない": {src: "func main() {}", wantOK: false},
			"init は対象でない": {src: "func init() {}", wantOK: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				gotName, gotCandidates, ok := expected(decl(t, tt.src))
				assert.Equal(t, tt.wantOK, ok)
				if tt.wantOK {
					assert.Equal(t, tt.wantName, gotName)
					assert.Equal(t, tt.wantCandidates, gotCandidates)
				}
			})
		}
	})
}

func Test_funcCandidates(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("公開された名前は2つの形を認める", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"TestFoo", "Test_Foo"}, funcCandidates("Foo"))
		})

		t.Run("非公開の名前はアンダースコアを挟む形だけ", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"Test_foo"}, funcCandidates("foo"), "正本が Testfoo という形を挙げていない")
		})
	})
}

func Test_receiverName(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
			ok   bool
		}{
			"値レシーバ":          {src: "func (c Client) F() {}", want: "Client", ok: true},
			"ポインタレシーバ":       {src: "func (c *Client) F() {}", want: "Client", ok: true},
			"レシーバ名の省略":       {src: "func (*Client) F() {}", want: "Client", ok: true},
			"型引数が1つ":         {src: "func (b Box[T]) F() {}", want: "Box", ok: true},
			"型引数が2つ":         {src: "func (p Pair[K, V]) F() {}", want: "Pair", ok: true},
			"ポインタと型引数の組み合わせ": {src: "func (b *Box[T]) F() {}", want: "Box", ok: true},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, ok := receiverName(decl(t, tt.src).Recv)
				assert.Equal(t, tt.ok, ok)
				if tt.ok {
					assert.Equal(t, tt.want, got)
				}
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("レシーバが空なら型名を返さない", func(t *testing.T) {
			t.Parallel()
			_, ok := receiverName(&ast.FieldList{})
			assert.False(t, ok)
		})
	})
}

func Test_matchSubjects(t *testing.T) {
	t.Parallel()

	sub := subject{
		dir: "a", file: "a/a.go", line: 3,
		name: "Foo", candidates: []string{"TestFoo", "Test_Foo"},
	}
	subjects := map[string][]subject{"a": {sub}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ちょうど1つ在れば違反にしない", func(t *testing.T) {
			t.Parallel()
			tests := map[string]map[string]testFunc{"a": {"TestFoo": {}}}
			assert.Empty(t, matchSubjects(subjects, tests))
		})

		t.Run("別のディレクトリの同名は数えない", func(t *testing.T) {
			t.Parallel()
			tests := map[string]map[string]testFunc{"b": {"TestFoo": {}}}
			findings := matchSubjects(subjects, tests)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "ありません")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			have        map[string]testFunc
			wantMessage string
		}{
			"1つも無い": {have: map[string]testFunc{}, wantMessage: "TestXxx がありません（期待: TestFoo / Test_Foo）"},
			"2つ在る":  {have: map[string]testFunc{"TestFoo": {}, "Test_Foo": {}}, wantMessage: "TestXxx が複数あります"},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings := matchSubjects(subjects, map[string]map[string]testFunc{"a": tt.have})
				require.Len(t, findings, 1)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
				assert.Equal(t, lintreport.Finding{File: "a/a.go", Line: 3, Message: findings[0].Message}, findings[0])
			})
		}
	})
}
