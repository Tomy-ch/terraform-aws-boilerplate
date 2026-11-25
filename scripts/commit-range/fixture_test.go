package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// repo は t.TempDir に建てた git のリポジトリです。
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.commit("base")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.CommandContext(r.t.Context(), "git", append([]string{
		"-C", r.dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null",
	}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, string(out))
	return strings.TrimSpace(string(out))
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("commit", "-q", "--allow-empty", "-m", msg)
}

func noEnv() ([]string, error) { return nil, nil }
