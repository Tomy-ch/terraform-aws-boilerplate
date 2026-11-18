package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lsRemoteOutput は、`git ls-remote --heads origin` の出力形式を模したものです。
const lsRemoteOutput = "9ce3aa244390610aafe2c2be564a757126c84e58\trefs/heads/release/v1.0.0\n" +
	"88f39914c09eee7330c854f49b7b5d053d692f2d\trefs/heads/release/v2.1.0\n" +
	"5741f714caa221bac8c3f4a315922934ed069864\trefs/heads/release/v2.2.0\n"

// stubList は、参照一覧の取得を固定の出力へ差し替えます。
func stubList(out string) func() (string, error) {
	return func() (string, error) { return out, nil }
}

// declBody は、リリースラインの接頭辞を release、hotfix の線の接頭辞を hotfix とする宣言を返します。
func declBody(release, hotfix string) string {
	return `{"deploy": [{"branch": "production", "release_base": true}], "lines": {"release": "` + release +
		`", "hotfix": "` + hotfix + `"}, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`
}

// repoRoot は、宣言 body を置いたリポジトリ直下を返す wd を作ります。
func repoRoot(t *testing.T, body string) func() (string, error) {
	t.Helper()

	root := t.TempDir()
	path := filepath.Join(root, branchdecl.File)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return func() (string, error) { return root, nil }
}

// decl は、接頭辞を release/ と hotfix/ とする宣言です。
var decl = branchdecl.Branches{Lines: branchdecl.Lines{Release: "release/", Hotfix: "hotfix/"}, LineVersion: branchdecl.VersionForm}

// gitEnv は、コミット者と日時を固定した git 実行用の環境変数を返します。
// 日時を引数で決められるようにしているのは、コミット日時とバージョン番号の
// 新旧が食い違う状態をフィクスチャとして作るためです。
func gitEnv(date string) []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_COMMITTER_DATE="+date,
	)
}

// runGit は、dir で git を実行します。失敗は出力ごとテストの失敗にします。
func runGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // 引数はテスト内で組み立てた固定値
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// remoteWithInvertedDates は、コミット日時とバージョン番号の新旧が逆になった
// リリースラインを持つリポジトリを作り、そのパスを返します。
//
// release/v1.10.0 が最新版でありながら最古のコミット、release/v1.9.0 が最古の版でありながら
// 最新のコミットになるので、「コミット日時で選ぶ」実装と「バージョン番号で選ぶ」実装が
// 別の答えを返します。文字列順で選ぶ実装もまた別の答え（v1.9.0）になります。
// 対象外の形式（feature / hotfix / release だが版でない）も一緒に置いて、選別も同時に見ます。
func remoteWithInvertedDates(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "remote")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	old, recent := gitEnv("2020-01-01T00:00:00+09:00"), gitEnv("2030-01-01T00:00:00+09:00")

	runGit(t, dir, old, "init", "--quiet", "--initial-branch=release/v1.10.0")
	runGit(t, dir, old, "commit", "--quiet", "--allow-empty", "-m", "old but newest version")
	runGit(t, dir, recent, "switch", "--quiet", "-c", "release/v1.9.0")
	runGit(t, dir, recent, "commit", "--quiet", "--allow-empty", "-m", "recent but oldest version")
	runGit(t, dir, recent, "switch", "--quiet", "-c", "feature/not-a-release")
	runGit(t, dir, recent, "switch", "--quiet", "-c", "hotfix/v1.11.0")
	runGit(t, dir, recent, "switch", "--quiet", "-c", "release/next")

	return dir
}

