package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dnsVariablesTF = `variable "env" {
  type = string

  validation {
    condition     = contains(["dev", "stg", "prd", "sandbox"], var.env)
    error_message = "x"
  }
}

variable "apex_domain" {
  type = string

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.)+[a-z]{2,63}$", var.apex_domain)) && length(var.apex_domain) <= 240
    error_message = "x"
  }
}

variable "delegations" {
  type = map(object({
    name_servers = list(string)
    ds_record    = optional(string)
  }))

  validation {
    condition     = length(setsubtract(keys(var.delegations), ["dev", "stg"])) == 0
    error_message = "x"
  }

  validation {
    condition = alltrue([for d in values(var.delegations) :
      length(d.name_servers) == 4 && length(distinct(d.name_servers)) == 4 &&
      alltrue([for ns in d.name_servers : can(regex("^ns-[0-9]{1,4}\\.awsdns-[0-9]{2}\\.(com|net|org|co\\.uk)$", ns))])
    ])
    error_message = "x"
  }

  validation {
    condition     = alltrue([for d in values(var.delegations) : d.ds_record == null || (can(regex("^[0-9]{1,5} 13 2 [0-9A-F]{64}$", d.ds_record)) && try(tonumber(split(" ", d.ds_record)[0]) <= 65535, false))])
    error_message = "x"
  }
}
`
	apexZone      = "example.com"
	devZone       = "dev.example.com"
	zoneID        = "Z1"
	dnsKeyARN     = "arn:aws:kms:us-east-1:111111111111:key/k1"
	kskName       = "tabpdev"
	kskPublicKey  = "mdsswUyr3DPW132mOi8V9xESWE8jTo0dxCjjnopKl+GqJxpVXckHAeF+KkxLbxILfDLUT0rAK9iUzy1L53eKGQ=="
	getZoneNS     = "route53 get-hosted-zone --id " + zoneID
	getDNSSEC     = "route53 get-dnssec --hosted-zone-id " + zoneID
	describeKey   = "kms describe-key --key-id " + dnsKeyARN
	activateWrite = "route53 activate-key-signing-key --hosted-zone-id " + zoneID + " --name " + kskName
)

var (
	zoneNS     = []string{"ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"}
	testDS     = "12345 13 2 " + strings.Repeat("AB", 32)
	goodKeyRes = ok(`{"KeyMetadata":{"KeyState":"Enabled","KeySpec":"ECC_NIST_P256","KeyUsage":"SIGN_VERIFY"}}`)
)

func listZones(name string) string { return "route53 list-hosted-zones-by-name --dns-name " + name }

func domainDetail(name string) string {
	return "route53domains get-domain-detail --domain-name " + name
}

func dnsFiles() map[string]string {
	return map[string]string{path.Join(testUsecase, dnsVariablesFile): dnsVariablesTF}
}

func dnsOptions(env, zone string) options {
	o := testOptions()
	o.usecase = testUsecase
	o.env = env
	o.zone = zone
	o.readFile = readFiles(dnsFiles())
	o.sleep = func(time.Duration) {}

	return o
}

func testDNSShape(t *testing.T) dnsShape {
	t.Helper()

	s, err := readDNSShape(dnsOptions("dev", devZone))
	require.NoError(t, err)

	return s
}

func zonesJSON(name string) awsResult {
	return ok(`{"HostedZones":[{"Id":"/hostedzone/` + zoneID + `","Name":"` + name + `.","Config":{"PrivateZone":false}}]}`)
}

func nsJSON(ns ...string) awsResult {
	b, _ := json.Marshal(map[string]any{"DelegationSet": map[string]any{"NameServers": ns}})

	return ok(string(b))
}

func testKSK(status string) signingKey {
	return signingKey{
		Name: kskName, KmsArn: dnsKeyARN, Flag: kskFlag, SigningAlgorithmType: 13, KeyTag: 12345,
		PublicKey: kskPublicKey, DSRecord: testDS, Status: status,
	}
}

func dnssecJSON(serve string, ksks ...signingKey) awsResult {
	if ksks == nil {
		ksks = []signingKey{}
	}

	b, _ := json.Marshal(map[string]any{"Status": map[string]any{"ServeSignature": serve}, "KeySigningKeys": ksks})

	return ok(string(b))
}

func zoneWorld(t *testing.T, zone, serve string, ksks ...signingKey) *fakeAWS {
	t.Helper()

	return newFakeAWS(t, map[string]awsResult{
		listZones(zone): zonesJSON(zone),
		getZoneNS:       nsJSON(zoneNS...),
		getDNSSEC:       dnssecJSON(serve, ksks...),
		describeKey:     goodKeyRes,
	})
}

func registrarJSON(ns []string, keys ...registrarKey) awsResult {
	servers := make([]map[string]any, len(ns))
	for i, n := range ns {
		servers[i] = map[string]any{"Name": n}
	}

	b, _ := json.Marshal(map[string]any{"DomainName": apexZone, "Nameservers": servers, "DnssecKeys": keys})

	return ok(string(b))
}

func nsWrite(ns ...string) string {
	return "route53domains update-domain-nameservers --domain-name " + apexZone + " --nameservers Name=" + strings.Join(ns, " Name=")
}

func dsWrite() string {
	return "route53domains associate-delegation-signer-to-domain --domain-name " + apexZone +
		` --signing-attributes {"Algorithm":13,"Flags":257,"PublicKey":"` + kskPublicKey + `"}`
}

