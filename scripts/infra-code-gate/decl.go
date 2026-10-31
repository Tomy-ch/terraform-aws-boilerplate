package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/adrdoc"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	defaultRulesetPath    = ".github/settings/branch-protection.json"
	defaultCodeownersPath = ".github/CODEOWNERS"
	// gateContext は検出の job が報告する required context です（ADR-0103 決定14）。
	gateContext = "infra-code-gate"
)

// codeownerTargets は、code owner の宣言が持たなければならない対象です（ADR-0103 決定14）。
// 本リポジトリ所有の項目が指すソースの所在は、宣言から足します。
var codeownerTargets = []string{
	"conftest/hcl/",
	"scripts/infra-code-gate/",
	"scripts/lib/adrdoc/",
	"scripts/tf-job-lint/",
	".makefiles/terraform/infra-code.mk",
	".makefiles/terraform/infra-code-test.mk",
	".github/workflows/terraform-gates.yaml",
	".github/settings/branch-protection.json",
	".github/CODEOWNERS",
}

// requiredHeadings は、本リポジトリ所有の項目が参照する ADR が持つ見出しです（ADR-0103 決定13）。
var requiredHeadings = []string{
	"ネイティブ統合で解けない理由",
	"入出力",
	"失敗時の扱い",
	"業務要件の変更で修正が必要にならない理由",
}

var (
	typeKey     = regexp.MustCompile(`^(resource|data|ephemeral|action)\.[a-z0-9_]+$`)
	itemID      = regexp.MustCompile(`^[^#\s]+#(?:(?:resource|data|ephemeral|action)\.[a-z0-9_]+\.[A-Za-z0-9_-]+|provider\.[a-z0-9_-]+(?:\.[A-Za-z0-9_-]+)?|removed\.[A-Za-z0-9_.-]+)$`)
	repository  = regexp.MustCompile(`^[a-z0-9.-]+/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	ownerToken  = regexp.MustCompile(`^(?:@[A-Za-z0-9-]+(?:/[A-Za-z0-9._-]+)?|[^@\s]+@[^@\s]+)$`)
	configFiles = []string{".tf", ".tf.json"}
	// topLevels は、ディレクトリ1段だけでユースケース・module・環境の全体を指す形です。
	topLevels = regexp.MustCompile(`^(?:usecases|modules|env)/[^/]+$`)
)

var (
	errDeclShape    = xerrors.New("宣言を解釈できません")
	errDuplicateKey = xerrors.New("宣言に同じキーが2回現れます")
	errCodeowners   = xerrors.New("code owner の宣言に解釈できない行があります")
)

type document struct {
	Decl *declaration `json:"infra_code_decl"`
}

type declaration struct {
	Types     map[string]typeClass     `json:"types"`
	Providers map[string]providerClass `json:"providers"`
	Languages map[string]language      `json:"languages"`
	Allowlist []item                   `json:"allowlist"`
}

type typeClass struct {
	ExecutesCode *bool    `json:"executes_code"`
	BodyArgs     []string `json:"body_args"`
	PathArgs     []string `json:"path_args"`
}

type providerClass struct {
	CommandBlocks []string `json:"command_blocks"`
}

type language struct {
	Contexts []string `json:"contexts"`
}

type item struct {
	ID         string `json:"id"`
	Owner      string `json:"owner"`
	Source     string `json:"source"`
	Language   string `json:"language"`
	ADR        string `json:"adr"`
	Repository string `json:"repository"`
}

type ownerRule struct {
	line    int
	pattern string
	owners  []string
}

func runDecl(args []string, repo string, out io.Writer) error {
	fset := flag.NewFlagSet("decl", flag.ContinueOnError)
	fset.SetOutput(out)
	declPath := fset.String("decl", "", "許可リストの宣言のパス。Conftest の --data に渡すものと同じファイル")
	rulesetPath := fset.String("ruleset", defaultRulesetPath, "保護設定の宣言のパス")
	codeownersPath := fset.String("codeowners", defaultCodeownersPath, "code owner の宣言のパス")
	repoFlag := fset.String("repo", repo, "リポジトリの root")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}
	repo = *repoFlag
	if *declPath == "" {
		return xerrors.Wrap(errUsage, "decl -decl <path>")
	}

	d, err := readDeclaration(filepath.Join(repo, *declPath))
	if err != nil {
		return err
	}
	contexts, err := readRequiredContexts(filepath.Join(repo, *rulesetPath))
	if err != nil {
		return err
	}
	rules, err := readCodeowners(filepath.Join(repo, *codeownersPath))
	if err != nil {
		return err
	}

	findings, err := checkDeclaration(repo, *declPath, d, contexts)
	if err != nil {
		return err
	}
	findings = append(findings, checkCodeowners(*codeownersPath, rules, ownedTargets(d))...)
	if err := report(out, "decl", findings); err != nil {
		return err
	}

	fmt.Fprintf(out, "✅ infra-code-gate decl: 型の分類 %d 行、許可リスト %d 項目、言語 %d 件、code owner の対象 %d 件を確認しました\n",
		len(d.Types), len(d.Allowlist), len(d.Languages), len(ownedTargets(d)))
	return nil
}

// readDeclaration は宣言を読みます。未知のキーと重複したキーはエラーにします。宣言の読み手は Rego と
// ここの2つで、どちらかが黙って読み飛ばしたキーは、もう片方の判定とずれたまま残るためです。
func readDeclaration(p string) (*declaration, error) {
	// ディレクトリを Conftest の --data に渡すと、その中のファイルがすべて深くマージされる。宣言は
	// 1つのファイルとして渡す。
	info, err := os.Stat(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	if !info.Mode().IsRegular() {
		return nil, xerrors.Wrapf(errDeclShape, "%s は通常のファイルではありません", p)
	}
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	if err := checkDuplicateKeys(raw); err != nil {
		return nil, xerrors.Wrapf(err, "%s", p)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc document
	if err := dec.Decode(&doc); err != nil {
		return nil, xerrors.Wrapf(errDeclShape, "%s: %v", p, err)
	}
	if dec.More() {
		return nil, xerrors.Wrapf(errDeclShape, "%s: 値の後ろに続きがあります", p)
	}
	if doc.Decl == nil {
		return nil, xerrors.Wrapf(errDeclShape, "%s: infra_code_decl がありません", p)
	}
	if len(doc.Decl.Types) == 0 {
		return nil, xerrors.Wrapf(errNoInput, "%s: 型の分類が1行もありません", p)
	}
	return doc.Decl, nil
}

// checkDuplicateKeys は、JSON のオブジェクトに同じキーが2回現れればエラーを返します。
// encoding/json は後に現れた値で黙って上書きします。
func checkDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return xerrors.Wrapf(errDeclShape, "%v", err)
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return xerrors.Wrapf(errDeclShape, "%v", err)
				}
				key, _ := keyTok.(string)
				// encoding/json は大文字小文字を区別せずにフィールドへ当てるので、綴りの違う同じキーも重複とする。
				if seen[strings.ToLower(key)] {
					return xerrors.Wrapf(errDuplicateKey, "%q", key)
				}
				seen[strings.ToLower(key)] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		}
		_, err = dec.Token()
		if err != nil {
			return xerrors.Wrapf(errDeclShape, "%v", err)
		}
		return nil
	}
	return walk()
}

func readRequiredContexts(p string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	var rs struct {
		Rules []struct {
			Type       string `json:"type"`
			Parameters struct {
				RequiredStatusChecks []struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
			} `json:"parameters"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, xerrors.Wrapf(err, "%s の解釈", p)
	}
	var contexts []string
	for _, r := range rs.Rules {
		if r.Type != "required_status_checks" {
			continue
		}
		for _, c := range r.Parameters.RequiredStatusChecks {
			contexts = append(contexts, c.Context)
		}
	}
	if len(contexts) == 0 {
		return nil, xerrors.Wrapf(errNoInput, "%s: required status check がありません", p)
	}
	return contexts, nil
}

