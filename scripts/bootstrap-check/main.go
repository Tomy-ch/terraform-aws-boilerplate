// Package main は、bootstrap の手順書が完了条件として人に実行させる確認の判定を持つツール。
// plan は make が渡した内容を読んで判定するだけで、terraform も aws も git も起動しない（ADR-0702 決定24）。
// account・verify・plan-permissions は aws だけを起動して読む。資格情報を変える環境変数の外し方と AWS_PROFILE は
// make か tf-local が整える。
//
//	bootstrap-check plan [-partial] -header-file <file> <plan の JSON | ->
//	    `terraform show -json` の出力（- なら標準入力）を、期待値と突き合わせる。-header-file は
//	    `tf-local plan-header` の出力で、どの commit に対する plan かを先頭に出すために読む。
//	    -partial は段4 のやり直し用で、期待値のうち plan に無いもの（apply 済み）を「済み」とし、
//	    余剰（変更・破棄・期待値に無い追加）が無いことだけを見る
//	bootstrap-check plan -mock -test-exit-file <file> <terraform test -json -verbose の出力>
//	    env/admin/identity/tests/imports.tftest.hcl の run "check_plan" が mock の provider で作った plan を、
//	    同じ判定に通す。-test-exit-file は、その terraform test の終了コードを書いたファイル
//	bootstrap-check account -project <project> -region <region>
//	    アカウントの準備（docs/setup/bootstrap.md の 1-0 から 2-4）を、一時 IAM user の資格情報で読んで確かめる。
//	    何も作らず、何も変えない
//	bootstrap-check verify -stage <stage> -project <project> -region <region> [-executor <user>] [-policy-source live|reference -reference-dir <make tf-policy の -out>]
//	    bootstrap の段の確認（stage1・before-apply・stage4・permissions・stage5・output-policy）を、実環境を読んで行う。
//	    -executor は stage4 の、段3 で例外に入れた段4 の実行者。-policy-source は output-policy が評価する
//	    Bucket Policy の文書の出所（既定は live）。何も作らず、何も変えない
//	bootstrap-check plan-permissions -plan <terraform show -json の出力> -project <project> -region <region>
//	    identity root の plan の CI の role の policy の文書を、permissions と同じ表で simulate-custom-policy にかける。
//	    CI の plan の job（tf-local ci-permissions）が plan の role の資格情報で起動する。出力は区分ごとの件数と NG の行だけで、
//	    resource の ARN と context の値を出さない。何も作らず、何も変えない
//
// 終了コードは、0 が一致、1 が差分あり、2 が検証できない（引数の誤りを含む）。出力は標準出力にまとめ、
// 12桁の数字（アカウント ID）は伏せる。出すのは resource address と変更の種類だけで、属性の値は出さない。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	defaultExpected = "docs/setup/scripts/check-plan.expected"
	// commitLinePrefix は、checkPlan が header から作る commit の行の先頭。
	commitLinePrefix = "# commit: "

	mockMatched      = "✅ check-plan.expected は identity root の mock の plan と一致します"
	mockMismatch     = "❌ check-plan.expected が identity root の plan と一致しません（NG の行を期待値へ写す。docs/setup/bootstrap.md の「この文書の保守」）"
	mockUnverifiable = "❌ check-plan.expected と identity root の plan を突き合わせられません（理由は上の行）"
)

var (
	accountPattern = regexp.MustCompile(`[0-9]{12}`)

	errUsage = xerrors.New("使い方: bootstrap-check plan [-partial] -header-file <plan-header の出力> <plan の JSON | -> | bootstrap-check plan -mock -test-exit-file <file> <terraform test -json -verbose の出力> | bootstrap-check account -project <project> -region <region> | bootstrap-check verify -stage <stage1|before-apply|stage4|permissions|stage5|output-policy> -project <project> -region <region> [-executor <user>] [-policy-source live|reference -reference-dir <make tf-policy の -out>] | bootstrap-check plan-permissions -plan <terraform show -json の出力> -project <project> -region <region>")
	// errMismatch は、期待値と plan に差分があること。理由は結果の行が述べるので、メッセージを持たない。
	errMismatch = xerrors.New("差分があります")
)

type deps struct {
	expected string
	readFile func(string) ([]byte, error)
	stdin    io.Reader
	aws      awsFunc
	getenv   func(string) string
}

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, deps{
		expected: defaultExpected, readFile: os.ReadFile, stdin: os.Stdin, aws: hostAWS("aws"), getenv: os.Getenv,
	}))
}

