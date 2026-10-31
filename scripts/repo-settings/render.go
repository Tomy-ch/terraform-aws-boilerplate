package main

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errInclude       = xerrors.New("conditions.ref_name.include を1つに定められません")
	errRulesetPolicy = xerrors.New("保護設定の宣言が ADR-0603 の要求を満たしません")
)

type includeSpan struct {
	start, end int
	indent     string
}

// locateInclude は src の conditions.ref_name.include の配列の位置を返します。
// どの階層でもキーの重複を拒む —— encoding/json は後の値を黙って採る。
func locateInclude(src []byte) (includeSpan, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	var spans []includeSpan
	if err := walkValue(dec, src, nil, &spans); err != nil {
		return includeSpan{}, err
	}
	if _, err := dec.Token(); !xerrors.Is(err, io.EOF) {
		return includeSpan{}, xerrors.Wrap(errInclude, "値の後に余分な内容があります")
	}
	if len(spans) != 1 {
		return includeSpan{}, xerrors.Wrapf(errInclude, "%d 件見つかりました", len(spans))
	}
	return spans[0], nil
}

// walkValue は dec の次の値を1つ読み、path が conditions.ref_name の object の include を spans へ集めます。
func walkValue(dec *json.Decoder, src []byte, path []string, spans *[]includeSpan) error {
	tok, err := dec.Token()
	if err != nil {
		return xerrors.Wrapf(errInclude, "JSON の解釈: %v", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return xerrors.Wrapf(errInclude, "JSON の解釈: %v", err)
			}
			key, _ := keyTok.(string)
			if keys[key] {
				return xerrors.Wrapf(errInclude, "キー %q が %s で重複しています", key, strings.Join(path, "."))
			}
			keys[key] = true
			if key == "include" && len(path) == 2 && path[0] == "conditions" && path[1] == "ref_name" {
				span, err := readInclude(dec, src)
				if err != nil {
					return err
				}
				*spans = append(*spans, span)
				continue
			}
			if err := walkValue(dec, src, append(append([]string{}, path...), key), spans); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := walkValue(dec, src, append(append([]string{}, path...), "[]"), spans); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return xerrors.Wrapf(errInclude, "JSON の解釈: %v", err)
	}
	return nil
}

