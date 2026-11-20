package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

const randomBlock = "provider \"registry.terraform.io/hashicorp/random\" {\n  version = \"3.6.0\"\n}\n"

func awsBlock(version, hash string) string {
	return fmt.Sprintf("provider %q {\n  version     = %q\n  constraints = \">= 6.0.0\"\n  hashes = [\n    %q,\n  ]\n}\n", awsProvider, version, hash)
}

// awsLock は、AWS Provider の block を別の Provider の block で挟んだ lock を返す。
func awsLock(version, hash string) string {
	return "# This file is maintained automatically by \"terraform init\".\n\n" + randomBlock + "\n" + awsBlock(version, hash) + "\n" + randomBlock
}

func Test_providerAddr_source(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		addr providerAddr
		want string
	}{
		"既定のレジストリはホストを省く": {
			addr: providerAddr{full: awsProvider, host: "registry.terraform.io", namespace: "hashicorp", name: "aws"}, want: "hashicorp/aws",
		},
		"ほかのホストはアドレスのまま": {
			addr: providerAddr{full: "example.com/acme/aws", host: "example.com", namespace: "acme", name: "aws"}, want: "example.com/acme/aws",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.addr.source())
		})
	}
}

func Test_parseProvider(t *testing.T) {
	t.Parallel()

	t.Run("3段に分ける", func(t *testing.T) {
		t.Parallel()
		got, err := parseProvider(awsProvider)
		require.NoError(t, err)
		assert.Equal(t, providerAddr{full: awsProvider, host: "registry.terraform.io", namespace: "hashicorp", name: "aws"}, got)
	})

	t.Run("形の違うアドレスは errUsage", func(t *testing.T) {
		t.Parallel()
		for name, addr := range map[string]string{
			"段が足りない":   "hashicorp/aws",
			"段が多い":     "registry.terraform.io/hashicorp/aws/x",
			"空の段":      "registry.terraform.io//aws",
			"引用符を含む":   `registry.terraform.io/hashicorp/a"ws`,
			"改行を含む":    "registry.terraform.io/hashicorp/aws\n",
			"親を指す段":    "registry.terraform.io/../aws",
			"空のアドレス":   "",
			"空白を含む段":   "registry.terraform.io/hashi corp/aws",
			"区切りで始まる段": "registry.terraform.io/-hashicorp/aws",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := parseProvider(addr)
				require.ErrorIs(t, err, errUsage)
			})
		}
	})
}

func Test_parseLock(t *testing.T) {
	t.Parallel()

	t.Run("指定の Provider の block だけを版とともに取り出す", func(t *testing.T) {
		t.Parallel()
		got, err := parseLock("l.hcl", []byte(awsLock("6.2.0", "h1:a=")), awsProvider)
		require.NoError(t, err)
		assert.Equal(t, lockResult{found: true, version: "6.2.0", block: []byte(awsBlock("6.2.0", "h1:a="))}, got)
	})

	t.Run("pre-release の版を受ける", func(t *testing.T) {
		t.Parallel()
		got, err := parseLock("l.hcl", []byte(awsBlock("6.0.0-beta.1", "h1:a=")), awsProvider)
		require.NoError(t, err)
		assert.Equal(t, "6.0.0-beta.1", got.version)
	})

	t.Run("Provider の block を持たなければ found が false", func(t *testing.T) {
		t.Parallel()
		for name, src := range map[string]string{
			"別の Provider だけ": randomBlock,
			"空の lock":        "",
			"ラベルの数が違う block": "provider \"registry.terraform.io/hashicorp/aws\" \"x\" {\n  version = \"6.2.0\"\n}\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, err := parseLock("l.hcl", []byte(src), awsProvider)
				require.NoError(t, err)
				assert.Equal(t, lockResult{}, got)
			})
		}
	})

	t.Run("読めない lock はエラー", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want error
		}{
			"構文の誤り":           {src: "provider \"" + awsProvider + "\" {\n", want: errLockSyntax},
			"block が2つ":       {src: awsBlock("6.2.0", "h1:a=") + awsBlock("6.3.0", "h1:b="), want: errDuplicateBlock},
			"version が無い":     {src: "provider \"" + awsProvider + "\" {\n  constraints = \">= 6.0.0\"\n}\n", want: errVersion},
			"version が参照":     {src: "provider \"" + awsProvider + "\" {\n  version = var.v\n}\n", want: errVersion},
			"version が補間":     {src: "provider \"" + awsProvider + "\" {\n  version = \"${var.v}\"\n}\n", want: errVersion},
			"version が数":      {src: "provider \"" + awsProvider + "\" {\n  version = 6\n}\n", want: errVersion},
			"version が版の形でない": {src: awsBlock("6.2", "h1:a="), want: errVersion},
			"version がパスを含む":  {src: awsBlock("../6.2.0", "h1:a="), want: errVersion},
			"version が空":      {src: awsBlock("", "h1:a="), want: errVersion},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := parseLock("l.hcl", []byte(tt.src), awsProvider)
				require.ErrorIs(t, err, tt.want)
			})
		}
	})
}

