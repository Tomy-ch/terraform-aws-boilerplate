package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

const (
	trivyCanaryJSON = `{"Results":[{"Target":"Dockerfile","Misconfigurations":[{"ID":"DS-0001","Severity":"MEDIUM"}]}]}`
	trivyMainJSON   = `{"Results":[{"Target":"usecases/a/reference","Type":"terraform"},{"Target":"modules/b/main.tf","Type":"terraform"},` +
		`{"Target":"env/c","Type":"terraform"},{"Target":"docker/Dockerfile","Type":"dockerfile"}]}`
	trivySev = "CRITICAL,HIGH,MEDIUM"
)

func Test_runTrivyConfig(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, mainRC int, mainJSON string) (*fake, runner, string) {
		t.Helper()
		f, r := newFake(t)
		f.scenario("trivy-config-canary", 1, trivyCanaryJSON, nil)
		f.scenario("default", mainRC, mainJSON, nil)
		return f, r, tree(t, map[string]string{
			".trivyignore.yaml":  "misconfigurations: []\n",
			"usecases/a/main.tf": "",
			"modules/b/main.tf":  "",
			"env/c/main.tf":      "",
			"docker/Dockerfile":  "",
		})
	}
	args := func(base string, extra ...string) []string {
		return append(append([]string{"-base", base, "-severity", trivySev}, extra...), "usecases", "modules", "env")
	}
	noScratch := func(t *testing.T, base string) {
		t.Helper()
		left, err := os.ReadDir(filepath.Join(base, "tmp", "trivy-config"))
		require.NoError(t, err)
		assert.Empty(t, left)
	}

	t.Run("canary が通れば json で本走査を起動し、要求された形へ変えたものだけを stdout へ流す", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		var stdout, log bytes.Buffer
		require.NoError(t, runTrivyConfig(args(base, "-format", "sarif"), &stdout, &log, r))
		assert.Equal(t, []string{
			"trivy-config-canary trivy-config/" + trivySev,
			filepath.Base(base) + " trivy-config/" + trivySev,
			"trivy-config-convert trivy-convert/sarif",
		}, f.calls())
		assert.Equal(t, "file://"+base+"/", rootPathOf(t, stdout.Bytes()), "ROOTPATH を base へ向け直す")
		assert.Contains(t, log.String(), "trivy-stderr "+filepath.Base(base), "本走査の stderr は log へ流す")
		assert.NotContains(t, log.String(), "trivy-stderr trivy-config-canary", "canary の出力は通ったときに出さない")
		noScratch(t, base)
	})

	t.Run("json の本走査は変換せずにそのまま stdout へ流す", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		var stdout bytes.Buffer
		require.NoError(t, runTrivyConfig(args(base, "-format", "json"), &stdout, &bytes.Buffer{}, r))
		assert.Equal(t, []string{
			"trivy-config-canary trivy-config/" + trivySev,
			filepath.Base(base) + " trivy-config/" + trivySev,
		}, f.calls())
		assert.Equal(t, trivyMainJSON, stdout.String())
	})

	t.Run("-format の既定は table", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		var stdout bytes.Buffer
		require.NoError(t, runTrivyConfig(args(base), &stdout, &bytes.Buffer{}, r))
		assert.Equal(t, "trivy-config-convert trivy-convert/table", f.calls()[2])
		assert.Equal(t, "converted table:"+trivyMainJSON, stdout.String())
	})

	t.Run("最大でない閾値も、その閾値の段へ写す", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		in := []string{"-base", base, "-severity", "CRITICAL,HIGH,MEDIUM,LOW", "-format", "json", "env"}
		require.NoError(t, runTrivyConfig(in, &bytes.Buffer{}, &bytes.Buffer{}, r))
		assert.Equal(t, []string{
			"trivy-config-canary trivy-config/CRITICAL,HIGH,MEDIUM,LOW",
			filepath.Base(base) + " trivy-config/CRITICAL,HIGH,MEDIUM,LOW",
		}, f.calls())
	})

	t.Run("canary は tmp の下の一時の置き場で、抑止の宣言を写して走る", func(t *testing.T) {
		t.Parallel()
		var seen, dir string
		tool := func(ctx context.Context, d string, env []string, _ string, _ ...string) *exec.Cmd {
			if strings.HasPrefix(filepath.Base(d), "trivy-config-canary.") {
				dir = d
				raw, err := os.ReadFile(filepath.Join(d, ".trivyignore.yaml"))
				require.NoError(t, err)
				seen = string(raw)
				docker, err := os.ReadFile(filepath.Join(d, "Dockerfile"))
				require.NoError(t, err)
				assert.Equal(t, trivyCanary, string(docker))
			}
			c := exec.CommandContext(ctx, "sh", "-c", `echo '`+trivyCanaryJSON+`'; exit 1`)
			c.Dir, c.Env = d, env
			return c
		}
		_, _, base := setup(t, 0, trivyMainJSON)
		require.NoError(t, os.WriteFile(filepath.Join(base, ".trivyignore.yaml"), []byte("misconfigurations: [x]\n"), 0o600))
		err := runTrivyConfig(args(base, "-format", "json"), &bytes.Buffer{}, &bytes.Buffer{}, runner{tool: tool, environ: os.Environ()})
		require.ErrorIs(t, err, errNoTargets, "本走査の結果に Terraform の対象が無い")
		assert.Equal(t, "misconfigurations: [x]\n", seen)
		assert.Equal(t, filepath.Join(base, "tmp", "trivy-config"), filepath.Dir(dir))
		noScratch(t, base)
	})

	t.Run("trivy に TRIVY_ で始まる環境変数を渡さない", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		r.environ = append(slices.Clone(r.environ), "TRIVY_SEVERITY=LOW", "TRIVY_IGNOREFILE=x", "IACSCAN_KEEP=1")
		require.NoError(t, runTrivyConfig(args(base, "-format", "sarif"), &bytes.Buffer{}, &bytes.Buffer{}, r))
		for _, c := range [][2]string{
			{"trivy-config-canary", "trivy-config/" + trivySev},
			{filepath.Base(base), "trivy-config/" + trivySev},
			{"trivy-config-convert", "trivy-convert/sarif"},
		} {
			env := f.envOf(c[0], c[1])
			assert.Contains(t, env, "IACSCAN_KEEP=1", c[1])
			for _, kv := range env {
				assert.False(t, strings.HasPrefix(kv, "TRIVY_"), "%s: %s", c[1], kv)
			}
		}
	})

	t.Run("所見があれば errFindings で、出力は stdout へ流す", func(t *testing.T) {
		t.Parallel()
		_, r, base := setup(t, 1, trivyMainJSON)
		var stdout bytes.Buffer
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "json"), &stdout, &bytes.Buffer{}, r), errFindings)
		assert.Equal(t, trivyMainJSON, stdout.String())
	})

	t.Run("canary が落ちなければ本走査を起動せず、stdout に何も書かずに errCanary", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		f.scenario("trivy-config-canary", 0, trivyCanaryJSON, nil)
		var stdout, log bytes.Buffer
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "json"), &stdout, &log, r), errCanary)
		assert.Equal(t, []string{"trivy-config-canary trivy-config/" + trivySev}, f.calls())
		assert.Empty(t, stdout.String())
		assert.Contains(t, log.String(), "trivy-stderr trivy-config-canary", "落ちた canary の stderr を出す")
		noScratch(t, base)
	})

	for name, c := range map[string]struct {
		json string
		want error
	}{
		"本走査の結果が JSON でない":           {"FATAL error", errResult},
		"本走査の結果に Results が無い":        {`{}`, errResult},
		"root の1つに Terraform の対象が無い": {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"}]}`, errNoTargets},
		"root の下にあるのが Terraform でない": {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"},{"Target":"env/Dockerfile","Type":"dockerfile"}]}`, errNoTargets},
		"Dockerfile の対象が無い":          {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"},{"Target":"env/c","Type":"terraform"}]}`, errNoTargets},
		"root と前置きだけが一致する Target":    {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"},{"Target":"envx/c","Type":"terraform"}]}`, errNoTargets},
		"Results が空": {`{"Results":[]}`, errNoTargets},
	} {
		for _, format := range []string{"json", "sarif"} {
			t.Run(name+"なら、所見の有無に依らず、"+format+" で何も書かずに失敗する", func(t *testing.T) {
				t.Parallel()
				f, r, base := setup(t, 1, c.json)
				var stdout bytes.Buffer
				require.ErrorIs(t, runTrivyConfig(args(base, "-format", format), &stdout, &bytes.Buffer{}, r), c.want)
				assert.Empty(t, stdout.String())
				assert.Len(t, f.calls(), 2, "変換を起動しない")
			})
		}
	}

	t.Run("stdout へ書けなければ errOutput", func(t *testing.T) {
		t.Parallel()
		_, r, base := setup(t, 0, trivyMainJSON)
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "json"), brokenWriter{}, &bytes.Buffer{}, r), errOutput)
	})

	t.Run("本走査がシグナルで終われば、何も書かずに errTool", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		f.killIn("trivy-config/"+trivySev, filepath.Base(base))
		var stdout bytes.Buffer
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "json"), &stdout, &bytes.Buffer{}, r), errTool)
		assert.Empty(t, stdout.String())
		assert.Len(t, f.calls(), 2)
	})

	t.Run("変換がシグナルで終われば、何も書かずに errTool", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		f.kill("trivy-convert/sarif")
		var stdout bytes.Buffer
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "sarif"), &stdout, &bytes.Buffer{}, r), errTool)
		assert.Empty(t, stdout.String())
		noScratch(t, base)
	})

	t.Run("本走査の結果を読めなければ stdout に何も書かない", func(t *testing.T) {
		t.Parallel()
		_, r, base := setup(t, 1, "FATAL error")
		var stdout bytes.Buffer
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "json"), &stdout, &bytes.Buffer{}, r), errResult)
		assert.Empty(t, stdout.String())
	})

	t.Run("変換が落ちれば errResult", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		f.fail("trivy-convert/sarif")
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "sarif"), &bytes.Buffer{}, &bytes.Buffer{}, r), errResult)
		noScratch(t, base)
	})

	t.Run("抑止の宣言が無ければ trivy を起動せずに errInput", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		require.NoError(t, os.Remove(filepath.Join(base, ".trivyignore.yaml")))
		require.ErrorIs(t, runTrivyConfig(args(base), &bytes.Buffer{}, &bytes.Buffer{}, r), errInput)
		assert.Empty(t, f.calls())
	})

	t.Run("trivy を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, _, base := setup(t, 0, trivyMainJSON)
		require.ErrorIs(t, runTrivyConfig(args(base), &bytes.Buffer{}, &bytes.Buffer{}, runner{tool: tooltest.Missing, environ: os.Environ()}), errTool)
	})

	t.Run("canary がシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		f.kill("trivy-config/" + trivySev)
		require.ErrorIs(t, runTrivyConfig(args(base), &bytes.Buffer{}, &bytes.Buffer{}, r), errTool)
		assert.Len(t, f.calls(), 1)
	})

	t.Run("作業ディレクトリが tmp/ の外を指せば、trivy を起動せずに errUsage", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		outside := tree(t, map[string]string{"keep": "k"})
		require.NoError(t, os.MkdirAll(filepath.Join(base, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(base, "tmp", "trivy-config")))
		require.ErrorIs(t, runTrivyConfig(args(base), &bytes.Buffer{}, &bytes.Buffer{}, r), errUsage)
		assert.Empty(t, f.calls())
		left, err := os.ReadDir(outside)
		require.NoError(t, err)
		assert.Len(t, left, 1, "外に canary の置き場を作らない")
	})

	t.Run("本走査だけを起動できなくても errTool", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		fakeTool := r.tool
		r.tool = func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
			if d == base {
				return tooltest.Missing(ctx, d, env, step)
			}
			return fakeTool(ctx, d, env, step)
		}
		var stdout bytes.Buffer
		require.ErrorIs(t, runTrivyConfig(args(base, "-format", "json"), &stdout, &bytes.Buffer{}, r), errTool)
		assert.Equal(t, []string{"trivy-config-canary trivy-config/" + trivySev}, f.calls())
		assert.Empty(t, stdout.String())
	})

	t.Run("root が無ければ trivy を起動せずに errMissingRoot", func(t *testing.T) {
		t.Parallel()
		f, r, base := setup(t, 0, trivyMainJSON)
		in := []string{"-base", base, "-severity", trivySev, "env", "nothing"}
		require.ErrorIs(t, runTrivyConfig(in, &bytes.Buffer{}, &bytes.Buffer{}, r), errMissingRoot)
		assert.Empty(t, f.calls())
	})

	for name, extra := range map[string][]string{
		"-severity が無い":   {"-format", "json", "env"},
		"一覧に無い -severity": {"-severity", "HIGH", "env"},
		"一覧に無い -format":   {"-severity", trivySev, "-format", "cyclonedx", "env"},
		"root が無い":        {"-severity", trivySev},
		"重なる root":        {"-severity", trivySev, "env", "env/c"},
		"base 自身の root":   {"-severity", trivySev, "."},
		"知らない flag":       {"-severity", trivySev, "-exit-code", "0", "env"},
	} {
		t.Run(name+"なら trivy を起動せずに errUsage", func(t *testing.T) {
			t.Parallel()
			f, r, base := setup(t, 0, trivyMainJSON)
			err := runTrivyConfig(append([]string{"-base", base}, extra...), &bytes.Buffer{}, &bytes.Buffer{}, r)
			require.ErrorIs(t, err, errUsage)
			assert.Empty(t, f.calls())
		})
	}
}

