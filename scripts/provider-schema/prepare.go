package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	dirPerm      = 0o750
	lockName     = ".terraform.lock.hcl"
	rawName      = "raw.json"
	versionsName = "versions.txt"
)

// defaultRegistry は、required_providers の source で省略できるホストです。
const defaultRegistry = "registry.terraform.io"

// addressSegment は Provider のアドレスの1段です。versions.tf の文字列へ埋めるので、引用符や改行を通しません。
var addressSegment = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]*$`)

// providerAddr は `<host>/<namespace>/<type>` の形の Provider のアドレスです。
type providerAddr struct {
	full      string
	host      string
	namespace string
	name      string
}

// source は required_providers の source に書く値です。既定のレジストリならホストを省きます。
func (p providerAddr) source() string {
	if p.host == defaultRegistry {
		return p.namespace + "/" + p.name
	}

	return p.full
}

func parseProvider(addr string) (providerAddr, error) {
	parts := strings.Split(addr, "/")
	if len(parts) != 3 {
		return providerAddr{}, xerrors.Wrapf(errUsage, "-provider %q は <host>/<namespace>/<type> の形ではありません", addr)
	}
	for _, s := range parts {
		if !addressSegment.MatchString(s) {
			return providerAddr{}, xerrors.Wrapf(errUsage, "-provider %q の段 %q を使えません", addr, s)
		}
	}

	return providerAddr{full: addr, host: parts[0], namespace: parts[1], name: parts[2]}, nil
}

// lockResult は1つの lock から得たものです。found が false なら、その lock は Provider の block を持ちません。
type lockResult struct {
	found   bool
	version string
	block   []byte
}

// parseLock は lock から provider の block を探し、その version と block のバイト列を返します。
// 診断は warning を含めて1つでもあれば errLockSyntax です。
func parseLock(name string, src []byte, provider string) (lockResult, error) {
	f, diags := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
	if len(diags) > 0 {
		return lockResult{}, xerrors.Wrapf(errLockSyntax, "%s", diags.Error())
	}
	// ParseConfig の body は常に *hclsyntax.Body です。
	body := f.Body.(*hclsyntax.Body)

	var res lockResult
	for _, b := range body.Blocks {
		if b.Type != "provider" || len(b.Labels) != 1 || b.Labels[0] != provider {
			continue
		}
		if res.found {
			return lockResult{}, xerrors.Wrapf(errDuplicateBlock, "%s: %s", b.TypeRange, provider)
		}
		attr, ok := b.Body.Attributes["version"]
		if !ok {
			return lockResult{}, xerrors.Wrapf(errVersion, "%s: version の属性がありません", b.TypeRange)
		}
		tmpl, ok := attr.Expr.(*hclsyntax.TemplateExpr)
		if !ok || !tmpl.IsStringLiteral() {
			return lockResult{}, xerrors.Wrapf(errVersion, "%s: version が文字列のリテラルではありません", attr.SrcRange)
		}
		v, vd := tmpl.Value(nil)
		if vd.HasErrors() {
			return lockResult{}, xerrors.Wrapf(errVersion, "%s: %s", attr.SrcRange, vd.Error())
		}
		version := v.AsString()
		if !versionPattern.MatchString(version) {
			return lockResult{}, xerrors.Wrapf(errVersion, "%s: version %q は版の形ではありません", attr.SrcRange, version)
		}
		r := b.Range()
		res = lockResult{found: true, version: version, block: append(bytes.Clone(src[r.Start.Byte:r.End.Byte]), '\n')}
	}

	return res, nil
}

// readListed は、独立に走査した lock の一覧を1行に1件で読み、filepath.Clean で正規化して返します。
// 空行と、正規化して重なる行は errLockList です。
func readListed(p string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var listed []string
	seen := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if line == "" {
			return nil, xerrors.Wrapf(errLockList, "%s:%d: 空行があります", p, i+1)
		}
		c := filepath.Clean(line)
		if seen[c] {
			return nil, xerrors.Wrapf(errLockList, "%s:%d: %q が重複しています", p, i+1, line)
		}
		seen[c] = true
		listed = append(listed, c)
	}

	return listed, nil
}

// reconcile は、引数の lock の集合を、独立に走査した lock の集合と突き合わせます（ADR-0702 決定15）。
// どちらのパスも filepath.Clean で正規化してから比べます。
func reconcile(args, listed []string) error {
	inArgs := map[string]bool{}
	for _, p := range args {
		inArgs[filepath.Clean(p)] = true
	}
	inListed := map[string]bool{}
	for _, p := range listed {
		inListed[filepath.Clean(p)] = true
	}
	var onlyArgs, onlyListed []string
	for p := range inArgs {
		if !inListed[p] {
			onlyArgs = append(onlyArgs, p)
		}
	}
	for p := range inListed {
		if !inArgs[p] {
			onlyListed = append(onlyListed, p)
		}
	}
	if len(onlyArgs)+len(onlyListed) == 0 {
		return nil
	}
	slices.Sort(onlyArgs)
	slices.Sort(onlyListed)

	return xerrors.Wrapf(errCountMismatch, "引数にだけある: %v、独立の走査にだけある: %v", onlyArgs, onlyListed)
}

// versionsTF は、版を1つに固定した required_providers を返します。
func versionsTF(p providerAddr, version string) string {
	return fmt.Sprintf("terraform {\n\trequired_providers {\n\t\t%s = {\n\t\t\tsource  = %q\n\t\t\tversion = %q\n\t\t}\n\t}\n}\n",
		p.name, p.source(), version)
}

// needsFetch は、dir の schema を取り直す必要があるかを返します。既存の lock が lock と同じ内容で、
// raw.json が空でなければ取り直しません。
func needsFetch(dir string, lock []byte) (bool, error) {
	old, err := os.ReadFile(filepath.Clean(filepath.Join(dir, lockName)))
	switch {
	case xerrors.Is(err, fs.ErrNotExist):
		return true, nil
	case err != nil:
		return false, xerrors.Wrapf(err, "%s の読み取り", filepath.Join(dir, lockName))
	}
	if !bytes.Equal(old, lock) {
		return true, nil
	}
	info, err := os.Stat(filepath.Join(dir, rawName))
	switch {
	case xerrors.Is(err, fs.ErrNotExist):
		return true, nil
	case err != nil:
		return false, xerrors.Wrapf(err, "%s の確認", filepath.Join(dir, rawName))
	}

	return info.Size() == 0, nil
}

// prepare は、lock をすべて読み終えてから書き始めます。読めない lock が1つでもあれば何も書きません。
func prepare(args []string, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("provider-schema prepare", flag.ContinueOnError)
	fset.SetOutput(stderr)
	providerFlag := fset.String("provider", "", "版を取り出す Provider のアドレス（例: registry.terraform.io/hashicorp/aws）")
	outDir := fset.String("out", "", "版ごとのディレクトリを置くディレクトリ")
	listedPath := fset.String("listed", "", "位置引数とは独立に走査した lock のパスの一覧（1行に1件）")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *providerFlag == "" || *outDir == "" || *listedPath == "" {
		return errUsage
	}
	provider, err := parseProvider(*providerFlag)
	if err != nil {
		return err
	}
	locks := fset.Args()
	if len(locks) == 0 {
		return errNoLocks
	}
	listed, err := readListed(*listedPath)
	if err != nil {
		return err
	}
	if err := reconcile(locks, listed); err != nil {
		return err
	}

	var versions []string
	blocks := map[string][]byte{}
	absent := 0
	for _, path := range locks {
		src, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", path)
		}
		res, err := parseLock(path, src, provider.full)
		if err != nil {
			return err
		}
		if !res.found {
			absent++

			continue
		}
		if _, ok := blocks[res.version]; !ok {
			versions = append(versions, res.version)
			blocks[res.version] = res.block
		}
	}
	if len(versions) == 0 {
		return xerrors.Wrapf(errNoVersions, "%s（lock %d 件）", provider.full, len(locks))
	}

	var fetch []string
	changes := map[string]string{
		filepath.Join(*outDir, versionsName): strings.Join(versions, "\n") + "\n",
	}
	for _, v := range versions {
		dir := filepath.Join(*outDir, v)
		stale, err := needsFetch(dir, blocks[v])
		if err != nil {
			return err
		}
		if !stale {
			continue
		}
		fetch = append(fetch, dir)
		changes[filepath.Join(dir, lockName)] = string(blocks[v])
		changes[filepath.Join(dir, "versions.tf")] = versionsTF(provider, v)
	}

	for _, dir := range fetch {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return xerrors.Wrapf(err, "%s の作成", dir)
		}
		if err := os.Remove(filepath.Join(dir, rawName)); err != nil && !xerrors.Is(err, fs.ErrNotExist) {
			return xerrors.Wrapf(err, "%s の削除", filepath.Join(dir, rawName))
		}
	}
	if err := os.MkdirAll(*outDir, dirPerm); err != nil {
		return xerrors.Wrapf(err, "%s の作成", *outDir)
	}
	if err := atomicwrite.Apply(changes, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", *outDir)
	}

	for _, dir := range fetch {
		fmt.Fprintln(stdout, dir)
	}
	fmt.Fprintf(stderr, "provider-schema prepare: %s の版 %s（lock %d 件、うち Provider の無い lock %d 件）、schema の取り直し %d 版\n",
		provider.full, strings.Join(versions, " "), len(locks), absent, len(fetch))

	return nil
}
