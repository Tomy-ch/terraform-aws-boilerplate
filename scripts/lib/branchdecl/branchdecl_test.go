package branchdecl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "b.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

const validBody = `{"deploy": [{"branch": "admin", "env": "admin", "from": ["release/"]}, {"branch": "production", "release_base": true}], "lines": {"release": "release/", "hotfix": "hotfix/"}, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("宣言を読む", func(t *testing.T) {
		t.Parallel()
		b, err := Read(writeFile(t, validBody))
		require.NoError(t, err)
		assert.Equal(t, Branches{
			Deploy:      []DeployBranch{{Branch: "admin", Env: "admin", From: []string{"release/"}}, {Branch: "production", ReleaseBase: true}},
			Lines:       Lines{Release: "release/", Hotfix: "hotfix/"},
			LineVersion: VersionForm,
			Revert:      "revert/",
		}, b)
	})
	tail := `, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`
	lines := `"lines": {"release": "release/", "hotfix": "hotfix/"}`
	for name, c := range map[string]struct {
		body string
		want error
	}{
		"解釈できなければ失敗する":          {`{"deploy": "production"}`, ErrDeclaration},
		"名前だけの deploy は拒む":      {`{"deploy": ["production"], ` + lines + tail, ErrDeclaration},
		"entry の未知のキーは拒む":       {`{"deploy": [{"branch": "a", "to": "b"}]` + tail, ErrDeclaration},
		"規則に反すれば失敗する":           {`{"deploy": [], "lines": {}` + tail, ErrEmpty},
		"null は0件として拒む":         {`null`, ErrEmpty},
		"配列なら拒む":                {`["production"]`, ErrDeclaration},
		"lines が配列なら拒む":         {`{"deploy": [{"branch": "production", "release_base": true}], "lines": ["release/"]` + tail, ErrDeclaration},
		"lines の未知の役割は拒む":       {`{"deploy": [{"branch": "production", "release_base": true}], "lines": {"release": "release/", "hotfix": "hotfix/", "feature": "feature/"}` + tail, ErrDeclaration},
		"revert が無ければ拒む":        {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "line_version": "v<major>.<minor>.<patch>"}`, ErrDeclaration},
		"revert の字面が違えば拒む":      {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "line_version": "v<major>.<minor>.<patch>", "Revert": "revert/"}`, ErrDeclaration},
		"revert が重複すれば拒む":       {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "line_version": "v<major>.<minor>.<patch>", "revert": "a/", "revert": "b/"}`, ErrDeclaration},
		"line_version が無ければ拒む":  {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "revert": "revert/"}`, ErrDeclaration},
		"environment のキーは拒む":    {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "name": "x"` + tail, ErrDeclaration},
		"最上位の env は拒む":          {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "env": "x"` + tail, ErrDeclaration},
		"最上位の from は拒む":         {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "from": ["release/"]` + tail, ErrDeclaration},
		"最上位の branch は拒む":       {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "branch": "x"` + tail, ErrDeclaration},
		"最上位の release_base は拒む": {`{"deploy": [{"branch": "production", "release_base": true}], ` + lines + `, "release_base": true` + tail, ErrDeclaration},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(writeFile(t, c.body))
			require.ErrorIs(t, err, c.want)
		})
	}
	t.Run("拒む理由は確かめたい規則のもの", func(t *testing.T) {
		t.Parallel()
		tail := `, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`
		lines := `"lines": {"release": "release/", "hotfix": "hotfix/"}`
		deploy := `{"deploy": [{"branch": "production", "release_base": true}], `
		for name, c := range map[string]struct {
			body, want string
		}{
			"字面の違うキーはキーの検査で拒む": {deploy + lines + `, "Revert": "revert/", "line_version": "v<major>.<minor>.<patch>"}`, "宣言のキーではありません"},
			"重複したキーはキーの検査で拒む":  {deploy + lines + `, "revert": "a/"` + tail, "重複しています"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := Read(writeFile(t, c.body))
				require.ErrorIs(t, err, ErrDeclaration)
				assert.ErrorContains(t, err, c.want)
			})
		}
	})
}

func deploys(names ...string) []DeployBranch {
	out := make([]DeployBranch, 0, len(names))
	for _, n := range names {
		out = append(out, DeployBranch{Branch: n})
	}
	return out
}

var (
	lines     = []string{"release/", "hotfix/"}
	bothLines = Lines{Release: "release/", Hotfix: "hotfix/"}
	admin     = DeployBranch{Branch: "admin", Env: "admin", From: []string{"release/"}}
	base      = DeployBranch{Branch: "production", ReleaseBase: true}
)

// valid は、Validate を通る宣言を返します。各ケースはここから1点だけを変える。
func valid(deploy ...DeployBranch) Branches {
	return Branches{Deploy: append([]DeployBranch{base}, deploy...), Lines: bothLines, LineVersion: VersionForm, Revert: "revert/"}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	with := func(f func(*Branches)) Branches {
		b := valid()
		f(&b)
		return b
	}
	cases := []struct {
		name    string
		in      Branches
		wantErr error
	}{
		{"固定名と接頭辞を持つ宣言は通る", valid(deploys("develop")...), nil},
		{"0件は拒む", Branches{Revert: "revert/", LineVersion: VersionForm}, ErrEmpty},
		{"デプロイブランチが無く線だけなら release_base が無いとして拒む", Branches{Lines: bothLines, LineVersion: VersionForm, Revert: "revert/"}, ErrDeclaration},
		{"glob の文字を含む固定名は拒む", valid(deploys("release/*")...), ErrDeclaration},
		{"大文字の固定名は拒む", valid(deploys("Main")...), ErrDeclaration},
		{"固定名の重複は拒む", valid(deploys("develop", "develop")...), ErrDeclaration},
		{"/ を含む固定名は拒む", valid(deploys("release/")...), ErrDeclaration},
		{"空文字列の固定名は拒む", valid(deploys("")...), ErrDeclaration},
		{"env と from を持つデプロイブランチは通る", valid(admin), nil},
		{"env を持つデプロイブランチが複数でも通る", valid(admin, DeployBranch{Branch: "security", Env: "security", From: []string{"release/"}}, DeployBranch{Branch: "log-archive", Env: "log-archive", From: []string{"release/"}}), nil},
		{"from を複数持つデプロイブランチは通る", valid(DeployBranch{Branch: "staging", Env: "stg", From: lines}), nil},
		{"release_base が env を持っても通る", with(func(b *Branches) { b.Deploy[0].Env, b.Deploy[0].From = "prd", lines }), nil},
		{"env が同じデプロイブランチが2つなら拒む", valid(admin, DeployBranch{Branch: "admin2", Env: "admin", From: []string{"release/"}}), ErrDeclaration},
		{"env を持ち from が無ければ拒む", valid(DeployBranch{Branch: "admin", Env: "admin"}), ErrDeclaration},
		{"env を持ち from が空なら拒む", valid(DeployBranch{Branch: "admin", Env: "admin", From: []string{}}), ErrDeclaration},
		{"from が lines に無ければ拒む", valid(DeployBranch{Branch: "admin", Env: "admin", From: []string{"feature/"}}), ErrDeclaration},
		{"from の重複は拒む", valid(DeployBranch{Branch: "admin", Env: "admin", From: []string{"release/", "release/"}}), ErrDeclaration},
		{"env を持たず from を持てば拒む", valid(DeployBranch{Branch: "develop", From: []string{"release/"}}), ErrDeclaration},
		{"env を持たず空の from を持てば拒む", valid(DeployBranch{Branch: "develop", From: []string{}}), ErrDeclaration},
		{"大文字の env は拒む", valid(DeployBranch{Branch: "admin", Env: "Admin", From: []string{"release/"}}), ErrDeclaration},
		{"glob の文字を含む env は拒む", valid(DeployBranch{Branch: "admin", Env: "ad*", From: []string{"release/"}}), ErrDeclaration},
		{"release_base が無ければ拒む", with(func(b *Branches) { b.Deploy[0].ReleaseBase = false }), ErrDeclaration},
		{"release_base が2つなら拒む", valid(DeployBranch{Branch: "develop", ReleaseBase: true}), ErrDeclaration},
		{"hotfix の線が無ければ拒む", with(func(b *Branches) { b.Lines.Hotfix = "" }), ErrDeclaration},
		{"release の線が無ければ拒む", with(func(b *Branches) { b.Lines.Release = "" }), ErrDeclaration},
		{"`/` で終わらない接頭辞は拒む", with(func(b *Branches) { b.Lines.Release = "release" }), ErrDeclaration},
		{"glob の文字を含む接頭辞は拒む", with(func(b *Branches) { b.Lines.Hotfix = "hotfix/*/" }), ErrDeclaration},
		{"2つの役割が同じ接頭辞なら拒む", with(func(b *Branches) { b.Lines.Hotfix = "release/" }), ErrDeclaration},
		{"line_version が空なら拒む", with(func(b *Branches) { b.LineVersion = "" }), ErrDeclaration},
		{"line_version が別の形なら拒む", with(func(b *Branches) { b.LineVersion = "v<major>.<minor>" }), ErrDeclaration},
		{"revert の接頭辞が無ければ拒む", with(func(b *Branches) { b.Revert = "" }), ErrDeclaration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tc.in)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func Test_validateRevert(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in      Branches
		wantErr error
	}{
		"lines と重ならない接頭辞は通る":       {Branches{Lines: bothLines, Revert: "revert/"}, nil},
		"数字で始まる接頭辞も通る":             {Branches{Lines: Lines{Release: "release/"}, Revert: "2fa/"}, nil},
		"空は拒む":                     {Branches{Lines: Lines{Release: "release/"}}, ErrDeclaration},
		"`/` で終わらない接頭辞は拒む":         {Branches{Revert: "revert"}, ErrDeclaration},
		"大文字の接頭辞は拒む":               {Branches{Revert: "Revert/"}, ErrDeclaration},
		"lines と同じ接頭辞は拒む":          {Branches{Lines: Lines{Hotfix: "hotfix/"}, Revert: "hotfix/"}, ErrDeclaration},
		"中に / を持つ接頭辞は拒む（入れ子を作れない）": {Branches{Lines: Lines{Release: "release/"}, Revert: "release/x/"}, ErrDeclaration},
		"lines の接頭辞を下に持つ接頭辞は拒む":    {Branches{Lines: Lines{Release: "revert/x/"}, Revert: "revert/"}, ErrDeclaration},
		"`/` で区切らずに始まりが同じだけなら通る":   {Branches{Lines: Lines{Release: "release/"}, Revert: "releasex/"}, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateRevert(tc.in)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestDecodeStrict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		wantErr error
	}{
		{"正しい JSON は読める", `{"deploy": [{"branch": "a"}]}`, nil},
		{"未知のキーは拒む", `{"deploy": [{"branch": "a"}], "extra": 1}`, ErrDeclaration},
		{"値の後の余分なトークンは拒む", `{"deploy": [{"branch": "a"}]} {}`, ErrDeclaration},
		{"JSON でないものは拒む", `deploy: a`, ErrDeclaration},
		{"空のファイルは拒む", ``, ErrDeclaration},
		{"キーの重複は拒む", `{"deploy": [{"branch": "b"}], "deploy": [{"branch": "a"}]}`, ErrDeclaration},
		{"字面の違うキーは拒む", `{"Deploy": [{"branch": "a"}]}`, ErrDeclaration},
		{"entry の字面の違うキーは拒む", `{"deploy": [{"Branch": "a"}]}`, ErrDeclaration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b Branches
			err := DecodeStrict(writeFile(t, tc.body), &b, branchKeys)
			if tc.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, []string{"a"}, b.DeployNames())
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		var b Branches
		err := DecodeStrict(filepath.Join(t.TempDir(), "none.json"), &b, branchKeys)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_checkKeys(t *testing.T) {
	t.Parallel()

	keys := map[string]bool{"deploy": true, "lines": true, "name": true, "deploy_from": true}
	cases := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{"宣言のキーだけなら通る", `[{"name": "x", "deploy_from": "any"}]`, false},
		{"入れ子の配列とスカラーも辿る", `{"deploy": ["a", "b"], "lines": []}`, false},
		{"宣言のキーでなければ拒む", `{"extra": 1}`, true},
		{"字面の違うキーは拒む", `{"DEPLOY_FROM": "any"}`, true},
		{"入れ子の object のキーの重複も拒む", `[{"name": "a", "name": "b"}]`, true},
		{"配列の中の不正なキーも拒む", `[{"name": "a"}, {"nope": 1}]`, true},
		{"壊れた JSON は拒む", `{"deploy": [`, true},
		{"閉じられていない object は拒む", `{"deploy": ["a"]`, true},
		{"キーの位置の構文エラーは拒む", `{"deploy": ["a"],}`, true},
		{"空の入力は拒む", ``, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkKeys(json.NewDecoder(strings.NewReader(tc.src)), keys)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrDeclaration)
		})
	}
}

func TestBranches_DeployNames(t *testing.T) {
	t.Parallel()

	t.Run("宣言の順に名前を返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"admin", "production"}, Branches{Deploy: []DeployBranch{admin, {Branch: "production"}}}.DeployNames())
	})
	t.Run("デプロイブランチが無ければ空を返す", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, Branches{}.DeployNames())
	})
}

func TestBranches_DeployForEnv(t *testing.T) {
	t.Parallel()

	b := Branches{Deploy: []DeployBranch{{Branch: "production"}, admin}}
	t.Run("env を持つ entry を返す", func(t *testing.T) {
		t.Parallel()
		got, ok := b.DeployForEnv("admin")
		require.True(t, ok)
		assert.Equal(t, admin, got)
	})
	t.Run("env を持たない entry は返さない", func(t *testing.T) {
		t.Parallel()
		_, ok := b.DeployForEnv("prd")
		assert.False(t, ok)
	})
	t.Run("空の env は env を持たない entry に当たらない", func(t *testing.T) {
		t.Parallel()
		_, ok := b.DeployForEnv("")
		assert.False(t, ok)
	})
}

func TestBranches_DeployForBranch(t *testing.T) {
	t.Parallel()

	b := Branches{Deploy: []DeployBranch{{Branch: "production"}, admin}}
	t.Run("env を持つデプロイブランチを返す", func(t *testing.T) {
		t.Parallel()
		got, ok := b.DeployForBranch("admin")
		require.True(t, ok)
		assert.Equal(t, admin, got)
	})
	t.Run("env を持たないデプロイブランチは返さない", func(t *testing.T) {
		t.Parallel()
		_, ok := b.DeployForBranch("production")
		assert.False(t, ok)
	})
	t.Run("デプロイブランチでない名前は返さない", func(t *testing.T) {
		t.Parallel()
		_, ok := b.DeployForBranch("release/v1.0.0")
		assert.False(t, ok)
	})
}

func TestDeployBranch_FromLine(t *testing.T) {
	t.Parallel()

	d := DeployBranch{Branch: "production", Env: "prd", From: []string{"release/", "hotfix/"}}
	for name, c := range map[string]struct {
		line string
		want bool
	}{
		"from の線に当たる":       {"release/v1.0.0", true},
		"2つ目の from の線にも当たる": {"hotfix/v1.0.1", true},
		"接頭辞そのものは線ではない":     {"release/", false},
		"別の線は当たらない":         {"feature/1", false},
		"区切りの無い同じ始まりは当たらない": {"releasex", false},
		"デプロイブランチ自身は当たらない":  {"production", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, d.FromLine(c.line))
		})
	}
}

func Test_validateDeployEnv(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		d       DeployBranch
		seen    map[string]bool
		wantErr string
	}{
		"env の無い entry は from が無ければ通る": {DeployBranch{Branch: "develop"}, map[string]bool{}, ""},
		"env と lines の from を持てば通る":    {admin, map[string]bool{}, ""},
		"既に数えた env は拒む":                {admin, map[string]bool{"admin": true}, "複数あります"},
		"from の無い env は拒む":             {DeployBranch{Branch: "admin", Env: "admin"}, map[string]bool{}, "from を1つ以上持たなければなりません"},
		"lines に無い from は拒む":           {DeployBranch{Branch: "admin", Env: "admin", From: []string{"feature/"}}, map[string]bool{}, "lines の接頭辞ではありません"},
		"env の無い entry の from は空でも拒む":  {DeployBranch{Branch: "develop", From: []string{}}, map[string]bool{}, "from を持てません"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateDeployEnv(tc.d, tc.seen, lines)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrDeclaration)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	t.Run("通った env を数える", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}
		require.NoError(t, validateDeployEnv(admin, seen, lines))
		assert.True(t, seen["admin"])
	})
}

func TestRoles(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"release", "hotfix"}, Roles())
}

func TestLines_Prefixes(t *testing.T) {
	t.Parallel()

	t.Run("役割の順で返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"rel/", "hot/"}, Lines{Hotfix: "hot/", Release: "rel/"}.Prefixes())
	})
	t.Run("空の役割は返さない", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"hot/"}, Lines{Hotfix: "hot/"}.Prefixes())
		assert.Empty(t, Lines{}.Prefixes())
	})
}

func TestLines_Prefix(t *testing.T) {
	t.Parallel()

	l := Lines{Release: "rel/", Hotfix: "hot/"}
	for name, c := range map[string]struct {
		role   string
		want   string
		wantOK bool
	}{
		"release の接頭辞":  {"release", "rel/", true},
		"hotfix の接頭辞":   {"hotfix", "hot/", true},
		"役割でない名前":       {"feature", "", false},
		"接頭辞そのものは役割でない": {"rel/", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := l.Prefix(c.role)
			assert.Equal(t, c.wantOK, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestBranches_ReleaseBase(t *testing.T) {
	t.Parallel()

	t.Run("印を持つ entry を返す", func(t *testing.T) {
		t.Parallel()
		got, ok := valid(admin).ReleaseBase()
		require.True(t, ok)
		assert.Equal(t, base, got)
	})
	t.Run("印が無ければ ok は false", func(t *testing.T) {
		t.Parallel()
		_, ok := Branches{Deploy: []DeployBranch{admin}}.ReleaseBase()
		assert.False(t, ok)
	})
}

func TestParseVersion(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		in     string
		want   Version
		wantOK bool
	}{
		"版を読む":         {"v1.10.3", Version{1, 10, 3}, true},
		"前置のゼロも数として読む": {"v01.0.0", Version{1, 0, 0}, true},
		"v の無いものは拒む":   {"1.0.0", Version{}, false},
		"2つの数は拒む":      {"v1.0", Version{}, false},
		"プレリリースは拒む":    {"v1.0.0-rc.1", Version{}, false},
		"ビルドメタデータは拒む":  {"v1.0.0+build", Version{}, false},
		"桁あふれは拒む":      {"v99999999999999999999.0.0", Version{}, false},
		"接頭辞の付いたものは拒む": {"release/v1.0.0", Version{}, false},
		"空は拒む":         {"", Version{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseVersion(c.in)
			assert.Equal(t, c.wantOK, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestVersion_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "v1.10.0", Version{1, 10, 0}.String())
}

func TestVersion_Less(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		a, b Version
		want bool
	}{
		"major で比べる":               {Version{1, 9, 9}, Version{2, 0, 0}, true},
		"minor は数で比べる（1.9 < 1.10）": {Version{1, 9, 0}, Version{1, 10, 0}, true},
		"patch で比べる":               {Version{1, 0, 1}, Version{1, 0, 0}, false},
		"同じ版は前でない":                 {Version{1, 0, 0}, Version{1, 0, 0}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, c.a.Less(c.b))
		})
	}
}

func TestBranches_LineName(t *testing.T) {
	t.Parallel()

	b := Branches{Lines: Lines{Release: "rel/", Hotfix: "hot/"}}
	t.Run("役割の接頭辞と版から名前を組む", func(t *testing.T) {
		t.Parallel()
		got, ok := b.LineName("hotfix", Version{1, 2, 3})
		require.True(t, ok)
		assert.Equal(t, "hot/v1.2.3", got)
	})
	t.Run("役割でなければ ok は false", func(t *testing.T) {
		t.Parallel()
		_, ok := b.LineName("feature", Version{1, 2, 3})
		assert.False(t, ok)
	})
	t.Run("接頭辞の無い役割は ok が false", func(t *testing.T) {
		t.Parallel()
		_, ok := Branches{Lines: Lines{Release: "rel/"}}.LineName("hotfix", Version{1, 2, 3})
		assert.False(t, ok)
	})
}

func TestBranches_ParseLine(t *testing.T) {
	t.Parallel()

	b := Branches{Lines: Lines{Release: "rel/", Hotfix: "hot/"}}
	for name, c := range map[string]struct {
		in       string
		wantRole string
		wantV    Version
		wantOK   bool
	}{
		"リリース線を読む":      {"rel/v1.2.3", "release", Version{1, 2, 3}, true},
		"hotfix の線を読む":  {"hot/v1.2.4", "hotfix", Version{1, 2, 4}, true},
		"接頭辞だけは線でない":    {"rel/", "", Version{}, false},
		"版の形でなければ線でない":  {"rel/next", "", Version{}, false},
		"別の接頭辞は線でない":    {"release/v1.2.3", "", Version{}, false},
		"入れ子の名前は線でない":   {"rel/x/v1.2.3", "", Version{}, false},
		"デプロイブランチは線でない": {"production", "", Version{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			role, v, ok := b.ParseLine(c.in)
			assert.Equal(t, c.wantOK, ok)
			assert.Equal(t, c.wantRole, role)
			assert.Equal(t, c.wantV, v)
		})
	}
}

func TestBranches_IsLine(t *testing.T) {
	t.Parallel()

	b := Branches{Lines: Lines{Release: "rel/", Hotfix: "hot/"}}
	for name, c := range map[string]struct {
		in   string
		want bool
	}{
		"リリース線":            {"rel/v1.0.0", true},
		"hotfix の線":        {"hot/v1.0.1", true},
		"版の形でなくても線":        {"rel/next", true},
		"接頭辞だけは線でない":       {"rel/", false},
		"途中に接頭辞を持つ名前は線でない": {"feature/rel/x", false},
		"区切りの無い同じ始まりは線でない": {"relx", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, b.IsLine(c.in))
		})
	}
}
