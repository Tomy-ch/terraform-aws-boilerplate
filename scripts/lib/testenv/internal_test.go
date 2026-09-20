package testenv

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeReporter は requireNonRoot が呼んだ経路を控えます。実際の *testing.T は Skip も
// Fatalf も runtime.Goexit するため、分岐の到達を観測できません。
type fakeReporter struct {
	skipped  bool
	failed   bool
	message  string
	helpered bool
}

func (f *fakeReporter) Helper() { f.helpered = true }

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

func root() int    { return 0 }
func nonRoot() int { return 1000 }

func Test_requireNonRoot(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root でなければ skip も失敗もしない", func(t *testing.T) {
			t.Parallel()
			f := &fakeReporter{}

			requireNonRoot(f, "理由", nonRoot, func(string) string { return "" })

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})

		// 環境変数が立っていても、root でなければ通常どおり戻る。立っているだけで
		// 落ちるなら、非 root の CI がこの経路を通った瞬間に全部赤くなる。
		t.Run("root でなければ環境変数が立っていても失敗しない", func(t *testing.T) {
			t.Parallel()
			f := &fakeReporter{}

			requireNonRoot(f, "理由", nonRoot, func(string) string { return "1" })

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root なら理由を添えて skip する", func(t *testing.T) {
			t.Parallel()
			f := &fakeReporter{}

			requireNonRoot(f, "権限を落としても効かない", root, func(string) string { return "" })

			assert.True(t, f.skipped)
			assert.False(t, f.failed)
			assert.Equal(t, "権限を落としても効かない", f.message)
		})

		// これがこのパッケージの存在理由。skip は既定の出力に現れないので、CI では
		// 失敗へ変えられなければ「報告より少ない検査で緑」が残り続ける。
		t.Run("root かつ環境変数が立っていれば skip せず失敗する", func(t *testing.T) {
			t.Parallel()
			f := &fakeReporter{}

			requireNonRoot(f, "権限を落としても効かない", root, func(k string) string {
				assert.Equal(t, RequireNonRootEnv, k)

				return "1"
			})

			assert.True(t, f.failed)
			assert.False(t, f.skipped, "失敗させるべき場面で skip した")
			assert.Contains(t, f.message, RequireNonRootEnv)
		})
	})
}

func found(string) (string, error)   { return "/usr/bin/shellcheck", nil }
func missing(string) (string, error) { return "", os.ErrNotExist }

func Test_requireShellcheck(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("PATH に在れば skip も失敗もしない", func(t *testing.T) {
			t.Parallel()
			f := &fakeReporter{}

			requireShellcheck(f, found, func(string) string { return "" })

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("PATH に無ければ skip する", func(t *testing.T) {
			t.Parallel()
			f := &fakeReporter{}

			requireShellcheck(f, missing, func(string) string { return "" })

			assert.True(t, f.skipped)
			assert.False(t, f.failed)
		})

		// skip は既定の出力に現れない。CI で失敗へ変えられなければ、報告より少ない検査で
		// 緑が残り続ける。
		t.Run("PATH に無く環境変数が立っていれば skip せず失敗する", func(t *testing.T) {
			t.Parallel()
			f := &fakeReporter{}

			requireShellcheck(f, missing, func(k string) string {
				assert.Equal(t, RequireShellcheckEnv, k)

				return "1"
			})

			assert.True(t, f.failed)
			assert.False(t, f.skipped, "失敗させるべき場面で skip した")
			assert.Contains(t, f.message, RequireShellcheckEnv)
		})
	})
}
