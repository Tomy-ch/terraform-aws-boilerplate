package publicdiag

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 識別子の部品。敵対的な入力はこれらを組み、出した行に1つも残らないことを見る。
var secrets = []string{
	"111111111111", "ops@example.com", "arn:aws:iam::111111111111:role/tabp-admin-ci-apply",
	"o-abc123defg", "ou-ab12-cdefgh34", "example.com", "r-ab12", "Security", "tabp-admin-tfstate-111111111111-ap-northeast-1-an",
}

// 文の断片。写し戻された文の中に、同じ形の2つ目の一致を作る。
var fragments = []string{
	"operation error ", "IAM: CreateRole, ", "https response error StatusCode: 403, RequestID: ", "api error ",
	"AccessDenied: ", "is not authorized to perform: ", "iam:CreateRole", " because no identity-based policy allows the ",
	"with an explicit deny in a service control policy: ", "An error occurred (", ") when calling the ", " operation",
	"; ", "=", "\n", "\r", "\"", "<", ">", "[", "]", "日本語", "\x00", "\t", "診断: ", "kind=", "code=",
}

func adversarial(r *rand.Rand) string {
	var b strings.Builder

	for range r.IntN(12) + 1 {
		if r.IntN(2) == 0 {
			b.WriteString(secrets[r.IntN(len(secrets))])
		} else {
			b.WriteString(fragments[r.IntN(len(fragments))])
		}
	}

	return b.String()
}

// samples は、欄ごとの文法に当たる見本の値。
var samples = map[string]string{
	"kind": "iam-deny", "address": "aws_iam_role.this", "file": "main.tf", "line": "3", "summary": "Resource postcondition failed",
	"service": "IAM", "operation": "CreateRole", "status": "403", "code": "AccessDenied", "action": "iam:CreateRole",
	"reason": "implicit:scp", "rule": "[PLAN-SBW-10]", "namespace": "plan.p", "elapsed": "3s", "count": "1", "exit": "1", "masked": "2",
}

// pickShort は、SDK の文の欄に入れる短い値。半分は形に当たる語、残りは識別子を含む語。
func pickShort(r *rand.Rand) string {
	words := []string{"IAM", "S3", "CreateRole", "AccessDenied", "111111111111", "ops@example.com", "o-abc123defg", "A b"}

	return words[r.IntN(len(words))]
}

func noSecret(t *testing.T, line string) {
	t.Helper()

	// 組織・OU の ID や名前は、識別子の形でなく構成の名前の形でも現れうるので、ここでは数えない（意味は文法の外）。
	for _, s := range []string{"111111111111", "@", "arn:aws:iam::", "tfstate-"} {
		assert.NotContains(t, line, s)
	}
}

func TestFormat(t *testing.T) {
	t.Parallel()

	t.Run("文法に当たる欄を決まった順に並べる", func(t *testing.T) {
		t.Parallel()

		line, masked := Format(Field{"code", "AccessDenied"}, Field{"kind", "iam-deny"}, Field{"action", "iam:CreateRole"})
		assert.Equal(t, "診断: kind=iam-deny; code=AccessDenied; action=iam:CreateRole", line)
		assert.Equal(t, 0, masked)
	})

	t.Run("文法に当たらない値と知らない欄と重なった欄を伏せて数える", func(t *testing.T) {
		t.Parallel()

		line, masked := Format(
			Field{"kind", "api-error"}, Field{"code", "Not Valid"}, Field{"detail", "x"}, Field{"kind", "core"},
			Field{"operation", "111111111111"}, Field{"summary", "creating IAM Role (tabp): x"},
		)
		assert.Equal(t, "診断: kind=api-error", line)
		assert.Equal(t, 5, masked)
	})

	t.Run("文法の形でも12桁の数字と @ を含む値は伏せる", func(t *testing.T) {
		t.Parallel()

		line, masked := Format(Field{"code", "A111111111111"}, Field{"kind", "cli"})
		assert.Equal(t, "診断: kind=cli", line)
		assert.Equal(t, 1, masked)
	})

	t.Run("空の値は欄にも伏せた数にも入れない", func(t *testing.T) {
		t.Parallel()

		line, masked := Format(Field{"kind", "core"}, Field{"code", ""})
		assert.Equal(t, "診断: kind=core", line)
		assert.Equal(t, 0, masked)
	})

	t.Run("どの欄も出せなければ空の行と伏せた数を返す", func(t *testing.T) {
		t.Parallel()

		line, masked := Format(Field{"kind", "nope"}, Field{"code", "x y"})
		assert.Empty(t, line)
		assert.Equal(t, 2, masked)
	})

	t.Run("敵対的な入力でも、出す行は文法に当たり識別子を含まない", func(t *testing.T) {
		t.Parallel()

		r := rand.New(rand.NewPCG(485, 1))
		names := append(append([]string{}, order...), "detail", "message")
		produced := 0

		for range 5000 {
			fields := make([]Field, r.IntN(6)+1)
			for i := range fields {
				name := names[r.IntN(len(names))]
				// 半分は文法に当たる見本の値のまま、残りは見本に敵対的な断片を前後へ足す。
				value := samples[name]
				if r.IntN(2) == 0 || value == "" {
					value += adversarial(r)
				}
				fields[i] = Field{name, value}
			}

			line, masked := Format(fields...)
			if line != "" {
				produced++
				require.True(t, Valid(line), line)
				noSecret(t, line)
			}

			assert.LessOrEqual(t, masked, len(fields))
		}

		assert.Greater(t, produced, 1000, "検査した行が少なすぎる")
	})
}

