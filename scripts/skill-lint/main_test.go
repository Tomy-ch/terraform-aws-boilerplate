package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// fixture は、検査対象になる最小のリポジトリを組み立てます。files は root からの相対パスです。
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	return root
}

// soundRepo は、違反を1件も持たない最小のリポジトリです。
func soundRepo() map[string]string {
	return map[string]string{
		"makefile":                       "include .makefiles/lint.mk\n\n.PHONY: help\nhelp:\n\t@echo\n",
		".makefiles/lint.mk":             ".PHONY: md-lint ## Markdown を検査\nmd-lint:\n\t@echo\n",
		".claude/skills/commit/SKILL.md": "`make md-lint` と `make help` と `/commit` と `.makefiles/lint.mk`\n",
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("違反が無ければ件数を添えて成功する", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.NoError(t, run([]string{"-root", fixture(t, soundRepo())}, &out))
			assert.Contains(t, out.String(), "✅ skill-lint")
			assert.Contains(t, out.String(), "参照 4 件")
		})

		t.Run("フェンスの中は検査しない", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[".claude/skills/commit/SKILL.md"] = "```sh\n`make no-such`\n```\n`make md-lint`\n"
			var out bytes.Buffer
			assert.NoError(t, run([]string{"-root", fixture(t, files)}, &out))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("存在しない make ターゲットを違反として返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[".claude/skills/commit/SKILL.md"] = "`make no-such`\n"
			var out bytes.Buffer
			require.Error(t, run([]string{"-root", fixture(t, files)}, &out))
			assert.Contains(t, out.String(), "存在しない make ターゲット")
			assert.Contains(t, out.String(), "no-such")
		})
		t.Run("存在しないスキルを違反として返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[".claude/skills/commit/SKILL.md"] = "`/no-such-skill`\n"
			var out bytes.Buffer
			require.Error(t, run([]string{"-root", fixture(t, files)}, &out))
			assert.Contains(t, out.String(), "存在しないスキル")
		})

		t.Run("存在しないパスを違反として返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[".claude/skills/commit/SKILL.md"] = "`.makefiles/no-such.mk`\n"
			var out bytes.Buffer
			require.Error(t, run([]string{"-root", fixture(t, files)}, &out))
			assert.Contains(t, out.String(), "存在しないパス")
		})

		t.Run("スキルの文書が1件も無ければ合格ではなく番兵で返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			delete(files, ".claude/skills/commit/SKILL.md")
			files[".claude/skills/commit/.keep"] = ""
			var out bytes.Buffer
			err := run([]string{"-root", fixture(t, files)}, &out)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoSkillDoc))
		})

		t.Run("参照を1件も読めなければ合格ではなく番兵で返す", func(t *testing.T) {
			t.Parallel()
			// 文書は在るのに参照が取れないのは、抽出が壊れた形である。
			files := soundRepo()
			files[".claude/skills/commit/SKILL.md"] = "名指しを1つも持たない散文\n"
			var out bytes.Buffer
			err := run([]string{"-root", fixture(t, files)}, &out)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoReference))
		})

		t.Run("make のターゲットを1件も読めなければ合格ではなく番兵で返す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files["makefile"] = "# ターゲットの宣言が無い\n"
			files[".makefiles/lint.mk"] = ""
			var out bytes.Buffer
			err := run([]string{"-root", fixture(t, files)}, &out)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, errNoMakeTarget))
		})

		t.Run("makefile が読めなければエラーを返す", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.Error(t, run([]string{"-root", t.TempDir()}, &out))
		})

		t.Run("解釈できない引数はエラーを返す", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.Error(t, run([]string{"-no-such-flag"}, &out))
		})
	})
}

