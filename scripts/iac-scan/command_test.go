package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_trivyStep(t *testing.T) {
	t.Parallel()

	t.Run("一覧の値なら段の名前を返す", func(t *testing.T) {
		t.Parallel()
		got, err := trivyStep("CRITICAL,HIGH,MEDIUM", "sarif")
		require.NoError(t, err)
		assert.Equal(t, "trivy-config/CRITICAL,HIGH,MEDIUM/sarif", got)
	})

	for name, c := range map[string][2]string{
		"一覧に無い閾値":    {"HIGH,MEDIUM", "json"},
		"並びの違う閾値":    {"MEDIUM,HIGH,CRITICAL", "json"},
		"小文字の閾値":     {"critical", "json"},
		"空の閾値":       {"", "json"},
		"一覧に無い出力の形":  {"CRITICAL", "cyclonedx"},
		"空の出力の形":     {"CRITICAL", ""},
		"引数を足そうとする値": {"CRITICAL --exit-code 0", "json"},
	} {
		t.Run(name+"は errUsage", func(t *testing.T) {
			t.Parallel()
			_, err := trivyStep(c[0], c[1])
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_command(t *testing.T) {
	t.Parallel()

	want := map[string][]string{
		stepImageCheckov: {"docker", "compose", "config", "--images", "checkov_runner"},
		stepImageKics:    {"docker", "compose", "config", "--images", "kics_runner"},
		stepImageSemgrep: {"docker", "compose", "config", "--images", "semgrep_runner"},
		stepCreate:       {"docker", "compose", "create"},
		stepCopyIn:       {"docker", "compose", "cp", "work/.", "scan:/work"},
		stepUp: {
			"docker", "compose", "up", "--no-recreate", "--no-build", "--pull", "never",
			"--no-log-prefix", "--exit-code-from", "scan", "scan",
		},
		stepCopyOut:    {"docker", "compose", "cp", "scan:/tmp/out/.", "out"},
		stepDown:       {"docker", "compose", "down"},
		"no-such-step": {"false"},
	}
	for _, sev := range trivySeverities {
		for _, format := range trivyFormats {
			step, err := trivyStep(sev, format)
			require.NoError(t, err)
			want[step] = []string{
				"trivy", "config", "--severity", sev, "--ignorefile", ".trivyignore.yaml", "--exit-code", "1",
				"--skip-version-check", "--skip-dirs", "tmp", "--format", format, ".",
			}
		}
	}
	for step, args := range want {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			c := command("/x/d", []string{"A=1"}, step)
			assert.Equal(t, args, c.Args)
			assert.Equal(t, "/x/d", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}
}

func Test_withoutPrefix(t *testing.T) {
	t.Parallel()

	got := withoutPrefix([]string{"PATH=/bin", "TRIVY_SEVERITY=LOW", "TRIVY_=x", "MY_TRIVY_X=1", "trivy_x=1"}, "TRIVY_")
	assert.Equal(t, []string{"PATH=/bin", "MY_TRIVY_X=1", "trivy_x=1"}, got)
	assert.Empty(t, withoutPrefix(nil, "COMPOSE_"))
}
