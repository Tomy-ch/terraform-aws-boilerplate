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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitrev"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func Test_trufflehogExcludeFile(t *testing.T) {
	t.Parallel()

	assert.Equal(t, trufflehogRangeExclude, trufflehogExcludeFile(stepTrufflehogRange))
	assert.Equal(t, trufflehogRangeExclude, trufflehogExcludeFile(stepTrufflehogRangeJSON))
	assert.Equal(t, trufflehogHistoryExclude, trufflehogExcludeFile(stepTrufflehogHistory))
	assert.Equal(t, trufflehogHistoryExclude, trufflehogExcludeFile(stepTrufflehogHistoryJSON))
}

func Test_trufflehogExcludes(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, base, history string) string {
		t.Helper()
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, trufflehogExclude), []byte(base), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, trufflehogExcludeHistory), []byte(history), 0o600))
		return root
	}

	t.Run("和集合は2つを連結し、改行で終わらない範囲の抑止の後ろに改行を足す", func(t *testing.T) {
		t.Parallel()
		base, union, err := trufflehogExcludes(write(t, "^a$", "^b$\n"))
		require.NoError(t, err)
		assert.Equal(t, "^a$", base)
		assert.Equal(t, "^a$\n^b$\n", union)

		_, union, err = trufflehogExcludes(write(t, "^a$\n", "^b$\n"))
		require.NoError(t, err)
		assert.Equal(t, "^a$\n^b$\n", union)
	})

	for name, missing := range map[string]string{"範囲の抑止が無い": trufflehogExclude, "全履歴の抑止が無い": trufflehogExcludeHistory} {
		t.Run(name+"なら errInput", func(t *testing.T) {
			t.Parallel()
			root := write(t, "", "")
			require.NoError(t, os.Remove(filepath.Join(root, missing)))
			_, _, err := trufflehogExcludes(root)
			require.ErrorIs(t, err, errInput)
		})
	}
}

