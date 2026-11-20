package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseGo(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	require.NoError(t, err)

	return f
}

func parseExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	e, err := parser.ParseExpr(src)
	require.NoError(t, err)

	return e
}

func Test_parseEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("partition ごとの region の集合を昇順で返す", func(t *testing.T) {
		t.Parallel()
		got, err := parseEndpoints([]byte(endpointsSrc))
		require.NoError(t, err)
		assert.Equal(t, partitionSet{"aws": {"ap-northeast-1", "us-east-1"}, "aws-cn": {"cn-north-1"}}, got)
	})

	cases := []struct {
		name string
		src  string
		want error
	}{
		{name: "Go として解析できなければ失敗する", src: "package endpoints\nvar (", want: errEndpointsSyntax},
		{name: "partitions の var が無ければ失敗する", src: strings.Replace(endpointsSrc, "partitions =", "others =", 1), want: errEndpointsShape},
		{name: "partitions が空なら成功で返さない", src: "package endpoints\n\nvar partitions = map[string]Partition{}\n", want: errNoPartitions},
		{name: "map のキーの識別子に const が無ければ失敗する", src: strings.Replace(endpointsSrc, "\tAwsCnPartitionID = \"aws-cn\"\n", "", 1), want: errEndpointsShape},
		{name: "const に無い partition が map に在れば一致しない", src: strings.Replace(endpointsSrc, "\t\tAwsCnPartitionID: {", "\t\t\"aws-x\": {regions: map[string]Region{}},\n\t\tAwsCnPartitionID: {", 1), want: errConstMismatch},
		{name: "const に無い region が map に在れば一致しない", src: strings.Replace(endpointsSrc, "\t\t\t\tCnNorth1RegionID: {id: CnNorth1RegionID},\n", "\t\t\t\tCnNorth1RegionID: {id: CnNorth1RegionID},\n\t\t\t\t\"cn-south-9\": {},\n", 1), want: errConstMismatch},
		{name: "region の件数が同じでも値が違えば一致しない", src: strings.Replace(endpointsSrc, "\t\t\t\tCnNorth1RegionID: {id: CnNorth1RegionID},\n", "\t\t\t\t\"cn-south-9\": {},\n", 1) + "\nvar _ = CnNorth1RegionID\n", want: errConstMismatch},
		{name: "ID の const が文字列のリテラルでなければ失敗する", src: strings.Replace(endpointsSrc, "AwsCnPartitionID = \"aws-cn\"", "AwsCnPartitionID = \"aws\" + \"-cn\"", 1), want: errEndpointsShape},
		{name: "map に無い partition の const が在れば一致しない", src: strings.Replace(endpointsSrc, "\tAwsCnPartitionID = \"aws-cn\"\n", "\tAwsCnPartitionID = \"aws-cn\"\n\tAwsXPartitionID = \"aws-x\"\n", 1), want: errConstMismatch},
		{name: "map に無い region の const が在れば一致しない", src: strings.Replace(endpointsSrc, "\tCnNorth1RegionID     = \"cn-north-1\"\n", "\tCnNorth1RegionID     = \"cn-north-1\"\n\tCnSouth9RegionID = \"cn-south-9\"\n", 1), want: errConstMismatch},
		{name: "partition の要素がキーと値の組でなければ失敗する", src: "package endpoints\n\nvar partitions = []Partition{{}}\n", want: errEndpointsShape},
		{name: "partition が2度あれば失敗する", src: strings.Replace(endpointsSrc, "AwsCnPartitionID: {", "AwsPartitionID: {", 1), want: errEndpointsShape},
		{name: "regions が無い partition があれば失敗する", src: strings.Replace(endpointsSrc, "regions: map[string]Region{\n\t\t\t\tCnNorth1RegionID: {id: CnNorth1RegionID},\n\t\t\t},", "", 1), want: errEndpointsShape},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseEndpoints([]byte(tc.src))
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func Test_idConsts(t *testing.T) {
	t.Parallel()

	t.Run("partition と region の ID の const だけを集める", func(t *testing.T) {
		t.Parallel()
		f := parseGo(t, "package p\n\nconst (\n\tAPartitionID = \"a\"\n\tX1RegionID = \"x-1\"\n\tOther = \"o\"\n)\n\nconst N = 3\n\nfunc f() {}\n")
		consts, partitions, regions, err := idConsts(f)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"APartitionID": "a", "X1RegionID": "x-1"}, consts)
		assert.Equal(t, []string{"a"}, partitions)
		assert.Equal(t, []string{"x-1"}, regions)
	})

	t.Run("var の ID は集めない", func(t *testing.T) {
		t.Parallel()
		f := parseGo(t, "package p\n\nconst APartitionID = \"a\"\n\nvar X1RegionID = \"x-1\"\n")
		consts, _, regions, err := idConsts(f)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"APartitionID": "a"}, consts)
		assert.Empty(t, regions)
	})

	t.Run("値の無い ID の const は失敗にする", func(t *testing.T) {
		t.Parallel()
		f := parseGo(t, "package p\n\nconst (\n\tAPartitionID = \"a\"\n\tBPartitionID\n)\n")
		_, _, _, err := idConsts(f)
		require.ErrorIs(t, err, errEndpointsShape)
	})

	t.Run("文字列のリテラルでない ID の const は失敗にする", func(t *testing.T) {
		t.Parallel()
		f := parseGo(t, "package p\n\nconst APartitionID = \"a\" + \"b\"\n")
		_, _, _, err := idConsts(f)
		require.ErrorIs(t, err, errEndpointsShape)
	})
}

