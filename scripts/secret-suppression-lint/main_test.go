package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const validGitleaksConfig = `[extend]
useDefault = true

# 理由。撤回: 条件
[allowlist]
description = "理由"
condition = "AND"
paths = ['''^a/b\.go$''']
regexTarget = "match"
regexes = ['k-\d+']
`

const validTrufflehog = `# 説明

# 理由。撤回: 条件
^a/b\.go$
`

const validTrivy = "vulnerabilities: []\n" +
	"secrets: [{id: aws-access-key-id, paths: [a/b.go], statement: '理由。撤回: 条件'}]\n"

var tracked = map[string]bool{"a/b.go": true}

const sha = "0123456789abcdef0123456789abcdef01234567"

func writeRepo(t *testing.T, override map[string]string) (string, []string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		gitleaksIgnore:  "# 説明\n",
		gitleaksConfig:  validGitleaksConfig,
		trufflehogPaths: validTrufflehog,
		trufflehogHist:  "# 理由。撤回: 条件\n^gone\\.go$\n",
		trivyIgnore:     validTrivy,
	}
	for name, body := range override {
		files[name] = body
	}
	for name, body := range files {
		if body == "" {
			continue
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))
	}
	lists := t.TempDir()
	list := filepath.Join(lists, "tracked.txt")
	require.NoError(t, os.WriteFile(list, []byte("a/b.go\n"), 0o600))
	commitList := filepath.Join(lists, "commits.txt")
	require.NoError(t, os.WriteFile(commitList, []byte(sha+"\n"), 0o600))
	return root, []string{"-root", root, "-tracked", list, "-commits", commitList}
}

func lines(findings []lintreport.Finding) []int {
	var out []int
	for _, f := range findings {
		out = append(out, f.Line)
	}
	return out
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("すべての抑止が理由つきの path・fingerprint 単位なら通り、件数を出す", func(t *testing.T) {
		t.Parallel()
		_, args := writeRepo(t, nil)
		var out bytes.Buffer
		require.NoError(t, run(args, &out))
		assert.Contains(t, out.String(), "5 ファイル / 抑止 4 件")
	})

	t.Run("違反があれば一覧を出して失敗する", func(t *testing.T) {
		t.Parallel()
		_, args := writeRepo(t, map[string]string{trufflehogPaths: "^a/.*$\n"})
		var out bytes.Buffer
		err := run(args, &out)
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out.String(), trufflehogPaths)
	})

	t.Run("履歴に在る commit の fingerprint は通る", func(t *testing.T) {
		t.Parallel()
		_, args := writeRepo(t, map[string]string{gitleaksIgnore: "# 理由。撤回: 条件\n" + sha + ":a/b.go:aws-access-token:1\n"})
		var out bytes.Buffer
		require.NoError(t, run(args, &out))
		assert.Contains(t, out.String(), "抑止 5 件")
	})

	// commit の一覧が .gitleaksignore の検査へ渡っていなければ、ここが通ってしまう。
	t.Run("履歴に無い commit の fingerprint で失敗する", func(t *testing.T) {
		t.Parallel()
		_, args := writeRepo(t, map[string]string{gitleaksIgnore: "# 理由。撤回: 条件\n" + sha + ":a/b.go:aws-access-token:1\n"})
		require.NoError(t, os.WriteFile(args[5], []byte("fedcba9876543210fedcba9876543210fedcba98\n"), 0o600))
		var out bytes.Buffer
		require.ErrorIs(t, run(args, &out), errViolations)
		assert.Contains(t, out.String(), "履歴に無い")
	})

	t.Run("追跡ファイルの一覧が空なら番兵で返す", func(t *testing.T) {
		t.Parallel()
		_, args := writeRepo(t, nil)
		require.NoError(t, os.WriteFile(args[3], []byte("\n"), 0o600))
		err := run(args, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoTracked)
		assert.NotErrorIs(t, err, errNoCommits)
	})

	t.Run("一覧の指定を省けば読めない番兵で返す", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}), errUnreadable)
	})

	t.Run("知らない引数は違反ではなく解釈のエラーで返す", func(t *testing.T) {
		t.Parallel()
		err := run([]string{"-bogus"}, &bytes.Buffer{})
		require.Error(t, err)
		assert.NotErrorIs(t, err, errViolations)
	})

	t.Run("抑止のファイルが無ければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		root, args := writeRepo(t, nil)
		require.NoError(t, os.Remove(filepath.Join(root, trivyIgnore)))
		err := run(args, &bytes.Buffer{})
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("追跡ファイルの一覧を読めなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		root, args := writeRepo(t, nil)
		args[3] = filepath.Join(root, "missing")
		err := run(args, &bytes.Buffer{})
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("commit の一覧が空なら番兵で返す", func(t *testing.T) {
		t.Parallel()
		_, args := writeRepo(t, nil)
		require.NoError(t, os.WriteFile(args[5], []byte("\n"), 0o600))
		err := run(args, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoCommits)
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want int
	}{
		"エラーが無い":        {want: 0},
		"検査対象を読めない":     {err: xerrors.Wrapf(errUnreadable, "x"), want: 2},
		"追跡ファイルの一覧が0件":  {err: xerrors.Wrapf(errNoTracked, "x"), want: 2},
		"commit の一覧が0件": {err: xerrors.Wrapf(errNoCommits, "x"), want: 2},
		"違反がある":         {err: xerrors.Wrapf(errViolations, "1 件"), want: 1},
		"引数の解釈の失敗":      {err: xerrors.New("x"), want: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}

func Test_readSet(t *testing.T) {
	t.Parallel()

	t.Run("空行を捨てて集合にする", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "tracked.txt")
		require.NoError(t, os.WriteFile(path, []byte("a\n\nb\n"), 0o600))
		got, err := readSet(path, errNoTracked)
		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"a": true, "b": true}, got)
	})

	t.Run("空の一覧は番兵で返す", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "tracked.txt")
		require.NoError(t, os.WriteFile(path, []byte("\n"), 0o600))
		_, err := readSet(path, errNoCommits)
		require.ErrorIs(t, err, errNoCommits)
	})
}

