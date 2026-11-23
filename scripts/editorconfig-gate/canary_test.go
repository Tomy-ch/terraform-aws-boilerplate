package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	checkHit   = "\x1b[33;1mcanary.yaml:\x1b[33;0m\n\x1b[31;1m\t2: wrong indent style found (tabs instead of spaces)\x1b[33;0m\n"
	controlHit = "\x1b[33;1mcontrol.yaml:\x1b[33;0m\n\x1b[31;1m\t2: wrong indent style found (tabs instead of spaces)\x1b[33;0m\n"
)

// canaryPaths は、設定の複写元と fixture の置き場を一時ディレクトリ（symlink を解いた実パス）に用意します。
func canaryPaths(t *testing.T) fixturePaths {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return fixturePaths{
		work:          filepath.Join(dir, "work"),
		editorconfig:  writeFile(t, filepath.Join(dir, "ec"), "root = true\n"),
		checkerConfig: writeFile(t, filepath.Join(dir, "cc.json"), "{}\n"),
	}
}

// isolatedEnv は、印の GIT_DIR を持つ env です。
var isolatedEnv = []string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/isolated"}

func Test_runCheckCanary(t *testing.T) {
	t.Parallel()

	t.Run("fixture のリポジトリで違反を名指して落ちれば通り、隔離した env を渡し、fixture を消す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepCheck, "out", checkHit)
		f.set(stepCheck, "rc", "1")
		f.set(stepCheck, "ls", "")
		f.set(stepCheck, "copy", "")
		p := canaryPaths(t)
		work := p.work
		var out bytes.Buffer
		require.NoError(t, runCheckCanary(p, &out, f.tool(), isolatedEnv))
		assert.Contains(t, f.read("copied-canary.yaml"), "\n\t", "canary.yaml は tab で字下げする")
		assert.Contains(t, out.String(), "✅ editorconfig check-canary")
		assert.Equal(t, []string{
			work + " " + stepGitInit + " /isolated",
			work + " " + stepGitAdd + " /isolated",
			work + " " + stepCheck + " /isolated",
		}, f.calls())
		assert.Equal(t, ".editorconfig\n.editorconfig-checker.json\ncanary.yaml\n", f.read(stepCheck+".ls"))
		assert.NoDirExists(t, work)
	})

	for name, c := range map[string]struct{ out, rc, want string }{
		"道具が常に 0 で何も出さない": {rc: "0", want: "落ちませんでした"},
		"違反を名指しても rc=0":   {out: checkHit, rc: "0", want: "落ちませんでした"},
		"rc=1 で出力が空":      {rc: "1", want: "想定外"},
		"ファイル名だけ":         {out: "canary.yaml:\n", rc: "1", want: "想定外"},
		"違反の文言だけ":         {out: "wrong indent\n", rc: "1", want: "想定外"},
	} {
		t.Run(name+"なら errCanary で、成功を出さない", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.set(stepCheck, "out", c.out)
			f.set(stepCheck, "rc", c.rc)
			var out bytes.Buffer
			err := runCheckCanary(canaryPaths(t), &out, f.tool(), isolatedEnv)
			require.ErrorIs(t, err, errCanary)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, out.String(), "✅")
		})
	}

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runCheckCanary(canaryPaths(t), &bytes.Buffer{}, missingTool, isolatedEnv), errTool)
	})
}

