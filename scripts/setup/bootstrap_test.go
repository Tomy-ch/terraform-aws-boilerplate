package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	callerUser   = `{"Account":"111111111111","Arn":"arn:aws:iam::111111111111:user/bootstrap-temp"}`
	getContact   = "account get-contact-information"
	putContact   = "account put-contact-information"
	putAlternate = "account put-alternate-contact"
	monitorARN   = "arn:aws:ce::111111111111:anomalymonitor/m"
	subARN       = "arn:aws:ce::111111111111:anomalysubscription/s"
	tagWrite     = "ce tag-resource"
	rnfError     = "An error occurred (ResourceNotFoundException)"
)

func getAlternate(typ string) string {
	return "account get-alternate-contact --alternate-contact-type " + typ
}

func listCostTags(arn string) string { return "ce list-tags-for-resource --resource-arn " + arn }

func tagResource(arn string) string {
	return tagWrite + " --resource-arn " + arn + " --resource-tags Key=tabp:bootstrap,Value=true"
}

var (
	wantContact = contactInformation{
		FullName: "Taro", AddressLine1: "1-1", City: "Chiyoda", StateOrRegion: "Tokyo", PostalCode: "100-0001", CountryCode: "JP", PhoneNumber: "+81312345678",
	}
	wantAlt = alternateContact{Name: "Hanako", Title: "Ops", EmailAddress: "ops@example.com", PhoneNumber: "+81312345679"}
)

// contactInput は、主の連絡先の入力（会社名と住所2は空）。
const contactInput = "Taro\n\n1-1\n\nChiyoda\nTokyo\n100-0001\nJP\n+81312345678\n"

// alternateInput は、BILLING を入力し、OPERATIONS と SECURITY を空にして同じにする入力。
const alternateInput = "Hanako\nOps\nops@example.com\n+81312345679\n\n\n"

func contactJSON(ci contactInformation) awsResult {
	b, _ := json.Marshal(map[string]any{"ContactInformation": ci})

	return ok(string(b))
}

func alternateJSON(a alternateContact) awsResult {
	b, _ := json.Marshal(map[string]any{"AlternateContact": a})

	return ok(string(b))
}

// contactsWorld は、連絡先が無い世界。書き込みの後は入力どおりの連絡先が在る。
func contactsWorld(t *testing.T) *fakeAWS {
	t.Helper()

	m := map[string]awsResult{
		"sts get-caller-identity": ok(callerUser),
		getContact:                fail(t, rnfError),
		putContact:                ok(""),
		putAlternate:              ok(""),
	}
	after := map[string]awsResult{getContact: contactJSON(wantContact)}

	for _, typ := range alternateTypes {
		m[getAlternate(typ)] = fail(t, rnfError)
		after[getAlternate(typ)] = alternateJSON(wantAlt)
	}

	f := newFakeAWS(t, m)
	f.onWrite(putContact, after)
	f.onWrite(putAlternate, after)

	return f
}

func Test_requireBootstrapUser(t *testing.T) {
	t.Parallel()

	t.Run("IAM user なら書き込む先を出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, requireBootstrapUser(testClient(newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": ok(callerUser)})), "tabp-bootstrap", &out))
		assert.Equal(t, "# 書き込む先: profile tabp-bootstrap、bootstrap の一時 IAM user\n", out.String())
	})

	for name, arn := range map[string]string{
		"root なら errPrecondition": "arn:aws:iam::111111111111:root",
		"role なら errPrecondition": "arn:aws:sts::111111111111:assumed-role/x/y",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			f := newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": ok(`{"Arn":"` + arn + `"}`)})
			require.ErrorIs(t, requireBootstrapUser(testClient(f), "p", &out), errPrecondition)
			assert.Empty(t, out.String())
		})
	}

	t.Run("ARN が無ければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": ok(`{}`)})
		require.ErrorIs(t, requireBootstrapUser(testClient(f), "p", &bytes.Buffer{}), errAWSResponse)
	})
}

