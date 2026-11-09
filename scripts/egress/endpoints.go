package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errNoRoots         = xerrors.New("SSOT に root が 1 件もありません")
	errRootNoJobs      = xerrors.New("SSOT の root が実行するジョブを持ちません")
	errNoTerraform     = xerrors.New("Terraform の設定ファイル（*.tf）が無いディレクトリがあります")
	errTerraformSyntax = xerrors.New("Terraform の設定を解釈できません")
	errModuleSource    = xerrors.New("module の source が相対パスのリテラルではありません")
	errNoTypes         = xerrors.New("root から resource / data source の型が 1 件も見つかりません")
	errTypeUnmapped    = xerrors.New("SSOT の type に当たらない resource / data source の型があります")
	errEndpointMissing = xerrors.New("型が呼ぶ宛先が、root を実行するジョブの許可リストにありません")
	errTypeOrphan      = xerrors.New("SSOT に、どの root の型にも当たらない type があります")
)

// typeBlocks は provider の型をラベルの先頭に持つブロック。
var typeBlocks = map[string]bool{"resource": true, "data": true, "ephemeral": true}

// checkEndpoints は各 root が module を辿って使う resource / data source の型を集め、型が呼ぶ宛先が、
// その root を実行するジョブの許可リストにあることを確かめる。欠けたときの症状は
// .github/workflows/README.md の「ランナーの封じ込め」節が持つ。
func (s *ssot) checkEndpoints(repo string) error {
	if len(s.rootOrder) == 0 {
		return errNoRoots
	}
	used := map[string]bool{}
	for _, name := range s.rootOrder {
		r := s.roots[name]
		if len(r.jobs) == 0 {
			return xerrors.Wrap(errRootNoJobs, name)
		}
		types, err := collectTypes(repo, name)
		if err != nil {
			return xerrors.Wrap(err, "root "+name)
		}
		for _, t := range types {
			prefix, ok := s.matchType(t)
			if !ok {
				return xerrors.Wrap(errTypeUnmapped, fmt.Sprintf("root %s: %s（%s へ type を追加してください）", name, t, ssotFile))
			}
			used[prefix] = true
			if err := s.requireHosts(r.jobs, s.types[prefix].hosts); err != nil {
				return xerrors.Wrap(err, fmt.Sprintf("root %s: %s", name, t))
			}
		}
	}
	var orphans []string
	for _, name := range s.typeOrder {
		if !used[name] {
			orphans = append(orphans, name)
		}
	}
	if len(orphans) > 0 {
		return xerrors.Wrap(errTypeOrphan, strings.Join(orphans, ", ")+"（"+ssotFile+" の該当エントリを削除してください）")
	}
	return nil
}

// matchType は型 t に当たる type のうち、最も長いものを返す。
func (s *ssot) matchType(t string) (string, bool) {
	best := ""
	for _, name := range s.typeOrder {
		if (name == t || strings.HasSuffix(name, "_") && strings.HasPrefix(t, name)) && len(name) > len(best) {
			best = name
		}
	}
	return best, best != ""
}

// requireHosts は hosts が jobs のどれの許可リストにもあることを見る。audit のジョブは遮断しないので見ない。
func (s *ssot) requireHosts(jobs, hosts []string) error {
	for _, key := range jobs {
		if s.jobs[key].policy == policyAudit {
			continue
		}
		allowed, err := s.hostsFor(key)
		if err != nil {
			return err
		}
		for _, h := range hosts {
			if !slices.Contains(allowed, h) {
				return xerrors.Wrap(errEndpointMissing, fmt.Sprintf("%s → %s（%s のクラスか extra へ追加してください）", h, key, ssotFile))
			}
		}
	}
	return nil
}

// collectTypes は root から相対パスの module を辿り、使われる resource / data source の型を整列して返す。
// 型が 1 件も無い root は拒む（ADR-0702 決定13）。
func collectTypes(repo, root string) ([]string, error) {
	found := map[string]bool{}
	if err := walkModule(repo, root, found, map[string]bool{}); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, errNoTypes
	}
	types := make([]string, 0, len(found))
	for t := range found {
		types = append(types, t)
	}
	slices.Sort(types)
	return types, nil
}

// walkModule は dir の *.tf を読み、型を found へ足し、呼ぶ module へ降りる。dir はリポジトリ直下からの
// スラッシュ区切りのパス。*.tf.json は読まないので、在れば拒む。
func walkModule(repo, dir string, found, visited map[string]bool) error {
	if visited[dir] {
		return nil
	}
	visited[dir] = true
	base := filepath.Join(repo, filepath.FromSlash(dir))
	jsons, err := filepath.Glob(filepath.Join(base, "*.tf.json"))
	if err != nil {
		return xerrors.Wrap(err, "glob "+dir)
	}
	if len(jsons) > 0 {
		return xerrors.Wrap(errTerraformSyntax, relTo(repo, jsons[0])+"（*.tf.json は読めません）")
	}
	files, err := filepath.Glob(filepath.Join(base, "*.tf"))
	if err != nil {
		return xerrors.Wrap(err, "glob "+dir)
	}
	if len(files) == 0 {
		return xerrors.Wrap(errNoTerraform, dir)
	}
	for _, f := range files {
		sources, err := scanTerraformFile(f, found)
		if err != nil {
			return xerrors.Wrap(err, relTo(repo, f))
		}
		for _, src := range sources {
			next := path.Join(dir, src)
			if !filepath.IsLocal(next) {
				return xerrors.Wrap(errModuleSource, relTo(repo, f)+": "+src+"（リポジトリの外を指します）")
			}
			if err := walkModule(repo, next, found, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// scanTerraformFile は 1 ファイルの resource / data / ephemeral の型を found へ足し、module の source を返す。
func scanTerraformFile(file string, found map[string]bool) ([]string, error) {
	src, err := os.ReadFile(file) //nolint:gosec // path from a glob under the declared root
	if err != nil {
		return nil, xerrors.Wrap(err, "read")
	}
	parsed, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrap(errTerraformSyntax, diags.Error())
	}
	body, ok := parsed.Body.(*hclsyntax.Body)
	if !ok {
		return nil, xerrors.Wrap(errTerraformSyntax, "本体を読めません")
	}
	var sources []string
	for _, b := range body.Blocks {
		switch {
		case typeBlocks[b.Type]:
			if len(b.Labels) != 2 {
				return nil, xerrors.Wrap(errTerraformSyntax, fmt.Sprintf("%d 行目: %s のラベルが 2 つではありません", b.TypeRange.Start.Line, b.Type))
			}
			found[b.Labels[0]] = true
		case b.Type == "module":
			source, err := moduleSource(b.Body)
			if err != nil {
				return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目: module %q", b.TypeRange.Start.Line, strings.Join(b.Labels, ".")))
			}
			sources = append(sources, source)
		}
	}
	return sources, nil
}

// moduleSource は module の source を、`./` か `../` で始まるリテラルの文字列として読む。
// registry や VCS の module は宛先を持ち込むうえ、ここからは中の型を辿れない。
func moduleSource(body *hclsyntax.Body) (string, error) {
	attr, ok := body.Attributes["source"]
	if !ok {
		return "", xerrors.Wrap(errModuleSource, "source がありません")
	}
	value, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || !value.IsKnown() || value.IsNull() || value.Type() != cty.String {
		return "", xerrors.Wrap(errModuleSource, "source がリテラルの文字列ではありません")
	}
	source := value.AsString()
	if !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../") {
		return "", xerrors.Wrap(errModuleSource, source)
	}
	return source, nil
}
