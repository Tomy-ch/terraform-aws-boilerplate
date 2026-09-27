package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func writeWorkflows(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	return dir
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("問題が無ければ通る", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    steps:\n      - run: echo hi\n"
		var out bytes.Buffer
		require.NoError(t, run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": src})}, &out))
		assert.Contains(t, out.String(), "workflow 1 件")
	})

	t.Run("固定長のフェンスを出力していれば落ちる", func(t *testing.T) {
		t.Parallel()
		// 本文に3連バッククォートが含まれた時点でブロックが閉じ、以降が bot 名義の生 Markdown になる。
		src := "jobs:\n  a:\n    steps:\n      - run: |\n          echo '```'\n"
		var out bytes.Buffer
		err := run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": src})}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "固定長のフェンス")
	})

	t.Run("変数でフェンスを組む行は対象外", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    steps:\n      - run: |\n          echo \"${fence}\"\n"
		var out bytes.Buffer
		require.NoError(t, run([]string{"-workflows", writeWorkflows(t, map[string]string{"a.yaml": src})}, &out))
	})

	t.Run("workflow が0件なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := run([]string{"-workflows", t.TempDir()}, &out)
		require.Error(t, err)
		assert.True(t, xerrors.Is(err, errNothingChecked))
	})
}

func Test_fencesBody(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want bool
	}{
		"値がある":      {line: "          details-summary: 'log'", want: true},
		"キーが無い":     {line: "          title: x", want: false},
		"空文字":       {line: "          details-summary: ''", want: false},
		"空のダブルクォート": {line: `          details-summary: ""`, want: false},
		"値なし":       {line: "          details-summary:", want: false},
		"式は静的に空か判定できないので倒す": {line: "          details-summary: ${{ steps.x.outputs.s }}", want: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, fencesBody(tt.line))
		})
	}
}

func Test_findInterpolatedSpans(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want int
	}{
		"シェル変数展開":      {line: "echo \"`${path}`\"", want: 1},
		"printf の変換指定": {line: "printf '`%s`' \"$x\"", want: 1},
		"補間の無い span":   {line: "echo '`literal`'", want: 0},
		"コメント行は見ない":    {line: "# `${x}`", want: 0},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Len(t, findInterpolatedSpans([]string{tt.line}), tt.want)
		})
	}
}

func Test_isFenced(t *testing.T) {
	t.Parallel()

	t.Run("同じステップ内の details-summary を見る", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split("      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          details-summary: 'log'\n", "\n")
		assert.True(t, isFenced(lines, 0))
	})

	t.Run("次のステップへ跨がない", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split("      - uses: ./.github/actions/upsert-pr-comment\n      - uses: other\n        with:\n          details-summary: 'log'\n", "\n")
		assert.False(t, isFenced(lines, 0))
	})
}

func Test_findFixedFences(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line string
		want int
	}{
		"3連バッククォート":        {line: "echo '```'", want: 1},
		"4連以上も当たる":         {line: "echo '````'", want: 1},
		"言語タグ付き":           {line: "echo '```json'", want: 1},
		"二重引用符":            {line: `echo "` + "```" + `"`, want: 1},
		"変数でフェンスを組む行は対象外":  {line: `echo "${fence}"`, want: 0},
		"echo 以外":          {line: "printf '```'", want: 0},
		"行の途中のバッククォートは対象外": {line: "echo '```' >> out", want: 0},
		"バッククォートを含まない":     {line: "echo hello", want: 0},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Len(t, findFixedFences([]string{tt.line}), tt.want)
		})
	}
}

func Test_hasPassThroughCall(t *testing.T) {
	t.Parallel()

	t.Run("details-summary があればフェンスされている", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split("      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          details-summary: 'log'\n", "\n")
		assert.False(t, hasPassThroughCall(lines))
	})

	t.Run("details-summary が無ければ素通し", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split("      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          body-file: /tmp/x\n", "\n")
		assert.True(t, hasPassThroughCall(lines))
	})

	t.Run("呼び出しが無ければ false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, hasPassThroughCall([]string{"      - run: echo x"}))
	})

	t.Run("素通しとフェンス済みが混在すれば素通しとして扱う", func(t *testing.T) {
		t.Parallel()
		// 1つでも素通しが在れば、補間された span が生 Markdown になる経路が開く。
		src := "      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          details-summary: 'log'\n" +
			"      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          body-file: /tmp/x\n"
		assert.True(t, hasPassThroughCall(strings.Split(src, "\n")))
	})
}

