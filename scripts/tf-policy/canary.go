package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/policyresult"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tfexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// canaryExcluded は、canary の宣言が usecase ごとの宣言を持たないので、Policy の木の写しから外す rego の名前の
// パターンです（宣言が無ければ各 package の 00 で落ちる）。usecase ごとの宣言を読む Policy を足したら、ここにも足す。
var canaryExcluded = []string{
	"deployment_identity_*.rego", "state_backend_*.rego", "kms_key_policy*.rego", "resource_policy_exclusive*.rego",
	"audit_evidence*.rego", "finops*.rego", "network_foundation*.rego", "dns_zone*.rego", "ops_notification*.rego",
	"image_registry*.rego", "publisher_trust*.rego", "iam_role_attachment_pairing*.rego", "organization_baseline_*.rego",
	"workload_role_boundary*.rego", "account_baseline*.rego",
}

// canaryData は、canary の Policy の宣言です。terraform_data の input を IAM の文書として読ませます。
const canaryData = `{"policy_plan": {"plan_format_version": "1.2", "provider_format_version": "1.0", "documents": {"terraform_data": ["input"]}, "required_documents": ["terraform_data"], "deployment_identity": {"roles": ["module.x.aws_iam_role.this"], "oidc_provider": "module.x.aws_iam_openid_connect_provider.this"}, "stateful_protection": {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration", "aws_ecr_lifecycle_policy", "terraform_data"], "allow": {}}, "inline_arguments": {"terraform_data": {"triggers_replace": null}}}}`

const canaryLayer = `{"layer_decl": {"attachments": {"aws_x_policy": {"parents": {"x": ["aws_x"]}, "optional": false, "parent_arguments": {}, "whole_document": false}}, "unrelated_deprecated_arguments": {}, "inline_owned": {}}}`

const canaryPolicyInput = "run \"policy_input\" {\n  command = plan\n}\n"

// canaryOK は、違反の無い文書を持つ構成です。
const canaryOK = `resource "terraform_data" "doc" { input = jsonencode({ Statement = [{ Effect = "Allow", Action = "s3:GetObject", Resource = "*" }] }) }`

// canaryCase は、canary の1ケースです。pass は通るか落ちるか、pattern は出力に現れるべき文です。
type canaryCase struct {
	name    string
	pass    bool
	pattern string
	main    string                        // reference の root の main.tf
	extra   func(c string) error          // 構成・Rego・宣言を書き換える
	data    func(d map[string]any)        // Policy の宣言を書き換える
	layer   func(d map[string]any) []byte // 層の宣言を差し替える（nil なら canaryLayer）
}

