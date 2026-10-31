package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
)

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドごとに、そのサブコマンドに固有の失敗へ届く", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			args []string
			want error
		}{
			"サブコマンドが無ければ使い方のエラー":       {args: nil, want: errUsage},
			"未知のサブコマンドは使い方のエラー":        {args: []string{"lint"}, want: errUsage},
			"decl は宣言を読みに行く":           {args: []string{"decl", "-decl", "d.json"}, want: os.ErrNotExist},
			"count は走査の root を見に行く":    {args: []string{"count", "-results", "r", "-combined", "c", "modules"}, want: errMissingRoot},
			"rego-tests は Rego を読みに行く": {args: []string{"rego-tests", "-policy", "p", "-report", "r", "-namespaces", "n"}, want: os.ErrNotExist},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				err := run(tt.args, t.TempDir(), &bytes.Buffer{})
				require.ErrorIs(t, err, tt.want)
			})
		}
	})

	t.Run("-repo で渡した root を読む", func(t *testing.T) {
		t.Parallel()
		repo := buildRepo(t)
		var out bytes.Buffer
		require.NoError(t, run([]string{"decl", "-repo", repo, "-decl", testDeclPath}, t.TempDir(), &out))
		assert.Contains(t, out.String(), "許可リスト 2 項目")
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("違反が無ければ何も書かない", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, report(&out, "decl", nil))
		assert.Empty(t, out.String())
	})

	t.Run("違反をファイル順に書き、エラーを返す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := report(&out, "decl", []lintreport.Finding{{File: "b", Line: 1, Message: "y"}, {File: "a", Line: 2, Message: "x"}})
		require.ErrorIs(t, err, errViolations)
		assert.Equal(t, "❌ infra-code-gate decl: 2 件の違反\n\n  a\n    :2  x\n\n  b\n    :1  y\n", out.String())
	})
}
