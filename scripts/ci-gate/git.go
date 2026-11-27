package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const gitTimeout = 60 * time.Second

var (
	// errGit は、git から差分か宣言を取れないことを表す。浅い clone で merge-base が無いときもここに来る。
	errGit       = xerrors.New("git から読めません")
	errDiffCount = xerrors.New("変更したファイルの件数が食い違います")
)

// gitReader は、ci-gate が git から読むもの。show は rev の木の path の中身を、diff は `git diff` に args を
// 渡した標準出力を返す。
type gitReader struct {
	show func(rev, path string) ([]byte, error)
	diff func(args ...string) ([]byte, error)
}

// hostGit は、dir のリポジトリを実物の git で読む gitReader を返す。
func hostGit(dir string) gitReader {
	return gitReader{
		show: func(rev, path string) ([]byte, error) { return git(dir, "show", rev+":"+path) },
		diff: func(args ...string) ([]byte, error) { return git(dir, append([]string{"diff"}, args...)...) },
	}
}

// git は、dir を作業ディレクトリとして git を起動し、標準出力を返す。
func git(dir string, args ...string) ([]byte, error) {
	env, err := gitenv.Env(os.Environ())
	if err != nil {
		return nil, xerrors.Wrapf(errGit, "%v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // 引数はこの道具が組み立てる git の部分コマンド
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, xerrors.Wrapf(errGit, "git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// changedFiles は、base と head の merge-base から head までに変わったファイルを返す（Pull Request の差分と同じ基点）。
// rename は削除と追加に分ける —— 新しい名前だけを見ると、検査の入力から外へ動かした変更でその検査が走らない。
// 件数は --name-only と --numstat の2つの経路で突き合わせる。
func changedFiles(g gitReader, base, head string) ([]string, error) {
	rng := base + "..." + head
	names, err := g.diff("--no-renames", "--name-only", "-z", rng)
	if err != nil {
		return nil, err
	}
	stat, err := g.diff("--no-renames", "--numstat", "-z", rng)
	if err != nil {
		return nil, err
	}
	files := splitZ(names)
	if n := len(splitZ(stat)); n != len(files) {
		return nil, xerrors.Wrapf(errDiffCount, "--name-only %d 件 / --numstat %d 件", len(files), n)
	}
	return files, nil
}

func splitZ(b []byte) []string {
	var out []string
	for _, s := range strings.Split(string(b), "\x00") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
