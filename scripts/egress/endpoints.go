package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	errJobNoRoot       = xerrors.New("type の宛先を許すジョブが、どの root にも名指されていません")
	errSymlink         = xerrors.New("Terraform の設定に symlink があります")
	errRegionUnknown   = xerrors.New("root の aws の provider の region をリテラルとして読めません")
	errRegionMismatch  = xerrors.New("type の宛先の region が、root の aws の provider の region にありません")
	// errRegionNotOne は aws の provider の region が 2 つ以上の root のエラー。resource がどの provider を
	// 使うかを読まないので、region ごとの宛先の過不足を判定できない。
	errRegionNotOne = xerrors.New("root の aws の provider の region が 1 つではありません")
	// errBlockCount は、解析で数えたブロックと字面で数えたブロックの件数が食い違う場合のエラー（ADR-0702 決定15）。
	errBlockCount = xerrors.New("Terraform のブロックの件数が字面と解析で一致しません")
)

// typeBlocks は provider の型をラベルの先頭に持つブロック。
var typeBlocks = map[string]bool{"resource": true, "data": true, "ephemeral": true, "action": true}

// blockLineRe は型を持つブロックと module の開始行。check の中の data を数えるため字下げを許す。
var blockLineRe = regexp.MustCompile(`(?m)^[ \t]*(?:resource|data|ephemeral|action|module)[ \t]+"`)

