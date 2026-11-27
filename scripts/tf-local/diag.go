package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/policyresult"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/publicdiag"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// conditionSummary は、address を持たない診断のうち、構成の条件が落ちたことを述べる summary。
	conditionSummary = regexp.MustCompile(`^(?:Resource|Module|Output|Data source) (?:pre|post)condition failed$|^Invalid value for variable$|^Check block assertion failed$`)
	// waiterSummary は、provider が待ちの打ち切りを述べる文の断片。
	waiterSummary = regexp.MustCompile(`timeout while waiting|unexpected state`)
	// ruleIDPattern は、Policy の違反の文の先頭の規則の ID。
	ruleIDPattern = regexp.MustCompile(`^\[PLAN-[A-Z0-9]+-[0-9]{2}\]`)

	errChangeSummary = xerrors.New("❌ terraform の change_summary を1つに読めません")
)

// changeSummaryFields は、Terraform 1.17 の change_summary が持つ欄。知らない欄が在れば読めないとする（版を上げたときに
// 気づくため）。
var changeSummaryFields = []string{"add", "change", "import", "remove", "action_invocation", "action_fail", "operation"}

// streamRange は、diagnostic の range のうち読む欄だけです。
type streamRange struct {
	Filename string    `json:"filename"`
	Start    streamPos `json:"start"`
}

// streamPos は、range の位置のうち行だけです。
type streamPos struct {
	Line int `json:"line"`
}

// streamDiagnostic は、`-json` の diagnostic のうち読む欄だけです。snippet は属性の値を持つので型に持たない。
type streamDiagnostic struct {
	Severity string       `json:"severity"`
	Summary  string       `json:"summary"`
	Detail   string       `json:"detail"`
	Address  string       `json:"address"`
	Range    *streamRange `json:"range"`
}

// streamLine は、`-json` の1行のうち読む欄だけです。
type streamLine struct {
	Level      string            `json:"@level"`
	Type       string            `json:"type"`
	Diagnostic *streamDiagnostic `json:"diagnostic"`
	Changes    map[string]any    `json:"changes"`
}

// streamDiag は、stream から導いた公開の値です。
type streamDiag struct {
	// Lines は公開する診断の行。error の診断1つに1行。
	Lines []string
	// Masked は、伏せた欄と、JSON として読めない行の数。
	Masked int
	// Errors は、stream の `"@level":"error"` の行を、JSON を読む経路と独立に数えた数。
	Errors int
	// Unparsed は、JSON として読めない行の数。UnparsedErrors はそのうち error の行の数。
	Unparsed       int
	UnparsedErrors int
	Summaries      []map[string]any
}

// reconciled は、error の行の数が、公開した診断の行と、読めなかった error の行の和に一致するかを返します
// （ADR-0702 決定15）。
func (d streamDiag) reconciled() bool {
	return d.Errors == len(d.Lines)+d.UnparsedErrors
}

// readStream は、terraform の `-json` の stream から、error の診断の公開の行と change_summary を導きます。読めない行は
// 落とさず、伏せた数に数える。error を返さない（診断の抽出は失敗の種類を変えない）。
func readStream(data []byte) streamDiag {
	var d streamDiag

	for raw := range bytes.SplitSeq(data, []byte("\n")) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}

		isError := bytes.Contains(raw, []byte(`"@level":"error"`))
		if isError {
			d.Errors++
		}

		var l streamLine
		if err := json.Unmarshal(raw, &l); err != nil || l.Type == "" {
			d.Unparsed++
			d.Masked++

			if isError {
				d.UnparsedErrors++
			}

			continue
		}

		switch {
		case l.Type == "change_summary":
			d.Summaries = append(d.Summaries, l.Changes)
		case l.Type == "diagnostic" && l.Diagnostic != nil && l.Diagnostic.Severity == "error":
			line, masked := diagnosticLine(*l.Diagnostic)
			d.Lines = append(d.Lines, line)
			d.Masked += masked
		}
	}

	return d
}

