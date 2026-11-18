package main

import (
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/reporoot"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	tagListCall       = "git tag"
	fetchTagsCall     = "git fetch --tags origin"
	annotateTagCall   = "git tag -a v1.2.4 --cleanup=verbatim -F .github/release/v1.2.4.md"
	tagPushCall       = "git push origin v1.2.4"
	releaseCreateCall = "gh release create v1.2.4 --title v1.2.4 --notes-file .github/release/v1.2.4.md"
	branchCreateCall  = "git switch -c release/v1.3.0 origin/production"
	branchPushCall    = "git push origin release/v1.3.0"
	defaultBranchCall = "gh repo edit --default-branch release/v1.3.0"
)

var errFakeCommand = xerrors.New("fake command failed")

// fakeRunner は、git / gh を一切起動せず呼び出しの並びだけを控える実行器。
type fakeRunner struct {
	// calls は、run / output が呼ばれた順の「実行ファイル 引数...」表記。
	calls []string
	// outputs は、output が返す標準出力（キーは calls と同じ表記）。
	outputs map[string]string
	// failOn は、この表記の呼び出しだけを失敗させます（空なら常に成功）。
	failOn         string
	remoteBranches map[string]bool
	lsRemoteFails  bool
}

func (f *fakeRunner) runner() runner {
	return runner{run: f.run, output: f.output, remoteBranchExists: f.remoteBranchExists}
}

func (f *fakeRunner) run(s step) error {
	call := s.String()
	f.calls = append(f.calls, call)

	if call == f.failOn {
		return errFakeCommand
	}

	return nil
}

func (f *fakeRunner) output(name string, args ...string) (string, error) {
	call := step{name: name, args: args}.String()
	f.calls = append(f.calls, call)

	if call == f.failOn {
		return "", errFakeCommand
	}

	return f.outputs[call], nil
}

var errLsRemote = xerrors.New("ls-remote に失敗しました")

func (f *fakeRunner) remoteBranchExists(branch string) (bool, error) {
	if f.lsRemoteFails {
		return false, errLsRemote
	}

	return f.remoteBranches[branch], nil
}

var testDecl = branchdecl.Branches{
	Deploy:      []branchdecl.DeployBranch{{Branch: "production", ReleaseBase: true}, {Branch: "develop"}},
	Lines:       branchdecl.Lines{Release: "release/", Hotfix: "hotfix/"},
	LineVersion: branchdecl.VersionForm,
	Revert:      "revert/",
}

// otherDecl は、基点と線の接頭辞を testDecl と違う名前にした宣言です。手順が名前を宣言から読むことを見る。
var otherDecl = branchdecl.Branches{
	Deploy:      []branchdecl.DeployBranch{{Branch: "main", ReleaseBase: true}},
	Lines:       branchdecl.Lines{Release: "rel/", Hotfix: "fix/"},
	LineVersion: branchdecl.VersionForm,
	Revert:      "revert/",
}

func taggableRunner() *fakeRunner {
	return &fakeRunner{outputs: map[string]string{tagListCall: "v1.2.3\n"}}
}

// writeNote は、writeNoteFor を v1.2.4（taggableRunner の次のパッチ版）で呼ぶ。
func writeNote(t *testing.T) {
	t.Helper()
	writeNoteFor(t, "v1.2.4")
}

// writeNoteFor は、作業ディレクトリを一時ディレクトリへ移し、指定の版のリリースノートを置きます。
func writeNoteFor(t *testing.T, v string) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, releaseNoteDir), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, notePath(v)), []byte("note\n"), 0o600))
	t.Chdir(dir)
}

func commands(steps []step) []string {
	got := make([]string, 0, len(steps))
	for _, s := range steps {
		got = append(got, s.String())
	}

	return got
}

// gitIn は、一時リポジトリに対して git を実行します。ホストの設定に左右されないよう
// 著者と署名を固定します。
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	base := []string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}

	//nolint:gosec // 引数は本ファイル内のリテラルと t.TempDir のパス
	out, err := exec.CommandContext(t.Context(), "git", append(base, args...)...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// newRepo は、origin を自分自身へ向けた production ブランチ 1 本のリポジトリを用意します。
// origin がローカルパスなので、fetch も ls-remote も実際の GitHub には触れません。
func newRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "production")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o600))
	gitIn(t, dir, "add", "README.md")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	gitIn(t, dir, "remote", "add", "origin", dir)
	gitIn(t, dir, "fetch", "-q", "--tags", "origin")

	return dir
}

func Test_latestVersion(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("数値順で比較するため v1.10.0 は v1.9.0 より新しい", func(t *testing.T) {
			t.Parallel()
			v, ok := latestVersion("v1.9.0\nv1.10.0\nv1.2.0\n")
			require.True(t, ok)
			assert.Equal(t, "v1.10.0", v.String())
		})

		t.Run("リリースタグ以外の行は無視する", func(t *testing.T) {
			t.Parallel()
			v, ok := latestVersion("v0.1.0\nnightly\nv1.0.0-rc1\nv0.2.0\n")
			require.True(t, ok)
			assert.Equal(t, "v0.2.0", v.String())
		})

		t.Run("major が大きいものを優先する", func(t *testing.T) {
			t.Parallel()
			v, ok := latestVersion("v2.0.0\nv1.99.99\n")
			require.True(t, ok)
			assert.Equal(t, "v2.0.0", v.String())
		})

		// major と minor が並んだ場合の順序が決まらないと、起点を取り違えて既存タグへ
		// 打ち直すか、番号を飛ばした次バージョンになる。
		t.Run("major と minor が同じなら patch が大きいものを優先する", func(t *testing.T) {
			t.Parallel()
			v, ok := latestVersion("v1.2.3\nv1.2.10\nv1.2.4\n")
			require.True(t, ok)
			assert.Equal(t, "v1.2.10", v.String())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リリースタグが 1 つも無ければ見つからないと返す", func(t *testing.T) {
			t.Parallel()
			_, ok := latestVersion("nightly\nv1.0.0-rc1\n")
			assert.False(t, ok)
		})

		t.Run("出力が空でも見つからないと返す", func(t *testing.T) {
			t.Parallel()
			_, ok := latestVersion("")
			assert.False(t, ok)
		})
	})
}

