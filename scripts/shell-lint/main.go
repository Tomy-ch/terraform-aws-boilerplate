// Package main はリポジトリ内の `*.sh` を shellcheck で検査する。
//
// composite action の中のシェルは actions-shellcheck が見るが、ファイルとして置かれたシェルは
// どのゲートにも掛かっていなかった。フックやセットアップの入口はそこに居る —— PreToolUse フック、
// SessionStart フック、スキル同梱スクリプト、コンテナの init。いずれも編集のたび、あるいは
// セッションのたびに走るのに、壊れても CI は緑を返していた。
//
// 内容をそのまま渡すので、指摘の行・列は写し戻さずに使える（起動の詳細は scripts/lib/shellcheck）。
package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/shellcheck"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const shellSuffix = ".sh"

// 走査から外すディレクトリ名。依存の取得物と VCS の内部で、いずれも我々が書いたものではない。
var skippedDirs = []string{".git", "node_modules", "vendor", "tmp"}

var (
	errFindings = xerrors.New("shellcheck が指摘を検出しました")

	// errNoTargets は、走査対象が1件も無い場合のエラー。
	//
	// **0件は成功ではない。** 対象を失った検査は、壊れたときに赤ではなく緑を返す
	// （ADR-0702 決定13）。このリポジトリに `*.sh` が1つも無い状態は、この道具が
	// 何も見ていない状態と区別できないので、成功で返さない。
	errNoTargets = xerrors.New("走査対象の *.sh が1件もありません")

	// errUnparsedFinding は、shellcheck の出力に解釈できない行があった場合のエラー。
	//
	// **解釈できない入力は、取りこぼしではなくエラーとして扱う**（ADR-0702 決定14）。
	// 黙って捨てると、出力形式が変わった日に「指摘なし」と「1行も解釈できなかった」が
	// 緑で区別できなくなる。
	errUnparsedFinding = xerrors.New("shellcheck の出力に解釈できない行があります")
)

func main() {
	log.SetFlags(0)

	if err := run(context.Background(), os.Getwd, exec.LookPath, os.Stdout); err != nil {
		log.Fatalf("❌ %v", err)
	}
}

// run はリポジトリ内のシェルスクリプトを shellcheck に掛け、結果を報告します。
// wd は走査の基点となるディレクトリの取得手段、lookPath は shellcheck の所在確認手段、
// out は報告の書き出し先です。
//
// **出力そのものが契約である**（指摘のテキストと、検査した件数）。書き出し先を引数で受けるのは、
// それをテストから読めるようにするためである。標準ロガーへ直接書くと、出力先がプロセス共通に
// なり、並列なテストが互いの出力を奪い合う。
func run(
	ctx context.Context,
	wd func() (string, error),
	lookPath func(string) (string, error),
	out io.Writer,
) error {
	root, err := shellcheck.Setup(wd, lookPath)
	if err != nil {
		return err
	}

	scripts, err := shellScripts(root)
	if err != nil {
		return err
	}

	if len(scripts) == 0 {
		return errNoTargets
	}

	var findings []string
	for _, script := range scripts {
		// script は shellScripts が root 配下を walk して得た相対パスだけを取る。
		body, err := os.ReadFile(filepath.Join(root, script)) //nolint:gosec // G304: 走査結果のみ
		if err != nil {
			return xerrors.Wrap(err, "read")
		}

		out, err := shellcheck.Run(ctx, string(body))
		if err != nil {
			return err
		}
		got, err := prefixFindings(script, out)
		if err != nil {
			return err
		}

		findings = append(findings, got...)
	}

	if len(findings) > 0 {
		fmt.Fprintln(out, strings.Join(findings, "\n"))

		return xerrors.Wrap(errFindings, fmt.Sprintf("%d 件", len(findings)))
	}

	fmt.Fprintf(out, "✅ シェルスクリプト %d ファイルを shellcheck で検査しました\n", len(scripts))

	return nil
}

// shellScripts は root 配下の `*.sh` をリポジトリ相対パスで昇順に返します。
func shellScripts(root string) ([]string, error) {
	var scripts []string

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if slices.Contains(skippedDirs, entry.Name()) {
				return fs.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(entry.Name(), shellSuffix) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return xerrors.Wrap(err, "rel")
		}
		scripts = append(scripts, filepath.ToSlash(rel))

		return nil
	})
	if err != nil {
		return nil, xerrors.Wrap(err, "walk")
	}

	slices.Sort(scripts)

	return scripts, nil
}

// prefixFindings は shellcheck の出力を 1 行 1 指摘へ整え、先頭をリポジトリ相対パスへ差し替えます。
// stdin で渡しているため shellcheck 自身は入力を `-` としか呼べず、どのファイルの指摘か言えません。
//
// 解釈できない行は errUnparsedFinding にします。空行は指摘を運ばないので読み飛ばします。
func prefixFindings(script, out string) ([]string, error) {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, nil
	}

	var findings []string
	for line := range strings.SplitSeq(trimmed, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		_, rest, found := strings.Cut(line, ":")
		if !found {
			return nil, xerrors.Wrap(errUnparsedFinding, script+": "+line)
		}

		findings = append(findings, script+":"+rest)
	}

	return findings, nil
}
