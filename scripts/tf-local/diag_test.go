package main

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/policyresult"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/publicdiag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// denyDetail は、合成の AccessDenied の文。アカウント ID は 111111111111 だけを使う。
const denyDetail = `creating IAM Role (tabp-admin-x): operation error IAM: CreateRole, https response error StatusCode: 403, ` +
	`RequestID: 0f0f, api error AccessDenied: User: arn:aws:sts::111111111111:assumed-role/tabp-admin-ci-apply-identity/gha ` +
	`is not authorized to perform: iam:CreateRole on resource: arn:aws:iam::111111111111:role/x because no identity-based policy allows the iam:CreateRole action`

const versionLine = `{"@level":"info","@message":"Terraform 1.17.2","type":"version"}`

func diagLine(t *testing.T, d streamDiagnostic, snippet string) string {
	t.Helper()

	diag := map[string]any{"severity": d.Severity, "summary": d.Summary, "detail": d.Detail, "address": d.Address}
	diag["range"] = map[string]any{"filename": "main.tf", "start": map[string]any{"line": 3}}
	diag["snippet"] = map[string]any{"values": []any{map[string]any{"traversal": "var.x", "statement": snippet}}}

	raw, err := json.Marshal(map[string]any{"@level": "error", "@message": "Error: " + d.Summary, "type": "diagnostic", "diagnostic": diag})
	require.NoError(t, err)

	return string(raw)
}

func Test_readStream(t *testing.T) {
	t.Parallel()

	t.Run("provider の診断から AWS の error と address を出し、識別子を出さない", func(t *testing.T) {
		t.Parallel()

		stream := versionLine + "\n" + diagLine(t, streamDiagnostic{
			Severity: "error", Summary: "creating IAM Role (tabp-admin-x): operation error IAM", Detail: denyDetail,
			Address: `module.org.aws_iam_role.this["ops@example.com"]`,
		}, "111111111111") + "\n"

		d := readStream([]byte(stream))
		require.Len(t, d.Lines, 1)
		assert.Equal(t, "診断: kind=iam-deny; address=module.org.aws_iam_role.this[<伏せた key>]; service=IAM; operation=CreateRole; "+
			"status=403; code=AccessDenied; action=iam:CreateRole; reason=implicit:identity-based", d.Lines[0])
		assert.Equal(t, 1, d.Errors)
		assert.True(t, d.reconciled())
		assert.NotContains(t, strings.Join(d.Lines, "\n"), "111111111111")
	})

	t.Run("読めない行を落とさず、伏せた数と読めない error の行に数える", func(t *testing.T) {
		t.Parallel()

		d := readStream([]byte("not json\n{\"@level\":\"error\" broken\n" + versionLine + "\n"))
		assert.Empty(t, d.Lines)
		assert.Equal(t, 2, d.Unparsed)
		assert.Equal(t, 1, d.UnparsedErrors)
		assert.Equal(t, 2, d.Masked)
		assert.Equal(t, 1, d.Errors)
		assert.True(t, d.reconciled())
	})

	t.Run("type を欠く JSON の error の行は、読めない error の行に数える", func(t *testing.T) {
		t.Parallel()

		d := readStream([]byte(`{"@level":"error","@message":"x"}`))
		assert.Empty(t, d.Lines)
		assert.Equal(t, 1, d.UnparsedErrors)
		assert.True(t, d.reconciled())
	})

	t.Run("level が error でも diagnostic を欠く行は、件数が合わないと報告する", func(t *testing.T) {
		t.Parallel()

		d := readStream([]byte(`{"@level":"error","type":"diagnostic"}`))
		assert.False(t, d.reconciled())
		assert.Contains(t, strings.Join(diagReport(d, 1), "\n"), "診断の件数が合いません")
	})

	t.Run("空の stream は何も導かない", func(t *testing.T) {
		t.Parallel()

		d := readStream(nil)
		assert.Equal(t, streamDiag{}, d)
	})

	t.Run("warning の診断は行にしない", func(t *testing.T) {
		t.Parallel()

		d := readStream([]byte(`{"@level":"warn","type":"diagnostic","diagnostic":{"severity":"warning","summary":"Deprecated"}}`))
		assert.Empty(t, d.Lines)
		assert.Zero(t, d.Errors)
	})

	t.Run("change_summary の欄を集める", func(t *testing.T) {
		t.Parallel()

		d := readStream([]byte(`{"@level":"info","type":"change_summary","changes":{"add":1,"change":0,"import":0,"remove":2,"operation":"plan"}}`))
		require.Len(t, d.Summaries, 1)
		assert.InDelta(t, 2, d.Summaries[0]["remove"], 0)
	})

	t.Run("敵対的な stream でも、出す行はすべて文法に当たり識別子を含まない", func(t *testing.T) {
		t.Parallel()

		parts := []string{
			"111111111111", "ops@example.com", "arn:aws:iam::111111111111:role/x", "o-abc123defg", "\n", "\"", "; ", "=",
			"operation error IAM: CreateRole, ", "https response error StatusCode: 403, RequestID: x, ", "api error AccessDenied: ",
			"is not authorized to perform: iam:CreateRole", " because no identity-based policy allows the ", "Resource postcondition failed",
			`["Security"]`, `module.a`, "[0]", "日本語",
		}
		r := rand.New(rand.NewPCG(485, 3))
		pick := func() string {
			var b strings.Builder
			for range r.IntN(8) {
				b.WriteString(parts[r.IntN(len(parts))])
			}

			return b.String()
		}

		var stream strings.Builder

		for range 2000 {
			stream.WriteString(diagLine(t, streamDiagnostic{Severity: "error", Summary: pick(), Detail: pick(), Address: pick()}, pick()) + "\n")
		}

		d := readStream([]byte(stream.String()))
		require.Len(t, d.Lines, 2000)
		assert.True(t, d.reconciled())

		for _, line := range diagReport(d, 1) {
			if strings.HasPrefix(line, "⚠️") {
				continue
			}

			require.True(t, publicdiag.Valid(line), line)
			assert.NotContains(t, line, "111111111111")
		}
	})
}