func Test_readListed(t *testing.T) {
	t.Parallel()

	t.Run("1行に1件を正規化して読む", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "locks.found", "./a/.terraform.lock.hcl\nb//.terraform.lock.hcl\n")
		got, err := readListed(p)
		require.NoError(t, err)
		assert.Equal(t, []string{"a/.terraform.lock.hcl", "b/.terraform.lock.hcl"}, got)
	})

	t.Run("末尾の改行が無くても読む", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "locks.found", "a/.terraform.lock.hcl")
		got, err := readListed(p)
		require.NoError(t, err)
		assert.Equal(t, []string{"a/.terraform.lock.hcl"}, got)
	})

	t.Run("空のファイルは空の一覧", func(t *testing.T) {
		t.Parallel()
		got, err := readListed(writeFile(t, t.TempDir(), "locks.found", ""))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("形の誤りは errLockList", func(t *testing.T) {
		t.Parallel()
		for name, body := range map[string]string{
			"途中の空行":   "a\n\nb\n",
			"末尾の空行":   "a\n\n",
			"改行だけ":    "\n",
			"重複":      "a\nb\na\n",
			"正規化して重複": "a\n./a\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := readListed(writeFile(t, t.TempDir(), "locks.found", body))
				require.ErrorIs(t, err, errLockList)
			})
		}
	})

	t.Run("読めなければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := readListed(filepath.Join(t.TempDir(), "missing.txt"))
		require.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func Test_reconcile(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args, listed []string
		want         string
	}{
		"一致":           {args: []string{"a", "b"}, listed: []string{"b", "a"}},
		"正規化で一致する":     {args: []string{"./a", "b/../c"}, listed: []string{"a", "c"}},
		"引数にだけある":      {args: []string{"a", "c", "b"}, listed: []string{"a"}, want: "引数にだけある: [b c]、独立の走査にだけある: []"},
		"listed にだけある": {args: []string{"a"}, listed: []string{"a", "c", "b"}, want: "引数にだけある: []、独立の走査にだけある: [b c]"},
		"両向き":          {args: []string{"a", "b"}, listed: []string{"a", "c"}, want: "引数にだけある: [b]、独立の走査にだけある: [c]"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := reconcile(tt.args, tt.listed)
			if tt.want == "" {
				require.NoError(t, err)

				return
			}
			require.ErrorIs(t, err, errCountMismatch)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func Test_versionsTF(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		addr   string
		source string
	}{
		"既定のレジストリはホストを省いた source": {addr: awsProvider, source: "hashicorp/aws"},
		"ほかのホストはアドレスのままの source":  {addr: "example.com/acme/aws", source: "example.com/acme/aws"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, err := parseProvider(tt.addr)
			require.NoError(t, err)
			assert.Equal(t, `terraform {
  required_providers {
    aws = {
      source  = "`+tt.source+`"
      version = "6.2.0"
    }
  }
}
`, versionsTF(p, "6.2.0"))
		})
	}
}

func Test_needsFetch(t *testing.T) {
	t.Parallel()
	lock := []byte(awsBlock("6.2.0", "h1:a="))

	tests := map[string]struct {
		files map[string]string
		want  bool
	}{
		"何も無い":              {files: nil, want: true},
		"lock も schema も同じ": {files: map[string]string{lockName: string(lock), rawName: "{}"}, want: false},
		"lock が違う":          {files: map[string]string{lockName: awsBlock("6.2.0", "h1:b="), rawName: "{}"}, want: true},
		"schema が空":         {files: map[string]string{lockName: string(lock), rawName: ""}, want: true},
		"schema が無い":        {files: map[string]string{lockName: string(lock)}, want: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for n, body := range tt.files {
				writeFile(t, dir, n, body)
			}
			got, err := needsFetch(dir, lock)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("既存の lock を読めなければエラー", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(dir, lockName), 0o750))
		_, err := needsFetch(dir, lock)
		require.ErrorIs(t, err, syscall.EISDIR)
	})
}