// readInclude は include のキーの直後から配列を読み、その範囲を返します。要素は文字列に限ります。
func readInclude(dec *json.Decoder, src []byte) (includeSpan, error) {
	keyEnd := int(dec.InputOffset())
	tok, err := dec.Token()
	if err != nil {
		return includeSpan{}, xerrors.Wrapf(errInclude, "JSON の解釈: %v", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return includeSpan{}, xerrors.Wrap(errInclude, "include が配列ではありません")
	}
	start := keyEnd + bytes.IndexByte(src[keyEnd:], '[')
	for dec.More() {
		item, err := dec.Token()
		if err != nil {
			return includeSpan{}, xerrors.Wrapf(errInclude, "JSON の解釈: %v", err)
		}
		if _, ok := item.(string); !ok {
			return includeSpan{}, xerrors.Wrap(errInclude, "include の要素が文字列ではありません")
		}
	}
	if _, err := dec.Token(); err != nil {
		return includeSpan{}, xerrors.Wrapf(errInclude, "JSON の解釈: %v", err)
	}
	lineStart := bytes.LastIndexByte(src[:keyEnd], '\n') + 1
	line := src[lineStart:keyEnd]
	indent := line[:len(line)-len(bytes.TrimLeft(line, " \t"))]
	return includeSpan{start: start, end: int(dec.InputOffset()), indent: string(indent)}, nil
}

// renderInclude は src の include の区間を patterns で置き換えた内容を返します。区間の外のバイトは変えません。
func renderInclude(src []byte, patterns []string) ([]byte, error) {
	span, err := locateInclude(src)
	if err != nil {
		return nil, err
	}
	itemIndent := span.indent + "  "
	var b strings.Builder
	b.WriteString("[\n")
	for i, p := range patterns {
		b.WriteString(itemIndent)
		b.WriteString(strconv.Quote(p))
		if i < len(patterns)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(span.indent + "]")
	out := make([]byte, 0, len(src))
	out = append(out, src[:span.start]...)
	out = append(out, b.String()...)
	out = append(out, src[span.end:]...)
	return out, nil
}

// verifyInclude は、書き換えた内容をデコードし直し、include が want と等しいことを確かめます。
// 区間の位置の計算とは別の経路で、生成の結果を突き合わせる（ADR-0702 決定15）。
func verifyInclude(rendered []byte, want []string) error {
	var decl struct {
		Conditions struct {
			RefName struct {
				Include []string `json:"include"`
			} `json:"ref_name"`
		} `json:"conditions"`
	}
	if err := json.Unmarshal(rendered, &decl); err != nil {
		return xerrors.Wrapf(errInclude, "書き換えた内容の解釈: %v", err)
	}
	if !reflect.DeepEqual(decl.Conditions.RefName.Include, want) {
		return xerrors.Wrapf(errInclude, "書き換えた include が %d 件で、宣言の %d 件と一致しません", len(decl.Conditions.RefName.Include), len(want))
	}
	return nil
}

type rulesetRule struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters"`
}

// checkRuleset は保護設定の宣言が ADR-0603 決定22・23 の Static Analysis の項目を満たすかを見ます。
// enforcement・target・exclude も見る —— どれを緩めても、決定22 の bypass と同じく全ルールが外れる。
func checkRuleset(src []byte) error {
	var decl struct {
		Name        string `json:"name"`
		Target      string `json:"target"`
		Enforcement string `json:"enforcement"`
		Conditions  struct {
			RefName struct {
				Exclude []string `json:"exclude"`
			} `json:"ref_name"`
		} `json:"conditions"`
		BypassActors *[]any        `json:"bypass_actors"`
		Rules        []rulesetRule `json:"rules"`
	}
	if err := json.Unmarshal(src, &decl); err != nil {
		return xerrors.Wrapf(errRulesetPolicy, "JSON の解釈: %v", err)
	}
	var problems []string
	if decl.Name == "" {
		problems = append(problems, "name がありません")
	}
	if decl.Target != "branch" {
		problems = append(problems, `target が "branch" ではありません`)
	}
	if decl.Enforcement != "active" {
		problems = append(problems, `enforcement が "active" ではありません`)
	}
	if len(decl.Conditions.RefName.Exclude) != 0 {
		problems = append(problems, "conditions.ref_name.exclude が空ではありません")
	}
	if decl.BypassActors == nil || len(*decl.BypassActors) != 0 {
		problems = append(problems, "bypass_actors が空の配列ではありません")
	}
	rules := map[string]map[string]any{}
	for _, r := range decl.Rules {
		if _, dup := rules[r.Type]; dup {
			problems = append(problems, "ルール "+r.Type+" が2件以上あります")
		}
		rules[r.Type] = r.Parameters
	}
	if rsc, ok := rules["required_status_checks"]; ok {
		if checks, _ := rsc["required_status_checks"].([]any); len(checks) == 0 {
			problems = append(problems, "required_status_checks の context が1件もありません")
		}
	}
	for _, t := range []string{"deletion", "non_fast_forward", "pull_request", "required_status_checks"} {
		if _, ok := rules[t]; !ok {
			problems = append(problems, "ルール "+t+" がありません")
		}
	}
	if pr, ok := rules["pull_request"]; ok {
		if pr["required_review_thread_resolution"] != true {
			problems = append(problems, "pull_request の required_review_thread_resolution が true ではありません")
		}
		if pr["require_code_owner_review"] != true {
			problems = append(problems, "pull_request の require_code_owner_review が true ではありません")
		}
		if !reflect.DeepEqual(pr["allowed_merge_methods"], []any{"merge"}) {
			problems = append(problems, `pull_request の allowed_merge_methods が ["merge"] ではありません`)
		}
	}
	if len(problems) > 0 {
		return xerrors.Wrapf(errRulesetPolicy, "%s", strings.Join(problems, "; "))
	}
	return nil
}
