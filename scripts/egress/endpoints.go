package main

import (
	"fmt"
	"io/fs"
	"maps"
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
	errRootNoTerraform = xerrors.New("SSOT の root が名指すジョブのどれも、その root で Terraform を実行しません")
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
	errRegionMismatch  = xerrors.New("type を置く region に、その region の宛先がありません")
	errHostUnplaced    = xerrors.New("type の宛先の region に、どの root もその type を置きません")
	errBlockRegion     = xerrors.New("ブロックの region 引数が region の形のリテラルではありません")
	// errRegionNotOne は aws の provider の region が 2 つ以上の root のエラー。resource がどの provider を
	// 使うかを読まないので、region ごとの宛先の過不足を判定できない。
	errRegionNotOne = xerrors.New("root の aws の provider の region が 1 つではありません")
	errBlockCount   = xerrors.New("Terraform のブロックの件数が字面と解析で一致しません")
)

var typeBlocks = map[string]bool{"resource": true, "data": true, "ephemeral": true, "action": true}

// blockLineRe は check の中の data を数えるため、字下げを許す。
var blockLineRe = regexp.MustCompile(`(?m)^[ \t]*(?:resource|data|ephemeral|action|module)[ \t]+"`)

// regionLabelRe は AWS の region の形。regionSuffixRe は宛先のラベルの末尾にある region で、
// `s3-us-west-2` のように `-` で続く形も拾う。
var (
	regionLabelRe  = regexp.MustCompile(`^[a-z]{2}(?:-[a-z]+)+-[0-9]+$`)
	regionSuffixRe = regexp.MustCompile(`(?:^|-)([a-z]{2}(?:-[a-z]+)+-[0-9]+)$`)
)

// globalEndpoints は、名前に region を含むが、AWS がその region に固定した global のサービスの宛先。
// provider の region に依らずこの宛先を呼ぶので、region の突き合わせから外す。
var globalEndpoints = map[string]bool{
	"organizations.us-east-1.amazonaws.com": true,
}

// typeRegions は型ごとの、その型のブロックを置く region の集合。region 引数を持たないブロックは空文字列で
// 表し、root の aws の provider の region に置かれる。
type typeRegions map[string]map[string]bool

// typeUse は root が使う 1 つの type の宛先と、その type に当たる型のブロックを置く region の集合。
type typeUse struct {
	hosts   []string
	regions map[string]bool
}