func TestValid(t *testing.T) {
	t.Parallel()

	type tc struct {
		name string
		line string
	}

	for _, c := range []tc{
		{
			name: "診断の行",
			line: "診断: kind=iam-deny; service=IAM; operation=CreateRole; status=403; code=AccessDenied; action=iam:CreateRole; reason=implicit:identity-based",
		},
		{
			name: "条件の失敗のファイルと行",
			line: "診断: kind=condition; file=../../usecases/state-backend/main.tf; line=42; summary=Resource postcondition failed",
		},
		{
			name: "伏せた key を持つ address",
			line: "診断: kind=api-error; address=module.org.aws_organizations_organizational_unit.this[<伏せた key>]",
		},
		{
			name: "登録簿の key と index",
			line: `診断: kind=core; address=module.ci["plan"].aws_iam_role.this[0]`,
		},
		{
			name: "規則の ID と件数",
			line: "診断: kind=policy; rule=[PLAN-SBW-10]; namespace=plan.state_backend_write_deny; count=2",
		},
	} {
		t.Run(c.name+"は通す", func(t *testing.T) {
			t.Parallel()

			assert.True(t, Valid(c.line))
		})
	}

	for _, c := range []tc{
		{
			name: "接頭辞が無い",
			line: "kind=core",
		},
		{
			name: "欄の区切りが無い",
			line: "診断: kind",
		},
		{
			name: "知らない欄",
			line: "診断: detail=x",
		},
		{
			name: "欄が重なる",
			line: "診断: kind=core; kind=cli",
		},
		{
			name: "12桁の数字",
			line: "診断: kind=core; code=A111111111111",
		},
		{
			name: "@",
			line: "診断: kind=core; code=a@b",
		},
		{
			name: "改行",
			line: "診断: kind=core\nx",
		},
		{
			name: "ドットを含む key",
			line: `診断: address=aws_s3_bucket.this["example.com"]`,
		},
		{
			name: "大文字の key",
			line: `診断: address=aws_s3_bucket.this["Security"]`,
		},
		{
			name: "空の行",
			line: "",
		},
		{
			name: "空の値",
			line: "診断: kind=",
		},
		{
			name: "自由文の summary",
			line: "診断: summary=creating IAM Role (x): y",
		},
	} {
		t.Run(c.name+"は落とす", func(t *testing.T) {
			t.Parallel()

			assert.False(t, Valid(c.line))
		})
	}
}