// diagnosticLine は、error の診断1つを公開の1行にします。address を持つ診断（provider の診断）は、summary と detail の
// 固定の位置から AWS の error を取る。address を持たない診断は detail を読まず、summary の AWS の error か分類と、
// ファイルと行を出す。
func diagnosticLine(diag streamDiagnostic) (string, int) {
	var fields []publicdiag.Field

	if diag.Address != "" {
		extracted, ok := publicdiag.SDK(diag.Summary)
		if !ok {
			extracted, ok = publicdiag.SDK(diag.Detail)
		}

		switch {
		case ok:
			fields = extracted
		case waiterSummary.MatchString(diag.Summary):
			fields = []publicdiag.Field{{Name: "kind", Value: "waiter"}}
		default:
			fields = []publicdiag.Field{{Name: "kind", Value: "unparsed"}}
		}

		fields = append(fields, publicdiag.Field{Name: "address", Value: publicdiag.MaskAddress(diag.Address, addressKey)})
	} else {
		kind := "core"
		if conditionSummary.MatchString(diag.Summary) {
			kind = "condition"
		}

		fields = []publicdiag.Field{{Name: "kind", Value: kind}, {Name: "summary", Value: diag.Summary}}
		// backend の init の失敗のように、address を持たない診断の summary にも AWS の error が入る。detail は読まない。
		if extracted, ok := publicdiag.SDK(diag.Summary); ok {
			fields = extracted
		}
		if diag.Range != nil && diag.Range.Start.Line > 0 {
			fields = append(fields,
				publicdiag.Field{Name: "file", Value: diag.Range.Filename},
				publicdiag.Field{Name: "line", Value: strconv.Itoa(diag.Range.Start.Line)})
		}
	}

	return publicdiag.Format(fields...)
}

// addressKey は、address の key のうち公開してよい名前（CI の role の登録簿の名前）を受けます。
func addressKey(key string) bool {
	return slices.Contains(cientry.Roles(), key)
}

// changeSummary は、stream の change_summary をちょうど1つ読み、import・add・change・remove の件数を返します。
// 0件か2件以上か、4つの欄の欠けか、知らない欄か、数でない値があれば失敗します（欠けを0件と読まない）。
func changeSummary(d streamDiag) ([4]int, error) {
	if len(d.Summaries) != 1 {
		return [4]int{}, xerrors.Wrap(errChangeSummary, fmt.Sprintf("%d 件", len(d.Summaries)))
	}

	var counts [4]int

	for _, k := range []string{"import", "add", "change", "remove"} {
		if _, ok := d.Summaries[0][k]; !ok {
			return [4]int{}, xerrors.Wrap(errChangeSummary, "欠けた欄")
		}
	}

	for k, v := range d.Summaries[0] {
		if !slices.Contains(changeSummaryFields, k) {
			return [4]int{}, xerrors.Wrap(errChangeSummary, "知らない欄")
		}

		i := slices.Index([]string{"import", "add", "change", "remove"}, k)
		if i < 0 {
			continue
		}

		n, ok := v.(float64)
		if !ok || n < 0 || n != float64(int(n)) {
			return [4]int{}, xerrors.Wrap(errChangeSummary, "数でない値")
		}

		counts[i] = int(n)
	}

	return counts, nil
}

// diagReport は、公開する診断の節の行です。終了コードが 0 でないのに診断が0件なら、そのことを明示する。件数が合わ
// なければそのことを1行で述べる（判定にはしない）。
func diagReport(d streamDiag, exitCode int) []string {
	lines := slices.Clone(d.Lines)

	if exitCode != 0 && len(d.Lines) == 0 {
		line, _ := publicdiag.Format(publicdiag.Field{Name: "count", Value: "0"}, publicdiag.Field{Name: "exit", Value: strconv.Itoa(exitCode)})
		lines = append(lines, line)
	}

	if d.Masked > 0 {
		line, _ := publicdiag.Format(publicdiag.Field{Name: "masked", Value: strconv.Itoa(d.Masked)})
		lines = append(lines, line)
	}

	if !d.reconciled() {
		lines = append(lines, "⚠️ 診断の件数が合いません（error の行 "+strconv.Itoa(d.Errors)+"、診断 "+strconv.Itoa(len(d.Lines))+
			"、読めない error の行 "+strconv.Itoa(d.UnparsedErrors)+"）")
	}

	return lines
}

