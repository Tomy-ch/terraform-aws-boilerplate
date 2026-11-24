// go-test-fails は、go test のログから、成功しか報告していない行を落とし、失敗の行だけを出します。
//
//	go-test-fails -log <path|-> [-strip <prefix>]
//
// カバレッジの行は3つの形で出ます。
//
//  1. `ok  <pkg> <時間>  coverage: X% ...` … 通過。行頭の ok で落とす
//  2. `coverage: X% of statements` … 失敗したパッケージの分。単独の行
//  3. `<TAB><pkg><TAB><TAB>coverage: 0.0% of statements` … テストの無いパッケージ
//
// 落ちたときに残るのは 2 と 3 なので、行頭の ok / ? だけを見ていると最大の行が素通りします。
// `gh run view --log` の行の接頭辞（job・step・時刻）は剥がします。剥がさないと行頭の一致が外れます。
// -strip に渡した接頭辞（作業ディレクトリの絶対パス）は、どの行からも消します。
//
// 失敗の行が無いときの報せは stderr へ出します —— 呼び出し側が stdout を受けて空かどうかで判定できるように。
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage = xerrors.New("使い方: go-test-fails -log <path|-> [-strip <prefix>]")
	errRead  = xerrors.New("ログを読めません")

	// ghPrefix は `gh run view --log` の行の接頭辞です。時刻の手前の [^0-9]* は BOM を吸います。
	ghPrefix = regexp.MustCompile(`^[^\t]*\t[^\t]*\t[^0-9]*[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z `)
	passed   = regexp.MustCompile(`^(ok|\?)[[:space:]]`)
	coverage = regexp.MustCompile(`coverage: [0-9.]+% of statements`)
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		log.Printf("❌ go-test-fails: %v", err)
		os.Exit(2)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fset := flag.NewFlagSet("go-test-fails", flag.ContinueOnError)
	fset.SetOutput(stderr)
	logPath := fset.String("log", "", "go test のログ（- か /dev/stdin なら標準入力）")
	strip := fset.String("strip", "", "どの行からも消す接頭辞（作業ディレクトリの絶対パスと /）")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *logPath == "" || fset.NArg() > 0 {
		return errUsage
	}
	in := stdin
	if *logPath != "-" && *logPath != "/dev/stdin" {
		f, err := os.Open(filepath.Clean(*logPath))
		if err != nil {
			return xerrors.Wrapf(errRead, "%s: %v", *logPath, err)
		}
		defer func() { _ = f.Close() }()
		in = f
	}
	lines, err := failures(in, *strip)
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", *logPath, err)
	}
	if len(lines) == 0 {
		fmt.Fprintf(stderr, "✅ 失敗行はありません（%s）\n", *logPath)

		return nil
	}
	for _, l := range lines {
		fmt.Fprintln(stdout, l)
	}

	return nil
}

// failures は、ログから成功しか報告していない行を落とした残りを返します。末尾の空の行は落とします。
func failures(in io.Reader, strip string) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := ghPrefix.ReplaceAllString(sc.Text(), "")
		if strip != "" {
			line = strings.ReplaceAll(line, strip, "")
		}
		if passed.MatchString(line) || coverage.MatchString(line) {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines, nil
}
