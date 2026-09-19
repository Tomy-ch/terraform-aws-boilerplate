// Package main は、カバレッジプロファイルの総カバレッジがしきい値以上かを検査するツール。
//
//	-profile:   `go test -coverprofile` が出力したプロファイル（既定 coverage.out）
//	-module:    `go tool cover` を起動するディレクトリ（既定 scripts）
//	-threshold: 下回ってはならない総カバレッジのパーセント（既定 90）
//	-warn:      下限割れを失敗させず警告に留める
//	-github:    警告を GitHub Actions の ::warning:: アノテーションで出す
//
// このリポジトリの計測対象は運用機構（scripts/）だけであり、下限割れは失敗にする。
// **運用機構はそれ自体がゲートであるため、テストが薄いことは「検査が働いていない」ことと
// 見分けがつかない**（ADR-0702 決定16）。-warn は、計測を足した直後など下限を確定できない
// 期間のための逃げ道であって、既定の運用ではない。
//
// 総カバレッジは `go tool cover -func` の `total:` 行から取る。プロファイルの
// 集計規則（-covermode ごとの重み付けなど）を写し取らずに済ませるためで、
// ここが持つのは「取り出す」ことと「比較する」ことだけ。
//
// このツールは CI のカバレッジゲートから呼ばれる。壊れ方が「何も検査しなくなる」
// 方向に出るため、判定ロジックはシェルの中ではなくテストの当たる Go 側に置く。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// defaultModuleDir は、`go tool cover` を起動するディレクトリの既定値。
//
// **プロファイルが記録する package path は module 相対であり、`go tool cover -func` はそれを
// 解決するために go.mod を要る。** このリポジトリの module は scripts/ に根を持つので、
// リポジトリ直下から起動すると `go.mod file not found` で落ちる —— カバレッジ不足ではなく
// 道具の失敗として。既定をここへ固定し、cwd に依存させない。
const defaultModuleDir = "scripts"

// defaultThreshold は、総カバレッジの下限の既定値。
// 実際の値は make 側（COVERAGE_THRESHOLD）が -threshold で渡す。
const defaultThreshold = 90

var (
	errNoTotalLine    = xerrors.New("no total line in cover output")
	errBelowThreshold = xerrors.New("coverage below threshold")
)

func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], coverTotal); err != nil {
		log.Fatalf("%v", err)
	}
}

// run は、プロファイルの総カバレッジをしきい値に照らして報告します。
// total は総カバレッジの取得手段で、差し替えられるよう引数で受けます。
func run(args []string, total func(moduleDir, profile string) (float64, error)) error {
	fs := flag.NewFlagSet("cover-gate", flag.ContinueOnError)
	profile := fs.String("profile", "coverage.out", "検査するカバレッジプロファイル")
	moduleDir := fs.String("module", defaultModuleDir, "`go tool cover` を起動するディレクトリ（module の根）")
	threshold := fs.Int("threshold", defaultThreshold, "総カバレッジの下限（パーセント）")
	warn := fs.Bool("warn", false, "下限を割っても失敗させず警告に留める")
	github := fs.Bool("github", false, "GitHub Actions の ::warning:: アノテーションを出力する")

	if err := fs.Parse(args); err != nil {
		// usage は flag が既に出力している。
		if xerrors.Is(err, flag.ErrHelp) {
			return nil
		}

		return xerrors.Wrap(err, "failed to parse flags")
	}

	if _, err := os.Stat(*profile); err != nil {
		return xerrors.Wrap(err, "❌ "+*profile+" がありません（先に make go-test-cover を実行）")
	}

	// profile は起動時の cwd から解釈する。moduleDir へ移って起動するため、相対のまま渡すと
	// 解決の基準が黙って変わる。
	abs, err := filepath.Abs(*profile)
	if err != nil {
		return xerrors.Wrap(err, "❌ "+*profile+" の絶対パスを解決できません")
	}

	value, err := total(*moduleDir, abs)
	if err != nil {
		return xerrors.Wrap(err, "❌ 総カバレッジを取得できません")
	}

	message, ok := judge(value, *threshold)
	if ok {
		log.Print(message)

		return nil
	}

	// 下限割れを失敗にするか警告に留めるかの理由は package doc を参照。ここはフラグの指示に従う。
	if *warn {
		log.Print(annotate(message, *github))

		return nil
	}

	return xerrors.Wrap(errBelowThreshold, message)
}

// coverTotal は、`go tool cover -func` を moduleDir で起動して総カバレッジを取り出します。
func coverTotal(moduleDir, profile string) (float64, error) {
	cover := exec.CommandContext(context.Background(), "go", "tool", "cover", "-func="+profile) //nolint:gosec // 引数は profile のみで、コマンド自体は固定
	cover.Dir = moduleDir

	out, err := cover.Output()
	if err != nil {
		return 0, xerrors.Wrap(err, "go tool cover")
	}

	return parseTotal(string(out))
}

// annotate は、GitHub Actions で読ませる場合に警告アノテーションの接頭辞を付けます。
// 付けないときは元の文言をそのまま返します。
func annotate(message string, github bool) string {
	if !github {
		return message
	}

	return "::warning::" + message
}

// parseTotal は、`go tool cover -func` の出力から総カバレッジのパーセント値を取り出します。
// `total:` で始まる行の最終フィールド（`87.5%` 形式）を読み、総カバレッジ行が無い場合や
// パーセント表記が数値でない場合はエラーを返します。
func parseTotal(coverOutput string) (float64, error) {
	for line := range strings.Lines(coverOutput) {
		if !strings.HasPrefix(line, "total:") {
			continue
		}

		fields := strings.Fields(line)

		last := fields[len(fields)-1]
		if !strings.HasSuffix(last, "%") {
			return 0, xerrors.Wrap(errNoTotalLine, "total is not a percentage: "+last)
		}

		total, err := strconv.ParseFloat(strings.TrimSuffix(last, "%"), 64)
		if err != nil {
			return 0, xerrors.Wrap(err, "invalid total coverage: "+last)
		}

		return total, nil
	}

	return 0, errNoTotalLine
}

// judge は、総カバレッジがしきい値以上かを判定し、表示する報告文と合否を返します。
// しきい値ちょうどは合格です。
func judge(total float64, threshold int) (string, bool) {
	if total < float64(threshold) {
		return fmt.Sprintf("❌ 総カバレッジ %.1f%% がしきい値 %d%% を下回っています", total, threshold), false
	}

	return fmt.Sprintf("✅ 総カバレッジ %.1f%% (しきい値 %d%%)", total, threshold), true
}
