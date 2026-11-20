package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sexpr は、構文木を括弧で結合を示す字面にします。
func sexpr(n *exprNode) string {
	switch n.kind {
	case nodeLiteral:
		if n.litKind == tokString {
			return "'" + n.text + "'"
		}
		return n.text
	case nodeIdent:
		return n.text
	case nodeMember:
		return sexpr(n.args[0]) + "." + n.text
	case nodeIndex:
		return sexpr(n.args[0]) + "[" + sexpr(n.args[1]) + "]"
	case nodeCall:
		args := make([]string, 0, len(n.args))
		for _, a := range n.args {
			args = append(args, sexpr(a))
		}
		return n.op + "(" + strings.Join(args, ", ") + ")"
	case nodeNot:
		return "!" + sexpr(n.args[0])
	}
	return "(" + sexpr(n.args[0]) + " " + n.op + " " + sexpr(n.args[1]) + ")"
}

func mustParse(t *testing.T, src string) *exprNode {
	t.Helper()
	n, err := parseExpr(src)
	require.NoError(t, err, src)
	return n
}

func newParser(t *testing.T, src string) *exprParser {
	t.Helper()
	toks, err := lexExpr(src)
	require.NoError(t, err)
	return &exprParser{toks: toks}
}

func Test_lexExpr(t *testing.T) {
	t.Parallel()

	type tok struct {
		kind tokenKind
		text string
	}
	cases := map[string]struct {
		src  string
		want []tok
	}{
		"識別子は - を含む":                    {"needs.secret-scan.result", []tok{{tokIdent, "needs"}, {tokPunct, "."}, {tokIdent, "secret-scan"}, {tokPunct, "."}, {tokIdent, "result"}}},
		"'' は ' を表す":                    {"'a''b'", []tok{{tokString, "a'b"}}},
		"空の文字列リテラル":                     {"''", []tok{{tokString, ""}}},
		"負数・16進・指数・小数点で終わる数":            {"-1 0xff 2.5e-3 1.", []tok{{tokNumber, "-1"}, {tokNumber, "0xff"}, {tokNumber, "2.5e-3"}, {tokNumber, "1."}}},
		"true / false / null は大文字でも予約語": {"true NULL False", []tok{{tokKeyword, "true"}, {tokKeyword, "NULL"}, {tokKeyword, "False"}}},
		"長い演算子を先に読む":                    {"!=<=>=!<>", []tok{{tokPunct, "!="}, {tokPunct, "<="}, {tokPunct, ">="}, {tokPunct, "!"}, {tokPunct, "<"}, {tokPunct, ">"}}},
		"改行とタブは区切り":                     {"a\n\t&&\r\nb", []tok{{tokIdent, "a"}, {tokPunct, "&&"}, {tokIdent, "b"}}},
		"空の式は字句が無い":                     {"  ", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := lexExpr(tc.src)
			require.NoError(t, err)
			var kinds []tok
			for _, g := range got {
				kinds = append(kinds, tok{g.kind, g.text})
			}
			assert.Equal(t, tc.want, kinds)
		})
	}
	t.Run("字句の位置を返す", func(t *testing.T) {
		t.Parallel()
		got, err := lexExpr(" a == 'b'")
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, []int{1, 2, 3, 5, 6, 9}, []int{got[0].start, got[0].end, got[1].start, got[1].end, got[2].start, got[2].end})
	})
	for name, src := range map[string]string{
		"閉じない文字列リテラルは失敗する":       "a == 'b",
		"二重引用符は字句ではない":           `a == "b"`,
		"= 1文字は字句ではない":           "a === b",
		"$ は字句ではない":              "${{ a }}",
		"- の後ろが数でなければ失敗する":       "a - b",
		"& 1文字は字句ではない":           "a & b",
		"| 1文字は字句ではない":           "a | b",
		"重ねた ' で終わる文字列リテラルは閉じない": "'a''",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := lexExpr(src)
			require.ErrorIs(t, err, errExpression)
		})
	}
}