func Test_reasonAbove(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want bool
	}{
		"直前のコメントに撤回がある": {body: "# 理由\n# 撤回: 条件\nx", want: true},
		"撤回の後に空行がある":    {body: "# 撤回: 条件\n\nx"},
		"直前がエントリである":    {body: "# 撤回: 条件\ny\nx"},
		"コメントはあるが撤回が無い": {body: "# 理由\nx"},
		"先頭の行": {body: "x"},
		"字下げしたコメントも同じ塊として読む": {body: "  # 撤回: 条件\nx", want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ls := strings.Split(tt.body, "\n")
			assert.Equal(t, tt.want, reasonAbove(ls, len(ls)-1))
		})
	}
}

func Test_isSkippable(t *testing.T) {
	t.Parallel()

	for line, want := range map[string]bool{"": true, "  ": true, "# x": true, "  # x": true, "x": false} {
		t.Run("「"+line+"」", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isSkippable(line))
		})
	}
}

func Test_lineEntry(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		line  string
		entry bool
		bad   bool
	}{
		"空行":           {line: ""},
		"行頭のコメント":      {line: "# x"},
		"エントリ":         {line: "^a$", entry: true},
		"字下げしたコメント":    {line: "  # x", entry: true, bad: true},
		"空白だけの行":       {line: "  ", entry: true, bad: true},
		"行末に空白があるエントリ": {line: "^a$ ", entry: true, bad: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			entry, msg := lineEntry(tt.line)
			assert.Equal(t, tt.entry, entry)
			assert.Equal(t, tt.bad, msg != "")
		})
	}
}