// runCanary は、Policy Test の段（Rego の形の検査、Rego のテスト、plan の取得と取り出し、非推奨の引数の取り出し、
// Conftest の適用、評価の数の突き合わせ）を、実物の terraform・Conftest・mock provider で通しで走らせ、違反の無い
// 入力で通り、Go の単体テストでは作れない落ち方（実物の Conftest が評価しない規則、mock の override が書いた
// 値を隠さないこと。ADR-0402 決定18）で落ちることを確かめます。[errTool] のときは、ケースの判定に
// 畳まず [errTool] をそのまま返します。root はリポジトリ直下です。
func runCanary(args []string, w io.Writer, tool toolFunc, root string) error {
	fset := flag.NewFlagSet("tf-policy plan の canary", flag.ContinueOnError)
	fset.SetOutput(w)
	data := fset.String("data", "", "TF_DATA_DIR を置くディレクトリ（絶対パス）")
	work := fset.String("work", "", "fixture を建てるディレクトリ（中身は毎回作り直す）")
	policy := fset.String("policy", "", "Policy の木（conftest/plan）")
	planDecl := fset.String("decl", "", "Policy の宣言（conftest/plan/decl.json）。Rego のテストに渡す")
	layerDecl := fset.String("layer", "", "層の宣言（conftest/hcl/layer/decl.json）")
	reference := fset.String("reference", "", "aws の fixture に写す versions.tf と lock の root")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if !filepath.IsAbs(*data) || *work == "" || *policy == "" || *planDecl == "" || *layerDecl == "" || *reference == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "canary -data <絶対パス> -work <tmp/ の下の相対パス> -policy <dir> -decl <path> -layer <path> -reference <dir>")
	}
	resolved, err := workdir.Resolve(root, *work)
	if err != nil {
		return xerrors.Wrapf(errUsage, "-work %v", err)
	}
	// fixture の中で起動する道具の作業ディレクトリと、dir からの相対パスを解くため、絶対パスにする。
	workAbs, err := filepath.Abs(resolved)
	if err != nil {
		return xerrors.Wrapf(err, "%s", resolved)
	}
	planDeclRaw, err := os.ReadFile(filepath.Clean(*planDecl))
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", *planDecl, err)
	}
	if err := os.RemoveAll(workAbs); err != nil {
		return xerrors.Wrapf(err, "%s の削除", workAbs)
	}
	layerRaw, err := os.ReadFile(filepath.Clean(*layerDecl))
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", *layerDecl, err)
	}
	nullValues, err := awsNullValues(layerRaw)
	if err != nil {
		return err
	}
	cases := canaryCases(*reference, layerRaw, nullValues)
	verified := map[string][]byte{}
	var failed []string
	for _, c := range cases {
		dir := filepath.Join(workAbs, c.name)
		if err := buildCase(dir, *policy, planDeclRaw, layerRaw, c); err != nil {
			return xerrors.Wrapf(err, "%s の fixture", c.name)
		}
		out, err := policyChain(dir, *data, tool, verified)
		if xerrors.Is(err, errTool) {
			return err
		}
		ok := strings.Contains(out, c.pattern)
		if c.pass {
			ok = ok && err == nil
		} else {
			ok = ok && err != nil
		}
		if !ok {
			failed = append(failed, c.name)
			fmt.Fprintf(w, "--- canary %s の出力（err: %v）:\n%s\n", c.name, err, out)
		}
	}
	if len(failed) > 0 {
		return xerrors.Wrapf(errCanary, "tf-policy の canary が期待どおりでなかったケース: %s", strings.Join(failed, " "))
	}
	fmt.Fprintf(w, "✅ tf-policy は違反の無い入力で通り、実物の Conftest が評価しない規則・テストの件数のずれ・違反・unknown の文書・宣言と schema のずれ・mock の override が隠さない書いた引数で落ちる（%d ケース）\n", len(cases))

	return nil
}

// planCanary は、root をリポジトリ直下として runCanary を走らせる canary です。
func planCanary(root string) canaryFunc {
	return func(args []string, w io.Writer, tool toolFunc) error { return runCanary(args, w, tool, root) }
}

