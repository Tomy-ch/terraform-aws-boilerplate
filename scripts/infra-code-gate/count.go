package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// countedSuffixes は、Terraform が構成として読むファイルです（ADR-0103 決定11）。読んだファイルの集合と find の
// 一覧との突き合わせは hcl-input が持ち、ここは走査の対象が0件でないこと、root の外に構成ファイルが無いことに使います。
var countedSuffixes = []string{".tf", ".tf.json", ".tftest.hcl", ".tftest.json", ".tfquery.hcl"}

var (
	errResults     = xerrors.New("Conftest の結果を解釈できません")
	errMissingRoot = xerrors.New("走査の root がありません")
)

// conftestResult は `conftest test --output json` の1要素です。
type conftestResult struct {
	Filename   string            `json:"filename"`
	Namespace  string            `json:"namespace"`
	Successes  int               `json:"successes"`
	Failures   []json.RawMessage `json:"failures"`
	Warnings   []json.RawMessage `json:"warnings"`
	Exceptions []json.RawMessage `json:"exceptions"`
}

func runCount(args []string, repo string, out io.Writer) error {
	fset := flag.NewFlagSet("count", flag.ContinueOnError)
	fset.SetOutput(out)
	combinedPath := fset.String("combined", "", "`conftest test --output json` の出力のパス")
	repoFlag := fset.String("repo", repo, "走査の root を解くリポジトリの root")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}
	repo = *repoFlag
	roots := fset.Args()
	if *combinedPath == "" || len(roots) == 0 {
		return xerrors.Wrap(errUsage, "count -combined <path> <root>...")
	}

	walked, err := walkConfigs(repo, roots)
	if err != nil {
		return err
	}
	if len(walked) == 0 {
		return xerrors.Wrapf(errNoInput, "Terraform の構成ファイルが %s に1件もありません", strings.Join(roots, " "))
	}
	combined, err := readResults(*combinedPath)
	if err != nil {
		return err
	}
	tracked, err := gitLsFiles(repo)
	if err != nil {
		return err
	}
	if len(tracked) == 0 {
		return xerrors.Wrapf(errNoInput, "%s で追跡しているファイルが1件もありません", repo)
	}

	findings := evaluated(combined, *combinedPath)
	findings = append(findings, violations(combined, *combinedPath)...)
	findings = append(findings, outsideRoots(tracked, roots)...)
	findings = append(findings, trackedTerraformDirs(tracked)...)
	if err := report(out, "count", findings); err != nil {
		return err
	}
	fmt.Fprintf(out, "✅ infra-code-gate count: 走査の root の構成ファイル %d 件について検出の規則が評価され、root の外と .terraform の下に追跡している構成ファイルはありません\n", len(walked))
	return nil
}

func readResults(p string) ([]conftestResult, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	var results []conftestResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, xerrors.Wrapf(errResults, "%s: %v", p, err)
	}
	return results, nil
}

// walkConfigs は roots の下の構成ファイルを、Conftest とは別の経路で列挙します。`.terraform` は init が
// 落とした写しなので降りません。
func walkConfigs(repo string, roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		base := filepath.Join(repo, filepath.FromSlash(root))
		if info, err := os.Stat(base); err != nil || !info.IsDir() {
			return nil, xerrors.Wrapf(errMissingRoot, "%s", root)
		}
		err := filepath.WalkDir(base, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() && e.Name() == ".terraform" {
				return filepath.SkipDir
			}
			if !e.IsDir() && hasSuffix(e.Name(), countedSuffixes) {
				rel, err := filepath.Rel(repo, p)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の走査", root)
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// violations は、Conftest の結果の違反を、その文のまま finding にします。Conftest の終了コードは make が
// 捨てるので、違反で落とすのはここです。
func violations(results []conftestResult, file string) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, r := range results {
		for _, f := range r.Failures {
			var v struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(f, &v); err != nil || v.Msg == "" {
				findings = append(findings, lintreport.Finding{File: file, Line: 1, Message: fmt.Sprintf("名前空間 %q の違反を読めません: %s", r.Namespace, string(f))})

				continue
			}
			findings = append(findings, lintreport.Finding{File: file, Line: 1, Message: v.Msg})
		}
	}

	return findings
}

// evaluated は、全ファイルを1つの入力として掛けた規則が実際に評価されたことを検査します。規則の無い
// 名前空間を渡された Conftest は、何も評価せずに 0 で終えます。
func evaluated(results []conftestResult, file string) []lintreport.Finding {
	if len(results) == 0 {
		return []lintreport.Finding{{File: file, Line: 1, Message: "検出の結果が1件もありません"}}
	}
	var findings []lintreport.Finding
	for _, r := range results {
		if r.Successes+len(r.Failures)+len(r.Warnings)+len(r.Exceptions) == 0 {
			findings = append(findings, lintreport.Finding{File: file, Line: 1, Message: fmt.Sprintf("検出の名前空間 %q の規則が1つも評価されていません", r.Namespace)})
		}
	}
	return findings
}

// trackedTerraformDirs は、`.terraform` を段に持つ追跡ファイルを返します。そこは init が落とす写しの置き場で、
// 検出も件数の突き合わせも歩かないが、Terraform は module の source として読める。
func trackedTerraformDirs(tracked []string) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, f := range tracked {
		if slices.Contains(strings.Split(f, "/"), ".terraform") {
			findings = append(findings, lintreport.Finding{File: f, Line: 1, Message: "`.terraform` の下のファイルを追跡しています。検出はこの下を見ません"})
		}
	}
	return findings
}

// outsideRoots は、追跡している構成ファイルのうち、走査の root の外に在るものを返します。root の外に
// 置いた構成は、どちらの経路も見ないので、件数の突き合わせでは捕まりません。
func outsideRoots(tracked, roots []string) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, f := range tracked {
		if !hasSuffix(f, countedSuffixes) {
			continue
		}
		inside := false
		for _, r := range roots {
			if strings.HasPrefix(f, strings.TrimSuffix(r, "/")+"/") {
				inside = true
			}
		}
		if !inside {
			findings = append(findings, lintreport.Finding{File: f, Line: 1, Message: fmt.Sprintf("Terraform の構成ファイルが走査の root（%s）の外に在ります", strings.Join(roots, " "))})
		}
	}
	return findings
}

func gitLsFiles(repo string) ([]string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "ls-files", "-z")
	cmd.Dir = repo
	raw, err := cmd.Output()
	if err != nil {
		return nil, xerrors.Wrapf(err, "git ls-files")
	}
	var files []string
	for _, f := range strings.Split(string(raw), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}
