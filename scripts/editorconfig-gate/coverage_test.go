package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

// staged は、paths を `git ls-files -z --stage` の形（mode 100644、段 0）で並べます。
func staged(paths ...string) string {
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(stageLine("100644", 0, p))
	}
	return b.String()
}

func stageLine(mode string, stage int, p string) string {
	return fmt.Sprintf("%s e69de29bb2d1d6434b8b29ae775ad8c2e48c5391 %d\t%s\x00", mode, stage, p)
}

func coverFake(t *testing.T, listed, deleted string) *tooltest.Fake {
	t.Helper()
	f := newFake(t)
	f.Set(stepLsFiles, "out", listed)
	f.Set(stepLsDeleted, "out", deleted)
	return f
}

func Test_runCoverage(t *testing.T) {
	t.Parallel()

	t.Run("ls-files が0件なら、-dry-run の一覧に関わらず errNoInput", func(t *testing.T) {
		t.Parallel()
		for name, scanned := range map[string][]string{"-dry-run も0件": nil, "-dry-run は在る": {"a"}} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f := coverFake(t, "", "")
				var out bytes.Buffer
				require.ErrorIs(t, runCoverage(scanned, &out, f.Tool(), isolatedEnv), errNoInput)
				assert.NotContains(t, out.String(), "✅")
			})
		}
	})

	t.Run("消えた追跡ファイル・submodule・宣言した除外を引いて0件なら errNoInput", func(t *testing.T) {
		t.Parallel()
		listed := staged("gone.md", "x/go.mod") + stageLine(gitlinkMode, 0, "sub")
		f := coverFake(t, listed, "gone.md\x00")
		require.ErrorIs(t, runCoverage([]string{"a"}, new(bytes.Buffer), f.Tool(), isolatedEnv), errNoInput)
	})

	t.Run("追跡ファイルが在り -dry-run が0件なら errUncovered で、終了コードは 1", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml"), "")
		var out bytes.Buffer
		err := runCoverage(nil, &out, f.Tool(), isolatedEnv)
		require.ErrorIs(t, err, errUncovered)
		assert.Equal(t, 1, exitCode(err))
		assert.Contains(t, out.String(), "a.yaml\n")
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("ls-files が stderr にだけ出したパスは数えず errNoInput", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, "", "")
		f.Set(stepLsFiles, "err", staged("a.yaml"))
		require.ErrorIs(t, runCoverage([]string{"a.yaml"}, new(bytes.Buffer), f.Tool(), isolatedEnv), errNoInput)
	})

	t.Run("ls-deleted が stderr にだけ出したパスは引かない", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml", "gone.md"), "")
		f.Set(stepLsDeleted, "err", "gone.md\x00")
		require.ErrorIs(t, runCoverage([]string{"a.yaml"}, new(bytes.Buffer), f.Tool(), isolatedEnv), errUncovered)
	})

	for name, c := range map[string]struct{ step, suffix, body string }{
		"ls-files が rc≠0":      {stepLsFiles, "rc", "128"},
		"ls-files がシグナルで終わる":   {stepLsFiles, "kill", ""},
		"ls-deleted が rc≠0":    {stepLsDeleted, "rc", "128"},
		"ls-deleted がシグナルで終わる": {stepLsDeleted, "kill", ""},
	} {
		t.Run(name+"なら errTool", func(t *testing.T) {
			t.Parallel()
			f := coverFake(t, staged("a"), "")
			f.Set(c.step, c.suffix, c.body)
			var out bytes.Buffer
			require.ErrorIs(t, runCoverage([]string{"a"}, &out, f.Tool(), isolatedEnv), errTool)
			assert.NotContains(t, out.String(), "✅")
		})
	}

	for _, step := range []string{stepLsFiles, stepLsDeleted} {
		t.Run(step+" が rc≠0 なら、stderr にだけ出した診断を載せて errTool", func(t *testing.T) {
			t.Parallel()
			f := coverFake(t, staged("a"), "")
			f.Set(step, "err", "STDERR-ONLY\n")
			f.Set(step, "rc", "1")
			err := runCoverage([]string{"a"}, new(bytes.Buffer), f.Tool(), isolatedEnv)
			require.ErrorIs(t, err, errTool)
			assert.Contains(t, err.Error(), "STDERR-ONLY")
		})
	}

	t.Run("ls-files が rc≠0 なら ls-deleted を起動しない", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a"), "")
		f.Set(stepLsFiles, "rc", "128")
		require.ErrorIs(t, runCoverage([]string{"a"}, new(bytes.Buffer), f.Tool(), isolatedEnv), errTool)
		assert.Equal(t, []string{"editorconfig-gate ls-files"}, stepsOf(f.Calls()))
	})

	t.Run("git を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runCoverage([]string{"a"}, new(bytes.Buffer), tooltest.Missing, isolatedEnv), errTool)
	})

	t.Run("ls-files・ls-deleted の順に、リポジトリ直下で隔離した env を渡して起動する", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a"), "")
		require.NoError(t, runCoverage([]string{"a"}, new(bytes.Buffer), f.Tool(), isolatedEnv))
		calls := f.Calls()
		assert.Equal(t, []string{"editorconfig-gate ls-files", "editorconfig-gate ls-deleted"}, stepsOf(calls))
		for _, c := range calls {
			assert.True(t, strings.HasSuffix(c, " /isolated"), c)
		}
	})

	t.Run("作業ツリーから消えた追跡ファイルは引き、件数を出す", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml", "gone.md", "x/gone two.md"), "gone.md\x00x/gone two.md\x00")
		var out bytes.Buffer
		require.NoError(t, runCoverage([]string{"a.yaml"}, &out, f.Tool(), isolatedEnv))
		assert.Contains(t, out.String(), "作業ツリーから消えた 2 件")
		assert.Contains(t, out.String(), "✅ editorconfig coverage: 追跡ファイル 1 件")
	})

	t.Run("submodule（mode 160000）は引き、件数を出す。symlink（120000）は引かない", func(t *testing.T) {
		t.Parallel()
		listed := staged("a.yaml") + stageLine(gitlinkMode, 0, "vendor/sub") + stageLine("120000", 0, "link.md")
		f := coverFake(t, listed, "")
		var out bytes.Buffer
		err := runCoverage([]string{"a.yaml"}, &out, f.Tool(), isolatedEnv)
		require.ErrorIs(t, err, errUncovered)
		assert.Contains(t, out.String(), "submodule 1 件")
		assert.Contains(t, out.String(), "link.md\n")
		assert.NotContains(t, out.String(), "vendor/sub\n")
	})

	t.Run("宣言した除外の名前は、どの深さでも引き、件数を出す", func(t *testing.T) {
		t.Parallel()
		paths := []string{"a.yaml"}
		for i, n := range excludedNames {
			paths = append(paths, n, path.Join(strings.Repeat("d/", i+1), n))
		}
		f := coverFake(t, staged(paths...), "")
		var out bytes.Buffer
		require.NoError(t, runCoverage([]string{"a.yaml"}, &out, f.Tool(), isolatedEnv))
		assert.Contains(t, out.String(), fmt.Sprintf("宣言した除外 %d 件", 2*len(excludedNames)))
	})

	t.Run("宣言した除外は名前の一致だけで、名前を含むだけのパスは引かない", func(t *testing.T) {
		t.Parallel()
		near := []string{"go.mod.bak", "xgo.mod", "x/go.sum.old", "go.mod/x.yaml", "pnpm-lock.yaml.orig", "a.terraform.lock.hcl"}
		f := coverFake(t, staged(near...), "")
		var out bytes.Buffer
		err := runCoverage([]string{"z"}, &out, f.Tool(), isolatedEnv)
		require.ErrorIs(t, err, errUncovered)
		assert.Contains(t, err.Error(), fmt.Sprintf("%d 件", len(near)))
		for _, p := range near {
			assert.Contains(t, out.String(), p+"\n")
		}
		assert.Contains(t, out.String(), "宣言した除外 0 件")
	})

	t.Run("欠けが1件なら、そのパスを名指して errUncovered", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml", "b.md"), "")
		var out bytes.Buffer
		err := runCoverage([]string{"a.yaml"}, &out, f.Tool(), isolatedEnv)
		require.ErrorIs(t, err, errUncovered)
		assert.Contains(t, err.Error(), "1 件")
		assert.Contains(t, out.String(), "❌ editorconfig coverage: 追跡ファイルが走査の対象から外れています:\nb.md\n")
	})

	t.Run("欠けが21件以上なら、先頭20件と総数を出す", func(t *testing.T) {
		t.Parallel()
		var paths []string
		for i := range 25 {
			paths = append(paths, fmt.Sprintf("f%02d", i))
		}
		f := coverFake(t, staged(paths...), "")
		var out bytes.Buffer
		err := runCoverage([]string{"f00"}, &out, f.Tool(), isolatedEnv)
		require.ErrorIs(t, err, errUncovered)
		assert.Contains(t, err.Error(), "24 件")
		assert.Contains(t, out.String(), "f20\n")
		assert.NotContains(t, out.String(), "f21")
		assert.NotContains(t, out.String(), "f00\n")
	})

	t.Run("-dry-run だけに在るもの（未追跡のファイル）は許す", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml"), "")
		require.NoError(t, runCoverage([]string{"untracked.md", "a.yaml", "b"}, new(bytes.Buffer), f.Tool(), isolatedEnv))
	})

	t.Run("衝突中の同じパスの複数の段は1件と数える", func(t *testing.T) {
		t.Parallel()
		listed := stageLine("100644", 1, "c.md") + stageLine("100644", 2, "c.md") + stageLine("100644", 3, "c.md") + staged("a.yaml")
		f := coverFake(t, listed, "")
		var out bytes.Buffer
		require.NoError(t, runCoverage([]string{"a.yaml", "c.md", "c.md"}, &out, f.Tool(), isolatedEnv))
		assert.Contains(t, out.String(), "✅ editorconfig coverage: 追跡ファイル 2 件")
	})

	t.Run("-z の一覧は空白・タブ・非 ASCII を含むパスをそのまま読む", func(t *testing.T) {
		t.Parallel()
		paths := []string{"sp ace.md", "d/ünï.md", "a\tb.md"}
		f := coverFake(t, staged(paths...), "")
		var out bytes.Buffer
		require.NoError(t, runCoverage(paths, &out, f.Tool(), isolatedEnv))
		assert.Contains(t, out.String(), "追跡ファイル 3 件")
	})

	t.Run("改行を含むパスは -dry-run の行に載り得ないので、引用して名指し errUncovered", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml", "new\nline.md"), "")
		var out bytes.Buffer
		err := runCoverage([]string{"a.yaml", "new", "line.md"}, &out, f.Tool(), isolatedEnv)
		require.ErrorIs(t, err, errUncovered)
		assert.Contains(t, out.String(), `"new\nline.md"`+"\n")
	})

	t.Run("ls-files の行を解釈できなければ errRead", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, "a.yaml\x00", "")
		require.ErrorIs(t, runCoverage([]string{"a.yaml"}, new(bytes.Buffer), f.Tool(), isolatedEnv), errRead)
	})
}