func Test_readDNSShape(t *testing.T) {
	t.Parallel()

	withoutDelegations, _, found := strings.Cut(dnsVariablesTF, `variable "delegations"`)
	require.True(t, found)

	t.Run("委任の key と NS と DS の形を読む", func(t *testing.T) {
		t.Parallel()

		got := testDNSShape(t)
		assert.Equal(t, []string{"dev", "stg"}, got.delegated)
		assert.True(t, got.ns.MatchString("ns-1.awsdns-01.co.uk"))
		assert.False(t, got.ns.MatchString("ns-1.awsdns-01.co.uk."))
		assert.True(t, got.ds.MatchString(testDS))
	})

	t.Run("prd は apex の zone を受ける", func(t *testing.T) {
		t.Parallel()

		_, err := readDNSShape(dnsOptions("prd", apexZone))
		require.NoError(t, err)
	})

	t.Run("宣言に無い env は errEnvName", func(t *testing.T) {
		t.Parallel()

		_, err := readDNSShape(dnsOptions("admin", "admin.example.com"))
		require.ErrorIs(t, err, errEnvName)
	})

	for name, tc := range map[string]struct{ env, zone string }{
		"末尾のドット":              {"prd", "example.com."},
		"大文字":                 {"prd", "Example.com"},
		"ドットの無い名前":            {"prd", "localhost"},
		"dev の zone が apex":   {"dev", apexZone},
		"dev の zone が stg の下": {"dev", "stg.example.com"},
		"env が名前の途中にだけ在る":     {"dev", "xdev.example.com"},
	} {
		t.Run(name+"なら errZoneName", func(t *testing.T) {
			t.Parallel()

			_, err := readDNSShape(dnsOptions(tc.env, tc.zone))
			require.ErrorIs(t, err, errZoneName)
		})
	}

	t.Run("variables.tf を読めなければ errHCL", func(t *testing.T) {
		t.Parallel()

		o := dnsOptions("dev", devZone)
		o.readFile = readFiles(nil)
		_, err := readDNSShape(o)
		require.ErrorIs(t, err, errHCL)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	for name, src := range map[string]string{
		"apex_domain の regex が無ければ":  strings.Replace(dnsVariablesTF, `can(regex("^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.)+[a-z]{2,63}$", var.apex_domain)) && `, "", 1),
		"delegations の key の一覧が無ければ": strings.Replace(dnsVariablesTF, `length(setsubtract(keys(var.delegations), ["dev", "stg"])) == 0`, "true", 1),
		"DS の regex が無ければ":           strings.Replace(dnsVariablesTF, `can(regex("^[0-9]{1,5} 13 2 [0-9A-F]{64}$", d.ds_record)) && `, "", 1),
		"delegations が無ければ":          withoutDelegations,
	} {
		t.Run(name+" errDeclShape", func(t *testing.T) {
			t.Parallel()

			o := dnsOptions("dev", devZone)
			o.readFile = readFiles(map[string]string{path.Join(testUsecase, dnsVariablesFile): src})
			_, err := readDNSShape(o)
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_delegationPatterns(t *testing.T) {
	t.Parallel()

	t.Run("ds_record に掛かる regex を DS、ほかを NS とする", func(t *testing.T) {
		t.Parallel()

		ns, ds, err := delegationPatterns(parseBody(t, `x = can(regex("^d$", d.ds_record)) && can(regex("^n$", ns))`))
		require.NoError(t, err)
		assert.Equal(t, "^n$", ns.String())
		assert.Equal(t, "^d$", ds.String())
	})

	for name, src := range map[string]string{
		"NS が2つ":    `x = can(regex("^a$", ns)) && can(regex("^b$", ns)) && can(regex("^d$", d.ds_record))`,
		"DS が無い":    `x = can(regex("^a$", ns))`,
		"NS が無い":    `x = can(regex("^d$", d.ds_record))`,
		"regex が無い": `x = true`,
	} {
		t.Run(name+"なら errDeclShape", func(t *testing.T) {
			t.Parallel()

			_, _, err := delegationPatterns(parseBody(t, src))
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_isAttrRef(t *testing.T) {
	t.Parallel()

	expr := func(t *testing.T, src string) hclsyntax.Expression {
		t.Helper()

		return parseBody(t, "x = "+src).Attributes["x"].Expr
	}

	cases := map[string]struct {
		src  string
		want bool
	}{
		"d.ds_record":     {"d.ds_record", true},
		"var.d.ds_record": {"var.d.ds_record", true},
		"ns":              {"ns", false},
		"d.other":         {"d.other", false},
		"ds_record だけ":    {"ds_record", false},
		"関数の呼び出し":         {`upper("ds_record")`, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isAttrRef(expr(t, tc.src), "ds_record"))
		})
	}
}

func Test_dnsShape_validNameServers(t *testing.T) {
	t.Parallel()

	s := testDNSShape(t)

	cases := map[string]struct {
		ns   []string
		want bool
	}{
		"重ならない4つ":      {zoneNS, true},
		"3つ":           {zoneNS[:3], false},
		"5つ":           {append(append([]string{}, zoneNS...), "ns-5.awsdns-05.com"), false},
		"重なる4つ":        {[]string{zoneNS[0], zoneNS[0], zoneNS[1], zoneNS[2]}, false},
		"末尾のドット":       {[]string{zoneNS[0] + ".", zoneNS[1], zoneNS[2], zoneNS[3]}, false},
		"Route 53 でない": {[]string{"ns1.example.net", zoneNS[1], zoneNS[2], zoneNS[3]}, false},
		"0件":           {nil, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, s.validNameServers(tc.ns))
		})
	}
}

func Test_dnsShape_validDS(t *testing.T) {
	t.Parallel()

	s := testDNSShape(t)
	digest := strings.Repeat("AB", 32)

	cases := map[string]struct {
		ds   string
		want bool
	}{
		"key tag 65535": {"65535 13 2 " + digest, true},
		"key tag 65536": {"65536 13 2 " + digest, false},
		"key tag 0":     {"0 13 2 " + digest, true},
		"小文字の digest":   {"1 13 2 " + strings.ToLower(digest), false},
		"algorithm が 8": {"1 8 2 " + digest, false},
		"digest が63桁":   {"1 13 2 " + digest[:63], false},
		"空":             {"", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, s.validDS(tc.ds))
		})
	}
}

func Test_hostedZoneID(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, body string) (string, error) {
		t.Helper()

		return hostedZoneID(testClient(newFakeAWS(t, map[string]awsResult{listZones(devZone): ok(body)})), devZone)
	}

	t.Run("名前の一致する公開の zone の ID から /hostedzone/ を除く", func(t *testing.T) {
		t.Parallel()

		got, err := run(t, `{"HostedZones":[{"Id":"/hostedzone/Z1","Name":"dev.example.com.","Config":{"PrivateZone":false}},{"Id":"/hostedzone/Z9","Name":"dev.example.net.","Config":{"PrivateZone":false}}]}`)
		require.NoError(t, err)
		assert.Equal(t, "Z1", got)
	})

	t.Run("非公開の zone は数えない", func(t *testing.T) {
		t.Parallel()

		got, err := run(t, `{"HostedZones":[{"Id":"/hostedzone/Z2","Name":"dev.example.com.","Config":{"PrivateZone":true}},{"Id":"/hostedzone/Z1","Name":"dev.example.com.","Config":{"PrivateZone":false}}]}`)
		require.NoError(t, err)
		assert.Equal(t, "Z1", got)
	})

	for name, body := range map[string]string{
		"一覧の始まりがほかの名前なら": `{"HostedZones":[{"Id":"/hostedzone/Z9","Name":"dev2.example.com.","Config":{}}]}`,
		"0件なら":           `{"HostedZones":[]}`,
		"公開の zone が2つなら": `{"HostedZones":[{"Id":"/hostedzone/Z1","Name":"dev.example.com.","Config":{}},{"Id":"/hostedzone/Z2","Name":"dev.example.com.","Config":{}}]}`,
		"末尾のドットの無い名前なら":  `{"HostedZones":[{"Id":"/hostedzone/Z1","Name":"dev.example.com","Config":{}}]}`,
	} {
		t.Run(name+" errPrecondition", func(t *testing.T) {
			t.Parallel()

			_, err := run(t, body)
			require.ErrorIs(t, err, errPrecondition)
		})
	}

	for name, body := range map[string]string{
		"HostedZones が無ければ": `{}`,
		"Id が空なら":           `{"HostedZones":[{"Id":"/hostedzone/","Name":"dev.example.com.","Config":{}}]}`,
	} {
		t.Run(name+" errAWSResponse", func(t *testing.T) {
			t.Parallel()

			_, err := run(t, body)
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_zoneNameServers(t *testing.T) {
	t.Parallel()

	t.Run("DelegationSet の NameServers を返す", func(t *testing.T) {
		t.Parallel()

		got, err := zoneNameServers(testClient(newFakeAWS(t, map[string]awsResult{getZoneNS: nsJSON(zoneNS...)})), zoneID)
		require.NoError(t, err)
		assert.Equal(t, zoneNS, got)
	})

	for name, body := range map[string]awsResult{
		"DelegationSet が無ければ": ok(`{}`),
		"NameServers が空なら":    nsJSON(),
		"空の名前が在れば":            nsJSON(zoneNS[0], ""),
	} {
		t.Run(name+" errAWSResponse", func(t *testing.T) {
			t.Parallel()

			_, err := zoneNameServers(testClient(newFakeAWS(t, map[string]awsResult{getZoneNS: body})), zoneID)
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_readDNSSEC(t *testing.T) {
	t.Parallel()

	t.Run("署名の状態と KSK を返す", func(t *testing.T) {
		t.Parallel()

		got, err := readDNSSEC(testClient(newFakeAWS(t, map[string]awsResult{getDNSSEC: dnssecJSON(serveSigning, testKSK(kskActive))})), zoneID)
		require.NoError(t, err)
		assert.Equal(t, dnssecState{serve: serveSigning, ksks: []signingKey{testKSK(kskActive)}}, got)
	})

	t.Run("KSK が0件でも返す", func(t *testing.T) {
		t.Parallel()

		got, err := readDNSSEC(testClient(newFakeAWS(t, map[string]awsResult{getDNSSEC: dnssecJSON("NOT_SIGNING")})), zoneID)
		require.NoError(t, err)
		assert.Empty(t, got.ksks)
	})

	noName := testKSK(kskActive)
	noName.Name = ""
	noArn := testKSK(kskActive)
	noArn.KmsArn = ""
	noStatus := testKSK("")

	for name, body := range map[string]awsResult{
		"ServeSignature が無ければ": ok(`{"Status":{},"KeySigningKeys":[]}`),
		"KeySigningKeys が無ければ": ok(`{"Status":{"ServeSignature":"SIGNING"}}`),
		"Name の欠けた KSK":        dnssecJSON(serveSigning, noName),
		"KmsArn の欠けた KSK":      dnssecJSON(serveSigning, noArn),
		"Status の欠けた KSK":      dnssecJSON(serveSigning, noStatus),
	} {
		t.Run(name+"は errAWSResponse", func(t *testing.T) {
			t.Parallel()

			_, err := readDNSSEC(testClient(newFakeAWS(t, map[string]awsResult{getDNSSEC: body})), zoneID)
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_dnssecState_activeKSK(t *testing.T) {
	t.Parallel()

	t.Run("SIGNING でただ1つの ACTIVE な KSK を返す", func(t *testing.T) {
		t.Parallel()

		got, reason := dnssecState{serve: serveSigning, ksks: []signingKey{testKSK(kskActive)}}.activeKSK()
		assert.Empty(t, reason)
		assert.Equal(t, testKSK(kskActive), got)
	})

	for name, tc := range map[string]struct {
		st   dnssecState
		want string
	}{
		"SIGNING でない":      {dnssecState{serve: "NOT_SIGNING", ksks: []signingKey{testKSK(kskActive)}}, "SIGNING ではありません（今: NOT_SIGNING）"},
		"KSK が0件":          {dnssecState{serve: serveSigning}, "KSK が 0 件"},
		"KSK が2件":          {dnssecState{serve: serveSigning, ksks: []signingKey{testKSK(kskActive), testKSK(kskActive)}}, "KSK が 2 件"},
		"KSK が ACTIVE でない": {dnssecState{serve: serveSigning, ksks: []signingKey{testKSK(kskActionNeeded)}}, "ACTIVE ではありません（今: ACTION_NEEDED）"},
	} {
		t.Run(name+"なら理由を返す", func(t *testing.T) {
			t.Parallel()

			_, reason := tc.st.activeKSK()
			assert.Contains(t, reason, tc.want)
		})
	}
}

func Test_dnssecKeyChecks(t *testing.T) {
	t.Parallel()

	t.Run("要件に合う鍵を us-east-1 で読み、すべて ok", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{describeKey: goodKeyRes})
		got, err := dnssecKeyChecks(testClient(f), dnsKeyARN)
		require.NoError(t, err)
		require.Len(t, got, 3)

		for _, kc := range got {
			assert.True(t, kc.ok, kc.what)
		}

		assert.Equal(t, "us-east-1", lastRegion(f.calls[0]))
	})

	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"Disabled":        {`{"KeyMetadata":{"KeyState":"Disabled","KeySpec":"ECC_NIST_P256","KeyUsage":"SIGN_VERIFY"}}`, "Enabled（今: Disabled）"},
		"PendingDeletion": {`{"KeyMetadata":{"KeyState":"PendingDeletion","KeySpec":"ECC_NIST_P256","KeyUsage":"SIGN_VERIFY"}}`, "Enabled（今: PendingDeletion）"},
		"ECC_NIST_P384":   {`{"KeyMetadata":{"KeyState":"Enabled","KeySpec":"ECC_NIST_P384","KeyUsage":"SIGN_VERIFY"}}`, "ECC_NIST_P256（今: ECC_NIST_P384）"},
		"ENCRYPT_DECRYPT": {`{"KeyMetadata":{"KeyState":"Enabled","KeySpec":"ECC_NIST_P256","KeyUsage":"ENCRYPT_DECRYPT"}}`, "SIGN_VERIFY（今: ENCRYPT_DECRYPT）"},
	} {
		t.Run(name+"の鍵は、その項目だけ NG", func(t *testing.T) {
			t.Parallel()

			got, err := dnssecKeyChecks(testClient(newFakeAWS(t, map[string]awsResult{describeKey: ok(tc.body)})), dnsKeyARN)
			require.NoError(t, err)

			var ngs []string

			for _, kc := range got {
				if !kc.ok {
					ngs = append(ngs, kc.what)
				}
			}

			require.Len(t, ngs, 1)
			assert.Contains(t, ngs[0], tc.want)
		})
	}

	for name, arn := range map[string]string{
		"us-east-1 でない鍵": "arn:aws:kms:ap-northeast-1:111111111111:key/k1",
		"KMS の ARN でない":  "arn:aws:sns:us-east-1:111111111111:k1",
		"ARN の形でない":      "k1",
	} {
		t.Run(name+"は読まずに NG", func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, nil)
			got, err := dnssecKeyChecks(testClient(f), arn)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.False(t, got[0].ok)
			assert.Empty(t, f.calls)
		})
	}

	t.Run("鍵が無ければ NG", func(t *testing.T) {
		t.Parallel()

		got, err := dnssecKeyChecks(testClient(newFakeAWS(t, map[string]awsResult{describeKey: fail(t, "An error occurred (NotFoundException)")})), dnsKeyARN)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.False(t, got[0].ok)
		assert.Contains(t, got[0].what, "が在る")
	})

	t.Run("KeyMetadata が欠けていれば errAWSResponse", func(t *testing.T) {
		t.Parallel()

		_, err := dnssecKeyChecks(testClient(newFakeAWS(t, map[string]awsResult{describeKey: ok(`{"KeyMetadata":{"KeyState":"Enabled"}}`)})), dnsKeyARN)
		require.ErrorIs(t, err, errAWSResponse)
	})

	t.Run("ほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		_, err := dnssecKeyChecks(testClient(newFakeAWS(t, map[string]awsResult{describeKey: fail(t, "AccessDeniedException")})), dnsKeyARN)
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_printDelegationValues(t *testing.T) {
	t.Parallel()

	t.Run("NS と DS を delegations の HCL の形で出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, printDelegationValues(dnsOptions("dev", devZone), testClient(zoneWorld(t, devZone, serveSigning, testKSK(kskActive))), &out))
		assert.Equal(t, "# prd の env の値の delegations に書く値（zone dev.example.com）\n"+
			"dev = {\n"+
			`  name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]`+"\n"+
			`  ds_record    = "`+testDS+`"`+"\n"+
			"}\n", out.String())
	})

	t.Run("署名が SIGNING でなければ ds_record を省き、理由を出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, printDelegationValues(dnsOptions("stg", "stg.example.com"), testClient(zoneWorld(t, "stg.example.com", "NOT_SIGNING")), &out))
		assert.NotContains(t, out.String(), "ds_record    =")
		assert.Contains(t, out.String(), "stg = {\n")
		assert.Contains(t, out.String(), "# ds_record を出しません: 署名が SIGNING ではありません（今: NOT_SIGNING）")
	})

	t.Run("KSK が ACTION_NEEDED なら ds_record を省く", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, printDelegationValues(dnsOptions("dev", devZone), testClient(zoneWorld(t, devZone, serveSigning, testKSK(kskActionNeeded))), &out))
		assert.NotContains(t, out.String(), testDS)
		assert.Contains(t, out.String(), "ACTIVE ではありません")
	})

	for name, env := range map[string]string{"prd": "prd", "sandbox": "sandbox"} {
		t.Run(name+" の zone は errEnvNotApplicable で aws を起動しない", func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, nil)
			zone := apexZone
			if env != "prd" {
				zone = env + "." + apexZone
			}

			require.ErrorIs(t, printDelegationValues(dnsOptions(env, zone), testClient(f), &bytes.Buffer{}), errEnvNotApplicable)
			assert.Empty(t, f.calls)
		})
	}

	t.Run("NS が delegations の形に合わなければ errPrecondition で何も出さない", func(t *testing.T) {
		t.Parallel()

		f := zoneWorld(t, devZone, serveSigning, testKSK(kskActive))
		f.responses[getZoneNS] = nsJSON(zoneNS[:3]...)

		var out bytes.Buffer
		require.ErrorIs(t, printDelegationValues(dnsOptions("dev", devZone), testClient(f), &out), errPrecondition)
		assert.Empty(t, out.String())
	})

	t.Run("DS が delegations の形に合わなければ errPrecondition で何も出さない", func(t *testing.T) {
		t.Parallel()

		ksk := testKSK(kskActive)
		ksk.DSRecord = "70000 13 2 " + strings.Repeat("AB", 32)

		var out bytes.Buffer
		require.ErrorIs(t, printDelegationValues(dnsOptions("dev", devZone), testClient(zoneWorld(t, devZone, serveSigning, ksk)), &out), errPrecondition)
		assert.Empty(t, out.String())
	})

	t.Run("zone が無ければ errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := zoneWorld(t, devZone, serveSigning, testKSK(kskActive))
		f.responses[listZones(devZone)] = ok(`{"HostedZones":[]}`)
		require.ErrorIs(t, printDelegationValues(dnsOptions("dev", devZone), testClient(f), &bytes.Buffer{}), errPrecondition)
	})
}

func Test_checkSigning(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, f *fakeAWS) (string, error) {
		t.Helper()

		var out bytes.Buffer

		err := checkSigning(dnsOptions("dev", devZone), testClient(f), &out)

		return out.String(), err
	}

	t.Run("SIGNING でただ1つの ACTIVE な KSK と要件に合う鍵なら通す", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, zoneWorld(t, devZone, serveSigning, testKSK(kskActive)))
		require.NoError(t, err)
		assert.Contains(t, out, "✅ 署名が SIGNING（今: SIGNING）")
		assert.Contains(t, out, "✅ KSK tabpdev が ACTIVE（今: ACTIVE）")
		assert.Contains(t, out, "✅ KSK の鍵が ECC_NIST_P256")
	})

	t.Run("SIGNING でなければ errNG", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, zoneWorld(t, devZone, "NOT_SIGNING", testKSK(kskActive)))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ 署名が SIGNING（今: NOT_SIGNING）")
	})

	t.Run("KSK が0件なら errNG", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, zoneWorld(t, devZone, "NOT_SIGNING"))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ KSK がただ1つ（今: 0）")
	})

	t.Run("KSK が2つなら errNG", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, zoneWorld(t, devZone, serveSigning, testKSK(kskActive), testKSK(kskActive)))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ KSK がただ1つ（今: 2）")
	})

	t.Run("ACTION_NEEDED なら errNG で、戻す入口を名指す", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, zoneWorld(t, devZone, serveSigning, testKSK(kskActionNeeded)))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ KSK tabpdev が ACTIVE（今: ACTION_NEEDED）。鍵を直してから make setup-dns-zone-ksk-activate で戻します")
	})

	t.Run("鍵が要件に合わなければ errNG", func(t *testing.T) {
		t.Parallel()

		f := zoneWorld(t, devZone, serveSigning, testKSK(kskActive))
		f.responses[describeKey] = ok(`{"KeyMetadata":{"KeyState":"Disabled","KeySpec":"ECC_NIST_P256","KeyUsage":"SIGN_VERIFY"}}`)
		out, err := run(t, f)
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ KSK の鍵が Enabled（今: Disabled）")
	})

	t.Run("宣言に合わない zone は aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		require.ErrorIs(t, checkSigning(dnsOptions("dev", apexZone), testClient(f), &bytes.Buffer{}), errZoneName)
		assert.Empty(t, f.calls)
	})
}

