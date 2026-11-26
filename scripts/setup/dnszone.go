package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	dnsVariablesFile = "variables.tf"
	apexEnv          = "prd"
	serveSigning     = "SIGNING"
	kskActive        = "ACTIVE"
	kskActionNeeded  = "ACTION_NEEDED"
	kskFlag          = 257
	// delegationNameServers は、委任の NS の数。usecases/dns-zone/variables.tf の delegations の validation の
	// length(d.name_servers) == 4 と同じ数で、HCL の式の中の数なので写す。
	delegationNameServers = 4
	// maxKeyTag は、DS の key tag の上限。variables.tf の delegations の validation の tonumber(...) <= 65535 と同じ数。
	maxKeyTag = 65535
	// kskKeySpec と kskKeyUsage は、KSK の KMS の鍵の要件。usecases/dns-zone/internal/dnssec-key/main.tf の
	// aws_kms_key の customer_master_key_spec と key_usage と同じ値。
	kskKeySpec  = "ECC_NIST_P256"
	kskKeyUsage = "SIGN_VERIFY"
	// notRegisteredCode は、このアカウントの Route 53 Domains に無いドメインか、正しくない名前への操作の error code。
	notRegisteredCode = "InvalidInput"
	kmsNotFoundCode   = "NotFoundException"
)

var (
	// errZoneName は、-zone が apex_domain の形でないか、-env の zone の名前でないこと。
	errZoneName = xerrors.New("zone の名前が、dns-zone の宣言の形ではありません")
	// errEnvNotApplicable は、入口がその env の zone では行わない手順であること（委任の値は dev・stg、registrar は prd）。
	errEnvNotApplicable = xerrors.New("この入口は、その env の zone では行いません")
)

// dnsShape は、dns-zone の Terraform から読む、手順が守るべき形。delegated は delegations の key に許す env。
type dnsShape struct {
	delegated []string
	ns        *regexp.Regexp
	ds        *regexp.Regexp
}

// readDNSShape は、-usecase の variables.tf から、env の一覧、zone の名前の形、委任の key と NS と DS の形を読み、
// -env と -zone がそれに合うことを求める。prd の zone は apex で、ほかは <env>.<apex> である。
func readDNSShape(o options) (dnsShape, error) {
	body, err := parseFile(o.readFile, path.Join(o.usecase, dnsVariablesFile))
	if err != nil {
		return dnsShape{}, err
	}

	if err := requireEnv(body, o.env); err != nil {
		return dnsShape{}, err
	}

	apex, err := variableBlock(body, "apex_domain")
	if err != nil {
		return dnsShape{}, err
	}

	apexCalls, err := regexCalls(apex.Body)
	if err != nil {
		return dnsShape{}, err
	}

	if len(apexCalls) != 1 {
		return dnsShape{}, xerrors.Wrapf(errDeclShape, "variable apex_domain の regex が %d 件", len(apexCalls))
	}

	if !apexCalls[0].pattern.MatchString(o.zone) || (o.env != apexEnv && !strings.HasPrefix(o.zone, o.env+".")) {
		return dnsShape{}, xerrors.Wrapf(errZoneName, "%s（env %s の zone は %s）", o.zone, o.env, map[bool]string{true: "<apex_domain>", false: o.env + ".<apex_domain>"}[o.env == apexEnv])
	}

	delegations, err := variableBlock(body, "delegations")
	if err != nil {
		return dnsShape{}, err
	}

	keys, err := literalListArg(delegations.Body, "setsubtract", 1)
	if err != nil {
		return dnsShape{}, err
	}

	ns, ds, err := delegationPatterns(delegations.Body)
	if err != nil {
		return dnsShape{}, err
	}

	return dnsShape{delegated: keys, ns: ns, ds: ds}, nil
}