func Test_runCoverage_realGit(t *testing.T) {
	t.Parallel()

	// -dry-run（editorconfig-checker が core.quotepath=false の git から得る一覧）と同じ形で、非 ASCII と空白を含む
	// パスを ls-files が返すことを、実物の git で確かめる。macOS の git は core.precomposeunicode で NFD の名前を
	// NFC で返すので、-dry-run も NFC になる。ほかの OS では作業ツリーの名前のまま返る。
	nfc, nfd := "docs/dé.md", "docs/de\u0301.md"
	repo := t.TempDir()
	for _, p := range []string{"docs/ünï.md", "sp ace.md", nfd} {
		writeFile(t, filepath.Join(repo, filepath.FromSlash(p)), "x\n")
	}
	env, err := gitEnv()
	require.NoError(t, err)
	inRepo := func(_ string, env []string, step string) *exec.Cmd { return command(repo, env, step) }
	for _, step := range []string{stepGitInit, stepGitAdd} {
		r, err := toolrun.Run(inRepo("", env, step))
		require.NoError(t, err)
		require.Equal(t, 0, r.Code, r.Combined)
	}
	listedNFD := nfd
	if runtime.GOOS == "darwin" {
		listedNFD = nfc
	}
	var out bytes.Buffer
	require.NoError(t, runCoverage([]string{"docs/ünï.md", "sp ace.md", listedNFD}, &out, inRepo, env), out.String())
	assert.Contains(t, out.String(), "追跡ファイル 3 件がすべて走査の対象です")
}