func Test_judgeTrivyTargets(t *testing.T) {
	t.Parallel()

	roots := []string{"usecases", "modules", "env"}
	for name, in := range map[string]string{
		"root ごとに Terraform が1件以上、Dockerfile が1件以上": trivyMainJSON,
		"root そのものの Target も数える": `{"Results":[{"Target":"usecases","Type":"terraform"},{"Target":"modules","Type":"terraform"},` +
			`{"Target":"env","Type":"terraform"},{"Target":"Dockerfile","Type":"dockerfile"}]}`,
	} {
		t.Run(name+"なら通る", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, judgeTrivyTargets([]byte(in), roots))
		})
	}

	for name, c := range map[string]struct {
		in   string
		want error
	}{
		"JSON でない":                {"x", errResult},
		"Results が無い":             {`{}`, errResult},
		"Results が null":          {`{"Results":null}`, errResult},
		"Results が配列でない":          {`{"Results":{}}`, errResult},
		"root の1つに Terraform が無い": {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"},{"Target":"Dockerfile","Type":"dockerfile"}]}`, errNoTargets},
		"前置きだけが一致する Target":       {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"},{"Target":"envx","Type":"terraform"},{"Target":"Dockerfile","Type":"dockerfile"}]}`, errNoTargets},
		"Dockerfile が無い":          {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"},{"Target":"env/c","Type":"terraform"}]}`, errNoTargets},
		"root の下の Dockerfile だけ":  {`{"Results":[{"Target":"usecases/a","Type":"terraform"},{"Target":"modules/b","Type":"terraform"},{"Target":"env/Dockerfile","Type":"dockerfile"}]}`, errNoTargets},
	} {
		t.Run(name+"は落ちる", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, judgeTrivyTargets([]byte(c.in), roots), c.want)
		})
	}
}

