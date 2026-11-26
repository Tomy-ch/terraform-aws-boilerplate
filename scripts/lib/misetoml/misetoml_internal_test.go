package misetoml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_sectionName(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			raw  string
			want string
		}{
			"装飾が無い場合、そのまま返す": {raw: "tools", want: "tools"},

			// 正規化を落とすと下の表記が別の節になる（理由は sectionName の宣言）。
			"前後に空白がある場合、空白を落とす":        {raw: " tools ", want: "tools"},
			"二重引用符付きの場合、引用符を外す":        {raw: `"tools"`, want: "tools"},
			"単引用符付きの場合、引用符を外す":         {raw: `'tools'`, want: "tools"},
			"引用符の外側に空白がある場合、両方を落とす":    {raw: ` "tools" `, want: "tools"},
			"引用符の内側の空白は、名前の一部として残す":    {raw: `" tools "`, want: " tools "},
			"dotted key の場合、別の節のままにする": {raw: "tools.go", want: "tools.go"},
			"閉じていない引用符の場合、外さない":        {raw: `"tools`, want: `"tools`},

			// 引用符を外す条件は `len(name) >= 2` です。両側を固定しないと、
			// 1文字の引用符で添字が負へ回る形を見落とします。
			"引用符2つだけの場合、空の名前になる": {raw: `""`, want: ""},
			"引用符1つだけの場合、外さない":    {raw: `"`, want: `"`},

			"空文字の場合、空文字を返す":  {raw: "", want: ""},
			"空白だけの場合、空文字を返す": {raw: "   ", want: ""},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, tt.want, sectionName(tt.raw))
			})
		}
	})
}

func Test_parseLine(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line        string
			wantKey     string
			wantVersion string
		}{
			"裸キーの場合、キーと版を返す":       {line: `go = "1.27.1"`, wantKey: "go", wantVersion: "1.27.1"},
			"数字始まりの裸キーの場合、キーと版を返す": {line: `1password = "2.0.0"`, wantKey: "1password", wantVersion: "2.0.0"},
			"引用符付きキーの場合、引用符を外して返す": {
				line: `"aqua:owner/repo" = "1.0.0"`, wantKey: "aqua:owner/repo", wantVersion: "1.0.0",
			},
			"等号の周りに空白が無い場合、キーと版を返す": {line: `go="1.27.1"`, wantKey: "go", wantVersion: "1.27.1"},
			"行末コメントがある場合、コメントを無視する": {
				line: `go = "1.27.1" # 理由`, wantKey: "go", wantVersion: "1.27.1",
			},
			"tool option 付きの場合、version を取り出す": {
				line: `node = { version = "24.21.0", postinstall = "x" }`, wantKey: "node", wantVersion: "24.21.0",
			},
			"tool option が version より前にある場合も、version を取り出す": {
				line: `node = { postinstall = "x", version = "24.21.0" }`, wantKey: "node", wantVersion: "24.21.0",
			},
			"引用符付きキーの tool option の場合、引用符を外して返す": {
				line: `"aqua:owner/repo" = { version = "1.0.0" }`, wantKey: "aqua:owner/repo", wantVersion: "1.0.0",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				key, version, ok := parseLine(tt.line)

				assert.True(t, ok)
				assert.Equal(t, tt.wantKey, key)
				assert.Equal(t, tt.wantVersion, version)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// ここで true を返した行は、呼び出し側でエラーにならずに通ります。
		// 「解釈できない行を読み飛ばさない」規律は、この false に乗っています。
		tests := map[string]string{
			"配列で複数版を並べている場合、ok が false になる":                `go = ["1.27.1", "1.26.0"]`,
			"tool option が version を持たない場合、ok が false になる": `node = { postinstall = "x" }`,
			"版が引用符で囲まれていない場合、ok が false になる":               `go = 1.27.1`,
			"版が単引用符で囲まれている場合、ok が false になる":               `go = '1.27.1'`,
			"版が空の場合、ok が false になる":                        `go = ""`,
			"キーが空の場合、ok が false になる":                       `"" = "1.27.1"`,
			"裸キーに使えない文字を引用符なしで含む場合、ok が false になる":         `aqua:owner/repo = "1.0.0"`,
			"等号が無い場合、ok が false になる":                       `go "1.27.1"`,
			"節の見出しの場合、ok が false になる":                      `[tools]`,
			"コメント行の場合、ok が false になる":                      `# go = "1.27.1"`,
			"空行の場合、ok が false になる":                         ``,
		}

		for name, line := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				key, version, ok := parseLine(line)

				assert.False(t, ok)
				assert.Empty(t, key)
				assert.Empty(t, version)
			})
		}
	})
}