func Test_parseExpr(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ src, want string }{
		"|| は && より弱く、== は < より弱い": {"a || b && c == d < e", "(a || (b && (c == (d < e))))"},
		"同じ強さの演算子は左結合":             {"a == b != c", "((a == b) != c)"},
		"! は比較より強い":                {"!a == b", "(!a == b)"},
		"括弧は結合を変える":                {"(a || b) && c", "((a || b) && c)"},
		"関数名は小文字で持つ":               {"StartsWith(a, 'b')", "startswith(a, 'b')"},
		"引数の無い関数を読む":               {"always() && success()", "(always() && success())"},
		"後置の . と [] と .* を読む":      {"a.b[0]['c'].*.d", "a.b[0]['c'].*.d"},
		"|| の連なりは左結合":              {"a || b || c", "((a || b) || c)"},
		"&& の連なりは左結合":              {"a && b && c", "((a && b) && c)"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, sexpr(mustParse(t, tc.src)))
		})
	}
	for name, src := range map[string]string{
		"空の式は失敗する":               "",
		"空白だけの式は失敗する":            " \n ",
		"読み残しがあれば失敗する":           "a b",
		"字句の誤りは失敗する":             "a == 'b",
		"構文の誤りは失敗する":             "a ==",
		"閉じない括弧は失敗する":            "(a == b",
		"未知の関数は失敗する":             "lower(github.ref) == 'main'",
		"引数の数が合わなければ失敗する":        "startsWith(github.ref)",
		"case の引数が偶数なら失敗する":      "case(a, 'b', c, 'd')",
		"引数の上限を超えれば失敗する":         "toJSON(a, b)",
		"引数の無い format は失敗する":     "format()",
		"引数の無い case は失敗する":       "case()",
		"引数が1つの case は失敗する":      "case(a)",
		"引数を取らない関数に引数があれば失敗する":   "always(a)",
		"contains の引数が3つなら失敗する":  "contains(a, b, c)",
		"context でない値の呼び出しは失敗する": "a.b(c)",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseExpr(src)
			require.ErrorIs(t, err, errExpression)
		})
	}
}

func Test_exprParser_peek(t *testing.T) {
	t.Parallel()

	p := newParser(t, "( a")
	assert.True(t, p.peek("("))
	assert.False(t, p.peek(")"))
	p.pos = 1
	assert.False(t, p.peek("a"), "識別子は記号として当たらない")
	p.pos = 2
	assert.False(t, p.peek("("), "字句の末尾では当たらない")
}

func Test_exprParser_expect(t *testing.T) {
	t.Parallel()

	p := newParser(t, ") a")
	got, err := p.expect(")")
	require.NoError(t, err)
	assert.Equal(t, ")", got.text)
	assert.Equal(t, 1, p.pos)

	_, err = p.expect(")")
	require.ErrorIs(t, err, errExpression)
	assert.Equal(t, 1, p.pos, "当たらなければ進まない")
}

func Test_exprParser_unexpected(t *testing.T) {
	t.Parallel()

	p := newParser(t, "a ]")
	p.pos = 1
	err := p.unexpected()
	require.ErrorIs(t, err, errExpression)
	assert.ErrorContains(t, err, `3 文字目の "]"`)

	p.pos = 2
	err = p.unexpected()
	require.ErrorIs(t, err, errExpression)
	assert.ErrorContains(t, err, "途中で終わっています")
}

func Test_exprParser_parseBinary(t *testing.T) {
	t.Parallel()

	t.Run("minPrec より弱い演算子の手前で止まる", func(t *testing.T) {
		t.Parallel()
		p := newParser(t, "a == b || c")
		n, err := p.parseBinary(binaryPrec["=="])
		require.NoError(t, err)
		assert.Equal(t, "(a == b)", sexpr(n))
		assert.True(t, p.peek("||"))
	})
	t.Run("演算子でない記号の手前で止まる", func(t *testing.T) {
		t.Parallel()
		p := newParser(t, "a && b )")
		n, err := p.parseBinary(1)
		require.NoError(t, err)
		assert.Equal(t, "(a && b)", sexpr(n))
		assert.True(t, p.peek(")"))
	})
	t.Run("右のオペランドが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := newParser(t, "a &&").parseBinary(1)
		require.ErrorIs(t, err, errExpression)
	})
	t.Run("左のオペランドが読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := newParser(t, "&& a").parseBinary(1)
		require.ErrorIs(t, err, errExpression)
	})
}