func activateWorld(t *testing.T, ksks ...signingKey) *fakeAWS {
	t.Helper()

	f := zoneWorld(t, devZone, serveSigning, ksks...)
	f.responses[activateWrite] = ok("{}")
	f.onWrite(activateWrite, map[string]awsResult{getDNSSEC: dnssecJSON(serveSigning, testKSK(kskActive))})

	return f
}

func activateClient(f *fakeAWS) *client {
	return testClient(f, "route53 activate-key-signing-key")
}

func Test_activateKSK(t *testing.T) {
	t.Parallel()

	t.Run("ACTION_NEEDED の KSK を有効に戻し、ACTIVE を読み直す", func(t *testing.T) {
		t.Parallel()

		f := activateWorld(t, testKSK(kskActionNeeded))
		term, out := tty("tabp\n")
		require.NoError(t, activateKSK(dnsOptions("dev", devZone), activateClient(f), term))
		assert.Equal(t, []string{activateWrite}, f.writes())
		assert.Contains(t, out.String(), "✅ KSK tabpdev を有効に戻しました")
		assert.Equal(t, getDNSSEC, f.joined()[len(f.joined())-1])
	})

	t.Run("ACTIVE なら確認を求めずに書かない", func(t *testing.T) {
		t.Parallel()

		f := activateWorld(t, testKSK(kskActive))
		term, out := tty("")
		require.NoError(t, activateKSK(dnsOptions("dev", devZone), activateClient(f), term))
		assert.Empty(t, f.writes())
		assert.NotContains(t, out.String(), "project の名前")
	})

	for name, ksks := range map[string][]signingKey{
		"KSK が INACTIVE": {testKSK("INACTIVE")},
		"KSK が DELETING": {testKSK("DELETING")},
		"KSK が0件":        nil,
		"KSK が2件":        {testKSK(kskActionNeeded), testKSK(kskActive)},
	} {
		t.Run(name+"なら errPrecondition で書かない", func(t *testing.T) {
			t.Parallel()

			f := activateWorld(t, ksks...)
			term, _ := tty("tabp\n")
			require.ErrorIs(t, activateKSK(dnsOptions("dev", devZone), activateClient(f), term), errPrecondition)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("鍵が要件に合わなければ errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := activateWorld(t, testKSK(kskActionNeeded))
		f.responses[describeKey] = ok(`{"KeyMetadata":{"KeyState":"Disabled","KeySpec":"ECC_NIST_P256","KeyUsage":"SIGN_VERIFY"}}`)
		term, _ := tty("tabp\n")
		err := activateKSK(dnsOptions("dev", devZone), activateClient(f), term)
		require.ErrorIs(t, err, errPrecondition)
		assert.Contains(t, err.Error(), "Enabled（今: Disabled）")
		assert.Empty(t, f.writes())
	})

	t.Run("鍵が無ければ errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := activateWorld(t, testKSK(kskActionNeeded))
		f.responses[describeKey] = fail(t, "An error occurred (NotFoundException)")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, activateKSK(dnsOptions("dev", devZone), activateClient(f), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := activateWorld(t, testKSK(kskActionNeeded))
		term, _ := tty("x\n")
		require.ErrorIs(t, activateKSK(dnsOptions("dev", devZone), activateClient(f), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("読み直しで ACTIVE に見えなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := activateWorld(t, testKSK(kskActionNeeded))
		f.after = nil
		term, _ := tty("tabp\n")
		err := activateKSK(dnsOptions("dev", devZone), activateClient(f), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "signing-check")
	})

	t.Run("書き込みが失敗すればそのエラーを返す", func(t *testing.T) {
		t.Parallel()

		f := activateWorld(t, testKSK(kskActionNeeded))
		f.responses[activateWrite] = fail(t, "InvalidKMSArn")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, activateKSK(dnsOptions("dev", devZone), activateClient(f), term), errAWSFailed)
	})
}

func Test_readRegistrar(t *testing.T) {
	t.Parallel()

	t.Run("us-east-1 で NS と DS を読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{domainDetail(apexZone): registrarJSON(zoneNS, registrarKey{KeyTag: 1, PublicKey: "k"})})
		got, err := readRegistrar(testClient(f), apexZone)
		require.NoError(t, err)
		assert.Equal(t, registrarDomain{nameServers: zoneNS, keys: []registrarKey{{KeyTag: 1, PublicKey: "k"}}}, got)
		assert.Equal(t, "us-east-1", lastRegion(f.calls[0]))
	})

	t.Run("このアカウントに無いドメインは errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{domainDetail(apexZone): fail(t, "An error occurred (InvalidInput) when calling the GetDomainDetail operation")})
		_, err := readRegistrar(testClient(f), apexZone)
		require.ErrorIs(t, err, errPrecondition)
		assert.Contains(t, err.Error(), "手で行う手順")
	})

	for name, body := range map[string]string{
		"Name の欠けた name server": `{"Nameservers":[{"Name":""}]}`,
		"PublicKey の欠けた DS":     `{"Nameservers":[{"Name":"a"}],"DnssecKeys":[{"KeyTag":1}]}`,
		"object でない":            `"x"`,
	} {
		t.Run(name+"は errAWSResponse", func(t *testing.T) {
			t.Parallel()

			_, err := readRegistrar(testClient(newFakeAWS(t, map[string]awsResult{domainDetail(apexZone): ok(body)})), apexZone)
			require.ErrorIs(t, err, errAWSResponse)
		})
	}

	t.Run("ほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		_, err := readRegistrar(testClient(newFakeAWS(t, map[string]awsResult{domainDetail(apexZone): fail(t, "AccessDenied")})), apexZone)
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_normalizeNS(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"ns-1.awsdns-01.com", "ns-2.awsdns-02.net"}, normalizeNS([]string{"NS-1.awsdns-01.com.", "ns-2.awsdns-02.net"}))
	assert.Empty(t, normalizeNS(nil))
}

func Test_sameNameServers(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		a, b []string
		want bool
	}{
		"順と末尾のドットと大文字の違いを除いて同じ": {[]string{"NS-2.awsdns-02.net.", "ns-1.awsdns-01.com"}, []string{"ns-1.awsdns-01.com", "ns-2.awsdns-02.net"}, true},
		"1つ違う":  {[]string{"ns-1.awsdns-01.com", "ns-3.awsdns-03.org"}, []string{"ns-1.awsdns-01.com", "ns-2.awsdns-02.net"}, false},
		"数が違う":  {[]string{"ns-1.awsdns-01.com"}, []string{"ns-1.awsdns-01.com", "ns-2.awsdns-02.net"}, false},
		"どちらも空": {nil, nil, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, sameNameServers(tc.a, tc.b))
		})
	}
}