func Test_partitionsLiteral(t *testing.T) {
	t.Parallel()

	t.Run("partitions の合成リテラルを返す", func(t *testing.T) {
		t.Parallel()
		f := parseGo(t, "package p\n\nvar x = 1\n\nvar partitions = map[string]P{}\n")
		lit, err := partitionsLiteral(f)
		require.NoError(t, err)
		assert.Empty(t, lit.Elts)
	})

	cases := []struct{ name, src, msg string }{
		{name: "無ければ失敗する", src: "package p\n\nvar x = 1\n", msg: "がありません"},
		{name: "const の partitions は var として読まない", src: "package p\n\nconst partitions = 1\n", msg: "がありません"},
		{name: "値が無ければ失敗する", src: "package p\n\nvar partitions map[string]P\n", msg: "1つの合成リテラルではありません"},
		{name: "合成リテラルでなければ失敗する", src: "package p\n\nvar partitions = load()\n", msg: "が合成リテラルではありません"},
		{name: "2つあれば失敗する", src: "package p\n\nvar partitions = map[string]P{}\n\nvar partitions = map[string]P{}\n", msg: "1つの合成リテラルではありません"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := partitionsLiteral(parseGo(t, tc.src))
			require.ErrorIs(t, err, errEndpointsShape)
			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

func Test_regionKeys(t *testing.T) {
	t.Parallel()

	consts := map[string]string{"A1RegionID": "a-1", "B1RegionID": "b-1"}

	t.Run("regions のキーを昇順で返し、ほかのフィールドは見ない", func(t *testing.T) {
		t.Parallel()
		got, err := regionKeys(parseExpr(t, `P{id: "x", regions: map[string]R{B1RegionID: {}, "a-1": {}}, services: map[string]S{"s3": {}}}`), consts)
		require.NoError(t, err)
		assert.Equal(t, []string{"a-1", "b-1"}, got)
	})

	cases := []struct{ name, src, msg string }{
		{name: "値が合成リテラルでなければ失敗する", src: `newP()`, msg: "partition の値が合成リテラルではありません"},
		{name: "regions が無ければ失敗する", src: `P{id: "x"}`, msg: "regions がありません"},
		{name: "regions が合成リテラルでなければ失敗する", src: `P{regions: load()}`, msg: "regions が合成リテラルではありません"},
		{name: "regions が2度あれば失敗する", src: `P{regions: map[string]R{A1RegionID: {}}, regions: map[string]R{B1RegionID: {}}}`, msg: "regions が2度あります"},
		{name: "regions の要素がキーと値の組でなければ失敗する", src: `P{regions: []R{{}}}`, msg: "キーと値の組ではありません"},
		{name: "region が2度あれば失敗する", src: `P{regions: map[string]R{A1RegionID: {}, "a-1": {}}}`, msg: "が2度あります"},
		{name: "知らない const の region は失敗にする", src: `P{regions: map[string]R{C1RegionID: {}}}`, msg: "ID の const C1RegionID がありません"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := regionKeys(parseExpr(t, tc.src), consts)
			require.ErrorIs(t, err, errEndpointsShape)
			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

func Test_resolveID(t *testing.T) {
	t.Parallel()

	consts := map[string]string{"AwsPartitionID": "aws"}

	t.Run("識別子は const の値へ解決する", func(t *testing.T) {
		t.Parallel()
		got, err := resolveID(parseExpr(t, "AwsPartitionID"), consts)
		require.NoError(t, err)
		assert.Equal(t, "aws", got)
	})

	t.Run("文字列のリテラルはそのまま返す", func(t *testing.T) {
		t.Parallel()
		got, err := resolveID(parseExpr(t, `"aws-cn"`), consts)
		require.NoError(t, err)
		assert.Equal(t, "aws-cn", got)
	})

	t.Run("知らない識別子は失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := resolveID(parseExpr(t, "Other"), consts)
		require.ErrorIs(t, err, errEndpointsShape)
	})
}

func Test_stringLiteral(t *testing.T) {
	t.Parallel()

	t.Run("引用符を外した値を返す", func(t *testing.T) {
		t.Parallel()
		got, err := stringLiteral(parseExpr(t, "`us-east-1`"))
		require.NoError(t, err)
		assert.Equal(t, "us-east-1", got)
	})

	t.Run("文字列でないリテラルは失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := stringLiteral(parseExpr(t, "1"))
		require.ErrorIs(t, err, errEndpointsShape)
	})

	t.Run("引用符を外せなければ失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := stringLiteral(&ast.BasicLit{Kind: token.STRING, Value: `"broken`})
		require.ErrorIs(t, err, errEndpointsShape)
	})
}