func Test_writeTrivyReport(t *testing.T) {
	t.Parallel()

	t.Run("変換しないなら、そのまま書く", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		var stdout bytes.Buffer
		require.NoError(t, writeTrivyReport([]byte(`{"a":1}`), "/repo", t.TempDir(), "", nil, r, &stdout, &bytes.Buffer{}))
		assert.Equal(t, `{"a":1}`, stdout.String())
		assert.Empty(t, f.calls())
	})

	t.Run("変換するなら、一時の置き場の result.json を変え、置き場を消す", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		tmp := t.TempDir()
		var stdout bytes.Buffer
		require.NoError(t, writeTrivyReport([]byte(`{"a":1}`), "/repo", tmp, "trivy-convert/table", os.Environ(), r, &stdout, &bytes.Buffer{}))
		assert.Equal(t, `converted table:{"a":1}`, stdout.String())
		assert.Equal(t, []string{"trivy-config-convert trivy-convert/table"}, f.calls())
		left, err := os.ReadDir(tmp)
		require.NoError(t, err)
		assert.Empty(t, left)
	})

	t.Run("sarif は ROOTPATH を base へ向け直してから書く", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		f.write("convert-sarif", `{"version":"2.1.0","runs":[{"originalUriBaseIds":{"ROOTPATH":{"uri":"file:///tmp/x/result.json/"}},`+
			`"results":[{"ruleId":"AVD-1","locations":[{"physicalLocation":{"artifactLocation":{"uri":"env/c/main.tf","uriBaseId":"ROOTPATH"}}}]}]}]}`)
		var stdout bytes.Buffer
		require.NoError(t, writeTrivyReport([]byte(`{}`), "/repo", t.TempDir(), "trivy-convert/sarif", os.Environ(), r, &stdout, &bytes.Buffer{}))
		assert.JSONEq(t, `{"version":"2.1.0","runs":[{"originalUriBaseIds":{"ROOTPATH":{"uri":"file:///repo/"}},`+
			`"results":[{"ruleId":"AVD-1","locations":[{"physicalLocation":{"artifactLocation":{"uri":"env/c/main.tf","uriBaseId":"ROOTPATH"}}}]}]}]}`, stdout.String())
	})

	for name, out := range map[string]string{
		"JSON でない":     "FATAL",
		"ROOTPATH が無い": `{"runs":[{"originalUriBaseIds":{}}]}`,
	} {
		t.Run("sarif が"+name+"なら、何も書かずに errResult", func(t *testing.T) {
			t.Parallel()
			f, r := newFake(t)
			f.write("convert-sarif", out)
			var stdout bytes.Buffer
			err := writeTrivyReport([]byte(`{}`), "/repo", t.TempDir(), "trivy-convert/sarif", os.Environ(), r, &stdout, &bytes.Buffer{})
			require.ErrorIs(t, err, errResult)
			assert.Empty(t, stdout.String())
		})
	}

	t.Run("置き場を作れなければ errInput", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		dir := tree(t, map[string]string{"file": ""})
		err := writeTrivyReport([]byte(`{}`), "/repo", filepath.Join(dir, "file"), "trivy-convert/sarif", os.Environ(), r, &bytes.Buffer{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
		assert.Empty(t, f.calls())
	})

	t.Run("変換を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		tool := tooltest.Missing
		err := writeTrivyReport([]byte(`{}`), "/repo", t.TempDir(), "trivy-convert/sarif", os.Environ(), runner{tool: tool}, &bytes.Buffer{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errTool)
	})
}

