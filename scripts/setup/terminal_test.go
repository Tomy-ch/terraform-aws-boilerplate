package main

import (
	"bytes"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

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

		term, _ := tty("")
		block := make(chan struct{})
		term.notify = func(c chan<- os.Signal) func() { c <- syscall.SIGHUP; return func() {} }
		term.hidden = func() ([]byte, error) {
			<-block

			return nil, nil
		}

		var restored atomic.Bool

		term.restore = func() { restored.Store(true) }

		_, err := term.secret("P: ")
		close(block)
		require.ErrorIs(t, err, errInterrupted)
		assert.True(t, restored.Load())
	})

	t.Run("読めなければ errNoHidden", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("")
		term.hidden = hiddenLines()

		_, err := term.secret("P: ")
		require.ErrorIs(t, err, errNoHidden)
	})
}

func Test_notifyInterrupt(t *testing.T) {
	t.Parallel()

	t.Run("受けるのをやめる関数を返す", func(t *testing.T) {
		t.Parallel()

		stop := notifyInterrupt(make(chan os.Signal, 1))
		require.NotNil(t, stop)
		assert.NotPanics(t, stop)
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
