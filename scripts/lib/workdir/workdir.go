// Package workdir は、検査の道具が毎回 RemoveAll で作り直す作業ディレクトリ（-work）の検証を持ちます。検証が
// 道具ごとに分かれて片方だけ緩むと、リポジトリ直下の tmp/ の外を消します。
package workdir

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ErrInvalid は、作業ディレクトリがリポジトリ直下の tmp/ の下の相対パスではないことです。
var ErrInvalid = xerrors.New("リポジトリ直下の tmp/ の下の相対パスではありません")

// Resolve は、作業ディレクトリの値 work を検証し、root からのパスを返します。work はリポジトリ直下の tmp/ の下の
// 相対パスに限ります（絶対パス・`..` を含むもの・tmp/ 自身は [ErrInvalid]）。字面が tmp/ の下でも、途中の symlink が
// 外を指せば [ErrInvalid] です。
func Resolve(root, work string) (string, error) {
	slashed := filepath.ToSlash(work)
	clean := path.Clean(slashed)
	if work == "" || filepath.IsAbs(work) || strings.HasPrefix(slashed, "/") || slices.Contains(strings.Split(slashed, "/"), "..") ||
		!strings.HasPrefix(clean, "tmp/") {
		return "", xerrors.Wrapf(ErrInvalid, "%q", work)
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	if err := staysUnder(filepath.Join(root, "tmp"), target); err != nil {
		return "", xerrors.Wrapf(ErrInvalid, "%q: %v", work, err)
	}

	return target, nil
}

// staysUnder は、target の在る祖先のうち最も深いものの symlink を解いた先が、base の symlink を解いた先の下に
// あることを確かめます。base が無ければ、その下に symlink も無いので確かめません。
func staysUnder(base, target string) error {
	base, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(base); xerrors.Is(err, fs.ErrNotExist) {
		return nil
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return err
	}
	existing := target
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !xerrors.Is(err, fs.ErrNotExist) {
			return err
		}
		existing = filepath.Dir(existing)
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(baseReal, resolved)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || (rel == "." && existing == target) {
		return xerrors.Newf("%s は %s の下にありません", resolved, baseReal)
	}

	return nil
}
