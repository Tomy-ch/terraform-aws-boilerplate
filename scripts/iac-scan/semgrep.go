package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// semgrepTool は Semgrep CE の起動です。rule は work/cfg/terraform に写したものを読みます。所見があれば severity に
// 関わらず非0で終わり（`--error`）、解釈できなかった箇所でも非0で終わります（`--strict`）。
var semgrepTool = imageTool{
	imageStep: stepImageSemgrep,
	command: []string{
		"semgrep", "scan", "--config", "terraform", "--metrics", "off", "--disable-version-check", "--error", "--strict",
		"--json-output=/tmp/out/results.json", "--sarif-output=/tmp/out/results.sarif", "/work/src",
	},
	workingDir: "/work/cfg",
}

// semgrepCanary は、固定した rule の取得と `--error` の両方が効いていることの証明です。
const (
	semgrepCanaryID = "terraform.lang.security.s3-public-rw-bucket"
	semgrepCanary   = `resource "aws_s3_bucket" "canary" {
  bucket = "canary"
  acl    = "public-read-write"
}
`
)

// runSemgrep は、走査の root を <base>/tmp/semgrep/work/src へ、rule を work/cfg へ写し（空の .semgrepignore も
// stageSemgrepWork が置きます）、canary が s3-public-rw-bucket で落ちることを確かめてから本走査を起動します。
// 走査したファイルが0件か、写した .tf と .hcl の数に届かなければ、所見の有無に依らず失敗です。
func runSemgrep(args []string, log io.Writer, tool toolFunc) error {
	var rules, sha string
	s, err := parseScan("semgrep", args, log, func(f *flag.FlagSet) {
		f.StringVar(&rules, "rules", "", "semgrep-rules の tarball")
		f.StringVar(&sha, "rules-sha256", "", "tarball の terraform/ の rule の sha256（stageRules の形）")
	})
	if err != nil {
		return err
	}
	if rules == "" || sha == "" {
		return xerrors.Wrap(errUsage, "iac-scan semgrep は -rules と -rules-sha256 が要ります")
	}
	dir := filepath.Join(s.base, "tmp", "semgrep")
	if err := prepareWork(dir); err != nil {
		return err
	}
	staged, err := stageTerraform(s.base, s.roots, filepath.Join(dir, "work", "src"))
	if err != nil {
		return err
	}
	want := countSuffix(staged, ".tf", ".hcl")
	if err := stageSemgrepWork(dir, rules, sha); err != nil {
		return err
	}
	image, err := lookupImage(tool, semgrepTool.imageStep)
	if err != nil {
		return err
	}
	canary := filepath.Join(s.base, "tmp", "semgrep-canary")
	err = runCanary("semgrep の canary", canary, semgrepCanary, image, semgrepTool, tool, log,
		func(rc int, out string) error {
			return judgeSemgrepCanary(rc, filepath.Join(out, "results.json"))
		}, func() error { return stageSemgrepWork(canary, rules, sha) })
	if err != nil {
		return err
	}
	fmt.Fprintln(log, "✅ semgrep の canary: public-read-write の構成を検出しました")

	rc, err := runImage(dir, image, semgrepTool, tool, log)
	if err != nil {
		return err
	}
	if err := normalizeSARIF(filepath.Join(dir, "out", "results.sarif")); err != nil {
		return err
	}
	n, err := judgeSemgrep(filepath.Join(dir, "out", "results.json"), want)
	if err != nil {
		return err
	}
	fmt.Fprintf(log, "semgrep: ファイル %d 件を走査しました（終了コード %d）\n", n, rc)
	if rc != 0 {
		return xerrors.Wrapf(errFindings, "semgrep が終了コード %d で終わりました", rc)
	}

	return nil
}

// stageSemgrepWork は、dir の work/cfg へ rule を写し、work/src に空の .semgrepignore を置きます。既定の
// .semgrepignore は tests/ を外すので、置かなければ Trivy と同じ面を見ません。
func stageSemgrepWork(dir, rules, sha string) error {
	if err := stageRules(rules, filepath.Join(dir, "work", "cfg"), sha); err != nil {
		return err
	}

	return writeText(filepath.Join(dir, "work", "src", ".semgrepignore"), "")
}

// judgeSemgrep は、走査したファイルの数を返します。paths.scanned が配列でない、0件、want に届かないときは失敗です。
func judgeSemgrep(p string, want int) (int, error) {
	v, err := readJSON(p)
	if err != nil {
		return 0, err
	}
	m, _ := v.(map[string]any)
	paths, _ := m["paths"].(map[string]any)
	scanned, ok := paths["scanned"].([]any)
	switch {
	case !ok:
		return 0, xerrors.Wrapf(errResult, "%s の paths.scanned が配列ではありません", p)
	case len(scanned) == 0:
		return 0, xerrors.Wrap(errNoTargets, "semgrep: 走査したファイルが0件です")
	case len(scanned) < want:
		return 0, xerrors.Wrapf(errCoverage, "semgrep: 走査したファイル %d 件が、写した .tf / .hcl の %d 件に届きません", len(scanned), want)
	}

	return len(scanned), nil
}

func judgeSemgrepCanary(rc int, p string) error {
	if rc == 0 {
		return xerrors.Wrap(errCanary, "semgrep: public-read-write の構成でゲートが落ちませんでした")
	}
	v, err := readJSON(p)
	if err != nil {
		return xerrors.Wrapf(errCanary, "semgrep: canary の結果を読めません（終了コード %d）: %v", rc, err)
	}
	m, _ := v.(map[string]any)
	results, _ := m["results"].([]any)
	for _, r := range results {
		if rm, ok := r.(map[string]any); ok && rm["check_id"] == semgrepCanaryID {
			return nil
		}
	}

	return xerrors.Wrapf(errCanary, "semgrep: %s を報告せずに終了しました（終了コード %d）", semgrepCanaryID, rc)
}
