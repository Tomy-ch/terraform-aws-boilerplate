package main

import (
	"bytes"
	"strings"
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