// delegationPatterns は、delegations の validation の regex のうち、ds_record に掛かるものを DS の形、ほかを NS の形
// として返す。それぞれがただ1つでなければ errDeclShape。
func delegationPatterns(body *hclsyntax.Body) (*regexp.Regexp, *regexp.Regexp, error) {
	calls, err := regexCalls(body)
	if err != nil {
		return nil, nil, err
	}

	var ns, ds []*regexp.Regexp

	for _, call := range calls {
		if isAttrRef(call.subject, "ds_record") {
			ds = append(ds, call.pattern)
		} else {
			ns = append(ns, call.pattern)
		}
	}

	if len(ns) != 1 || len(ds) != 1 {
		return nil, nil, xerrors.Wrapf(errDeclShape, "delegations の regex が NS に %d 件、DS に %d 件", len(ns), len(ds))
	}

	return ns[0], ds[0], nil
}

// isAttrRef は、expr が、最後の段が attr の名前の参照（d.ds_record など）か。
func isAttrRef(expr hclsyntax.Expression, attr string) bool {
	st, ok := expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(st.Traversal) < 2 {
		return false
	}

	last, ok := st.Traversal[len(st.Traversal)-1].(hcl.TraverseAttr)

	return ok && last.Name == attr
}

// validNameServers は、ns が delegations の NS の形（重ならない4つ）か。
func (s dnsShape) validNameServers(ns []string) bool {
	if len(ns) != delegationNameServers || len(slices.Compact(slices.Sorted(slices.Values(ns)))) != len(ns) {
		return false
	}

	for _, n := range ns {
		if !s.ns.MatchString(n) {
			return false
		}
	}

	return true
}

// validDS は、ds が delegations の DS の形か。
func (s dnsShape) validDS(ds string) bool {
	if !s.ds.MatchString(ds) {
		return false
	}

	tag, err := strconv.Atoi(strings.SplitN(ds, " ", 2)[0])

	return err == nil && tag <= maxKeyTag
}

// hostedZoneID は、name の公開の Hosted Zone の ID（/hostedzone/ を除く）。list-hosted-zones-by-name の --dns-name は
// 一覧の始まりの位置なので、名前の一致を確かめる。ただ1つに決まらなければ errPrecondition。
func hostedZoneID(c *client, name string) (string, error) {
	var resp struct {
		HostedZones *[]struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Config struct {
				PrivateZone bool `json:"PrivateZone"`
			} `json:"Config"`
		} `json:"HostedZones"`
	}

	if err := c.read(&resp, "route53", "list-hosted-zones-by-name", "--dns-name", name); err != nil {
		return "", err
	}

	if resp.HostedZones == nil {
		return "", xerrors.Wrap(errAWSResponse, "route53 list-hosted-zones-by-name: HostedZones がありません")
	}

	var ids []string

	for _, z := range *resp.HostedZones {
		if z.Name == name+"." && !z.Config.PrivateZone {
			ids = append(ids, z.ID)
		}
	}

	if len(ids) != 1 {
		return "", xerrors.Wrapf(errPrecondition, "公開の Hosted Zone %s が %d 件です（無ければ apply が先）", name, len(ids))
	}

	id := strings.TrimPrefix(ids[0], "/hostedzone/")
	if id == "" {
		return "", xerrors.Wrap(errAWSResponse, "route53 list-hosted-zones-by-name: "+name+" の Id がありません")
	}

	return id, nil
}

func zoneNameServers(c *client, id string) ([]string, error) {
	var resp struct {
		DelegationSet struct {
			NameServers []string `json:"NameServers"`
		} `json:"DelegationSet"`
	}

	if err := c.read(&resp, "route53", "get-hosted-zone", "--id", id); err != nil {
		return nil, err
	}

	if len(resp.DelegationSet.NameServers) == 0 || slices.Contains(resp.DelegationSet.NameServers, "") {
		return nil, xerrors.Wrap(errAWSResponse, "route53 get-hosted-zone: DelegationSet の NameServers がありません")
	}

	return resp.DelegationSet.NameServers, nil
}

