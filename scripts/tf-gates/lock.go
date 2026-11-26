package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tfexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const lockPerm fs.FileMode = 0o644

// lsFilesFunc は、base の git が追跡するファイルを、base からの `/` 区切りの相対パスで返します。
type lsFilesFunc func(base string) ([]string, error)

// canonical は provider の版の正本です。content は lock のバイト列です。
type canonical struct {
	decls   []tflock.Decl
	content []byte
}

// loadCanonical は、正本の宣言と lock を読み、lock が宣言と同じ provider の集合と版を持ち、mise の
// lockfile_platforms の数だけ h1: を持つことを確かめます。
func loadCanonical(providersDir, misePath string) (canonical, error) {
	decls, err := tflock.ParseDeclFile(filepath.Join(providersDir, tflock.DeclFile))
	if err != nil {
		return canonical{}, err
	}
	platforms, err := platformsOf(misePath)
	if err != nil {
		return canonical{}, err
	}
	lockPath := filepath.Join(providersDir, tflock.LockFile)
	content, err := os.ReadFile(filepath.Clean(lockPath))
	if err != nil {
		return canonical{}, xerrors.Wrapf(err, "%s（make tf-providers-lock で作る）", lockPath)
	}
	if err := checkLock(decls, content, lockPath, len(platforms)); err != nil {
		return canonical{}, err
	}

	return canonical{decls: decls, content: content}, nil
}

func platformsOf(misePath string) ([]string, error) {
	names, ok, err := misetoml.LockfilePlatformsFile(misePath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, xerrors.Wrapf(tflock.ErrInvalidPlatform, "%s に lockfile_platforms がありません", misePath)
	}

	return tflock.Platforms(names)
}

func checkLock(decls []tflock.Decl, content []byte, name string, platforms int) error {
	providers, err := tflock.ParseLock(content, name)
	if err != nil {
		return err
	}
	if problems := tflock.Check(decls, providers, platforms); len(problems) > 0 {
		return xerrors.Wrapf(errLock, "%s: %s", name, strings.Join(problems, "; "))
	}

	return nil
}

// runLockGenerate は、空の作業ディレクトリに正本の宣言どおりの versions.tf を置いて `terraform providers lock` を
// 起動し、書かれた lock を確かめてから正本へ置きます。正本のディレクトリで直接走らせないのは、providers lock が
// 既にある lock の選択を保ち、版を上げられないためです。root はリポジトリ直下です。
func runLockGenerate(args []string, w io.Writer, tool toolFunc, root string) error {
	fset := flag.NewFlagSet("tf-gates lock-generate", flag.ContinueOnError)
	fset.SetOutput(w)
	providersDir := fset.String("providers", "", "正本のディレクトリ")
	work := fset.String("work", "", "作業ディレクトリ（tmp/ の下の相対パス）")
	misePath := fset.String("mise", "", "lockfile_platforms を読む mise.toml")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *providersDir == "" || *work == "" || *misePath == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "tf-gates lock-generate は -providers・-work・-mise だけを取ります")
	}
	decls, err := tflock.ParseDeclFile(filepath.Join(*providersDir, tflock.DeclFile))
	if err != nil {
		return err
	}
	platforms, err := platformsOf(*misePath)
	if err != nil {
		return err
	}
	dir, err := workdir.Recreate(root, *work)
	if err != nil {
		return xerrors.Wrapf(errUsage, "-work %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "versions.tf"), []byte(tflock.VersionsTF(decls)), 0o600); err != nil {
		return xerrors.Wrap(err, "versions.tf の作成")
	}
	cmd := tool(context.Background(), dir, tfexec.Env(os.Environ()), stepProvidersLock, platforms...)
	cmd.Stdout, cmd.Stderr = w, w
	code, err := toolrun.Exec(cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return xerrors.Wrapf(errToolFailed, "terraform providers lock: exit status %d", code)
	}
	generated := filepath.Join(dir, tflock.LockFile)
	content, err := os.ReadFile(filepath.Clean(generated))
	if err != nil {
		return xerrors.Wrap(err, "terraform providers lock が lock を書きませんでした")
	}
	if err := checkLock(decls, content, generated, len(platforms)); err != nil {
		return err
	}
	dest := filepath.Join(*providersDir, tflock.LockFile)
	if err := atomicwrite.Apply(map[string]string{dest: string(content)}, lockPerm); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", dest)
	}
	fmt.Fprintf(w, "✅ tf-gates lock-generate: %s を書きました（provider %d 件・platform %s）\n", dest, len(decls), strings.Join(platforms, " "))

	return nil
}

func runLockApply(args []string, w io.Writer, lsFiles lsFilesFunc) error {
	return runLockSync("lock-apply", true, args, w, lsFiles)
}

func runLockCheck(args []string, w io.Writer, lsFiles lsFilesFunc) error {
	return runLockSync("lock-check", false, args, w, lsFiles)
}