func Test_setSARIFRoot(t *testing.T) {
	t.Parallel()

	t.Run("すべての run の ROOTPATH を base の file URI にし、ほかの欄と数を残す", func(t *testing.T) {
		t.Parallel()
		in := `{"runs":[{"originalUriBaseIds":{"ROOTPATH":{"uri":"file:///t/result.json/"},"OTHER":{"uri":"file:///o/"}},"rank":12345678901234567890},` +
			`{"originalUriBaseIds":{"ROOTPATH":{"uri":"file:///t/result.json/","description":{"text":"d"}}}}]}`
		got, err := setSARIFRoot([]byte(in), "/repo/a")
		require.NoError(t, err)
		assert.JSONEq(t, `{"runs":[{"originalUriBaseIds":{"ROOTPATH":{"uri":"file:///repo/a/"},"OTHER":{"uri":"file:///o/"}},"rank":12345678901234567890},`+
			`{"originalUriBaseIds":{"ROOTPATH":{"uri":"file:///repo/a/","description":{"text":"d"}}}}]}`, string(got))
		assert.Contains(t, string(got), "12345678901234567890", "大きな数を浮動小数にしない")
	})

	t.Run("相対の base は絶対パスにする", func(t *testing.T) {
		t.Parallel()
		got, err := setSARIFRoot([]byte(`{"runs":[{"originalUriBaseIds":{"ROOTPATH":{"uri":"x"}}}]}`), ".")
		require.NoError(t, err)
		wd, err := os.Getwd()
		require.NoError(t, err)
		assert.Equal(t, "file://"+wd+"/", rootPathOf(t, got))
	})

	for name, in := range map[string]string{
		"JSON でない":                  "FATAL",
		"object でない":                `[]`,
		"runs が無い":                  `{}`,
		"runs が空":                   `{"runs":[]}`,
		"originalUriBaseIds が無い":    `{"runs":[{}]}`,
		"ROOTPATH が無い":              `{"runs":[{"originalUriBaseIds":{"OTHER":{"uri":"x"}}}]}`,
		"ROOTPATH が object でない":     `{"runs":[{"originalUriBaseIds":{"ROOTPATH":"x"}}]}`,
		"2つ目の run にだけ ROOTPATH が無い": `{"runs":[{"originalUriBaseIds":{"ROOTPATH":{"uri":"x"}}},{}]}`,
		"run が object でない":          `{"runs":[1]}`,
	} {
		t.Run(name+"は errResult", func(t *testing.T) {
			t.Parallel()
			_, err := setSARIFRoot([]byte(in), "/repo")
			require.ErrorIs(t, err, errResult)
		})
	}
}