func Test_readContact(t *testing.T) {
	t.Parallel()

	t.Run("在れば返し、ResourceNotFoundException なら無い", func(t *testing.T) {
		t.Parallel()

		got, found, err := readContact(testClient(newFakeAWS(t, map[string]awsResult{getContact: contactJSON(wantContact)})))
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wantContact, got)

		_, found, err = readContact(testClient(newFakeAWS(t, map[string]awsResult{getContact: fail(t, rnfError)})))
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("ほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		_, _, err := readContact(testClient(newFakeAWS(t, map[string]awsResult{getContact: fail(t, "AccessDenied")})))
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_readAlternate(t *testing.T) {
	t.Parallel()

	t.Run("在れば返し、ResourceNotFoundException なら無い", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getAlternate("BILLING"): alternateJSON(wantAlt), getAlternate("SECURITY"): fail(t, rnfError)})

		got, found, err := readAlternate(testClient(f), "BILLING")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, wantAlt, got)

		_, found, err = readAlternate(testClient(f), "SECURITY")
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func Test_askContact(t *testing.T) {
	t.Parallel()

	t.Run("入力を読み、空の任意の項目は空のまま", func(t *testing.T) {
		t.Parallel()

		term, _ := tty(contactInput)
		got, err := askContact(term)
		require.NoError(t, err)
		assert.Equal(t, wantContact, got)
	})

	for name, input := range map[string]string{
		"氏名が空なら errContactInput":           "\n\n1-1\n\nChiyoda\nTokyo\n100-0001\nJP\n+81312345678\n",
		"国が小文字なら errContactInput":          "Taro\n\n1-1\n\nChiyoda\nTokyo\n100-0001\njp\n+81312345678\n",
		"電話番号が + で始まらなければ errContactInput": "Taro\n\n1-1\n\nChiyoda\nTokyo\n100-0001\nJP\n0312345678\n",
		"電話番号に空白が在れば errContactInput":      "Taro\n\n1-1\n\nChiyoda\nTokyo\n100-0001\nJP\n+81 312345678\n",
		"JP で都道府県が空なら errContactInput":     "Taro\n\n1-1\n\nChiyoda\n\n100-0001\nJP\n+81312345678\n",
		"氏名が51文字なら errContactInput":        strings.Repeat("a", 51) + "\n\n1-1\n\nChiyoda\nTokyo\n100-0001\nJP\n+81312345678\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			term, _ := tty(input)
			_, err := askContact(term)
			require.ErrorIs(t, err, errContactInput)
		})
	}

	t.Run("氏名が50文字なら受ける", func(t *testing.T) {
		t.Parallel()

		term, _ := tty(strings.Repeat("a", 50) + "\n\n1-1\n\nChiyoda\nTokyo\n100-0001\nJP\n+81312345678\n")
		_, err := askContact(term)
		require.NoError(t, err)
	})

	t.Run("都道府県が要らない国なら空でよい", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("Taro\n\n1-1\n\nParis\n\n75001\nFR\n+33112345678\n")
		got, err := askContact(term)
		require.NoError(t, err)
		assert.Empty(t, got.StateOrRegion)
	})

	t.Run("入力が尽きれば errNoInput", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("Taro\n")
		_, err := askContact(term)
		require.ErrorIs(t, err, errNoInput)
	})
}