// runLockSync は、走査の root の下の root（[isRoot]）の lock を正本と比べます。write なら違うものを正本で書き換え、
// そうでなければ違うものを名指して落とします。どちらも、走査の root の下で git が追跡する lock が root のものに
// 限られ、どの root の lock も追跡されていることを先に確かめます。追跡されていない写しは、CI の checkout に届きません。
func runLockSync(name string, write bool, args []string, w io.Writer, lsFiles lsFilesFunc) error {
	var providersDir, misePath string
	s, err := parseCommon(name, args, w, func(f *flag.FlagSet) {
		f.StringVar(&providersDir, "providers", "", "正本のディレクトリ")
		f.StringVar(&misePath, "mise", "", "lockfile_platforms を読む mise.toml")
	})
	if err != nil {
		return err
	}
	if providersDir == "" || misePath == "" {
		return xerrors.Wrapf(errUsage, "tf-gates %s は -providers と -mise が要ります", name)
	}
	c, err := loadCanonical(providersDir, misePath)
	if err != nil {
		return err
	}
	targets, err := requireDirs(s)
	if err != nil {
		return err
	}
	var roots []target
	for _, t := range targets {
		if isRoot(t.rel) {
			roots = append(roots, t)
		}
	}
	if len(roots) == 0 {
		return xerrors.Wrapf(errNoTargets, "lock を持つ root が0件です（走査: %s）", strings.Join(s.roots, " "))
	}
	if err := checkTracked(s, roots, lsFiles, !write); err != nil {
		return err
	}
	changes, drifted, err := diffRoots(roots, c.content)
	if err != nil {
		return err
	}
	if !write {
		if len(drifted) > 0 {
			return xerrors.Wrapf(errLock, "正本（%s）と違う root の lock: %s（make tf-lock-apply で写す）", filepath.Join(providersDir, tflock.LockFile), strings.Join(drifted, " "))
		}
		fmt.Fprintf(w, "✅ tf-gates lock-check: root %d 件の lock が正本と同じです\n", len(roots))

		return nil
	}
	if err := atomicwrite.Apply(changes, lockPerm); err != nil {
		return xerrors.Wrap(err, "root の lock の書き込み")
	}
	fmt.Fprintf(w, "✅ tf-gates lock-apply: root %d 件のうち %d 件の lock を正本で書き換えました\n", len(roots), len(changes))

	return nil
}

// diffRoots は、正本と違う（無いものを含む）root の lock の書き換えと、その root の相対パスを返します。
func diffRoots(roots []target, content []byte) (map[string]string, []string, error) {
	changes := map[string]string{}
	var drifted []string
	for _, r := range roots {
		p := filepath.Join(r.path, tflock.LockFile)
		got, err := os.ReadFile(filepath.Clean(p))
		if err != nil && !xerrors.Is(err, fs.ErrNotExist) {
			return nil, nil, xerrors.Wrapf(err, "%s", p)
		}
		if err == nil && bytes.Equal(got, content) {
			continue
		}
		changes[p] = string(content)
		drifted = append(drifted, r.rel)
	}

	return changes, drifted, nil
}

// checkTracked は、走査の root の下で git が追跡する lock のディレクトリが、すべて root であることを確かめます。
// requireRoots なら、どの root の lock も追跡されていることも確かめます（lock-apply は写した後に人が追跡するので
// 求めません）。追跡しているファイルが0件なら、base の取り違えとして落とします。
func checkTracked(s scan, roots []target, lsFiles lsFilesFunc, requireRoots bool) error {
	files, err := lsFiles(s.base)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return xerrors.Wrapf(errLock, "%s で git が追跡するファイルが0件です", s.base)
	}
	isRootDir := map[string]bool{}
	for _, r := range roots {
		isRootDir[r.rel] = true
	}
	tracked := map[string]bool{}
	var stray []string
	for _, f := range files {
		if path.Base(f) != tflock.LockFile {
			continue
		}
		dir := path.Dir(f)
		switch {
		case isRootDir[dir]:
			tracked[dir] = true
		case underRoots(dir, s.roots):
			stray = append(stray, dir)
		}
	}
	if len(stray) > 0 {
		slices.Sort(stray)

		return xerrors.Wrapf(errLock, "root でないディレクトリの lock が追跡されています: %s（root でないディレクトリの lock は追跡しない）", strings.Join(stray, " "))
	}
	if !requireRoots {
		return nil
	}
	var untracked []string
	for _, r := range roots {
		if !tracked[r.rel] {
			untracked = append(untracked, r.rel)
		}
	}
	if len(untracked) > 0 {
		return xerrors.Wrapf(errLock, "lock が追跡されていない root: %s（make tf-lock-apply の後に git add する）", strings.Join(untracked, " "))
	}

	return nil
}

func underRoots(dir string, roots []string) bool {
	for _, r := range roots {
		if dir == r || strings.HasPrefix(dir, r+"/") {
			return true
		}
	}

	return false
}

func gitLsFiles(base string) ([]string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "ls-files", "-z")
	cmd.Dir = base
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, xerrors.Wrapf(errTool, "git ls-files: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 }), nil
}
