package main

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

// hit は、editorconfig-checker が file の字下げの違反を名指す出力です。
func hit(file string) string {
	return "\x1b[33;1m" + file + ":\x1b[33;0m\n\x1b[31;1m\t2: wrong indent style found (tabs instead of spaces)\x1b[33;0m\n"
}

var (
	checkHit   = hit(canaryFile) + hit(nonASCIIFile)
	controlHit = hit("control.yaml")
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

var isolatedEnv = []string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/isolated"}

func Test_runCheckCanary(t *testing.T) {
	t.Parallel()

	t.Run("fixture のリポジトリで違反を名指して落ちれば通り、隔離した env を渡し、fixture を消す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepCheck, "out", checkHit)
		f.Set(stepCheck, "rc", "1")
		f.Set(stepCheck, "ls", "")
		f.Set(stepCheck, "copy", "")
		p := canaryPaths(t)
		work := p.work
		var out bytes.Buffer
		require.NoError(t, runCheckCanary(p, &out, f.Tool(), isolatedEnv))
		assert.Contains(t, f.Read("copied-canary.yaml"), "\n\t", "canary.yaml は tab で字下げする")
		assert.Equal(t, f.Read("copied-canary.yaml"), f.Read("copied-"+path.Base(nonASCIIFile)), "非 ASCII の名前のファイルも同じ違反を持つ")
		assert.Contains(t, out.String(), "✅ editorconfig check-canary")
		assert.Equal(t, []string{
			work + " " + stepGitInit + " /isolated",
			work + " " + stepGitAdd + " /isolated",
			work + " " + stepCheck + " /isolated",
		}, f.Calls())
		assert.Equal(t, ".editorconfig\n.editorconfig-checker.json\ncanary.yaml\nnested\n", f.Read(stepCheck+".ls"))
		assert.NoDirExists(t, work)
	})

	for name, c := range map[string]struct{ out, rc, want string }{
		"道具が常に 0 で何も出さない":        {rc: "0", want: "落ちませんでした"},
		"違反を名指しても rc=0":          {out: checkHit, rc: "0", want: "落ちませんでした"},
		"rc=1 で出力が空":             {rc: "1", want: "想定外"},
		"ファイル名だけ":                {out: "canary.yaml:\n", rc: "1", want: "想定外"},
		"違反の文言だけ":                {out: "wrong indent\n", rc: "1", want: "想定外"},
		"非 ASCII の名前のファイルを名指さない": {out: hit(canaryFile), rc: "1", want: "core.quotepath"},
	} {
		t.Run(name+"なら errCanary で、成功を出さない", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.Set(stepCheck, "out", c.out)
			f.Set(stepCheck, "rc", c.rc)
			var out bytes.Buffer
			err := runCheckCanary(canaryPaths(t), &out, f.Tool(), isolatedEnv)
			require.ErrorIs(t, err, errCanary)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, out.String(), "✅")
		})
	}

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runCheckCanary(canaryPaths(t), &bytes.Buffer{}, tooltest.Missing, isolatedEnv), errTool)
	})
}

func Test_runDirectiveCanary(t *testing.T) {
	t.Parallel()

	t.Run("対照だけを名指して落ちれば通り、印つきと対照のファイルを置く", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepCheck, "out", controlHit)
		f.Set(stepCheck, "rc", "1")
		f.Set(stepCheck, "ls", "")
		f.Set(stepCheck, "copy", "")
		p := canaryPaths(t)
		work := p.work
		var out bytes.Buffer
		require.NoError(t, runDirectiveCanary(p, &out, f.Tool(), isolatedEnv))
		assert.Contains(t, out.String(), "✅ editorconfig directive-canary")
		assert.Equal(t, ".editorconfig\n.editorconfig-checker.json\ncanary.yaml\ncontrol.yaml\n", f.Read(stepCheck+".ls"))
		first, rest, ok := strings.Cut(f.Read("copied-canary.yaml"), "\n")
		require.True(t, ok)
		assert.Equal(t, "# "+marker, first, "canary.yaml の1行目は印")
		control := f.Read("copied-control.yaml")
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
			f.Set(stepCheck, "out", c.out)
			f.Set(stepCheck, "rc", c.rc)
			var out bytes.Buffer
			err := runDirectiveCanary(canaryPaths(t), &out, f.Tool(), isolatedEnv)
			require.ErrorIs(t, err, errCanary)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, out.String(), "✅")
		})
	}
}