func canaryCases(reference string, layerRaw []byte, nullValues string) []canaryCase {
	appendTo := func(rel, text string) func(c string) error {
		return func(c string) error { return appendText(filepath.Join(c, filepath.FromSlash(rel)), text) }
	}
	aws := func(written string) func(c string) error {
		return func(c string) error {
			root := filepath.Join(c, "base", "usecases", "x", "reference")
			for _, f := range []string{"versions.tf", ".terraform.lock.hcl"} {
				raw, err := os.ReadFile(filepath.Clean(filepath.Join(reference, f)))
				if err != nil {
					return xerrors.Wrapf(err, "%s", f)
				}
				if err := writeText(filepath.Join(root, f), string(raw)); err != nil {
					return err
				}
			}
			if err := appendText(filepath.Join(root, "main.tf"), fmt.Sprintf("resource \"aws_s3_bucket\" \"b\" {\n  bucket           = \"canary\"\n  bucket_namespace = \"account-regional\"\n  %s\n}\n", written)); err != nil {
				return err
			}

			return writeText(filepath.Join(root, "tests", "policy-input.tftest.hcl"), fmt.Sprintf("mock_provider \"aws\" {\n  override_during = plan\n}\n\noverride_resource {\n  target          = aws_s3_bucket.b\n  override_during = plan\n  values = {\n%s\n  }\n}\n\nrun \"policy_input\" {\n  command = plan\n}\n", nullValues))
		}
	}
	noInline := func(d map[string]any) { d["policy_plan"].(map[string]any)["inline_arguments"] = map[string]any{} }
	realLayer := func(map[string]any) []byte { return layerRaw }
	uncovered := func(attachment, arg string) func(map[string]any) []byte {
		return func(map[string]any) []byte {
			var d map[string]any
			if err := json.Unmarshal(layerRaw, &d); err != nil {
				return nil
			}
			if pa, ok := dig(d, "layer_decl", "attachments", attachment, "parent_arguments", "aws_s3_bucket"); ok {
				delete(pa, arg)
			}
			out, _ := json.Marshal(d)

			return out
		}
	}

	return []canaryCase{
		{name: "ok", pass: true, pattern: "規則", main: canaryOK},
		{name: "star", pattern: "[PLAN-IAM-01]", main: `resource "terraform_data" "doc" { input = jsonencode({ Statement = [{ Effect = "Allow", Action = "*", Resource = "*" }] }) }`},
		{name: "unknown", pattern: "[PLAN-INPUT-04]", main: "resource \"terraform_data\" \"a\" {}\nresource \"terraform_data\" \"doc\" { input = terraform_data.a.id }"},
		{name: "failing-rego-test", pattern: "Rego のテストが落ちました", main: canaryOK, extra: appendTo("rego/iam_action_wildcard_test.rego", "\ntest_pass_fails if false\n")},
		{name: "unevaluated-package", pattern: "plan.extra: ❌ 名前空間の規則が1つも評価されていません", main: canaryOK, extra: func(c string) error {
			if err := writeText(filepath.Join(c, "rego", "extra.rego"), "package plan.extra\n\nnote := `\ndeny contains \"[PLAN-IAM-01] x\" if false\n`\n"); err != nil {
				return err
			}

			return writeText(filepath.Join(c, "rego", "extra_test.rego"), "package plan.extra_test\n\ntest_pass_note if true\n\ntest_violation_note if true\n")
		}},
		{name: "indented-rule", pattern: "plan.iam_action_wildcard: 評価", main: canaryOK, extra: appendTo("rego/iam_action_wildcard.rego", "\n  deny contains \"[PLAN-IAM-01] y\" if false\n")},
		{name: "count-mismatch", pattern: "宣言された", main: canaryOK, extra: appendTo("rego/iam_action_wildcard_test.rego", "\nnot_a_test := `\ntest_counted_but_not_run\n`\n")},
		{name: "undeclared-arg", pattern: "[PLAN-INPUT-10] inline_arguments の terraform_data の nope", main: canaryOK, data: func(d map[string]any) {
			d["policy_plan"].(map[string]any)["inline_arguments"].(map[string]any)["terraform_data"].(map[string]any)["nope"] = nil
		}},
		{name: "layer-decl-missing", pattern: "[PLAN-INPUT-12] 層の宣言 data.layer_decl", main: canaryOK, layer: func(map[string]any) []byte { return []byte("{}") }},
		{name: "aws-unwritten", pass: true, pattern: "規則", main: canaryOK, extra: aws(""), data: noInline, layer: realLayer},
		{name: "aws-written", pattern: "aws_s3_bucket.b の policy が書かれています", main: canaryOK, extra: aws(`policy = "{}"`), data: noInline, layer: realLayer},
		{name: "aws-uncovered", pattern: "[PLAN-INPUT-11] aws_s3_bucket の非推奨の引数 website", main: canaryOK, extra: aws(""), data: noInline, layer: uncovered("aws_s3_bucket_website_configuration", "website")},
		{name: "aws-uncovered-attr", pattern: "[PLAN-INPUT-11] aws_s3_bucket の非推奨の引数 acl", main: canaryOK, extra: aws(""), data: noInline, layer: uncovered("aws_s3_bucket_acl", "acl")},
	}
}

// dig は、入れ子の map を keys の順にたどり、最後の map を返します。
func dig(d map[string]any, keys ...string) (map[string]any, bool) {
	cur := d
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}

	return cur, true
}

