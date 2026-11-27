// Package main は、CI の run が AWS で拒まれた・失敗した呼び出しを、CloudTrail の event history から引くツール。
// CI の apply と plan の全文は CI の role しか復号できないので、人の手元の診断は CloudTrail が持つ（ADR-0602 決定13）。
// aws だけを起動して読み、何も作らず、何も変えない。資格情報を変える環境変数の外し方と AWS_PROFILE は make が整える。
//
//	ci-trail -run <run ID> -attempt <n> -root-region <region> -regions-file <modules/default-regions/outputs.tf>
//	    [-since <期間>] [-member-profile <profile>] [-raw]
//
// CI の role の session 名は `gha-<run>-<attempt>` である（scripts/tf-local の ci-credentials）。その session の
// AssumeRoleWithWebIdentity の event から一時の access key ID を取り、その key の event を、root の region と
// us-east-1 と ADR-0209 の集合で引く。CI がメンバーへ引き受けた role の key は、-member-profile の資格情報で引く
// （メンバーの role からさらに引き受けた role は追わない）。出力は既定で、許可の文法（scripts/lib/publicdiag）に
// 当たる欄だけの行である。-raw は errorMessage をそのまま出すので、人の端末だけで使う。root の資格情報では引かない
// （ADR-0301 決定19 の最後の手段を日常に使わない）。終了コードは、0 が引けた（拒否が無いことを含む）、2 が引けない
// （引数の誤り、session か event が見えないことを含む）。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/awsenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/publicdiag"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// globalRegion は、IAM・Organizations・STS の global の endpoint の event が記録されるリージョン。
const globalRegion = "us-east-1"

var (
	counterPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	profilePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	keyPattern     = regexp.MustCompile(`^ASIA[A-Z0-9]{12,124}$`)

	errUsage    = xerrors.New("使い方: ci-trail -run <run ID> -attempt <n> -root-region <region> -regions-file <outputs.tf> [-since <期間>] [-member-profile <profile>] [-raw]")
	errAWS      = xerrors.New("❌ aws の呼び出しが失敗しました")
	errEnv      = xerrors.New("❌ 資格情報を変える環境変数が外されていません（make ci-trail から起動してください）")
	errRoot     = xerrors.New("❌ root の資格情報では引きません（読み取り専用の主体の profile を使ってください）")
	errRegions  = xerrors.New("❌ ADR-0209 のリージョンの集合を読めません")
	errSession  = xerrors.New("❌ その run の CI の session が CloudTrail に見つかりません（run ID・attempt・-since と、event history への反映の遅れを確かめてください）")
	errNoEvents = xerrors.New("❌ session の key の event が1件も見えません（event history への反映の遅れを待って、引き直してください）")
	errEvents   = xerrors.New("❌ CloudTrail の応答を読めません")
)

// awsFunc は aws を起動します。profile が空なら AWS_PROFILE の資格情報を使う。
type awsFunc func(profile string, args ...string) ([]byte, error)

type deps struct {
	aws      awsFunc
	now      func() time.Time
	readFile func(string) ([]byte, error)
	getenv   func(string) string
}

type options struct {
	run           string
	attempt       string
	rootRegion    string
	regionsFile   string
	memberProfile string
	since         time.Duration
	raw           bool
}

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, deps{aws: hostAWS("aws"), now: time.Now, readFile: os.ReadFile, getenv: os.Getenv}))
}

// execute は、run の出力を stdout へ書き、終了コードを返します。
func execute(args []string, stdout io.Writer, d deps) int {
	var out bytes.Buffer

	err := run(args, &out, d)
	if err != nil {
		fmt.Fprintln(&out, err)
	}

	if _, werr := io.WriteString(stdout, out.String()); werr != nil || err != nil {
		return 2
	}

	return 0
}