// checkoutOf は、remote を origin として持つだけの作業リポジトリを作り、そのパスを返します。
// staleHead が空でなければ、陳腐化した `refs/remotes/origin/HEAD` をそこに仕込みます。
func checkoutOf(t *testing.T, remote, staleHead string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "checkout")
	require.NoError(t, os.MkdirAll(dir, 0o750))

	env := gitEnv("2020-01-01T00:00:00+09:00")
	runGit(t, dir, env, "init", "--quiet", "--initial-branch=main")
	runGit(t, dir, env, "remote", "add", "origin", remote)

	if staleHead != "" {
		runGit(t, dir, env, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+staleHead)
	}

	return dir
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("最新のリリースラインのブランチ名だけを 1 行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			require.NoError(t, run(nil, repoRoot(t, declBody("release/", "hotfix/")), stubList(lsRemoteOutput), &out))
			assert.Equal(t, "release/v2.2.0\n", out.String())
		})

		t.Run("リリースラインの接頭辞は宣言から読む", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			listed := "a\trefs/heads/rel/v1.0.0\nb\trefs/heads/release/v9.0.0\nc\trefs/heads/hot/v9.9.9\n"

			require.NoError(t, run(nil, repoRoot(t, declBody("rel/", "hot/")), stubList(listed), &out))
			assert.Equal(t, "rel/v1.0.0\n", out.String())
		})

		t.Run("ヘルプ要求は失敗にしない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			require.NoError(t, run([]string{"-h"}, repoRoot(t, declBody("release/", "hotfix/")), stubList(lsRemoteOutput), &out))
			assert.Empty(t, out.String())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リリースラインが 1 本も無ければ、宣言の形を添えて失敗する", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := run(nil, repoRoot(t, declBody("release/", "hotfix/")), stubList(""), &out)

			require.ErrorIs(t, err, errNoReleaseBranch)
			assert.ErrorContains(t, err, "release/v<major>.<minor>.<patch>")
			assert.Empty(t, out.String())
		})

		t.Run("参照一覧を取得できなければ失敗する", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			failing := func() (string, error) { return "", errNoReleaseBranch }

			require.ErrorIs(t, run(nil, repoRoot(t, declBody("release/", "hotfix/")), failing, &out), errNoReleaseBranch)
			assert.Empty(t, out.String())
		})

		t.Run("宣言が無ければ git を呼ぶ前に失敗する", func(t *testing.T) {
			t.Parallel()

			var (
				out    bytes.Buffer
				called bool
			)

			list := func() (string, error) {
				called = true
				return lsRemoteOutput, nil
			}
			root := t.TempDir()

			err := run(nil, func() (string, error) { return root, nil }, list, &out)

			require.ErrorIs(t, err, os.ErrNotExist)
			assert.False(t, called)
			assert.Empty(t, out.String())
		})

		t.Run("宣言が壊れていれば git を呼ぶ前に失敗する", func(t *testing.T) {
			t.Parallel()

			var (
				out    bytes.Buffer
				called bool
			)

			list := func() (string, error) {
				called = true
				return lsRemoteOutput, nil
			}

			err := run(nil, repoRoot(t, `{"lines": ["release/"]}`), list, &out)

			require.ErrorIs(t, err, branchdecl.ErrDeclaration)
			assert.False(t, called)
		})

		t.Run("作業ディレクトリを得られなければ失敗する", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			wd := func() (string, error) { return "", os.ErrPermission }

			require.ErrorIs(t, run(nil, wd, stubList(lsRemoteOutput), &out), os.ErrPermission)
		})

		t.Run("余計な引数は黙って捨てずに失敗する", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := run([]string{"release/v1.0.0"}, repoRoot(t, declBody("release/", "hotfix/")), stubList(lsRemoteOutput), &out)

			require.ErrorIs(t, err, errUnexpectedArgs)
			assert.Empty(t, out.String())
		})

		t.Run("未知のフラグはヘルプ要求と混同せず失敗する", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			require.ErrorContains(t, run([]string{"-bogus"}, repoRoot(t, declBody("release/", "hotfix/")), stubList(lsRemoteOutput), &out), "failed to parse flags")
		})
	})
}

//nolint:paralleltest // t.Chdir を使うため並列化できない
func Test_lsRemoteHeads(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("コミット日時が逆順でもバージョン番号で最新を選ぶ", func(t *testing.T) {
			t.Chdir(checkoutOf(t, remoteWithInvertedDates(t), ""))

			refs, err := lsRemoteHeads()
			require.NoError(t, err)

			latest, err := latestRelease(decl, refs)
			require.NoError(t, err)
			assert.Equal(t, "release/v1.10.0", latest.name)
		})

		t.Run("origin/HEAD が古いままでも答えは変わらない", func(t *testing.T) {
			t.Chdir(checkoutOf(t, remoteWithInvertedDates(t), "release/v1.9.0"))

			refs, err := lsRemoteHeads()
			require.NoError(t, err)

			latest, err := latestRelease(decl, refs)
			require.NoError(t, err)
			assert.Equal(t, "release/v1.10.0", latest.name)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("origin を引けない場所では部分的な出力を返さず失敗する", func(t *testing.T) {
			t.Chdir(t.TempDir())

			_, err := lsRemoteHeads()

			require.ErrorContains(t, err, "git ls-remote")
		})
	})
}

