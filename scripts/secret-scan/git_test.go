package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitRunner() runner {
	return runner{git: command}
}

func Test_gitOutput(t *testing.T) {
	t.Parallel()

	t.Run("stdin を渡して stdout を返す", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t)
		out, err := gitOutput(gitRunner(), os.Environ(), f.dir, stepResolve, "", "HEAD\n")
		require.NoError(t, err)
		assert.Equal(t, f.head()+" commit\n", out)
	})

	t.Run("0 以外で終われば errInput", func(t *testing.T) {
		t.Parallel()
		_, err := gitOutput(gitRunner(), os.Environ(), t.TempDir(), stepLsFiles, "", "")
		require.ErrorIs(t, err, errInput)
	})

	t.Run("起動できなければ ErrTool", func(t *testing.T) {
		t.Parallel()
		_, err := gitOutput(runner{git: newScanner(t).tool()}, []string{"PATH=/nonexistent"}, "/nonexistent", stepResolve, "", "")
		require.ErrorIs(t, err, errTool)
	})
}

func Test_resolveFrom(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	base := f.head()
	f.git("tag", "v1")
	f.git("tag", "-a", "-m", "annotated", "v2")
	f.write("a", "a\n")
	f.commit("a")
	tree := f.git("rev-parse", "HEAD^{tree}")
	blob := f.git("rev-parse", "HEAD:a")

	for from, want := range map[string]string{
		"HEAD":   f.head(),
		"HEAD~1": base,
		"v1":     base,
		"v2":     base,
		base:     base,
		base[:8]: base,
	} {
		t.Run("解決する: "+from, func(t *testing.T) {
			t.Parallel()
			got, err := resolveFrom(gitRunner(), os.Environ(), f.dir, from)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}

	t.Run("空は errFromEmpty で、git を起動しない", func(t *testing.T) {
		t.Parallel()
		_, err := resolveFrom(runner{git: newScanner(t).tool()}, nil, "/nonexistent", "")
		require.ErrorIs(t, err, errFromEmpty)
	})

	for name, from := range map[string]string{
		"在らない ref":      "origin/does-not-exist",
		"tree":          tree,
		"blob":          blob,
		"改行を含む":         "HEAD\nHEAD",
		"CR を含む":        "HEAD\r",
		"`-` で始まる":      "--all",
		"空白を含む":         "HEAD HEAD",
		"commit の後ろの空白": "HEAD ",
	} {
		t.Run(name+"は errFromUnresolved", func(t *testing.T) {
			t.Parallel()
			_, err := resolveFrom(gitRunner(), os.Environ(), f.dir, from)
			require.ErrorIs(t, err, errFromUnresolved)
			assert.Contains(t, err.Error(), "が解決できません")
		})
	}

	t.Run("リポジトリでなければ errInput", func(t *testing.T) {
		t.Parallel()
		_, err := resolveFrom(gitRunner(), os.Environ(), t.TempDir(), "HEAD")
		require.ErrorIs(t, err, errInput)
	})
}

func Test_numstat(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	from := f.head()
	f.write("a", "a\n")
	f.commit("a")

	t.Run("範囲", func(t *testing.T) {
		t.Parallel()
		out, err := numstat(gitRunner(), os.Environ(), f.dir, from)
		require.NoError(t, err)
		must, maybe, err := expected(out)
		require.NoError(t, err)
		assert.Equal(t, [2]int{1, 0}, [2]int{must, maybe})
	})

	t.Run("全履歴", func(t *testing.T) {
		t.Parallel()
		out, err := numstat(gitRunner(), os.Environ(), f.dir, "")
		require.NoError(t, err)
		must, _, err := expected(out)
		require.NoError(t, err)
		assert.Equal(t, 2, must)
	})

	t.Run("sha でない rev は git を起動せずに落ちる", func(t *testing.T) {
		t.Parallel()
		_, err := numstat(gitRunner(), os.Environ(), f.dir, "HEAD~1")
		require.ErrorIs(t, err, errInput)
	})
}

func Test_buildCanaryRepo(t *testing.T) {
	t.Parallel()

	t.Run("空の base と、偽の資格情報を足す commit を積み、base の sha を返す", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "canary")
		from, err := buildCanaryRepo(gitRunner(), os.Environ(), dir, "CREDS\n")
		require.NoError(t, err)
		f := &fixture{t: t, dir: dir}
		assert.Equal(t, from, f.git("rev-parse", "HEAD~1"))
		assert.Equal(t, canaryCredentials, f.git("diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"))
		assert.Empty(t, f.git("diff-tree", "--no-commit-id", "--name-only", "-r", "--root", "HEAD~1"), "base は空の commit")
		assert.Equal(t, "CREDS", f.git("show", "HEAD:"+canaryCredentials))
	})

	t.Run("作れない場所なら errInput", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		_, err := buildCanaryRepo(gitRunner(), os.Environ(), filepath.Join(file, "canary"), "")
		require.ErrorIs(t, err, errInput)
	})
}

func Test_fakeAWSCredentials(t *testing.T) {
	t.Parallel()

	a, b := fakeAWSCredentials(), fakeAWSCredentials()
	re := `^\[default\]\naws_access_key_id = AK` + `IA[A-Z2-7]{16}\naws_secret_access_key = [A-Za-z0-9/+]{40}\n$`
	assert.Regexp(t, re, a)
	assert.NotEqual(t, a, b, "実行ごとに乱数で作る")
}

func Test_fakeGitHubToken(t *testing.T) {
	t.Parallel()

	a, b := fakeGitHubToken(), fakeGitHubToken()
	assert.Regexp(t, `^token = "gh`+`p_[A-Za-z0-9]{36}"\n$`, a)
	assert.NotEqual(t, a, b)
}

func Test_randomString(t *testing.T) {
	t.Parallel()

	got := randomString("ab", 64)
	assert.Len(t, got, 64)
	assert.Regexp(t, `^[ab]+$`, got)
	assert.Contains(t, got, "a")
	assert.Contains(t, got, "b")
}