// hostAWS は、bin を子プロセスとして起動する awsFunc を返します。stderr は返す error に入れない（公開の面へ写されうる）。
func hostAWS(bin string) awsFunc {
	return func(profile string, args ...string) ([]byte, error) {
		if profile != "" {
			args = append([]string{"--profile", profile}, args...)
		}

		var stdout bytes.Buffer

		cmd := exec.Command(bin, args...)
		cmd.Stdout = &stdout

		if err := cmd.Run(); err != nil {
			return nil, xerrors.Wrap(errAWS, strings.Join(args[:min(len(args), 3)], " "))
		}

		return stdout.Bytes(), nil
	}
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("ci-trail", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var o options

	fs.StringVar(&o.run, "run", "", "")
	fs.StringVar(&o.attempt, "attempt", "", "")
	fs.StringVar(&o.rootRegion, "root-region", "", "")
	fs.StringVar(&o.regionsFile, "regions-file", "", "")
	fs.StringVar(&o.memberProfile, "member-profile", "", "")
	fs.DurationVar(&o.since, "since", 24*time.Hour, "")
	fs.BoolVar(&o.raw, "raw", false, "")

	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return options{}, errUsage
	}

	switch {
	case !counterPattern.MatchString(o.run), !counterPattern.MatchString(o.attempt), !publicdiag.IsRegion(o.rootRegion),
		o.regionsFile == "", o.since <= 0, o.memberProfile != "" && !profilePattern.MatchString(o.memberProfile):
		return options{}, errUsage
	}

	return o, nil
}

func run(args []string, out io.Writer, d deps) error {
	o, err := parseArgs(args)
	if err != nil {
		return err
	}

	if found := awsenv.Found(d.getenv); len(found) > 0 {
		return xerrors.Wrap(errEnv, strings.Join(found, " "))
	}

	for _, profile := range slices.Compact([]string{"", o.memberProfile}) {
		if err := refuseRoot(d, profile); err != nil {
			return err
		}
	}

	set, err := defaultRegions(d.readFile, o.regionsFile)
	if err != nil {
		return err
	}

	regions := regionList(o.rootRegion, set)
	start := d.now().Add(-o.since).UTC().Format(time.RFC3339)
	session := "gha-" + o.run + "-" + o.attempt

	keys, err := sessionKeys(d, start, session, []string{o.rootRegion, globalRegion})
	if err != nil {
		return err
	}

	if len(keys) == 0 {
		return errSession
	}

	found, err := keyEvents(d, "", regions, start, keys)
	if err != nil {
		return err
	}

	// session の role は plan か apply で必ず何かを呼ぶので、event が1件も見えないのは、反映の遅れか取りこぼしである。
	if found.seen == 0 {
		return errNoEvents
	}

	if o.memberProfile != "" && len(found.members) > 0 {
		more, err := keyEvents(d, o.memberProfile, regions, start, found.members)
		if err != nil {
			return err
		}

		found.failed = append(found.failed, more.failed...)
		found.unread += more.unread
		sortEvents(found.failed)
	}

	return report(out, found, len(keys), o.memberProfile != "", o.raw)
}

func refuseRoot(d deps, profile string) error {
	raw, err := d.aws(profile, "sts", "get-caller-identity", "--output", "json")
	if err != nil {
		return err
	}

	var id struct {
		Arn string `json:"Arn"`
	}
	if err := json.Unmarshal(raw, &id); err != nil || id.Arn == "" {
		return xerrors.Wrap(errEvents, "get-caller-identity")
	}

	if strings.HasSuffix(id.Arn, ":root") {
		return errRoot
	}

	return nil
}

// defaultRegions は、modules/default-regions の output by_partition の aws の literal を読みます。正本を写さず、
// 宣言の形（`toset([...])` の文字列の literal の並び）だけを受け、ほかの形と空の集合を拒む。宣言の形を変えたら、
// ここが読めるかを確かめる。
func defaultRegions(readFile func(string) ([]byte, error), path string) ([]string, error) {
	src, err := readFile(path)
	if err != nil {
		return nil, xerrors.Wrap(errRegions, "読めません")
	}

	file, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrap(errRegions, "HCL を解釈できません")
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, errRegions
	}

	for _, b := range body.Blocks {
		if b.Type != "output" || len(b.Labels) != 1 || b.Labels[0] != "by_partition" {
			continue
		}

		value, ok := b.Body.Attributes["value"]
		if !ok {
			return nil, xerrors.Wrap(errRegions, "value がありません")
		}

		return partitionRegions(value.Expr, "aws")
	}

	return nil, xerrors.Wrap(errRegions, "output by_partition がありません")
}

