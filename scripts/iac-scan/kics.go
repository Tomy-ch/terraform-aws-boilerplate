package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// kicsFailOn は、KICS の閾値（`--fail-on`）として受ける値です。閾値の canary は INFO の所見で落ちることを求めるので、
// INFO を含む値だけを受けます。
var kicsFailOn = []string{
	"critical,high,medium,low,info",
}

// kicsTool は KICS の起動です。終了コードは閾値に入る所見の最も重い severity を表します（INFO 20 〜 CRITICAL 60）。
// クラッシュ報告は DISABLE_CRASH_REPORT で止めます。
func kicsTool(failOn string) imageTool {
	return imageTool{
		imageStep: stepImageKics,
		command: []string{
			"scan", "-p", "/work/src", "--type", "Terraform", "--disable-secrets", "--no-progress", "--ci",
			"-o", "/tmp/out", "--report-formats", "json,sarif", "--fail-on", failOn,
		},
		environment: map[string]string{"DISABLE_CRASH_REPORT": "0"},
	}
}

const (
	kicsCanaryID = "d0cc8694-fcad-43ff-ac86-32331d7e867f"
	kicsCanary   = `resource "aws_s3_bucket" "canary" {
	bucket = "canary"
}
resource "aws_s3_bucket_public_access_block" "canary" {
	bucket                  = aws_s3_bucket.canary.id
	block_public_acls       = false
	block_public_policy     = false
	ignore_public_acls      = false
	restrict_public_buckets = false
}
`
)

// kicsThresholdCanary は、INFO の所見（tag 無し）だけが残る構成です。閾値が緩められると、INFO の所見は件数0の
// 「所見なし」として通ります。
const (
	kicsThresholdRC     = 20
	kicsThresholdID     = "e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10"
	kicsThresholdCanary = `# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
resource "aws_cloudwatch_log_group" "canary" {
	name              = "canary"
	retention_in_days = 365
	kms_key_id        = "arn:aws:kms:us-east-1:111111111111:key/canary"
}
`
)

// runKics は、走査の root を <base>/tmp/kics/work/src へ写し、2つの canary（Public Access Block を外した構成で落ちる、
// INFO の所見だけで終了コード 20 になる）を確かめてから本走査を起動します。走査したファイルが0件か、写した .tf の数に
// 届かないか、解釈できなかったファイルか実行できなかった query があれば、所見の有無に依らず失敗です —— KICS はそれらを
// 終了コードに出しません。
func runKics(args []string, log io.Writer, r runner) error {
	var failOn string
	s, err := parseScan("kics", args, log, func(f *flag.FlagSet) {
		f.StringVar(&failOn, "fail-on", "", "閾値（"+strings.Join(kicsFailOn, " | ")+"）")
	})
	if err != nil {
		return err
	}
	if !slices.Contains(kicsFailOn, failOn) {
		return xerrors.Wrapf(errUsage, "-fail-on %q は受けない値です（%s）", failOn, strings.Join(kicsFailOn, " | "))
	}
	t := kicsTool(failOn)
	dir, err := prepareWork(s.base, "tmp/kics")
	if err != nil {
		return err
	}
	staged, err := stageTerraform(s.base, s.roots, filepath.Join(dir, "work", "src"))
	if err != nil {
		return err
	}
	want := countSuffix(staged, ".tf")
	image, err := lookupImage(r, t.imageStep)
	if err != nil {
		return err
	}
	err = runCanary("kics の canary", s.base, "tmp/kics-canary", kicsCanary, image, t, r, log,
		func(rc int, out string) error {
			return judgeKicsCanary(rc, filepath.Join(out, "results.json"), kicsCanaryID, 0)
		}, nil)
	if err != nil {
		return err
	}
	fmt.Fprintln(log, "✅ kics の canary: Public Access Block を外した構成を検出しました")
	err = runCanary("kics の閾値の canary", s.base, "tmp/kics-canary-threshold", kicsThresholdCanary, image, t, r, log,
		func(rc int, out string) error {
			return judgeKicsCanary(rc, filepath.Join(out, "results.json"), kicsThresholdID, kicsThresholdRC)
		}, nil)
	if err != nil {
		return err
	}
	fmt.Fprintln(log, "✅ kics の閾値の canary: INFO の所見だけの構成で落ちました")

	rc, err := runImage(dir, image, t, r, log)
	if err != nil {
		return err
	}
	if err := normalizeSARIF(filepath.Join(dir, "out", "results.sarif")); err != nil {
		return err
	}
	n, err := judgeKics(filepath.Join(dir, "out", "results.json"), want)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "kics: ファイル %d 件を走査しました（終了コード %d）\n", n, rc)
	if rc != 0 {
		return xerrors.Wrapf(errFindings, "kics が終了コード %d で終わりました", rc)
	}

	return nil
}

// judgeKics は、走査したファイルの数を返します。0件、want に届かない、走査と解釈の数が違う、実行できなかった query が
// あるときは失敗です。
func judgeKics(p string, want int) (int64, error) {
	v, err := readJSON(p)
	if err != nil {
		return 0, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return 0, xerrors.Wrapf(errResult, "%s が object ではありません", p)
	}
	var n [3]int64
	for i, k := range []string{"files_scanned", "files_parsed", "queries_failed_to_execute"} {
		if n[i], err = intField(m, k); err != nil {
			return 0, xerrors.Wrapf(err, "%s", p)
		}
	}
	scanned, parsed, failed := n[0], n[1], n[2]
	switch {
	case scanned == 0:
		return 0, xerrors.Wrap(errNoTargets, "kics: 走査したファイルが0件です")
	case scanned < int64(want):
		return 0, xerrors.Wrapf(errCoverage, "kics: 走査したファイル %d 件が、写した .tf の %d 件に届きません", scanned, want)
	case parsed != scanned:
		return 0, xerrors.Wrapf(errCoverage, "kics: 走査したファイル %d 件のうち、解釈したのは %d 件です", scanned, parsed)
	case failed != 0:
		return 0, xerrors.Wrapf(errCoverage, "kics: 実行できなかった query が %d 件あります", failed)
	}

	return scanned, nil
}

// judgeKicsCanary は、canary の終了コードと、報告した query に id があることを確かめます。wantRC が0なら非0の
// 終了コードを、0でなければその値を求めます。
func judgeKicsCanary(rc int, p, id string, wantRC int) error {
	switch {
	case rc == 0:
		return xerrors.Wrapf(errCanary, "kics: %s を報告するはずの構成でゲートが落ちませんでした", id)
	case wantRC != 0 && rc != wantRC:
		return xerrors.Wrapf(errCanary, "kics: 終了コード %d を期待しましたが %d でした（%s）", wantRC, rc, id)
	}
	v, err := readJSON(p)
	if err != nil {
		return xerrors.Wrapf(errCanary, "kics: canary の結果を読めません（終了コード %d）: %v", rc, err)
	}
	m, _ := v.(map[string]any)
	queries, _ := m["queries"].([]any)
	for _, q := range queries {
		if qm, ok := q.(map[string]any); ok && qm["query_id"] == id {
			return nil
		}
	}

	return xerrors.Wrapf(errCanary, "kics: %s を報告せずに終了しました（終了コード %d）", id, rc)
}