func Test_diagnosticLine(t *testing.T) {
	t.Parallel()

	t.Run("address の無い条件の失敗は、detail を読まずに分類とファイルと行を出す", func(t *testing.T) {
		t.Parallel()

		line, masked := diagnosticLine(streamDiagnostic{
			Severity: "error", Summary: "Resource postcondition failed", Detail: "value 111111111111",
			Range: &streamRange{Filename: "../../usecases/state-backend/main.tf", Start: streamPos{Line: 42}},
		})
		assert.Equal(t, "診断: kind=condition; file=../../usecases/state-backend/main.tf; line=42; summary=Resource postcondition failed", line)
		assert.Zero(t, masked)
	})

	t.Run("core の error で summary が自由文なら summary を伏せる", func(t *testing.T) {
		t.Parallel()

		line, masked := diagnosticLine(streamDiagnostic{Severity: "error", Summary: `Reference to undeclared resource "x"`})
		assert.Equal(t, "診断: kind=core", line)
		assert.Equal(t, 1, masked)
	})

	t.Run("待ちの打ち切りを分類する", func(t *testing.T) {
		t.Parallel()

		line, _ := diagnosticLine(streamDiagnostic{Severity: "error", Summary: "waiting for X: timeout while waiting for state", Address: "aws_x.this"})
		assert.Equal(t, "診断: kind=waiter; address=aws_x.this", line)
	})

	t.Run("AWS の error が読めない provider の診断は unparsed にする", func(t *testing.T) {
		t.Parallel()

		line, _ := diagnosticLine(streamDiagnostic{Severity: "error", Summary: "something", Address: "aws_x.this"})
		assert.Equal(t, "診断: kind=unparsed; address=aws_x.this", line)
	})

	t.Run("summary に AWS の error があれば detail を読まない", func(t *testing.T) {
		t.Parallel()

		line, _ := diagnosticLine(streamDiagnostic{Severity: "error", Address: "aws_iam_role.this",
			Summary: "x: operation error IAM: GetRole, https response error StatusCode: 404, RequestID: a, api error NoSuchEntity: y", Detail: denyDetail})
		assert.Contains(t, line, "code=NoSuchEntity")
		assert.NotContains(t, line, "AccessDenied")
	})

	t.Run("line が 0 の range は file と line を出さない", func(t *testing.T) {
		t.Parallel()

		line, _ := diagnosticLine(streamDiagnostic{Severity: "error", Summary: "Invalid value for variable", Range: &streamRange{Filename: "main.tf"}})
		assert.Equal(t, "診断: kind=condition; summary=Invalid value for variable", line)
	})

	t.Run("summary に AWS の error が無ければ detail から読む", func(t *testing.T) {
		t.Parallel()

		line, _ := diagnosticLine(streamDiagnostic{Severity: "error", Summary: "creating IAM Role", Detail: denyDetail, Address: "aws_iam_role.this"})
		assert.Contains(t, line, "code=AccessDenied")
	})
}