func Test_requireApex(t *testing.T) {
	t.Parallel()

	require.NoError(t, requireApex(dnsOptions("prd", apexZone)))
	require.ErrorIs(t, requireApex(dnsOptions("dev", devZone)), errEnvNotApplicable)
}

var oldNS = []string{"ns1.registrar.example", "ns2.registrar.example"}

func registrarWorld(t *testing.T, regNS []string, keys ...registrarKey) *fakeAWS {
	t.Helper()

	f := zoneWorld(t, apexZone, serveSigning, testKSK(kskActive))
	f.responses[domainDetail(apexZone)] = registrarJSON(regNS, keys...)
	f.responses[nsWrite(zoneNS...)] = ok(`{"OperationId":"op"}`)
	f.responses[dsWrite()] = ok(`{"OperationId":"op"}`)
	f.onWrite(nsWrite(zoneNS...), map[string]awsResult{domainDetail(apexZone): registrarJSON(zoneNS, keys...)})
	f.onWrite(dsWrite(), map[string]awsResult{domainDetail(apexZone): registrarJSON(regNS, append(keys, registrarKey{KeyTag: 12345, PublicKey: kskPublicKey})...)})

	return f
}

func registrarClient(f *fakeAWS) *client {
	return testClient(f, "route53domains update-domain-nameservers", "route53domains associate-delegation-signer-to-domain")
}