func Test_exprParser_parseUnary(t *testing.T) {
	t.Parallel()

	n, err := newParser(t, "!!a.b").parseUnary()
	require.NoError(t, err)
	assert.Equal(t, "!!a.b", sexpr(n))

	t.Run("! の後ろが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := newParser(t, "!").parseUnary()
		require.ErrorIs(t, err, errExpression)
	})
}

func Test_exprParser_parsePostfix(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ src, want string }{
		"呼び出しの結果に続く区間":      {"fromJSON(a).b[0]", "fromjson(a).b[0]"},
		". の後ろの予約語は名前として読む": {"a.true.null", "a.true.null"},
		"添字の中は式":            {"a[b == 'c']", "a[(b == 'c')]"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n, err := newParser(t, tc.src).parsePostfix()
			require.NoError(t, err)
			assert.Equal(t, tc.want, sexpr(n))
		})
	}
	for name, src := range map[string]string{
		". で終われば失敗する":       "a.",
		". の後ろが名前でなければ失敗する": "a.'b'",
		"閉じない添字は失敗する":       "a[0",
		"添字の中が読めなければ失敗する":   "a[]",
		"先頭が読めなければ失敗する":     ")",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := newParser(t, src).parsePostfix()
			require.ErrorIs(t, err, errExpression)
		})
	}
}

func Test_exprParser_parsePrimary(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		src  string
		kind nodeKind
		want string
	}{
		"文字列リテラル":  {"'a'", nodeLiteral, "'a'"},
		"数":        {"1", nodeLiteral, "1"},
		"予約語":      {"null", nodeLiteral, "null"},
		"context":  {"github", nodeIdent, "github"},
		"関数の呼び出し":  {"format('{0}', a)", nodeCall, "format('{0}', a)"},
		"括弧の中の式":   {"(a == b)", nodeBinary, "(a == b)"},
		"case の引数": {"case(a, 'b', 'c')", nodeCall, "case(a, 'b', 'c')"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n, err := newParser(t, tc.src).parsePrimary()
			require.NoError(t, err)
			assert.Equal(t, tc.kind, n.kind)
			assert.Equal(t, tc.want, sexpr(n))
		})
	}
	for name, src := range map[string]string{
		"字句が無ければ失敗する":        "",
		"記号で始まれば失敗する":        "]",
		"閉じない括弧は失敗する":        "(a",
		"括弧の中が読めなければ失敗する":    "()",
		"閉じない呼び出しは失敗する":      "format('a'",
		"引数が読めなければ失敗する":      "format(,)",
		"引数の数が下限に届かなければ失敗する": "contains('a')",
		"未知の関数は失敗する":         "upper('a')",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := newParser(t, src).parsePrimary()
			require.ErrorIs(t, err, errExpression)
		})
	}
}

func Test_isRefSegment(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"ref", "base_ref", "HEAD_REF", "head_branch", "branches", "default_branch", "ref_name", "Ref-Name", "target_commitish", "base-ref", "target-branch", "release-branch", "workflow_ref", "full_ref", "targetBranch", "baseRef", "RefName", "headRef", "defaultBranch"} {
		assert.True(t, isRefSegment(name), name)
	}
	for _, name := range []string{"ref_type", "ref_protected", "branches_url", "refs", "dry-run", "name", "ref_x", "prefix", "-ref", "ref-", "refType", "branchesUrl", "dryRun", "REFNAME"} {
		assert.False(t, isRefSegment(name), name)
	}
}

func Test_isRefRoot(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"github", "GitHub", "inputs", "INPUTS"} {
		assert.True(t, isRefRoot(name), name)
	}
	for _, name := range []string{"env", "needs", "githubx", "vars", "steps"} {
		assert.False(t, isRefRoot(name), name)
	}
}