func Test_allowed(t *testing.T) {
	t.Parallel()

	t.Run("欄の文法に当たる値は受ける", func(t *testing.T) {
		t.Parallel()

		assert.True(t, allowed("status", "403"))
	})

	t.Run("知らない欄は受けない", func(t *testing.T) {
		t.Parallel()

		assert.False(t, allowed("detail", "x"))
	})

	t.Run("長さの上限の境界", func(t *testing.T) {
		t.Parallel()

		assert.True(t, allowed("operation", "A"+strings.Repeat("b", 63)))
		assert.False(t, allowed("operation", "A"+strings.Repeat("b", 64)))
	})

	t.Run("status は 100 と 599 を受け、99 と 600 を受けない", func(t *testing.T) {
		t.Parallel()

		assert.True(t, allowed("status", "100"))
		assert.True(t, allowed("status", "599"))
		assert.False(t, allowed("status", "99"))
		assert.False(t, allowed("status", "600"))
	})

	t.Run("line は 1 と 999999 を受け、0 と 1000000 を受けない", func(t *testing.T) {
		t.Parallel()

		assert.True(t, allowed("line", "1"))
		assert.True(t, allowed("line", "999999"))
		assert.False(t, allowed("line", "0"))
		assert.False(t, allowed("line", "1000000"))
	})

	t.Run("count は先頭の 0 を受けない", func(t *testing.T) {
		t.Parallel()

		assert.True(t, allowed("count", "0"))
		assert.False(t, allowed("count", "01"))
	})

	t.Run("summary は固定の文だけを受け、ほかの英字の文を受けない", func(t *testing.T) {
		t.Parallel()

		assert.True(t, allowed("summary", "Error acquiring the state lock"))
		assert.False(t, allowed("summary", "Reference to undeclared resource"))
	})

	t.Run("address は長さの上限まで受け、超えれば受けない", func(t *testing.T) {
		t.Parallel()

		assert.True(t, allowed("address", "a"+strings.Repeat("b", 511)))
		assert.False(t, allowed("address", "a"+strings.Repeat("b", 512)))
	})

	t.Run("12桁の数字は文法の形でも受けない、11桁は受ける", func(t *testing.T) {
		t.Parallel()

		assert.False(t, allowed("code", "E111111111111"))
		assert.True(t, allowed("code", "E11111111111"))
	})
}