func Test_checkDoc(t *testing.T) {
	t.Parallel()

	root := fixture(t, soundRepo())
	e := env{
		root:    root,
		roots:   map[string]bool{".makefiles": true, ".claude": true},
		targets: targetSet{exact: map[string]bool{"md-lint": true}},
		skills:  map[string]bool{"commit": true},
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			body     string
			wantRefs int
		}{
			"実在する参照は違反を生まず件数に入る": {body: "`make md-lint` `/commit`", wantRefs: 2},
			"参照が無ければ件数は0":        {body: "ただの散文", wantRefs: 0},
			"フェンスの中は件数にも入らない":    {body: "```\n`make no-such`\n```", wantRefs: 0},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings, refs := checkDoc(".claude/skills/commit/SKILL.md", []byte(tt.body), e)
				assert.Empty(t, findings)
				assert.Equal(t, tt.wantRefs, refs)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			body        string
			wantLine    int
			wantMessage string
		}{
			"存在しない make ターゲット": {body: "一\n`make no-such`", wantLine: 2, wantMessage: "make ターゲット"},
			"存在しないスキル":         {body: "`/no-such`", wantLine: 1, wantMessage: "スキル"},
			"存在しないパス":          {body: "`.makefiles/no-such.mk`", wantLine: 1, wantMessage: "パス"},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				findings, refs := checkDoc(".claude/skills/commit/SKILL.md", []byte(tt.body), e)
				require.Len(t, findings, 1)
				assert.Equal(t, tt.wantLine, findings[0].Line)
				assert.Contains(t, findings[0].Message, tt.wantMessage)
				assert.Equal(t, 1, refs, "違反も、実在を確かめた参照として数える")
			})
		}
	})
}

func Test_makeTargets(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			span string
			want []string
		}{
			"1つ":      {span: "make md-lint", want: []string{"md-lint"}},
			"複数":      {span: "make a b", want: []string{"a", "b"}},
			"フラグは飛ばす": {span: "make -s base-branch", want: []string{"base-branch"}},
			"渡せない形が出たら打ち切る":           {span: "make test 2>&1 other", want: []string{"test"}},
			"変数代入が出たら打ち切る":            {span: "make DB=local migrate", want: nil},
			"引数が無ければ空":                {span: "make", want: nil},
			"プレースホルダもターゲットとして拾う":      {span: "make versions-<verb>", want: []string{"versions-<verb>"}},
			"RUNNER_MODE のような後置も打ち切る": {span: "make md-lint RUNNER_MODE=host", want: []string{"md-lint"}},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, makeTargets(tt.span))
			})
		}
	})
}

func Test_targetSet_has(t *testing.T) {
	t.Parallel()

	set := targetSet{
		exact:    map[string]bool{"md-lint": true, "versions-apply": true},
		patterns: []*regexp.Regexp{regexp.MustCompile(`^pin-.+$`)},
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			name string
			want bool
		}{
			"厳密一致":     {name: "md-lint", want: true},
			"接尾規則に当たる": {name: "pin-actions", want: true},
			"プレースホルダは実在のターゲットに当てる":   {name: "versions-<verb>", want: true},
			"当たるものが無ければ false":       {name: "no-such", want: false},
			"プレースホルダでも当たらなければ false": {name: "nothing-<verb>", want: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, set.has(tt.name))
			})
		}
	})
}

func Test_collectMakeTargets(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("include を辿って断片のターゲットも集める", func(t *testing.T) {
			t.Parallel()
			root := fixture(t, soundRepo())
			set, err := collectMakeTargets(root)
			require.NoError(t, err)
			assert.True(t, set.exact["help"], "注記を持たない help も集める")
			assert.True(t, set.exact["md-lint"], "include した断片のターゲットも集める")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("makefile が無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, err := collectMakeTargets(t.TempDir())
			require.Error(t, err)
		})

		t.Run("include した先が無ければエラー", func(t *testing.T) {
			t.Parallel()
			root := fixture(t, map[string]string{"makefile": "include .makefiles/missing.mk\n"})
			_, err := collectMakeTargets(root)
			require.Error(t, err)
		})
	})
}