func Test_coverageTargets(t *testing.T) {
	t.Parallel()

	t.Run("消えた・submodule・宣言した除外の順に1つの区分へ数え、残りを ls-files の順で返す", func(t *testing.T) {
		t.Parallel()
		listed := staged("b", "gone/go.mod", "x/go.sum", "a") + stageLine(gitlinkMode, 0, "sub") + stageLine(gitlinkMode, 0, "gone-sub")
		got, err := coverageTargets(listed, "gone/go.mod\x00gone-sub\x00")
		require.NoError(t, err)
		assert.Equal(t, targets{paths: []string{"b", "a"}, tracked: 6, deleted: 2, submodules: 1, declared: 1}, got)
	})

	t.Run("空なら0件", func(t *testing.T) {
		t.Parallel()
		got, err := coverageTargets("", "")
		require.NoError(t, err)
		assert.Equal(t, targets{}, got)
	})
}

func Test_parseStage(t *testing.T) {
	t.Parallel()

	t.Run("mode とパスを読み、同じパスは最初の1つだけ返す", func(t *testing.T) {
		t.Parallel()
		got, err := parseStage(stageLine("100644", 1, "a b") + stageLine("100644", 2, "a b") + stageLine(gitlinkMode, 0, "s"))
		require.NoError(t, err)
		assert.Equal(t, []stageEntry{{mode: "100644", path: "a b"}, {mode: gitlinkMode, path: "s"}}, got)
	})

	for name, out := range map[string]string{
		"タブが無い":       "a\x00",
		"欄が足りない":      "100644 0\ta\x00",
		"パスが空":        "100644 e69d 0\t\x00",
		"mode が数字でない": "10064x e69d 0\ta\x00",
	} {
		t.Run(name+"なら errRead", func(t *testing.T) {
			t.Parallel()
			_, err := parseStage(out)
			require.ErrorIs(t, err, errRead)
		})
	}
}

