package main

import (
	"fmt"
	"io"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// errNG は、確認に通らない項目があること。理由は結果の行が述べるので、メッセージを持たない。
var errNG = xerrors.New("確認に通らない項目があります")

// report は、確認の結果の行を出し、NG の有無を覚える。
type report struct {
	w     io.Writer
	count int
	ngs   int
}

func newReport(w io.Writer, title string) *report {
	fmt.Fprintf(w, "# %s\n", title)

	return &report{w: w}
}

// check は、ok なら ✅、そうでなければ ❌ を付けて what を出す。
func (r *report) check(ok bool, what string) {
	r.count++

	if ok {
		fmt.Fprintf(r.w, "✅ %s\n", what)

		return
	}

	r.ngs++
	fmt.Fprintf(r.w, "❌ %s\n", what)
}

// ng は、判定の前に NG と決まったことを出す。
func (r *report) ng(what string) {
	r.check(false, what)
}

// result は、NG があれば errNG。1件も確かめていなければ、確認が対象を持たないので errNG（ADR-0702 決定13）。
func (r *report) result() error {
	if r.count == 0 {
		fmt.Fprintln(r.w, "❌ 確かめた項目がありません")

		return errNG
	}

	if r.ngs > 0 {
		return errNG
	}

	return nil
}
