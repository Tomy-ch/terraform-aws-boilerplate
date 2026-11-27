// Package main は、CI の run が AWS で拒まれた・失敗した呼び出しを、CloudTrail の event history から引くツール。
// CI の apply と plan の全文は CI の role しか復号できないので、人の手元の診断は CloudTrail が持つ（ADR-0602 決定13）。
// aws だけを起動して読み、何も作らず、何も変えない。資格情報を変える環境変数の外し方と AWS_PROFILE は make が整える。
//
//	ci-trail -run <run ID> -attempt <n> -root-region <region> -regions-file <modules/default-regions/outputs.tf>
//	    [-since <期間>] [-member-profile <profile>] [-raw]
//
// CI の role の session 名は `gha-<run>-<attempt>` である（scripts/tf-local の ci-credentials）。その session の
// AssumeRoleWithWebIdentity の event から一時の access key ID を取り、その key の event を、root の region と
// us-east-1 と ADR-0209 の集合で引く。CI がメンバーへ引き受けた role の key は、-member-profile の資格情報で引く。
// 出力は既定で、許可の文法（scripts/lib/publicdiag）に当たる欄だけの行である。-raw は errorMessage をそのまま出す
// ので、人の端末だけで使う。管理アカウントの root の資格情報では引かない（ADR-0301 決定19 の最後の手段を日常に
// 使わない）。終了コードは、0 が引けた（拒否が無いことを含む）、2 が引けない（引数の誤りを含む）。
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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/publicdiag"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// globalRegion は、IAM・Organizations・STS の global の endpoint の event が記録されるリージョン。
const globalRegion = "us-east-1"

var (
	counterPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(?:-gov|-iso[a-z]?)?-[a-z]+-[0-9]$`)
	profilePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	keyPattern     = regexp.MustCompile(`^ASIA[A-Z0-9]{12,124}$`)

	errUsage   = xerrors.New("使い方: ci-trail -run <run ID> -attempt <n> -root-region <region> -regions-file <outputs.tf> [-since <期間>] [-member-profile <profile>] [-raw]")
	errAWS     = xerrors.New("❌ aws の呼び出しが失敗しました")
	errRoot    = xerrors.New("❌ root の資格情報では引きません（IAM Identity Center の permission set か、一時 IAM user の profile を使ってください）")
	errRegions = xerrors.New("❌ ADR-0209 のリージョンの集合を読めません")
	errSession = xerrors.New("❌ その run の CI の session が CloudTrail に見つかりません（run ID・attempt・-since と、event history への反映の遅れを確かめてください）")
	errEvents  = xerrors.New("❌ CloudTrail の応答を読めません")
)

// awsFunc は aws を起動します。profile が空なら AWS_PROFILE の資格情報を使う。
type awsFunc func(profile string, args ...string) ([]byte, error)

type deps struct {
	aws      awsFunc
	now      func() time.Time
	readFile func(string) ([]byte, error)
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
	os.Exit(execute(os.Args[1:], os.Stdout, deps{aws: hostAWS("aws"), now: time.Now, readFile: os.ReadFile}))
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
	case !counterPattern.MatchString(o.run), !counterPattern.MatchString(o.attempt), !regionPattern.MatchString(o.rootRegion),
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

	if err := refuseRoot(d); err != nil {
		return err
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

	events, members, err := keyEvents(d, "", regions, start, keys)
	if err != nil {
		return err
	}

	if o.memberProfile != "" && len(members) > 0 {
		more, _, err := keyEvents(d, o.memberProfile, regions, start, members)
		if err != nil {
			return err
		}

		events = append(events, more...)
	}

	return report(out, events, len(keys), len(members), o.raw)
}

// refuseRoot は、呼び出し元が root なら失敗します。
func refuseRoot(d deps) error {
	raw, err := d.aws("", "sts", "get-caller-identity", "--output", "json")
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
// 宣言の形（`toset([...])` の文字列の literal の並び）だけを受け、ほかの形と空の集合を拒む。
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
			if diags.HasErrors() || v.Type() != cty.String || !regionPattern.MatchString(v.AsString()) {
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

// trailEvent は、CloudTrailEvent の JSON のうち読む欄だけです。
type trailEvent struct {
	EventTime         string            `json:"eventTime"`
	AWSRegion         string            `json:"awsRegion"`
	EventSource       string            `json:"eventSource"`
	EventName         string            `json:"eventName"`
	ErrorCode         string            `json:"errorCode"`
	ErrorMessage      string            `json:"errorMessage"`
	RequestParameters map[string]any    `json:"requestParameters"`
	ResponseElements  *responseElements `json:"responseElements"`
}

type responseElements struct {
	Credentials *struct {
		AccessKeyID string `json:"accessKeyId"`
	} `json:"credentials"`
}

// lookup は、1つのリージョンで1つの属性の event を引きます。aws CLI はページを読み切って1つの応答にまとめる。
func lookup(d deps, profile, region, key, value, start string) ([]trailEvent, error) {
	raw, err := d.aws(profile, "cloudtrail", "lookup-events", "--region", region,
		"--lookup-attributes", "AttributeKey="+key+",AttributeValue="+value, "--start-time", start, "--output", "json")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Events *[]struct {
			CloudTrailEvent string `json:"CloudTrailEvent"`
		} `json:"Events"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Events == nil {
		return nil, xerrors.Wrap(errEvents, region)
	}

	events := make([]trailEvent, 0, len(*resp.Events))

	for _, e := range *resp.Events {
		var ev trailEvent
		if err := json.Unmarshal([]byte(e.CloudTrailEvent), &ev); err != nil {
			return nil, xerrors.Wrap(errEvents, region+" の CloudTrailEvent")
		}

		events = append(events, ev)
	}

	return events, nil
}

