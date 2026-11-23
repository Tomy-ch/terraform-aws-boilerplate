package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	trivyCanaryJSON = `{"Results":[{"Target":"Dockerfile","Misconfigurations":[{"ID":"DS-0001","Severity":"MEDIUM"}]}]}`
	trivyMainJSON   = `{"Results":[{"Target":"main.tf"}]}`
)

func Test_runTrivyConfig(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, mainRC int) (*fake, toolFunc, string) {
		t.Helper()
		f, tool := newFake(t)
		f.scenario("trivy-config-canary", 1, trivyCanaryJSON, nil)
		f.scenario("default", mainRC, trivyMainJSON, nil)
		return f, tool, tree(t, map[string]string{".trivyignore.yaml": "misconfigurations: []\n"})
	}
	const sev = "CRITICAL,HIGH,MEDIUM"

	t.Run("canary が通れば本走査を起動し、本走査の出力だけを stdout へ流す", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0)
		var stdout, log bytes.Buffer
		require.NoError(t, runTrivyConfig([]string{"-base", base, "-severity", sev, "-format", "sarif"}, &stdout, &log, tool))
		assert.Equal(t, []string{
			"trivy-config-canary trivy-config/" + sev + "/json",
			filepath.Base(base) + " trivy-config/" + sev + "/sarif",
		}, f.calls())
		assert.Equal(t, trivyMainJSON, stdout.String())
		assert.Contains(t, log.String(), "trivy-stderr "+filepath.Base(base), "本走査の stderr は log へ流す")
		assert.NotContains(t, log.String(), "trivy-stderr trivy-config-canary", "canary の出力は通ったときに出さない")
		assert.NoDirExists(t, filepath.Join(base, "tmp", "trivy-config-canary"))
	})

	t.Run("json の本走査は canary と同じ段で起動する", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0)
		require.NoError(t, runTrivyConfig([]string{"-base", base, "-severity", sev, "-format", "json"}, &bytes.Buffer{}, &bytes.Buffer{}, tool))
		calls := f.calls()
		require.Len(t, calls, 2)
		assert.Equal(t, "trivy-config-canary trivy-config/"+sev+"/json", calls[0])
		assert.Equal(t, filepath.Base(base)+" trivy-config/"+sev+"/json", calls[1])
	})

	t.Run("-format の既定は table", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0)
		require.NoError(t, runTrivyConfig([]string{"-base", base, "-severity", sev}, &bytes.Buffer{}, &bytes.Buffer{}, tool))
		assert.Equal(t, filepath.Base(base)+" trivy-config/"+sev+"/table", f.calls()[1])
	})

	t.Run("canary に抑止の宣言を写す", func(t *testing.T) {
		t.Parallel()
		var seen string
		tool := func(d string, env []string, _ string) *exec.Cmd {
			if filepath.Base(d) == "trivy-config-canary" {
				raw, err := os.ReadFile(filepath.Join(d, ".trivyignore.yaml"))
				require.NoError(t, err)
				seen = string(raw)
				docker, err := os.ReadFile(filepath.Join(d, "Dockerfile"))
				require.NoError(t, err)
				assert.Equal(t, trivyCanary, string(docker))
			}
			c := exec.CommandContext(context.Background(), "sh", "-c", `echo '`+trivyCanaryJSON+`'; exit 1`)
			c.Dir, c.Env = d, env
			return c
		}
		base := tree(t, map[string]string{".trivyignore.yaml": "misconfigurations: [x]\n"})
		require.ErrorIs(t, runTrivyConfig([]string{"-base", base, "-severity", sev}, &bytes.Buffer{}, &bytes.Buffer{}, tool), errFindings)
		assert.Equal(t, "misconfigurations: [x]\n", seen)
	})

	t.Run("trivy に TRIVY_ で始まる環境変数を渡さない", func(t *testing.T) {
		t.Parallel()
		var envs [][]string
		tool := func(d string, env []string, _ string) *exec.Cmd {
			envs = append(envs, env)
			c := exec.CommandContext(context.Background(), "sh", "-c", `echo '`+trivyCanaryJSON+`'; exit 1`)
			c.Dir, c.Env = d, env
			return c
		}
		_, _, base := setup(t, 0)
		require.ErrorIs(t, runTrivyConfig([]string{"-base", base, "-severity", sev}, &bytes.Buffer{}, &bytes.Buffer{}, tool), errFindings)
		require.Len(t, envs, 2)
		assert.Equal(t, withoutPrefix(os.Environ(), "TRIVY_"), envs[0])
		assert.Equal(t, envs[0], envs[1])
	})

	t.Run("所見があれば errFindings で、出力は stdout へ流す", func(t *testing.T) {
		t.Parallel()
		_, tool, base := setup(t, 1)
		var stdout bytes.Buffer
		require.ErrorIs(t, runTrivyConfig([]string{"-base", base, "-severity", sev, "-format", "json"}, &stdout, &bytes.Buffer{}, tool), errFindings)
		assert.Equal(t, trivyMainJSON, stdout.String())
	})

	t.Run("canary が落ちなければ本走査を起動せず、stdout に何も書かずに errCanary", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0)
		f.scenario("trivy-config-canary", 0, trivyCanaryJSON, nil)
		var stdout, log bytes.Buffer
		require.ErrorIs(t, runTrivyConfig([]string{"-base", base, "-severity", sev, "-format", "json"}, &stdout, &log, tool), errCanary)
		assert.Len(t, f.calls(), 1)
		assert.Empty(t, stdout.String())
		assert.Contains(t, log.String(), "trivy-stderr trivy-config-canary", "落ちた canary の stderr を出す")
		assert.DirExists(t, filepath.Join(base, "tmp", "trivy-config-canary"))
	})

	t.Run("抑止の宣言が無ければ trivy を起動せずに errInput", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		require.ErrorIs(t, runTrivyConfig([]string{"-base", t.TempDir(), "-severity", sev}, &bytes.Buffer{}, &bytes.Buffer{}, tool), errInput)
		assert.Empty(t, f.calls())
	})

	t.Run("trivy を起動できなければ errStart", func(t *testing.T) {
		t.Parallel()
		_, _, base := setup(t, 0)
		tool := func(d string, env []string, _ string) *exec.Cmd {
			c := exec.CommandContext(context.Background(), filepath.Join(t.TempDir(), "no-such-trivy"))
			c.Dir, c.Env = d, env
			return c
		}
		require.ErrorIs(t, runTrivyConfig([]string{"-base", base, "-severity", sev}, &bytes.Buffer{}, &bytes.Buffer{}, tool), errStart)
	})

	for name, args := range map[string][]string{
		"-severity が無い":   {"-format", "json"},
		"一覧に無い -severity": {"-severity", "HIGH"},
		"一覧に無い -format":   {"-severity", sev, "-format", "cyclonedx"},
		"位置引数がある":         {"-severity", sev, "."},
		"知らない flag":       {"-severity", sev, "-exit-code", "0"},
	} {
		t.Run(name+"なら trivy を起動せずに errUsage", func(t *testing.T) {
			t.Parallel()
			f, tool, base := setup(t, 0)
			err := runTrivyConfig(append([]string{"-base", base}, args...), &bytes.Buffer{}, &bytes.Buffer{}, tool)
			require.ErrorIs(t, err, errUsage)
			assert.Empty(t, f.calls())
		})
	}
}

func Test_exitStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, exitStatus(nil))
	assert.Equal(t, 3, exitStatus(exec.CommandContext(context.Background(), "sh", "-c", "exit 3").Run()))
	assert.Equal(t, -1, exitStatus(exec.CommandContext(context.Background(), filepath.Join(t.TempDir(), "none")).Run()))
}

func Test_judgeTrivyCanary(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rc  int
		out string
		ok  bool
	}{
		"非0で DS-0001 を報告すれば通る": {1, trivyCanaryJSON, true},
		"0 で終われば落ちる":           {0, trivyCanaryJSON, false},
		"DS-0001 が無ければ落ちる":     {1, `{"Results":[{"Misconfigurations":[{"ID":"DS-0002"}]}]}`, false},
		"Results が無ければ落ちる":     {1, `{}`, false},
		"JSON でなければ落ちる":        {1, `FATAL error`, false},
		"空の出力なら落ちる":            {1, ``, false},
		"ID が別の欄にあるだけなら落ちる":    {1, `{"Results":[{"Secrets":[{"ID":"DS-0001"}]}]}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := judgeTrivyCanary(c.rc, []byte(c.out))
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errCanary)
		})
	}
}