// checkDeclaration は、宣言を ADR・ファイルシステム・保護設定と突き合わせます（ADR-0103 決定12-15・18）。
func checkDeclaration(repo, declPath string, d *declaration, contexts []string) ([]lintreport.Finding, error) {
	var findings []lintreport.Finding
	add := func(format string, args ...any) {
		findings = append(findings, lintreport.Finding{File: declPath, Line: 1, Message: fmt.Sprintf(format, args...)})
	}

	if !slices.Contains(contexts, gateContext) {
		add("required status check に %s がありません（ADR-0103 決定14）", gateContext)
	}

	for _, k := range sortedKeys(d.Types) {
		t := d.Types[k]
		if !typeKey.MatchString(k) {
			add("型の分類の行 %q が `<種別>.<型>` の形ではありません", k)
		}
		if t.ExecutesCode == nil {
			add("型の分類の行 %s に executes_code がありません", k)
			continue
		}
		if !*t.ExecutesCode && (len(t.BodyArgs) > 0 || len(t.PathArgs) > 0) {
			add("型の分類の行 %s はコードを実行させないのに、引数を名指しています", k)
		}
	}

	for _, name := range sortedKeys(d.Languages) {
		if len(d.Languages[name].Contexts) == 0 {
			add("言語 %s に lint と test の required context がありません（ADR-0103 決定18）", name)
		}
		for _, c := range d.Languages[name].Contexts {
			if !slices.Contains(contexts, c) {
				add("言語 %s の context %s が required status check にありません（ADR-0103 決定18）", name, c)
			}
		}
	}

	seen := map[string]bool{}
	for _, it := range d.Allowlist {
		if !itemID.MatchString(it.ID) {
			add("項目 %q の識別が `<module のディレクトリ>#<種別>.<型>.<名前>` の形ではありません", it.ID)
		}
		if seen[it.ID] {
			add("項目 %s が2回現れます", it.ID)
		}
		seen[it.ID] = true

		switch it.Owner {
		case "repository":
			if it.Repository != "" {
				add("項目 %s は本リポジトリ所有なのに repository を持っています", it.ID)
			}
			if it.Source == "" || it.Language == "" || it.ADR == "" {
				add("項目 %s は本リポジトリ所有なので、source・language・adr を要します（ADR-0103 決定12）", it.ID)
				continue
			}
			for _, msg := range checkSource(repo, it.Source) {
				add("項目 %s: %s", it.ID, msg)
			}
			if _, ok := d.Languages[it.Language]; !ok {
				add("項目 %s の言語 %s が言語の対応にありません（ADR-0103 決定18）", it.ID, it.Language)
			}
			msgs, err := checkADR(repo, it.ADR)
			if err != nil {
				return nil, err
			}
			for _, msg := range msgs {
				add("項目 %s: %s", it.ID, msg)
			}
		case "external":
			if it.Source != "" || it.Language != "" || it.ADR != "" {
				add("項目 %s は外部所有なのに source・language・adr のいずれかを持っています", it.ID)
			}
			if !repository.MatchString(it.Repository) {
				add("項目 %s は外部所有なので、ソースを持つリポジトリ（`<host>/<owner>/<repo>`）を要します（ADR-0103 決定12）", it.ID)
			}
		default:
			add("項目 %s の owner %q は repository か external のどちらかです", it.ID, it.Owner)
		}
	}
	return findings, nil
}

