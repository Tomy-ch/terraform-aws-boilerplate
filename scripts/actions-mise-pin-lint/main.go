// actions-mise-pin-lint は、CI が mise 本体を導入する composite action の3点が一致していることを
// 検査します。
//
//	MISE_VERSION   入れる版
//	MISE_SHA256    その版のバイナリの digest
//	cache の key   復元したバイナリをどの版・どの digest のものとして扱うか
//
// 解いている問題:
//
// **キャッシュから復元しただけのバイナリを信用しない。** 版だけを上げて digest を据え置くと、
// キャッシュは古いバイナリを新しい版として返し続けます。キャッシュキーが版と digest の両方を
// 含んでいれば、どちらが動いてもキーが変わり、復元は外れます。
//
// 3点のどれかが欠けた状態は、検証があるように見えて効いていない状態です。読み取れない時点で落とします。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const digestPrefixLength = 8

const defaultActionPath = ".github/actions/setup-mise/action.yaml"

var errUnreadable = xerrors.New("検査対象の action を読み取れません")

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errUnreadable) {
			log.Printf("❌ actions-mise-pin-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ actions-mise-pin-lint: %v", err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("actions-mise-pin-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("action", defaultActionPath, "検査する composite action のパス")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	raw, err := os.ReadFile(filepath.Clean(*path))
	if err != nil {
		return xerrors.Wrapf(errUnreadable, "%s", *path)
	}

	pin := readPin(string(raw))
	violations := findViolations(pin)

	if len(violations) > 0 {
		findings := make([]lintreport.Finding, 0, len(violations))
		for _, v := range violations {
			findings = append(findings, lintreport.Finding{File: *path, Line: 1, Message: v})
		}
		fmt.Fprintf(out, "❌ actions-mise-pin-lint: %d 件の不一致\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の不一致", len(findings))
	}

	fmt.Fprintf(out, "✅ actions-mise-pin-lint: 版 %s / digest %s… / キャッシュキーの三点が一致しています\n",
		pin.version, pin.digest[:digestPrefixLength])
	return nil
}

type misePin struct {
	version  string
	digest   string
	cacheKey string
}

func readYAMLValue(source, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(source, "\n") {
		v := strings.TrimSpace(line)
		if !strings.HasPrefix(v, prefix) {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(v, prefix))
	}
	return ""
}

func readPin(source string) misePin {
	return misePin{
		version:  readYAMLValue(source, "MISE_VERSION"),
		digest:   readYAMLValue(source, "MISE_SHA256"),
		cacheKey: readYAMLValue(source, "key"),
	}
}

func findViolations(pin misePin) []string {
	var violations []string

	if pin.version == "" {
		violations = append(violations, "MISE_VERSION を読み取れません")
	}
	if pin.digest == "" {
		violations = append(violations, "MISE_SHA256 を読み取れません")
	}
	if pin.cacheKey == "" {
		violations = append(violations, "キャッシュの key を読み取れません")
	}
	if len(violations) > 0 {
		return violations
	}

	if !strings.Contains(pin.cacheKey, pin.version) {
		violations = append(violations,
			fmt.Sprintf("キャッシュキーが版を含んでいません（版 %s / キー %s）", pin.version, pin.cacheKey))
	}

	// 短い digest を素通りさせない。キャッシュキーへ埋める先頭桁を取れない値は、
	// 「キーが digest を含んでいる」ことを確かめようがないため、値そのものを違反とする。
	if len(pin.digest) < digestPrefixLength {
		violations = append(violations,
			fmt.Sprintf("MISE_SHA256 が短すぎます（%d 桁。先頭 %d 桁をキャッシュキーへ埋める必要があります）",
				len(pin.digest), digestPrefixLength))
		return violations
	}

	prefix := pin.digest[:digestPrefixLength]
	if !strings.Contains(pin.cacheKey, prefix) {
		violations = append(violations,
			fmt.Sprintf("キャッシュキーが digest の先頭 %d 桁を含んでいません（%s / キー %s）",
				digestPrefixLength, prefix, pin.cacheKey))
	}

	return violations
}
