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

	t.Run("サブコマンドが無いか知らなければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, command), errUsage)
		require.ErrorIs(t, run([]string{"canary"}, &bytes.Buffer{}, command), errUsage)
	})

	t.Run("check へ渡す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, run([]string{"check", "-no-such-flag"}, &out, command), errUsage)
		assert.Contains(t, out.String(), "-no-such-flag", "check の flag の解釈に届いている")
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("成功なら何も書かず 0", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 0, report(nil, &out))
		assert.Empty(t, out.String())
	})

	t.Run("エラーを1行で書き、終了コードを返す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 1, report(xerrors.Wrap(errFailed, "x"), &out))
		assert.Equal(t, "❌ gofmt-gate: x: 検査が落ちました\n", out.String())
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"成功は 0":                 {nil, 0},
		"canary の不成立は 1":        {xerrors.Wrap(errCanary, "x"), 1},
		"整形されていないファイルは 1":       {xerrors.Wrap(errFailed, "x"), 1},
		"使い方の誤りは 2":             {xerrors.Wrap(errUsage, "x"), 2},
		"読めない入力は 2":             {xerrors.Wrap(errRead, "x"), 2},
		".go が0件は 2":            {xerrors.Wrap(errNoInput, "x"), 2},
		"道具を起動できないのは 2":         {xerrors.Wrap(errTool, "x"), 2},
		"gofmt の 0 でない終了コードは 2": {xerrors.Wrap(errExit, "x"), 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}