func Test_parseTargets(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			content string
			want    []string
		}{
			".PHONY から取る":    {content: ".PHONY: a\n", want: []string{".PHONY:", "a"}[1:]},
			"## の注記を落とす":     {content: ".PHONY: a ## 説明\n", want: []string{"a"}},
			".PHONY の複数宣言":   {content: ".PHONY: a b\n", want: []string{"a", "b"}},
			"規則の行から取る":       {content: "a: b\n", want: []string{"a"}},
			"変数代入は取らない":      {content: "VAR := v\n", want: nil},
			"簡易代入も取らない":      {content: "VAR = v\n", want: nil},
			"POSIX の代入も取らない": {content: "VAR ::= v\n", want: nil},
			"二重コロンの規則は取る":    {content: "a:: b\n", want: []string{"a"}},
			"レシピ行は読まない":      {content: "a:\n\techo x: y\n", want: []string{"a"}},
			"接尾規則も取る":        {content: "%.o: %.c\n", want: []string{"%.o"}},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, parseTargets(tt.content))
			})
		}
	})
}

func Test_addTarget(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("通常の名前は厳密一致として持つ", func(t *testing.T) {
			t.Parallel()
			set := targetSet{exact: map[string]bool{}}
			addTarget(&set, "md-lint")
			assert.True(t, set.exact["md-lint"])
		})

		t.Run("% を含む名前は当たる形として持つ", func(t *testing.T) {
			t.Parallel()
			set := targetSet{exact: map[string]bool{}}
			addTarget(&set, "%.o")
			require.Len(t, set.patterns, 1)
			assert.True(t, set.patterns[0].MatchString("a.o"))
			assert.False(t, set.patterns[0].MatchString("a.c"))
		})

		t.Run("空と特殊ターゲットは持たない", func(t *testing.T) {
			t.Parallel()
			set := targetSet{exact: map[string]bool{}}
			addTarget(&set, "")
			addTarget(&set, ".PHONY")
			assert.Empty(t, set.exact)
		})
	})
}

func Test_placeholderPattern(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			text    string
			match   string
			noMatch string
		}{
			"<name> は任意の1語以上に当たる": {text: "versions-<verb>", match: "versions-apply", noMatch: "versions-"},
			"* も任意の1語以上に当たる":      {text: "pin-*", match: "pin-actions", noMatch: "pin-"},
			"閉じない < はそのままの文字":     {text: "a<b", match: "a<b", noMatch: "axb"},
			"約物はそのままの文字として扱う":     {text: "a.b", match: "a.b", noMatch: "axb"},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				re := placeholderPattern(tt.text)
				assert.True(t, re.MatchString(tt.match))
				assert.False(t, re.MatchString(tt.noMatch))
			})
		}
	})
}

func Test_collectSkills(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("直下のディレクトリ名を集め、ファイルは集めない", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[".claude/skills/README.md"] = ""
			skills, err := collectSkills(fixture(t, files))
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"commit": true}, skills)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, err := collectSkills(t.TempDir())
			require.Error(t, err)
		})
	})
}

func Test_collectDocs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("配下の Markdown を並べ替えて集める", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files[".claude/skills/commit/references/note.md"] = ""
			files[".claude/skills/commit/script.sh"] = ""
			root := fixture(t, files)
			docs, err := collectDocs(root)
			require.NoError(t, err)
			assert.Equal(t, []string{
				".claude/skills/commit/SKILL.md",
				".claude/skills/commit/references/note.md",
			}, docs, "root からの相対パスで、並べ替えて返す")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, err := collectDocs(t.TempDir())
			require.Error(t, err)
		})
	})
}

func Test_rootEntries(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("直下の名前を集め、作業場所と .git を外す", func(t *testing.T) {
			t.Parallel()
			files := soundRepo()
			files["tmp/x"] = ""
			files[".git/config"] = ""
			roots, err := rootEntries(fixture(t, files))
			require.NoError(t, err)
			assert.True(t, roots[".makefiles"])
			assert.False(t, roots["tmp"], "gitignore された作業場所は在ることを前提にできない")
			assert.False(t, roots[".git"])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ディレクトリが無ければエラー", func(t *testing.T) {
			t.Parallel()
			_, err := rootEntries(filepath.Join(t.TempDir(), "absent"))
			require.Error(t, err)
		})
	})
}

