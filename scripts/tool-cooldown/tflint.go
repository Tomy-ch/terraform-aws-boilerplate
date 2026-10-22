package main

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// tflintFile は TFLint の plugin の版を宣言するファイル。窓を掛ける理由は scripts/README.md の
// tool-cooldown の行が持つ。
const tflintFile = ".tflint.hcl"

var (
	// ErrInvalidTflint は .tflint.hcl に解釈できない記述があった場合のエラー。
	ErrInvalidTflint = xerrors.New(".tflint.hcl に解釈できない記述があります")
	// ErrUntrustedPlugin は TFLint の公式の org 以外から plugin を取得しようとした場合のエラー。
	// plugin は手元の資格情報が届く環境で実行されるので、窓を満たしても取得元を広げない。
	ErrUntrustedPlugin = xerrors.New("TFLint の plugin は github.com/terraform-linters/ からだけ取得します")

	tflintPluginOpen = regexp.MustCompile(`^plugin\s+"([^"]+)"\s*\{\s*(#.*)?$`)
	tflintPluginHead = regexp.MustCompile(`^plugin\b`)
	tflintAttribute  = regexp.MustCompile(`^([a-z_]+)\s*=\s*("([^"]*)"|true|false)\s*(#.*)?$`)
	tflintGitHubSrc  = regexp.MustCompile(`^github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)$`)
	tflintTrustedOrg = "terraform-linters"
)

// parseTflintPlugins は .tflint.hcl の `plugin` block が固定する版を読む。
//
// 読めない記述は読み飛ばさずエラーにする（ADR-0702 決定14）。読み飛ばすと、その plugin は窓を
// 掛けられないまま `tflint --init` に取得される。どの block が対象かは tflintPluginTool が決める。
func parseTflintPlugins(content []byte) ([]tool, error) {
	var (
		tools   []tool
		inBlock bool
		name    string
		attrs   map[string]string
	)

	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		// 複数行のコメントは中に plugin を隠せる。行単位では区別できないので、`/*` を含む行は読まない。
		if strings.Contains(line, "/*") {
			return nil, xerrors.Wrap(ErrInvalidTflint, fmt.Sprintf("%d 行目: `/*` を含む行は読めません（複数行のコメントと区別できないため）", lineNo))
		}

		if !inBlock {
			m := tflintPluginOpen.FindStringSubmatch(line)
			if m == nil {
				if tflintPluginHead.MatchString(line) {
					return nil, xerrors.Wrap(ErrInvalidTflint, fmt.Sprintf("%d 行目（plugin の開始）: %s", lineNo, line))
				}
				// plugin 以外の block（config、rule）は版を持たないので読まない。
				continue
			}
			inBlock, name, attrs = true, m[1], map[string]string{}

			continue
		}

		if line == "}" {
			t, ok, err := tflintPluginTool(name, attrs)
			if err != nil {
				return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目で閉じる plugin %q", lineNo, name))
			}
			if ok {
				tools = append(tools, t)
			}
			inBlock = false

			continue
		}

		m := tflintAttribute.FindStringSubmatch(line)
		if m == nil {
			return nil, xerrors.Wrap(ErrInvalidTflint, fmt.Sprintf("%d 行目（plugin %q の中）: %s", lineNo, name, line))
		}
		if _, dup := attrs[m[1]]; dup {
			return nil, xerrors.Wrap(ErrInvalidTflint, fmt.Sprintf("%d 行目（plugin %q の中）: %s が重複しています", lineNo, name, m[1]))
		}
		attrs[m[1]] = m[3]
	}
	if err := scanner.Err(); err != nil {
		return nil, xerrors.Wrap(err, "読み取り")
	}
	if inBlock {
		return nil, xerrors.Wrap(ErrInvalidTflint, fmt.Sprintf("plugin %q が閉じていません", name))
	}

	return tools, nil
}

// tflintPluginTool は plugin block の属性を検査の対象へ変換する。第2戻り値が false のものは、
// `source` を持たない、取得を伴わない TFLint 同梱の ruleset である。`source` を持ちながら
// `version` が無い・空の block は版を固定していないので、`source` を持たずに `version` だけが
// ある block は記述の誤りなので、いずれもエラーにする。
func tflintPluginTool(name string, attrs map[string]string) (tool, bool, error) {
	source, hasSource := attrs["source"]
	version, hasVersion := attrs["version"]
	if !hasSource {
		if hasVersion {
			return tool{}, false, xerrors.Wrap(ErrInvalidTflint, "version があるのに source がありません")
		}

		return tool{}, false, nil
	}
	if !hasVersion || version == "" {
		return tool{}, false, xerrors.Wrap(ErrInvalidTflint, "source があるのに version で版を固定していません")
	}
	m := tflintGitHubSrc.FindStringSubmatch(source)
	if m == nil {
		return tool{}, false, xerrors.Wrap(errUnsupportedBackend, fmt.Sprintf("plugin %q の source %q", name, source))
	}
	if m[1] != tflintTrustedOrg {
		return tool{}, false, xerrors.Wrap(ErrUntrustedPlugin, fmt.Sprintf("plugin %q の source %q", name, source))
	}

	return tool{key: "github:" + m[1] + "/" + m[2], version: version}, true, nil
}
