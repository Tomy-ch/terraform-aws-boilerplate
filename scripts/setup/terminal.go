package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// errNotTerminal は、標準入力が端末でないこと。手順の実行は端末からの入力でしか書き込まない（ADR-0702 決定27）。
	errNotTerminal  = xerrors.New("標準入力が端末ではありません。書き込みは端末から起動したときだけ行います")
	errNoInput      = xerrors.New("入力が尽きました")
	errNotConfirmed = xerrors.New("確認の入力が project の名前と一致しないので、書き込みません")
)

// terminal は、人への問いと答えの読み取り。
type terminal struct {
	in    *bufio.Reader
	out   io.Writer
	isTTY bool
}

func newTerminal(in io.Reader, out io.Writer, isTTY bool) *terminal {
	return &terminal{in: bufio.NewReader(in), out: out, isTTY: isTTY}
}

// requireTTY は、標準入力が端末でなければ errNotTerminal。書き込みのための入力を読む前に呼ぶ。
func (t *terminal) requireTTY() error {
	if !t.isTTY {
		return errNotTerminal
	}

	return nil
}

// ask は、prompt を出して1行を読み、前後の空白を落として返す。
func (t *terminal) ask(prompt string) (string, error) {
	if _, err := io.WriteString(t.out, prompt); err != nil {
		return "", err
	}

	line, err := t.in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", xerrors.Wrap(errNoInput, strings.TrimSpace(prompt))
	}

	return strings.TrimSpace(line), nil
}

// confirm は、project の名前の打ち込みを求める。一致しなければ errNotConfirmed。
func (t *terminal) confirm(project string) error {
	if err := t.requireTTY(); err != nil {
		return err
	}

	got, err := t.ask(fmt.Sprintf("書き込むなら project の名前（%s）を入力してください: ", project))
	if err != nil {
		return err
	}

	if got != project {
		return errNotConfirmed
	}

	return nil
}