func Test_contextPath(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		src  string
		root string
		segs []string
	}{
		"ドット記法":         {"github.event.ref", "github", []string{"event", "ref"}},
		"文字列リテラルの添字は区間": {"github.event['base_ref']", "github", []string{"event", "base_ref"}},
		"数の添字と .* は空":   {"github.event.pull_requests[0].base.*.ref", "github", []string{"event", "pull_requests", "", "base", "", "ref"}},
		"根だけ":           {"inputs", "inputs", nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, segs, ok := contextPath(mustParse(t, tc.src))
			require.True(t, ok)
			assert.Equal(t, tc.root, root)
			assert.Equal(t, tc.segs, segs)
		})
	}
	for name, src := range map[string]string{
		"リテラルでない添字はパスでない": "github.event[github.x]",
		"予約語の添字はパスでない":    "github[true]",
		"呼び出しから続く値はパスでない": "fromJSON(a).b",
		"リテラルはパスでない":      "'a'",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, ok := contextPath(mustParse(t, src))
			assert.False(t, ok)
		})
	}
}

func Test_refPath(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		root       string
		segs       []string
		serialized bool
		want       bool
	}{
		"名前が ref の区間":                  {"github", []string{"ref"}, false, true},
		"途中の区間が ref ならその下も":            {"github", []string{"event", "changes", "base", "ref", "from"}, false, true},
		"inputs の kebab-case の名前":      {"inputs", []string{"base-ref"}, false, true},
		"github そのもの":                  {"github", nil, false, true},
		"inputs そのもの":                  {"inputs", nil, false, true},
		"github.event そのもの":            {"GitHub", []string{"Event"}, false, true},
		"toJSON の中の github の下":         {"github", []string{"sha"}, true, true},
		"action_ref は除く":               {"github", []string{"action_ref"}, false, false},
		"action_ref の除外は github の直下だけ": {"github", []string{"event", "inputs", "action_ref"}, false, true},
		"名前が ref でない区間":                {"github", []string{"event", "ref_type"}, false, false},
		"根が github / inputs でない":       {"steps", []string{"a", "outputs", "branch"}, false, false},
		"toJSON の中でも根が違えば ref でない":     {"matrix", nil, true, false},
		"inputs.event はそのものでない":        {"inputs", []string{"event"}, false, false},
		"base.label":                   {"github", []string{"event", "pull_request", "base", "label"}, false, true},
		"inputs の head.label":          {"inputs", []string{"head", "label"}, false, true},
		"label だけの区間は ref でない":         {"github", []string{"label"}, false, false},
		"toJSON の中の inputs の下":         {"inputs", []string{"x"}, true, true},
		"inputs の camelCase の名前":       {"inputs", []string{"targetBranch"}, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, refPath(tc.root, tc.segs, tc.serialized))
		})
	}
	t.Run("生成する項の context はすべて ref と見分ける", func(t *testing.T) {
		t.Parallel()
		for _, p := range generatedRefPaths {
			root, segs, ok := contextPath(mustParse(t, "github."+p))
			require.True(t, ok, p)
			assert.True(t, refPath(root, segs, false), p)
		}
	})
}

func Test_refDerived(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"github.event.merge_group.base_ref",
		"github.event['ref']",
		"format('{0}', github.ref_name)",
		"join(github.event.commits.*.branch, ',')",
		"fromJSON(github.event.inputs.ref)",
		"toJSON(github)",
		"toJSON(github.event.pull_request.base)",
		"toJSON(format('{0}', github.sha))",
		"toJSON(github.event.pull_request.labels)",
		"fromJSON(toJSON(github)).x",
		"case(a, github.ref, 'b')",
		"case(a, 'b', github.head_ref)",
		"github.head_ref || github.ref",
		"a && github.ref",
		"github.event",
	} {
		assert.True(t, refDerived(mustParse(t, src), false), src)
	}
	for _, src := range []string{
		"github.ref_type",
		"github.action_ref",
		"steps.a.outputs.branch",
		"github.ref == 'x'",
		"!github.ref",
		"startsWith(github.ref, 'x')",
		"case(github.ref == 'x', 'a', 'b')",
		"fromJSON(vars.x).ref",
		"'refs/heads/main'",
		"toJSON(matrix)",
		"hashFiles(github.ref)",
	} {
		assert.False(t, refDerived(mustParse(t, src), false), src)
	}
}