func Test_bump(t *testing.T) {
	t.Parallel()

	base := version{Major: 1, Minor: 2, Patch: 3}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("patch は最下位だけを進める", func(t *testing.T) {
			t.Parallel()
			v, err := bump(base, "patch")
			require.NoError(t, err)
			assert.Equal(t, "v1.2.4", v.String())
		})

		t.Run("minor は patch を 0 へ戻す", func(t *testing.T) {
			t.Parallel()
			v, err := bump(base, "minor")
			require.NoError(t, err)
			assert.Equal(t, "v1.3.0", v.String())
		})

		t.Run("major は minor と patch を 0 へ戻す", func(t *testing.T) {
			t.Parallel()
			v, err := bump(base, "major")
			require.NoError(t, err)
			assert.Equal(t, "v2.0.0", v.String())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未知の粒度は拒否する", func(t *testing.T) {
			t.Parallel()
			_, err := bump(base, "hotfix")
			require.ErrorIs(t, err, errUnknownBump)
			assert.Contains(t, err.Error(), "unknown -bump")
		})
	})
}

func Test_syncReleaseBaseSteps(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("基点のブランチを origin の最新へ揃えてからタグを取り直す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{
				"git fetch origin production",
				"git switch production",
				"git reset --hard origin/production",
				"git fetch --tags origin",
			}, commands(syncReleaseBaseSteps("production")))
		})
	})
}

func Test_tagSteps(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("タグ作成 → push → GitHub Release の順に並べる", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{
				"git tag -a v1.2.4 --cleanup=verbatim -F .github/release/v1.2.4.md",
				"git push origin v1.2.4",
				"gh release create v1.2.4 --title v1.2.4 --notes-file .github/release/v1.2.4.md",
			}, commands(tagSteps("v1.2.4", notePath("v1.2.4"))))
		})

		t.Run("タグ本文と Release 本文は同じリリースノートを指す", func(t *testing.T) {
			t.Parallel()
			steps := tagSteps("v9.9.9", notePath("v9.9.9"))
			assert.Equal(t, ".github/release/v9.9.9.md", steps[0].args[len(steps[0].args)-1])
			assert.Equal(t, ".github/release/v9.9.9.md", steps[2].args[len(steps[2].args)-1])
		})

		t.Run("タグ本文はリリースノートを見出しまでそのまま持つ", func(t *testing.T) {
			t.Parallel()
			dir := newRepo(t)
			note := "# v1.2.4\n\n## 変更\n\n- 本文\n"
			require.NoError(t, os.MkdirAll(filepath.Join(dir, releaseNoteDir), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dir, notePath("v1.2.4")), []byte(note), 0o600))

			gitIn(t, dir, append([]string{"-c", "tag.gpgsign=false"}, tagSteps("v1.2.4", notePath("v1.2.4"))[0].args...)...)
			//nolint:gosec // 引数は本ファイル内のリテラルと t.TempDir のパス
			got, err := exec.CommandContext(t.Context(), "git", "-C", dir, "tag", "-l", "--format=%(contents)", "v1.2.4").Output()
			require.NoError(t, err)
			assert.Equal(t, note, strings.TrimSuffix(string(got), "\n"))
		})
	})
}

func Test_branchSteps(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("base から切って push した後にデフォルトブランチを変える", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{
				"git fetch origin production",
				"git switch -c release/v1.3.0 origin/production",
				"git push origin release/v1.3.0",
				"gh repo edit --default-branch release/v1.3.0",
			}, commands(branchSteps("production", "release/v1.3.0")))
		})

		t.Run("デフォルトブランチの切り替えは push の後に置く", func(t *testing.T) {
			t.Parallel()
			steps := branchSteps("production", "hotfix/v1.2.4")
			assert.Equal(t, "git", steps[2].name)
			assert.Equal(t, "gh", steps[3].name)
		})
	})
}

func Test_notePath(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リリースノートは .github/release 配下の <version>.md", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, ".github/release/v1.0.0.md", notePath("v1.0.0"))
		})
	})
}

func Test_step_String(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("実行ファイル名と引数を空白で繋ぐ", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "git push origin v1.0.0", step{name: "git", args: []string{"push", "origin", "v1.0.0"}}.String())
		})

		t.Run("引数を持たない手順でも実行ファイル名が分かる", func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, step{name: "gh"}.String(), "gh")
		})
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("成功したコマンドはエラーを返さない", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, run(step{name: "git", args: []string{"--version"}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// どの手順で落ちたかが分からないと、途中まで進んだ状態から手作業で復旧できない。
		t.Run("失敗したコマンドはどの手順かを含めて返す", func(t *testing.T) {
			t.Parallel()
			err := run(step{name: "git", args: []string{"no-such-subcommand"}})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed: git no-such-subcommand")
		})
	})
}

