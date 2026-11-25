package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_trivyStep(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		severity, format, scan, convert string
	}{
		"json は変換しない": {"CRITICAL,HIGH,MEDIUM", "json", "trivy-config/CRITICAL,HIGH,MEDIUM", ""},
		"sarif は変換する": {"CRITICAL,HIGH,MEDIUM,LOW", "sarif", "trivy-config/CRITICAL,HIGH,MEDIUM,LOW", "trivy-convert/sarif"},
		"table は変換する": {"CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN", "table", "trivy-config/CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN", "trivy-convert/table"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			scan, convert, err := trivyStep(c.severity, c.format)
			require.NoError(t, err)
			assert.Equal(t, c.scan, scan)
			assert.Equal(t, c.convert, convert)
		})
	}

	for name, c := range map[string][2]string{
		"MEDIUM を含まない閾値": {"CRITICAL,HIGH", "json"},
		"CRITICAL だけの閾値": {"CRITICAL", "json"},
		"一覧に無い閾値":        {"HIGH,MEDIUM", "json"},
		"並びの違う閾値":        {"MEDIUM,HIGH,CRITICAL", "json"},
		"小文字の閾値":         {"critical,high,medium", "json"},
		"空の閾値":           {"", "json"},
		"一覧に無い出力の形":      {"CRITICAL,HIGH,MEDIUM", "cyclonedx"},
		"空の出力の形":         {"CRITICAL,HIGH,MEDIUM", ""},
		"引数を足そうとする値":     {"CRITICAL,HIGH,MEDIUM --exit-code 0", "json"},
	} {
		t.Run(name+"は errUsage", func(t *testing.T) {
			t.Parallel()
			_, _, err := trivyStep(c[0], c[1])
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_trivySeverities(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"CRITICAL,HIGH,MEDIUM", "CRITICAL,HIGH,MEDIUM,LOW", "CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN"}, trivySeverities)
	for _, sev := range trivySeverities {
		t.Run(sev+" は command がその閾値で trivy を起動する", func(t *testing.T) {
			t.Parallel()
			scan, _, err := trivyStep(sev, "json")
			require.NoError(t, err)
			args := command("", nil, scan).Args
			i := slices.Index(args, "--severity")
			require.GreaterOrEqual(t, i, 0, "command に %s の段が無い", scan)
			assert.Equal(t, sev, args[i+1])
		})
	}
}

func Test_command(t *testing.T) {
	t.Parallel()

	scan := func(sev string) []string {
		return []string{
			"trivy", "config", "--severity", sev, "--ignorefile", ".trivyignore.yaml", "--exit-code", "1",
			"--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".",
		}
	}
	want := map[string][]string{
		stepImageCheckov: {"docker", "compose", "config", "--images", "checkov_runner"},
		stepImageKics:    {"docker", "compose", "config", "--images", "kics_runner"},
		stepImageSemgrep: {"docker", "compose", "config", "--images", "semgrep_runner"},
		stepCreate:       {"docker", "compose", "create"},
		stepCopyIn:       {"docker", "compose", "cp", "-a", "-", "scan:/"},
		stepUp: {
			"docker", "compose", "up", "--no-recreate", "--no-build", "--pull", "never",
			"--no-log-prefix", "--exit-code-from", "scan", "scan",
		},
		stepCopyOut:                             {"docker", "compose", "cp", "scan:/tmp/out/.", "out"},
		stepDown:                                {"docker", "compose", "down"},
		"trivy-config/CRITICAL,HIGH,MEDIUM":     scan("CRITICAL,HIGH,MEDIUM"),
		"trivy-config/CRITICAL,HIGH,MEDIUM,LOW": scan("CRITICAL,HIGH,MEDIUM,LOW"),
		"trivy-config/CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN": scan("CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN"),
		"trivy-convert/table": {
			"trivy", "convert", "--format", "table", "--scanners", "misconfig", "--ignorefile", "", "result.json",
		},
		"trivy-convert/sarif":        {"trivy", "convert", "--format", "sarif", "--ignorefile", "", "result.json"},
		"trivy-config/CRITICAL,HIGH": {"false"},
		"trivy-convert/json":         {"false"},
		"no-such-step":               {"false"},
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