// partitionRegions は、object の partition の key の `toset([...])` から、リージョンの名前を読みます。
func partitionRegions(expr hclsyntax.Expression, partition string) ([]string, error) {
	obj, ok := expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, xerrors.Wrap(errRegions, "object ではありません")
	}

	for _, item := range obj.Items {
		key, diags := item.KeyExpr.Value(nil)
		if diags.HasErrors() || key.Type() != cty.String || key.AsString() != partition {
			continue
		}

		call, ok := item.ValueExpr.(*hclsyntax.FunctionCallExpr)
		if !ok || call.Name != "toset" || len(call.Args) != 1 {
			return nil, xerrors.Wrap(errRegions, "toset([...]) ではありません")
		}

		tuple, ok := call.Args[0].(*hclsyntax.TupleConsExpr)
		if !ok || len(tuple.Exprs) == 0 {
			return nil, xerrors.Wrap(errRegions, "空か、一覧ではありません")
		}

		regions := make([]string, 0, len(tuple.Exprs))

		for _, e := range tuple.Exprs {
			v, diags := e.Value(nil)
			if diags.HasErrors() || v.Type() != cty.String || !publicdiag.IsRegion(v.AsString()) {
				return nil, xerrors.Wrap(errRegions, "リージョンの名前の文字列でない要素があります")
			}

			regions = append(regions, v.AsString())
		}

		return regions, nil
	}

	return nil, xerrors.Wrap(errRegions, partition+" の key がありません")
}

// regionList は、root の region と us-east-1 と集合を、重ねずに並べます。
func regionList(root string, set []string) []string {
	regions := []string{root}

	for _, r := range append([]string{globalRegion}, set...) {
		if !slices.Contains(regions, r) {
			regions = append(regions, r)
		}
	}

	return regions
}

// trailEvent は、CloudTrailEvent の JSON のうち読む欄だけです。requestParameters と responseElements は event ごとに
// 形が違うので、要るときだけ読む。
type trailEvent struct {
	EventTime         string          `json:"eventTime"`
	AWSRegion         string          `json:"awsRegion"`
	EventSource       string          `json:"eventSource"`
	EventName         string          `json:"eventName"`
	ErrorCode         string          `json:"errorCode"`
	ErrorMessage      string          `json:"errorMessage"`
	RequestParameters json.RawMessage `json:"requestParameters"`
	ResponseElements  json.RawMessage `json:"responseElements"`
}

