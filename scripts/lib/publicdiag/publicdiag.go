// Package publicdiag は、CI の道具の出力から導いた値を公開の面（run のログ・job summary）へ出すときの、許可の文法の
// 正本です（ADR-0602 決定13）。公開する1行は欄の名前と値の組で、各欄の値は欄ごとの文法に当たるものだけを出し、
// 当たらない値は伏せてその件数を数えます。文法は AWS と Terraform が定める閉じた語彙か、属性の値・識別子・自由文を
// 受けない形だけから成ります。文法に当たった値にも、12桁の数字と `@` の網を掛け直します。
package publicdiag

import (
	"regexp"
	"strings"
)

// Prefix は、公開する診断の行の先頭です。
const Prefix = "診断: "

// MaskedKey は、伏せた address の key の置き換えです。
const MaskedKey = "[<伏せた key>]"

// Field は、公開する1行の欄です。
type Field struct {
	Name  string
	Value string
}

// grammars の欄の名前は、この一覧に閉じる。
var grammars = map[string]*regexp.Regexp{
	"kind": regexp.MustCompile(`^(?:iam-deny|api-error|waiter|condition|core|cli|policy|unparsed)$`),
	// AWS の service ID と operation（aws-sdk-go-v2 の `operation error <service>: <operation>`、aws CLI の operation）。
	"service":   regexp.MustCompile(`^[A-Za-z0-9]+(?: [A-Za-z0-9]+){0,5}$`),
	"operation": regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`),
	"status":    regexp.MustCompile(`^[1-5][0-9]{2}$`),
	"code":      regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.]{0,63}$`),
	"action":    regexp.MustCompile(`^[a-z0-9-]{1,32}:[A-Za-z0-9]{1,128}$`),
	// 拒否の理由の種類。implicit は allow の無いもの、explicit は明示の Deny。
	"reason": regexp.MustCompile(`^(?:implicit|explicit):(?:scp|rcp|vpc-endpoint|permissions-boundary|session|resource-based|role-trust|identity-based)$`),
	// Terraform の diagnostic の summary のうち、構成の条件と lock の固定の文だけ。provider の summary は値を含みうる。
	"summary": regexp.MustCompile(`^(?:(?:Resource|Module|Output|Data source) (?:pre|post)condition failed|Invalid value for variable|Check block assertion failed|Error acquiring the state lock)$`),
	"file":    regexp.MustCompile(`^(?:\.\./)*(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.tf$`),
	"line":    regexp.MustCompile(`^[1-9][0-9]{0,5}$`),
	"address": addressPattern,
	// Policy の規則の ID と名前空間。
	"rule":      regexp.MustCompile(`^\[PLAN-[A-Z0-9]+-[0-9]{2}\]$`),
	"namespace": regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}(?:\.[a-z][a-z0-9_]{0,63}){0,3}$`),
	"elapsed":   regexp.MustCompile(`^[0-9]{1,6}s$`),
	"count":     regexp.MustCompile(`^(?:0|[1-9][0-9]{0,5})$`),
	"masked":    regexp.MustCompile(`^[1-9][0-9]{0,5}$`),
	"exit":      regexp.MustCompile(`^[0-9]{1,3}$`),
}

// order は、Format が欄を並べる順です。Valid はこの順を要求しない。
var order = []string{"kind", "address", "file", "line", "summary", "service", "operation", "status", "code", "action", "reason", "rule", "namespace", "elapsed", "count", "exit", "masked"}

var (
	// identifierPattern は、文法に当たった後でも出さない識別子（12桁の数字のアカウント ID と、`@` を含むメール）。
	identifierPattern = regexp.MustCompile(`[0-9]{12}|@`)

	// addressPattern は、resource の address の形。key は登録簿の名前・リージョン・整数の index と、伏せた key に限る。
	// 長さは maxAddress で限る（入れ子の繰り返しの回数を正規表現に書くと、Go の regexp の上限を超える）。
	addressPattern = regexp.MustCompile(`^(?:` + addressSegment + `)(?:\.` + addressSegment + `)*$`)

	addressKeyPattern = regexp.MustCompile(`\["(?:[^"\\]|\\.)*"\]`)
	regionPattern     = regexp.MustCompile(`^[a-z]{2}(?:-gov|-iso[a-z]?)?-[a-z]+-[0-9]$`)

	// sdkPattern は、aws-sdk-go-v2 の `OperationError` の文。
	sdkPattern = regexp.MustCompile(`operation error ([A-Za-z0-9 ]{1,64}): ([A-Za-z0-9]{1,64}), (?:exceeded maximum number of attempts, [0-9]+, )?https response error StatusCode: ([0-9]{3}), RequestID: [^,]*, (?:HostID: [^,]*, )?(?:api error )?([A-Za-z][A-Za-z0-9.]{0,63}): `)
	// cliPattern は、aws CLI が API のエラーを stderr に書く文。行頭の接頭辞は CLI の版で変わるので見ない。
	cliPattern = regexp.MustCompile(`An error occurred \(([A-Za-z][A-Za-z0-9.]{0,63})\) when calling the ([A-Za-z0-9]{1,64}) operation`)
	// actionPattern と reasonPattern は、IAM の AccessDenied の文の固定の位置。
	actionPattern = regexp.MustCompile(`is not authorized to perform: ([a-z0-9-]{1,32}:[A-Za-z0-9]{1,128})`)
	reasonPattern = regexp.MustCompile(`(?:because no (` + reasonWords + `) (?:policy )?allows the |with an explicit deny in (?:a|an|the) (` + reasonWords + `)(?: policy)?(?:[:.]|$| ))`)
)

