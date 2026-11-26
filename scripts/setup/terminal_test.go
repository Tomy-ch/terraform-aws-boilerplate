package main

import (
	"bytes"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_newTerminal(t *testing.T) {
	t.Parallel()

	t.Run("入力と出力と端末であるかを持つ", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		term := newTerminal(strings.NewReader("x\n"), &out, true)
		assert.True(t, term.isTTY)
		assert.Same(t, &out, term.out)
	})
}

func Test_terminal_requireTTY(t *testing.T) {
	t.Parallel()

	t.Run("端末なら nil", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("")
		require.NoError(t, term.requireTTY())
	})

	t.Run("端末でなければ errNotTerminal", func(t *testing.T) {
		t.Parallel()

		term := newTerminal(strings.NewReader(""), &bytes.Buffer{}, false)
		require.ErrorIs(t, term.requireTTY(), errNotTerminal)
	})
}

func Test_terminal_ask(t *testing.T) {
	t.Parallel()

	t.Run("問いを出し、前後の空白を落とした1行を返す", func(t *testing.T) {
		t.Parallel()

		term, out := tty("  a@b.c  \nnext\n")
		got, err := term.ask("Q: ")
		require.NoError(t, err)
		assert.Equal(t, "a@b.c", got)
		assert.Equal(t, "Q: ", out.String())
	})

	t.Run("改行の無い最後の行も読む", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("last")
		got, err := term.ask("Q: ")
		require.NoError(t, err)
		assert.Equal(t, "last", got)
	})

	t.Run("入力が尽きていれば errNoInput", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("")
		_, err := term.ask("Q: ")
		require.ErrorIs(t, err, errNoInput)
	})

	t.Run("問いを書けなければ、そのエラーを返す", func(t *testing.T) {
		t.Parallel()

		_, err := newTerminal(strings.NewReader("a\n"), failingWriter{}, true).ask("Q: ")
		require.ErrorIs(t, err, os.ErrClosed)
	})
}

func Test_terminal_confirm(t *testing.T) {
	t.Parallel()

	t.Run("project の名前と一致すれば nil", func(t *testing.T) {
		t.Parallel()

		term, out := tty(testProject + "\n")
		require.NoError(t, term.confirm(testProject))
		assert.Contains(t, out.String(), "project の名前（tabp）")
	})

	t.Run("一致しなければ errNotConfirmed", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("yes\n")
		require.ErrorIs(t, term.confirm(testProject), errNotConfirmed)
	})

	t.Run("端末でなければ、問わずに errNotTerminal", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		term := newTerminal(strings.NewReader(testProject+"\n"), &out, false)
		require.ErrorIs(t, term.confirm(testProject), errNotTerminal)
		assert.Empty(t, out.String())
	})
}

func Test_terminal_secret(t *testing.T) {
	t.Parallel()

	t.Run("問いを出し、伏せた入力を返して改行を出す", func(t *testing.T) {
		t.Parallel()

		term, out := tty("")
		term.hidden = hiddenLines("s3cret")

		got, err := term.secret("P: ")
		require.NoError(t, err)
		assert.Equal(t, "s3cret", got)
		assert.Equal(t, "P: \n", out.String())
	})

	t.Run("端末でなければ読まずに errNotTerminal", func(t *testing.T) {
		t.Parallel()

		term := newTerminal(strings.NewReader(""), &bytes.Buffer{}, false)
		term.hidden = func() ([]byte, error) { t.Error("読んだ"); return nil, nil }

		_, err := term.secret("P: ")
		require.ErrorIs(t, err, errNotTerminal)
	})

	t.Run("伏せて読む手段が無ければ errNoHidden（伏せずに読まない）", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("s3cret\n")

		_, err := term.secret("P: ")
		require.ErrorIs(t, err, errNoHidden)
	})

	t.Run("読む途中で中断のシグナルを受ければ、端末を戻して errInterrupted", func(t *testing.T) {
		t.Parallel()

		term, out := tty("")
		block := make(chan struct{})
		term.hidden = func() ([]byte, error) {
			<-block

			return nil, nil
		}

		var (
			restored atomic.Int32
			stopped  atomic.Bool
		)

		term.notify = func(c chan<- os.Signal) func() { c <- syscall.SIGHUP; return func() { stopped.Store(true) } }
		term.restore = func() { restored.Add(1) }

		got, err := term.secret("P: ")
		close(block)
		require.ErrorIs(t, err, errInterrupted)
		assert.Empty(t, got)
		assert.Equal(t, int32(1), restored.Load())
		assert.True(t, stopped.Load())
		assert.Equal(t, "P: \n", out.String())
	})

	t.Run("読めなければ errNoHidden", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("")
		term.hidden = hiddenLines()

		_, err := term.secret("P: ")
		require.ErrorIs(t, err, errNoHidden)
	})

	t.Run("問いを書けなければ、読まずにそのエラーを返す", func(t *testing.T) {
		t.Parallel()

		term := newTerminal(strings.NewReader(""), failingWriter{}, true)
		term.hidden = func() ([]byte, error) { t.Error("読んだ"); return nil, nil }

		_, err := term.secret("P: ")
		require.ErrorIs(t, err, os.ErrClosed)
	})

	t.Run("読んだ後の改行を書けなければ、そのエラーを返す", func(t *testing.T) {
		t.Parallel()

		w := &failAfter{n: 1}
		term := newTerminal(strings.NewReader(""), w, true)
		term.hidden = hiddenLines("s3cret")

		got, err := term.secret("P: ")
		require.ErrorIs(t, err, os.ErrClosed)
		assert.Empty(t, got)
	})
}

// 自プロセスへ中断のシグナルを送るので、並行して走るケースの notifier にも届く。並行にせず、ほかのケースが
// 止まっている間に走らせる。
func Test_notifyInterrupt(t *testing.T) {
	for _, sig := range interruptSignals {
		t.Run(sig.String()+" を受ければ channel に届き、stop で受けるのをやめる", func(t *testing.T) {
			c := make(chan os.Signal, 1)
			stop := notifyInterrupt(c)

			p, err := os.FindProcess(os.Getpid())
			require.NoError(t, err)
			require.NoError(t, p.Signal(sig))

			select {
			case got := <-c:
				assert.Equal(t, sig, got)
			case <-time.After(5 * time.Second):
				t.Fatal("シグナルが届きません")
			}

			stop()
		})
	}

	t.Run("中断のシグナルは INT・TERM・HUP", func(t *testing.T) {
		assert.ElementsMatch(t, []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}, interruptSignals)
	})
}

func Test_notifier_start(t *testing.T) {
	t.Parallel()

	t.Run("渡した関数へ channel を渡し、その stop を返す", func(t *testing.T) {
		t.Parallel()

		c := make(chan os.Signal, 1)

		var got chan<- os.Signal

		stopped := false
		n := notifier(func(ch chan<- os.Signal) func() { got = ch; return func() { stopped = true } })
		n.start(c)()
		assert.Equal(t, chan<- os.Signal(c), got)
		assert.True(t, stopped)
	})

	t.Run("nil なら既定の notifyInterrupt で受ける", func(t *testing.T) {
		t.Parallel()

		var n notifier

		stop := n.start(make(chan os.Signal, 1))
		require.NotNil(t, stop)
		stop()
	})
}

// failAfter は、n 回の書き込みの後に失敗する。
type failAfter struct{ n int }

func (f *failAfter) Write(p []byte) (int, error) {
	if f.n == 0 {
		return 0, os.ErrClosed
	}

	f.n--

	return len(p), nil
}