// excludeListed は、exclude-canary の fixture で -dry-run が対照だけを載せた出力です。
const excludeListed = "\x1b[0m.editorconfig\n./.editorconfig-checker.json\n" + excludeDir + "/" + excludeControl + "\n" + excludeDir + "/" + excludeControlNonASCII + "\n"

func Test_runExcludeCanary(t *testing.T) {
	t.Parallel()

	t.Run("対照が載り宣言した除外が載らなければ通り、-dry-run を隔離した env で起動して fixture を消す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepDryRun, "out", excludeListed)
		p := canaryPaths(t)
		work := p.work
		var out bytes.Buffer
		require.NoError(t, runExcludeCanary(p, &out, f.Tool(), isolatedEnv))
		assert.Contains(t, out.String(), "✅ editorconfig exclude-canary")
		assert.Equal(t, []string{
			work + " " + stepGitInit + " /isolated",
			work + " " + stepGitAdd + " /isolated",
			work + " " + stepDryRun + " /isolated",
		}, f.Calls())
		assert.NoDirExists(t, work)
	})

	for _, n := range excludedNames {
		t.Run("宣言した除外 "+n+" が -dry-run に載れば、それを名指して errCanary", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.Set(stepDryRun, "out", excludeListed+"./"+excludeDir+"/"+n+"\n")
			var out bytes.Buffer
			err := runExcludeCanary(canaryPaths(t), &out, f.Tool(), isolatedEnv)
			require.ErrorIs(t, err, errCanary)
			assert.Contains(t, err.Error(), n)
			assert.NotContains(t, out.String(), "✅")
		})
	}

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runExcludeCanary(canaryPaths(t), &bytes.Buffer{}, tooltest.Missing, isolatedEnv), errTool)
	})
}

func Test_excludeCanaryFiles(t *testing.T) {
	t.Parallel()

	files := excludeCanaryFiles()
	require.Len(t, files, len(excludedNames)+2)
	for _, n := range append([]string{excludeControl, excludeControlNonASCII}, excludedNames...) {
		body, ok := files[excludeDir+"/"+n]
		require.True(t, ok, n)
		assert.Equal(t, files[excludeDir+"/"+excludeControl], body, "宣言した除外と対照は名前だけが違う: %s", n)
	}
	assert.NotContains(t, excludedNames, excludeControl, "対照は宣言に無い名前")
	assert.NotContains(t, excludedNames, excludeControlNonASCII, "対照は宣言に無い名前")
	assert.NotEqual(t, strconv.QuoteToASCII(excludeControlNonASCII), strconv.Quote(excludeControlNonASCII), "非 ASCII の対照は ASCII でない文字を含む")
	assert.NotEqual(t, strconv.QuoteToASCII(nonASCIIFile), strconv.Quote(nonASCIIFile), "check-canary の非 ASCII のファイルは ASCII でない文字を含む")
	assert.Contains(t, excludeDir, "/", "入れ子は2段以上")
}