func Test_constString(t *testing.T) {
	t.Parallel()

	for _, src := range []string{
		"'main'",
		"format('{0}', 'main')",
		"fromJSON('[\"a\"]')",
		"join(fromJSON('[\"a\"]'), ',')",
		"x && 'a' || 'b'",
		"x && 'a'",
		"case(x, 'a', 'b')",
		"case(x, y, 'b')",
		"toJSON('a')",
	} {
		assert.True(t, constString(mustParse(t, src)), src)
	}
	for _, src := range []string{
		"''",
		"1",
		"true",
		"null",
		"vars.x",
		"format('{0}', vars.x)",
		"format(1)",
		"case(x == 'a', y, z)",
		"'a' == 'b'",
		"!'a'",
		"hashFiles('a')",
		"x && y",
		"format('')",
		"x && '' || format('')",
	} {
		assert.False(t, constString(mustParse(t, src)), src)
	}
}

func Test_caseResults(t *testing.T) {
	t.Parallel()

	got := caseResults(mustParse(t, "case(p1, v1, p2, v2, d)"))
	names := make([]string, 0, len(got))
	for _, n := range got {
		names = append(names, sexpr(n))
	}
	assert.Equal(t, []string{"v1", "v2", "d"}, names)
}

func Test_literalOnly(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]bool{
		"format('a', 1, true)":            true,
		"fromJSON(format('{0}', 'a'))":    true,
		"'a'":                             true,
		"format('a', x)":                  false,
		"x":                               false,
		"'a' && 'b'":                      false,
		"hashFiles('a')":                  false,
		"fromJSON(format('{0}', vars.a))": false,
	} {
		assert.Equal(t, want, literalOnly(mustParse(t, src)), src)
	}
}

func Test_hasStringLiteral(t *testing.T) {
	t.Parallel()

	for src, want := range map[string]bool{
		"'a'":                   true,
		"format(1, 'a')":        true,
		"fromJSON(format('a'))": true,
		"1":                     false,
		"format(1, true)":       false,
		"''":                    false,
		"format('')":            false,
	} {
		assert.Equal(t, want, hasStringLiteral(mustParse(t, src)), src)
	}
}

func Test_walkExpr(t *testing.T) {
	t.Parallel()

	t.Run("比べる項を字面で返し、辿った件数を数える", func(t *testing.T) {
		t.Parallel()
		src := "github.head_ref != 'release/x' && contains(fromJSON('[\"a\"]'), needs.a.result) && inputs.x[0] < 1"
		found, c, err := walkExpr(mustParse(t, src), src)
		require.NoError(t, err)
		assert.Equal(t, []string{"github.head_ref != 'release/x'"}, found)
		assert.Equal(t, exprCounts{roots: 2, members: 4, indexes: 1, compares: 2, predicates: 1}, c)
	})
	t.Run("入れ子の中の比較も返す", func(t *testing.T) {
		t.Parallel()
		src := "case(a, x[github.ref == 'm'], !(b && startsWith(inputs.ref, 'r')))"
		found, _, err := walkExpr(mustParse(t, src), src)
		require.NoError(t, err)
		assert.Equal(t, []string{"github.ref == 'm'", "startsWith(inputs.ref, 'r')"}, found)
	})
	for name, src := range map[string]string{
		"github の下のリテラルでない添字は失敗する": "github.event[format('{0}', 'ref')] == 'main'",
		"inputs のリテラルでない添字は失敗する":   "inputs[x]",
		"予約語の添字は失敗する":              "github[null]",
		"添字の中の添字も見る":               "matrix[github[x]]",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := walkExpr(mustParse(t, src), src)
			require.ErrorIs(t, err, errExpression)
		})
	}
	t.Run("定数の表を ref の添字で引く項を返す", func(t *testing.T) {
		t.Parallel()
		src := `fromJSON('{"main":true}')[github.ref_name] == true && case(x, fromJSON('{"a":1}'), fromJSON('{"b":1}'))[inputs.base-ref]`
		found, _, err := walkExpr(mustParse(t, src), src)
		require.NoError(t, err)
		assert.Equal(t, []string{`fromJSON('{"main":true}')[github.ref_name]`, `case(x, fromJSON('{"a":1}'), fromJSON('{"b":1}'))[inputs.base-ref]`}, found)
	})
	t.Run("定数でない表か ref でない添字の引きは返さない", func(t *testing.T) {
		t.Parallel()
		src := `fromJSON(vars.x)[github.ref] && fromJSON('{"a":1}')[matrix.k] && fromJSON('{"a":1}')['a']`
		found, _, err := walkExpr(mustParse(t, src), src)
		require.NoError(t, err)
		assert.Empty(t, found)
	})
	t.Run("github / inputs でない根のリテラルでない添字は読む", func(t *testing.T) {
		t.Parallel()
		src := "matrix[needs.a.outputs.k] == 'x'"
		found, _, err := walkExpr(mustParse(t, src), src)
		require.NoError(t, err)
		assert.Empty(t, found)
	})
}

