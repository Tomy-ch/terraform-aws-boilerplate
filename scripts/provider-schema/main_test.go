package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(b)
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("prepare と merge を選び、間を fetch がつなぐ", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:a="))
		gitIn(t, base, "init", "-q")
		gitIn(t, base, "add", "a")
		found := writeFile(t, base, "found.txt", "a/.terraform.lock.hcl\n")
		dir := filepath.Join(base, "schema")
		require.NoError(t, run([]string{"prepare", "-provider", awsProvider, "-out", dir, "-base", base, "-listed", found, "a"}, &bytes.Buffer{}))
		assert.Equal(t, filepath.Join(dir, "6.2.0")+"\n", readFile(t, filepath.Join(dir, fetchName)))

		require.NoError(t, fetch([]string{"-dir", dir}, &bytes.Buffer{}, fakeTerraform(t, 0, 0, rawJSON(awsProvider, resourceX))))
		assert.FileExists(t, filepath.Join(dir, "6.2.0", rawName))
		require.ErrorIs(t, run([]string{"fetch"}, &bytes.Buffer{}), errUsage, "fetch を選ぶ")

		out := filepath.Join(base, "schema.json")
		require.NoError(t, run([]string{"merge", "-provider", awsProvider, "-dir", dir, "-out", out}, &bytes.Buffer{}))
		assert.FileExists(t, out)
	})

	t.Run("plan-deprecated を選ぶ", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		in := writeFile(t, base, "a.jsonl", planJSONL(providerBody(awsProvider, resourceX)))
		out := filepath.Join(base, "provider-schema.json")
		require.NoError(t, run([]string{"plan-deprecated", "-out", out, in}, &bytes.Buffer{}))
		assert.FileExists(t, out)
	})

	t.Run("サブコマンドが無いか知らなければ errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{"引数が無い": nil, "知らないサブコマンド": {"schema"}} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, run(args, &bytes.Buffer{}), errUsage)
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
