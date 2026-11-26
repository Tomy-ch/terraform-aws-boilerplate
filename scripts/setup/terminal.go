package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// errNotTerminal: 手順の実行は端末からの入力でしか書き込まない（ADR-0702 決定27）。
	errNotTerminal  = xerrors.New("標準入力が端末ではありません。書き込みは端末から起動したときだけ行います")
	errNoInput      = xerrors.New("入力が尽きました")
	errNotConfirmed = xerrors.New("確認の入力が project の名前と一致しないので、書き込みません")
)

type terminal struct {
	in      *bufio.Reader
	out     io.Writer
	isTTY   bool
	hidden  func() ([]byte, error)
	restore func()
	notify  notifier
}

func newTerminal(in io.Reader, out io.Writer, isTTY bool) *terminal {
	return &terminal{in: bufio.NewReader(in), out: out, isTTY: isTTY}
}

// requireTTY は、書き込みのための入力を読む前に呼ぶ。端末でなければ errNotTerminal。
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

var (
	// errInterrupted は、入力か書き込みの途中で中断のシグナルを受けたこと。
	errInterrupted = xerrors.New("中断しました")

	interruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
)

// notifier は、中断のシグナルを c で受け始め、受けるのをやめる関数を返す。nil なら notifyInterrupt。
type notifier func(c chan<- os.Signal) (stop func())

func notifyInterrupt(c chan<- os.Signal) func() {
	signal.Notify(c, interruptSignals...)

	return func() { signal.Stop(c) }
}

func (n notifier) start(c chan<- os.Signal) func() {
	if n == nil {
		return notifyInterrupt(c)
	}

	return n(c)
}

var errNoHidden = xerrors.New("伏せた入力を読めません")

// secret は、prompt を出して、打ち込みを表示せずに1行を読む。端末でなければ読まない。
func (t *terminal) secret(prompt string) (string, error) {
	if err := t.requireTTY(); err != nil {
		return "", err
	}

	if t.hidden == nil {
		return "", errNoHidden
	}

	if _, err := io.WriteString(t.out, prompt); err != nil {
		return "", err
	}

	type result struct {
		b   []byte
		err error
	}

	sigs := make(chan os.Signal, 1)
	defer t.notify.start(sigs)()

	done := make(chan result, 1)

	go func() {
		b, err := t.hidden()
		done <- result{b, err}
	}()

	var r result

	select {
	case r = <-done:
	case s := <-sigs:
		// 伏せた入力は端末の表示を止めている。中断で戻さずに終えると、表示が止まったまま残る。
		if t.restore != nil {
			t.restore()
		}

		r = result{err: xerrors.Wrap(errInterrupted, s.String())}
	}

	if _, werr := io.WriteString(t.out, "\n"); werr != nil {
		return "", werr
	}

	if r.err != nil {
		if xerrors.Is(r.err, errInterrupted) {
			return "", r.err
		}

		return "", xerrors.Join(errNoHidden, r.err)
	}

	return string(r.b), nil
}