// awsNullValues は、層の宣言から、aws_s3_bucket の書かないときの値（null）を override に並べる行を組みます。
// 親の引数で null と宣言したものと、非推奨の引数で書かないときの値が null のものです。0件なら失敗します。
func awsNullValues(layerRaw []byte) (string, error) {
	var d struct {
		LayerDecl struct {
			Attachments map[string]struct {
				ParentArguments map[string]map[string]any `json:"parent_arguments"`
			} `json:"attachments"`
			Unrelated map[string]map[string]map[string]any `json:"unrelated_deprecated_arguments"`
		} `json:"layer_decl"`
	}
	if err := json.Unmarshal(layerRaw, &d); err != nil {
		return "", xerrors.Wrapf(errRead, "層の宣言を解釈できません: %v", err)
	}
	names := map[string]bool{}
	for _, a := range d.LayerDecl.Attachments {
		for k, v := range a.ParentArguments["aws_s3_bucket"] {
			if v == nil {
				names[k] = true
			}
		}
	}
	for k, v := range d.LayerDecl.Unrelated["aws_s3_bucket"] {
		if u, ok := v["unwritten"]; ok && u == nil {
			names[k] = true
		}
	}
	if len(names) == 0 {
		return "", xerrors.Wrap(errCanary, "層の宣言から aws_s3_bucket の書かないときの値を組めませんでした")
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, "    "+k+" = null")
	}
	sort.Strings(keys)

	return strings.Join(keys, "\n"), nil
}

// buildCase は、ケースの配置（rego・decl.json・layer-decl.json・data.json・layer.json・base の reference の root）を
// dir に建てます。
func buildCase(dir, policy string, planDecl, layerDecl []byte, c canaryCase) error {
	if err := copyPolicy(policy, filepath.Join(dir, "rego")); err != nil {
		return err
	}
	if err := writeText(filepath.Join(dir, "decl.json"), string(planDecl)); err != nil {
		return err
	}
	if err := writeText(filepath.Join(dir, "layer-decl.json"), string(layerDecl)); err != nil {
		return err
	}
	var decl map[string]any
	if err := json.Unmarshal([]byte(canaryData), &decl); err != nil {
		return xerrors.Wrap(err, "canary の宣言")
	}
	if c.data != nil {
		c.data(decl)
	}
	raw, err := json.Marshal(decl)
	if err != nil {
		return xerrors.Wrap(err, "canary の宣言")
	}
	if err := writeText(filepath.Join(dir, "data.json"), string(raw)+"\n"); err != nil {
		return err
	}
	layer := []byte(canaryLayer)
	if c.layer != nil {
		layer = c.layer(nil)
	}
	if err := writeText(filepath.Join(dir, "layer.json"), string(layer)+"\n"); err != nil {
		return err
	}
	root := filepath.Join(dir, "base", "usecases", "x", "reference")
	if err := writeText(filepath.Join(root, "main.tf"), c.main+"\n"); err != nil {
		return err
	}
	if err := writeText(filepath.Join(root, "tests", "policy-input.tftest.hcl"), canaryPolicyInput); err != nil {
		return err
	}
	if c.extra != nil {
		return c.extra(dir)
	}

	return nil
}

// copyPolicy は、Policy の木を dst へ写し、canaryExcluded に当たる rego を外します。各パターンが1件以上に
// 当たらなければ失敗します —— 名前の変わった Policy を外し損ねると、canary の ok が宣言の欠けで落ちる。
func copyPolicy(src, dst string) error {
	root, err := os.OpenRoot(src)
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", src, err)
	}
	defer func() { _ = root.Close() }()
	hits := map[string]int{}
	err = fs.WalkDir(root.FS(), ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		if !strings.Contains(p, "/") {
			for _, pat := range canaryExcluded {
				if ok, _ := path.Match(pat, e.Name()); ok {
					hits[pat]++

					return nil
				}
			}
		}
		raw, err := root.ReadFile(p)
		if err != nil {
			return err
		}

		return writeText(filepath.Join(dst, filepath.FromSlash(p)), string(raw))
	})
	if err != nil {
		return xerrors.Wrapf(errRead, "%s: %v", src, err)
	}
	var unused []string
	for _, pat := range canaryExcluded {
		if hits[pat] == 0 {
			unused = append(unused, pat)
		}
	}
	if len(unused) > 0 {
		return xerrors.Wrapf(errCanary, "外すパターンが Policy の木の rego に1つも当たりません: %s", strings.Join(unused, " "))
	}

	return nil
}