func Test_addressKey(t *testing.T) {
	t.Parallel()

	t.Run("CI の role の名前は受ける", func(t *testing.T) {
		t.Parallel()

		assert.True(t, addressKey("apply-identity"))
	})

	t.Run("ほかの名前は受けない", func(t *testing.T) {
		t.Parallel()

		assert.False(t, addressKey("security"))
	})
}

func Test_changeSummary(t *testing.T) {
	t.Parallel()

	t.Run("import・add・change・remove の件数を読む", func(t *testing.T) {
		t.Parallel()

		got, err := changeSummary(streamDiag{Summaries: []map[string]any{{"add": 2.0, "change": 1.0, "import": 3.0, "remove": 4.0, "action_invocation": 0.0, "action_fail": 0.0, "operation": "plan"}}})
		require.NoError(t, err)
		assert.Equal(t, [4]int{3, 2, 1, 4}, got)
	})

	t.Run("0件なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := changeSummary(streamDiag{})
		require.ErrorIs(t, err, errChangeSummary)
	})

	t.Run("2件なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := changeSummary(streamDiag{Summaries: append(summaryOf(0, 0, 0, 0).Summaries, summaryOf(0, 0, 0, 0).Summaries...)})
		require.ErrorIs(t, err, errChangeSummary)
	})

	t.Run("知らない欄があれば失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := changeSummary(streamDiag{Summaries: []map[string]any{{"add": 0.0, "change": 0.0, "import": 0.0, "remove": 0.0, "forget": 1.0}}})
		require.ErrorIs(t, err, errChangeSummary)
	})

	t.Run("4つの欄のどれかが欠ければ、0件と読まずに失敗する", func(t *testing.T) {
		t.Parallel()

		for _, m := range []map[string]any{nil, {}, {"add": 0.0, "change": 0.0, "import": 0.0, "operation": "plan"}} {
			_, err := changeSummary(streamDiag{Summaries: []map[string]any{m}})
			require.ErrorIs(t, err, errChangeSummary)
		}
	})

	t.Run("負の件数と小数の件数は失敗する", func(t *testing.T) {
		t.Parallel()

		for _, v := range []float64{-1, 1.5} {
			_, err := changeSummary(streamDiag{Summaries: []map[string]any{{"add": v, "change": 0.0, "import": 0.0, "remove": 0.0}}})
			require.ErrorIs(t, err, errChangeSummary)
		}
	})

	t.Run("数でない値なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := changeSummary(streamDiag{Summaries: []map[string]any{{"add": "1", "change": 0.0, "import": 0.0, "remove": 0.0}}})
		require.ErrorIs(t, err, errChangeSummary)
	})
}