func Test_runDirectiveCanary(t *testing.T) {
	t.Parallel()

	t.Run("対照だけを名指して落ちれば通り、印つきと対照のファイルを置く", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepCheck, "out", controlHit)
		f.set(stepCheck, "rc", "1")
		f.set(stepCheck, "ls", "")
		f.set(stepCheck, "copy", "")
		p := canaryPaths(t)
		work := p.work
		var out bytes.Buffer
		require.NoError(t, runDirectiveCanary(p, &out, f.tool(), isolatedEnv))
		assert.Contains(t, out.String(), "✅ editorconfig directive-canary")
		assert.Equal(t, ".editorconfig\n.editorconfig-checker.json\ncanary.yaml\ncontrol.yaml\n", f.read(stepCheck+".ls"))
		first, rest, ok := strings.Cut(f.read("copied-canary.yaml"), "\n")
		require.True(t, ok)
		assert.Equal(t, "# "+marker, first, "canary.yaml の1行目は印")
		control := f.read("copied-control.yaml")
		assert.NotContains(t, control, marker, "対照は印を持たない")
		assert.Equal(t, control, rest, "印の行を除けば対照と同じ")
		assert.Contains(t, control, "\t")
		assert.NoDirExists(t, work)
	})

	for name, c := range map[string]struct{ out, rc, want string }{
		"道具が常に 0 で何も出さない": {rc: "0", want: "走査が働いていません"},
		"印の後ろの違反を名指す":     {out: checkHit + controlHit, rc: "1", want: "分類を見直す"},
		"rc=1 で出力が空":      {rc: "1", want: "想定外"},
	} {
		t.Run(name+"なら errCanary で、成功を出さない", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.set(stepCheck, "out", c.out)
			f.set(stepCheck, "rc", c.rc)
			var out bytes.Buffer
			err := runDirectiveCanary(canaryPaths(t), &out, f.tool(), isolatedEnv)
			require.ErrorIs(t, err, errCanary)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, out.String(), "✅")
		})
	}
}

func Test_runCanary(t *testing.T) {
	t.Parallel()

	spec := canarySpec{files: map[string]string{"x.yaml": tabYAML}, judge: func(result) (bool, string) { return true, "" }, ok: "ok"}

	for _, step := range []string{stepGitInit, stepGitAdd} {
		t.Run(step+" が rc≠0 なら editorconfig-checker を起動せずに errTool", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.set(step, "rc", "128")
			require.ErrorIs(t, runCanary("x", spec, canaryPaths(t), &bytes.Buffer{}, f.tool(), isolatedEnv), errTool)
			for _, c := range f.calls() {
				assert.NotContains(t, c, " "+stepCheck+" ")
			}
		})
	}

	t.Run("不成立なら、道具の stdout・stderr と rc の見出しを出す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepCheck, "out", "CHECK-STDOUT\n")
		f.set(stepCheck, "err", "STDERR-ONLY\n")
		f.set(stepCheck, "rc", "3")
		failing := canarySpec{files: spec.files, judge: func(result) (bool, string) { return false, "x" }}
		var out bytes.Buffer
		require.ErrorIs(t, runCanary("x", failing, canaryPaths(t), &out, f.tool(), isolatedEnv), errCanary)
		assert.Contains(t, out.String(), "--- editorconfig-checker（rc=3）:")
		assert.Contains(t, out.String(), "CHECK-STDOUT")
		assert.Contains(t, out.String(), "STDERR-ONLY")
	})

	t.Run("editorconfig-checker がシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepCheck, "kill", "")
		require.ErrorIs(t, runCanary("x", spec, canaryPaths(t), &bytes.Buffer{}, f.tool(), isolatedEnv), errTool)
	})

	t.Run("設定を複写できなければ道具を起動しない", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		p := canaryPaths(t)
		require.NoError(t, os.WriteFile(p.checkerConfig, []byte(`{"Exclude":["x"]}`), 0o600))
		require.ErrorIs(t, runCanary("x", spec, p, &bytes.Buffer{}, f.tool(), isolatedEnv), errScope)
		assert.Empty(t, f.calls())
	})
}