func Test_runner_runAll(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("与えた手順を順に実行する", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
			require.NoError(t, hostRunner().runAll([]step{
				{name: "touch", args: []string{first}},
				{name: "touch", args: []string{second}},
			}))
			assert.FileExists(t, first)
			assert.FileExists(t, second)
		})

		t.Run("手順が無ければ何もせず成功する", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, hostRunner().runAll(nil))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 手順にはタグの push や Release の作成が並ぶため、失敗を跨いで進むと取り消せない。
		t.Run("失敗した時点で以降の手順を実行しない", func(t *testing.T) {
			t.Parallel()
			skipped := filepath.Join(t.TempDir(), "skipped")
			err := hostRunner().runAll([]step{
				{name: "git", args: []string{"no-such-subcommand"}},
				{name: "touch", args: []string{skipped}},
			})
			require.Error(t, err)
			assert.NoFileExists(t, skipped)
		})
	})
}

//nolint:paralleltest // t.Chdir でプロセスの作業ディレクトリを変えるため並列化不可
func Test_hostRunner(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("run はホストのコマンドを実行する", func(t *testing.T) {
			require.NoError(t, hostRunner().run(step{name: "git", args: []string{"--version"}}))
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("output はホストのコマンドの標準出力を返す", func(t *testing.T) {
			got, err := hostRunner().output("echo", "v1.0.0")
			require.NoError(t, err)
			assert.Equal(t, "v1.0.0\n", got)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("remoteBranchExists はホストの git で origin のブランチを見る", func(t *testing.T) {
			t.Chdir(newRepo(t))

			got, err := hostRunner().remoteBranchExists("production")
			require.NoError(t, err)
			assert.True(t, got)

			missing, err := hostRunner().remoteBranchExists("release/v9.9.9")
			require.NoError(t, err)
			assert.False(t, missing)
		})
	})
}

func Test_output(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 出力はタグ一覧と作業ツリーの汚れ判定の入力になるため、加工せずに渡す。
		t.Run("標準出力をそのまま返す", func(t *testing.T) {
			t.Parallel()
			got, err := output("echo", "hello")
			require.NoError(t, err)
			assert.Equal(t, "hello\n", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("失敗したコマンドは引数まで含めて返す", func(t *testing.T) {
			t.Parallel()
			got, err := output("git", "no-such-subcommand")
			require.Error(t, err)
			assert.Empty(t, got)
			assert.Contains(t, err.Error(), "failed: git no-such-subcommand")
		})
	})
}

//nolint:paralleltest // t.Chdir でプロセスの作業ディレクトリを変えるため並列化不可
func Test_remoteBranchExists(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		// 既存ブランチを見落とすと、リリースブランチを取り違えたまま push まで進む。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("origin に同名ブランチがあれば真を返す", func(t *testing.T) {
			dir := newRepo(t)
			t.Chdir(dir)
			got, err := remoteBranchExists("production")
			require.NoError(t, err)
			assert.True(t, got)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("origin に無いブランチには偽を返す", func(t *testing.T) {
			dir := newRepo(t)
			t.Chdir(dir)
			got, err := remoteBranchExists("release/v9.9.9")
			require.NoError(t, err)
			assert.False(t, got)
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		// **exit code 2 だけが「無い」である。** origin を解決できないときの 128 を
		// 「無い」と読むと、衝突しているブランチへ向かって取り消しの効かない手順が走る。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("origin を解決できなければ偽ではなくエラーを返す", func(t *testing.T) {
			dir := newRepo(t)
			t.Chdir(dir)
			gitIn(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "no-such-repo"))

			got, err := remoteBranchExists("production")
			require.Error(t, err)
			assert.False(t, got)
		})
	})
}

func Test_listTags(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("タグを取り直してから一覧を返す", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			out, err := listTags(f.runner())
			require.NoError(t, err)
			assert.Equal(t, "v1.2.3\n", out)
			assert.Equal(t, []string{fetchTagsCall, tagListCall}, f.calls)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("タグを取り直せなければ一覧を見ない", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{failOn: fetchTagsCall}

			_, err := listTags(f.runner())
			require.ErrorIs(t, err, errFakeCommand)
			assert.Equal(t, []string{fetchTagsCall}, f.calls)
		})

		t.Run("一覧を取得できなければエラーにする", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{failOn: tagListCall}

			_, err := listTags(f.runner())
			require.ErrorIs(t, err, errFakeCommand)
		})
	})
}