// rootPathOf は、SARIF の最初の run の ROOTPATH の uri を、パスの部分を復号した形で返す。
func rootPathOf(t *testing.T, raw []byte) string {
	t.Helper()
	var doc struct {
		Runs []struct {
			OriginalURIBaseIDs map[string]struct {
				URI string `json:"uri"`
			} `json:"originalUriBaseIds"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Runs)
	u, err := url.Parse(doc.Runs[0].OriginalURIBaseIDs["ROOTPATH"].URI)
	require.NoError(t, err)
	return u.Scheme + "://" + u.Path
}

func Test_judgeTrivyCanary(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rc  int
		out string
		ok  bool
	}{
		"非0で DS-0001 を報告すれば通る": {1, trivyCanaryJSON, true},
		"0 で終われば落ちる":           {0, trivyCanaryJSON, false},
		"DS-0001 が無ければ落ちる":     {1, `{"Results":[{"Misconfigurations":[{"ID":"DS-0002"}]}]}`, false},
		"Results が無ければ落ちる":     {1, `{}`, false},
		"JSON でなければ落ちる":        {1, `FATAL error`, false},
		"空の出力なら落ちる":            {1, ``, false},
		"ID が別の欄にあるだけなら落ちる":    {1, `{"Results":[{"Secrets":[{"ID":"DS-0001"}]}]}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := judgeTrivyCanary(c.rc, []byte(c.out))
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errCanary)
		})
	}
}