func Test_buildFixture(t *testing.T) {
	t.Parallel()

	t.Run("作り直し、2つの設定の複写と files を置く", func(t *testing.T) {
		t.Parallel()
		p := canaryPaths(t)
		work := p.work
		stale := writeFile(t, filepath.Join(work, "stale"), "")
		require.NoError(t, buildFixture(work, p.editorconfig, p.checkerConfig, map[string]string{"a.yaml": "A"}))
		assert.NoFileExists(t, stale)
		for name, want := range map[string]string{".editorconfig": "root = true\n", ".editorconfig-checker.json": "{}\n", "a.yaml": "A"} {
			raw, err := os.ReadFile(filepath.Join(work, name))
			require.NoError(t, err)
			assert.Equal(t, want, string(raw), name)
		}
	})

	t.Run(".editorconfig が無い・空なら errRead", func(t *testing.T) {
		t.Parallel()
		p := canaryPaths(t)
		work := p.work
		require.ErrorIs(t, buildFixture(work, filepath.Join(work, "none"), p.checkerConfig, nil), errRead)
		require.ErrorIs(t, buildFixture(work, writeFile(t, filepath.Join(t.TempDir(), "e"), " \n"), p.checkerConfig, nil), errRead)
		assert.NoDirExists(t, work)
	})

	t.Run(".editorconfig-checker.json が {} でなければ errScope、無ければ errRead", func(t *testing.T) {
		t.Parallel()
		p := canaryPaths(t)
		require.ErrorIs(t, buildFixture(p.work, p.editorconfig, writeFile(t, filepath.Join(t.TempDir(), "c"), `{"a":1}`), nil), errScope)
		require.ErrorIs(t, buildFixture(p.work, p.editorconfig, filepath.Join(t.TempDir(), "none"), nil), errRead)
	})

	t.Run("置けなければ errRead", func(t *testing.T) {
		t.Parallel()
		p := canaryPaths(t)
		file := writeFile(t, filepath.Join(t.TempDir(), "f"), "")
		require.ErrorIs(t, buildFixture(filepath.Join(file, "w"), p.editorconfig, p.checkerConfig, nil), errRead)
	})
}

func Test_runInFixture(t *testing.T) {
	t.Parallel()

	t.Run("init・add・本走査の順に、同じ env で起動する", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepCheck, "out", "x")
		dir, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		r, err := runInFixture(dir, []string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/i"}, f.tool())
		require.NoError(t, err)
		assert.Equal(t, "x", r.combined)
		assert.Equal(t, []string{dir + " git-init /i", dir + " git-add /i", dir + " check /i"}, f.calls())
	})

	t.Run("git を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, err := runInFixture(t.TempDir(), nil, missingTool)
		require.ErrorIs(t, err, errTool)
	})
}

func Test_judgeCheck(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		r    result
		want bool
	}{
		"rc≠0・制御列つきでファイル名の後に違反": {result{combined: checkHit, code: 1}, true},
		"制御列が文言を割っても成り立つ":       {result{combined: "canary.yaml:\n\t2: wrong \x1b[0mindent style", code: 1}, true},
		"rc=2 でも名指せば成り立つ":       {result{combined: "canary.yaml: wrong indent", code: 2}, true},
		"rc=0":                  {result{combined: checkHit}, false},
		"出力が空":                  {result{code: 1}, false},
		"違反がファイル名より前":           {result{combined: "wrong indent\ncanary.yaml\n", code: 1}, false},
		"別のファイルの違反":             {result{combined: controlHit, code: 1}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, reason := judgeCheck(c.r)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.want, reason == "")
		})
	}
}

func Test_judgeDirective(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		r    result
		want string
	}{
		"対照だけなら成り立つ": {result{combined: controlHit, code: 1}, ""},
		"rc=0":       {result{combined: controlHit}, "走査が働いていません"},
		"印の後ろを名指す（対照より先に見る）": {result{combined: controlHit + checkHit, code: 1}, "分類を見直す"},
		"どちらも名指さない":          {result{combined: "x", code: 2}, "想定外"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ok, reason := judgeDirective(c.r)
			assert.Equal(t, c.want == "", ok)
			assert.True(t, strings.Contains(reason, c.want))
		})
	}
}

func Test_containsInOrder(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		s    string
		want bool
	}{
		"順に現れる": {"a x b", true},
		"逆順":    {"b x a", false},
		"片方だけ":  {"a", false},
		"重ならない": {"ab", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, containsInOrder(c.s, "a", "b"))
		})
	}
}