func Test_resolveExplicit(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("タグが 1 つも無ければ形だけを見て受け付ける", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{}

			v, err := resolveExplicit(f.runner(), "v0.1.0")
			require.NoError(t, err)
			assert.Equal(t, "v0.1.0", v.String())
			assert.Equal(t, []string{fetchTagsCall, tagListCall}, f.calls)
		})

		t.Run("最新のタグより patch が 1 つだけ新しい版を受け付ける", func(t *testing.T) {
			t.Parallel()

			v, err := resolveExplicit(taggableRunner().runner(), "v1.2.4")
			require.NoError(t, err)
			assert.Equal(t, "v1.2.4", v.String())
		})

		t.Run("最新のタグより新しい版を受け付ける", func(t *testing.T) {
			t.Parallel()

			v, err := resolveExplicit(taggableRunner().runner(), "v1.3.0")
			require.NoError(t, err)
			assert.Equal(t, "v1.3.0", v.String())
		})

		// 比較を数値で行わないと、v1.10.0 より v1.9.1 を新しいと読む。
		t.Run("最新のタグとの比較は数値で行う", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{outputs: map[string]string{tagListCall: "v1.9.0\n"}}

			_, err := resolveExplicit(f.runner(), "v1.10.0")
			require.NoError(t, err)
		})

		t.Run("リリースタグ以外のタグは比較に使わない", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{outputs: map[string]string{tagListCall: "nightly\nv9.0.0-rc1\n"}}

			_, err := resolveExplicit(f.runner(), "v0.1.0")
			require.NoError(t, err)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("空の版はタグを見ずに拒否する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			_, err := resolveExplicit(f.runner(), "")
			require.ErrorIs(t, err, errVersionFormat)
			assert.Empty(t, f.calls)
		})

		t.Run("接頭辞 v を欠く版は拒否する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			_, err := resolveExplicit(f.runner(), "1.3.0")
			require.ErrorIs(t, err, errVersionFormat)
			assert.Empty(t, f.calls)
		})

		t.Run("プレリリース付きの版は拒否する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			_, err := resolveExplicit(f.runner(), "v1.3.0-rc1")
			require.ErrorIs(t, err, errVersionFormat)
			assert.Empty(t, f.calls)
		})

		t.Run("前後に空白を含む版は拒否する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			_, err := resolveExplicit(f.runner(), " v1.3.0")
			require.ErrorIs(t, err, errVersionFormat)
			assert.Empty(t, f.calls)
		})

		t.Run("先頭に 0 を含む版は拒否する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			_, err := resolveExplicit(f.runner(), "v1.03.0")
			require.ErrorIs(t, err, errVersionFormat)
			assert.Empty(t, f.calls)
		})

		// 桁あふれは Atoi が上限へ丸めるので、丸めた版を受け付けると指定と違うタグ名になる。
		t.Run("int に収まらない桁の版は拒否する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			_, err := resolveExplicit(f.runner(), "v99999999999999999999.0.0")
			require.ErrorIs(t, err, errVersionFormat)
			assert.Empty(t, f.calls)
		})

		t.Run("既に在るタグは拒否する", func(t *testing.T) {
			t.Parallel()

			_, err := resolveExplicit(taggableRunner().runner(), "v1.2.3")
			require.ErrorIs(t, err, errVersionExists)
			assert.Contains(t, err.Error(), "v1.2.3")
		})

		// 先頭に 0 を含む既存のタグは文字列では一致しないので、同じ版を止めるのは順序の検査だけになる。
		t.Run("表記の違う既存のタグと同じ版は最新より新しくないとして拒否する", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{outputs: map[string]string{tagListCall: "v1.02.3\n"}}

			_, err := resolveExplicit(f.runner(), "v1.2.3")
			require.ErrorIs(t, err, errVersionNotNewer)
		})

		// 最新でないタグと同じ版も、最新より古いことより先に「既に在る」として返す。
		t.Run("最新でない既存のタグも既に在るとして拒否する", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{outputs: map[string]string{tagListCall: "v1.0.0\nv1.2.3\n"}}

			_, err := resolveExplicit(f.runner(), "v1.0.0")
			require.ErrorIs(t, err, errVersionExists)
		})

		t.Run("最新のタグより古い版は拒否する", func(t *testing.T) {
			t.Parallel()

			_, err := resolveExplicit(taggableRunner().runner(), "v1.2.2")
			require.ErrorIs(t, err, errVersionNotNewer)
			assert.Contains(t, err.Error(), "v1.2.3")
		})

		t.Run("タグを取り直せなければ一覧を見ずにエラーにする", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{failOn: fetchTagsCall}

			_, err := resolveExplicit(f.runner(), "v0.1.0")
			require.ErrorIs(t, err, errFakeCommand)
			assert.Equal(t, []string{fetchTagsCall}, f.calls)
		})

		t.Run("タグ一覧を取得できなければエラーにする", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{failOn: tagListCall}

			_, err := resolveExplicit(f.runner(), "v0.1.0")
			require.ErrorIs(t, err, errFakeCommand)
		})
	})
}

func Test_flagsSet(t *testing.T) {
	t.Parallel()

	newFlags := func() *flag.FlagSet {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.String("bump", "", "")
		fs.String("version", "", "")

		return fs
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("指定されたフラグだけを返す", func(t *testing.T) {
			t.Parallel()
			fs := newFlags()
			require.NoError(t, fs.Parse([]string{"-version", "v0.1.0"}))

			assert.Equal(t, map[string]bool{"version": true}, flagsSet(fs))
		})

		// 空の -version を「指定なし」と読むと、-bump の経路へ黙って落ちる。
		t.Run("空文字の指定も指定ありに数える", func(t *testing.T) {
			t.Parallel()
			fs := newFlags()
			require.NoError(t, fs.Parse([]string{"-version", ""}))

			assert.Equal(t, map[string]bool{"version": true}, flagsSet(fs))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("何も指定されなければ空を返す", func(t *testing.T) {
			t.Parallel()
			fs := newFlags()
			require.NoError(t, fs.Parse(nil))

			assert.Empty(t, flagsSet(fs))
		})
	})
}

func Test_resolveTagVersion(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("-version が無ければ -bump で最新のタグから決める", func(t *testing.T) {
			t.Parallel()

			v, err := resolveTagVersion(taggableRunner().runner(), map[string]bool{"bump": true}, "minor", "")
			require.NoError(t, err)
			assert.Equal(t, "v1.3.0", v.String())
		})

		t.Run("-version があれば指定の版を返す", func(t *testing.T) {
			t.Parallel()

			v, err := resolveTagVersion((&fakeRunner{}).runner(), map[string]bool{"version": true}, "", "v0.1.0")
			require.NoError(t, err)
			assert.Equal(t, "v0.1.0", v.String())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("-version と -bump を同時に与えるとタグを見ずに拒否する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			_, err := resolveTagVersion(f.runner(), map[string]bool{"bump": true, "version": true}, "patch", "v1.3.0")
			require.ErrorIs(t, err, errVersionWithBump)
			assert.Empty(t, f.calls)
		})

		t.Run("タグが無いのに -bump だけを与えると初回の案内を返す", func(t *testing.T) {
			t.Parallel()

			_, err := resolveTagVersion((&fakeRunner{}).runner(), map[string]bool{"bump": true}, "patch", "")
			require.ErrorIs(t, err, errNoTag)
			assert.Contains(t, err.Error(), "make tag-version VERSION=vX.Y.Z")
		})

		t.Run("-version の検証に落ちればそのエラーを返す", func(t *testing.T) {
			t.Parallel()

			_, err := resolveTagVersion(taggableRunner().runner(), map[string]bool{"version": true}, "", "v1.2.3")
			require.ErrorIs(t, err, errVersionExists)
		})
	})
}