// signingKey は、get-dnssec の KSK の1つ。
type signingKey struct {
	Name                 string `json:"Name"`
	KmsArn               string `json:"KmsArn"`
	Flag                 int    `json:"Flag"`
	SigningAlgorithmType int    `json:"SigningAlgorithmType"`
	KeyTag               int    `json:"KeyTag"`
	PublicKey            string `json:"PublicKey"`
	DSRecord             string `json:"DSRecord"`
	Status               string `json:"Status"`
	StatusMessage        string `json:"StatusMessage"`
}

type dnssecState struct {
	serve string
	ksks  []signingKey
}

func readDNSSEC(c *client, id string) (dnssecState, error) {
	var resp struct {
		Status struct {
			ServeSignature string `json:"ServeSignature"`
		} `json:"Status"`
		KeySigningKeys *[]signingKey `json:"KeySigningKeys"`
	}

	if err := c.read(&resp, "route53", "get-dnssec", "--hosted-zone-id", id); err != nil {
		return dnssecState{}, err
	}

	if resp.Status.ServeSignature == "" || resp.KeySigningKeys == nil {
		return dnssecState{}, xerrors.Wrap(errAWSResponse, "route53 get-dnssec: ServeSignature か KeySigningKeys がありません")
	}

	for _, k := range *resp.KeySigningKeys {
		if k.Name == "" || k.Status == "" || k.KmsArn == "" {
			return dnssecState{}, xerrors.Wrap(errAWSResponse, "route53 get-dnssec: Name・Status・KmsArn の欠けた KSK があります")
		}
	}

	return dnssecState{serve: resp.Status.ServeSignature, ksks: *resp.KeySigningKeys}, nil
}

// activeKSK は、署名が SIGNING で、KSK がただ1つで ACTIVE なら、その KSK。そうでなければ理由を返す。
func (s dnssecState) activeKSK() (signingKey, string) {
	switch {
	case s.serve != serveSigning:
		return signingKey{}, "署名が SIGNING ではありません（今: " + s.serve + "）"
	case len(s.ksks) != 1:
		return signingKey{}, fmt.Sprintf("KSK が %d 件です", len(s.ksks))
	case s.ksks[0].Status != kskActive:
		return signingKey{}, "KSK " + s.ksks[0].Name + " が ACTIVE ではありません（今: " + s.ksks[0].Status + "）"
	default:
		return s.ksks[0], ""
	}
}

// keyCheck は、DNSSEC の KMS の鍵の要件の1つの判定。
type keyCheck struct {
	ok   bool
	what string
}

// dnssecKeyChecks は、KSK の KMS の鍵が Route 53 の DNSSEC の要件（us-east-1、Enabled、ECC_NIST_P256、SIGN_VERIFY）に
// 合うかの判定の並び。鍵の ARN が us-east-1 でなければ、鍵を読まずにそれだけを返す。
func dnssecKeyChecks(c *client, arn string) ([]keyCheck, error) {
	p := strings.Split(arn, ":")
	if len(p) < 6 || p[0] != "arn" || p[2] != "kms" || p[3] != usEast1Region {
		return []keyCheck{{false, "鍵 " + arn + " が us-east-1 の KMS の鍵"}}, nil
	}

	var resp struct {
		KeyMetadata struct {
			KeyState string `json:"KeyState"`
			KeySpec  string `json:"KeySpec"`
			KeyUsage string `json:"KeyUsage"`
		} `json:"KeyMetadata"`
	}

	found, err := c.readOptionalIn(usEast1Region, kmsNotFoundCode, &resp, "kms", "describe-key", "--key-id", arn)
	if err != nil {
		return nil, err
	}

	if !found {
		return []keyCheck{{false, "鍵 " + arn + " が在る"}}, nil
	}

	m := resp.KeyMetadata
	if m.KeyState == "" || m.KeySpec == "" || m.KeyUsage == "" {
		return nil, xerrors.Wrap(errAWSResponse, "kms describe-key: KeyState・KeySpec・KeyUsage がありません")
	}

	return []keyCheck{
		{m.KeyState == "Enabled", "KSK の鍵が Enabled（今: " + m.KeyState + "）"},
		{m.KeySpec == kskKeySpec, "KSK の鍵が " + kskKeySpec + "（今: " + m.KeySpec + "）"},
		{m.KeyUsage == kskKeyUsage, "KSK の鍵が " + kskKeyUsage + "（今: " + m.KeyUsage + "）"},
	}, nil
}

