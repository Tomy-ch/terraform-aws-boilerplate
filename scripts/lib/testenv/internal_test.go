package testenv

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeReporter は判断が呼んだ経路を控えます。実際の *testing.T は Skip も Fatalf も
// runtime.Goexit するため、分岐の到達を観測できません。
type fakeReporter struct {
	skipped bool
	failed  bool
	message string
}

func (f *fakeReporter) Helper() {}

func (f *fakeReporter) Skip(args ...any) {
	f.skipped = true
	if len(args) > 0 {
		f.message, _ = args[0].(string)
	}
}

func (f *fakeReporter) Fatalf(format string, args ...any) {
	f.failed = true
	f.message = fmt.Sprintf(format, args...)
}

func found(string) (string, error)   { return "/usr/bin/shellcheck", nil }
func missing(string) (string, error) { return "", os.ErrNotExist }

// swap は外界を差し替え、テストの終わりに戻します。
func swap(t *testing.T, uid int, env string, look func(string) (string, error)) *fakeReporter {
	t.Helper()

	oldEuid, oldEnv, oldLook := geteuid, getenv, lookPath
	geteuid = func() int { return uid }
	getenv = func(string) string { return env }
	lookPath = look
	t.Cleanup(func() { geteuid, getenv, lookPath = oldEuid, oldEnv, oldLook })

	return &fakeReporter{}
}

// 既定値が本物を指していることを、実際の syscall と突き合わせて見ます。差し替え可能に
// したことで、既定値を書き間違えても全部緑になる余地が生まれるため。
func Test_外界の既定値(t *testing.T) { //nolint:paralleltest // パッケージ変数を読む
	assert.Equal(t, os.Geteuid(), geteuid(), "geteuid が os.Geteuid を指していない")
	assert.Equal(t, os.Getenv("PATH"), getenv("PATH"), "getenv が os.Getenv を指していない")

	want, wantErr := exec.LookPath("go")
	got, gotErr := lookPath("go")
	assert.Equal(t, want, got, "lookPath が exec.LookPath を指していない")
	assert.Equal(t, wantErr == nil, gotErr == nil)
}

func Test_requireNonRoot(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("正常系", func(t *testing.T) {
		t.Run("root でなければ skip も失敗もしない", func(t *testing.T) {
			f := swap(t, 1000, "", nil)

			requireNonRoot(f, "理由")

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})

		// 環境変数が立っているだけで落ちるなら、非 root の CI がこの経路を通った瞬間に
		// 全部赤くなる。
		t.Run("root でなければ環境変数が立っていても失敗しない", func(t *testing.T) {
			f := swap(t, 1000, "1", nil)

			requireNonRoot(f, "理由")

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("root なら理由を添えて skip する", func(t *testing.T) {
			f := swap(t, 0, "", nil)

			requireNonRoot(f, "権限を落としても効かない")

			assert.True(t, f.skipped)
			assert.False(t, f.failed)
			assert.Equal(t, "権限を落としても効かない", f.message)
		})

		// これがこのパッケージの存在理由。skip は既定の出力に現れないので、CI では
		// 失敗へ変えられなければ「報告より少ない検査で緑」が残り続ける。
		t.Run("root かつ環境変数が立っていれば skip せず失敗する", func(t *testing.T) {
			f := swap(t, 0, "1", nil)

			requireNonRoot(f, "権限を落としても効かない")

			assert.True(t, f.failed)
			assert.False(t, f.skipped, "失敗させるべき場面で skip した")
			assert.Contains(t, f.message, RequireNonRootEnv)
		})
	})
}

func Test_requireShellcheck(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("正常系", func(t *testing.T) {
		t.Run("PATH に在れば skip も失敗もしない", func(t *testing.T) {
			f := swap(t, 1000, "", found)

			requireShellcheck(f)

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("PATH に無ければ skip する", func(t *testing.T) {
			f := swap(t, 1000, "", missing)

			requireShellcheck(f)

			assert.True(t, f.skipped)
			assert.False(t, f.failed)
		})

		t.Run("PATH に無く環境変数が立っていれば skip せず失敗する", func(t *testing.T) {
			f := swap(t, 1000, "1", missing)

			requireShellcheck(f)

			assert.True(t, f.failed)
			assert.False(t, f.skipped, "失敗させるべき場面で skip した")
			assert.Contains(t, f.message, RequireShellcheckEnv)
		})
	})
}