func Test_resolveNext(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// origin のタグを取り直さずに判定すると、他所で打たれた最新タグを見落として
		// 既存のバージョンへ二重にタグを打つ。
		t.Run("タグを取り直してから最新のタグを起点に次バージョンを決める", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{outputs: map[string]string{tagListCall: "v1.2.3\nv1.10.0\n"}}

			current, next, err := resolveNext(f.runner(), "minor")
			require.NoError(t, err)
			assert.Equal(t, "v1.10.0", current.String())
			assert.Equal(t, "v1.11.0", next.String())
			assert.Equal(t, []string{fetchTagsCall, tagListCall}, f.calls)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("タグを取り直せなければタグ一覧を見ない", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{failOn: fetchTagsCall}

			_, _, err := resolveNext(f.runner(), "patch")
			require.ErrorIs(t, err, errFakeCommand)
			assert.Equal(t, []string{fetchTagsCall}, f.calls)
		})

		t.Run("タグ一覧を取得できなければエラーにする", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{failOn: tagListCall}

			_, _, err := resolveNext(f.runner(), "patch")
			require.ErrorIs(t, err, errFakeCommand)
		})

		t.Run("起点となるタグが無ければ初期タグを促す", func(t *testing.T) {
			t.Parallel()

			_, _, err := resolveNext((&fakeRunner{}).runner(), "patch")
			require.ErrorIs(t, err, errNoTag)
		})

		t.Run("未知の粒度は次バージョンを決めずに返す", func(t *testing.T) {
			t.Parallel()

			_, _, err := resolveNext(taggableRunner().runner(), "hotfix")
			require.ErrorIs(t, err, errUnknownBump)
		})
	})
}