func Test_literalCounts(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		src  string
		want exprCounts
	}{
		"文字列リテラルの中は数えない":      {"'github.ref == x[0] contains('", exprCounts{}},
		"根は前後が名前の文字でないものだけ":   {"github.x || GitHub.y || inputs || my-github || githubx || github_x || needs.github.z", exprCounts{roots: 3, members: 4}},
		"空白を挟んだ . の後ろの根は数えない": {"needs . inputs", exprCounts{members: 1}},
		"添字・比較・述語を数える":        {"a[0] == b && c <= d && STARTSWITH (e, 'f') && endsWith(g, h) && contains(i, j) && k != l", exprCounts{indexes: 1, compares: 3, predicates: 3}},
		".* は区間":              {"a.*.b", exprCounts{members: 2}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, literalCounts(tc.src))
		})
	}
	t.Run("refComparisons の表の式は、構文木を辿った件数と一致する", func(t *testing.T) {
		t.Parallel()
		for _, table := range []map[string]string{refComparisonsDetected, refComparisonsUndetected} {
			for name, text := range table {
				body, err := exprBody(text)
				require.NoError(t, err, name)
				_, read, err := walkExpr(mustParse(t, body), body)
				require.NoError(t, err, name)
				assert.Equal(t, read, literalCounts(body), name)
			}
		}
	})
}

func Test_countMismatch(t *testing.T) {
	t.Parallel()

	require.NoError(t, countMismatch(exprCounts{roots: 1, members: 2}, exprCounts{roots: 1, members: 2}))
	for name, literal := range map[string]exprCounts{
		"根":  {roots: 2, members: 2},
		"区間": {roots: 1, members: 3},
		"添字": {roots: 1, members: 2, indexes: 1},
		"比較": {roots: 1, members: 2, compares: 1},
		"述語": {roots: 1, members: 2, predicates: 1},
	} {
		t.Run(name+"の件数が食い違えば失敗する", func(t *testing.T) {
			t.Parallel()
			err := countMismatch(exprCounts{roots: 1, members: 2}, literal)
			require.ErrorIs(t, err, errExpression)
			assert.ErrorContains(t, err, "食い違います")
		})
	}
}