func Test_checkGitleaksIgnore(t *testing.T) {
	t.Parallel()

	commits := map[string]bool{sha: true}
	const gone = "fedcba9876543210fedcba9876543210fedcba98"
	tests := map[string]struct {
		body  string
		lines []int
		n     int
	}{
		"エントリが無い":                {body: "# 説明\n"},
		"commit つきの fingerprint": {body: "# 撤回: x\n" + sha + ":a/b.go:aws-access-token:3\n", n: 1},
		"大文字と下線を含む rule id":      {body: "# 撤回: x\n" + sha + ":dir with space/b.go:My_Rule:3\n", n: 1},
		"commit の無い fingerprint": {body: "# 撤回: x\na/b.go:generic-api-key:12\n", lines: []int{2}, n: 1},
		"履歴に無い commit":           {body: "# 撤回: x\n" + gone + ":a/b.go:aws-access-token:3\n", lines: []int{2}, n: 1},
		"fingerprint の形ではない":     {body: "# 撤回: x\na/b.go\n", lines: []int{2}, n: 1},
		"理由が無い":                  {body: sha + ":a/b.go:generic-api-key:12\n", lines: []int{1}, n: 1},
		"字下げしたエントリ":              {body: "# 撤回: x\n " + sha + ":a/b.go:generic-api-key:12\n", lines: []int{2, 2}, n: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, n := checkGitleaksIgnore("f", tt.body, commits)
			assert.Equal(t, tt.lines, lines(f))
			assert.Equal(t, tt.n, n)
		})
	}
}

func Test_checkPathRegexes(t *testing.T) {
	t.Parallel()

	t.Run("理由つきの実在する path は通る", func(t *testing.T) {
		t.Parallel()
		f, n := checkPathRegexes("f", validTrufflehog, tracked)
		assert.Empty(t, f)
		assert.Equal(t, 1, n)
	})

	t.Run("理由の無い行を報告する", func(t *testing.T) {
		t.Parallel()
		f, n := checkPathRegexes("f", "# 撤回: x\n^a/b\\.go$\n^a/b\\.go$\n", tracked)
		assert.Equal(t, []int{3}, lines(f))
		assert.Equal(t, 2, n)
	})

	t.Run("tracked を渡さなければ実在を見ない", func(t *testing.T) {
		t.Parallel()
		f, _ := checkPathRegexes("f", "# 撤回: x\n^gone\\.go$\n", nil)
		assert.Empty(t, f)
	})

	t.Run("字下げしたコメントをエントリとして報告する", func(t *testing.T) {
		t.Parallel()
		f, n := checkPathRegexes("f", "# 撤回: x\n^a/b\\.go$\n  # x\n", tracked)
		assert.NotEmpty(t, f)
		assert.Equal(t, 2, n)
	})
}

func Test_checkPathRegex(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		expr    string
		tracked map[string]bool
		bad     bool
	}{
		"実在する path":           {expr: `^a/b\.go$`, tracked: tracked},
		"ディレクトリへ広がる":          {expr: `^a/.*$`, tracked: tracked, bad: true},
		"錨が無い":                {expr: `a/b\.go`, tracked: tracked, bad: true},
		"追跡ファイルに無い":           {expr: `^a/c\.go$`, tracked: tracked, bad: true},
		"正規表現として解釈できない":       {expr: `^a/(b$`, tracked: tracked, bad: true},
		"実在を見ないなら無い path も通る": {expr: `^a/c\.go$`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := checkPathRegex("f", 0, tt.expr, tt.tracked)
			assert.Equal(t, tt.bad, len(f) > 0, f)
		})
	}
}

func Test_literalPath(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		expr string
		want string
		ok   bool
	}{
		"エスケープしたドット":      {expr: `^\.github/actions-pin\.toml$`, want: ".github/actions-pin.toml", ok: true},
		"エスケープしたハイフン":     {expr: `^a\-b$`, want: "a-b", ok: true},
		"エスケープしないドット":     {expr: `^a.go$`},
		"量指定子":            {expr: `^a/.+$`},
		"選択":              {expr: `^(a|b)$`},
		"終わりの錨が無い":        {expr: `^a`},
		"始まりの錨が無い":        {expr: `a$`},
		"空":               {expr: `^$`},
		"文字クラスのエスケープ":     {expr: `^a\d$`},
		"末尾の孤立したバックスラッシュ": {expr: `^a\$`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := literalPath(tt.expr)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_finding(t *testing.T) {
	t.Parallel()

	t.Run("0始まりの行を1始まりにする", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, lintreport.Finding{File: "f", Line: 3, Message: "m"}, finding("f", 2, "m"))
	})
}