func Test_askAlternate(t *testing.T) {
	t.Parallel()

	t.Run("4つの項目を読む", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("Hanako\nOps\nops@example.com\n+81312345679\n")
		got, err := askAlternate(term, "BILLING", nil)
		require.NoError(t, err)
		assert.Equal(t, wantAlt, got)
	})

	t.Run("1つ前が在り、氏名が空なら1つ前と同じ", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("\n")
		prev := wantAlt
		got, err := askAlternate(term, "OPERATIONS", &prev)
		require.NoError(t, err)
		assert.Equal(t, wantAlt, got)
	})

	for name, input := range map[string]string{
		"1つ前が無く氏名が空なら errContactInput": "\nOps\nops@example.com\n+81312345679\n",
		"役職が空なら errContactInput":       "Hanako\n\nops@example.com\n+81312345679\n",
		"メールの形でなければ errContactInput":   "Hanako\nOps\nops\n+81312345679\n",
		"電話番号に文字が在れば errContactInput":  "Hanako\nOps\nops@example.com\nabc\n",
		"電話番号が26文字なら errContactInput":  "Hanako\nOps\nops@example.com\n" + strings.Repeat("1", 26) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			term, _ := tty(input)
			_, err := askAlternate(term, "BILLING", nil)
			require.ErrorIs(t, err, errContactInput)
		})
	}

	t.Run("電話番号が25文字なら受ける", func(t *testing.T) {
		t.Parallel()

		term, _ := tty("Hanako\nOps\nops@example.com\n" + strings.Repeat("1", 25) + "\n")
		_, err := askAlternate(term, "BILLING", nil)
		require.NoError(t, err)
	})
}

func Test_setContacts(t *testing.T) {
	t.Parallel()

	t.Run("無い連絡先を、入力から一時ファイルで置き、読み直す", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		term, out := tty(contactInput + alternateInput + "tabp\n")

		require.NoError(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term))
		assert.Equal(t, []string{putContact, putAlternate, putAlternate, putAlternate}, f.writes())
		require.Len(t, f.secrets, 4)
		assert.Contains(t, f.secrets[0], `"FullName":"Taro"`)
		assert.Contains(t, f.secrets[1], `"AlternateContactType":"BILLING"`)
		assert.Contains(t, f.secrets[3], `"AlternateContactType":"SECURITY"`)
		assert.NotContains(t, strings.Join(f.joined(), " "), "Taro")
		assert.Contains(t, out.String(), "✅ 連絡先を置きました")
	})

	t.Run("入力と同じなら、確認を求めず書かない", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		f.responses[getContact] = contactJSON(wantContact)

		for _, typ := range alternateTypes {
			f.responses[getAlternate(typ)] = alternateJSON(wantAlt)
		}

		term, out := tty(contactInput + alternateInput)
		require.NoError(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "書き込みません")
	})

	t.Run("違う代替連絡先だけを書く", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		f.responses[getContact] = contactJSON(wantContact)
		f.responses[getAlternate("BILLING")] = alternateJSON(wantAlt)
		f.responses[getAlternate("OPERATIONS")] = alternateJSON(wantAlt)

		term, _ := tty(contactInput + alternateInput + "tabp\n")
		require.NoError(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term))
		assert.Equal(t, []string{putAlternate}, f.writes())
		assert.Contains(t, f.secrets[0], `"AlternateContactType":"SECURITY"`)
	})

	t.Run("確認が project の名前でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		term, _ := tty(contactInput + alternateInput + "x\n")
		require.ErrorIs(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("入力の形が誤りなら書かない", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		term, _ := tty("\n")
		require.ErrorIs(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term), errContactInput)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ、入力を読まずに errNotTerminal", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		term := newTerminal(strings.NewReader(contactInput+alternateInput+"tabp\n"), &bytes.Buffer{}, false)
		require.ErrorIs(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term), errNotTerminal)
		assert.Empty(t, f.writes())
	})

	t.Run("今の連絡先を読めなければ errAWSFailed で書かない", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		f.responses[getAlternate("OPERATIONS")] = fail(t, "AccessDenied")
		term, _ := tty(contactInput + alternateInput + "tabp\n")
		require.ErrorIs(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term), errAWSFailed)
		assert.Empty(t, f.writes())
	})

	t.Run("書き込みが拒まれれば errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := contactsWorld(t)
		f.responses[putContact] = fail(t, "ValidationException")
		term, _ := tty(contactInput + alternateInput + "tabp\n")
		require.ErrorIs(t, setContacts(bgOptions(), testClient(f, putContact, putAlternate), term), errAWSFailed)
		assert.Equal(t, []string{putContact}, f.writes())
	})
}