// checkSource は、本リポジトリ所有の項目のソースの所在を検査します（ADR-0103 決定15）。
func checkSource(repo, src string) []string {
	clean := path.Clean(src)
	if path.IsAbs(src) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != src {
		return []string{fmt.Sprintf("ソースの所在 %q は、リポジトリ内の正規化した相対パスではありません", src)}
	}
	if topLevels.MatchString(clean) {
		return []string{fmt.Sprintf("ソースの所在 %s はユースケース・module・環境の全体を指しています", src)}
	}

	full := filepath.Join(repo, filepath.FromSlash(clean))
	if _, err := os.Lstat(full); err != nil {
		return []string{fmt.Sprintf("ソースの所在 %s が実在しません", src)}
	}
	realRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return []string{fmt.Sprintf("リポジトリの root を解決できません: %v", err)}
	}
	realSrc, err := filepath.EvalSymlinks(full)
	if err != nil {
		return []string{fmt.Sprintf("ソースの所在 %s を解決できません: %v", src, err)}
	}
	if !within(realRoot, realSrc) || realSrc == realRoot {
		return []string{fmt.Sprintf("ソースの所在 %s は、解決するとリポジトリの外か root そのものを指します", src)}
	}

	info, err := os.Stat(realSrc)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		if info.Size() == 0 {
			return []string{fmt.Sprintf("ソースの所在 %s は空のファイルです", src)}
		}
		return nil
	}
	var msgs []string
	files := 0
	err = filepath.WalkDir(realSrc, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			files++
		}
		rel, _ := filepath.Rel(realSrc, p)
		rel = path.Join(clean, filepath.ToSlash(rel))
		if e.Type()&fs.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(p)
			if err != nil || !within(realSrc, target) {
				msgs = append(msgs, rel+" は所在の外を指す symlink です")
			}
			return nil
		}
		if !e.IsDir() && hasSuffix(e.Name(), configFiles) {
			msgs = append(msgs, fmt.Sprintf("所在が Terraform の構成ファイル %s を含みます。所在が module の全体を指しています", rel))
		}
		return nil
	})
	if err != nil {
		msgs = append(msgs, fmt.Sprintf("ソースの所在 %s を走査できません: %v", src, err))
	}
	if err == nil && files == 0 {
		msgs = append(msgs, fmt.Sprintf("ソースの所在 %s はコードのファイルを1つも含みません", src))
	}
	return msgs
}

