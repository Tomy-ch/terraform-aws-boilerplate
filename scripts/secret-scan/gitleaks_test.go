package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitrev"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// rangeRepo は、起点（HEAD~1）の後ろにテキストの commit を1つ持つリポジトリと、起点の sha である。
func rangeRepo(t *testing.T) (*repo, string) {
	t.Helper()
	r := newRepo(t)
	from := r.head()
	r.write("a", "a\n")
	r.commit("a")

	return r, from
}

func Test_runGitleaks(t *testing.T) {
	t.Parallel()

	t.Run("全履歴: GIT_DIR が外側を指していても、外側を書き換えず、走査器へ GIT_DIR を渡さない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		outer := newFixture(t)
		before := outer.snapshot()
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksHistory+"@"+r.name(), "err", "INF 1 commits scanned.\n")
		run := r.runner(s)
		run.environ = leaked(outer.dir)
		require.NoError(t, runGitleaks(nil, false, &bytes.Buffer{}, run))
		assert.Equal(t, before, outer.snapshot(), "外側のリポジトリが書き換わった")
		calls := s.calls()
		require.Len(t, calls, 3)
		for _, c := range calls {
			assert.Empty(t, c.gitDir, "%s へ GIT_DIR を渡した", c.step)
		}
	})

	t.Run("本走査の stdout の報告は log へ出さない", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksRange+"@"+r.name(), "err", "INF 1 commits scanned.\n")
		s.set(stepGitleaksRange+"@"+r.name(), "out", "MAIN-REPORT\n")
		var log bytes.Buffer
		require.NoError(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &log, r.runner(s)))
		assert.NotContains(t, log.String(), "MAIN-REPORT")
	})

	t.Run("範囲: 空の範囲は 0 commits の走査で通す", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksRange+"@"+r.name(), "err", "INF 0 commits scanned.\n")
		require.NoError(t, runGitleaks([]string{"-from", "HEAD"}, true, &bytes.Buffer{}, r.runner(s)))
		assert.Len(t, s.calls(), 3)
	})

	t.Run("範囲: 本走査の numstat を取れなければ errInput で、本走査を起動しない", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		run := r.runner(s)
		run.git = failingGit(stepNumstatRange, r.dir)
		require.ErrorIs(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, run), errInput)
		assert.Equal(t, []string{"canary " + stepGitleaksRange, "canary " + stepGitleaksHistory}, s.steps())
	})

	t.Run("canary の出力に数の行が無ければ errNoCount で、本走査を起動しない", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksRange+"@canary", "err", "ERR boom\n")
		require.ErrorIs(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, r.runner(s)), errNoCount)
		assert.Len(t, s.calls(), 1)
	})

	t.Run("canary で走査器が異常終了すれば errScan で、本走査を起動しない", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksHistory+"@canary", "rc", "2")
		err := runGitleaks([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, r.runner(s))
		require.ErrorIs(t, err, errScan)
		assert.NotErrorIs(t, err, errCanary)
		assert.Len(t, s.calls(), 2)
	})

	t.Run("本走査だけがシグナルで終われば ErrTool", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksRange+"@"+r.name(), "kill", "")
		require.ErrorIs(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, r.runner(s)), errTool)
		assert.Len(t, s.calls(), 3, "canary は通っている")
	})

	t.Run("作業ディレクトリが tmp/ の外を指せば errUsage で、走査器を起動しない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		escapeWork(t, r.dir)
		s := newScanner(t)
		require.ErrorIs(t, runGitleaks(nil, false, &bytes.Buffer{}, r.runner(s)), errUsage)
		assert.Nil(t, s.calls())
	})

	t.Run("範囲: canary を範囲と全履歴で走らせてから、解決した sha で本走査を root で起動する", func(t *testing.T) {
		t.Parallel()
		r, from := rangeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksRange+"@"+r.name(), "err", "MAIN-LOG 1 commits scanned.\n")
		var log bytes.Buffer
		require.NoError(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &log, r.runner(s)))

		assert.Equal(t, []string{"canary " + stepGitleaksRange, "canary " + stepGitleaksHistory, r.name() + " " + stepGitleaksRange}, s.steps(),
			"canary は本走査と同じ段（引数）を、作業ディレクトリだけ替えて起動する")
		calls := s.calls()
		assert.Equal(t, from, calls[2].rev, "本走査は起点を解決した完全な sha を受ける")
		assert.True(t, gitrev.IsHash(calls[0].rev))
		assert.NotEqual(t, from, calls[0].rev, "canary は canary のリポジトリの起点を受ける")
		assert.Empty(t, calls[1].rev)
		assert.Equal(t, "# config\n# ignore\n", s.read("config-"+stepGitleaksRange+"@canary"), "canary は root の設定と抑止の写しを読む")
		assert.Contains(t, log.String(), "✅ gitleaks の canary")
		assert.Contains(t, log.String(), "MAIN-LOG", "本走査の出力を log へ流す")
		noWork(t, r.dir, "gitleaks-range")
	})

	t.Run("全履歴: 本走査は rev を受けず、1件以上の走査を通す", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksHistory+"@"+r.name(), "err", "INF 1 commits scanned.\n")
		require.NoError(t, runGitleaks(nil, false, &bytes.Buffer{}, r.runner(s)))
		calls := s.calls()
		require.Len(t, calls, 3)
		assert.Equal(t, call{r.name(), stepGitleaksHistory, "", ""}, calls[2])
		noWork(t, r.dir, "gitleaks-history")
	})

	for name, c := range map[string]struct {
		ranged bool
		err    string
		rc     string
		want   error
	}{
		"範囲: 走査が0件なら errMismatch":      {true, "INF 0 commits scanned.\n", "0", errMismatch},
		"範囲: 範囲より多い走査は errMismatch":    {true, "INF 2 commits scanned.\n", "0", errMismatch},
		"範囲: 数の行が無ければ errNoCount":      {true, "ERR boom\n", "1", errNoCount},
		"範囲: 所見（終了コード 1）は errFindings": {true, "INF 1 commits scanned.\n", "1", errFindings},
		"範囲: それ以外の終了コードは errScan":      {true, "INF 1 commits scanned.\n", "3", errScan},
		"全履歴: 走査が0件なら errMismatch":     {false, "INF 0 commits scanned.\n", "0", errMismatch},
		"全履歴: 所見は errFindings":         {false, "INF 9 commits scanned.\n", "1", errFindings},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, _ := rangeRepo(t)
			s := newScanner(t)
			s.passingCanaries()
			step, args := stepGitleaksHistory, []string(nil)
			if c.ranged {
				step, args = stepGitleaksRange, []string{"-from", "HEAD~1"}
			}
			s.set(step+"@"+r.name(), "err", c.err)
			s.set(step+"@"+r.name(), "rc", c.rc)
			var log bytes.Buffer
			err := runGitleaks(args, c.ranged, &log, r.runner(s))
			require.ErrorIs(t, err, c.want)
			assert.Contains(t, log.String(), c.err, "落ちるときも本走査の出力を先に log へ流す")
		})
	}

	t.Run("範囲: 起点が空・解決できない・`-` で始まるなら、走査器を起動せずに落とす", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		for from, want := range map[string]error{"": errFromEmpty, "origin/does-not-exist": errFromUnresolved, "-x": errFromUnresolved} {
			s := newScanner(t)
			s.passingCanaries()
			err := runGitleaks([]string{"-from", from}, true, &bytes.Buffer{}, r.runner(s))
			require.ErrorIs(t, err, want, from)
			assert.Nil(t, s.calls(), "%q で走査器を起動した", from)
		}
	})

	for name, c := range map[string]struct {
		step, out, rc, err string
	}{
		"範囲の canary が検出しない":        {stepGitleaksRange, "[]", "0", "INF 1 commits scanned.\n"},
		"全履歴の canary が検出しない":       {stepGitleaksHistory, "[]", "0", "INF 1 commits scanned.\n"},
		"canary の走査した数が合わない":       {stepGitleaksRange, gitleaksHit, "1", "INF 0 commits scanned.\n"},
		"canary の報告に AWS キーの所見が無い": {stepGitleaksHistory, `[{"RuleID": "generic-api-key"}]`, "1", "INF 1 commits scanned.\n"},
	} {
		t.Run(name+"なら、本走査を起動せず、canary の出力を log にだけ出す", func(t *testing.T) {
			t.Parallel()
			r, _ := rangeRepo(t)
			s := newScanner(t)
			s.passingCanaries()
			s.behave(c.step, "canary", c.out, c.rc)
			s.set(c.step+"@canary", "err", c.err+"CANARY-LOG\n")
			var log bytes.Buffer
			err := runGitleaks([]string{"-from", "HEAD~1"}, true, &log, r.runner(s))
			require.ErrorIs(t, err, errCanary)
			for _, step := range s.steps() {
				assert.NotEqual(t, r.name()+" "+stepGitleaksRange, step, "本走査を起動した")
			}
			assert.Contains(t, log.String(), "CANARY-LOG")
			noWork(t, r.dir, "gitleaks-range")
		})
	}

	t.Run("設定の複写元が無ければ errInput で、走査器を起動しない", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		require.NoError(t, os.Remove(filepath.Join(r.dir, gitleaksIgnore)))
		s := newScanner(t)
		require.ErrorIs(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, r.runner(s)), errInput)
		assert.Nil(t, s.calls())
	})

	t.Run("走査器がシグナルで終われば ErrTool", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		s := newScanner(t)
		s.set(stepGitleaksRange, "kill", "")
		require.ErrorIs(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, r.runner(s)), errTool)
	})

	t.Run("GIT_DIR が外側を指していても、root を読み、外側を書き換えず、走査器へ GIT_DIR を渡さない", func(t *testing.T) {
		t.Parallel()
		r, from := rangeRepo(t)
		outer := newFixture(t)
		before := outer.snapshot()
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksRange+"@"+r.name(), "err", "INF 1 commits scanned.\n")
		run := r.runner(s)
		run.environ = leaked(outer.dir)
		require.NoError(t, runGitleaks([]string{"-from", "HEAD~1"}, true, &bytes.Buffer{}, run))
		assert.Equal(t, before, outer.snapshot(), "外側のリポジトリが書き換わった")
		calls := s.calls()
		assert.Equal(t, from, calls[2].rev, "起点を root で解決した")
		for _, c := range calls {
			assert.Empty(t, c.gitDir, "%s へ GIT_DIR を渡した", c.step)
		}
	})

	t.Run("位置引数と知らない flag は errUsage で、走査器を起動しない", func(t *testing.T) {
		t.Parallel()
		r, _ := rangeRepo(t)
		s := newScanner(t)
		require.ErrorIs(t, runGitleaks([]string{"x"}, false, &bytes.Buffer{}, r.runner(s)), errUsage)
		require.ErrorIs(t, runGitleaks([]string{"-from", "HEAD"}, false, &bytes.Buffer{}, r.runner(s)), errUsage)
		assert.Nil(t, s.calls())
	})
}