// checkEndpoints は各 root が module を辿って使う resource / data source の型を集め、その root で型が呼ぶ宛先が、
// その root で Terraform を実行するジョブ（ran が plan / apply / lock を残す入口を呼ぶと言うジョブ）の許可リストに
// あることを確かめる。呼ぶ宛先の決め方は checkRegions が持つ。lock の解除の入口だけを呼ぶジョブには型の宛先を求めない。
// 欠けたときの症状は .github/workflows/README.md の「ランナーの封じ込め」節が持つ。
func (s *ssot) checkEndpoints(repo string, ran map[string]rootRuns) error {
	if len(s.rootOrder) == 0 {
		return errNoRoots
	}
	used := map[string]bool{}
	called := map[string]bool{}
	for _, name := range s.rootOrder {
		r := s.roots[name]
		if len(r.jobs) == 0 {
			return xerrors.Wrap(errRootNoJobs, name)
		}
		runners := slices.DeleteFunc(slices.Clone(r.jobs), func(key string) bool { return !ran[key][name] })
		if len(runners) == 0 {
			return xerrors.Wrap(errRootNoTerraform, name)
		}
		types, dirs, err := collectTypes(repo, name)
		if err != nil {
			return xerrors.Wrap(err, "root "+name)
		}
		uses := map[string]typeUse{}
		for _, t := range sortedKeys(types) {
			prefix, ok := s.matchType(t)
			if !ok {
				return xerrors.Wrap(errTypeUnmapped, fmt.Sprintf("root %s: %s（%s へ type を追加してください）", name, t, ssotFile))
			}
			used[prefix] = true
			u, ok := uses[prefix]
			if !ok {
				u = typeUse{hosts: s.types[prefix].hosts, regions: map[string]bool{}}
				uses[prefix] = u
			}
			maps.Copy(u.regions, types[t])
		}
		hosts, err := checkRegions(repo, dirs, uses)
		if err != nil {
			return xerrors.Wrap(err, "root "+name)
		}
		for _, prefix := range sortedKeys(hosts) {
			if err := s.requireHosts(runners, hosts[prefix]); err != nil {
				return xerrors.Wrap(err, fmt.Sprintf("root %s: %s", name, prefix))
			}
			for _, h := range hosts[prefix] {
				called[prefix+" "+h] = true
			}
		}
	}
	// どの root も呼ばない宛先を、許可リストに残さない。
	for _, name := range s.typeOrder {
		for _, h := range s.types[name].hosts {
			if used[name] && !called[name+" "+h] {
				return xerrors.Wrap(errHostUnplaced, fmt.Sprintf("%s: %s（%s の該当の宛先を削除してください）", name, h, ssotFile))
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

// collectTypes は root から相対パスの module を辿り、使われる resource / data source の型とそれを置く region、
// 読んだディレクトリを整列して返す。読み取りは repo の中に閉じる。型が 1 件も無い root は拒む（ADR-0702 決定13）。
func collectTypes(repo, root string) (typeRegions, []string, error) {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, nil, xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	found, visited := typeRegions{}, map[string]bool{}
	if err := walkModule(r, root, found, visited); err != nil {
		return nil, nil, err
	}
	if len(found) == 0 {
		return nil, nil, errNoTypes
	}
	return found, sortedKeys(visited), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// checkRegions は root が使う type ごとに、type を置くどの region にもその region の宛先があることを確かめ、
// その root で type が呼ぶ宛先を返す。返すのは、region を持たない宛先、AWS が region を固定した global の宛先、
// type を置く region の宛先である。type を置かない region の宛先は、別の root が呼ぶので、ここでは返さない。
// region 引数を持たないブロックは dirs の aws の provider のただ 1 つの region に置かれる。provider は、その region が
// 要る type があるときだけ読む。
func checkRegions(repo string, dirs []string, uses map[string]typeUse) (map[string][]string, error) {
	provider := ""
	called := map[string][]string{}
	for _, name := range sortedKeys(uses) {
		u := uses[name]
		hosts := map[string]string{}
		for _, h := range u.hosts {
			if r := hostRegion(h); r != "" && !globalEndpoints[h] {
				hosts[h] = r
			}
		}
		if len(hosts) == 0 {
			called[name] = u.hosts
			continue
		}
		placed := map[string]bool{}
		for r := range u.regions {
			if r == "" {
				if provider == "" {
					regions, err := providerRegions(repo, dirs)
					if err != nil {
						return nil, err
					}
					if len(regions) != 1 {
						return nil, xerrors.Wrap(errRegionNotOne, strings.Join(regions, ", "))
					}
					provider = regions[0]
				}
				r = provider
			}
			placed[r] = true
		}
		declared := map[string]bool{}
		for _, h := range u.hosts {
			r, regional := hosts[h]
			if !regional || placed[r] {
				called[name] = append(called[name], h)
			}
			if regional {
				declared[r] = true
			}
		}
		for _, r := range sortedKeys(placed) {
			if !declared[r] {
				return nil, xerrors.Wrap(errRegionMismatch, fmt.Sprintf("%s: %s の宛先がありません（宛先の region: %s）", name, r, strings.Join(sortedKeys(declared), ", ")))
			}
		}
	}
	return called, nil
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
		region, ok := regionLiteral(attr)
		if !ok {
			return xerrors.Wrap(errRegionUnknown, line+": region が region の形のリテラルではありません")
		}
		found[region] = true
	}
	return nil
}

func regionLiteral(attr *hclsyntax.Attribute) (string, bool) {
	value, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || !value.IsKnown() || value.IsNull() || value.Type() != cty.String || !regionLabelRe.MatchString(value.AsString()) {
		return "", false
	}
	return value.AsString(), true
}

// walkModule は dir の *.tf を読み、型を found へ足し、呼ぶ module へ降りる。dir は repo 直下からの
// スラッシュ区切りのパス。*.tf.json は読まないので、在れば拒む。symlink は宣言した木の外を読ませるので拒む。
func walkModule(repo *os.Root, dir string, found typeRegions, visited map[string]bool) error {
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
func scanTerraformFile(repo *os.Root, file string, found typeRegions) ([]string, error) {
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

// addType は型を持つブロックのラベルの先頭と、ブロックを置く region（region 引数のリテラル。無ければ空文字列）を
// found へ足す。
func addType(b *hclsyntax.Block, found typeRegions) error {
	if len(b.Labels) != 2 {
		return xerrors.Wrap(errTerraformSyntax, fmt.Sprintf("%d 行目: %s のラベルが 2 つではありません", b.TypeRange.Start.Line, b.Type))
	}
	at := fmt.Sprintf("%d 行目: %s.%s", b.TypeRange.Start.Line, b.Labels[0], b.Labels[1])
	attr, ok, err := regionAttribute(b)
	if err != nil {
		return xerrors.Wrap(err, at)
	}
	region := ""
	if ok {
		if region, ok = regionLiteral(attr); !ok {
			return xerrors.Wrap(errBlockRegion, at)
		}
	}
	if found[b.Labels[0]] == nil {
		found[b.Labels[0]] = map[string]bool{}
	}
	found[b.Labels[0]][region] = true
	return nil
}

// regionAttribute はブロックの region 引数を返す。action は引数を config ブロックの中に持つので、config の外の
// region、2 つ以上の config、dynamic の config は、置く region を読めない形として拒む。
func regionAttribute(b *hclsyntax.Block) (*hclsyntax.Attribute, bool, error) {
	if b.Type != "action" {
		attr, ok := b.Body.Attributes["region"]
		return attr, ok, nil
	}
	if _, ok := b.Body.Attributes["region"]; ok {
		return nil, false, xerrors.Wrap(errBlockRegion, "action の region が config の外にあります")
	}
	var config *hclsyntax.Block
	for _, nested := range b.Body.Blocks {
		switch {
		case nested.Type == "dynamic" && len(nested.Labels) == 1 && nested.Labels[0] == "config":
			return nil, false, xerrors.Wrap(errBlockRegion, "action の config が dynamic です")
		case nested.Type == "config" && config != nil:
			return nil, false, xerrors.Wrap(errBlockRegion, "action の config が 2 つ以上あります")
		case nested.Type == "config":
			config = nested
		}
	}
	if config == nil {
		return nil, false, nil
	}
	attr, ok := config.Body.Attributes["region"]
	return attr, ok, nil
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
