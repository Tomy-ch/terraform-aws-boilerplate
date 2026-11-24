package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runTrufflehogWorktree は、commit 前の作業ツリーを trufflehog filesystem で走査します。filesystem は .gitignore を
// 読まず、名指したファイルには --exclude-paths が効かないので、走査の対象は git の一覧から selectFiles が選び、
// 作業ディレクトリの stage/ へ同じ相対パスで置いて、そこを走査します。canary は範囲の走査と同じ git の段と、
// filesystem の段で走らせます。
func runTrufflehogWorktree(args []string, stdout, log io.Writer, r runner) error {
	_, format, err := parseTrufflehogFlags("secret-scan trufflehog-worktree", args, false, log)
	if err != nil {
		return err
	}
	env, err := gitenv.Env(r.environ)
	if err != nil {
		return err
	}
	base, union, err := trufflehogExcludes(r.root)
	if err != nil {
		return err
	}
	patterns, err := parseExcludes(base)
	if err != nil {
		return err
	}
	list, err := gitOutput(r, env, r.root, stepLsFiles, "", "")
	if err != nil {
		return err
	}
	files, err := selectFiles(r.root, list, patterns)
	if err != nil {
		return err
	}
	dir, err := prepareWork(r.root, "trufflehog-worktree")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if err := trufflehogCanary(r, env, dir, stepTrufflehogRangeJSON, union, log); err != nil {
		return err
	}
	stage := filepath.Join(dir, "stage")
	if err := stageFiles(r.root, files, stage); err != nil {
		return err
	}

	return trufflehogMain(r.tool(stage, env, trufflehogSteps[format][2], ""), stdout, log)
}

// parseExcludes は、trufflehog の抑止のファイルの各行を正規表現（RE2。trufflehog と同じ）として読みます。`#` で
// 始まる行と空行だけを除き、行の前後の空白は落としません。解釈できない行があれば errExclude です。
func parseExcludes(raw string) ([]*regexp.Regexp, error) {
	var patterns []*regexp.Regexp
	for _, line := range strings.Split(raw, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			return nil, xerrors.Wrapf(errExclude, "%q: %v", line, err)
		}
		patterns = append(patterns, re)
	}

	return patterns, nil
}

func excluded(path string, patterns []*regexp.Regexp) bool {
	for _, re := range patterns {
		if re.MatchString(path) {
			return true
		}
	}

	return false
}

// selectFiles は、git ls-files -z の出力 list（root からの相対パス）から、走査するファイルを選びます。lstat で
// 無いもの（削除した追跡ファイル）と、ディレクトリ（gitlink）を飛ばし、symlink は指す先が無ければ飛ばします
// 。抑止に当たるものも外します。残りが0件なら errNoTargets です。
func selectFiles(root, list string, patterns []*regexp.Regexp) ([]string, error) {
	var files []string
	for _, p := range strings.Split(list, "\x00") {
		if p == "" {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		info, err := os.Lstat(full)
		switch {
		case xerrors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, xerrors.Wrapf(errInput, "%v", err)
		case info.IsDir():
			continue
		case info.Mode()&fs.ModeSymlink != 0:
			if _, err := os.Stat(full); err != nil {
				continue
			}
		}
		if excluded(p, patterns) {
			continue
		}
		files = append(files, p)
	}
	if len(files) == 0 {
		return nil, xerrors.Wrap(errNoTargets, "trufflehog-worktree")
	}

	return files, nil
}

// stageFiles は、root からの相対パス files を stage の下へ同じ相対パスで置きます。通常のファイルは hard link で、
// 張れなければ複写で置きます。symlink は同じ指す先の symlink として置き、辿るかどうかを trufflehog に任せます。
func stageFiles(root string, files []string, stage string) error {
	for _, p := range files {
		src := filepath.Join(root, filepath.FromSlash(p))
		dst := filepath.Join(stage, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return xerrors.Wrapf(errInput, "%v", err)
		}
		info, err := os.Lstat(src)
		if err != nil {
			return xerrors.Wrapf(errInput, "%v", err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(src)
			if err != nil {
				return xerrors.Wrapf(errInput, "%v", err)
			}
			if err := os.Symlink(target, dst); err != nil {
				return xerrors.Wrapf(errInput, "%v", err)
			}

			continue
		}
		if os.Link(src, dst) == nil {
			continue
		}
		body, err := readFile(src)
		if err != nil {
			return err
		}
		if err := writeFile(dst, body); err != nil {
			return err
		}
	}

	return nil
}