func Test_verifyContacts(t *testing.T) {
	t.Parallel()

	alts := map[string]alternateContact{"BILLING": wantAlt, "OPERATIONS": wantAlt, "SECURITY": wantAlt}

	t.Run("読み直して一致すれば ✅", func(t *testing.T) {
		t.Parallel()

		m := map[string]awsResult{getContact: contactJSON(wantContact)}
		for _, typ := range alternateTypes {
			m[getAlternate(typ)] = alternateJSON(wantAlt)
		}

		var out bytes.Buffer
		require.NoError(t, verifyContacts(bgOptions(), testClient(newFakeAWS(t, m)), &out, wantContact, alts))
		assert.Equal(t, "✅ 連絡先を置きました\n", out.String())
	})

	t.Run("読み直しても一致しなければ errAWSResponse で確認の入口を案内する", func(t *testing.T) {
		t.Parallel()

		other := wantContact
		other.City = "Minato"

		m := map[string]awsResult{getContact: contactJSON(other)}
		for _, typ := range alternateTypes {
			m[getAlternate(typ)] = alternateJSON(wantAlt)
		}

		var out bytes.Buffer

		err := verifyContacts(bgOptions(), testClient(newFakeAWS(t, m)), &out, wantContact, alts)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "make bootstrap-check-account")
		assert.Empty(t, out.String())
	})

	t.Run("代替連絡先が一致しなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		m := map[string]awsResult{getContact: contactJSON(wantContact)}
		for _, typ := range alternateTypes {
			m[getAlternate(typ)] = alternateJSON(wantAlt)
		}

		m[getAlternate("SECURITY")] = fail(t, rnfError)

		err := verifyContacts(bgOptions(), testClient(newFakeAWS(t, m)), &bytes.Buffer{}, wantContact, alts)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "SECURITY")
	})
}

// anomalyWorld は、AWS services monitor とその subscription が1つずつ在り、tag の有無が tagged の世界。
func anomalyWorld(t *testing.T, tagged map[string]bool) *fakeAWS {
	t.Helper()

	tags := func(has bool) awsResult {
		if has {
			return ok(`{"ResourceTags":[{"Key":"tabp:bootstrap","Value":"true"}]}`)
		}

		return ok(`{"ResourceTags":[]}`)
	}

	m := map[string]awsResult{
		"sts get-caller-identity":      ok(callerUser),
		"ce get-anomaly-monitors":      ok(`{"AnomalyMonitors":[{"MonitorArn":"` + monitorARN + `","MonitorDimension":"SERVICE"},{"MonitorArn":"x","MonitorType":"CUSTOM"}]}`),
		"ce get-anomaly-subscriptions": ok(`{"AnomalySubscriptions":[{"SubscriptionArn":"` + subARN + `","MonitorArnList":["` + monitorARN + `"]}]}`),
		listCostTags(monitorARN):       tags(tagged[monitorARN]),
		listCostTags(subARN):           tags(tagged[subARN]),
		tagResource(monitorARN):        ok(""),
		tagResource(subARN):            ok(""),
	}

	f := newFakeAWS(t, m)
	f.onWrite(tagResource(monitorARN), map[string]awsResult{listCostTags(monitorARN): tags(true)})
	f.onWrite(tagResource(subARN), map[string]awsResult{listCostTags(subARN): tags(true)})

	return f
}

