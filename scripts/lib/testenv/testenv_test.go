package testenv

import (
	"bytes"
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/shellcheck"
)

func TestMain(m *testing.M) {
	os.Exit(RunGitIsolated(m))
}

// 環境変数の名前は workflow 側にリテラルで書かれています。テスト内のリテラルと突き合わせても
// 「自分で書いた文字列と一致した」しか言えないので、workflow そのものを読みます。
// 定数だけを改名すると、CI では skip が失敗へ変わらなくなります。
func TestEnvNames_workflowと一致する(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../../.github/workflows/go-test.yaml")
	require.NoError(t, err)

	for _, name := range []string{RequireNonRootEnv, RequireShellcheckEnv} {
		re := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(name) + `:\s`)
		assert.True(t, re.Match(body), "go-test.yaml が %s を立てていない", name)
	}
}

type probe struct {
	euidCalls int
	envNames  []string
	lookNames []string
}

func installProbe(t *testing.T, uid int, found bool) *probe {
	t.Helper()

	p := &probe{}
	oldEuid, oldEnv, oldLook := geteuid, getenv, lookPath

	geteuid = func() int {
		p.euidCalls++

		return uid
	}
	getenv = func(name string) string {
		p.envNames = append(p.envNames, name)

		return ""
	}
	lookPath = func(name string) (string, error) {
		p.lookNames = append(p.lookNames, name)
		if !found {
			return "", os.ErrNotExist
		}

		return "/usr/bin/" + name, nil
	}

	t.Cleanup(func() { geteuid, getenv, lookPath = oldEuid, oldEnv, oldLook })

	return p
}

// 公開の入口は *testing.T を具体型で受けるため、偽の reporter を差し込めません。
// skip と失敗の分岐は呼び出したテスト自身を終了させるので、ここで観測できるのは
// 「skip も失敗もしない経路」と、その経路が外界のどれを読んだかだけです。
func TestRequireNonRoot(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("正常系", func(t *testing.T) {
		t.Run("root でない場合、skip も失敗もせずに戻る", func(t *testing.T) {
			p := installProbe(t, 1000, true)

			RequireNonRoot(t, "権限を落としても効かない")

			assert.False(t, t.Failed())
			assert.Equal(t, 1, p.euidCalls, "実効 uid を見ずに判断している")
			assert.Empty(t, p.envNames, "root でない時点で決まるのに環境変数を読んでいる")
		})
	})
}

func TestRequireShellcheck(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("正常系", func(t *testing.T) {
		t.Run("PATH に在る場合、skip も失敗もせずに戻る", func(t *testing.T) {
			p := installProbe(t, 1000, true)

			RequireShellcheck(t)

			assert.False(t, t.Failed())
			// 探す名前がずれると、実物が在っても skip し続けます。
			assert.Equal(t, []string{shellcheck.Binary}, p.lookNames)
			assert.Empty(t, p.envNames, "在ると分かった時点で決まるのに環境変数を読んでいる")
		})
	})
}

type fakeM struct {
	called bool
	seen   map[string]bool
	code   int
}

func (f *fakeM) Run() int {
	f.called = true
	f.seen = map[string]bool{}
	for _, name := range []string{"TESTENV_PROBE_GIT_DIR", "TESTENV_PROBE_KEEP"} {
		_, ok := os.LookupEnv(name)
		f.seen[name] = ok
	}

	return f.code
}

func TestRunGitIsolated(t *testing.T) { //nolint:paralleltest // パッケージ変数と環境変数を差し替える
	swapList := func(t *testing.T, names []string, err error) *bytes.Buffer {
		t.Helper()

		var buf bytes.Buffer
		oldList, oldErr := localEnvVars, stderr
		localEnvVars = func() ([]string, error) { return names, err }
		stderr = &buf
		t.Cleanup(func() { localEnvVars, stderr = oldList, oldErr })

		return &buf
	}

	t.Run("正常系", func(t *testing.T) {
		t.Run("一覧の変数を外してから走らせ、その終了コードを返す", func(t *testing.T) {
			t.Setenv("TESTENV_PROBE_GIT_DIR", "/outer/.git")
			t.Setenv("TESTENV_PROBE_KEEP", "1")
			swapList(t, []string{"TESTENV_PROBE_GIT_DIR"}, nil)
			m := &fakeM{code: 7}

			got := RunGitIsolated(m)

			assert.Equal(t, 7, got)
			assert.False(t, m.seen["TESTENV_PROBE_GIT_DIR"], "一覧の変数が残ったまま走った")
			assert.True(t, m.seen["TESTENV_PROBE_KEEP"], "一覧に無い変数まで外した")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("一覧を得られなければ走らせず 2 を返す", func(t *testing.T) {
			buf := swapList(t, nil, errors.New("git が無い"))
			m := &fakeM{}

			got := RunGitIsolated(m)

			assert.Equal(t, 2, got)
			assert.False(t, m.called, "外せないまま走った")
			assert.Contains(t, buf.String(), "git が無い")
		})
	})
}