func checkADR(repo, id string) ([]string, error) {
	p, err := adrdoc.Find(repo, id)
	if err != nil {
		if xerrors.Is(err, adrdoc.ErrIdentity) || xerrors.Is(err, adrdoc.ErrNotFound) || xerrors.Is(err, adrdoc.ErrAmbiguous) {
			return []string{fmt.Sprintf("ADR %s: %v（ADR-0103 決定13）", id, err)}, nil
		}
		return nil, err
	}
	root, err := os.OpenRoot(repo)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s を開く", repo)
	}
	defer func() { _ = root.Close() }()
	raw, err := root.ReadFile(p)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	var msgs []string
	if status, _ := adrdoc.Status(string(raw)); status != "Accepted" {
		msgs = append(msgs, fmt.Sprintf("ADR %s の Status が Accepted ではありません（%q）", id, status))
	}
	headings := adrdoc.Headings(raw)
	for _, h := range requiredHeadings {
		if !slices.Contains(headings, h) {
			msgs = append(msgs, fmt.Sprintf("ADR %s に見出し「%s」がありません（ADR-0103 決定13）", id, h))
		}
	}
	return msgs, nil
}

func ownedTargets(d *declaration) []string {
	targets := slices.Clone(codeownerTargets)
	for _, it := range d.Allowlist {
		if it.Owner == "repository" && it.Source != "" {
			targets = append(targets, it.Source)
		}
	}
	return targets
}

// readCodeowners は code owner の宣言を、閉じた文法で読みます。GitHub は解釈できない行を黙って無視するので、
// そのような行が1つでもあれば全体をエラーにします。
func readCodeowners(p string) ([]ownerRule, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	var rules []ownerRule
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		pattern := fields[0]
		if strings.ContainsAny(pattern, `\![]`) {
			return nil, xerrors.Wrapf(errCodeowners, "%s:%d: パターン %q は CODEOWNERS が解釈しない記法を含みます", p, i+1, pattern)
		}
		for _, o := range fields[1:] {
			if !ownerToken.MatchString(o) {
				return nil, xerrors.Wrapf(errCodeowners, "%s:%d: 所有者 %q", p, i+1, o)
			}
		}
		rules = append(rules, ownerRule{line: i + 1, pattern: pattern, owners: fields[1:]})
	}
	if len(rules) == 0 {
		return nil, xerrors.Wrapf(errNoInput, "%s: 規則が1行もありません", p)
	}
	return rules, nil
}

// checkCodeowners は、各対象について、当たる行のうち最後のものが対象そのものの字面の行であり、所有者を
// 持つことを検査します。GitHub は最後に当たった行を採るので、後ろに置いた広い行が対象の行を上書きします。
func checkCodeowners(file string, rules []ownerRule, targets []string) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, target := range targets {
		var last *ownerRule
		for i := range rules {
			if codeownersMatch(rules[i].pattern, target) {
				last = &rules[i]
			}
		}
		switch {
		case last == nil:
			findings = append(findings, lintreport.Finding{File: file, Line: 1, Message: target + " に当たる行がありません（ADR-0103 決定14）"})
		case strings.TrimPrefix(last.pattern, "/") != target:
			findings = append(findings, lintreport.Finding{File: file, Line: last.line, Message: fmt.Sprintf("%s に最後に当たる行が %s です。対象そのものの行を最後に置いてください", target, last.pattern)})
		case len(last.owners) == 0:
			findings = append(findings, lintreport.Finding{File: file, Line: last.line, Message: target + " の行に所有者がありません"})
		}
	}
	return findings
}

// codeownersMatch は、CODEOWNERS のパターンが対象に当たるかを返します。対象の末尾の `/` はディレクトリを表します。
// パターンが対象の祖先のディレクトリに当たれば、対象にも当たります。
func codeownersMatch(pattern, target string) bool {
	dirOnly := strings.HasSuffix(pattern, "/")
	p := strings.Trim(pattern, "/")
	anchored := strings.HasPrefix(pattern, "/") || strings.Contains(p, "/")
	isDir := strings.HasSuffix(target, "/")
	segs := strings.Split(strings.Trim(target, "/"), "/")

	// `dir/*` は dir の直下だけに当たり、その下の階層には当たらない（GitHub の CODEOWNERS の文書）。
	lowest := 1
	if strings.HasSuffix(p, "/*") {
		lowest = len(segs)
	}
	for n := len(segs); n >= lowest; n-- {
		candidateIsDir := n < len(segs) || isDir
		if dirOnly && !candidateIsDir {
			continue
		}
		cand := segs[:n]
		if anchored {
			if globMatch(strings.Split(p, "/"), cand) {
				return true
			}
			continue
		}
		if p == "*" || globMatch([]string{p}, cand[len(cand)-1:]) {
			return true
		}
	}
	return false
}

// globMatch は、`/` で区切ったパターンの各段を、対象の各段へ当てます。`**` は0段以上に当たります。
func globMatch(pattern, segs []string) bool {
	if len(pattern) == 0 {
		return len(segs) == 0
	}
	head, rest := pattern[0], pattern[1:]
	if head == "**" {
		for i := range len(segs) + 1 {
			if globMatch(rest, segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, err := path.Match(head, segs[0])
	return err == nil && ok && globMatch(rest, segs[1:])
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func hasSuffix(name string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
