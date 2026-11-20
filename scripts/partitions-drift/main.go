// partitions-drift は、テストが手で写した partition と region の一覧を、CI の `terraform test` が解決する
// hashicorp/aws の版が使う aws-sdk-go-base の endpoints と突き合わせます。
//
//	partitions-drift -versions-tf <versions.tf> -copy <*.tftest.hcl> [-summary-out <md>] [-state-out <path>]
//
// 比べる版は、-versions-tf の制約を満たす最新の正式版です。lock を持たない module の `terraform test` が
// 解決する版がそれだからです（ADR-0205 決定9）。終了コードは 0 が一致、1 がずれ、2 が確認不能です。
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
	registryBase = "https://registry.terraform.io"
	rawBase      = "https://raw.githubusercontent.com"
	fetchTimeout = 30 * time.Second
	summaryPerm  = 0o644

	stateMatch       = "match"
	stateDrift       = "drift"
	stateUnconfirmed = "unconfirmed"
)

var (
	errUsage       = xerrors.New("使い方: partitions-drift -versions-tf <path> -copy <path> [-summary-out <path>] [-state-out <path>]")
	errDrift       = xerrors.New("写しが上流の一覧と一致しません")
	errSummaryFile = xerrors.New("要約を書けません")
	errStateFile   = xerrors.New("判定を書けません")
	errOutput      = xerrors.New("要約を出力できません")
	errNotCompared = xerrors.New("比べる前に終わりました")
)

func main() {
	log.SetFlags(0)
	client := &http.Client{Timeout: fetchTimeout}
	err := run(context.Background(), os.Args[1:], upstream{client: client, registry: registryBase, raw: rawBase}, os.Stdout)
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
	versionsTF := fset.String("versions-tf", "", "比べる provider の source と版の制約を持つ versions.tf")
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
	if *versionsTF == "" || *copyPath == "" || fset.NArg() > 0 {
		return errUsage
	}

	r := compareWithUpstream(ctx, up, *versionsTF, *copyPath)
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
func compareWithUpstream(ctx context.Context, up upstream, versionsTF, copyPath string) report {
	var r report
	cp, err := readCopy(copyPath)
	if err != nil {
		r.err = err

		return r
	}
	source, constraint, err := readConstraint(versionsTF)
	if err != nil {
		r.err = err

		return r
	}
	r.constraint = constraint
	versions, err := up.providerVersions(ctx, source)
	if err != nil {
		r.err = err

		return r
	}
	r.provider, err = latestMatching(versions, constraint)
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