func Test_anomalyTargets(t *testing.T) {
	t.Parallel()

	t.Run("monitor と subscription を us-east-1 で読んで返す", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, nil)
		monitor, sub, err := anomalyTargets(testClient(f))
		require.NoError(t, err)
		assert.Equal(t, monitorARN, monitor)
		assert.Equal(t, subARN, sub)

		for _, c := range f.calls {
			assert.Equal(t, costRegion, c[len(c)-1])
		}
	})

	for name, tc := range map[string]struct{ key, stdout, want string }{
		"monitor が無ければ errPrecondition で 2-2 を案内する": {"ce get-anomaly-monitors", `{"AnomalyMonitors":[]}`, "（2-2）"},
		"subscription が無ければ errPrecondition":        {"ce get-anomaly-subscriptions", `{"AnomalySubscriptions":[]}`, "subscription が1つではない（0 個）"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := anomalyWorld(t, nil)
			f.responses[tc.key] = ok(tc.stdout)
			_, _, err := anomalyTargets(testClient(f))
			require.ErrorIs(t, err, errPrecondition)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	t.Run("読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, nil)
		f.responses["ce get-anomaly-monitors"] = fail(t, "AccessDenied")
		_, _, err := anomalyTargets(testClient(f))
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_hasCostTag(t *testing.T) {
	t.Parallel()

	t.Run("key が在れば true、無ければ false（値は見ない）", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{
			listCostTags("a"): ok(`{"ResourceTags":[{"Key":"tabp:bootstrap","Value":"no"}]}`),
			listCostTags("b"): ok(`{"ResourceTags":[{"Key":"other","Value":"true"}]}`),
		})

		yes, err := hasCostTag(testClient(f), "a", "tabp:bootstrap")
		require.NoError(t, err)
		assert.True(t, yes)

		no, err := hasCostTag(testClient(f), "b", "tabp:bootstrap")
		require.NoError(t, err)
		assert.False(t, no)
	})

	t.Run("ResourceTags が無ければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listCostTags("a"): ok(`{}`)})
		_, err := hasCostTag(testClient(f), "a", "tabp:bootstrap")
		require.ErrorIs(t, err, errAWSResponse)
	})
}

func Test_tagAnomaly(t *testing.T) {
	t.Parallel()

	t.Run("tag の無いほうにだけ付け、読み直す", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, map[string]bool{monitorARN: true})
		term, out := tty("tabp\n")
		require.NoError(t, tagAnomaly(bgOptions(), testClient(f, tagWrite), term))
		assert.Equal(t, []string{tagResource(subARN)}, f.writes())
		assert.Contains(t, out.String(), "✅ tag を付けました")
	})

	t.Run("どちらにも在れば、確認を求めず書かない", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, map[string]bool{monitorARN: true, subARN: true})
		term, out := tty("")
		require.NoError(t, tagAnomaly(bgOptions(), testClient(f, tagWrite), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "書き込みません")
	})

	t.Run("確認が project の名前でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, nil)
		term, _ := tty("x\n")
		require.ErrorIs(t, tagAnomaly(bgOptions(), testClient(f, tagWrite), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("monitor が決まらなければ errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, nil)
		f.responses["ce get-anomaly-monitors"] = ok(`{"AnomalyMonitors":[]}`)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, tagAnomaly(bgOptions(), testClient(f, tagWrite), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("付けた後に tag が見えなければ errAWSResponse で確認の入口を案内する", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, map[string]bool{monitorARN: true})
		f.after = nil
		term, out := tty("tabp\n")

		err := tagAnomaly(bgOptions(), testClient(f, tagWrite), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "make bootstrap-check-account")
		assert.NotContains(t, out.String(), "✅ tag を付けました")
	})

	t.Run("tag の key は project から組む", func(t *testing.T) {
		t.Parallel()

		f := anomalyWorld(t, map[string]bool{monitorARN: true})
		o := bgOptions()
		o.project = "acme"
		maps.Copy(f.responses, map[string]awsResult{
			listCostTags(monitorARN): ok(`{"ResourceTags":[{"Key":"acme:bootstrap","Value":"true"}]}`),
			listCostTags(subARN):     ok(`{"ResourceTags":[]}`),
			tagWrite + " --resource-arn " + subARN + " --resource-tags Key=acme:bootstrap,Value=true": ok(""),
		})
		f.onWrite(tagWrite+" --resource-arn "+subARN+" --resource-tags Key=acme:bootstrap,Value=true",
			map[string]awsResult{listCostTags(subARN): ok(`{"ResourceTags":[{"Key":"acme:bootstrap","Value":"true"}]}`)})

		term, _ := tty("acme\n")
		require.NoError(t, tagAnomaly(o, testClient(f, tagWrite), term))
		assert.Equal(t, []string{tagWrite + " --resource-arn " + subARN + " --resource-tags Key=acme:bootstrap,Value=true"}, f.writes())
	})
}