func Test_diagReport(t *testing.T) {
	t.Parallel()

	t.Run("終了コードが 0 でないのに診断が0件なら、そのことを出す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"診断: count=0; exit=1"}, diagReport(streamDiag{}, 1))
	})

	t.Run("成功で診断が無ければ何も出さない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, diagReport(streamDiag{}, 0))
	})

	t.Run("伏せた数を出す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"診断: kind=core", "診断: masked=2"}, diagReport(streamDiag{Lines: []string{"診断: kind=core"}, Errors: 1, Masked: 2}, 1))
	})

	t.Run("件数が合わなければ、そのことを出す", func(t *testing.T) {
		t.Parallel()

		got := diagReport(streamDiag{Errors: 2, Lines: []string{"診断: kind=core"}}, 1)
		require.Len(t, got, 2)
		assert.Contains(t, got[1], "診断の件数が合いません")
	})
}

func Test_streamDiag_reconciled(t *testing.T) {
	t.Parallel()

	t.Run("error の行が診断と読めない error の行の和に一致すれば合う", func(t *testing.T) {
		t.Parallel()

		assert.True(t, streamDiag{Errors: 3, Lines: []string{"a", "b"}, UnparsedErrors: 1}.reconciled())
	})

	t.Run("1つ多くても少なくても合わない", func(t *testing.T) {
		t.Parallel()

		assert.False(t, streamDiag{Errors: 4, Lines: []string{"a", "b"}, UnparsedErrors: 1}.reconciled())
		assert.False(t, streamDiag{Errors: 2, Lines: []string{"a", "b"}, UnparsedErrors: 1}.reconciled())
	})
}

func policyEntries(t *testing.T, raw string) []policyresult.Entry {
	t.Helper()

	entries, err := policyresult.Parse([]byte(raw))
	require.NoError(t, err)

	return entries
}

func Test_policyLines(t *testing.T) {
	t.Parallel()

	t.Run("規則の ID と名前空間ごとの件数だけを出し、文を出さない", func(t *testing.T) {
		t.Parallel()

		entries := policyEntries(t, `[{"filename":"p","namespace":"plan.state_backend_write_deny","successes":1,`+
			`"failures":[{"msg":"[PLAN-SBW-10] arn:aws:iam::111111111111:role/x が書ける"},{"msg":"[PLAN-SBW-10] y"},{"msg":"no id"}]}]`)

		got := policyLines(entries)
		assert.Equal(t, []string{
			"診断: kind=policy; rule=[PLAN-SBW-10]; namespace=plan.state_backend_write_deny; count=2",
			"診断: kind=policy; namespace=plan.state_backend_write_deny; count=1",
		}, got)
	})

	t.Run("違反が無ければ何も出さない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, policyLines(policyEntries(t, `[{"filename":"p","namespace":"plan.x","successes":2,"failures":[]}]`)))
	})
}

func Test_policyMessages(t *testing.T) {
	t.Parallel()

	t.Run("名前空間と文を1行ずつ並べる", func(t *testing.T) {
		t.Parallel()

		entries := policyEntries(t, `[{"filename":"p","namespace":"plan.x","successes":0,"failures":[{"msg":"a"},{"msg":"b"}]}]`)
		assert.Equal(t, "plan.x: a\nplan.x: b\n", policyMessages(entries))
	})
}

