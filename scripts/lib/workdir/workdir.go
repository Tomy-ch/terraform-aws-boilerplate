// Package workdir は、検査の道具が毎回作り直す作業ディレクトリ（-work）の検証と作り直し、一時ディレクトリの
// 後片付けを持ちます。検証や削除が道具ごとに分かれて片方だけ緩むと、リポジトリ直下の tmp/ の外を消します。
package workdir

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ErrInvalid は、作業ディレクトリがリポジトリ直下の tmp/ の下の相対パスではないことです。
var ErrInvalid = xerrors.New("リポジトリ直下の tmp/ の下の相対パスではありません")

// lsFilesFunc は、root で、大文字小文字を区別せずに tmp とその下の追跡しているパスを git に問い、その出力を返します。
type lsFilesFunc func(root string) ([]byte, error)

// Resolve は、作業ディレクトリの値 work を検証し、root からのパスを返します。work はリポジトリ直下の tmp/ の下の
// 相対パスに限ります（絶対パス・`..` を含むもの・tmp/ 自身は [ErrInvalid]）。字面が tmp/ の下でも、途中の symlink が
// 外を指せば [ErrInvalid] です。
//
// root の tmp が symlink なら、その先を tmp/ として扱うのは git が追跡していない symlink に限ります。追跡している
// symlink は Pull Request が持ち込めるので、その先を消させないよう [ErrInvalid] にします。大文字小文字を区別しない
// ファイルシステムでは TMP なども tmp として開くので、追跡しているかは大文字小文字を区別せずに問い、tmp/ の下の
// 追跡しているパスがあっても落とします。追跡していないことを git で確かめられないとき（root がリポジトリでない、
// git を起動できない）も [ErrInvalid] です。tmp/ の下の追跡している symlink は、外を指せば上の symlink の検証が
// 落とします。
func Resolve(root, work string) (string, error) {
	return resolve(root, work, lsFilesTmp)
}

// Recreate は、work を [Resolve] で検証し、空のディレクトリとして作り直して root からのパスを返します。消すのと作るのは
// root の tmp を開いた [os.Root] の中で行うので、検証の後に途中を外への symlink へ差し替えられても tmp の外は消えません。
func Recreate(root, work string) (string, error) {
	return recreate(root, work, lsFilesTmp)
}

func recreate(root, work string, lsFiles lsFilesFunc) (string, error) {
	target, err := resolve(root, work, lsFiles)
	if err != nil {
		return "", err
	}
	rel := strings.TrimPrefix(path.Clean(filepath.ToSlash(work)), "tmp/")
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o750); err != nil {
		return "", xerrors.Wrap(err, "tmp")
	}
	if err := rebuild(filepath.Join(root, "tmp"), rel); err != nil {
		return "", xerrors.Wrapf(err, "%q", work)
	}

	return target, nil
}

// rebuild は、tmp を開いた [os.Root] の中で rel を消して作り直します。rel の途中の symlink が tmp の外を指せば、
// 外へは辿らずに失敗します。
func rebuild(tmp, rel string) error {
	r, err := os.OpenRoot(tmp)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	if err := r.RemoveAll(rel); err != nil {
		return xerrors.Wrap(err, "消せません")
	}
	if err := r.MkdirAll(rel, 0o750); err != nil {
		return xerrors.Wrap(err, "作れません")
	}

	return nil
}

// Temp は、parent の下に pattern の一時ディレクトリを作り、その子だけを消す後片付けとともに返します。parent が空なら
// [os.TempDir] の下です。後片付けは parent を開いた [os.Root] の中で消すので、parent の外へは出ません。
func Temp(parent, pattern string) (dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", nil, err
	}
	base := filepath.Dir(dir)
	name := filepath.Base(dir)

	return dir, func() {
		r, err := os.OpenRoot(base)
		if err != nil {
			return
		}
		_ = r.RemoveAll(name)
		_ = r.Close()
	}, nil
}

func resolve(root, work string, lsFiles lsFilesFunc) (string, error) {
	slashed := filepath.ToSlash(work)
	clean := path.Clean(slashed)
	if work == "" || filepath.IsAbs(work) || strings.HasPrefix(slashed, "/") || slices.Contains(strings.Split(slashed, "/"), "..") ||
		!strings.HasPrefix(clean, "tmp/") {
		return "", xerrors.Wrapf(ErrInvalid, "%q", work)
	}
	if err := untrackedIfSymlink(root, lsFiles); err != nil {
		return "", xerrors.Wrapf(ErrInvalid, "%q: %v", work, err)
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	if err := staysUnder(filepath.Join(root, "tmp"), target); err != nil {
		return "", xerrors.Wrapf(ErrInvalid, "%q: %v", work, err)
	}

	return target, nil
}

// untrackedIfSymlink は、root の tmp が symlink なら、git がそれ（とその下）を追跡していないことを確かめます。
// symlink でなければ git を起動しません。
func untrackedIfSymlink(root string, lsFiles lsFilesFunc) error {
	info, err := os.Lstat(filepath.Join(root, "tmp"))
	if xerrors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	out, err := lsFiles(root)
	if err != nil {
		return xerrors.Newf("tmp は symlink で、git が追跡していないことを確かめられません: %v", err)
	}
	if len(out) > 0 {
		return xerrors.New("tmp は git が追跡している symlink です")
	}

	return nil
}

func lsFilesTmp(root string) ([]byte, error) {
	env, err := gitenv.Env(os.Environ())
	if err != nil {
		return nil, err
	}
	// GIT_LITERAL_PATHSPECS が立つと :(icase) を字面として読み、何も返さずに通してしまう。
	env = append(env, "GIT_LITERAL_PATHSPECS=0")
	// ls-files は hook を起動しないが、hook の設定を読む道が増えても起動しないよう外しておく。
	cmd := exec.CommandContext(context.Background(), "git", "-c", "core.hooksPath=/dev/null", "ls-files", "-z", "--", ":(icase)tmp")
	cmd.Dir, cmd.Env = root, env
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if xerrors.As(err, &exitErr) {
		return nil, xerrors.Newf("%v: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}

	return out, err
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