func Test_parseSettingList(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line string
			want []string
		}{
			"要素を並べた場合、書かれた順で返す":     {line: `disable_tools = ["npm:a", "npm:b"]`, want: []string{"npm:a", "npm:b"}},
			"末尾にカンマがある場合、要素だけを返す":   {line: `disable_tools = ["npm:a",]`, want: []string{"npm:a"}},
			"空の配列の場合、空を返す":          {line: `disable_tools = []`, want: []string{}},
			"引用符付きキーの場合、裸のキーと同じに読む": {line: `"disable_tools" = ["npm:a"]`, want: []string{"npm:a"}},
			"空白だけの配列の場合、空を返す":       {line: `disable_tools = [ ]`, want: []string{}},
			"行末コメントがある場合、コメントを無視する": {line: `disable_tools = ["npm:a"] # 理由`, want: []string{"npm:a"}},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, ok := parseSettingList(settingListRe("disable_tools"), tt.line)

				assert.True(t, ok)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"配列が閉じていない場合、ok が false になる":      `disable_tools = [`,
			"要素が引用符で囲まれていない場合、ok が false になる": `disable_tools = [npm:a]`,
			"空の要素を挟む場合、ok が false になる":        `disable_tools = [, "npm:a"]`,
			"単引用符の要素の場合、ok が false になる":       `disable_tools = ['npm:a']`,
			"要素の間が空の場合、ok が false になる":        `disable_tools = ["npm:a",,"npm:b"]`,
			"カンマだけの場合、ok が false になる":         `disable_tools = [,]`,
			"空の文字列の要素の場合、ok が false になる":      `disable_tools = [""]`,
			"配列の後に余りがある場合、ok が false になる":     `disable_tools = ["npm:a"] x`,
		}

		for name, line := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, ok := parseSettingList(settingListRe("disable_tools"), line)

				assert.False(t, ok)
				assert.Nil(t, got)
			})
		}
	})
}

func Test_settingList(t *testing.T) {
	t.Parallel()

	t.Run("[settings] の key の配列を返し、ほかの key と節を読まない", func(t *testing.T) {
		t.Parallel()

		got, found, err := settingList([]byte("[tools]\nkey = [\"t\"]\n[settings]\nother = [\"x\"]\nkey = [\"a\", \"b\"]\n"), "key", ErrInvalidLine)

		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("重複した宣言は、渡したエラーで返す", func(t *testing.T) {
		t.Parallel()

		_, _, err := settingList([]byte("[settings]\nkey = [\"a\"]\nkey = [\"b\"]\n"), "key", ErrInvalidLine)

		require.ErrorIs(t, err, ErrInvalidLine)
	})

	t.Run("節の外の dotted key は、渡したエラーで返す", func(t *testing.T) {
		t.Parallel()

		_, _, err := settingList([]byte("settings.key = [\"a\"]\n"), "key", ErrInvalidLine)

		require.ErrorIs(t, err, ErrInvalidLine)
	})

	t.Run("key の名前に正規表現の記号があっても、その字面にだけ当てる", func(t *testing.T) {
		t.Parallel()

		got, found, err := settingList([]byte("[settings]\naxb = [\"x\"]\n"), "a.b", ErrInvalidLine)

		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, got)
	})
}

func Test_settingListFile(t *testing.T) {
	t.Parallel()

	t.Run("ファイルを読んで渡した読み手の結果を返す", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "mise.toml")
		require.NoError(t, os.WriteFile(path, []byte("[settings]\nlockfile_platforms = [\"linux-x64\"]\n"), 0o600))

		got, found, err := settingListFile(path, LockfilePlatforms)

		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []string{"linux-x64"}, got)
	})

	t.Run("読み手のエラーに path を添える", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "mise.toml")
		require.NoError(t, os.WriteFile(path, []byte("[settings]\nlockfile_platforms = [x]\n"), 0o600))

		_, _, err := settingListFile(path, LockfilePlatforms)

		require.ErrorIs(t, err, ErrInvalidLockfilePlatforms)
		assert.Contains(t, err.Error(), path)
	})
}