// printDelegationValues は、3.2: 子の zone の NS と、署名が SIGNING でただ1つの ACTIVE な KSK の DS を、prd の
// delegations に書く HCL の形で出す。DS を出せなければ ds_record を省き、その理由を出す。DS に12桁続く数字が在れば、
// 出力を伏せる処理が値を壊すので、-ids でなければ何も出さずに errPrecondition。
func printDelegationValues(o options, c *client, w io.Writer) error {
	shape, err := readDNSShape(o)
	if err != nil {
		return err
	}

	if !slices.Contains(shape.delegated, o.env) {
		return xerrors.Wrapf(errEnvNotApplicable, "委任の値は %s の zone のものです", joinOrNone(shape.delegated))
	}

	id, err := hostedZoneID(c, o.zone)
	if err != nil {
		return err
	}

	ns, err := zoneNameServers(c, id)
	if err != nil {
		return err
	}

	if !shape.validNameServers(ns) {
		return xerrors.Wrapf(errPrecondition, "zone の NS（%s）が delegations の name_servers の形に合いません", joinOrNone(ns))
	}

	st, err := readDNSSEC(c, id)
	if err != nil {
		return err
	}

	ksk, reason := st.activeKSK()
	if reason == "" && !shape.validDS(ksk.DSRecord) {
		return xerrors.Wrapf(errPrecondition, "KSK の DS（%s）が delegations の ds_record の形に合いません", ksk.DSRecord)
	}

	if reason == "" && !o.showIDs && accountPattern.MatchString(ksk.DSRecord) {
		return xerrors.Wrapf(errPrecondition, "KSK の DS（key tag %d）に12桁続く数字が在り、アカウント ID を伏せる処理が値を壊します。IDS=1 で出し直してください", ksk.KeyTag)
	}

	quoted := make([]string, len(ns))
	for i, n := range ns {
		quoted[i] = strconv.Quote(n)
	}

	fmt.Fprintf(w, "# prd の env の値の delegations に書く値（zone %s）\n", o.zone)
	fmt.Fprintf(w, "%s = {\n  name_servers = [%s]\n", o.env, strings.Join(quoted, ", "))

	if reason == "" {
		fmt.Fprintf(w, "  ds_record    = %s\n", strconv.Quote(ksk.DSRecord))
	}

	fmt.Fprintln(w, "}")

	if reason != "" {
		fmt.Fprintf(w, "# ds_record を出しません: %s。署名が戻ってから出し直してください\n", reason)
	}

	return nil
}

// checkSigning は、3.5・4 の確かめ: 署名が SIGNING で、KSK がただ1つで ACTIVE で、その KMS の鍵が要件に合う。
func checkSigning(o options, c *client, w io.Writer) error {
	if _, err := readDNSShape(o); err != nil {
		return err
	}

	id, err := hostedZoneID(c, o.zone)
	if err != nil {
		return err
	}

	st, err := readDNSSEC(c, id)
	if err != nil {
		return err
	}

	r := newReport(w, "zone "+o.zone+" の DNSSEC の署名（3.5・4）")
	r.check(st.serve == serveSigning, "署名が SIGNING（今: "+st.serve+"）")
	r.check(len(st.ksks) == 1, fmt.Sprintf("KSK がただ1つ（今: %d）", len(st.ksks)))

	for _, k := range st.ksks {
		what := "KSK " + k.Name + " が ACTIVE（今: " + k.Status + "）"
		if k.Status == kskActionNeeded {
			what += "。鍵を直してから make setup-dns-zone-ksk-activate で戻します"
		}

		r.check(k.Status == kskActive, what)

		checks, err := dnssecKeyChecks(c, k.KmsArn)
		if err != nil {
			return err
		}

		for _, kc := range checks {
			r.check(kc.ok, kc.what)
		}
	}

	return r.result()
}

