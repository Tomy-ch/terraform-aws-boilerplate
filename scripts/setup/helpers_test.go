package main

import (
	"bytes"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/require"
)

const (
	testProject  = "tabp"
	testRegion   = "ap-northeast-1"
	testAccounts = `locals {
  member_accounts = {
    security      = aws_organizations_organizational_unit.top["Security"].id
    "log-archive" = aws_organizations_organizational_unit.top["Security"].id
  }
}
`
)

// fakeAWS は、aws の偽物。応答は、--output と --region を除いた引数を空白で繋いだものか、"<service> <operation>" で引く。
// 書き込みを1度でも起動した後は、afterWrite を先に引く。引けない呼び出しはテストを落とす。
// 起動した引数の列（--output・--region を含む）を覚える。
type fakeAWS struct {
	t          *testing.T
	mu         sync.Mutex
	responses  map[string]awsResult
	afterWrite map[string]awsResult
	written    bool
	calls      [][]string
}

func newFakeAWS(t *testing.T, responses map[string]awsResult) *fakeAWS {
	t.Helper()

	return &fakeAWS{t: t, responses: responses}
}

func (f *fakeAWS) fn() awsFunc {
	return func(args ...string) awsResult {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.calls = append(f.calls, args)

		bare := args
		if len(bare) >= 4 && bare[len(bare)-4] == "--output" {
			bare = bare[:len(bare)-4]
		}

		if !isRead(bare) {
			f.written = true
		}

		tables := []map[string]awsResult{f.responses}
		if f.written {
			tables = []map[string]awsResult{f.afterWrite, f.responses}
		}

		for _, m := range tables {
			if r, ok := m[strings.Join(bare, " ")]; ok {
				return r
			}

			if r, ok := m[op(bare)]; ok {
				return r
			}
		}

		f.t.Errorf("想定していない aws の呼び出し: %v", args)

		return awsResult{err: exitError(f.t), stderr: "unexpected"}
	}
}

func (f *fakeAWS) ops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = op(c)
	}

	return out
}

// writes は、起動した操作のうち読み取りでないものの並び。
func (f *fakeAWS) writes() []string {
	var out []string

	for _, o := range f.ops() {
		if !isRead(strings.Fields(o)) {
			out = append(out, o)
		}
	}

	return out
}

// exitError は、本物の *exec.ExitError。
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

func testMembers(t *testing.T) []memberAccount {
	t.Helper()

	m, err := readMemberAccounts([]byte(testAccounts), "accounts.tf")
	require.NoError(t, err)

	return m
}

func testOptions() options {
	return options{project: testProject, region: testRegion}
}

const paramName = "/tabp/organization-baseline/member-account-emails"

func paramResponse(value string) awsResult {
	b := strings.ReplaceAll(value, `"`, `\"`)

	return ok(`{"Parameter":{"Name":"` + paramName + `","Value":"` + b + `"}}`)
}

func hclPos() hcl.Pos {
	return hcl.InitialPos
}
