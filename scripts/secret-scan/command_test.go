package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_command(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("0123456789", 4)
	trufflehogFlags := []string{"--no-verification", "--no-update", "--fail", "--fail-on-scan-errors", "--no-ignore-tag", "--results=verified,unverified,unknown"}
	gitleaksFlags := []string{"git", "--redact", "--no-banner", "--ignore-gitleaks-allow", "--config", ".gitleaks.toml", "--gitleaks-ignore-path", ".", "--report-format", "json", "--report-path", "-"}
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	identity := []string{"-c", "user.name=canary", "-c", "user.email=canary@example.invalid", "-c", "commit.gpgsign=false"}

	for step, want := range map[string][]string{
		stepResolve:      {"git", "-c", "core.hooksPath=/dev/null", "cat-file", "--batch-check=%(objectname) %(objecttype)"},
		stepNumstatRange: {"git", "-c", "core.hooksPath=/dev/null", "log", "--no-merges", "-M", "--diff-filter=d", "--format=commit %H", "--numstat", "-z", sha + "..HEAD"},
		stepNumstatHead:  {"git", "-c", "core.hooksPath=/dev/null", "log", "--no-merges", "-M", "--diff-filter=d", "--format=commit %H", "--numstat", "-z", "HEAD"},
		stepLsFiles:      {"git", "-c", "core.hooksPath=/dev/null", "-c", "core.quotepath=false", "ls-files", "-z", "-co", "--exclude-standard"},
		stepInit:         {"git", "-c", "core.hooksPath=/dev/null", "init", "-q"},
		stepCommitBase:   join([]string{"git", "-c", "core.hooksPath=/dev/null"}, identity, []string{"commit", "-q", "--allow-empty", "-m", "base"}),
		stepAddCanary:    {"git", "-c", "core.hooksPath=/dev/null", "add", "--", "credentials"},
		stepCommitCanary: join([]string{"git", "-c", "core.hooksPath=/dev/null"}, identity, []string{"commit", "-q", "-m", "canary"}),

		stepGitleaksRange:   join([]string{"gitleaks"}, gitleaksFlags, []string{"--log-opts=" + sha + "..HEAD", "."}),
		stepGitleaksHistory: join([]string{"gitleaks"}, gitleaksFlags, []string{"."}),

		stepTrufflehogRange: join([]string{"trufflehog", "git", "file://.", "--since-commit", sha, "--branch", "HEAD"}, trufflehogFlags,
			[]string{"--exclude-paths=tmp/secret-scan/trufflehog-range/exclude-paths.txt"}),
		stepTrufflehogRangeJSON: join([]string{"trufflehog", "git", "file://.", "--since-commit", sha, "--branch", "HEAD"}, trufflehogFlags,
			[]string{"--exclude-paths=tmp/secret-scan/trufflehog-range/exclude-paths.txt", "--json"}),
		stepTrufflehogHistory:        join([]string{"trufflehog", "git", "file://."}, trufflehogFlags, []string{"--exclude-paths=tmp/secret-scan/trufflehog-history/exclude-paths.txt"}),
		stepTrufflehogHistoryJSON:    join([]string{"trufflehog", "git", "file://."}, trufflehogFlags, []string{"--exclude-paths=tmp/secret-scan/trufflehog-history/exclude-paths.txt", "--json"}),
		stepTrufflehogFilesystem:     join([]string{"trufflehog", "filesystem", "."}, trufflehogFlags),
		stepTrufflehogFilesystemJSON: join([]string{"trufflehog", "filesystem", "."}, trufflehogFlags, []string{"--json"}),

		stepTrivyCanary: {"trivy", "fs", "--scanners", "secret", "--exit-code", "0", "--skip-version-check", "--quiet", "--format", "json", "."},
		stepTrivyTable:  {"trivy", "fs", "--scanners", "secret", "--exit-code", "1", "--ignorefile", ".trivyignore.yaml", "--skip-version-check", "--format", "table", "."},
		stepTrivyJSON:   {"trivy", "fs", "--scanners", "secret", "--exit-code", "1", "--ignorefile", ".trivyignore.yaml", "--skip-version-check", "--format", "json", "."},
		stepTrivySARIF:  {"trivy", "fs", "--scanners", "secret", "--exit-code", "1", "--ignorefile", ".trivyignore.yaml", "--skip-version-check", "--format", "sarif", "."},
	} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			cmd := command("/d", []string{"A=1"}, step, sha)
			assert.Equal(t, want, cmd.Args)
			assert.Equal(t, "/d", cmd.Dir)
			assert.Equal(t, []string{"A=1"}, cmd.Env)
		})
	}

	for _, step := range []string{stepNumstatRange, stepGitleaksRange, stepTrufflehogRange, stepTrufflehogRangeJSON} {
		for name, rev := range map[string]string{"空": "", "ref の名前": "HEAD", "短い sha": sha[:8], "選択肢の形": "--all", "大文字": strings.ToUpper(strings.Repeat("abcdef0123", 4))} {
			t.Run(step+": rev が "+name+"なら起動せずに落ちる", func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, []string{"false"}, command("/d", nil, step, rev).Args)
			})
		}
	}

	t.Run("SHA-256 の sha を受ける", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("a", 64)
		assert.Contains(t, command("/d", nil, stepGitleaksRange, long).Args, "--log-opts="+long+"..HEAD")
	})

	t.Run("知らない段は起動せずに落ちる", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"false"}, command("/d", nil, "trivy-cyclonedx", "").Args)
	})
}