// activateKSK は、4 手順3: ただ1つの KSK が ACTION_NEEDED で、その KMS の鍵が要件に合うときだけ、KSK を有効に戻す。
func activateKSK(o options, c *client, t *terminal) error {
	if _, err := readDNSShape(o); err != nil {
		return err
	}

	id, err := hostedZoneID(c, o.zone)
	if err != nil {
		return err
	}

	st, err := readDNSSEC(c, id)
	if err != nil {
		return err
	}

	if len(st.ksks) != 1 {
		return xerrors.Wrapf(errPrecondition, "KSK が %d 件です。どれを戻すか決めません", len(st.ksks))
	}

	ksk := st.ksks[0]

	switch ksk.Status {
	case kskActive:
		fmt.Fprintf(t.out, "✅ KSK %s は ACTIVE です。書きません\n", ksk.Name)

		return nil
	case kskActionNeeded:
	default:
		return xerrors.Wrapf(errPrecondition, "KSK %s が ACTION_NEEDED ではありません（今: %s）", ksk.Name, ksk.Status)
	}

	checks, err := dnssecKeyChecks(c, ksk.KmsArn)
	if err != nil {
		return err
	}

	for _, kc := range checks {
		if !kc.ok {
			return xerrors.Wrap(errPrecondition, "KMS の鍵が要件に合いません: "+kc.what+"。先に鍵と Key Policy を直します")
		}
	}

	fmt.Fprintf(t.out, "zone %s の KSK %s を有効に戻します（今: ACTION_NEEDED。%s）\n", o.zone, ksk.Name, ksk.StatusMessage)

	if err := t.confirm(o.project); err != nil {
		return err
	}

	if err := c.write("route53", "activate-key-signing-key", "--hosted-zone-id", id, "--name", ksk.Name); err != nil {
		return err
	}

	if err := reread(o, func() (bool, error) {
		got, err := readDNSSEC(c, id)
		if err != nil {
			return false, err
		}

		return slices.ContainsFunc(got.ksks, func(k signingKey) bool { return k.Name == ksk.Name && k.Status == kskActive }), nil
	}); err != nil {
		return xerrors.Wrap(err, "KSK "+ksk.Name+" が ACTIVE に見えません。make setup-dns-zone-signing-check で確かめてください")
	}

	fmt.Fprintf(t.out, "✅ KSK %s を有効に戻しました\n", ksk.Name)

	return nil
}

// registrarKey は、Route 53 Domains のドメインの DNSSEC の鍵（registrar の DS）の1つ。
type registrarKey struct {
	KeyTag    int    `json:"KeyTag"`
	PublicKey string `json:"PublicKey"`
}

type registrarDomain struct {
	nameServers []string
	keys        []registrarKey
}

// readRegistrar は、このアカウントの Route 53 Domains の domain の NS と DS。InvalidInput（登録されていないか、名前が
// 正しくない）なら、aws のエラーの文を添えて errPrecondition。
func readRegistrar(c *client, domain string) (registrarDomain, error) {
	var resp struct {
		Nameservers []struct {
			Name string `json:"Name"`
		} `json:"Nameservers"`
		DnssecKeys []registrarKey `json:"DnssecKeys"`
	}

	err := c.readIn(usEast1Region, &resp, "route53domains", "get-domain-detail", "--domain-name", domain)
	if xerrors.Is(err, errAWSFailed) && strings.Contains(err.Error(), notRegisteredCode) {
		return registrarDomain{}, xerrors.Wrap(errPrecondition, "ドメイン "+domain+" はこのアカウントの Route 53 Domains に無いか、名前が正しくありません（"+err.Error()+
			"）。ほかの registrar では手順書 3.4 の手で行う手順で登録します")
	}

	if err != nil {
		return registrarDomain{}, err
	}

	if len(resp.Nameservers) == 0 {
		return registrarDomain{}, xerrors.Wrap(errAWSResponse, "route53domains get-domain-detail: Nameservers がありません")
	}

	var ns []string

	for _, n := range resp.Nameservers {
		if n.Name == "" {
			return registrarDomain{}, xerrors.Wrap(errAWSResponse, "route53domains get-domain-detail: Name の欠けた name server があります")
		}

		ns = append(ns, n.Name)
	}

	for _, k := range resp.DnssecKeys {
		if k.PublicKey == "" {
			return registrarDomain{}, xerrors.Wrap(errAWSResponse, "route53domains get-domain-detail: PublicKey の欠けた DNSSEC の鍵があります")
		}
	}

	return registrarDomain{nameServers: ns, keys: resp.DnssecKeys}, nil
}

