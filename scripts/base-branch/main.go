// Package main は、フィーチャーブランチの分岐元となるリリースラインを解決するツール。
//
//	base-branch    最新のリリースラインのブランチ名を標準出力へ 1 行で出す
//
// リリースラインの接頭辞と版の形は、分岐のパターンの宣言（`.github/settings/branches.json` の
// `lines.release` と `line_version`。ADR-0603 決定1）を作業ディレクトリから読む。
//
// このリポジトリはリリースのたびにリリースラインが移る運用のため、「どこから切るか」の
// 答えが参照先ごとに食い違う。ローカルの `refs/remotes/origin/HEAD` は clone 時に一度
// 決まったきり `git fetch` では更新されず（更新には `git remote set-head` が要る）、
// GitHub のデフォルトブランチは最新のリリースラインへ移る前の値であることがある。
// どちらも警告を出さずに古い答えを返すので、判断の出所を 1 つに固定する。
//
// 出所は origin の実状態（`git ls-remote`）。ローカルの git の参照は一切読まない。上の 2 つが
// 陳腐化していても答えは変わらない。
//
// 最新の定義はバージョン番号の数値比較であって、コミット日時ではない。リリースラインの
// 新しさはバージョン番号そのものが表しており、日時は古いラインへの hotfix やベース merge で
// 前後する。ブランチを切る scripts/release も同じ宣言の読み手（scripts/lib/branchdecl）で
// 次版を決めるので、作る側と解決する側の基準が揃う。
//
// git はホストの認証情報を使うため、ツールランナーではなくホストで実行する
// （scripts/release と同じ扱い）。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// remoteName は、実状態を問い合わせるリモート。
	remoteName = "origin"
	// refPrefix は、`git ls-remote --heads` が返す参照の接頭辞。
	refPrefix = "refs/heads/"
	// commandTimeout は、git 1 コマンドあたりの上限。ネットワーク越しのため余裕を持たせる。
	commandTimeout = 60 * time.Second
)

var (
	errNoReleaseBranch = xerrors.New("❌ origin にリリースラインのブランチがありません")
	errUnexpectedArgs  = xerrors.New("❌ usage: base-branch（引数は取りません）")
)

// releaseLine は、1 本のリリースライン。
type releaseLine struct {
	name    string
	version branchdecl.Version
}

func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Getwd, lsRemoteHeads, os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

// run は、最新のリリースラインのブランチ名を out へ 1 行で書き出します。wd はリポジトリ直下（宣言を
// 読む場所）を返し、list は origin の参照一覧を返します。宣言を読めなければ、git を呼ぶ前に失敗する。
func run(args []string, wd func() (string, error), list func() (string, error), out io.Writer) error {
	fs := flag.NewFlagSet("base-branch", flag.ContinueOnError)

	if err := fs.Parse(args); err != nil {
		// usage は flag が既に出力している。
		if xerrors.Is(err, flag.ErrHelp) {
			return nil
		}

		return xerrors.Wrap(err, "failed to parse flags")
	}

	// 引数を黙って捨てると、フラグのつもりで渡された指定が無視されたまま
	// もっともらしいブランチ名が返る。
	if fs.NArg() > 0 {
		return xerrors.Wrap(errUnexpectedArgs, strings.Join(fs.Args(), " "))
	}

	root, err := wd()
	if err != nil {
		return xerrors.Wrap(err, "作業ディレクトリの取得")
	}

	decl, err := branchdecl.Read(filepath.Join(root, branchdecl.File))
	if err != nil {
		return err
	}

	refs, err := list()
	if err != nil {
		return err
	}

	latest, err := latestRelease(decl, refs)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, latest.name)

	return err
}

// lsRemoteHeads は、origin のブランチの参照一覧を取得します。リリースラインへの絞り込みは宣言で行う
// （[parseLine]）。
func lsRemoteHeads() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", remoteName)
	// git の失敗理由（認証・名前解決）はそのまま利用者へ見せる。握り潰すと
	// 「リリースラインが無い」との区別が付かなくなる。
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", xerrors.Wrap(err, "git ls-remote --heads "+remoteName)
	}

	return string(out), nil
}

// latestRelease は、`git ls-remote --heads` の出力から最新のリリースラインを選びます。
//
// 解釈できる行が 1 つも無い場合はエラーにします。取得自体は成功しうる（リモートに
// リリースラインがまだ無い、参照の書式が変わった）ため、0 件を「最新は空文字」として
// 返すと、呼び出し側は解決できなかったことに気付かないまま空のベースを使います。
func latestRelease(decl branchdecl.Branches, lsRemoteOutput string) (releaseLine, error) {
	var (
		latest releaseLine
		found  bool
	)

	for line := range strings.Lines(lsRemoteOutput) {
		parsed, ok := parseLine(decl, line)
		if !ok {
			continue
		}

		if !found || latest.version.Less(parsed.version) {
			latest, found = parsed, true
		}
	}

	if !found {
		return releaseLine{}, xerrors.Wrapf(errNoReleaseBranch, "%s%s 形式", decl.Lines.Release, decl.LineVersion)
	}

	return latest, nil
}

// parseLine は、`git ls-remote --heads` の 1 行（`<sha>\t<ref>`）をリリースラインへ変換します。
// リリースラインの名前でない行は対象外として false を返します。名前は origin が返したものをそのまま持つ。
func parseLine(decl branchdecl.Branches, line string) (releaseLine, bool) {
	_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
	if !ok {
		return releaseLine{}, false
	}

	name, ok := strings.CutPrefix(strings.TrimSpace(ref), refPrefix)
	if !ok {
		return releaseLine{}, false
	}

	role, version, ok := decl.ParseLine(name)
	if !ok || role != branchdecl.RoleRelease {
		return releaseLine{}, false
	}

	return releaseLine{name: name, version: version}, true
}