func Test_parseTrufflehogFlags(t *testing.T) {
	t.Parallel()

	from, format, err := parseTrufflehogFlags("x", []string{"-from", "a", "-format", "json"}, true, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, [2]string{"a", "json"}, [2]string{from, format})

	_, format, err = parseTrufflehogFlags("x", nil, false, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, "text", format, "既定は text")

	for name, c := range map[string]struct {
		args   []string
		ranged bool
	}{
		"受けない形":          {[]string{"-format", "sarif"}, false},
		"位置引数":           {[]string{"x"}, false},
		"範囲でない走査の -from": {[]string{"-from", "a"}, false},
		"知らない flag":      {[]string{"-x"}, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseTrufflehogFlags("x", c.args, c.ranged, &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_runTrufflehogGit(t *testing.T) {
	t.Parallel()

	t.Run("全履歴: GIT_DIR が外側を指していても、外側を書き換えず、走査器へ GIT_DIR を渡さない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		outer := newFixture(t)
		before := outer.snapshot()
		s := newScanner(t)
		s.passingCanaries()
		run := r.runner(s)
		run.environ = leaked(outer.dir)
		require.NoError(t, runTrufflehogGit(nil, false, &bytes.Buffer{}, &bytes.Buffer{}, run))
		assert.Equal(t, before, outer.snapshot())
		calls := s.calls()
		require.Len(t, calls, 3)
		for _, c := range calls {
			assert.Empty(t, c.gitDir, "%s へ GIT_DIR を渡した", c.step)
		}
	})

	for name, ranged := range map[string]bool{"全履歴": false, "範囲": true} {
		t.Run(name+": 全履歴の抑止を解釈できなければ errExclude で、本走査を起動しない", func(t *testing.T) {
			t.Parallel()
			r, _ := rangeRepo(t)
			require.NoError(t, os.WriteFile(filepath.Join(r.dir, trufflehogExcludeHistory), []byte("(\n"), 0o600))
			s := newScanner(t)
			s.passingCanaries()
			args := []string(nil)
			if ranged {
				args = []string{"-from", "HEAD~1"}
			}
			require.ErrorIs(t, runTrufflehogGit(args, ranged, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), errExclude)
			for _, step := range s.steps() {
				assert.NotContains(t, step, r.name()+" ", "本走査を起動した")
			}
		})
	}

	t.Run("範囲: canary を git と filesystem で走らせてから、解決した sha と範囲の抑止で本走査を起動し、stdout には本走査だけを出す", func(t *testing.T) {
		t.Parallel()
		r, from := rangeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepTrufflehogRange, "out", "MAIN\n")
		var stdout, log bytes.Buffer
		require.NoError(t, runTrufflehogGit([]string{"-from", "HEAD~1", "-format", "text"}, true, &stdout, &log, r.runner(s)))

		assert.Equal(t, "MAIN\n", stdout.String(), "canary の出力を stdout へ出した")
		assert.Equal(t, []string{"canary-git " + stepTrufflehogRangeJSON, "canary-fs " + stepTrufflehogFilesystemJSON, r.name() + " " + stepTrufflehogRange}, s.steps())
		calls := s.calls()
		assert.Equal(t, from, calls[2].rev)
		assert.True(t, gitrev.IsHash(calls[0].rev))
		assert.NotEqual(t, from, calls[0].rev)
		assert.Equal(t, "# base\n^base-excluded$\n", s.read("exclude-"+stepTrufflehogRange+"@"+r.name()), "範囲の本走査は範囲の抑止だけを読む")
		assert.Equal(t, "# base\n^base-excluded$\n# history\n^history-excluded$\n", s.read("exclude-"+stepTrufflehogRangeJSON+"@canary-git"), "canary は和集合を読む")
		assert.Equal(t, "./"+canaryCredentials+"\n", s.read("files-"+stepTrufflehogFilesystemJSON+"@canary-fs"))
		assert.Contains(t, log.String(), "✅ trufflehog の canary")
		noWork(t, r.dir, "trufflehog-range")
	})

	t.Run("全履歴: json の本走査は rev を受けず、和集合の抑止を読む", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepTrufflehogHistoryJSON+"@"+r.name(), "out", "{}\n")
		var stdout bytes.Buffer
		require.NoError(t, runTrufflehogGit([]string{"-format", "json"}, false, &stdout, &bytes.Buffer{}, r.runner(s)))
		assert.Equal(t, "{}\n", stdout.String())
		assert.Equal(t, []string{"canary-git " + stepTrufflehogHistoryJSON, "canary-fs " + stepTrufflehogFilesystemJSON, r.name() + " " + stepTrufflehogHistoryJSON}, s.steps())
		assert.Empty(t, s.calls()[0].rev)
		assert.Equal(t, "# base\n^base-excluded$\n# history\n^history-excluded$\n", s.read("exclude-"+stepTrufflehogHistoryJSON+"@"+r.name()))
		noWork(t, r.dir, "trufflehog-history")
	})

	for name, c := range map[string]struct {
		rc   string
		want error
	}{
		"所見（183）は errFindings": {"183", errFindings},
		"1 は errScan":          {"1", errScan},
		"2 は errScan":          {"2", errScan},
	} {
		t.Run("本走査: "+name, func(t *testing.T) {
			t.Parallel()
			r, _ := rangeRepo(t)
			s := newScanner(t)
			s.passingCanaries()
			s.set(stepTrufflehogRange+"@"+r.name(), "rc", c.rc)
			require.ErrorIs(t, runTrufflehogGit([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), c.want)
		})
	}

	for name, c := range map[string]struct {
		key, out, rc string
		want         error
	}{
		"git の canary が検出しない":                {stepTrufflehogRangeJSON + "@canary-git", "", "0", errCanary},
		"git の canary が異常終了":                 {stepTrufflehogRangeJSON + "@canary-git", "", "1", errScan},
		"filesystem の canary が検出しない":         {stepTrufflehogFilesystemJSON + "@canary-fs", "", "0", errCanary},
		"filesystem の canary が JSON でない行を出す": {stepTrufflehogFilesystemJSON + "@canary-fs", "not json\n", "183", errScan},
		"AWS 以外の所見だけ":                        {stepTrufflehogFilesystemJSON + "@canary-fs", `{"DetectorName":"Slack"}` + "\n", "183", errCanary},
	} {
		t.Run(name+"なら、本走査を起動しない", func(t *testing.T) {
			t.Parallel()
			r, _ := rangeRepo(t)
			s := newScanner(t)
			s.passingCanaries()
			s.set(c.key, "out", c.out)
			s.set(c.key, "rc", c.rc)
			var stdout bytes.Buffer
			require.ErrorIs(t, runTrufflehogGit([]string{"-from", "HEAD~1"}, true, &stdout, &bytes.Buffer{}, r.runner(s)), c.want)
			for _, step := range s.steps() {
				assert.NotEqual(t, r.name()+" "+stepTrufflehogRange, step, "本走査を起動した")
			}
			assert.Empty(t, stdout.String())
			noWork(t, r.dir, "trufflehog-range")
		})
	}

	t.Run("範囲: 起点が空・解決できないなら、走査器を起動せずに落とす", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		for from, want := range map[string]error{"": errFromEmpty, "origin/does-not-exist": errFromUnresolved, "HEAD\nHEAD": errFromUnresolved} {
			s := newScanner(t)
			s.passingCanaries()
			require.ErrorIs(t, runTrufflehogGit([]string{"-from", from}, true, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), want, from)
			assert.Nil(t, s.calls(), "%q で走査器を起動した", from)
		}
	})

	t.Run("抑止のファイルが無ければ errInput で、走査器を起動しない", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		require.NoError(t, os.Remove(filepath.Join(r.dir, trufflehogExcludeHistory)))
		s := newScanner(t)
		require.ErrorIs(t, runTrufflehogGit([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), errInput)
		assert.Nil(t, s.calls())
	})

	t.Run("GIT_DIR が外側を指していても、外側を書き換えず、走査器へ GIT_DIR を渡さない", func(t *testing.T) {
		t.Parallel()
		r, from := rangeRepo(t)
		outer := newFixture(t)
		before := outer.snapshot()
		s := newScanner(t)
		s.passingCanaries()
		run := r.runner(s)
		run.environ = leaked(outer.dir)
		require.NoError(t, runTrufflehogGit([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, &bytes.Buffer{}, run))
		assert.Equal(t, before, outer.snapshot())
		calls := s.calls()
		assert.Equal(t, from, calls[2].rev)
		for _, c := range calls {
			assert.Empty(t, c.gitDir, "%s へ GIT_DIR を渡した", c.step)
		}
	})
}

func Test_trufflehogMain(t *testing.T) {
	t.Parallel()

	t.Run("stdout と stderr を渡し、終了コードを判定する", func(t *testing.T) {
		t.Parallel()
		s := newScanner(t)
		s.set("x", "out", "O\n")
		s.set("x", "err", "E\n")
		s.set("x", "rc", "183")
		var stdout, log bytes.Buffer
		require.ErrorIs(t, trufflehogMain(s.tool()(t.TempDir(), os.Environ(), "x", ""), &stdout, &log), errFindings)
		assert.Equal(t, "O\n", stdout.String())
		assert.Equal(t, "E\n", log.String())
	})

	t.Run("起動できなければ ErrTool", func(t *testing.T) {
		t.Parallel()
		cmd := exec.CommandContext(context.Background(), "/nonexistent/trufflehog")
		require.ErrorIs(t, trufflehogMain(cmd, &bytes.Buffer{}, &bytes.Buffer{}), errTool)
	})
}

func Test_trufflehogCanary(t *testing.T) {
	t.Parallel()

	t.Run("filesystem の経路の偽のキーを書けなければ errInput で、filesystem の経路を起動しない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "canary-fs"), nil, 0o600))
		err := trufflehogCanary(r.runner(s), os.Environ(), dir, stepTrufflehogRangeJSON, "", &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
		assert.Equal(t, []string{"canary-git " + stepTrufflehogRangeJSON}, s.steps())
	})

	t.Run("抑止の和集合が偽のキーのファイルに当たれば、filesystem の経路を起動せずに落とす", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		err := trufflehogCanary(r.runner(s), os.Environ(), t.TempDir(), stepTrufflehogRangeJSON, "^cred", &bytes.Buffer{})
		require.ErrorIs(t, err, errCanary)
		assert.Equal(t, []string{"canary-git " + stepTrufflehogRangeJSON}, s.steps())
	})

	t.Run("抑止の和集合を解釈できなければ errExclude", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		err := trufflehogCanary(r.runner(s), os.Environ(), t.TempDir(), stepTrufflehogRangeJSON, "(", &bytes.Buffer{})
		require.ErrorIs(t, err, errExclude)
	})

	t.Run("偽のキーは git の経路と filesystem の経路で別の乱数から作る", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		dir := t.TempDir()
		require.NoError(t, trufflehogCanary(r.runner(s), os.Environ(), dir, stepTrufflehogRangeJSON, "", &bytes.Buffer{}))
		a, err := os.ReadFile(filepath.Join(dir, "canary-git", canaryCredentials))
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(dir, "canary-fs", canaryCredentials))
		require.NoError(t, err)
		assert.NotEqual(t, string(a), string(b))
	})
}

