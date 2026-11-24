package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_runTrivy(t *testing.T) {
	t.Parallel()

	for format, step := range map[string]string{"table": stepTrivyTable, "json": stepTrivyJSON, "sarif": stepTrivySARIF} {
		t.Run(format+": canary の後に、canary の置き場を消してから root で本走査し、stdout には本走査だけを出す", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			s := newScanner(t)
			s.passingCanaries()
			s.set(step, "out", "MAIN\n")
			var stdout, log bytes.Buffer
			r := runner{tool: s.tool(), environ: append(os.Environ(), "TRIVY_PROBE=set"), root: root}
			require.NoError(t, runTrivy([]string{"-format", format}, &stdout, &log, r))
			assert.Equal(t, "MAIN\n", stdout.String())
			assert.Equal(t, []string{"canary " + stepTrivyCanary, filepath.Base(root) + " " + step}, s.steps())
			assert.Equal(t, "no\n", s.read("trivy-work-"+step), "本走査の時点で canary の置き場が残っている")
			assert.Equal(t, "\n", s.read("trivy-env-"+stepTrivyCanary), "TRIVY_ で始まる変数を渡した")
			assert.Equal(t, "\n", s.read("trivy-env-"+step), "TRIVY_ で始まる変数を渡した")
			assert.Contains(t, log.String(), "✅ trivy の canary")
		})
	}

	for name, c := range map[string]struct {
		rc   string
		want error
	}{
		"所見（1）は errFindings": {"1", errFindings},
		"2 は errScan":        {"2", errScan},
	} {
		t.Run("本走査: "+name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			s := newScanner(t)
			s.passingCanaries()
			s.set(stepTrivyTable, "rc", c.rc)
			require.ErrorIs(t, runTrivy(nil, &bytes.Buffer{}, &bytes.Buffer{}, runner{tool: s.tool(), root: root}), c.want)
		})
	}

	t.Run("canary が落ちれば、本走査を起動せず、stderr を log にだけ出す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		s := newScanner(t)
		s.behave(stepTrivyCanary, "canary", `{"Results":[]}`, "0")
		s.set(stepTrivyCanary, "err", "CANARY-ERR\n")
		var stdout, log bytes.Buffer
		require.ErrorIs(t, runTrivy(nil, &stdout, &log, runner{tool: s.tool(), root: root}), errCanary)
		assert.Len(t, s.calls(), 1)
		assert.Empty(t, stdout.String())
		assert.Contains(t, log.String(), "CANARY-ERR")
		noWork(t, root, "trivy")
	})

	for name, args := range map[string][]string{"受けない形": {"-format", "cyclonedx"}, "位置引数": {"."}, "知らない flag": {"-x"}} {
		t.Run(name+"は errUsage で、走査器を起動しない", func(t *testing.T) {
			t.Parallel()
			s := newScanner(t)
			require.ErrorIs(t, runTrivy(args, &bytes.Buffer{}, &bytes.Buffer{}, runner{tool: s.tool(), root: t.TempDir()}), errUsage)
			assert.Nil(t, s.calls())
		})
	}
}

func Test_trivyCanary(t *testing.T) {
	t.Parallel()

	t.Run("偽の token を canary の置き場に置いて、canary の段を起動する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		s := newScanner(t)
		s.passingCanaries()
		require.NoError(t, trivyCanary(runner{tool: s.tool(), root: root}, nil, &bytes.Buffer{}))
		assert.Equal(t, []string{"canary " + stepTrivyCanary}, s.steps())
		noWork(t, root, "trivy")
	})

	t.Run("シグナルで終われば ErrTool", func(t *testing.T) {
		t.Parallel()
		s := newScanner(t)
		s.set(stepTrivyCanary, "kill", "")
		require.ErrorIs(t, trivyCanary(runner{tool: s.tool(), root: t.TempDir()}, nil, &bytes.Buffer{}), errTool)
	})
}

func Test_judgeTrivyCanary(t *testing.T) {
	t.Parallel()

	require.NoError(t, judgeTrivyCanary(0, []byte(trivyHit)))

	for name, c := range map[string]struct {
		rc  int
		out string
	}{
		"異常終了":           {1, trivyHit},
		"JSON でない":       {0, "x"},
		"空":              {0, ""},
		"Results が無い":    {0, `{}`},
		"github-pat が無い": {0, `{"Results":[{"Secrets":[{"RuleID":"aws-access-key-id"}]}]}`},
		"Secrets が無い":    {0, `{"Results":[{"Target":"canary.txt"}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, judgeTrivyCanary(c.rc, []byte(c.out)), errCanary)
		})
	}
}

func Test_withoutPrefix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"PATH=/bin", "XTRIVY_A=1"}, withoutPrefix([]string{"TRIVY_A=1", "PATH=/bin", "XTRIVY_A=1", "TRIVY_=2"}, "TRIVY_"))
	assert.Empty(t, withoutPrefix(nil, "TRIVY_"))
}