//nolint:paralleltest // t.Chdir でプロセスの作業ディレクトリを変えるため並列化不可
func Test_runTag(t *testing.T) {
	// 作業ディレクトリを移さないので、t.Chdir を使う兄弟が終わった後で並列に走る。
	t.Run("基点のブランチは宣言から読み、そこへ合わせてからノートを見る", func(t *testing.T) {
		t.Parallel()
		f := taggableRunner()

		require.ErrorIs(t, runTag(f.runner(), otherDecl, []string{"-bump", "patch"}), errNoReleaseNote)
		assert.Contains(t, f.calls, "git switch main")
		assert.Contains(t, f.calls, "git reset --hard origin/main")
		assert.NotContains(t, f.calls, "git switch production")
	})

	t.Run("宣言に基点が無ければ手順を 1 つも実行しない", func(t *testing.T) {
		t.Parallel()
		f := taggableRunner()

		require.ErrorIs(t, runTag(f.runner(), branchdecl.Branches{Lines: testDecl.Lines}, []string{"-bump", "patch"}), errNoReleaseBase)
		assert.Empty(t, f.calls)
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("production へ合わせ、ノートを確かめてからタグ・push・Release の順に進む", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.NoError(t, runTag(f.runner(), testDecl, []string{"-bump", "patch"}))
			assert.Equal(t, []string{
				fetchTagsCall,
				tagListCall,
				"git fetch origin production",
				"git switch production",
				"git reset --hard origin/production",
				fetchTagsCall,
				annotateTagCall,
				tagPushCall,
				releaseCreateCall,
			}, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("タグが無くても -version の版で -bump と同じ順に進む", func(t *testing.T) {
			writeNoteFor(t, "v0.1.0")
			f := &fakeRunner{}

			require.NoError(t, runTag(f.runner(), testDecl, []string{"-version", "v0.1.0"}))
			assert.Equal(t, []string{
				fetchTagsCall,
				tagListCall,
				"git fetch origin production",
				"git switch production",
				"git reset --hard origin/production",
				fetchTagsCall,
				"git tag -a v0.1.0 --cleanup=verbatim -F .github/release/v0.1.0.md",
				"git push origin v0.1.0",
				"gh release create v0.1.0 --title v0.1.0 --notes-file .github/release/v0.1.0.md",
			}, f.calls)
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("タグが無いのに -bump だけでは production へ切り替える前に中止する", func(t *testing.T) {
			writeNote(t)
			f := &fakeRunner{}

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "patch"}), errNoTag)
			assert.NotContains(t, f.calls, "git switch production")
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("空の -version では手順を 1 つも実行しない", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-version", ""}), errVersionFormat)
			assert.Empty(t, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("-version と -bump の同時指定では手順を 1 つも実行しない", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "patch", "-version", "v1.2.4"}), errVersionWithBump)
			assert.Empty(t, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("既に在る版の -version では production へ切り替える前に中止する", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-version", "v1.2.3"}), errVersionExists)
			assert.NotContains(t, f.calls, "git switch production")
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("最新のタグより古い -version では production へ切り替える前に中止する", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-version", "v1.2.2"}), errVersionNotNewer)
			assert.Equal(t, []string{fetchTagsCall, tagListCall}, f.calls)
		})

		// 空の -bump も指定に数えないと、-version だけが与えられたものとして黙って進む。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("空の -bump と -version の同時指定でも手順を 1 つも実行しない", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "", "-version", "v1.2.4"}), errVersionWithBump)
			assert.Empty(t, f.calls)
		})

		// 粒度の既定値を置くと、指定し忘れた tag が意図しない版のタグと Release を作る。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("-bump も -version も無ければ production へ切り替える前に中止する", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, nil), errUnknownBump)
			assert.Equal(t, []string{fetchTagsCall, tagListCall}, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("空の -bump では production へ切り替える前に中止する", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", ""}), errUnknownBump)
			assert.Equal(t, []string{fetchTagsCall, tagListCall}, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("解釈できないフラグでは手順を 1 つも実行しない", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			err := runTag(f.runner(), testDecl, []string{"-no-such-flag"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to parse flags")
			assert.Empty(t, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("未知の粒度では production へ切り替える前に中止する", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "bogus"}), errUnknownBump)
			assert.NotContains(t, f.calls, "git switch production")
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("production へ合わせられなければノートを見ずにタグを打たない", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()
			f.failOn = "git reset --hard origin/production"

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "patch"}), errFakeCommand)
			assert.NotContains(t, f.calls, annotateTagCall)
		})

		// ノートはタグと Release の本文になるので、無いまま進むと本文の無いリリースが残る。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("production にノートが無ければ production へ合わせた後で中止する", func(t *testing.T) {
			t.Chdir(t.TempDir())
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "patch"}), errNoReleaseNote)
			assert.Contains(t, f.calls, "git switch production")
			assert.NotContains(t, f.calls, annotateTagCall)
			assert.NotContains(t, f.calls, tagPushCall)
			assert.NotContains(t, f.calls, releaseCreateCall)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("production のノートが空ならタグも push も Release も作らない", func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, releaseNoteDir), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dir, notePath("v1.2.4")), nil, 0o600))
			t.Chdir(dir)
			f := taggableRunner()

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "patch"}), errEmptyReleaseNote)
			assert.NotContains(t, f.calls, annotateTagCall)
			assert.NotContains(t, f.calls, tagPushCall)
			assert.NotContains(t, f.calls, releaseCreateCall)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("タグを作れなければ push も Release 作成も行わない", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()
			f.failOn = annotateTagCall

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "patch"}), errFakeCommand)
			assert.NotContains(t, f.calls, tagPushCall)
			assert.NotContains(t, f.calls, releaseCreateCall)
		})

		// push できていないタグで Release を作ると、参照先の無いリリースが GitHub に残る。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("タグを push できなければ Release を作らない", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()
			f.failOn = tagPushCall

			require.ErrorIs(t, runTag(f.runner(), testDecl, []string{"-bump", "patch"}), errFakeCommand)
			assert.NotContains(t, f.calls, releaseCreateCall)
		})
	})
}

