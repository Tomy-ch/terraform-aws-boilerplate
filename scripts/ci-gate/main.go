// ci-gate は、Pull Request の gate と線の検証の判定を行う（ADR-0603 決定17・18・37・38）。
//
//	changes: 走るべき検査を決め、GITHUB_OUTPUT の run へ JSON の配列で書く。gate の workflow の caller の job が読む。
//	judge:   Pull Request の gate の判定。走るべき検査を自分で決め直し、needs の結果と照らす。
//	line:    線の検証の判定。線への push で、木の検査がすべて success であることを見る。
//
// 宣言（checks.json と branches.json）は、引数の rev の木から読む。gate は Pull Request の base の commit を渡す ——
// Pull Request の木の宣言を読むと、その Pull Request が宣言を1行変えるだけで自分の検査を省ける。
//
// 終了コードは、合格が 0、判定が不合格か入力が誤っているときが 1、宣言に検査が無いときが 2（ADR-0702 決定13）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage = xerrors.New("使い方が誤っています")
	errEvent = xerrors.New("線の検証は線への push でだけ判定します")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		log.Printf("❌ ci-gate: %v", err)
		if xerrors.Is(err, checksdecl.ErrNoChecks) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type options struct {
	declRev string
	base    string
	head    string
	baseRef string
	event   string
	ref     string
	output  string
	summary string
}

func run(args []string, getenv func(string) string, out io.Writer) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "ci-gate <changes|judge|line> [flags]")
	}
	sub := args[0]
	fs := flag.NewFlagSet(sub, flag.ContinueOnError)
	fs.SetOutput(out)
	var o options
	fs.StringVar(&o.declRev, "decl-rev", "", "宣言を読む rev（Pull Request では base の commit）")
	fs.StringVar(&o.base, "base", "", "差分の基点の commit")
	fs.StringVar(&o.head, "head", "", "差分の先の commit")
	fs.StringVar(&o.baseRef, "base-ref", "", "Pull Request の base のブランチ名")
	fs.StringVar(&o.event, "event", "", "line: 起動したイベント")
	fs.StringVar(&o.ref, "ref", "", "line: 起動した ref")
	fs.StringVar(&o.output, "output", "", "changes: 出力を追記するファイル（GITHUB_OUTPUT）")
	fs.StringVar(&o.summary, "summary", "", "judge・line: 判定の表を追記するファイル（GITHUB_STEP_SUMMARY）")
	if err := fs.Parse(args[1:]); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if o.declRev == "" {
		return xerrors.Wrap(errUsage, "--decl-rev が要ります")
	}
	d, b, err := readDecls(o.declRev)
	if err != nil {
		return err
	}

	switch sub {
	case "changes", "judge":
		if o.base == "" || o.head == "" || o.baseRef == "" {
			return xerrors.Wrap(errUsage, "--base・--head・--base-ref が要ります")
		}
		files, err := changedFiles(o.base, o.head)
		if err != nil {
			return err
		}
		should, err := selectChecks(d, files, o.baseRef, b)
		if err != nil {
			return err
		}
		if sub == "changes" {
			return writeChanges(o.output, should, len(files), out)
		}
		needs, err := parseNeeds(getenv("NEEDS"))
		if err != nil {
			return err
		}
		rows, verdict := judgePR(d, should, needs)
		return report(o.summary, "gate", rows, verdict, out)
	case "line":
		if o.event != "push" {
			return xerrors.Wrapf(errEvent, "event=%q", o.event)
		}
		branch, ok := strings.CutPrefix(o.ref, "refs/heads/")
		if !ok || !b.IsLine(branch) {
			return xerrors.Wrapf(errEvent, "ref=%q は線ではありません", o.ref)
		}
		needs, err := parseNeeds(getenv("NEEDS"))
		if err != nil {
			return err
		}
		rows, verdict := judgeLine(d, needs)
		return report(o.summary, "line-gate", rows, verdict, out)
	default:
		return xerrors.Wrapf(errUsage, "未知のサブコマンド %q", sub)
	}
}

// readDecls は、rev の木から検査の宣言と分岐の宣言を読む。
func readDecls(rev string) (checksdecl.Decl, branchdecl.Branches, error) {
	raw, err := fileAt(rev, checksdecl.File)
	if err != nil {
		return checksdecl.Decl{}, branchdecl.Branches{}, err
	}
	d, err := checksdecl.Parse(raw)
	if err != nil {
		return checksdecl.Decl{}, branchdecl.Branches{}, xerrors.Wrapf(err, "%s:%s", rev, checksdecl.File)
	}
	braw, err := fileAt(rev, branchdecl.File)
	if err != nil {
		return checksdecl.Decl{}, branchdecl.Branches{}, err
	}
	tmp, cleanup, err := workdir.Temp("", "ci-gate-")
	if err != nil {
		return checksdecl.Decl{}, branchdecl.Branches{}, xerrors.Wrap(err, "作業ディレクトリ")
	}
	defer cleanup()
	p := filepath.Join(tmp, "branches.json")
	if err := os.WriteFile(p, braw, 0o600); err != nil {
		return checksdecl.Decl{}, branchdecl.Branches{}, xerrors.Wrap(err, "分岐の宣言の写し")
	}
	b, err := branchdecl.Read(p)
	if err != nil {
		return checksdecl.Decl{}, branchdecl.Branches{}, xerrors.Wrapf(err, "%s:%s", rev, branchdecl.File)
	}
	if err := branchdecl.Validate(b); err != nil {
		return checksdecl.Decl{}, branchdecl.Branches{}, xerrors.Wrapf(err, "%s:%s", rev, branchdecl.File)
	}
	return d, b, nil
}

func writeChanges(output string, should []string, nFiles int, out io.Writer) error {
	enc, err := json.Marshal(should)
	if err != nil {
		return xerrors.Wrap(err, "出力の組み立て")
	}
	fmt.Fprintf(out, "ℹ️ ci-gate changes: 変更したファイル %d 件、走るべき検査 %d 件: %s\n", nFiles, len(should), checksdecl.JoinNames(should))
	if output == "" {
		return nil
	}
	f, err := os.OpenFile(filepath.Clean(output), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return xerrors.Wrapf(err, "%s", output)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "run=%s\n", enc); err != nil {
		return xerrors.Wrapf(err, "%s", output)
	}
	return nil
}

// report は判定の表を out と summary へ全件書き、verdict を返す。行を落とさない。
func report(summary, title string, rows []row, verdict error, out io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n| 検査 | 合否に数える | 走るべき | 結果 | 判定 |\n| --- | --- | --- | --- | --- |\n", title)
	for _, r := range rows {
		mark := "✅"
		if !r.OK {
			mark = "❌"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.Name, yesNo(r.Required), yesNo(r.Should), r.Result, mark)
	}
	if verdict != nil {
		fmt.Fprintf(&b, "\n❌ %v\n", verdict)
	} else {
		fmt.Fprintf(&b, "\n✅ %d 件を確かめました\n", len(rows))
	}
	fmt.Fprint(out, b.String())
	if summary != "" {
		f, err := os.OpenFile(filepath.Clean(summary), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return xerrors.Wrapf(err, "%s", summary)
		}
		defer func() { _ = f.Close() }()
		if _, err := fmt.Fprint(f, b.String()); err != nil {
			return xerrors.Wrapf(err, "%s", summary)
		}
	}
	return verdict
}

func yesNo(v bool) string {
	if v {
		return "はい"
	}
	return "いいえ"
}