func Test_updateRegistrarNS(t *testing.T) {
	t.Parallel()

	prd := dnsOptions("prd", apexZone)

	t.Run("registrar の NS を zone の NS に替え、読み直す", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, oldNS)
		term, out := tty("tabp\n")
		require.NoError(t, updateRegistrarNS(prd, registrarClient(f), term))
		assert.Equal(t, []string{nsWrite(zoneNS...)}, f.writes())
		assert.Contains(t, out.String(), "  registrar の NS: ns1.registrar.example・ns2.registrar.example\n")
		assert.Contains(t, out.String(), "✅ registrar の NS を zone の NS に替えました")

		for _, c := range f.calls {
			if op(c) == "route53domains update-domain-nameservers" {
				assert.Equal(t, "us-east-1", lastRegion(c))
			}
		}
	})

	t.Run("末尾のドットと大文字の違いだけなら書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, []string{"NS-4.awsdns-04.co.uk.", "ns-3.awsdns-03.org.", "ns-2.awsdns-02.net.", "ns-1.awsdns-01.com."})
		term, _ := tty("")
		require.NoError(t, updateRegistrarNS(prd, registrarClient(f), term))
		assert.Empty(t, f.writes())
	})

	t.Run("registrar に zone の KSK の DS が在っても替える", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, oldNS, registrarKey{KeyTag: 12345, PublicKey: kskPublicKey})
		term, _ := tty("tabp\n")
		require.NoError(t, updateRegistrarNS(prd, registrarClient(f), term))
		assert.Equal(t, []string{nsWrite(zoneNS...)}, f.writes())
	})

	t.Run("registrar にほかの鍵の DS が在れば errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, oldNS, registrarKey{KeyTag: 999, PublicKey: "other"})
		term, _ := tty("tabp\n")
		err := updateRegistrarNS(prd, registrarClient(f), term)
		require.ErrorIs(t, err, errPrecondition)
		assert.Contains(t, err.Error(), "key tag 999")
		assert.Empty(t, f.writes())
	})

	t.Run("このアカウントに無いドメインは errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, oldNS)
		f.responses[domainDetail(apexZone)] = fail(t, "An error occurred (InvalidInput)")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, updateRegistrarNS(prd, registrarClient(f), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("prd でなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, updateRegistrarNS(dnsOptions("dev", devZone), registrarClient(f), term), errEnvNotApplicable)
		assert.Empty(t, f.calls)
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, oldNS)
		term, _ := tty("x\n")
		require.ErrorIs(t, updateRegistrarNS(prd, registrarClient(f), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("読み直しで替わって見えなければ errAWSResponse で、やり直しを伝える", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, oldNS)
		f.after = nil
		term, _ := tty("tabp\n")
		err := updateRegistrarNS(prd, registrarClient(f), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "make setup-dns-zone-registrar-ns をやり直す")
	})
}

func Test_associateRegistrarDS(t *testing.T) {
	t.Parallel()

	prd := dnsOptions("prd", apexZone)

	t.Run("ACTIVE な KSK の DS を足し、読み直す", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, zoneNS)
		term, out := tty("tabp\n")
		require.NoError(t, associateRegistrarDS(prd, registrarClient(f), term))
		assert.Equal(t, []string{dsWrite()}, f.writes())
		assert.Contains(t, out.String(), "Algorithm 13、Flags 257、key tag 12345")
		assert.Contains(t, out.String(), "早いと名前が引けなくなります")
	})

	t.Run("同じ公開鍵の DS が在れば書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, zoneNS, registrarKey{KeyTag: 12345, PublicKey: kskPublicKey})
		term, _ := tty("")
		require.NoError(t, associateRegistrarDS(prd, registrarClient(f), term))
		assert.Empty(t, f.writes())
	})

	t.Run("ほかの鍵の DS が在っても足し、外さないことを出す", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, zoneNS, registrarKey{KeyTag: 999, PublicKey: "other"})
		term, out := tty("tabp\n")
		require.NoError(t, associateRegistrarDS(prd, registrarClient(f), term))
		assert.Equal(t, []string{dsWrite()}, f.writes())
		assert.Contains(t, out.String(), "key tag 999）が在ります。この入口は外しません")
	})

	t.Run("registrar の NS が zone の NS と違えば errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, oldNS)
		term, _ := tty("tabp\n")
		err := associateRegistrarDS(prd, registrarClient(f), term)
		require.ErrorIs(t, err, errPrecondition)
		assert.Contains(t, err.Error(), "registrar-ns")
		assert.Empty(t, f.writes())
	})

	for name, st := range map[string]awsResult{
		"SIGNING でない":         dnssecJSON("NOT_SIGNING", testKSK(kskActive)),
		"KSK が ACTION_NEEDED": dnssecJSON(serveSigning, testKSK(kskActionNeeded)),
		"KSK が2つ":             dnssecJSON(serveSigning, testKSK(kskActive), testKSK(kskActive)),
	} {
		t.Run(name+"なら errPrecondition で書かない", func(t *testing.T) {
			t.Parallel()

			f := registrarWorld(t, zoneNS)
			f.responses[getDNSSEC] = st
			term, _ := tty("tabp\n")
			require.ErrorIs(t, associateRegistrarDS(prd, registrarClient(f), term), errPrecondition)
			assert.Empty(t, f.writes())
		})
	}

	for name, mutate := range map[string]func(*signingKey){
		"Flag が 256":              func(k *signingKey) { k.Flag = 256 },
		"PublicKey が空":            func(k *signingKey) { k.PublicKey = "" },
		"SigningAlgorithmType が0": func(k *signingKey) { k.SigningAlgorithmType = 0 },
	} {
		t.Run(name+"なら errAWSResponse で書かない", func(t *testing.T) {
			t.Parallel()

			ksk := testKSK(kskActive)
			mutate(&ksk)

			f := registrarWorld(t, zoneNS)
			f.responses[getDNSSEC] = dnssecJSON(serveSigning, ksk)
			term, _ := tty("tabp\n")
			require.ErrorIs(t, associateRegistrarDS(prd, registrarClient(f), term), errAWSResponse)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("prd でなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, associateRegistrarDS(dnsOptions("stg", "stg.example.com"), registrarClient(f), term), errEnvNotApplicable)
		assert.Empty(t, f.calls)
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, zoneNS)
		term, _ := tty("x\n")
		require.ErrorIs(t, associateRegistrarDS(prd, registrarClient(f), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, zoneNS)
		term := newTerminal(strings.NewReader("tabp\n"), &bytes.Buffer{}, false)
		require.ErrorIs(t, associateRegistrarDS(prd, registrarClient(f), term), errNotTerminal)
		assert.Empty(t, f.writes())
	})

	t.Run("読み直しで DS が見えなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := registrarWorld(t, zoneNS)
		f.after = nil
		term, _ := tty("tabp\n")
		err := associateRegistrarDS(prd, registrarClient(f), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "registrar-ds をやり直す")
	})
}