// policyChain は、ケースの配置 dir で Policy Test の段を順に走らせ、出力と、最初に落ちた段のエラーを返します。
// Rego のテストの結果は、Rego の木と宣言の中身が同じケースの間で verified に持ち回ります（同じ入力の conftest
// verify を繰り返さない）。[errTool] を返した段は、その結果を持ち回りません。
func policyChain(dir, data string, tool toolFunc, verified map[string][]byte) (string, error) {
	var out bytes.Buffer
	files, err := readPolicy(filepath.Join(dir, "rego"))
	if err != nil {
		return out.String(), err
	}
	if problems := checkRego(files); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintf(&out, "❌ %s\n", p)
		}

		return out.String(), xerrors.Wrap(errFailed, "Policy の形")
	}
	env := tfexec.Env(os.Environ())
	key, err := verifyKey(dir, files)
	if err != nil {
		return out.String(), err
	}
	report, ok := verified[key]
	if !ok {
		var buf bytes.Buffer
		verify := tool(dir, env, stepVerify)
		verify.Stdout, verify.Stderr = &buf, &out
		if _, err := toolrun.Exec(verify); err != nil {
			return out.String(), xerrors.Wrap(err, "conftest verify")
		}
		report = buf.Bytes()
		verified[key] = report
	}
	if _, failed, err := judgeVerify(report, countTests(files)); err != nil {
		for _, f := range failed {
			fmt.Fprintf(&out, "  %s\n", f)
		}
		fmt.Fprintf(&out, "❌ %v\n", err)

		return out.String(), err
	}
	roots, err := listRoots(filepath.Join(dir, "base"))
	if err != nil {
		return out.String(), err
	}
	for _, r := range roots {
		cmd := tool(r.path, tfexec.Env(os.Environ(), "TF_DATA_DIR="+tfexec.DataDir(data, r.rel)), stepInit)
		cmd.Stdout, cmd.Stderr = &out, &out
		code, err := toolrun.Exec(cmd)
		if err != nil {
			return out.String(), xerrors.Wrapf(err, "%s の init", r.path)
		}
		if code != 0 {
			return out.String(), xerrors.Wrapf(errFailed, "%s の init: exit status %d", r.path, code)
		}
	}
	outDir := filepath.Join(dir, "out")
	if err := planRoots(roots, data, outDir, &out, tool); err != nil {
		return out.String(), err
	}
	declRaw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "data.json")))
	if err != nil {
		return out.String(), xerrors.Wrapf(errRead, "%v", err)
	}
	want, err := declaredFormat(declRaw)
	if err != nil {
		return out.String(), err
	}
	if _, err := extractAll(outDir, want, &out); err != nil {
		return out.String(), err
	}
	deprecated := tool(dir, env, stepDeprecated)
	deprecated.Stdout, deprecated.Stderr = &out, &out
	code, err := toolrun.Exec(deprecated)
	if err != nil {
		return out.String(), xerrors.Wrap(err, "非推奨の引数の取り出し")
	}
	if code != 0 {
		return out.String(), xerrors.Wrapf(errFailed, "非推奨の引数を取り出せません: exit status %d", code)
	}
	var result bytes.Buffer
	test := tool(dir, env, stepConftest)
	test.Stdout, test.Stderr = &result, &out
	if _, err := toolrun.Exec(test); err != nil {
		return out.String(), xerrors.Wrap(err, "conftest test")
	}
	rules, err := policyresult.Rules(filepath.Join(dir, "rego"), os.ReadFile)
	if err != nil {
		fmt.Fprintf(&out, "%v\n", err)

		return out.String(), err
	}
	entries, err := policyresult.Parse(result.Bytes())
	if err != nil {
		fmt.Fprintf(&out, "%v\n", err)

		return out.String(), err
	}
	msg, err := policyresult.Judge(rules, entries)
	if err != nil {
		fmt.Fprintf(&out, "%v\n", err)

		return out.String(), err
	}
	fmt.Fprintln(&out, msg)

	return out.String(), nil
}

// verifyKey は、Rego の木と Rego のテストに渡す宣言の中身から、conftest verify の結果を持ち回る鍵を作ります。
func verifyKey(dir string, files []regoFile) (string, error) {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%s\x00", f.rel, f.src)
	}
	for _, name := range []string{"decl.json", "layer-decl.json"} {
		raw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, name)))
		if err != nil {
			return "", xerrors.Wrapf(errRead, "%s: %v", name, err)
		}
		fmt.Fprintf(h, "%s\x00%s\x00", name, raw)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeText(p, body string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", filepath.Dir(p))
	}
	if err := atomicwrite.Apply(map[string]string{p: body}, 0o600); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", p)
	}

	return nil
}

func appendText(p, body string) error {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", p)
	}

	return writeText(p, string(raw)+body)
}