func Test_checkGitleaksConfig(t *testing.T) {
	t.Parallel()

	replace := func(old, repl string) string { return strings.Replace(validGitleaksConfig, old, repl, 1) }
	tests := map[string]struct {
		body  string
		lines []int
		n     int
	}{
		"path と値の形を AND で置く":  {body: validGitleaksConfig, n: 1},
		"[[allowlists]] でも同じ": {body: replace("[allowlist]", "[[allowlists]]"), n: 1},
		"regexes の無い path だけの抑止": {
			body: strings.Replace(replace("condition = \"AND\"\n", ""), "regexes = ['k-\\d+']\n", "", 1),
			n:    1,
		},
		"regexes があるのに AND でない": {body: replace("condition = \"AND\"\n", ""), lines: []int{5}, n: 1},
		"paths が無い":             {body: replace("paths = ['''^a/b\\.go$''']\n", ""), lines: []int{5}, n: 1},
		"paths がディレクトリへ広がる":     {body: replace(`^a/b\.go$`, `^a/.*$`), lines: []int{5}, n: 1},
		"description が無い":       {body: replace("description = \"理由\"\n", ""), lines: []int{5}, n: 1},
		"理由の無い allowlist":       {body: replace("# 理由。撤回: 条件\n", ""), lines: []int{4}, n: 1},
		"既定の規則を外す":              {body: replace("useDefault = true", "useDefault = false"), lines: []int{2}, n: 1},
		"規則を無効にする":              {body: replace("useDefault = true", "useDefault = true\ndisabledRules = ['generic-api-key']"), lines: []int{3}, n: 1},
		"規則を足す":                 {body: validGitleaksConfig + "[[rules]]\nid = \"x\"\n", lines: []int{11, 12}, n: 1},
		"知らないキー":                {body: replace("regexTarget = \"match\"", "commits = ['abc']"), lines: []int{9}, n: 1},
		"複数行の配列":                {body: replace("regexes = ['k-\\d+']", "regexes = [\n  'k-\\d+',\n]"), lines: []int{10, 11, 12}, n: 1},
		"基本文字列の配列":              {body: replace(`['''^a/b\.go$''']`, `["^a/b\\.go$"]`), lines: []int{8, 5}, n: 1},
		"regexTarget の値の誤り":     {body: replace(`regexTarget = "match"`, `regexTarget = "value"`), lines: []int{9}, n: 1},
		"paths が追跡ファイルに無い":      {body: replace(`^a/b\.go$`, `^a/zz\.go$`), lines: []int{5}, n: 1},
		"paths が空":              {body: replace(`['''^a/b\.go$''']`, `[]`), lines: []int{5, 8}, n: 1},
		"2つ目の allowlist だけの違反": {
			body:  validGitleaksConfig + "\n# 理由。撤回: 条件\n[[allowlists]]\ndescription = \"\"\npaths = ['^a/b\\.go$']\n",
			lines: []int{13},
			n:     2,
		},
		"テーブルの外のキー": {body: "title = \"x\"\n", lines: []int{1}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, n := checkGitleaksConfig("f", tt.body, tracked)
			assert.ElementsMatch(t, tt.lines, lines(f), f)
			assert.Equal(t, tt.n, n)
		})
	}
}