func Test_scanWorkflow(t *testing.T) {
	t.Parallel()

	t.Run("素通しの呼び出しが無ければ span を見ない", func(t *testing.T) {
		t.Parallel()
		// フェンスされていれば、span に補間があってもブロックの外には出ない。
		src := "      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          details-summary: 'log'\n        run: echo \"`${x}`\"\n"
		assert.Empty(t, scanWorkflow("a.yaml", strings.Split(src, "\n")))
	})

	t.Run("素通しの呼び出しがあれば span を違反にする", func(t *testing.T) {
		t.Parallel()
		src := "      - uses: ./.github/actions/upsert-pr-comment\n        with:\n          body-file: /tmp/x\n      - run: echo \"`${y}`\"\n"
		got := scanWorkflow("a.yaml", strings.Split(src, "\n"))
		require.NotEmpty(t, got)
		assert.Contains(t, got[0].Message, "inline code span")
		assert.Equal(t, "a.yaml", got[0].File)
	})

	t.Run("固定長フェンスは素通しの有無に関わらず違反", func(t *testing.T) {
		t.Parallel()
		got := scanWorkflow("a.yaml", []string{"echo '```'"})
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "固定長のフェンス")
		assert.Equal(t, 1, got[0].Line)
	})
}

func Test_stepIndentOf(t *testing.T) {
	t.Parallel()

	lines := []string{"    steps:", "      - uses: x", "        with:", "          k: v"}
	indent, ok := stepIndentOf(lines, 3)
	require.True(t, ok)
	assert.Equal(t, 6, indent)

	// 直前に "-" が無ければ found が false。0 へ倒すと、後続の行がすべて「より深い桁」となり、
	// 別のステップの details-summary を自分のものと見なす。
	_, ok = stepIndentOf([]string{"    steps:"}, 0)
	assert.False(t, ok)
}

func Test_findInterpolatedSpans_spanの境界(t *testing.T) {
	t.Parallel()

	t.Run("改行を跨いだ2つのバッククォートを1つの span と見なさない", func(t *testing.T) {
		t.Parallel()
		// 跨いで見ると、別々の行に在る無関係なバッククォートが span に化ける。
		assert.Empty(t, findInterpolatedSpans([]string{"echo '`'", "echo '${x}`'"}))
	})

	t.Run("補間を含まない span を違反にしない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, findInterpolatedSpans([]string{"echo '`literal`'"}))
	})

	t.Run("規約そのものを説明するコメント行に反応しない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, findInterpolatedSpans([]string{"# `${x}` のような補間は禁止"}))
	})
}

func Test_isFenced_ステップの境界(t *testing.T) {
	t.Parallel()

	t.Run("空行を挟んでも同じステップの details-summary を見つける", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split("      - uses: ./.github/actions/upsert-pr-comment\n\n        with:\n          details-summary: 'log'\n", "\n")
		assert.True(t, isFenced(lines, 0))
	})

	t.Run("- uses: から書き始めたステップでも次のステップで区切る", func(t *testing.T) {
		t.Parallel()
		lines := strings.Split("      - uses: ./.github/actions/upsert-pr-comment\n      - uses: other\n        with:\n          details-summary: 'log'\n", "\n")
		assert.False(t, isFenced(lines, 0))
	})

	t.Run("ステップを開く - が上に無い呼び出しでも、後続の details-summary を自分のものにしない", func(t *testing.T) {
		t.Parallel()
		// 桁が0へ倒れる仕組みは Test_stepIndentOf を参照。
		lines := strings.Split("        uses: ./.github/actions/upsert-pr-comment\n      - uses: other\n        with:\n          details-summary: 'log'\n", "\n")
		assert.False(t, isFenced(lines, 0))
	})
}

func Test_findFixedFences_フェンスを出さない行と行番号(t *testing.T) {
	t.Parallel()

	t.Run("フェンスを出さない echo 行を違反にしない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, findFixedFences([]string{"echo 'ふつうの文字列'"}))
	})

	t.Run("行番号は1始まりで返す", func(t *testing.T) {
		t.Parallel()
		got := findFixedFences([]string{"echo ok", "echo '```'"})
		require.Len(t, got, 1)
		assert.Equal(t, 2, got[0].Line)
	})
}

func Test_indentOf(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line string
			want int
		}{
			"字下げの無い行":         {line: "jobs:", want: 0},
			"半角空白の字下げ":        {line: "      - uses: x", want: 6},
			"タブは1文字として数える":    {line: "\tuses: x", want: 1},
			"空白とタブの混在":        {line: " \t uses: x", want: 3},
			"空行":              {line: "", want: 0},
			"空白だけの行は行の長さを返す":  {line: "    ", want: 4},
			"行の途中の空白は数えない":    {line: "  a b", want: 2},
			"字下げに使わない空白は数えない": {line: "　uses: x", want: 0},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, indentOf(tt.line))
			})
		}
	})
}
