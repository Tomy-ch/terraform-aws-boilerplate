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
		b, err := Read(writeFile(t, `{"deploy": ["production"], "lines": ["release/"], "revert": "revert/"}`))
		require.NoError(t, err)
		assert.Equal(t, Branches{Deploy: []string{"production"}, Lines: []string{"release/"}, Revert: "revert/"}, b)
	})
	for name, c := range map[string]struct {
		body string
		want error
	}{
		"解釈できなければ失敗する":       {`{"deploy": "production"}`, ErrDeclaration},
		"規則に反すれば失敗する":        {`{"deploy": [], "lines": [], "revert": "revert/"}`, ErrEmpty},
		"null は0件として拒む":      {`null`, ErrEmpty},
		"配列なら拒む":             {`["production"]`, ErrDeclaration},
		"revert が無ければ拒む":     {`{"deploy": ["production"], "lines": ["release/"]}`, ErrDeclaration},
		"revert の字面が違えば拒む":   {`{"lines": ["release/"], "Revert": "revert/"}`, ErrDeclaration},
		"revert が重複すれば拒む":    {`{"lines": ["release/"], "revert": "a/", "revert": "b/"}`, ErrDeclaration},
		"environment のキーは拒む": {`{"lines": ["release/"], "revert": "revert/", "name": "x"}`, ErrDeclaration},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(writeFile(t, c.body))
			require.ErrorIs(t, err, c.want)
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      Branches
		wantErr error
	}{
		{"固定名と接頭辞を持つ宣言は通る", Branches{Deploy: []string{"production", "develop"}, Lines: []string{"release/"}, Revert: "revert/"}, nil},
		{"接頭辞だけの宣言も通る", Branches{Lines: []string{"release/"}, Revert: "revert/"}, nil},
		{"0件は拒む", Branches{Revert: "revert/"}, ErrEmpty},
		{"glob の文字を含む固定名は拒む", Branches{Deploy: []string{"release/*"}, Revert: "revert/"}, ErrDeclaration},
		{"大文字の固定名は拒む", Branches{Deploy: []string{"Main"}, Revert: "revert/"}, ErrDeclaration},
		{"固定名の重複は拒む", Branches{Deploy: []string{"develop", "develop"}, Revert: "revert/"}, ErrDeclaration},
		{"`/` で終わらない接頭辞は拒む", Branches{Lines: []string{"release"}, Revert: "revert/"}, ErrDeclaration},
		{"glob の文字を含む接頭辞は拒む", Branches{Lines: []string{"release/*/"}, Revert: "revert/"}, ErrDeclaration},
		{"接頭辞の重複は拒む", Branches{Lines: []string{"release/", "release/"}, Revert: "revert/"}, ErrDeclaration},
		{"空文字列の固定名は拒む", Branches{Deploy: []string{""}, Revert: "revert/"}, ErrDeclaration},
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
		{"正しい JSON は読める", `{"deploy": ["a"]}`, nil},
		{"未知のキーは拒む", `{"deploy": ["a"], "extra": 1}`, ErrDeclaration},
		{"値の後の余分なトークンは拒む", `{"deploy": ["a"]} {}`, ErrDeclaration},
		{"JSON でないものは拒む", `deploy: a`, ErrDeclaration},
		{"空のファイルは拒む", ``, ErrDeclaration},
		{"キーの重複は拒む", `{"deploy": ["b"], "deploy": ["a"]}`, ErrDeclaration},
		{"字面の違うキーは拒む", `{"Deploy": ["a"]}`, ErrDeclaration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var b Branches
			err := DecodeStrict(writeFile(t, tc.body), &b, branchKeys)
			if tc.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, []string{"a"}, b.Deploy)
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