func TestSDK(t *testing.T) {
	t.Parallel()

	const denied = `creating IAM Role (tabp-admin-x): operation error IAM: CreateRole, https response error StatusCode: 403, ` +
		`RequestID: 0f0f, api error AccessDenied: User: arn:aws:sts::111111111111:assumed-role/tabp-admin-ci-apply-identity/gha ` +
		`is not authorized to perform: iam:CreateRole on resource: arn:aws:iam::111111111111:role/x because no identity-based policy allows the iam:CreateRole action`

	t.Run("AccessDenied から service・operation・status・code・Action・理由を取る", func(t *testing.T) {
		t.Parallel()

		got, ok := SDK(denied)
		require.True(t, ok)
		assert.Equal(t, []Field{
			{"kind", "iam-deny"}, {"service", "IAM"}, {"operation", "CreateRole"}, {"status", "403"}, {"code", "AccessDenied"},
			{"action", "iam:CreateRole"}, {"reason", "implicit:identity-based"},
		}, got)

		line, masked := Format(got...)
		assert.True(t, Valid(line))
		assert.Equal(t, 0, masked)
		noSecret(t, line)
	})

	t.Run("モデル化された error は api error を持たない", func(t *testing.T) {
		t.Parallel()

		got, ok := SDK(`operation error IAM: CreateRole, https response error StatusCode: 409, RequestID: a, EntityAlreadyExists: Role with name x already exists.`)
		require.True(t, ok)
		assert.Equal(t, []Field{{"kind", "api-error"}, {"service", "IAM"}, {"operation", "CreateRole"}, {"status", "409"}, {"code", "EntityAlreadyExists"}}, got)
	})

	t.Run("S3 の HostID を code と取り違えない", func(t *testing.T) {
		t.Parallel()

		got, ok := SDK(`operation error S3: PutObject, https response error StatusCode: 403, RequestID: ABC, HostID: xyz/abc=, api error AccessDenied: ` +
			`User: x is not authorized to perform: s3:PutObject`)
		require.True(t, ok)
		assert.Contains(t, got, Field{"code", "AccessDenied"})
		assert.Contains(t, got, Field{"action", "s3:PutObject"})
	})

	t.Run("再試行が尽きた文も読む", func(t *testing.T) {
		t.Parallel()

		got, ok := SDK(`operation error S3: PutObject, exceeded maximum number of attempts, 3, https response error StatusCode: 503, RequestID: a, api error SlowDown: x`)
		require.True(t, ok)
		assert.Contains(t, got, Field{"code", "SlowDown"})
	})

	t.Run("明示の Deny の種類を取る", func(t *testing.T) {
		t.Parallel()

		got, ok := SDK(`operation error S3: PutObject, https response error StatusCode: 403, RequestID: a, api error AccessDenied: User: x ` +
			`is not authorized to perform: s3:PutObject on resource: "arn:aws:s3:::b" with an explicit deny in a service control policy: arn:aws:organizations::111111111111:policy/o-x/p-y`)
		require.True(t, ok)
		assert.Contains(t, got, Field{"reason", "explicit:scp"})
	})

	t.Run("2つ目の一致は読まない", func(t *testing.T) {
		t.Parallel()

		got, ok := SDK(`operation error STS: AssumeRole, https response error StatusCode: 400, RequestID: a, api error ValidationError: ` +
			`value operation error IAM: CreateRole, https response error StatusCode: 403, RequestID: b, api error AccessDenied: x`)
		require.True(t, ok)
		assert.Contains(t, got, Field{"code", "ValidationError"})
		assert.NotContains(t, got, Field{"code", "AccessDenied"})
	})

	t.Run("知らない理由の語は理由を出さない", func(t *testing.T) {
		t.Parallel()

		got, ok := SDK(`operation error S3: GetObject, https response error StatusCode: 403, RequestID: a, api error AccessDenied: ` +
			`is not authorized to perform: s3:GetObject because no foo policy allows the s3:GetObject action`)
		require.True(t, ok)
		assert.Contains(t, got, Field{"action", "s3:GetObject"})

		for _, f := range got {
			assert.NotEqual(t, "reason", f.Name)
		}
	})

	t.Run("SDK の文でなければ false", func(t *testing.T) {
		t.Parallel()

		_, ok := SDK("Error: Unsupported argument")
		assert.False(t, ok)
	})

	t.Run("敵対的な入力でも、取った欄の行は文法に当たる", func(t *testing.T) {
		t.Parallel()

		r := rand.New(rand.NewPCG(485, 2))
		matched := 0

		for range 5000 {
			// SDK の文の形の各欄に、敵対的な断片を入れる。
			text := adversarial(r) + "operation error " + pickShort(r) + ": " + pickShort(r) + ", https response error StatusCode: 403, RequestID: " +
				adversarial(r) + ", api error " + pickShort(r) + ": " + adversarial(r)

			got, ok := SDK(text)
			if !ok {
				continue
			}

			matched++

			if line, _ := Format(got...); line != "" {
				require.True(t, Valid(line), line)
				noSecret(t, line)
			}
		}

		assert.Greater(t, matched, 100, "SDK の文に当たった入力が少なすぎる")
	})
}

func TestCLI(t *testing.T) {
	t.Parallel()

	t.Run("enhanced 形式の stderr から code と operation を取る", func(t *testing.T) {
		t.Parallel()

		got, ok := CLI("\naws: [ERROR]: An error occurred (AccessDenied) when calling the AssumeRoleWithWebIdentity operation: Not authorized\n")
		require.True(t, ok)
		assert.Equal(t, []Field{{"kind", "cli"}, {"code", "AccessDenied"}, {"operation", "AssumeRoleWithWebIdentity"}}, got)
	})

	t.Run("AccessDenied の文から Action を取る", func(t *testing.T) {
		t.Parallel()

		got, ok := CLI("An error occurred (AccessDenied) when calling the PutObject operation (reached max retries: 2): User: x is not authorized to perform: s3:PutObject")
		require.True(t, ok)
		assert.Contains(t, got, Field{"action", "s3:PutObject"})
	})

	t.Run("2つ目の一致は読まない", func(t *testing.T) {
		t.Parallel()

		got, ok := CLI("An error occurred (ValidationError) when calling the PutObject operation: An error occurred (AccessDenied) when calling the X operation")
		require.True(t, ok)
		assert.Contains(t, got, Field{"code", "ValidationError"})
		assert.NotContains(t, got, Field{"code", "AccessDenied"})
	})

	t.Run("API の error でなければ false", func(t *testing.T) {
		t.Parallel()

		_, ok := CLI(`Unable to locate credentials. You can configure credentials by running "aws login".`)
		assert.False(t, ok)
	})
}