// normalizeNS は、name server の名前を、末尾のドットと大文字小文字の違いを除いた形にする。
func normalizeNS(ns []string) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = strings.ToLower(strings.TrimSuffix(n, "."))
	}

	return out
}

func sameNameServers(a, b []string) bool {
	return sameSet(normalizeNS(a), normalizeNS(b))
}

// requireApex は、registrar の手順の -env が prd（apex の zone）であることを求める。
func requireApex(o options) error {
	if o.env != apexEnv {
		return xerrors.Wrap(errEnvNotApplicable, "registrar に登録するのは prd の zone（apex）です")
	}

	return nil
}

// updateRegistrarNS は、3.4: Route 53 Domains の registrar の NS を、zone の NS に替える。registrar に zone の KSK の
// ものでない DS が在れば、替えると名前が引けなくなるので書かない。
func updateRegistrarNS(o options, c *client, t *terminal) error {
	if err := requireApex(o); err != nil {
		return err
	}

	if _, err := readDNSShape(o); err != nil {
		return err
	}

	reg, err := readRegistrar(c, o.zone)
	if err != nil {
		return err
	}

	id, err := hostedZoneID(c, o.zone)
	if err != nil {
		return err
	}

	ns, err := zoneNameServers(c, id)
	if err != nil {
		return err
	}

	fmt.Fprintf(t.out, "  registrar の NS: %s\n  zone の NS: %s\n", joinOrNone(reg.nameServers), joinOrNone(ns))

	if sameNameServers(reg.nameServers, ns) {
		fmt.Fprintln(t.out, "✅ registrar の NS は zone の NS と一致しています。書きません")

		return nil
	}

	if len(reg.keys) > 0 {
		st, err := readDNSSEC(c, id)
		if err != nil {
			return err
		}

		for _, k := range reg.keys {
			if !slices.ContainsFunc(st.ksks, func(s signingKey) bool { return s.PublicKey == k.PublicKey }) {
				return xerrors.Wrapf(errPrecondition, "registrar に、この zone の KSK のものでない DS（key tag %d）が在ります。NS を替えると名前が引けなくなるので、先に registrar の DS を外します（手順書 4）", k.KeyTag)
			}
		}
	}

	fmt.Fprintf(t.out, "ドメイン %s の registrar の NS を zone の NS に替えます\n", o.zone)

	if err := t.confirm(o.project); err != nil {
		return err
	}

	args := []string{"route53domains", "update-domain-nameservers", "--domain-name", o.zone, "--nameservers"}
	for _, n := range ns {
		args = append(args, "Name="+n)
	}

	if err := c.writeIn(usEast1Region, args...); err != nil {
		return err
	}

	if err := reread(o, func() (bool, error) {
		got, err := readRegistrar(c, o.zone)
		if err != nil {
			return false, err
		}

		return sameNameServers(got.nameServers, ns), nil
	}); err != nil {
		return xerrors.Wrap(err, "registrar の NS の書き換えは受け付けられましたが、読み直しで zone の NS に見えません。Route 53 Domains の操作は非同期で、まだ進んでいることがあります。"+
			"数分後に make setup-dns-zone-registrar-ns をやり直して確かめます（反映されていれば書きません）")
	}

	fmt.Fprintln(t.out, "✅ registrar の NS を zone の NS に替えました。DS は、切り替えが行き渡ってから make setup-dns-zone-registrar-ds で足します")

	return nil
}