func Test_runBranch(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("衝突も汚れも無ければブランチを切って push しデフォルトブランチを移す", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			require.NoError(t, runBranch(f.runner(), testDecl, []string{"-bump", "minor"}))
			assert.Equal(t, []string{
				fetchTagsCall,
				tagListCall,
				"git status --porcelain",
				"git fetch origin production",
				branchCreateCall,
				branchPushCall,
				defaultBranchCall,
			}, f.calls)
		})

		t.Run("線の役割と分岐元はフラグで差し替えられる", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			require.NoError(t, runBranch(f.runner(), testDecl, []string{"-bump", "patch", "-line", "hotfix", "-base", "staging"}))
			assert.Contains(t, f.calls, "git switch -c hotfix/v1.2.4 origin/staging")
			assert.Contains(t, f.calls, "gh repo edit --default-branch hotfix/v1.2.4")
		})

		t.Run("線の接頭辞と既定の分岐元は宣言から読む", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			require.NoError(t, runBranch(f.runner(), otherDecl, []string{"-bump", "patch", "-line", "hotfix"}))
			assert.Contains(t, f.calls, "git switch -c fix/v1.2.4 origin/main")
			assert.Contains(t, f.calls, "gh repo edit --default-branch fix/v1.2.4")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("未知の線の役割では origin へ触れる前に中止する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			err := runBranch(f.runner(), testDecl, []string{"-bump", "patch", "-line", "release/"})
			require.ErrorIs(t, err, errUnknownLine)
			assert.ErrorContains(t, err, "release / hotfix")
			assert.Empty(t, f.calls)
		})

		t.Run("接頭辞を受ける -prefix は無い", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			require.ErrorContains(t, runBranch(f.runner(), testDecl, []string{"-bump", "patch", "-prefix", "hotfix"}), "failed to parse flags")
			assert.Empty(t, f.calls)
		})

		t.Run("接頭辞の無い線の役割では origin へ触れる前に中止する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()
			decl := testDecl
			decl.Lines = branchdecl.Lines{Release: "release/"}

			require.ErrorIs(t, runBranch(f.runner(), decl, []string{"-bump", "patch", "-line", "hotfix"}), errUnknownLine)
			assert.Empty(t, f.calls)
		})

		t.Run("宣言に基点が無ければ手順を 1 つも実行しない", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			require.ErrorIs(t, runBranch(f.runner(), branchdecl.Branches{Lines: testDecl.Lines}, []string{"-bump", "patch"}), errNoReleaseBase)
			assert.Empty(t, f.calls)
		})

		// **「無い」と「確認できなかった」を畳むと、ここが通ってしまう。** 照会に失敗した
		// だけで「衝突なし」と読み、取り消しの効かない手順へ進む。
		t.Run("origin へ照会できなければ手順を 1 つも実行しない", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()
			f.lsRemoteFails = true

			err := runBranch(f.runner(), testDecl, []string{"-bump", "patch"})
			require.ErrorIs(t, err, errLsRemote)
			assert.NotContains(t, f.calls, branchCreateCall)
			assert.NotContains(t, f.calls, branchPushCall)
			assert.NotContains(t, f.calls, defaultBranchCall)
		})

		t.Run("解釈できないフラグでは手順を 1 つも実行しない", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()

			err := runBranch(f.runner(), testDecl, []string{"-no-such-flag"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to parse flags")
			assert.Empty(t, f.calls)
		})

		// タグ不在は tag 側と原因が同じでも復旧手順が違うため、案内を差し替える。
		t.Run("起点となるタグが無ければ初期タグ作成の案内へ差し替える", func(t *testing.T) {
			t.Parallel()

			err := runBranch((&fakeRunner{}).runner(), testDecl, []string{"-bump", "patch"})
			require.ErrorIs(t, err, errNoTagForBranch)
			require.NotErrorIs(t, err, errNoTag)
			assert.Contains(t, err.Error(), "make tag-version VERSION=vX.Y.Z")
		})

		// 差し替えるのはタグ不在だけ。取得そのものの失敗まで案内へ丸めると原因が消える。
		t.Run("タグの取得に失敗した場合は案内へ差し替えない", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{failOn: tagListCall}

			err := runBranch(f.runner(), testDecl, []string{"-bump", "patch"})
			require.ErrorIs(t, err, errFakeCommand)
			require.NotErrorIs(t, err, errNoTagForBranch)
		})

		t.Run("origin に同名ブランチが既にあればブランチを切らずに中止する", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()
			f.remoteBranches = map[string]bool{"release/v1.3.0": true}

			require.ErrorIs(t, runBranch(f.runner(), testDecl, []string{"-bump", "minor"}), errBranchExists)
			assert.NotContains(t, f.calls, "git status --porcelain")
			assert.NotContains(t, f.calls, branchCreateCall)
		})

		t.Run("作業ツリーの状態を取得できなければブランチを切らない", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()
			f.failOn = "git status --porcelain"

			require.ErrorIs(t, runBranch(f.runner(), testDecl, []string{"-bump", "minor"}), errFakeCommand)
			assert.NotContains(t, f.calls, branchCreateCall)
		})

		// 未コミットの変更を抱えたままブランチを切ると、切り替え先へ変更が付いて回る。
		t.Run("作業ツリーが汚れていれば中止し何が汚れているかを見せる", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()
			f.outputs["git status --porcelain"] = " M main.go\n"

			require.ErrorIs(t, runBranch(f.runner(), testDecl, []string{"-bump", "minor"}), errDirtyWorktree)
			assert.Contains(t, f.calls, "git status --short")
			assert.NotContains(t, f.calls, branchCreateCall)
		})

		t.Run("ブランチを push できなければデフォルトブランチを移さない", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()
			f.failOn = branchPushCall

			require.ErrorIs(t, runBranch(f.runner(), testDecl, []string{"-bump", "minor"}), errFakeCommand)
			assert.NotContains(t, f.calls, defaultBranchCall)
		})
	})
}

func Test_parseFlags(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("フラグを解釈して値を取り込む", func(t *testing.T) {
			t.Parallel()
			fs := flag.NewFlagSet("tag", flag.ContinueOnError)
			fs.SetOutput(io.Discard)
			bumpKind := fs.String("bump", "", "")

			require.NoError(t, parseFlags(fs, []string{"-bump", "minor"}))
			assert.Equal(t, "minor", *bumpKind)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ヘルプ要求は通常のエラーと区別して返す", func(t *testing.T) {
			t.Parallel()
			fs := flag.NewFlagSet("tag", flag.ContinueOnError)
			fs.SetOutput(io.Discard)

			err := parseFlags(fs, []string{"-h"})

			require.ErrorIs(t, err, errHelpRequested)
		})

		t.Run("未知のフラグはヘルプ要求と混同せずエラーにする", func(t *testing.T) {
			t.Parallel()
			fs := flag.NewFlagSet("tag", flag.ContinueOnError)
			fs.SetOutput(io.Discard)

			err := parseFlags(fs, []string{"-bogus"})

			require.Error(t, err)
			assert.NotErrorIs(t, err, errHelpRequested)
		})
	})
}