func Test_settingListRe(t *testing.T) {
	t.Parallel()

	re := settingListRe("lockfile_platforms")
	for line, want := range map[string]bool{
		`lockfile_platforms = ["linux-x64"]`: true,
		`"lockfile_platforms" = []`:          true,
		`'lockfile_platforms' = []`:          false,
		`disable_tools = ["npm:a"]`:          false,
		`lockfile_platforms = "linux-x64"`:   false,
	} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, re.MatchString(line))
		})
	}
}

func Test_lockField(t *testing.T) {
	t.Parallel()

	t.Run("entry の version と backend を書き、specifiers を読み飛ばす", func(t *testing.T) {
		t.Parallel()
		e := &LockEntry{Platforms: map[string]LockPlatform{}}
		fields := map[string]bool{}

		require.NoError(t, lockField(e, "", `version = "1"`, fields))
		require.NoError(t, lockField(e, "", `backend = "aqua:o/a"`, fields))
		require.NoError(t, lockField(e, "", `specifiers = ["1"]`, fields))

		assert.Equal(t, "1", e.Version)
		assert.Equal(t, "aqua:o/a", e.Backend)
	})

	t.Run("platform の url と checksum を書き、url_api と provenance を読み飛ばす", func(t *testing.T) {
		t.Parallel()
		e := &LockEntry{Platforms: map[string]LockPlatform{"linux-x64": {}}}
		fields := map[string]bool{}

		for _, line := range []string{`url = "https://x/1"`, `checksum = "sha256:a"`, `url_api = "https://api/1"`, `provenance = "github-attestations"`} {
			require.NoError(t, lockField(e, "linux-x64", line, fields))
		}

		assert.Equal(t, LockPlatform{URL: "https://x/1", Checksum: "sha256:a"}, e.Platforms["linux-x64"])
	})

	for name, tt := range map[string]struct{ platform, line string }{
		"entry の中の url":          {"", `url = "https://x/1"`},
		"platform の中の version":   {"linux-x64", `version = "1"`},
		"entry の中の inline table": {"", `options = { a = 1 }`},
	} {
		t.Run(name+"は ErrInvalidLock", func(t *testing.T) {
			t.Parallel()
			e := &LockEntry{Platforms: map[string]LockPlatform{"linux-x64": {}}}
			fields := map[string]bool{}

			require.ErrorIs(t, lockField(e, tt.platform, tt.line, fields), ErrInvalidLock)
		})
	}
}

func Test_markSeen(t *testing.T) {
	t.Parallel()

	t.Run("初めての key は控え、同じ所の2度目は ErrInvalidLock", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}

		require.NoError(t, markSeen(seen, "", "version"))
		require.ErrorIs(t, markSeen(seen, "", "version"), ErrInvalidLock)
	})

	t.Run("entry と platform、platform どうしは別に数える", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}

		require.NoError(t, markSeen(seen, "", "url"))
		require.NoError(t, markSeen(seen, "linux-x64", "url"))
		require.NoError(t, markSeen(seen, "linux-arm64", "url"))
	})
}
