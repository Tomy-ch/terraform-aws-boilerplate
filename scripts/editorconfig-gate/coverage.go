package main

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const missingShown = 20

// excludedNames は、本走査の対象から外れてよい追跡ファイルの名前（basename）です。editorconfig-checker が既定で
// 外すもの（mise.toml が固定した版の README の Default Excludes）のうち、このリポジトリが追跡する名前を写します。
//
// 既定の除外は先頭を固定しない末尾一致の正規表現（xgo.mod にも当たる）ですが、ここは名前の完全一致で持ちます。
// 正規表現を写せば道具の内部の2つ目の写しになり、道具と一緒に広がります。名前で持てば、当たらない名前は走査される
// べきものとして残り、道具の側の縮みは欠けとして現れます。
//
// 版を上げて既定の除外からこの名前が外れれば exclude-canary が落ち、既定の除外が増えてこのリポジトリの追跡ファイルに
// 当たれば coverage が落ちます。どちらも、新しい版の Default Excludes を読んでこの集合を直す合図です。
var excludedNames = []string{"go.mod", "go.sum", "pnpm-lock.yaml", ".terraform.lock.hcl"}

// gitlinkMode は、submodule（gitlink）の index の mode です。editorconfig-checker は submodule を走査しません。
const gitlinkMode = "160000"

// targets は、追跡ファイルの一覧から、走査の対象であるべきもの（paths）と、引いた件数を区分ごとに持ちます。
// tracked は重複を除いた追跡ファイルの件数です。
type targets struct {
	paths                                  []string
	tracked, deleted, submodules, declared int
}

// runCoverage は、本走査の対象（scanned。-dry-run の一覧）が、git の追跡ファイルから作業ツリーから消えたもの・
// submodule・excludedNames の名前を引いたものを、すべて含むことを確かめます。scanned だけに在るもの（ignore されて
// いない未追跡のファイル）は許します。引いた後が0件なら、scanned に関わらず errNoInput です。
func runCoverage(scanned []string, w io.Writer, tool toolFunc, env []string) error {
	listed, err := gitList(tool, env, stepLsFiles)
	if err != nil {
		return err
	}
	deleted, err := gitList(tool, env, stepLsDeleted)
	if err != nil {
		return err
	}
	tg, err := coverageTargets(listed, deleted)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "editorconfig coverage: 追跡ファイル %d 件から、作業ツリーから消えた %d 件・submodule %d 件・宣言した除外 %d 件を引いた %d 件を突き合わせます\n",
		tg.tracked, tg.deleted, tg.submodules, tg.declared, len(tg.paths))
	if len(tg.paths) == 0 {
		return xerrors.Wrap(errNoInput, "引いた後の追跡ファイルが0件です")
	}
	missing := missingFiles(scanned, tg.paths)
	if len(missing) > 0 {
		fmt.Fprintln(w, "❌ editorconfig coverage: 追跡ファイルが走査の対象から外れています:")
		for _, p := range missing[:min(len(missing), missingShown)] {
			fmt.Fprintln(w, displayPath(p))
		}

		return xerrors.Wrapf(errUncovered, "%d 件", len(missing))
	}
	fmt.Fprintf(w, "✅ editorconfig coverage: 追跡ファイル %d 件がすべて走査の対象です\n", len(tg.paths))

	return nil
}

// gitList は、リポジトリ直下で段 step の git を起動し、stdout を返します。0 でない終了コードは errTool です。
func gitList(tool toolFunc, env []string, step string) (string, error) {
	r, err := toolrun.Run(tool(context.Background(), ".", env, step))
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", xerrors.Wrapf(errTool, "git の %s が rc=%d で終わりました\n%s", step, r.Code, r.Combined)
	}

	return r.Stdout, nil
}

// coverageTargets は、`git ls-files -z --stage` の出力 listed から、`git ls-files -z --deleted` の出力 deleted に
// 在るもの、submodule、excludedNames の名前を、この順に1つの区分へ数えて引きます。
func coverageTargets(listed, deleted string) (targets, error) {
	entries, err := parseStage(listed)
	if err != nil {
		return targets{}, err
	}
	gone := map[string]bool{}
	for _, p := range strings.Split(deleted, "\x00") {
		if p != "" {
			gone[p] = true
		}
	}
	tg := targets{tracked: len(entries)}
	for _, e := range entries {
		switch {
		case gone[e.path]:
			tg.deleted++
		case e.mode == gitlinkMode:
			tg.submodules++
		case isExcludedName(e.path):
			tg.declared++
		default:
			tg.paths = append(tg.paths, e.path)
		}
	}

	return tg, nil
}

type stageEntry struct {
	mode, path string
}

// parseStage は、`git ls-files -z --stage` の出力（`<mode> <object> <段>\t<パス>` を NUL で区切る）を読みます。
// 衝突中の同じパスの複数の段は、最初の1つだけを返します。形に合わない項目は errRead です。
func parseStage(out string) ([]stageEntry, error) {
	var entries []stageEntry
	seen := map[string]bool{}
	for _, item := range strings.Split(out, "\x00") {
		if item == "" {
			continue
		}
		meta, p, ok := strings.Cut(item, "\t")
		fields := strings.Fields(meta)
		if !ok || p == "" || len(fields) != 3 {
			return nil, xerrors.Wrapf(errRead, "git ls-files --stage の項目を解釈できません: %q", item)
		}
		if _, err := strconv.ParseUint(fields[0], 8, 32); err != nil {
			return nil, xerrors.Wrapf(errRead, "git ls-files --stage の mode を解釈できません: %q", item)
		}
		if !seen[p] {
			seen[p] = true
			entries = append(entries, stageEntry{mode: fields[0], path: p})
		}
	}

	return entries, nil
}

func isExcludedName(p string) bool {
	base := path.Base(p)
	for _, n := range excludedNames {
		if base == n {
			return true
		}
	}

	return false
}

// dryRunPaths は、-dry-run の出力（1行1パス）から、色の制御列と先頭の `./` を外したパスを順に返します。空白だけの
// 行は外します。
func dryRunPaths(out string) []string {
	var paths []string
	for _, line := range strings.Split(stripANSI(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		paths = append(paths, strings.TrimPrefix(line, "./"))
	}

	return paths
}

// missingFiles は、want のうち scanned に無いものを want の順で返します。
func missingFiles(scanned, want []string) []string {
	seen := make(map[string]bool, len(scanned))
	for _, p := range scanned {
		seen[p] = true
	}
	var missing []string
	for _, p := range want {
		if !seen[p] {
			missing = append(missing, p)
		}
	}

	return missing
}

// displayPath は、改行か復帰を含むパスを引用して1行に収めます。
func displayPath(p string) string {
	if strings.ContainsAny(p, "\n\r") {
		return strconv.Quote(p)
	}

	return p
}