func Test_exprBody(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ text, want string }{
		"素の if: の値はそのまま":          {"  a == 'b' ", "a == 'b'"},
		"全体を包んだ値は中身":              {"${{ a == 'b' }}", " a == 'b' "},
		"二重引用符の値は YAML の文字列として読む": {`"${{ a == \"b\" }}"`, ` a == "b" `},
		"一重引用符の値の '' は ' を表す":     {"'a == ''b'''", "a == 'b'"},
		"文字列リテラルの中の }} で包みを切らない":  {"${{ format('}}') }}", " format('}}') "},
		"改行を含むブロックスカラーの値":         {"      a &&\n      b", "a &&\n      b"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := exprBody(tc.text)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	for name, tc := range map[string]struct{ text, msg string }{
		"包みの外に字面が続けば失敗する":        {"${{ a }} == 'x'", "包みの外の字面"},
		"包みの前に字面があれば失敗する":        {"a && ${{ b }}", "包みの外の字面"},
		"包みが2つあれば失敗する":           {"${{ a }}${{ b }}", "包みの外の字面"},
		"閉じない包みは失敗する":            {"${{ a", "閉じていません"},
		"YAML として読めない引用符の値は失敗する": {`"a" && b`, "YAML の文字列として読めません"},
		"引用符を外すと包みが混ざる値は失敗する":    {`"x ${{ a }}"`, "包みの外の字面"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := exprBody(tc.text)
			require.ErrorIs(t, err, errExpression)
			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

func Test_exprSpans(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		s    string
		want []string
	}{
		"左から順に返す":              {"a ${{ x }} b ${{ y }}", []string{"${{ x }}", "${{ y }}"}},
		"文字列リテラルの中の }} では閉じない": {"${{ '}}' }}-${{ 'a''}}' }}", []string{"${{ '}}' }}", "${{ 'a''}}' }}"}},
		"閉じない式からは読まない":         {"${{ x }} ${{ y", []string{"${{ x }}"}},
		"閉じない文字列リテラルの式は読まない":   {"${{ 'x }}", nil},
		"式が無ければ空":              {"a }} b", nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, exprSpans(tc.s))
		})
	}
}

var refComparisonsDetected = map[string]string{
	"merge_group の base_ref":    "github.event.merge_group.base_ref == 'x'",
	"format で包んだ ref":           "format('{0}', github.ref_name) == 'main'",
	"push の event.base_ref":     "github.event.base_ref == 'main'",
	"check_suite の head_branch": "github.event.check_suite.head_branch == 'main'",
	"配列の中の ref":                 "github.event.workflow_run.pull_requests[0].base.ref == 'main'",
	"途中の区間が ref":                "github.event.changes.base.ref.from == 'main'",
	"head.label":                "github.event.pull_request.head.label == 'o:main'",
	"workflow_ref":              "github.workflow_ref == 'o/r/.github/workflows/a.yaml@refs/heads/main'",
	"inputs.ref":                "inputs.ref == 'main'",
	"kebab-case の input":        "inputs.base-ref == 'main'",
	"kebab-case の payload の区間":  "github.event.client_payload.release-branch == 'main'",
	"文字列リテラルの添字":                "github.event['ref'] == 'main'",
	"右辺が定数の format":             "github.ref_name == format('{0}', 'main')",
	"大小の比較":                     "github.ref_name >= 'release/'",
	"case の述語の中":                "case(github.ref == 'x', 'a', 'b') == 'a'",
	"括弧と && の中":                 "a && (github.ref_name == 'x')",
	"toJSON の ref":              "toJSON(github.ref) == '\"x\"'",
	"|| の結果の ref":               "(github.head_ref || github.ref) == 'x'",
	"大文字の context":              "GITHUB.EVENT.MERGE_GROUP.BASE_REF == 'x'",
	"大文字の関数名":                   "FORMAT('{0}', github.ref) == 'x'",
	"三項の慣用句の定数":                 "github.ref_name == (github.event_name == 'push' && 'main' || 'dev')",
	"三項の慣用句の定数（括弧を省いた右辺）":                 "github.ref == (x && 'refs/heads/a' || 'refs/heads/b')",
	"case の結果の定数":                         "github.ref_name == case(x, 'a', 'b')",
	"toJSON(github) の中の検索":                "contains(toJSON(github), 'refs/heads/main')",
	"toJSON した祖先のパス":                      "toJSON(github.event.pull_request.base) == 'x'",
	"toJSON(github.event) の前方一致":          "startsWith(toJSON(github.event), 'x')",
	"左辺のリテラル":                             "${{ 'main' == github.ref_name }}",
	"endsWith":                            "endsWith(github.ref, '/main')",
	"contains の文字列":                       "contains('a,b', github.ref)",
	"contains の fromJSON":                 "contains(fromJSON('[\"a\"]'), github.ref)",
	"名前が branch の input の文字列 'true' との比較": "inputs.create-branch == 'true'",
	"event.inputs の文字列 'false' との比較":      "github.event.inputs.use_branch == 'false'",
	"`${{ }}` で包んだ式":                      "${{ github.event.pull_request.base.ref != 'x' }}",
	"一重引用符の if: の値":                       "'github.ref_name == ''main'''",
	"定数の表を ref で引く":                       `fromJSON('{"main":true}')[github.ref_name] == true`,
	"camelCase の input":                   "inputs.targetBranch == 'main'",
	"camelCase の input の Ref":             "inputs.baseRef != 'main'",
	"大文字で始まる camelCase の input":           "inputs.RefName == 'main'",
	"`${{ }}` の一重引用符の値":                   "'${{ github.ref_name == ''main'' }}'",
}

var refComparisonsUndetected = map[string]string{
	"ref_type":                       "github.ref_type == 'tag'",
	"action_ref":                     "github.action_ref == 'v4'",
	"context 同士":                     "github.ref_name == github.event.repository.default_branch",
	"vars との比較":                      "github.ref_name == vars.X",
	"値として使うだけ":                       "${{ github.ref }}",
	"|| の値として使うだけ":                   "github.head_ref || github.ref",
	"step の出力":                       "steps.a.outputs.branch == 'x'",
	"ref でない fromJSON の集合":           "contains(fromJSON('[\"failure\"]'), needs.a.result)",
	"名前が ref でない kebab-case の input": "inputs.dry-run == 'true'",
	"空文字列との比較":                       "github.head_ref != '' && github.base_ref == ''",
	"真偽値のリテラルとの比較":                   "inputs.create-branch == true",
	"状態関数と - を含むジョブ名":                "always() && needs.secret-scan.result == 'success'",
	"数との比較":                          "-1 < github.run_number",
	"ref でない名前の配列":                   "contains(github.event.pull_request.labels.*.name, 'bug')",
	"定数でない format":                   "github.ref_name == format('{0}', vars.v)",
	"ref でない github の値（event_name）":  "github.event_name == 'push'",
	"ref でない github の値（repository）":  "github.repository == 'o/r'",
	"ref でない github の値（actor）":       "github.actor == 'x'",
	"空の format との比較":                 "github.ref_name == format('')",
}

func Test_refComparisons(t *testing.T) {
	t.Parallel()

	for name, text := range refComparisonsDetected {
		t.Run("検出する: "+name, func(t *testing.T) {
			t.Parallel()
			got, err := refComparisons(text)
			require.NoError(t, err)
			assert.Len(t, got, 1)
		})
	}
	for name, text := range refComparisonsUndetected {
		t.Run("検出しない: "+name, func(t *testing.T) {
			t.Parallel()
			got, err := refComparisons(text)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	}
	for name, tc := range map[string]struct{ text, want string }{
		"括弧の右辺":      {"github.ref_name == (github.event_name == 'push' && 'main' || 'dev')", "github.ref_name == (github.event_name == 'push' && 'main' || 'dev')"},
		"括弧の左辺":      {"(github.head_ref || github.ref) == 'x'", "(github.head_ref || github.ref) == 'x'"},
		"&& の中の括弧の項": {"a && (github.ref_name == 'x') && b", "(github.ref_name == 'x')"},
	} {
		t.Run("項を括弧ごと返す: "+name, func(t *testing.T) {
			t.Parallel()
			got, err := refComparisons(tc.text)
			require.NoError(t, err)
			assert.Equal(t, []string{tc.want}, got)
		})
	}
	for name, text := range map[string]string{
		"空の式":               "${{ }}",
		"空の if: の値":         "",
		"閉じない文字列リテラル":       "github.ref == 'x",
		"閉じない括弧":            "(github.ref == 'x'",
		"未知の演算子":            "github.ref === 'x'",
		"リテラルでない添字":         "github.event[format('{0}', 'ref')] == 'main'",
		"inputs のリテラルでない添字": "inputs[x] == 'main'",
		"包みの外に字面が残る":        "${{ github.ref }} == 'x'",
		"YAML として読めない引用符の値": `"a" && b`,
		"未知の関数":             "lower(github.ref) == 'main'",
		"二重引用符の文字列":         `${{ github.ref == "x" }}`,
		"字句と字面で読み方が食い違う数":   "github.run_number == 1.e5",
	} {
		t.Run("読めない: "+name, func(t *testing.T) {
			t.Parallel()
			_, err := refComparisons(text)
			require.ErrorIs(t, err, errExpression)
		})
	}
}