func Test_setAllowlistKey(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		key, value string
		bad        bool
		check      func(t *testing.T, l allowlist)
	}{
		"description": {key: "description", value: `"理由"`, check: func(t *testing.T, l allowlist) {
			t.Helper()
			assert.Equal(t, "理由", l.description)
		}},
		"condition をリテラル文字列で": {key: "condition", value: `'AND'`, check: func(t *testing.T, l allowlist) {
			t.Helper()
			assert.Equal(t, "AND", l.condition)
		}},
		"文字列でない値":         {key: "condition", value: `AND`, bad: true},
		"regexTarget の誤り": {key: "regexTarget", value: `"x"`, bad: true},
		"paths": {key: "paths", value: `['''^a$''', '^b$']`, check: func(t *testing.T, l allowlist) {
			t.Helper()
			assert.Equal(t, []string{"^a$", "^b$"}, l.paths)
			assert.True(t, l.hasPaths)
		}},
		"空の regexes":     {key: "regexes", value: `[]`, bad: true},
		"解釈できない regexes": {key: "regexes", value: `['(']`, bad: true},
		"配列でない paths":    {key: "paths", value: `'^a$'`, bad: true},
		"知らないキー":         {key: "stopwords", value: `['x']`, bad: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var l allowlist
			var msgs []string
			setAllowlistKey(&l, tt.key, tt.value, func(m string) { msgs = append(msgs, m) })
			assert.Equal(t, tt.bad, len(msgs) > 0, msgs)
			if tt.check != nil {
				tt.check(t, l)
			}
		})
	}
}

func Test_literalArray(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value string
		want  []string
		ok    bool
	}{
		"三重引用と単引用": {value: `['''a''', 'b']`, want: []string{"a", "b"}, ok: true},
		"空の配列":     {value: `[]`, want: []string{}, ok: true},
		"末尾のカンマ":   {value: `['a',]`, want: []string{"a"}, ok: true},
		"カンマの無い要素": {value: `['a' 'b']`},
		"連続したカンマ":  {value: `['a',,'b']`},
		"基本文字列":    {value: `["a"]`},
		"閉じない配列":   {value: `['a'`},
		"引用の無い要素":  {value: `[a]`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := literalArray(tt.value)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_checkTrivySecrets(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body  string
		lines []int
		n     int
	}{
		"理由つきの実在する path": {body: validTrivy, n: 1},
		"空ファイル":          {body: "", lines: []int{1}},
		"コメントだけ":         {body: "# x\n", lines: []int{1}},
		"YAML の文書が2つ":    {body: validTrivy + "---\nsecrets: []\n", lines: []int{3}},
		"secrets が無い":    {body: "vulnerabilities: []\n"},
		"綴りを誤った節":        {body: "secret:\n  - id: x\n", lines: []int{1}},
		"secrets が列ではない": {body: "secrets: x\n", lines: []int{1}},
		"YAML として壊れている":  {body: "secrets: [\n", lines: []int{1}},
		"トップレベルが列":       {body: "- a\n", lines: []int{1}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, n := checkTrivySecrets("f", tt.body, tracked)
			assert.Equal(t, tt.lines, lines(f), f)
			assert.Equal(t, tt.n, n)
		})
	}
}

func Test_checkTrivySecret(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		bad  int
	}{
		"揃っている":          {body: "id: x\npaths: [a/b.go]\nstatement: '理由。撤回: 条件'\n"},
		"期限つき":           {body: "id: x\npaths: [a/b.go]\nstatement: '撤回: 条件'\nexpired_at: 2027-01-01\n"},
		"id が無い":         {body: "paths: [a/b.go]\nstatement: '撤回: 条件'\n", bad: 1},
		"paths が無い":      {body: "id: x\nstatement: '撤回: 条件'\n", bad: 1},
		"glob の path":    {body: "id: x\npaths: ['a/*']\nstatement: '撤回: 条件'\n", bad: 1},
		"追跡ファイルに無い path": {body: "id: x\npaths: [a/c.go]\nstatement: '撤回: 条件'\n", bad: 1},
		"撤回条件が無い":        {body: "id: x\npaths: [a/b.go]\nstatement: 理由\n", bad: 1},
		"知らないキー":         {body: "id: x\npaths: [a/b.go]\nstatement: '撤回: 条件'\nrule: y\n", bad: 1},
		"マッピングでない":       {body: "- x\n", bad: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var doc yaml.Node
			require.NoError(t, yaml.Unmarshal([]byte(tt.body), &doc))
			f := checkTrivySecret("f", *doc.Content[0], tracked)
			assert.Len(t, f, tt.bad, f)
		})
	}
}
