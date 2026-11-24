package main

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// checkovTool は Checkov の起動です。Checkov は severity を返さないので、所見が1件でもあれば非0で終わります。
// `--skip-download` で Prisma Cloud からの取得を止めます。
var checkovTool = imageTool{
	imageStep: stepImageCheckov,
	command: []string{
		"-d", "/work/src", "--framework", "terraform", "--skip-download", "--compact", "--quiet",
		"-o", "cli", "-o", "json", "-o", "sarif",
		"--output-file-path", "console,/tmp/out/results_json.json,/tmp/out/results_sarif.sarif",
	},
}

// checkovCanaryID は、ACL を有効にした S3 Bucket を落とす検査です。
const checkovCanaryID = "CKV2_AWS_65"

const checkovCanary = `resource "aws_s3_bucket" "canary" {
  bucket = "canary"
}
resource "aws_s3_bucket_ownership_controls" "canary" {
  bucket = aws_s3_bucket.canary.id
  rule {
    object_ownership = "ObjectWriter"
  }
}
resource "aws_s3_bucket_acl" "canary" {
  bucket = aws_s3_bucket.canary.id
  acl    = "private"
}
`

// runCheckov は、走査の root を <base>/tmp/checkov/work/src へ写し、canary が CKV2_AWS_65 で落ちることを確かめてから
// 本走査を起動します。走査した resource が0件か、解釈できなかったファイルがあれば、所見の有無に依らず失敗です ——
// Checkov は解釈できないファイルを飛ばして終了コードを変えません。
func runCheckov(args []string, log io.Writer, r runner) error {
	s, err := parseScan("checkov", args, log, nil)
	if err != nil {
		return err
	}
	dir, err := prepareWork(s.base, "tmp/checkov")
	if err != nil {
		return err
	}
	if _, err := stageTerraform(s.base, s.roots, filepath.Join(dir, "work", "src")); err != nil {
		return err
	}
	image, err := lookupImage(r, checkovTool.imageStep)
	if err != nil {
		return err
	}
	err = runCanary("checkov の canary", s.base, "tmp/checkov-canary", checkovCanary, image, checkovTool, r, log,
		func(rc int, out string) error {
			return judgeCheckovCanary(rc, filepath.Join(out, "results_json.json"))
		}, nil)
	if err != nil {
		return err
	}
	fmt.Fprintln(log, "✅ checkov の canary: ACL を有効にした構成を "+checkovCanaryID+" で検出しました")

	rc, err := runImage(dir, image, checkovTool, r, log)
	if err != nil {
		return err
	}
	if err := normalizeSARIF(filepath.Join(dir, "out", "results_sarif.sarif")); err != nil {
		return err
	}
	resources, parseErrors, err := checkovSummary(filepath.Join(dir, "out", "results_json.json"))
	switch {
	case err != nil:
		return err
	case resources == 0:
		return xerrors.Wrap(errNoTargets, "checkov: 走査した resource が0件です")
	case parseErrors != 0:
		return xerrors.Wrapf(errCoverage, "checkov: 解釈できなかったファイルが %d 件あります", parseErrors)
	}
	fmt.Fprintf(log, "checkov: resource %d 件を走査しました（終了コード %d）\n", resources, rc)
	if rc != 0 {
		return xerrors.Wrapf(errFindings, "checkov が終了コード %d で終わりました", rc)
	}

	return nil
}

// checkovReports は、results_json.json の報告を返します。framework が1つなら object、複数なら array です。
func checkovReports(p string) ([]map[string]any, error) {
	v, err := readJSON(p)
	if err != nil {
		return nil, err
	}
	var items []any
	switch x := v.(type) {
	case map[string]any:
		items = []any{x}
	case []any:
		items = x
	default:
		return nil, xerrors.Wrapf(errResult, "%s が object でも array でもありません", p)
	}
	if len(items) == 0 {
		return nil, xerrors.Wrapf(errResult, "%s に報告がありません", p)
	}
	reports := make([]map[string]any, 0, len(items))
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, xerrors.Wrapf(errResult, "%s の %d 番目の報告が object ではありません", p, i)
		}
		reports = append(reports, m)
	}

	return reports, nil
}

// checkovSummary は、すべての報告の summary の resource_count と parsing_errors の和を返します。
func checkovSummary(p string) (resources, parseErrors int64, err error) {
	reports, err := checkovReports(p)
	if err != nil {
		return 0, 0, err
	}
	for i, r := range reports {
		sum, ok := r["summary"].(map[string]any)
		if !ok {
			return 0, 0, xerrors.Wrapf(errResult, "%s の %d 番目の報告に summary がありません", p, i)
		}
		n, err := intField(sum, "resource_count")
		if err != nil {
			return 0, 0, xerrors.Wrapf(err, "%s", p)
		}
		e, err := intField(sum, "parsing_errors")
		if err != nil {
			return 0, 0, xerrors.Wrapf(err, "%s", p)
		}
		resources += n
		parseErrors += e
	}

	return resources, parseErrors, nil
}

func judgeCheckovCanary(rc int, p string) error {
	if rc == 0 {
		return xerrors.Wrap(errCanary, "checkov: ACL を有効にした構成でゲートが落ちませんでした")
	}
	reports, err := checkovReports(p)
	if err != nil {
		return xerrors.Wrapf(errCanary, "checkov: canary の結果を読めません（終了コード %d）: %v", rc, err)
	}
	var ids []string
	for _, r := range reports {
		results, _ := r["results"].(map[string]any)
		failed, _ := results["failed_checks"].([]any)
		for _, f := range failed {
			if c, ok := f.(map[string]any); ok {
				if id, ok := c["check_id"].(string); ok {
					ids = append(ids, id)
				}
			}
		}
	}
	if !slices.Contains(ids, checkovCanaryID) {
		return xerrors.Wrapf(errCanary, "checkov: %s を報告せずに終了しました（終了コード %d）", checkovCanaryID, rc)
	}

	return nil
}