// associateRegistrarDS は、3.4: Route 53 Domains の registrar に、zone のただ1つの ACTIVE な KSK の DS を足す。
// 順を誤ると名前が引けなくなるので、registrar の NS が zone の NS と一致し、zone が SIGNING であることを前提にする。
func associateRegistrarDS(o options, c *client, t *terminal) error {
	if err := requireApex(o); err != nil {
		return err
	}

	if _, err := readDNSShape(o); err != nil {
		return err
	}

	reg, err := readRegistrar(c, o.zone)
	if err != nil {
		return err
	}

	id, err := hostedZoneID(c, o.zone)
	if err != nil {
		return err
	}

	ns, err := zoneNameServers(c, id)
	if err != nil {
		return err
	}

	if !sameNameServers(reg.nameServers, ns) {
		return xerrors.Wrapf(errPrecondition, "registrar の NS（%s）が zone の NS（%s）と一致しません。先に make setup-dns-zone-registrar-ns で替えます", joinOrNone(reg.nameServers), joinOrNone(ns))
	}

	st, err := readDNSSEC(c, id)
	if err != nil {
		return err
	}

	ksk, reason := st.activeKSK()
	if reason != "" {
		return xerrors.Wrap(errPrecondition, reason+"。DS は署名が有効になってから足します")
	}

	if ksk.PublicKey == "" || ksk.SigningAlgorithmType <= 0 || ksk.Flag != kskFlag {
		return xerrors.Wrap(errAWSResponse, "route53 get-dnssec: KSK "+ksk.Name+" の PublicKey・SigningAlgorithmType・Flag を読めません")
	}

	if slices.ContainsFunc(reg.keys, func(k registrarKey) bool { return k.PublicKey == ksk.PublicKey }) {
		fmt.Fprintf(t.out, "✅ registrar に KSK %s の DS（key tag %d）が在ります。書きません\n", ksk.Name, ksk.KeyTag)

		return nil
	}

	for _, k := range reg.keys {
		fmt.Fprintf(t.out, "  registrar に、この KSK のものでない DS（key tag %d）が在ります。この入口は外しません\n", k.KeyTag)
	}

	fmt.Fprintf(t.out, "ドメイン %s の registrar に KSK %s の DS（Algorithm %d、Flags %d、key tag %d）を足します。NS の切り替えが行き渡ってから（registrar の NS の TTL を過ぎてから）行います。早いと名前が引けなくなります\n",
		o.zone, ksk.Name, ksk.SigningAlgorithmType, kskFlag, ksk.KeyTag)

	if err := t.confirm(o.project); err != nil {
		return err
	}

	attrs, err := json.Marshal(map[string]any{"Algorithm": ksk.SigningAlgorithmType, "Flags": kskFlag, "PublicKey": ksk.PublicKey})
	if err != nil {
		return err
	}

	if err := c.writeIn(usEast1Region, "route53domains", "associate-delegation-signer-to-domain", "--domain-name", o.zone, "--signing-attributes", string(attrs)); err != nil {
		return err
	}

	if err := reread(o, func() (bool, error) {
		got, err := readRegistrar(c, o.zone)
		if err != nil {
			return false, err
		}

		return slices.ContainsFunc(got.keys, func(k registrarKey) bool { return k.PublicKey == ksk.PublicKey }), nil
	}); err != nil {
		return xerrors.Wrap(err, "registrar への DS の追加は受け付けられましたが、読み直しで DS が見えません。Route 53 Domains の操作は非同期で、まだ進んでいることがあります。"+
			"数分後に make setup-dns-zone-registrar-ds をやり直して確かめます（反映されていれば書きません）")
	}

	fmt.Fprintln(t.out, "✅ registrar に DS を足しました。make setup-dns-zone-signing-check と手順書 3.5 の dig で確かめます")

	return nil
}