// regionLabelRe は AWS の region の形。regionSuffixRe は宛先のラベルの末尾にある region で、
// `s3-us-west-2` のように `-` で続く形も拾う。
var (
	regionLabelRe  = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-[0-9]+$`)
	regionSuffixRe = regexp.MustCompile(`(?:^|-)([a-z]{2}(?:-[a-z]+)+-[0-9]+)$`)
)

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
		types, dirs, err := collectTypes(repo, name)
		if err != nil {
			return xerrors.Wrap(err, "root "+name)
		}
		var hosts []string
		for _, t := range types {
			prefix, ok := s.matchType(t)
			if !ok {
				return xerrors.Wrap(errTypeUnmapped, fmt.Sprintf("root %s: %s（%s へ type を追加してください）", name, t, ssotFile))
			}
			used[prefix] = true
			if err := s.requireHosts(r.jobs, s.types[prefix].hosts); err != nil {
				return xerrors.Wrap(err, fmt.Sprintf("root %s: %s", name, t))
			}
			hosts = append(hosts, s.types[prefix].hosts...)
		}
		if err := checkRegions(repo, dirs, hosts); err != nil {
			return xerrors.Wrap(err, "root "+name)
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
	return s.requireRoots()
}

// requireRoots は、いずれかの type の宛先を許すジョブが、どれかの root に名指されていることを見る。
// root の宣言だけを起点にすると、宣言し忘れた root を実行するジョブは突き合わせを受けずに通る。
func (s *ssot) requireRoots() error {
	named := map[string]bool{}
	for _, name := range s.rootOrder {
		for _, key := range s.roots[name].jobs {
			named[key] = true
		}
	}
	var typeHosts []string
	for _, name := range s.typeOrder {
		typeHosts = append(typeHosts, s.types[name].hosts...)
	}
	for _, key := range s.jobOrder {
		if named[key] {
			continue
		}
		hosts, err := s.hostsFor(key)
		if err != nil {
			return err
		}
		if i := slices.IndexFunc(typeHosts, func(h string) bool { return covers(hosts, h) }); i >= 0 {
			return xerrors.Wrap(errJobNoRoot, fmt.Sprintf("%s は %s を許します（%s の root の jobs へ追加してください）", key, typeHosts[i], ssotFile))
		}
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

// covers は許可リスト allowed が宛先 h へ通すかを、bullfrog と同じく path.Match の意味で判定する。
// `*.example.com` は apex を含まず、`*` は `.` をまたいで深いサブドメインにも当たる。
func covers(allowed []string, h string) bool {
	return slices.ContainsFunc(allowed, func(a string) bool {
		if a == h {
			return true
		}
		ok, err := path.Match(a, h)
		return err == nil && ok
	})
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
			if !covers(allowed, h) {
				return xerrors.Wrap(errEndpointMissing, fmt.Sprintf("%s → %s（%s のクラスか extra へ追加してください）", h, key, ssotFile))
			}
		}
	}
	return nil
}

// collectTypes は root から相対パスの module を辿り、使われる resource / data source の型と、読んだ
// ディレクトリを整列して返す。読み取りは repo の中に閉じる。型が 1 件も無い root は拒む（ADR-0702 決定13）。
func collectTypes(repo, root string) ([]string, []string, error) {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, nil, xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	found, visited := map[string]bool{}, map[string]bool{}
	if err := walkModule(r, root, found, visited); err != nil {
		return nil, nil, err
	}
	if len(found) == 0 {
		return nil, nil, errNoTypes
	}
	return sortedKeys(found), sortedKeys(visited), nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// checkRegions は hosts のうち region を含む宛先の region が、dirs の aws の provider のただ 1 つの region と
// 一致することを見る。region を含む宛先が無ければ provider を読まない。
func checkRegions(repo string, dirs, hosts []string) error {
	var regional []string
	for _, h := range hosts {
		if hostRegion(h) != "" {
			regional = append(regional, h)
		}
	}
	if len(regional) == 0 {
		return nil
	}
	regions, err := providerRegions(repo, dirs)
	if err != nil {
		return err
	}
	if len(regions) != 1 {
		return xerrors.Wrap(errRegionNotOne, strings.Join(regions, ", "))
	}
	for _, h := range regional {
		if hostRegion(h) != regions[0] {
			return xerrors.Wrap(errRegionMismatch, fmt.Sprintf("%s（provider: %s）", h, regions[0]))
		}
	}
	return nil
}

// hostRegion は宛先のラベルのうち region の形のものを返す。無ければ空文字列。
func hostRegion(h string) string {
	for label := range strings.SplitSeq(h, ".") {
		if m := regionSuffixRe.FindStringSubmatch(label); m != nil {
			return m[1]
		}
	}
	return ""
}

// providerRegions は dirs の *.tf にある aws の provider の region を整列して返す。region を持たない
// provider は環境変数の region で動くので、宛先と突き合わせられず拒む。
func providerRegions(repo string, dirs []string) ([]string, error) {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	found := map[string]bool{}
	for _, dir := range dirs {
		files, err := fs.Glob(r.FS(), path.Join(dir, "*.tf"))
		if err != nil {
			return nil, xerrors.Wrap(err, "glob "+dir)
		}
		for _, f := range files {
			if err := fileRegions(r, f, found); err != nil {
				return nil, xerrors.Wrap(err, f)
			}
		}
	}
	if len(found) == 0 {
		return nil, xerrors.Wrap(errRegionUnknown, "aws の provider がありません")
	}
	return sortedKeys(found), nil
}

// fileRegions は 1 ファイルの aws の provider の region を found へ足す。
func fileRegions(repo *os.Root, file string, found map[string]bool) error {
	src, err := repo.ReadFile(file)
	if err != nil {
		return xerrors.Wrap(err, "read")
	}
	parsed, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return xerrors.Wrap(errTerraformSyntax, diags.Error())
	}
	body, ok := parsed.Body.(*hclsyntax.Body)
	if !ok {
		return xerrors.Wrap(errTerraformSyntax, "本体を読めません")
	}
	for _, b := range body.Blocks {
		if b.Type != "provider" || len(b.Labels) != 1 || b.Labels[0] != "aws" {
			continue
		}
		line := fmt.Sprintf("%d 行目", b.TypeRange.Start.Line)
		attr, ok := b.Body.Attributes["region"]
		if !ok {
			return xerrors.Wrap(errRegionUnknown, line+": region がありません")
		}
		value, diags := attr.Expr.Value(nil)
		if diags.HasErrors() || !value.IsKnown() || value.IsNull() || value.Type() != cty.String || !regionLabelRe.MatchString(value.AsString()) {
			return xerrors.Wrap(errRegionUnknown, line+": region が region の形のリテラルではありません")
		}
		found[value.AsString()] = true
	}
	return nil
}

// walkModule は dir の *.tf を読み、型を found へ足し、呼ぶ module へ降りる。dir は repo 直下からの
// スラッシュ区切りのパス。*.tf.json は読まないので、在れば拒む。symlink は宣言した木の外を読ませるので拒む。
func walkModule(repo *os.Root, dir string, found, visited map[string]bool) error {
	if visited[dir] {
		return nil
	}
	visited[dir] = true
	if err := rejectSymlink(repo, dir); err != nil {
		return xerrors.Wrap(err, dir)
	}
	jsons, err := fs.Glob(repo.FS(), path.Join(dir, "*.tf.json"))
	if err != nil {
		return xerrors.Wrap(err, "glob "+dir)
	}
	if len(jsons) > 0 {
		return xerrors.Wrap(errTerraformSyntax, jsons[0]+"（*.tf.json は読めません）")
	}
	files, err := fs.Glob(repo.FS(), path.Join(dir, "*.tf"))
	if err != nil {
		return xerrors.Wrap(err, "glob "+dir)
	}
	if len(files) == 0 {
		return xerrors.Wrap(errNoTerraform, dir)
	}
	for _, f := range files {
		if err := rejectSymlink(repo, f); err != nil {
			return xerrors.Wrap(err, f)
		}
		sources, err := scanTerraformFile(repo, f, found)
		if err != nil {
			return xerrors.Wrap(err, f)
		}
		for _, src := range sources {
			next := path.Join(dir, src)
			if !filepath.IsLocal(next) {
				return xerrors.Wrap(errModuleSource, f+": "+src+"（リポジトリの外を指します）")
			}
			if err := walkModule(repo, next, found, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// rejectSymlink は repo の中の name の各要素のどれかが symlink なら拒む。os.Root は途中の要素の
// symlink を repo の中に限って辿るので、最後の要素だけを見ると宣言と違う木を読む。
// 存在しない要素は後続の glob が 0 件として扱う。
func rejectSymlink(repo *os.Root, name string) error {
	cur := ""
	for _, elem := range strings.Split(name, "/") {
		cur = path.Join(cur, elem)
		info, err := repo.Lstat(cur)
		if xerrors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return xerrors.Wrap(err, "lstat")
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return xerrors.Wrap(errSymlink, cur)
		}
	}
	return nil
}

// scanTerraformFile は 1 ファイルの resource / data / ephemeral / action の型（check の中の data を含む）を
// found へ足し、module の source を返す。解析で数えたブロックを字面の件数と突き合わせる。
func scanTerraformFile(repo *os.Root, file string, found map[string]bool) ([]string, error) {
	src, err := repo.ReadFile(file)
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
	counted := 0
	for _, b := range body.Blocks {
		switch {
		case typeBlocks[b.Type]:
			if err := addType(b, found); err != nil {
				return nil, err
			}
			counted++
		case b.Type == "check":
			for _, nested := range b.Body.Blocks {
				if nested.Type != "data" {
					continue
				}
				if err := addType(nested, found); err != nil {
					return nil, err
				}
				counted++
			}
		case b.Type == "module":
			source, err := moduleSource(b.Body)
			if err != nil {
				return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目: module %q", b.TypeRange.Start.Line, strings.Join(b.Labels, ".")))
			}
			sources = append(sources, source)
			counted++
		}
	}
	if lines := len(blockLineRe.FindAllIndex(src, -1)); lines != counted {
		return nil, xerrors.Wrap(errBlockCount, fmt.Sprintf("解析 %d 件 / 字面 %d 件", counted, lines))
	}
	return sources, nil
}

// addType は型を持つブロックのラベルの先頭を found へ足す。
func addType(b *hclsyntax.Block, found map[string]bool) error {
	if len(b.Labels) != 2 {
		return xerrors.Wrap(errTerraformSyntax, fmt.Sprintf("%d 行目: %s のラベルが 2 つではありません", b.TypeRange.Start.Line, b.Type))
	}
	found[b.Labels[0]] = true
	return nil
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