func Test_gitleaksCanary(t *testing.T) {
	t.Parallel()

	t.Run("偽の AWS キーを commit したリポジトリへ root の設定を写し、範囲と全履歴を走らせる", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		dir := t.TempDir()
		var log bytes.Buffer
		require.NoError(t, gitleaksCanary(r.runner(s), os.Environ(), dir, &log))
		creds, err := os.ReadFile(filepath.Join(dir, "canary", canaryCredentials))
		require.NoError(t, err)
		assert.Regexp(t, `aws_access_key_id = AK`+`IA[A-Z2-7]{16}\n`, string(creds))
		assert.Equal(t, []string{"canary " + stepGitleaksRange, "canary " + stepGitleaksHistory}, s.steps())
		assert.Contains(t, log.String(), "✅")
	})

	t.Run("落ちたときは段の名前と出力を添える", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepGitleaksRange+"@canary", "rc", "0")
		var log bytes.Buffer
		err := gitleaksCanary(r.runner(s), os.Environ(), t.TempDir(), &log)
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "範囲")
		assert.Contains(t, log.String(), "1 commits scanned")
		assert.Len(t, s.calls(), 1, "範囲で落ちたら全履歴を走らせない")
	})
}

func Test_judgeGitleaksCanary(t *testing.T) {
	t.Parallel()

	require.NoError(t, judgeGitleaksCanary(1, []byte(gitleaksHit), nil))
	require.NoError(t, judgeGitleaksCanary(1, []byte(`[{"RuleID":"x"},{"RuleID":"aws-access-token"}]`), nil))

	for name, c := range map[string]struct {
		rc       int
		report   string
		countErr error
		want     error
	}{
		"検出しない（0）は errCanary":      {0, gitleaksHit, nil, errCanary},
		"異常終了（2）は errScan":         {2, gitleaksHit, nil, errScan},
		"数が合わないのは errCanary":       {1, gitleaksHit, xerrors.Wrap(errMismatch, "x"), errCanary},
		"数の行が無いのは errNoCount のまま":  {1, gitleaksHit, xerrors.Wrap(errNoCount, "x"), errNoCount},
		"報告が空なのは errScan":          {1, "", nil, errScan},
		"報告が壊れているのは errScan":       {1, "[{", nil, errScan},
		"報告が配列でないのは errScan":       {1, `{"RuleID":"aws-access-token"}`, nil, errScan},
		"報告が空の配列なのは errCanary":     {1, "[]", nil, errCanary},
		"AWS キーの所見が無いのは errCanary": {1, `[{"RuleID":"generic-api-key"}]`, nil, errCanary},
		"RuleID の欄が無いのは errCanary": {1, `[{"Rule":"aws-access-token"}]`, nil, errCanary},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := judgeGitleaksCanary(c.rc, []byte(c.report), c.countErr)
			require.ErrorIs(t, err, c.want)
			for _, other := range []error{errCanary, errScan, errNoCount} {
				if !xerrors.Is(other, c.want) {
					assert.NotErrorIs(t, err, other)
				}
			}
		})
	}
}

func Test_gitleaksVerdict(t *testing.T) {
	t.Parallel()

	require.NoError(t, gitleaksVerdict(0))
	require.ErrorIs(t, gitleaksVerdict(1), errFindings)
	require.ErrorIs(t, gitleaksVerdict(2), errScan)
	require.ErrorIs(t, gitleaksVerdict(126), errScan)
}
