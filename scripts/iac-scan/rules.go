package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ruleMember は、semgrep-rules の tarball のうち写す member です（先頭の段は tarball の最上位のディレクトリ）。
var ruleMember = regexp.MustCompile(`^[^/]*/terraform/.*\.ya?ml$`)

const maxRuleSize = 4 << 20

// stageRules は、tarball tgz の terraform/ の rule（*.yaml・*.yml）を dst/terraform/ へ写し、写した中身が want と一致する
// ことを確かめます。tarball には rule ごとのテスト用のサンプル（攻撃コードを含む）も入っているので、当たらない member は
// ディスクへ書きません。want は、写した rule を `/` 区切りの相対パスのバイト順に並べた `sha256sum` の出力
// （`<hex>  terraform/<path>` の行）の sha256 です —— codeload の tarball はバイト列が安定しないので、中身で照合します。
// 通常のファイルでない member、`..` を含む名前、上限を超える rule、rule が0件のときは失敗です。
func stageRules(tgz, dst, want string) error {
	f, err := os.Open(filepath.Clean(tgz))
	if err != nil {
		return xerrors.Wrapf(errInput, "semgrep-rules: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return xerrors.Wrapf(errInput, "semgrep-rules: %s は gzip として読めません: %v", tgz, err)
	}
	tr := tar.NewReader(gz)
	sums := map[string]string{}
	for {
		h, err := tr.Next()
		if xerrors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return xerrors.Wrapf(errInput, "semgrep-rules: %s を読めません: %v", tgz, err)
		}
		if !ruleMember.MatchString(h.Name) {
			continue
		}
		rel, err := ruleRel(h)
		if err != nil {
			return xerrors.Wrapf(errInput, "semgrep-rules: %v", err)
		}
		sum, err := writeRule(tr, filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil {
			return xerrors.Wrapf(errInput, "semgrep-rules: %s: %v", h.Name, err)
		}
		sums[rel] = sum
	}
	if len(sums) == 0 {
		return xerrors.Wrapf(errInput, "semgrep-rules: %s に terraform/ の rule がありません", tgz)
	}
	if got := rulesDigest(sums); got != want {
		return xerrors.Wrapf(errInput, "semgrep-rules: %s の terraform/ の rule が宣言と一致しません（%s）", tgz, got)
	}

	return nil
}

// ruleRel は、member の名前から最上位のディレクトリを外した相対パスを返します。通常のファイルでない member と、
// `..` を含む名前はエラーです。
func ruleRel(h *tar.Header) (string, error) {
	if h.Typeflag != tar.TypeReg {
		return "", xerrors.Newf("%s は通常のファイルではありません（type %q）", h.Name, h.Typeflag)
	}
	_, rel, _ := strings.Cut(h.Name, "/")
	if slices.Contains(strings.Split(rel, "/"), "..") || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", xerrors.Newf("%s は写せない名前です", h.Name)
	}

	return rel, nil
}

// writeRule は、r の中身を p へ書き、その sha256 の16進を返します。maxRuleSize を超える中身はエラーです。
func writeRule(r io.Reader, p string) (sum string, err error) {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return "", err
	}
	out, err := os.OpenFile(filepath.Clean(p), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	h := sha256.New()
	n, err := io.CopyN(io.MultiWriter(out, h), r, maxRuleSize+1)
	if err != nil && !xerrors.Is(err, io.EOF) {
		return "", err
	}
	if n > maxRuleSize {
		return "", xerrors.Newf("%d バイトを超えています", maxRuleSize)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// rulesDigest は、相対パスから sha256 への対応を `sha256sum` の出力の形で並べ、その sha256 を返します。
func rulesDigest(sums map[string]string) string {
	paths := make([]string, 0, len(sums))
	for p := range sums {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s  %s\n", sums[p], p)
	}

	return hex.EncodeToString(h.Sum(nil))
}