func Test_trufflehogCanaryRun(t *testing.T) {
	t.Parallel()

	t.Run("落ちたときは stderr の末尾5行を log へ出し、stdout は出さない", func(t *testing.T) {
		t.Parallel()
		s := newScanner(t)
		s.set("x", "out", "SECRET-OUT\n")
		s.set("x", "err", "1\n2\n3\n4\n5\n6\n")
		s.set("x", "rc", "1")
		var log bytes.Buffer
		err := trufflehogCanaryRun(s.tool()(t.TempDir(), os.Environ(), "x", ""), "git", &log)
		require.ErrorIs(t, err, errScan)
		assert.Contains(t, log.String(), "2\n3\n4\n5\n6")
		assert.NotContains(t, log.String(), "1\n2")
		assert.NotContains(t, log.String(), "SECRET-OUT")
	})

	t.Run("シグナルで終われば ErrTool", func(t *testing.T) {
		t.Parallel()
		s := newScanner(t)
		s.set("x", "kill", "")
		require.ErrorIs(t, trufflehogCanaryRun(s.tool()(t.TempDir(), os.Environ(), "x", ""), "git", &bytes.Buffer{}), errTool)
	})
}

func Test_judgeTrufflehogCanary(t *testing.T) {
	t.Parallel()

	require.NoError(t, judgeTrufflehogCanary(183, trufflehogHit))
	require.NoError(t, judgeTrufflehogCanary(183, `{"DetectorName":"Slack"}`+"\n\n"+trufflehogHit))

	for name, c := range map[string]struct {
		rc   int
		out  string
		want error
	}{
		"検出しない（0）は errCanary":          {0, trufflehogHit, errCanary},
		"異常終了（1）は errScan":             {1, trufflehogHit, errScan},
		"AWS 以外だけは errCanary":          {183, `{"DetectorName":"Slack"}`, errCanary},
		"出力が空なのは errCanary":            {183, "", errCanary},
		"JSON でない行があるのは errScan":       {183, trufflehogHit + "not json\n", errScan},
		"DetectorName が無いのは errCanary": {183, `{"Detector":"AWS"}`, errCanary},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := judgeTrufflehogCanary(c.rc, c.out)
			require.ErrorIs(t, err, c.want)
			for _, other := range []error{errCanary, errScan} {
				if !xerrors.Is(other, c.want) {
					assert.NotErrorIs(t, err, other)
				}
			}
		})
	}
}

func Test_trufflehogVerdict(t *testing.T) {
	t.Parallel()

	require.NoError(t, trufflehogVerdict(0))
	require.ErrorIs(t, trufflehogVerdict(183), errFindings)
	require.ErrorIs(t, trufflehogVerdict(1), errScan)
	require.ErrorIs(t, trufflehogVerdict(182), errScan)
}

func Test_tail(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "b\nc", tail("a\nb\nc\n", 2))
	assert.Equal(t, "a\nb", tail("a\nb", 5))
	assert.Empty(t, tail("", 5))
}
