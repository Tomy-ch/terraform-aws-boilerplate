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

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("宣言を読む", func(t *testing.T) {
		t.Parallel()
		b, err := Read(writeFile(t, `{"deploy": [{"branch": "admin", "env": "admin", "from": ["release/"]}, {"branch": "production"}], "lines": ["release/"], "revert": "revert/"}`))
		require.NoError(t, err)
		assert.Equal(t, Branches{
			Deploy: []DeployBranch{{Branch: "admin", Env: "admin", From: []string{"release/"}}, {Branch: "production"}},
			Lines:  []string{"release/"},
			Revert: "revert/",
		}, b)
	})
	for name, c := range map[string]struct {
		body string
		want error
	}{
		"解釈できなければ失敗する":       {`{"deploy": "production"}`, ErrDeclaration},
		"名前だけの deploy は拒む":   {`{"deploy": ["production"], "lines": ["release/"], "revert": "revert/"}`, ErrDeclaration},
		"entry の未知のキーは拒む":    {`{"deploy": [{"branch": "a", "to": "b"}], "revert": "revert/"}`, ErrDeclaration},
		"規則に反すれば失敗する":        {`{"deploy": [], "lines": [], "revert": "revert/"}`, ErrEmpty},
		"null は0件として拒む":      {`null`, ErrEmpty},
		"配列なら拒む":             {`["production"]`, ErrDeclaration},
		"revert が無ければ拒む":     {`{"deploy": [{"branch": "production"}], "lines": ["release/"]}`, ErrDeclaration},
		"revert の字面が違えば拒む":   {`{"lines": ["release/"], "Revert": "revert/"}`, ErrDeclaration},
		"revert が重複すれば拒む":    {`{"lines": ["release/"], "revert": "a/", "revert": "b/"}`, ErrDeclaration},
		"environment のキーは拒む": {`{"lines": ["release/"], "revert": "revert/", "name": "x"}`, ErrDeclaration},
		"最上位の env は拒む":       {`{"lines": ["release/"], "revert": "revert/", "env": "x"}`, ErrDeclaration},
		"最上位の from は拒む":      {`{"lines": ["release/"], "revert": "revert/", "from": ["release/"]}`, ErrDeclaration},
		"最上位の branch は拒む":    {`{"lines": ["release/"], "revert": "revert/", "branch": "x"}`, ErrDeclaration},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(writeFile(t, c.body))
			require.ErrorIs(t, err, c.want)
		})
	}
}

func deploys(names ...string) []DeployBranch {
	out := make([]DeployBranch, 0, len(names))
	for _, n := range names {
		out = append(out, DeployBranch{Branch: n})
	}
	return out
}

var (
	lines = []string{"release/", "hotfix/"}
	admin = DeployBranch{Branch: "admin", Env: "admin", From: []string{"release/"}}
)

func TestValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      Branches
		wantErr error
	}{
		{"固定名と接頭辞を持つ宣言は通る", Branches{Deploy: deploys("production", "develop"), Lines: []string{"release/"}, Revert: "revert/"}, nil},
		{"接頭辞だけの宣言も通る", Branches{Lines: []string{"release/"}, Revert: "revert/"}, nil},
		{"0件は拒む", Branches{Revert: "revert/"}, ErrEmpty},
		{"glob の文字を含む固定名は拒む", Branches{Deploy: deploys("release/*"), Revert: "revert/"}, ErrDeclaration},
		{"大文字の固定名は拒む", Branches{Deploy: deploys("Main"), Revert: "revert/"}, ErrDeclaration},
		{"固定名の重複は拒む", Branches{Deploy: deploys("develop", "develop"), Revert: "revert/"}, ErrDeclaration},
		{"固定名が接頭辞と同じなら拒む", Branches{Deploy: deploys("release/"), Lines: []string{"release/"}, Revert: "revert/"}, ErrDeclaration},
		{"env と from を持つデプロイブランチは通る", Branches{Deploy: []DeployBranch{admin}, Lines: lines, Revert: "revert/"}, nil},
		{"from を複数持つデプロイブランチは通る", Branches{Deploy: []DeployBranch{{Branch: "production", Env: "prd", From: lines}}, Lines: lines, Revert: "revert/"}, nil},
		{"env が同じデプロイブランチが2つなら拒む", Branches{Deploy: []DeployBranch{admin, {Branch: "admin2", Env: "admin", From: []string{"release/"}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"env を持ち from が無ければ拒む", Branches{Deploy: []DeployBranch{{Branch: "admin", Env: "admin"}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"env を持ち from が空なら拒む", Branches{Deploy: []DeployBranch{{Branch: "admin", Env: "admin", From: []string{}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"from が lines に無ければ拒む", Branches{Deploy: []DeployBranch{{Branch: "admin", Env: "admin", From: []string{"feature/"}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"from の重複は拒む", Branches{Deploy: []DeployBranch{{Branch: "admin", Env: "admin", From: []string{"release/", "release/"}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"env を持たず from を持てば拒む", Branches{Deploy: []DeployBranch{{Branch: "develop", From: []string{"release/"}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"env を持たず空の from を持てば拒む", Branches{Deploy: []DeployBranch{{Branch: "develop", From: []string{}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"大文字の env は拒む", Branches{Deploy: []DeployBranch{{Branch: "admin", Env: "Admin", From: []string{"release/"}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"glob の文字を含む env は拒む", Branches{Deploy: []DeployBranch{{Branch: "admin", Env: "ad*", From: []string{"release/"}}}, Lines: lines, Revert: "revert/"}, ErrDeclaration},
		{"`/` で終わらない接頭辞は拒む", Branches{Lines: []string{"release"}, Revert: "revert/"}, ErrDeclaration},
		{"glob の文字を含む接頭辞は拒む", Branches{Lines: []string{"release/*/"}, Revert: "revert/"}, ErrDeclaration},
		{"接頭辞の重複は拒む", Branches{Lines: []string{"release/", "release/"}, Revert: "revert/"}, ErrDeclaration},
		{"空文字列の固定名は拒む", Branches{Deploy: deploys(""), Revert: "revert/"}, ErrDeclaration},
		{"空文字列の接頭辞は拒む", Branches{Lines: []string{""}, Revert: "revert/"}, ErrDeclaration},
		{"revert の接頭辞が無ければ拒む", Branches{Lines: []string{"release/"}}, ErrDeclaration},
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
		"lines と重ならない接頭辞は通る":       {Branches{Lines: []string{"release/", "hotfix/"}, Revert: "revert/"}, nil},
		"数字で始まる接頭辞も通る":             {Branches{Lines: []string{"release/"}, Revert: "2fa/"}, nil},
		"空は拒む":                     {Branches{Lines: []string{"release/"}}, ErrDeclaration},
		"`/` で終わらない接頭辞は拒む":         {Branches{Revert: "revert"}, ErrDeclaration},
		"大文字の接頭辞は拒む":               {Branches{Revert: "Revert/"}, ErrDeclaration},
		"lines と同じ接頭辞は拒む":          {Branches{Lines: []string{"hotfix/"}, Revert: "hotfix/"}, ErrDeclaration},
		"中に / を持つ接頭辞は拒む（入れ子を作れない）": {Branches{Lines: []string{"release/"}, Revert: "release/x/"}, ErrDeclaration},
		"lines の接頭辞を下に持つ接頭辞は拒む":    {Branches{Lines: []string{"revert/x/"}, Revert: "revert/"}, ErrDeclaration},
		"`/` で区切らずに始まりが同じだけなら通る":   {Branches{Lines: []string{"release/"}, Revert: "releasex/"}, nil},
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