func Test_judgeExclude(t *testing.T) {
	t.Parallel()

	control := excludeDir + "/" + excludeControl
	for name, c := range map[string]struct {
		r    toolrun.Result
		want string
	}{
		"対照だけが載れば成り立つ":        {toolrun.Result{Stdout: excludeListed}, ""},
		"rc≠0":                {toolrun.Result{Stdout: excludeListed, Code: 1}, "rc=1"},
		"何も載らない":              {toolrun.Result{}, "列挙が働いていません"},
		"対照が stderr にだけ載る":    {toolrun.Result{Combined: control + "\n"}, "列挙が働いていません"},
		"非 ASCII の対照だけが載らない":  {toolrun.Result{Stdout: control + "\n"}, "core.quotepath"},
		"全部が載る":               {toolrun.Result{Stdout: excludeListed + excludeDir + "/" + excludedNames[0] + "\n" + excludeDir + "/" + excludedNames[len(excludedNames)-1] + "\n"}, excludedNames[len(excludedNames)-1]},
		"同じ名前の別の段は除外の判定に使わない": {toolrun.Result{Stdout: excludeListed + excludedNames[0] + "\n"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ok, reason := judgeExclude(c.r)
			assert.Equal(t, c.want == "", ok)
			assert.Contains(t, reason, c.want)
			if c.want == "core.quotepath" {
				assert.NotContains(t, reason, "列挙が働いていません", "ASCII の対照は載っている")
			}
		})
	}

	t.Run("載った宣言の名前をすべて名指す", func(t *testing.T) {
		t.Parallel()
		var out strings.Builder
		out.WriteString(excludeListed)
		for _, n := range excludedNames {
			out.WriteString(excludeDir + "/" + n + "\n")
		}
		ok, reason := judgeExclude(toolrun.Result{Stdout: out.String()})
		assert.False(t, ok)
		for _, n := range excludedNames {
			assert.Contains(t, reason, n)
		}
	})
}

func Test_runCanary(t *testing.T) {
	t.Parallel()

	spec := canarySpec{files: map[string]string{"x.yaml": tabYAML}, step: stepCheck, judge: func(toolrun.Result) (bool, string) { return true, "" }, ok: "ok"}

	for _, step := range []string{stepGitInit, stepGitAdd} {
		t.Run(step+" が rc≠0 なら editorconfig-checker を起動せずに errTool", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.Set(step, "rc", "128")
			require.ErrorIs(t, runCanary("x", spec, canaryPaths(t), &bytes.Buffer{}, f.Tool(), isolatedEnv), errTool)
			for _, c := range f.Calls() {
				assert.NotContains(t, c, " "+stepCheck+" ")
			}
		})
	}

	t.Run("不成立なら、道具の stdout・stderr と rc の見出しを出す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepCheck, "out", "CHECK-STDOUT\n")
		f.Set(stepCheck, "err", "STDERR-ONLY\n")
		f.Set(stepCheck, "rc", "3")
		failing := canarySpec{files: spec.files, step: stepCheck, judge: func(toolrun.Result) (bool, string) { return false, "x" }}
		var out bytes.Buffer
		require.ErrorIs(t, runCanary("x", failing, canaryPaths(t), &out, f.Tool(), isolatedEnv), errCanary)
		assert.Contains(t, out.String(), "--- editorconfig-checker（rc=3）:")
		assert.Contains(t, out.String(), "CHECK-STDOUT")
		assert.Contains(t, out.String(), "STDERR-ONLY")
	})

	t.Run("editorconfig-checker がシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepCheck, "kill", "")
		require.ErrorIs(t, runCanary("x", spec, canaryPaths(t), &bytes.Buffer{}, f.Tool(), isolatedEnv), errTool)
	})

	t.Run("設定を複写できなければ道具を起動しない", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		p := canaryPaths(t)
		require.NoError(t, os.WriteFile(p.checkerConfig, []byte(`{"Exclude":["x"]}`), 0o600))
		require.ErrorIs(t, runCanary("x", spec, p, &bytes.Buffer{}, f.Tool(), isolatedEnv), errScope)
		assert.Empty(t, f.Calls())
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

	t.Run("入れ子のパスはディレクトリを作って置く", func(t *testing.T) {
		t.Parallel()
		p := canaryPaths(t)
		require.NoError(t, buildFixture(p.work, p.editorconfig, p.checkerConfig, map[string]string{"n/d/go.mod": "x\n"}))
		raw, err := os.ReadFile(filepath.Join(p.work, "n", "d", "go.mod"))
		require.NoError(t, err)
		assert.Equal(t, "x\n", string(raw))
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

	for _, step := range []string{stepCheck, stepDryRun} {
		t.Run("init・add・"+step+" の順に、同じ env で起動する", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.Set(step, "out", "x")
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			r, err := runInFixture(dir, []string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/i"}, f.Tool(), step)
			require.NoError(t, err)
			assert.Equal(t, "x", r.Combined)
			assert.Equal(t, []string{dir + " git-init /i", dir + " git-add /i", dir + " " + step + " /i"}, f.Calls())
		})
	}

	t.Run("git を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, err := runInFixture(t.TempDir(), nil, tooltest.Missing, stepCheck)
		require.ErrorIs(t, err, errTool)
	})
}

func Test_judgeCheck(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		r    toolrun.Result
		want string
	}{
		"rc≠0・制御列つきで2つのファイルの違反を名指す": {toolrun.Result{Combined: checkHit, Code: 1}, ""},
		"制御列が文言を割っても成り立つ":           {toolrun.Result{Combined: "canary.yaml:\n\t2: wrong \x1b[0mindent style\n" + nonASCIIFile + ":\n\t2: wrong indent\n", Code: 1}, ""},
		"rc=2 でも名指せば成り立つ":           {toolrun.Result{Combined: checkHit, Code: 2}, ""},
		"rc=0":                      {toolrun.Result{Combined: checkHit}, "落ちませんでした"},
		"出力が空":                      {toolrun.Result{Code: 1}, "想定外"},
		"見出しの無い違反":                  {toolrun.Result{Combined: "\t2: wrong indent\ncanary.yaml:\n", Code: 1}, "想定外"},
		"別のファイルの違反":                 {toolrun.Result{Combined: controlHit, Code: 1}, "想定外"},
		"非 ASCII の名前のファイルを名指さない":    {toolrun.Result{Combined: hit(canaryFile), Code: 1}, "core.quotepath"},
		"非 ASCII の名前のファイルだけを名指す":    {toolrun.Result{Combined: hit(nonASCIIFile), Code: 1}, "想定外"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ok, reason := judgeCheck(c.r)
			assert.Equal(t, c.want == "", ok)
			assert.Contains(t, reason, c.want)
		})
	}
}