//nolint:paralleltest // t.Chdir を使うため並列化不可
func Test_execute(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("tag はタグ作成の手順へ振り分ける", func(t *testing.T) {
			writeNote(t)
			f := taggableRunner()

			require.NoError(t, execute(f.runner(), testDecl, []string{"tag", "-bump", "patch"}))
			assert.Contains(t, f.calls, annotateTagCall)
			assert.Contains(t, f.calls, tagPushCall)
			assert.Contains(t, f.calls, releaseCreateCall)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("branch はブランチ作成の手順へ振り分ける", func(t *testing.T) {
			f := taggableRunner()

			require.NoError(t, execute(f.runner(), testDecl, []string{"branch", "-bump", "minor"}))
			assert.Contains(t, f.calls, branchCreateCall)
			assert.Contains(t, f.calls, branchPushCall)
			assert.Contains(t, f.calls, defaultBranchCall)
		})

		// ヘルプを異常終了にすると、同じリポジトリの他ツールと終了コードが食い違う。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("tag のヘルプ要求は失敗にせず手順を 1 つも実行しない", func(t *testing.T) {
			f := taggableRunner()

			require.NoError(t, execute(f.runner(), testDecl, []string{"tag", "-h"}))
			assert.Empty(t, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("branch のヘルプ要求は失敗にせず手順を 1 つも実行しない", func(t *testing.T) {
			f := taggableRunner()

			require.NoError(t, execute(f.runner(), testDecl, []string{"branch", "-h"}))
			assert.Empty(t, f.calls)
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("サブコマンドが無ければ使い方を示して手順を 1 つも実行しない", func(t *testing.T) {
			f := taggableRunner()

			require.ErrorIs(t, execute(f.runner(), testDecl, nil), errUsage)
			assert.Empty(t, f.calls)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("未知のサブコマンドでは手順を 1 つも実行しない", func(t *testing.T) {
			f := taggableRunner()

			require.ErrorIs(t, execute(f.runner(), testDecl, []string{"bogus"}), errUnknownSubcommand)
			assert.Empty(t, f.calls)
		})

		// ヘルプ要求だけを飲み込む。失敗まで飲み込むと、タグを打てていないのに 0 で終わる。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("tag の失敗はヘルプ要求と区別してそのまま返す", func(t *testing.T) {
			require.ErrorIs(t, execute((&fakeRunner{}).runner(), testDecl, []string{"tag", "-bump", "patch"}), errNoTag)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("branch の失敗はヘルプ要求と区別してそのまま返す", func(t *testing.T) {
			require.ErrorIs(t, execute((&fakeRunner{}).runner(), testDecl, []string{"branch", "-bump", "patch"}), errNoTagForBranch)
		})
	})
}

func Test_start(t *testing.T) {
	t.Parallel()

	withDecl := func(t *testing.T, body string) func() (string, error) {
		t.Helper()
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, filepath.Dir(branchdecl.File)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, branchdecl.File), []byte(body), 0o600))
		return func() (string, error) { return dir, nil }
	}
	same := withDecl(t, `{"deploy": [{"branch": "production", "release_base": true}, {"branch": "staging"}, {"branch": "develop"}], "lines": {"release": "release/", "hotfix": "hotfix/"}, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("直下で宣言を読めれば execute へ進む", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{}

			require.ErrorIs(t, start(f.runner(), nil, same, same), errUsage)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("直下でなければ手順を 1 つも実行しない", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{}
			other := t.TempDir()
			sub := func() (string, error) { return other, nil }

			require.ErrorIs(t, start(f.runner(), []string{"tag"}, sub, same), reporoot.ErrNotRoot)
			assert.Empty(t, f.calls)
		})

		t.Run("直下の宣言が壊れていれば手順を 1 つも実行しない", func(t *testing.T) {
			t.Parallel()
			f := taggableRunner()
			broken := withDecl(t, `{"lines": ["release/"]}`)

			require.ErrorIs(t, start(f.runner(), []string{"tag", "-bump", "patch"}, broken, broken), branchdecl.ErrDeclaration)
			assert.Empty(t, f.calls)
		})

		t.Run("直下に宣言が無ければ手順を 1 つも実行しない", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{}
			empty := t.TempDir()
			bare := func() (string, error) { return empty, nil }

			require.ErrorIs(t, start(f.runner(), []string{"tag", "-bump", "patch"}, bare, bare), os.ErrNotExist)
			assert.Empty(t, f.calls)
		})

		t.Run("直下を解決できなければ手順を 1 つも実行しない", func(t *testing.T) {
			t.Parallel()
			f := &fakeRunner{}
			unresolved := func() (string, error) { return "", reporoot.ErrNoToplevel }

			require.ErrorIs(t, start(f.runner(), []string{"tag"}, same, unresolved), reporoot.ErrNoToplevel)
			assert.Empty(t, f.calls)
		})
	})
}

func Test_checkReleaseNote(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "v1.2.4.md")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		return path
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("本文のあるノートを受け付ける", func(t *testing.T) {
			t.Parallel()

			require.NoError(t, checkReleaseNote(write(t, "# v1.2.4\n")))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("無ければ errNoReleaseNote を返す", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "v1.2.4.md")

			err := checkReleaseNote(path)
			require.ErrorIs(t, err, errNoReleaseNote)
			assert.Contains(t, err.Error(), path)
		})

		// git tag -F にディレクトリを渡すと、production へ合わせた後でタグの作成が失敗する。
		t.Run("ディレクトリなら errReleaseNoteNotFile を返す", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "v1.2.4.md")
			require.NoError(t, os.Mkdir(path, 0o750))

			require.ErrorIs(t, checkReleaseNote(path), errReleaseNoteNotFile)
		})

		// 空のノートでも git tag -F は成功し、本文の無いタグと Release が push される。
		for name, body := range map[string]string{"空": "", "空白だけ": " \n\t\n"} {
			t.Run(name+"なら errEmptyReleaseNote を返す", func(t *testing.T) {
				t.Parallel()

				require.ErrorIs(t, checkReleaseNote(write(t, body)), errEmptyReleaseNote)
			})
		}

		t.Run("読めなければ「無い」と言わずに読めない理由を返す", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "特権実行では権限を落としても読めてしまい、読めないノートを作れない")
			path := write(t, "# v1.2.4\n")
			require.NoError(t, os.Chmod(path, 0o000))

			err := checkReleaseNote(path)
			require.ErrorIs(t, err, os.ErrPermission)
			require.NotErrorIs(t, err, errNoReleaseNote)
		})
	})
}