func Test_asRepoPath(t *testing.T) {
	t.Parallel()

	root := fixture(t, map[string]string{
		"scripts/lib/xerrors/errors.go":  "",
		".claude/skills/commit/SKILL.md": "",
		"docs/adr/0001-x.md":             "",
	})
	roots := map[string]bool{"scripts": true, "docs": true, ".claude": true}
	const from = ".claude/skills/commit"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			span string
			want string
			ok   bool
		}{
			"通常のパス":               {span: "docs/adr/0001-x.md", want: "docs/adr/0001-x.md", ok: true},
			"先頭の ./ を落とす":         {span: "./docs/adr/0001-x.md", want: "docs/adr/0001-x.md", ok: true},
			"末尾の / を落としてディレクトリ参照": {span: "docs/adr/", want: "docs/adr", ok: true},
			"スラッシュが無ければパスでない":     {span: "makefile", ok: false},
			"空白を含めばパスでない":         {span: "cd docs/adr", ok: false},
			"約物を含めばパスでない":         {span: "docs/adr/#anchor", ok: false},
			"省略記法を含めばパスでない":       {span: "scripts/.../x.go", ok: false},
			"拡張子の無いファイル参照はパスでない":  {span: "scripts/lib/xerrors", ok: false},
			"パッケージとシンボルの組はパスでない":  {span: "scripts/lib/xerrors.Wrap", ok: false},
			// 前半が実在しなければシンボルの名指しではない。ここを落とすと、綴りを誤った
			// パス参照が「シンボルだから」と検査対象から外れる。
			"前半が実在しなければパスとして扱う": {span: "scripts/lib/absent.Wrap", want: "scripts/lib/absent.Wrap", ok: true},
			"先頭が直下に無ければパスでない":   {span: "modules/foo/README.md", ok: false},
			"`..` を含めばパスでない":    {span: "../../../etc/passwd", ok: false},
			"途中の `..` もパスでない":   {span: "docs/../../etc/passwd", ok: false},
			"参照元の隣にも無ければパスでない":  {span: "references/audit.md", ok: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, ok := asRepoPath(tt.span, roots, from, root)
				assert.Equal(t, tt.ok, ok)
				if tt.ok {
					assert.Equal(t, tt.want, got)
				}
			})
		}
	})
}

func Test_existsIn(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		root := fixture(t, map[string]string{"a/b.txt": ""})
		assert.True(t, existsIn(root, "a"))
		assert.False(t, existsIn(root, "absent"))
	})
}

func Test_pathExists(t *testing.T) {
	t.Parallel()

	root := fixture(t, map[string]string{
		".claude/skills/commit/SKILL.md":           "",
		".claude/skills/commit/references/note.md": "",
		".claude/agents/reviewer.md":               "",
		"docs/adr/0001-x.md":                       "",
	})
	const from = ".claude/skills/commit"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			candidate string
			want      bool
		}{
			"ルートから解決する":              {candidate: "docs/adr/0001-x.md", want: true},
			"参照元の隣から解決する":            {candidate: "references/note.md", want: true},
			"どちらでも解決しなければ false":     {candidate: "docs/absent.md", want: false},
			"列挙はすべて実在して初めて真":         {candidate: ".claude/{skills,agents}/", want: true},
			"列挙の片方が欠ければ偽":            {candidate: ".claude/{skills,commands}/", want: false},
			"展開が上限を超えれば偽":            {candidate: ".claude/{a,b,c,d}/{a,b,c,d}/{a,b,c,d}/{a,b,c,d}/x.md", want: false},
			"ワイルドカードはその手前の実在を見る":     {candidate: "docs/adr/*.md", want: true},
			"ワイルドカードの手前が無ければ偽":       {candidate: "docs/absent/*.md", want: false},
			"先頭からワイルドカードなら確かめる部分が無い": {candidate: "**/*.go", want: true},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, pathExists(root, from, tt.candidate))
			})
		}
	})
}

// alternatives は、選択肢を n 個持つ列挙を組み立てます。展開数がちょうど n になります。
func alternatives(n int) string {
	return "{" + strings.TrimSuffix(strings.Repeat("x,", n), ",") + "}"
}