func Test_flaggedFiles(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		out  string
		want map[string]bool
	}{
		"空": {"", map[string]bool{}},
		"見出しの後の違反の行で数える":            {"a.yaml:\n\t2: wrong indent style\nb.md:\n\t3: x\n", map[string]bool{"a.yaml": true}},
		"非 ASCII と空白を含む見出し":         {"d/ünï x.yaml:\n\t2: wrong indent\n", map[string]bool{"d/ünï x.yaml": true}},
		"見出しの無い違反は数えない":             {"\t2: wrong indent\n", map[string]bool{}},
		"字下げした行は見出しにしない":            {"a.yaml:\n\tx:\n\t2: wrong indent\n", map[string]bool{"a.yaml": true}},
		"末尾が : でない字下げの無い行は見出しを替えない": {"a.yaml:\nsummary line\n\t2: wrong indent\n", map[string]bool{"a.yaml": true}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, flaggedFiles(c.out))
		})
	}
}

func Test_judgeDirective(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		r    toolrun.Result
		want string
	}{
		"対照だけなら成り立つ": {toolrun.Result{Combined: controlHit, Code: 1}, ""},
		"rc=0":       {toolrun.Result{Combined: controlHit}, "走査が働いていません"},
		"印の後ろを名指す（対照より先に見る）": {toolrun.Result{Combined: controlHit + checkHit, Code: 1}, "分類を見直す"},
		"どちらも名指さない":          {toolrun.Result{Combined: "x", Code: 2}, "想定外"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ok, reason := judgeDirective(c.r)
			assert.Equal(t, c.want == "", ok)
			assert.True(t, strings.Contains(reason, c.want))
		})
	}
}