func Test_ciRunTool(t *testing.T) {
	t.Parallel()

	t.Run("失敗したら、stderr の許可の文法に当たる欄だけを出す", func(t *testing.T) {
		t.Parallel()

		tool, _ := fakeFlow(t, map[string]string{
			stepCallerIdentity + ".rc":     "254",
			stepCallerIdentity + ".stderr": "An error occurred (ExpiredToken) when calling the GetCallerIdentity operation: arn:aws:iam::111111111111:x\n",
		})

		var stderr bytes.Buffer

		_, err := ciRunTool(tool, t.TempDir(), nil, stepCallerIdentity, &stderr)
		require.ErrorIs(t, err, errLocalAWS)
		assert.Equal(t, "診断: kind=cli; operation=GetCallerIdentity; code=ExpiredToken\n", stderr.String())
	})

	t.Run("成功したら stderr を出さずに stdout を返す", func(t *testing.T) {
		t.Parallel()

		tool, _ := fakeFlow(t, map[string]string{stepCallerIdentity + ".stderr": "warning 111111111111\n"})

		var stderr bytes.Buffer

		out, err := ciRunTool(tool, t.TempDir(), nil, stepCallerIdentity, &stderr)
		require.NoError(t, err)
		assert.Equal(t, callerIdentity, string(out))
		assert.Empty(t, stderr.String())
	})

	t.Run("stderr が空なら何も出さない", func(t *testing.T) {
		t.Parallel()

		tool, _ := fakeFlow(t, map[string]string{stepCallerIdentity + ".rc": "254"})

		var stderr bytes.Buffer

		_, err := ciRunTool(tool, t.TempDir(), nil, stepCallerIdentity, &stderr)
		require.ErrorIs(t, err, errLocalAWS)
		assert.Empty(t, stderr.String())
	})

	t.Run("stderr へ書けなければ、aws の失敗と書き込みの失敗を両方返す", func(t *testing.T) {
		t.Parallel()

		tool, _ := fakeFlow(t, map[string]string{stepCallerIdentity + ".rc": "254", stepCallerIdentity + ".stderr": "x\n"})

		_, err := ciRunTool(tool, t.TempDir(), nil, stepCallerIdentity, failingWriter{})
		require.ErrorIs(t, err, errLocalAWS)
		require.ErrorIs(t, err, errWrite)
	})

	t.Run("失敗しても終了コードの error を返す", func(t *testing.T) {
		t.Parallel()

		tool, _ := fakeFlow(t, map[string]string{stepCallerIdentity + ".rc": "255", stepCallerIdentity + ".stderr": "Could not connect\n"})

		var stderr bytes.Buffer

		_, err := ciRunTool(tool, t.TempDir(), nil, stepCallerIdentity, &stderr)
		require.ErrorIs(t, err, errLocalAWS)
		assert.Equal(t, "診断: kind=cli; masked=1\n", stderr.String())
	})
}

func Test_cliLine(t *testing.T) {
	t.Parallel()

	t.Run("空白だけなら空", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, cliLine(" \n"))
	})

	t.Run("API の error でなければ伏せたことだけを出す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "診断: kind=cli; masked=1", cliLine("Unable to locate credentials ops@example.com"))
	})
}

func Test_diagSection(t *testing.T) {
	t.Parallel()

	t.Run("行が無ければ空", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, diagSection(nil))
	})

	t.Run("行を escape して pre に入れる", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "<pre>\n診断: address=a.b[&lt;伏せた key&gt;]\n</pre>\n\n", diagSection([]string{"診断: address=a.b[<伏せた key>]"}))
	})
}

func Test_writeLines(t *testing.T) {
	t.Parallel()

	t.Run("1行ずつ改行を付けて書く", func(t *testing.T) {
		t.Parallel()

		var b bytes.Buffer
		require.NoError(t, writeLines(&b, []string{"a", "b"}))
		assert.Equal(t, "a\nb\n", b.String())
	})

	t.Run("書き込みに失敗したら error を返す", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, writeLines(failingWriter{}, []string{"a"}), errWrite)
	})
}

func Test_appendPolicyOut(t *testing.T) {
	t.Parallel()

	t.Run("全文の末尾に足す", func(t *testing.T) {
		t.Parallel()

		s := ciState{Out: fileWith(t, "head\n")}
		require.NoError(t, appendPolicyOut(s, "tail\n"))

		raw, err := os.ReadFile(s.Out)
		require.NoError(t, err)
		assert.Equal(t, "head\ntail\n", string(raw))
	})

	t.Run("空なら全文を開かない", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, appendPolicyOut(ciState{Out: filepath.Join(t.TempDir(), "none", "o.txt")}, ""))
	})

	t.Run("全文を開けなければ失敗する", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, appendPolicyOut(ciState{Out: filepath.Join(t.TempDir(), "none", "o.txt")}, "x"), os.ErrNotExist)
	})
}
