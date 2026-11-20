package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const awsProvider = "registry.terraform.io/hashicorp/aws"

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))

	return p
}

func writeListed(t *testing.T, dir string, paths ...string) string {
	t.Helper()
	var body strings.Builder
	for _, p := range paths {
		body.WriteString(p + "\n")
	}

	return writeFile(t, dir, "locks.found", body.String())
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(b)
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("prepare と merge を選ぶ", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		lock := writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:a="))
		dir := filepath.Join(base, "schema")
		var stdout, stderr bytes.Buffer
		require.NoError(t, run([]string{"prepare", "-provider", awsProvider, "-out", dir, "-listed", writeListed(t, base, lock), lock}, &stdout, &stderr))
		assert.Equal(t, filepath.Join(dir, "6.2.0")+"\n", stdout.String())

		writeFile(t, dir, "6.2.0/raw.json", rawJSON(awsProvider, resourceX))
		out := filepath.Join(base, "schema.json")
		stdout.Reset()
		require.NoError(t, run([]string{"merge", "-provider", awsProvider, "-dir", dir, "-out", out}, &stdout, &stderr))
		assert.Empty(t, stdout.String(), "merge は stdout に何も出さない")
		assert.FileExists(t, out)
	})

	t.Run("plan-deprecated を選ぶ", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		in := writeFile(t, base, "a.jsonl", planJSONL(providerBody(awsProvider, resourceX)))
		out := filepath.Join(base, "provider-schema.json")
		var stdout, stderr bytes.Buffer
		require.NoError(t, run([]string{"plan-deprecated", "-out", out, in}, &stdout, &stderr))
		assert.Empty(t, stdout.String(), "plan-deprecated は stdout に何も出さない")
		assert.FileExists(t, out)
	})

	t.Run("サブコマンドが無いか知らなければ errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{"引数が無い": nil, "知らないサブコマンド": {"fetch"}} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, run(args, &bytes.Buffer{}, &bytes.Buffer{}), errUsage)
			})
		}
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want int
	}{
		"エラーが無い":               {want: 0},
		"lock が0件":             {err: xerrors.Wrap(errNoLocks, "x"), want: 2},
		"引数の誤り":                {err: errUsage, want: 1},
		"lock の構文の誤り":          {err: xerrors.Wrap(errLockSyntax, "x"), want: 1},
		"Provider の block が重複": {err: xerrors.Wrap(errDuplicateBlock, "x"), want: 1},
		"版を取れない":               {err: xerrors.Wrap(errVersion, "x"), want: 1},
		"lock の一覧を解釈できない":      {err: xerrors.Wrap(errLockList, "x"), want: 1},
		"lock の集合の不一致":         {err: xerrors.Wrap(errCountMismatch, "x"), want: 1},
		"版が1つも無い":              {err: xerrors.Wrap(errNoVersions, "x"), want: 1},
		"版の一覧を解釈できない":          {err: xerrors.Wrap(errVersionList, "x"), want: 1},
		"schema が無い":           {err: xerrors.Wrap(errSchemaMissing, "x"), want: 1},
		"schema を読めない":         {err: xerrors.Wrap(errSchemaMalformed, "x"), want: 1},
		"resource の型が無い":       {err: xerrors.Wrap(errNoResources, "x"), want: 1},
		"plan が0件":             {err: xerrors.Wrap(errNoPlans, "x"), want: 2},
		"plan を解釈できない":         {err: xerrors.Wrap(errPlanMalformed, "x"), want: 1},
		"plan の型が schema に無い":  {err: xerrors.Wrap(errPlanTypeMissing, "x"), want: 1},
		"書ける引数を持つ型が無い":         {err: xerrors.Wrap(errNoArguments, "x"), want: 1},
		"センチネルでないエラーは既定の 1":    {err: fs.ErrNotExist, want: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}