func Test_prepare(t *testing.T) {
	t.Parallel()

	t.Run("版ごとに lock と versions.tf を置き、取り直す版を出す", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		locks := []string{
			writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:a=")),
			writeFile(t, base, "b/.terraform.lock.hcl", randomBlock),
			writeFile(t, base, "c/.terraform.lock.hcl", awsLock("6.2.0", "h1:other=")),
			writeFile(t, base, "d/.terraform.lock.hcl", awsLock("5.9.0", "h1:d=")),
		}
		dir := filepath.Join(base, "schema")
		var stdout, stderr bytes.Buffer
		require.NoError(t, prepare(append([]string{"-provider", awsProvider, "-out", dir, "-listed", writeListed(t, base, locks...)}, locks...), &stdout, &stderr))

		assert.Equal(t, filepath.Join(dir, "6.2.0")+"\n"+filepath.Join(dir, "5.9.0")+"\n", stdout.String())
		assert.Equal(t, "6.2.0\n5.9.0\n", readFile(t, filepath.Join(dir, versionsName)))
		assert.Equal(t, awsBlock("6.2.0", "h1:a="), readFile(t, filepath.Join(dir, "6.2.0", lockName)), "その版を持つ最初の lock の block")
		assert.Equal(t, awsBlock("5.9.0", "h1:d="), readFile(t, filepath.Join(dir, "5.9.0", lockName)))
		assert.Contains(t, readFile(t, filepath.Join(dir, "5.9.0", "versions.tf")), `version = "5.9.0"`)
		assert.Equal(t, "provider-schema prepare: "+awsProvider+" の版 6.2.0 5.9.0（lock 4 件、うち Provider の無い lock 1 件）、schema の取り直し 2 版\n", stderr.String())
	})

	t.Run("lock が同じで schema があれば取り直さず、lock が変われば schema を消す", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		a := writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:a="))
		b := writeFile(t, base, "b/.terraform.lock.hcl", awsLock("5.9.0", "h1:b="))
		dir := filepath.Join(base, "schema")
		args := []string{"-provider", awsProvider, "-out", dir, "-listed", writeListed(t, base, a, b), a, b}
		require.NoError(t, prepare(args, &bytes.Buffer{}, &bytes.Buffer{}))
		writeFile(t, dir, "6.2.0/raw.json", "{}")
		writeFile(t, dir, "5.9.0/raw.json", "{}")

		var stdout bytes.Buffer
		require.NoError(t, prepare(args, &stdout, &bytes.Buffer{}))
		assert.Empty(t, stdout.String())
		assert.FileExists(t, filepath.Join(dir, "6.2.0", rawName))

		writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:changed="))
		stdout.Reset()
		require.NoError(t, prepare(args, &stdout, &bytes.Buffer{}))
		assert.Equal(t, filepath.Join(dir, "6.2.0")+"\n", stdout.String())
		assert.NoFileExists(t, filepath.Join(dir, "6.2.0", rawName))
		assert.Equal(t, awsBlock("6.2.0", "h1:changed="), readFile(t, filepath.Join(dir, "6.2.0", lockName)))
		assert.FileExists(t, filepath.Join(dir, "5.9.0", rawName))
	})

	t.Run("引数が足りなければ errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"-provider が無い":   {"-out", "d", "-listed", "f", "l"},
			"-out が無い":        {"-provider", awsProvider, "-listed", "f", "l"},
			"-listed が無い":     {"-provider", awsProvider, "-out", "d", "l"},
			"-provider の形が違う": {"-provider", "aws", "-out", "d", "-listed", "f", "l"},
			"知らないフラグ":         {"-x", "-provider", awsProvider, "-out", "d", "-listed", "f", "l"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, prepare(args, &bytes.Buffer{}, &bytes.Buffer{}), errUsage)
			})
		}
	})

	t.Run("失敗した実行は何も書かない", func(t *testing.T) {
		t.Parallel()

		good := awsLock("6.2.0", "h1:a=")
		tests := map[string]struct {
			locks map[string]string
			want  error
		}{
			"lock が0件":          {want: errNoLocks},
			"構文の誤り":             {locks: map[string]string{"a": good, "b": "provider \"x\" {\n"}, want: errLockSyntax},
			"version の無い block": {locks: map[string]string{"a": good, "b": "provider \"" + awsProvider + "\" {\n}\n"}, want: errVersion},
			"版の形でない version":    {locks: map[string]string{"a": good, "b": awsBlock("6.2.0/../../x", "h1:a=")}, want: errVersion},
			"Provider の版が1つも無い": {locks: map[string]string{"a": randomBlock, "b": ""}, want: errNoVersions},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base := t.TempDir()
				var paths []string
				for _, n := range []string{"a", "b"} {
					if body, ok := tt.locks[n]; ok {
						paths = append(paths, writeFile(t, base, n+"/.terraform.lock.hcl", body))
					}
				}
				dir := filepath.Join(base, "schema")
				var stdout bytes.Buffer
				listed := writeListed(t, base, paths...)
				err := prepare(append([]string{"-provider", awsProvider, "-out", dir, "-listed", listed}, paths...), &stdout, &bytes.Buffer{})
				require.ErrorIs(t, err, tt.want)
				assert.NoDirExists(t, dir)
				assert.Empty(t, stdout.String())
			})
		}
	})

	t.Run("lock を読めなければ失敗し、何も書かない", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		dir := filepath.Join(base, "schema")
		missing := filepath.Join(base, "missing.hcl")
		err := prepare([]string{"-provider", awsProvider, "-out", dir, "-listed", writeListed(t, base, missing), missing}, &bytes.Buffer{}, &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.NoDirExists(t, dir)
	})

	t.Run("-listed を読めなければ失敗し、何も書かない", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		lock := writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:a="))
		dir := filepath.Join(base, "schema")
		var stdout bytes.Buffer
		err := prepare([]string{"-provider", awsProvider, "-out", dir, "-listed", filepath.Join(base, "missing.txt"), lock}, &stdout, &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.NoDirExists(t, dir)
		assert.Empty(t, stdout.String())
	})

	t.Run("lock の一覧が引数と食い違えば失敗し、何も書かない", func(t *testing.T) {
		t.Parallel()

		// listed の各行の {base} は、テストごとの一時ディレクトリに置き換える。
		tests := map[string]struct {
			listed string
			want   error
		}{
			"引数にだけある":  {listed: "{base}/a/.terraform.lock.hcl\n", want: errCountMismatch},
			"一覧にだけある":  {listed: "{base}/a/.terraform.lock.hcl\n{base}/b/.terraform.lock.hcl\n{base}/c/.terraform.lock.hcl\n", want: errCountMismatch},
			"一覧に空行がある": {listed: "{base}/a/.terraform.lock.hcl\n\n{base}/b/.terraform.lock.hcl\n", want: errLockList},
			"一覧に重複がある": {listed: "{base}/a/.terraform.lock.hcl\n{base}/b/.terraform.lock.hcl\n{base}/./a/.terraform.lock.hcl\n", want: errLockList},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base := t.TempDir()
				locks := []string{
					writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:a=")),
					writeFile(t, base, "b/.terraform.lock.hcl", awsLock("5.9.0", "h1:b=")),
				}
				listed := writeFile(t, base, "locks.found", strings.ReplaceAll(tt.listed, "{base}", base))
				dir := filepath.Join(base, "schema")
				var stdout bytes.Buffer
				err := prepare(append([]string{"-provider", awsProvider, "-out", dir, "-listed", listed}, locks...), &stdout, &bytes.Buffer{})
				require.ErrorIs(t, err, tt.want)
				assert.NoDirExists(t, dir)
				assert.Empty(t, stdout.String())
			})
		}
	})

	t.Run("書き込めなければ失敗し、取り直す版を出さない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読み取り専用ディレクトリへも書けるため検証できない")
		base := t.TempDir()
		lock := writeFile(t, base, "a/.terraform.lock.hcl", awsLock("6.2.0", "h1:a="))
		dir := filepath.Join(base, "schema")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "6.2.0"), 0o750))
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		var stdout bytes.Buffer
		err := prepare([]string{"-provider", awsProvider, "-out", dir, "-listed", writeListed(t, base, lock), lock}, &stdout, &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Empty(t, stdout.String())
		assert.NoFileExists(t, filepath.Join(dir, versionsName))
	})
}