// execute は、判定の出力を伏せてから stdout へ書き、終了コードを返す。出力を書けなかったときは、
// 判定が一致でも差分でも 2 を返す。
func execute(args []string, stdout io.Writer, d deps) int {
	var out bytes.Buffer

	err := run(args, &out, d)

	mask := maskAccounts
	if len(args) > 0 && (args[0] == "account" || args[0] == "verify" || args[0] == "plan-permissions") {
		mask = maskAllAccounts
	}

	if _, werr := io.WriteString(stdout, mask(out.String())); werr != nil {
		return 2
	}

	return exitCode(err)
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errMismatch):
		return 1
	default:
		return 2
	}
}

// maskAccounts は、出力をそのまま Pull Request に貼れるように、12桁の数字（アカウント ID）を伏せる。
// commit の sha の中の数字の並びを伏せて commit を読めなくしないよう、commit の行と、16進の英字に
// 隣り合う並びは伏せない。
func maskAccounts(s string) string {
	lines := strings.SplitAfter(s, "\n")

	for i, line := range lines {
		if strings.HasPrefix(line, commitLinePrefix) {
			continue
		}

		lines[i] = maskLine(line)
	}

	return strings.Join(lines, "")
}

// maskAllAccounts は、12桁の数字をすべて伏せる。commit の行を出さない account・verify・plan-permissions の出力に使う。
func maskAllAccounts(s string) string {
	return accountPattern.ReplaceAllString(s, "<account>")
}

func maskLine(line string) string {
	var b strings.Builder

	last := 0

	for _, m := range accountPattern.FindAllStringIndex(line, -1) {
		if isHexLetterAt(line, m[0]-1) || isHexLetterAt(line, m[1]) {
			continue
		}

		b.WriteString(line[last:m[0]])
		b.WriteString("<account>")

		last = m[1]
	}

	b.WriteString(line[last:])

	return b.String()
}

func isHexLetterAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}

	c := s[i]

	return (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

type options struct {
	partial      bool
	mock         bool
	headerFile   string
	testExitFile string
	input        string
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var o options

	fs.BoolVar(&o.partial, "partial", false, "")
	fs.BoolVar(&o.mock, "mock", false, "")
	fs.StringVar(&o.headerFile, "header-file", "", "")
	fs.StringVar(&o.testExitFile, "test-exit-file", "", "")

	if err := fs.Parse(args); err != nil {
		return options{}, xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 1 {
		return options{}, errUsage
	}

	o.input = fs.Arg(0)

	switch {
	case o.mock && (o.partial || o.headerFile != ""):
		return options{}, errUsage
	case o.mock && o.testExitFile == "":
		return options{}, errUsage
	case !o.mock && (o.headerFile == "" || o.testExitFile != ""):
		return options{}, errUsage
	}

	return o, nil
}

func run(args []string, out io.Writer, d deps) error {
	if len(args) > 0 && args[0] == "account" {
		o, err := parseAccountArgs(args[1:])
		if err != nil {
			return finish(out, false, err)
		}

		return finish(out, false, checkAccount(o, out, d))
	}

	if len(args) > 0 && args[0] == "verify" {
		o, err := parseVerifyArgs(args[1:])
		if err != nil {
			return finish(out, false, err)
		}

		return finish(out, false, checkVerify(o, out, d))
	}

	if len(args) > 0 && args[0] == "plan-permissions" {
		o, err := parsePlanPermArgs(args[1:])
		if err != nil {
			return finish(out, false, err)
		}

		return finish(out, false, checkPlanPermissions(o, out, d))
	}

	if len(args) == 0 || args[0] != "plan" {
		return finish(out, false, errUsage)
	}

	o, err := parseArgs(args[1:])
	if err != nil {
		return finish(out, false, err)
	}

	return finish(out, o.mock, checkPlan(o, out, d))
}

// finish は、結果の行の後に出す行を足す。mock の経路は CI の make check-plan-expected の入口なので、
// 人が読む合否の行（✅ ❌）もここで出す。
func finish(out io.Writer, mock bool, err error) error {
	switch {
	case err == nil:
		if mock {
			fmt.Fprintln(out, mockMatched)
		}
	case xerrors.Is(err, errMismatch):
		if mock {
			fmt.Fprintln(out, mockMismatch)
		}
	default:
		fmt.Fprintln(out, err)

		if mock {
			fmt.Fprintln(out, mockUnverifiable)
		}
	}

	return err
}