func Test_latestRelease(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("major / minor / patch を数値で比べて最新を選ぶ", func(t *testing.T) {
			t.Parallel()

			got, err := latestRelease(decl, lsRemoteOutput)

			require.NoError(t, err)
			assert.Equal(t, "release/v2.2.0", got.name)
		})

		t.Run("文字列順では前に並ぶ二桁の版を最新と判定する", func(t *testing.T) {
			t.Parallel()

			out := "a\trefs/heads/release/v1.9.0\nb\trefs/heads/release/v1.10.0\n"

			got, err := latestRelease(decl, out)

			require.NoError(t, err)
			assert.Equal(t, "release/v1.10.0", got.name)
		})

		t.Run("並び順が逆でも同じ答えを返す", func(t *testing.T) {
			t.Parallel()

			out := "a\trefs/heads/release/v1.10.0\nb\trefs/heads/release/v1.9.0\n"

			got, err := latestRelease(decl, out)

			require.NoError(t, err)
			assert.Equal(t, "release/v1.10.0", got.name)
		})

		t.Run("リリースライン以外の行は最新の判定に混ぜない", func(t *testing.T) {
			t.Parallel()

			out := "a\trefs/heads/hotfix/v9.9.9\nb\trefs/heads/release/next\nc\trefs/heads/release/v1.0.0\n"

			got, err := latestRelease(decl, out)

			require.NoError(t, err)
			assert.Equal(t, "release/v1.0.0", got.name)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("出力が空なら空文字を返さず失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := latestRelease(decl, "")

			require.ErrorIs(t, err, errNoReleaseBranch)
		})

		t.Run("解釈できる行が 1 つも無ければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := latestRelease(decl, "a\trefs/heads/feature/x\nb\trefs/heads/production\n")

			require.ErrorIs(t, err, errNoReleaseBranch)
		})
	})
}

func Test_parseLine(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("参照からブランチ名とバージョンを取り出す", func(t *testing.T) {
			t.Parallel()

			got, ok := parseLine(decl, "5741f714\trefs/heads/release/v2.10.3\n")

			require.True(t, ok)
			assert.Equal(t, releaseLine{name: "release/v2.10.3", version: branchdecl.Version{Major: 2, Minor: 10, Patch: 3}}, got)
		})

		t.Run("名前は組み直さず origin が返したものを持つ", func(t *testing.T) {
			t.Parallel()

			got, ok := parseLine(decl, "5741f714\trefs/heads/release/v02.0.0\n")

			require.True(t, ok)
			assert.Equal(t, "release/v02.0.0", got.name)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("タブ区切りでない行は対象外にする", func(t *testing.T) {
			t.Parallel()

			_, ok := parseLine(decl, "refs/heads/release/v1.0.0")

			assert.False(t, ok)
		})

		t.Run("refs/heads 配下でない参照は対象外にする", func(t *testing.T) {
			t.Parallel()

			_, ok := parseLine(decl, "5741f714\trefs/tags/release/v1.0.0")

			assert.False(t, ok)
		})

		t.Run("release 配下でもバージョン形式でなければ対象外にする", func(t *testing.T) {
			t.Parallel()

			_, ok := parseLine(decl, "5741f714\trefs/heads/release/next")

			assert.False(t, ok)
		})

		t.Run("接頭辞の違うブランチは対象外にする", func(t *testing.T) {
			t.Parallel()

			_, ok := parseLine(decl, "5741f714\trefs/heads/hotfix/v1.0.1")

			assert.False(t, ok)
		})

		t.Run("空行は対象外にする", func(t *testing.T) {
			t.Parallel()

			_, ok := parseLine(decl, "")

			assert.False(t, ok)
		})

		t.Run("int に収まらない桁のバージョンは対象外にする", func(t *testing.T) {
			t.Parallel()

			_, ok := parseLine(decl, "5741f714\trefs/heads/release/v99999999999999999999.0.0")

			assert.False(t, ok)
		})
	})
}