func Test_dryRunPaths(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		out  string
		want []string
	}{
		"空":            {"", nil},
		"1行1パスを順に":     {"b\na\n", []string{"b", "a"}},
		"空行と空白だけの行を外す": {"a\n\n  \nb", []string{"a", "b"}},
		"色の制御列を外す":     {"\x1b[0m\n\x1b[33ma.yaml\x1b[0m\n", []string{"a.yaml"}},
		"先頭の ./ を外す":   {"./a.yaml\n./d/b.md\n", []string{"a.yaml", "d/b.md"}},
		"空白を含むパスはそのまま": {"sp ace.md\n", []string{"sp ace.md"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, dryRunPaths(c.out))
		})
	}
}

func Test_missingFiles(t *testing.T) {
	t.Parallel()

	t.Run("want のうち scanned に無いものを want の順で返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"c", "a"}, missingFiles([]string{"b", "x/c"}, []string{"c", "b", "x/c", "a"}))
	})

	t.Run("scanned が空なら want のすべて", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"a"}, missingFiles(nil, []string{"a"}))
	})

	t.Run("欠けが無ければ nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, missingFiles([]string{"a", "extra"}, []string{"a"}))
	})
}

func Test_isExcludedName(t *testing.T) {
	t.Parallel()

	for p, want := range map[string]bool{
		"go.mod":                         true,
		"scripts/go.sum":                 true,
		"docker/tools/pnpm-lock.yaml":    true,
		"usecases/x/.terraform.lock.hcl": true,
		"go.mod.bak":                     false,
		"xgo.mod":                        false,
		"go.mod/x":                       false,
		"go.work":                        false,
		"a/b.yaml":                       false,
	} {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isExcludedName(p))
		})
	}
}

func Test_displayPath(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"a/b.md":       "a/b.md",
		"sp ace.md":    "sp ace.md",
		"ünï.md":       "ünï.md",
		"new\nline.md": `"new\nline.md"`,
		"cr\r.md":      `"cr\r.md"`,
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, displayPath(in))
		})
	}
}

func Test_gitList(t *testing.T) {
	t.Parallel()

	t.Run("リポジトリ直下で段の git を隔離した env で起動し、stdout だけを返す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepLsDeleted, "out", "a\x00b\x00")
		f.Set(stepLsDeleted, "err", "STDERR-ONLY\n")
		got, err := gitList(f.Tool(), isolatedEnv, stepLsDeleted)
		require.NoError(t, err)
		assert.Equal(t, "a\x00b\x00", got)
		calls := f.Calls()
		require.Len(t, calls, 1)
		assert.True(t, strings.HasSuffix(calls[0], " "+stepLsDeleted+" /isolated"), calls[0])
	})

	t.Run("rc≠0 なら、段と出力を名指して errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepLsDeleted, "err", "fatal: x\n")
		f.Set(stepLsDeleted, "rc", "128")
		_, err := gitList(f.Tool(), isolatedEnv, stepLsDeleted)
		require.ErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), stepLsDeleted+" が rc=128")
		assert.Contains(t, err.Error(), "fatal: x")
	})

	t.Run("シグナルで終わるか起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepLsDeleted, "kill", "")
		_, err := gitList(f.Tool(), isolatedEnv, stepLsDeleted)
		require.ErrorIs(t, err, errTool)
		_, err = gitList(tooltest.Missing, isolatedEnv, stepLsDeleted)
		require.ErrorIs(t, err, errTool)
	})
}