func Test_expandBraces(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			text string
			want []string
		}{
			"列挙を展開する":        {text: "a/{b,c}/d", want: []string{"a/b/d", "a/c/d"}},
			"直列の列挙を展開する":     {text: "{a,b}/{c,d}", want: []string{"a/c", "a/d", "b/c", "b/d"}},
			"入れ子を展開する":       {text: "a/{b,{c,d}}.tf", want: []string{"a/b.tf", "a/c.tf", "a/d.tf"}},
			"空の選択肢は候補を増やさない": {text: "a/{b,c,}.tf", want: []string{"a/b.tf", "a/c.tf"}},
			"列挙が無ければそのまま":    {text: "a/b", want: []string{"a/b"}},
			"閉じが無ければそのまま":    {text: "a/{b", want: []string{"a/{b"}},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, ok := expandBraces(tt.text)
				require.True(t, ok)
				assert.Equal(t, tt.want, got)
			})
		}

		// 上限そのもの。1つ上（異常系）と対にしないと `>` を `>=` へ書き換える退行を区別できない。
		t.Run("上限ちょうどは展開する", func(t *testing.T) {
			t.Parallel()
			got, ok := expandBraces("docs/" + alternatives(maxBraceCandidates) + ".md")
			require.True(t, ok)
			assert.Len(t, got, maxBraceCandidates)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("上限を1つ超えたら false を返す", func(t *testing.T) {
			t.Parallel()
			// 黙って諦めると、そこだけ検査が消える。
			_, ok := expandBraces("docs/" + alternatives(maxBraceCandidates+1) + ".md")
			assert.False(t, ok)
		})
	})
}

func Test_splitAlternatives(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			inner string
			want  []string
		}{
			"最も外側のカンマで分ける":    {inner: "a,b", want: []string{"a", "b"}},
			"入れ子の中のカンマでは分けない": {inner: "b,{c,d}", want: []string{"b", "{c,d}"}},
			"入れ子が2段でも分けない":    {inner: "b,{c,{d,e}}", want: []string{"b", "{c,{d,e}}"}},
			"カンマが無ければ1つ":      {inner: "a", want: []string{"a"}},
			"末尾のカンマは空の選択肢になる": {inner: "a,b,", want: []string{"a", "b", ""}},
			"空なら空の選択肢1つ":      {inner: "", want: []string{""}},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, splitAlternatives(tt.inner))
			})
		}
	})
}

func Test_matchingBrace(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			text  string
			begin int
			want  int
		}{
			"対応する閉じを返す":    {text: "a{b}c", begin: 1, want: 3},
			"入れ子の外側を返す":    {text: "a{b,{c}}d", begin: 1, want: 7},
			"閉じが無ければ -1":   {text: "a{b", begin: 1, want: -1},
			"閉じが足りなければ -1": {text: "a{b,{c}", begin: 1, want: -1},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, matchingBrace(tt.text, tt.begin))
			})
		}
	})
}

func Test_resolves(t *testing.T) {
	t.Parallel()

	root := fixture(t, map[string]string{"docs/adr/0001-x.md": "", ".claude/skills/commit/note.md": ""})

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		assert.True(t, resolves(root, ".", "docs/adr/0001-x.md"))
		assert.True(t, resolves(root, ".claude/skills/commit", "note.md"))
		assert.False(t, resolves(root, ".", "docs/absent.md"))
		assert.True(t, resolves(root, ".", "docs/adr/<name>.md"), "形が置かれる場所が在れば真")
		assert.False(t, resolves(root, ".", "absent/<name>.md"))
	})
}

func Test_literalParent(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			candidate string
			want      string
		}{
			"ワイルドカードの手前を返す":    {candidate: "docs/adr/*.md", want: "docs/adr"},
			"プレースホルダの手前を返す":    {candidate: "docs/<a>/x.md", want: "docs"},
			"先頭がワイルドカードなら空":    {candidate: "**/x.go", want: ""},
			"ワイルドカードが無ければそのまま": {candidate: "docs/adr", want: "docs/adr"},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, literalParent(tt.candidate))
			})
		}
	})
}