func Test_deny(t *testing.T) {
	t.Parallel()

	type tc struct {
		name string
		msg  string
		want []Field
	}

	for _, c := range []tc{
		{
			name: "resource-based の暗黙の拒否",
			msg:  "is not authorized to perform: kms:Decrypt on resource: x because no resource-based policy allows the kms:Decrypt action",
			want: []Field{{"action", "kms:Decrypt"}, {"reason", "implicit:resource-based"}},
		},
		{
			name: "permissions boundary の暗黙の拒否",
			msg:  "is not authorized to perform: s3:GetObject because no permissions boundary allows the s3:GetObject action",
			want: []Field{{"action", "s3:GetObject"}, {"reason", "implicit:permissions-boundary"}},
		},
		{
			name: "role trust の明示の拒否",
			msg:  "is not authorized to perform: s3:GetObject with an explicit deny in the role trust policy",
			want: []Field{{"action", "s3:GetObject"}, {"reason", "explicit:role-trust"}},
		},
		{
			name: "RCP の明示の拒否",
			msg:  "is not authorized to perform: s3:GetObject with an explicit deny in a resource control policy: arn:x",
			want: []Field{{"action", "s3:GetObject"}, {"reason", "explicit:rcp"}},
		},
		{
			name: "理由の無い拒否は Action だけを出す",
			msg:  "is not authorized to perform: s3:GetObject",
			want: []Field{{"action", "s3:GetObject"}},
		},
		{
			name: "Action の無い文は欄を出さない",
			msg:  "Access Denied",
			want: nil,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, deny(c.msg))
		})
	}
}

func TestMaskAddress(t *testing.T) {
	t.Parallel()

	registry := func(k string) bool { return k == "plan" || k == "admin" || k == "a111111111111" }

	type tc struct {
		name string
		in   string
		want string
	}

	for _, c := range []tc{
		{
			name: "登録簿が受けても、識別子を含む key は伏せる",
			in:   `aws_x.this["a111111111111"]`,
			want: `aws_x.this[<伏せた key>]`,
		},
		{
			name: "登録簿の role の key を残す",
			in:   `module.ci["plan"].aws_iam_role.this`,
			want: `module.ci["plan"].aws_iam_role.this`,
		},
		{
			name: "リージョンの key を残す",
			in:   `aws_cloudwatch_event_rule.this["us-east-1"]`,
			want: `aws_cloudwatch_event_rule.this["us-east-1"]`,
		},
		{
			name: "整数の index を残す",
			in:   `aws_iam_role.this[0]`,
			want: `aws_iam_role.this[0]`,
		},
		{
			name: "OU の名前の key を伏せる",
			in:   `aws_organizations_organizational_unit.this["Security"]`,
			want: `aws_organizations_organizational_unit.this[<伏せた key>]`,
		},
		{
			name: "ドメインの key を伏せる",
			in:   `aws_route53_record.ds["example.com"]`,
			want: `aws_route53_record.ds[<伏せた key>]`,
		},
		{
			name: "アカウント ID の key を伏せる",
			in:   `aws_organizations_account.this["111111111111"]`,
			want: `aws_organizations_account.this[<伏せた key>]`,
		},
		{
			name: "メールの key を伏せる",
			in:   `aws_iam_user.this["ops@example.com"]`,
			want: `aws_iam_user.this[<伏せた key>]`,
		},
		{
			name: "key の外の識別子は address ごと伏せる",
			in:   `module.m_111111111111.aws_iam_role.this`,
			want: `<伏せた address>`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := MaskAddress(c.in, registry)
			assert.Equal(t, c.want, got)

			if got != "<伏せた address>" {
				assert.True(t, Valid(Prefix+"address="+got), got)
			}
		})
	}
}