// reasonWords は、reasonTypes の key の語の選択。
const reasonWords = `permissions boundary|resource control|service control|resource-based|identity-based|VPC endpoint|role trust|session`

const addressSegment = `[A-Za-z_][A-Za-z0-9_-]*(?:\[[0-9]{1,6}\]|\["[a-z][a-z0-9]*(?:-[a-z0-9]+)*"\]|\[<伏せた key>\])?`

// maxAddress は、公開する address の長さの上限。
const maxAddress = 512

// reasonTypes は、AccessDenied の文の policy の型の語と、公開する種類。
var reasonTypes = map[string]string{
	"service control":      "scp",
	"resource control":     "rcp",
	"VPC endpoint":         "vpc-endpoint",
	"permissions boundary": "permissions-boundary",
	"session":              "session",
	"resource-based":       "resource-based",
	"role trust":           "role-trust",
	"identity-based":       "identity-based",
}

// Format は、文法に当たる欄だけを `order` の順に並べた1行と、伏せた欄の数を返します。どの欄も出せなければ空文字を返す。
// 知らない欄の名前も伏せた数に数える。
func Format(fields ...Field) (string, int) {
	values := map[string]string{}
	masked := 0

	for _, f := range fields {
		if f.Value == "" {
			continue
		}

		if _, dup := values[f.Name]; dup || !allowed(f.Name, f.Value) {
			masked++

			continue
		}

		values[f.Name] = f.Value
	}

	parts := make([]string, 0, len(values))
	for _, name := range order {
		if v, ok := values[name]; ok {
			parts = append(parts, name+"="+v)
		}
	}

	if len(parts) == 0 {
		return "", masked
	}

	return Prefix + strings.Join(parts, "; "), masked
}

// Valid は、1行が行頭から行末まで許可の文法に当たり、12桁の数字と `@` を含まないかを返します。公開の面へ出す
// 診断の行の述語で、出す側のテストはこれで出力を確かめる。
func Valid(line string) bool {
	if !strings.HasPrefix(line, Prefix) || identifierPattern.MatchString(line) || strings.ContainsAny(line, "\n\r") {
		return false
	}

	seen := map[string]bool{}

	for part := range strings.SplitSeq(strings.TrimPrefix(line, Prefix), "; ") {
		name, value, ok := strings.Cut(part, "=")
		if !ok || seen[name] || !allowed(name, value) {
			return false
		}

		seen[name] = true
	}

	return true
}

func allowed(name, value string) bool {
	g, ok := grammars[name]

	return ok && (name != "address" || len(value) <= maxAddress) && g.MatchString(value) && !identifierPattern.MatchString(value)
}

// SDK は、aws-sdk-go-v2 の error の文から、service・operation・status・code と、AccessDenied なら Action と理由の
// 種類を取ります。文の中の最初の `operation error` の一致だけを読む（AWS の文は入力値を写し戻すので、写し戻された
// 中に同じ形が現れうる）。一致が無ければ false を返す。
func SDK(text string) ([]Field, bool) {
	loc := sdkPattern.FindStringSubmatchIndex(text)
	if loc == nil {
		return nil, false
	}

	m := func(i int) string { return text[loc[2*i]:loc[2*i+1]] }
	fields := []Field{{"service", m(1)}, {"operation", m(2)}, {"status", m(3)}, {"code", m(4)}}

	kind := "api-error"
	if denied := deny(text[loc[1]:]); denied != nil {
		kind = "iam-deny"
		fields = append(fields, denied...)
	}

	return append([]Field{{"kind", kind}}, fields...), true
}

// CLI は、aws CLI の stderr から code と operation を取ります。最初の一致だけを読む。一致が無ければ false を返す。
func CLI(stderr string) ([]Field, bool) {
	loc := cliPattern.FindStringSubmatchIndex(stderr)
	if loc == nil {
		return nil, false
	}

	fields := []Field{{"kind", "cli"}, {"code", stderr[loc[2]:loc[3]]}, {"operation", stderr[loc[4]:loc[5]]}}

	return append(fields, deny(stderr[loc[1]:])...), true
}

// deny は、AccessDenied の message の固定の位置から、拒否された Action と理由の種類を取ります。
func deny(msg string) []Field {
	a := actionPattern.FindStringSubmatchIndex(msg)
	if a == nil {
		return nil
	}

	fields := []Field{{"action", msg[a[2]:a[3]]}}

	r := reasonPattern.FindStringSubmatch(msg[a[1]:])
	switch {
	case r == nil:
	case r[1] != "":
		if t, ok := reasonTypes[r[1]]; ok {
			fields = append(fields, Field{"reason", "implicit:" + t})
		}
	default:
		if t, ok := reasonTypes[r[2]]; ok {
			fields = append(fields, Field{"reason", "explicit:" + t})
		}
	}

	return fields
}

// MaskAddress は、address の key のうち、allowed が受けるもの（登録簿の env と role の名前）と、リージョンの名前の形の
// ものを残し、ほかを伏せます。key の外に識別子が残れば address ごと伏せる。整数の index は残す。
func MaskAddress(address string, allowed func(string) bool) string {
	masked := addressKeyPattern.ReplaceAllStringFunc(address, func(key string) string {
		k := key[2 : len(key)-2]
		if !identifierPattern.MatchString(k) && (allowed(k) || regionPattern.MatchString(k)) {
			return key
		}

		return MaskedKey
	})
	if identifierPattern.MatchString(masked) {
		return "<伏せた address>"
	}

	return masked
}
