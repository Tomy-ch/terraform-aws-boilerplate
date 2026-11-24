package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, &bytes.Buffer{}, runner{tool: command}), errUsage)
	})

	t.Run("知らないサブコマンドは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"tfsec"}, &bytes.Buffer{}, &bytes.Buffer{}, runner{tool: command}), errUsage)
	})

	t.Run("各サブコマンドへ渡し、flag の誤りを stdout へ書かない", func(t *testing.T) {
		t.Parallel()
		for _, sub := range []string{"trivy-config", "checkov", "kics", "semgrep"} {
			t.Run(sub, func(t *testing.T) {
				t.Parallel()
				var stdout, stderr bytes.Buffer
				err := run([]string{sub, "-no-such-flag"}, &stdout, &stderr, runner{tool: command})
				require.ErrorIs(t, err, errUsage)
				assert.Contains(t, stderr.String(), "-no-such-flag")
				assert.Contains(t, stderr.String(), "Usage of iac-scan "+sub+":", "そのサブコマンドの flag の解釈に届いている")
				assert.Empty(t, stdout.String())
			})
		}
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("エラーが無ければ何も書かずに 0", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 0, report(nil, &out))
		assert.Empty(t, out.String())
	})

	t.Run("エラーを1行で書き、終了コードを返す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 1, report(xerrors.Wrap(errFindings, "checkov"), &out))
		assert.Equal(t, "❌ iac-scan: checkov: 所見があります\n", out.String())
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"成功は 0":           {nil, 0},
		"所見は 1":           {errFindings, 1},
		"canary の失敗は 1":   {errCanary, 1},
		"件数のずれは 1":        {errCoverage, 1},
		"結果を取り出せないのは 1":   {errOutput, 1},
		"使い方の誤りは 2":       {errUsage, 2},
		"root が無いのは 2":    {errMissingRoot, 2},
		"0件は 2":           {errNoTargets, 2},
		"入力を読めないのは 2":     {errInput, 2},
		"イメージを決められないのは 2": {errImage, 2},
		"docker の失敗は 2":   {errDocker, 2},
		"道具を起動できないのは 2":   {errTool, 2},
		"結果を読めないのは 2":     {errResult, 2},
		"包んでも区分は変わらない":    {xerrors.Wrap(errCanary, "x"), 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}
