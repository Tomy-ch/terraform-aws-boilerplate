// partitions-drift は、テストが手で写した partition と region の一覧を、CI の `terraform test` が解決する
// hashicorp/aws の版が使う aws-sdk-go-base の endpoints と突き合わせます。
//
//	partitions-drift -lock <.terraform.lock.hcl> -copy <*.tftest.hcl> [-summary-out <md>] [-state-out <path>]
//
// 比べる版は、provider の版の正本の lock が固定する版です。root でない module の `terraform test` も、その版で
// 走ります（ADR-0205 決定9）。終了コードは 0 が一致、1 がずれ、2 が確認不能です。
// make を通すと非0の終了コードは区別できないので、-state-out に同じ判定を match・drift・unconfirmed で書きます。
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	rawBase      = "https://raw.githubusercontent.com"
	fetchTimeout = 30 * time.Second
	summaryPerm  = 0o644

	stateMatch       = "match"
	stateDrift       = "drift"
	stateUnconfirmed = "unconfirmed"
)

var (
	errUsage       = xerrors.New("使い方: partitions-drift -lock <path> -copy <path> [-summary-out <path>] [-state-out <path>]")
	errDrift       = xerrors.New("写しが上流の一覧と一致しません")
	errSummaryFile = xerrors.New("要約を書けません")
	errStateFile   = xerrors.New("判定を書けません")
	errOutput      = xerrors.New("要約を出力できません")
	errNotCompared = xerrors.New("比べる前に終わりました")
)

func main() {
	log.SetFlags(0)
	client := &http.Client{Timeout: fetchTimeout}
	err := run(context.Background(), os.Args[1:], upstream{client: client, raw: rawBase}, os.Stdout)
	if err != nil {
		log.Printf("❌ partitions-drift: %v", err)
	}
	os.Exit(exitCode(err))
}

func exitCode(err error) int {
	switch stateOf(err) {
	case stateMatch:
		return 0
	case stateDrift:
		return 1
	default:
		return 2
	}
}

func stateOf(err error) string {
	switch {
	case err == nil:
		return stateMatch
	case xerrors.Is(err, errDrift):
		return stateDrift
	default:
		return stateUnconfirmed
	}
}

// run は、確認できなかったときも -summary-out と -state-out を書きます。週次の報告が、ずれと確認不能を
// 同じ issue へ書くためです。-state-out は要約を書けた後にだけ書くので、在れば要約も在ります。
func run(ctx context.Context, args []string, up upstream, out io.Writer) error {
	fset := flag.NewFlagSet("partitions-drift", flag.ContinueOnError)
	fset.SetOutput(out)
	lockPath := fset.String("lock", "", "比べる hashicorp/aws の版を持つ provider の版の正本の lock")
	copyPath := fset.String("copy", "", "写し（variables の aws_partitions）を持つ .tftest.hcl")
	summaryOut := fset.String("summary-out", "", "Markdown の要約を書くパス")
	stateOut := fset.String("state-out", "", "判定（match・drift・unconfirmed）を書くパス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	// 前の実行の要約と判定が残っていると、今回それを書かずに終わっても読まれてしまう。
	for path, sentinel := range map[string]error{*summaryOut: errSummaryFile, *stateOut: errStateFile} {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return xerrors.Wrapf(sentinel, "%v", err)
		}
	}
	if *lockPath == "" || *copyPath == "" || fset.NArg() > 0 {
		return errUsage
	}

	r := compareWithUpstream(ctx, up, *lockPath, *copyPath)
	summary := renderSummary(r)
	if _, err := io.WriteString(out, summary); err != nil {
		return xerrors.Wrapf(errOutput, "%v", err)
	}
	if *summaryOut != "" {
		if err := os.WriteFile(*summaryOut, []byte(summary), summaryPerm); err != nil {
			return xerrors.Wrapf(errSummaryFile, "%v", err)
		}
	}

	verdict := r.verdict()
	if *stateOut != "" {
		if err := os.WriteFile(*stateOut, []byte(stateOf(verdict)+"\n"), summaryPerm); err != nil {
			return xerrors.Wrapf(errStateFile, "%v", err)
		}
	}

	return verdict
}

// compareWithUpstream は、途中で失敗しても、そこまでに分かった版と URL を report に残します。
func compareWithUpstream(ctx context.Context, up upstream, lockPath, copyPath string) report {
	var r report
	cp, err := readCopy(copyPath)
	if err != nil {
		r.err = err

		return r
	}
	r.lock = lockPath
	r.provider, err = readLockedVersion(lockPath)
	if err != nil {
		r.err = err

		return r
	}
	r.sdk, r.goModURL, err = up.sdkVersion(ctx, r.provider)
	if err != nil {
		r.err = err

		return r
	}
	src, url, err := up.endpoints(ctx, r.sdk)
	r.endpointsURL = url
	if err != nil {
		r.err = err

		return r
	}
	parsed, err := parseEndpoints(src)
	if err != nil {
		r.err = err

		return r
	}
	r.drift = compare(parsed, cp)
	r.compared = true

	return r
}
