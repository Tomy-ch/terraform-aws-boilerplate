// Package toolrun は、検査の道具が外部の道具を段ごとに起動するときの、共有の取り決めを持ちます。起動できない、
// またはシグナルで終わったことは [ErrTool]（終了コード 2）で、0 以外の終了コードはエラーにせず呼び出し側の判定へ
// 渡します。引数は各道具の [Func] の実物がリテラルで固定し、ここは組み上がった *exec.Cmd を受けるだけです。
package toolrun

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ErrTool は、道具を起動できない、またはシグナルで終わったことです。
var ErrTool = xerrors.New("道具を起動できません")

// Func は、ディレクトリ dir で段 step の道具を起動するコマンドを作ります。テストは差し替えます。
type Func func(dir string, env []string, step string) *exec.Cmd

// Result は、道具の stdout、stdout と stderr を束ねたもの、終了コードです。
type Result struct {
	Stdout   string
	Combined string
	Code     int
}

// Run は cmd の stdout と、stdout・stderr を束ねたものを捕捉して走らせ、[Exec] と同じく分類します。シグナルで
// 終わったときは、捕捉した出力をエラーに添えます。
func Run(cmd *exec.Cmd) (Result, error) {
	var stdout bytes.Buffer
	combined := &lockedBuffer{}
	cmd.Stdout = io.MultiWriter(&stdout, combined)
	cmd.Stderr = combined
	err := cmd.Run()
	r := Result{Stdout: stdout.String(), Combined: combined.String()}
	r.Code, err = classify(cmd, err, "\n"+r.Combined)

	return r, err
}

// Exec は cmd を走らせ、終了コードを返します。起動できない、またはシグナルで終わったときは [ErrTool] です。0 以外の
// 終了コードはエラーにせず返し、判定は呼び出し側が持ちます。出力の行き先は cmd のままです。
func Exec(cmd *exec.Cmd) (int, error) {
	return classify(cmd, cmd.Run(), "")
}

// classify は、cmd.Run の結果 err を終了コードと [ErrTool] に分けます。detail はシグナルのエラーの末尾に添えます。
func classify(cmd *exec.Cmd, err error, detail string) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if !xerrors.As(err, &exit) {
		return 0, xerrors.Wrapf(ErrTool, "%s: %v", cmd.Path, err)
	}
	if exit.ExitCode() < 0 {
		return 0, xerrors.Wrapf(ErrTool, "%s がシグナルで終わりました: %v%s", cmd.Path, err, detail)
	}

	return exit.ExitCode(), nil
}

// ExitCode は err の終了コードです。nil は 0、failures のどれかに当たれば 1（検査が落ちた）、ほかは 2（検査を
// 成り立たせられなかった）です。
func ExitCode(err error, failures ...error) int {
	if err == nil {
		return 0
	}
	for _, f := range failures {
		if xerrors.Is(err, f) {
			return 1
		}
	}

	return 2
}

// Report は、err があれば道具の名前 name を付けて1行で w に書きます。
func Report(name string, err error, w io.Writer) {
	if err != nil {
		fmt.Fprintf(w, "❌ %s: %v\n", name, err)
	}
}

// lockedBuffer は、stdout と stderr を別々の goroutine から受ける束ね先です。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
