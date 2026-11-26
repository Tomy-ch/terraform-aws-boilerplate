package main

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testProject = "tabp"
	testRegion  = "ap-northeast-1"
	testUsecase = "uc"
	paramName   = "/tabp/organization-baseline/member-account-emails"

	usecaseMainTF = `locals {
	break_glass_role_name = "${var.project}-break-glass"
}
`
	ouMainTF = `locals {
	root_id = "r"
}

resource "aws_organizations_organizational_unit" "top" {
	for_each  = toset(["Security", "Workloads", "Sandbox"])
	name      = each.key
	parent_id = local.root_id
}

resource "aws_organizations_organizational_unit" "workloads" {
	for_each  = toset(["Prod", "NonProd"])
	name      = each.key
	parent_id = aws_organizations_organizational_unit.top["Workloads"].id
}
`
	accountsTF = `locals {
	member_accounts = {
		security      = aws_organizations_organizational_unit.top["Security"].id
		"log-archive" = aws_organizations_organizational_unit.top["Security"].id
	}
}

resource "aws_organizations_account" "this" {
	for_each = local.member_accounts

	lifecycle {
		precondition {
			condition     = can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", local.member_account_emails[each.key]))
			error_message = "x"
		}
	}
}
`
)

func fixtureFiles(usecaseMain, ouMain, accounts string) map[string]string {
	return map[string]string{
		path.Join(testUsecase, usecaseFile):  usecaseMain,
		path.Join(testUsecase, ouFile):       ouMain,
		path.Join(testUsecase, accountsFile): accounts,
	}
}

func readFiles(files map[string]string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		s, ok := files[name]
		if !ok {
			return nil, os.ErrNotExist
		}

		return []byte(s), nil
	}
}

func testOrg(t *testing.T) organization {
	t.Helper()

	org, err := readOrganization(readFiles(fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)), testUsecase, testProject)
	require.NoError(t, err)

	return org
}

// fakeAWS は、aws の偽物。応答は、--output と --region を除いた引数を空白で繋いだもので引く。
// 読み取りに限り "<service> <operation>" でも引ける。書き込みは引数の列ごとに応答を置かなければ落ちるので、
// 組んだ引数の誤りが黙って通らない。--cli-input-json の一時ファイルの中身は secrets に覚える。
// after は書き込みの引数の列ごとの、その書き込みが成功した後の応答で、成功した順に重ね、新しいものから引く。
// 書き込みごとに世界が進むので、読み直しが書き込みより前に動けば古い応答を読む。引けない呼び出しはテストを落とす。
type fakeAWS struct {
	t         *testing.T
	mu        sync.Mutex
	responses map[string]awsResult
	after     map[string]map[string]awsResult
	overlays  []map[string]awsResult
	calls     [][]string
	secrets   []string
}

// onWrite は、書き込み w が成功した後の応答 m を置く。
func (f *fakeAWS) onWrite(w string, m map[string]awsResult) {
	if f.after == nil {
		f.after = map[string]map[string]awsResult{}
	}

	f.after[w] = m
}

func newFakeAWS(t *testing.T, responses map[string]awsResult) *fakeAWS {
	t.Helper()

	if responses == nil {
		responses = map[string]awsResult{}
	}

	return &fakeAWS{t: t, responses: responses}
}

func (f *fakeAWS) fn() awsFunc {
	return func(args ...string) awsResult {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.calls = append(f.calls, args)

		bare := stripCommon(args)

		if n := len(bare); n >= 2 && bare[n-2] == "--cli-input-json" {
			name := strings.TrimPrefix(bare[n-1], "file://")

			info, err := os.Stat(name)
			require.NoError(f.t, err)
			require.Equal(f.t, os.FileMode(0o600), info.Mode().Perm())

			b, err := os.ReadFile(name)
			require.NoError(f.t, err)

			f.secrets = append(f.secrets, string(b))
			bare = bare[:n-2]
		}

		r, ok := f.lookup(bare)
		if !ok {
			f.t.Errorf("想定していない aws の呼び出し: %v", args)

			return awsResult{err: exitError(f.t), stderr: "unexpected"}
		}

		if m, ok := f.after[strings.Join(bare, " ")]; ok && !isRead(bare) && r.err == nil {
			f.overlays = append(f.overlays, m)
		}

		return r
	}
}

func stripCommon(args []string) []string {
	if len(args) >= 4 && args[len(args)-4] == "--output" {
		return args[:len(args)-4]
	}

	return args
}

func (f *fakeAWS) lookup(bare []string) (awsResult, bool) {
	tables := make([]map[string]awsResult, 0, len(f.overlays)+1)
	for i := len(f.overlays) - 1; i >= 0; i-- {
		tables = append(tables, f.overlays[i])
	}

	for _, m := range append(tables, f.responses) {
		if r, ok := m[strings.Join(bare, " ")]; ok {
			return r, true
		}

		if r, ok := m[op(bare)]; ok && isRead(bare) {
			return r, true
		}
	}

	return awsResult{}, false
}

// ops は、起動した操作（"<service> <operation>"）の並び。
func (f *fakeAWS) ops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = op(c)
	}

	return out
}

// writes は、起動した書き込みの引数の列（--output・--region と --cli-input-json を除く）を空白で繋いだものの並び。
func (f *fakeAWS) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string

	for _, c := range f.calls {
		bare := stripCommon(c)
		if n := len(bare); n >= 2 && bare[n-2] == "--cli-input-json" {
			bare = bare[:n-2]
		}

		if !isRead(bare) {
			out = append(out, strings.Join(bare, " "))
		}
	}

	return out
}

// joined は、起動した引数の列（--output・--region を除く）を空白で繋いだものの並び。
func (f *fakeAWS) joined() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = strings.Join(stripCommon(c), " ")
	}

	return out
}

func exitError(t *testing.T) error {
	t.Helper()

	err := exec.CommandContext(t.Context(), "false").Run()
	require.Error(t, err)

	return err
}

func ok(stdout string) awsResult {
	return awsResult{stdout: []byte(stdout)}
}

func fail(t *testing.T, stderr string) awsResult {
	t.Helper()

	return awsResult{err: exitError(t), stderr: stderr}
}

func profileEnv(k string) string {
	if k == "AWS_PROFILE" {
		return "p"
	}

	return ""
}

func testClient(f *fakeAWS, writes ...string) *client {
	return &client{aws: f.fn(), region: testRegion, writes: writes}
}

func tty(input string) (*terminal, *bytes.Buffer) {
	var out bytes.Buffer

	return newTerminal(strings.NewReader(input), &out, true), &out
}

func testOptions() options {
	return options{project: testProject, region: testRegion}
}

func paramResponse(value string) awsResult {
	b := strings.ReplaceAll(value, `"`, `\"`)

	return ok(`{"Parameter":{"Name":"` + paramName + `","Value":"` + b + `"}}`)
}

func putParam(value string) string {
	return "ssm put-parameter --name " + paramName + " --type String --overwrite --value " + value
}