// sessionKeys は、session の AssumeRoleWithWebIdentity の event から、一時の access key ID を集めます。
func sessionKeys(d deps, start, session string, regions []string) ([]string, error) {
	var keys []string

	for _, region := range regions {
		events, err := lookup(d, "", region, "EventName", "AssumeRoleWithWebIdentity", start)
		if err != nil {
			return nil, err
		}

		for _, ev := range events {
			if name, _ := ev.RequestParameters["roleSessionName"].(string); name != session {
				continue
			}

			if k := ev.accessKey(); k != "" && !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}

	return keys, nil
}

// accessKey は、応答の一時の access key ID を返します。形が違えば空文字を返す。
func (ev trailEvent) accessKey() string {
	if ev.ResponseElements == nil || ev.ResponseElements.Credentials == nil || !keyPattern.MatchString(ev.ResponseElements.Credentials.AccessKeyID) {
		return ""
	}

	return ev.ResponseElements.Credentials.AccessKeyID
}

// keyEvents は、key ごと・リージョンごとに event を引き、失敗した event と、成功した AssumeRole が返した key
// （メンバーへの引き受け）を返します。
func keyEvents(d deps, profile string, regions []string, start string, keys []string) ([]trailEvent, []string, error) {
	var (
		failed  []trailEvent
		members []string
	)

	for _, key := range keys {
		for _, region := range regions {
			events, err := lookup(d, profile, region, "AccessKeyId", key, start)
			if err != nil {
				return nil, nil, err
			}

			for _, ev := range events {
				switch {
				case ev.ErrorCode != "":
					failed = append(failed, ev)
				case ev.EventName == "AssumeRole":
					if k := ev.accessKey(); k != "" && !slices.Contains(members, k) {
						members = append(members, k)
					}
				}
			}
		}
	}

	slices.SortStableFunc(failed, func(a, b trailEvent) int { return strings.Compare(a.EventTime, b.EventTime) })

	return failed, members, nil
}

// report は、失敗した event を1行ずつ出します。既定は許可の文法に当たる欄だけで、raw は errorMessage をそのまま出す。
func report(out io.Writer, events []trailEvent, keys, members int, raw bool) error {
	if raw {
		fmt.Fprintln(out, "⚠️ -raw の出力は人の端末だけで読み、会話・issue・Pull Request へ写さない")
	}

	for _, ev := range events {
		if raw {
			fmt.Fprintf(out, "%s %s %s %s %s %s\n", ev.EventTime, ev.AWSRegion, ev.EventSource, ev.EventName, ev.ErrorCode, ev.ErrorMessage)

			continue
		}

		fmt.Fprintln(out, eventLine(ev))
	}

	_, err := fmt.Fprintf(out, "CloudTrail: session の key %d 件、メンバーの key %d 件、失敗した event %d 件\n", keys, members, len(events))

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