// policyLines は、Policy の違反を、名前空間ごと・規則の ID ごとの件数の行にします。違反の文は plan の値を含むので
// 公開せず、全文へ置く（ADR-0602 決定13）。ID の無い文は、規則の ID の欄を伏せて数える。
func policyLines(entries []policyresult.Entry) []string {
	type key struct{ ns, rule string }

	counts := map[key]int{}

	var keys []key

	for _, e := range entries {
		for _, f := range e.Failures {
			k := key{e.Namespace, ruleIDPattern.FindString(f.Msg)}
			if counts[k] == 0 {
				keys = append(keys, k)
			}

			counts[k]++
		}
	}

	lines := make([]string, 0, len(keys))

	for _, k := range keys {
		line, _ := publicdiag.Format(
			publicdiag.Field{Name: "kind", Value: "policy"},
			publicdiag.Field{Name: "namespace", Value: k.ns},
			publicdiag.Field{Name: "rule", Value: k.rule},
			publicdiag.Field{Name: "count", Value: strconv.Itoa(counts[k])},
		)
		lines = append(lines, line)
	}

	return lines
}

// policyMessages は、Policy の違反の文を、全文へ置く形にします。
func policyMessages(entries []policyresult.Entry) string {
	var b strings.Builder

	for _, e := range entries {
		for _, f := range e.Failures {
			b.WriteString(e.Namespace + ": " + f.Msg + "\n")
		}
	}

	return b.String()
}

// ciRunTool は、CI の段で aws を起動する runTool です。stderr は公開のログへそのまま流さず、失敗したときだけ、aws CLI
// の error の文の許可の文法に当たる欄を stderr へ出す（ADR-0602 決定13）。
func ciRunTool(tool toolFunc, dir string, env []string, step string, stderr io.Writer) ([]byte, error) {
	var buf bytes.Buffer

	out, err := runTool(tool, dir, env, step, &buf)
	if err == nil {
		return out, nil
	}

	if line := cliLine(buf.String()); line != "" {
		if _, werr := io.WriteString(stderr, line+"\n"); werr != nil {
			return out, errors.Join(err, werr)
		}
	}

	return out, err
}

// cliLine は、aws CLI の stderr を公開の1行にします。空なら空文字を返す。error の文が読めなければ、伏せたことだけを
// 出す。
func cliLine(stderr string) string {
	if strings.TrimSpace(stderr) == "" {
		return ""
	}

	fields, ok := publicdiag.CLI(stderr)
	if !ok {
		fields = []publicdiag.Field{{Name: "kind", Value: "cli"}, {Name: "masked", Value: "1"}}
	}

	line, _ := publicdiag.Format(fields...)

	return line
}

// diagSection は、job summary の診断の節です。行が無ければ空文字を返す。
func diagSection(lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	return "<pre>\n" + html.EscapeString(strings.Join(lines, "\n")) + "\n</pre>\n\n"
}

func writeLines(w io.Writer, lines []string) error {
	for _, l := range lines {
		if _, err := io.WriteString(w, l+"\n"); err != nil {
			return err
		}
	}

	return nil
}

// appendPolicyOut は、Policy の段の、ログへ出さない文を全文の末尾に足します。
func appendPolicyOut(s ciState, text string) error {
	if text == "" {
		return nil
	}

	f, err := appendOut(s)
	if err != nil {
		return err
	}

	_, werr := io.WriteString(f, text)

	return errors.Join(werr, f.Close())
}