// lookup は、1つのリージョンで1つの属性の event を引きます。aws CLI はページを読み切って1つの応答にまとめる。
// 形の読めない CloudTrailEvent は落とさず、unread に数える。
func lookup(d deps, profile, region, key, value, start string) ([]trailEvent, int, error) {
	raw, err := d.aws(profile, "cloudtrail", "lookup-events", "--region", region,
		"--lookup-attributes", "AttributeKey="+key+",AttributeValue="+value, "--start-time", start, "--output", "json")
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		Events *[]struct {
			CloudTrailEvent string `json:"CloudTrailEvent"`
		} `json:"Events"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Events == nil {
		return nil, 0, xerrors.Wrap(errEvents, region)
	}

	events := make([]trailEvent, 0, len(*resp.Events))
	unread := 0

	for _, e := range *resp.Events {
		var ev trailEvent
		if err := json.Unmarshal([]byte(e.CloudTrailEvent), &ev); err != nil {
			unread++

			continue
		}

		events = append(events, ev)
	}

	return events, unread, nil
}

// sessionKeys は、session の AssumeRoleWithWebIdentity の event から、一時の access key ID を集めます。
func sessionKeys(d deps, start, session string, regions []string) ([]string, error) {
	var keys []string

	for _, region := range regions {
		events, _, err := lookup(d, "", region, "EventName", "AssumeRoleWithWebIdentity", start)
		if err != nil {
			return nil, err
		}

		for _, ev := range events {
			if ev.sessionName() != session {
				continue
			}

			if k := ev.accessKey(); k != "" && !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}

	return keys, nil
}

// sessionName は、要求の roleSessionName を返します。読めなければ空文字を返す。
func (ev trailEvent) sessionName() string {
	var p struct {
		RoleSessionName string `json:"roleSessionName"`
	}
	if json.Unmarshal(ev.RequestParameters, &p) != nil {
		return ""
	}

	return p.RoleSessionName
}

// accessKey は、応答の一時の access key ID を返します。読めないか形が違えば空文字を返す。
func (ev trailEvent) accessKey() string {
	var r struct {
		Credentials struct {
			AccessKeyID string `json:"accessKeyId"`
		} `json:"credentials"`
	}
	if json.Unmarshal(ev.ResponseElements, &r) != nil || !keyPattern.MatchString(r.Credentials.AccessKeyID) {
		return ""
	}

	return r.Credentials.AccessKeyID
}

// keyFindings は、key の event を引いた結果です。
type keyFindings struct {
	// failed は失敗した event、members は成功した AssumeRole が返した key（メンバーへの引き受け）。
	failed  []trailEvent
	members []string
	// seen は読めた event の数、unread は読めなかった event の数。
	seen   int
	unread int
}

// keyEvents は、key ごと・リージョンごとに event を引きます。
func keyEvents(d deps, profile string, regions []string, start string, keys []string) (keyFindings, error) {
	var f keyFindings

	for _, key := range keys {
		for _, region := range regions {
			events, unread, err := lookup(d, profile, region, "AccessKeyId", key, start)
			if err != nil {
				return keyFindings{}, err
			}

			f.seen += len(events)
			f.unread += unread

			for _, ev := range events {
				switch {
				case ev.ErrorCode != "":
					f.failed = append(f.failed, ev)
				case ev.EventName == "AssumeRole":
					if k := ev.accessKey(); k != "" && !slices.Contains(f.members, k) {
						f.members = append(f.members, k)
					}
				}
			}
		}
	}

	sortEvents(f.failed)

	return f, nil
}

// sortEvents は、event を時刻の順に並べます。拒否の時刻を権限の更新の時刻と比べるのに使う。
func sortEvents(events []trailEvent) {
	slices.SortStableFunc(events, func(a, b trailEvent) int { return strings.Compare(a.EventTime, b.EventTime) })
}

// report は、失敗した event を1行ずつ出します。既定は許可の文法に当たる欄だけで、raw は errorMessage をそのまま出す。
func report(out io.Writer, f keyFindings, keys int, memberChecked, raw bool) error {
	if raw {
		fmt.Fprintln(out, "⚠️ -raw の出力は人の端末だけで読み、会話・issue・Pull Request へ写さない")
	}

	for _, ev := range f.failed {
		if raw {
			fmt.Fprintf(out, "%s %s %s %s %s %s\n", ev.EventTime, ev.AWSRegion, ev.EventSource, ev.EventName, ev.ErrorCode, ev.ErrorMessage)

			continue
		}

		fmt.Fprintln(out, eventLine(ev))
	}

	if len(f.members) > 0 && !memberChecked {
		fmt.Fprintf(out, "⚠️ メンバーの key %d 件は、-member-profile が無いので引いていません\n", len(f.members))
	}

	if f.unread > 0 {
		fmt.Fprintf(out, "⚠️ 形の読めない event %d 件は数えていません\n", f.unread)
	}

	_, err := fmt.Fprintf(out, "CloudTrail: session の key %d 件、メンバーの key %d 件、読めた event %d 件、失敗した event %d 件\n",
		keys, len(f.members), f.seen, len(f.failed))

	return err
}

// eventLine は、失敗した event を公開の文法の1行にします。
func eventLine(ev trailEvent) string {
	fields := []publicdiag.Field{{Name: "kind", Value: "api-error"}}

	denied := publicdiag.Deny(ev.ErrorMessage)
	if denied != nil {
		fields[0].Value = "iam-deny"
	}

	fields = append(fields,
		publicdiag.Field{Name: "time", Value: ev.EventTime},
		publicdiag.Field{Name: "region", Value: ev.AWSRegion},
		publicdiag.Field{Name: "source", Value: ev.EventSource},
		publicdiag.Field{Name: "operation", Value: ev.EventName},
		publicdiag.Field{Name: "code", Value: ev.ErrorCode},
	)

	line, masked := publicdiag.Format(append(fields, denied...)...)
	if masked > 0 {
		extra, _ := publicdiag.Format(publicdiag.Field{Name: "masked", Value: fmt.Sprint(masked)})
		line += "; " + strings.TrimPrefix(extra, publicdiag.Prefix)
	}

	return line
}
