package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rulesSHA256 は、rulesMembers の terraform/ の rule を写したディレクトリで
// `find terraform -type f \( -name '*.yaml' -o -name '*.yml' \) | LC_ALL=C sort | xargs sha256sum | sha256sum` が出す値である。
const rulesSHA256 = "8a0a4ee46153e059e51eff2a42d2e4878c8c504ef5e54d4f33c1e8e462914893"

type member struct {
	name string
	body string
	typ  byte
}

// rulesMembers は、semgrep-rules の tarball の形の member である。terraform/ の rule のほかに、rule のテスト用の
// サンプルと、ほかの言語の rule を持つ。
var rulesMembers = []member{
	{name: "semgrep-rules-x/", typ: tar.TypeDir},
	{name: "semgrep-rules-x/terraform/aws/a.yaml", body: "rules: []\n"},
	{name: "semgrep-rules-x/terraform/aws/a.tf", body: "sample\n"},
	{name: "semgrep-rules-x/terraform/aws/B.yml", body: "x: 1\n"},
	{name: "semgrep-rules-x/terraform/z/c.yaml", body: "z\n"},
	{name: "semgrep-rules-x/python/p.yaml", body: "p\n"},
	{name: "semgrep-rules-x/README.md", body: "r\n"},
}

func tarball(t *testing.T, members []member) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, m := range members {
		h := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.body)), Typeflag: m.typ}
		if m.typ == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
			h.Linkname = m.body
		}
		require.NoError(t, tw.WriteHeader(h))
		if h.Typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(m.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	p := filepath.Join(t.TempDir(), "rules.tar.gz")
	require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
	return p
}

func ruleMembers(n int) []member {
	ms := make([]member, 0, n)
	for i := range n {
		ms = append(ms, member{name: fmt.Sprintf("semgrep-rules-x/terraform/r%04d.yaml", i), body: "x\n"})
	}
	return ms
}

// digestOf は、members のうち terraform/ の rule を写したときに stageRules が照合する値を返す。
func digestOf(members []member) string {
	sums := map[string]string{}
	for _, m := range members {
		if ruleMember.MatchString(m.name) {
			_, rel, _ := strings.Cut(m.name, "/")
			sum := sha256.Sum256([]byte(m.body))
			sums[rel] = hex.EncodeToString(sum[:])
		}
	}
	return rulesDigest(sums)
}

// files は dir の下の通常のファイルの、dir からの `/` 区切りの相対パスを返す。
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		require.NoError(t, err)
		if !e.IsDir() {
			rel, err := filepath.Rel(dir, p)
			require.NoError(t, err)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	}))
	return out
}

func Test_stageRules(t *testing.T) {
	t.Parallel()

	t.Run("terraform/ の rule だけを写し、宣言した値と照合する", func(t *testing.T) {
		t.Parallel()
		dst := t.TempDir()
		require.NoError(t, stageRules(tarball(t, rulesMembers), dst, rulesSHA256))
		assert.ElementsMatch(t, []string{"terraform/aws/a.yaml", "terraform/aws/B.yml", "terraform/z/c.yaml"}, files(t, dst))
		raw, err := os.ReadFile(filepath.Join(dst, "terraform", "aws", "a.yaml"))
		require.NoError(t, err)
		assert.Equal(t, "rules: []\n", string(raw))
	})

	t.Run("中身が宣言と違えば errInput", func(t *testing.T) {
		t.Parallel()
		changed := append([]member{}, rulesMembers...)
		changed[1].body = "rules: [x]\n"
		err := stageRules(tarball(t, changed), t.TempDir(), rulesSHA256)
		require.ErrorIs(t, err, errInput)
		assert.Contains(t, err.Error(), "宣言と一致しません")
	})

	t.Run("rule が1つ増えても errInput", func(t *testing.T) {
		t.Parallel()
		more := append(append([]member{}, rulesMembers...), member{name: "semgrep-rules-x/terraform/new.yaml", body: "n\n"})
		require.ErrorIs(t, stageRules(tarball(t, more), t.TempDir(), rulesSHA256), errInput)
	})

	t.Run("terraform/ の rule が0件なら errInput", func(t *testing.T) {
		t.Parallel()
		err := stageRules(tarball(t, []member{{name: "semgrep-rules-x/python/p.yaml", body: "p\n"}}), t.TempDir(), rulesSHA256)
		require.ErrorIs(t, err, errInput)
		assert.Contains(t, err.Error(), "rule がありません")
	})

	for name, m := range map[string]member{
		"symlink":     {name: "semgrep-rules-x/terraform/l.yaml", body: "../../../etc/passwd", typ: tar.TypeSymlink},
		"hard link":   {name: "semgrep-rules-x/terraform/h.yaml", body: "semgrep-rules-x/terraform/aws/a.yaml", typ: tar.TypeLink},
		"親へ出る名前":      {name: "semgrep-rules-x/terraform/../../x.yaml", body: "x\n"},
		"ディレクトリの形の名前": {name: "semgrep-rules-x/terraform/d.yaml", typ: tar.TypeDir},
	} {
		t.Run(name+"の rule は errInput", func(t *testing.T) {
			t.Parallel()
			dst := t.TempDir()
			err := stageRules(tarball(t, append(append([]member{}, rulesMembers...), m)), dst, rulesSHA256)
			require.ErrorIs(t, err, errInput)
			assert.NoFileExists(t, filepath.Join(filepath.Dir(dst), "x.yaml"))
		})
	}

	t.Run("上限を超える rule は errInput", func(t *testing.T) {
		t.Parallel()
		big := member{name: "semgrep-rules-x/terraform/big.yaml", body: strings.Repeat("a", maxRuleSize+1)}
		require.ErrorIs(t, stageRules(tarball(t, []member{big}), t.TempDir(), rulesSHA256), errInput)
	})

	t.Run("rule の件数は上限ちょうどまで写せる", func(t *testing.T) {
		t.Parallel()
		ms := ruleMembers(maxRules)
		require.NoError(t, stageRules(tarball(t, ms), t.TempDir(), digestOf(ms)))
	})

	t.Run("rule の件数が上限を1つ超えれば errInput", func(t *testing.T) {
		t.Parallel()
		ms := ruleMembers(maxRules + 1)
		err := stageRules(tarball(t, ms), t.TempDir(), digestOf(ms))
		require.ErrorIs(t, err, errInput)
		assert.Contains(t, err.Error(), "件を超えています")
	})

	t.Run("rule の合計は上限ちょうどまで写せる", func(t *testing.T) {
		t.Parallel()
		ms := []member{
			{name: "semgrep-rules-x/terraform/a.yaml", body: strings.Repeat("a", maxRuleSize)},
			{name: "semgrep-rules-x/terraform/b.yaml", body: strings.Repeat("b", maxRulesBytes-maxRuleSize)},
		}
		require.NoError(t, stageRules(tarball(t, ms), t.TempDir(), digestOf(ms)))
	})

	t.Run("rule の合計が上限を1バイト超えれば errInput", func(t *testing.T) {
		t.Parallel()
		ms := []member{
			{name: "semgrep-rules-x/terraform/a.yaml", body: strings.Repeat("a", maxRuleSize)},
			{name: "semgrep-rules-x/terraform/b.yaml", body: strings.Repeat("b", maxRulesBytes-maxRuleSize)},
			{name: "semgrep-rules-x/terraform/c.yaml", body: "c"},
		}
		err := stageRules(tarball(t, ms), t.TempDir(), digestOf(ms))
		require.ErrorIs(t, err, errInput)
		assert.Contains(t, err.Error(), "バイトを超えています")
	})

	t.Run("同じ名前の rule が2つあれば errInput", func(t *testing.T) {
		t.Parallel()
		dup := append(append([]member{}, rulesMembers...), rulesMembers[1])
		require.ErrorIs(t, stageRules(tarball(t, dup), t.TempDir(), rulesSHA256), errInput)
	})

	t.Run("tarball が無ければ errInput", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, stageRules(filepath.Join(t.TempDir(), "none.tar.gz"), t.TempDir(), rulesSHA256), errInput)
	})

	t.Run("gzip でなければ errInput", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"x.tar.gz": "not gzip"})
		require.ErrorIs(t, stageRules(filepath.Join(dir, "x.tar.gz"), t.TempDir(), rulesSHA256), errInput)
	})

	t.Run("tar として壊れていれば errInput", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, err := gz.Write(bytes.Repeat([]byte{1}, 1024))
		require.NoError(t, err)
		require.NoError(t, gz.Close())
		p := filepath.Join(t.TempDir(), "x.tar.gz")
		require.NoError(t, os.WriteFile(p, buf.Bytes(), 0o600))
		require.ErrorIs(t, stageRules(p, t.TempDir(), rulesSHA256), errInput)
	})
}

func Test_ruleRel(t *testing.T) {
	t.Parallel()

	t.Run("最上位のディレクトリを外す", func(t *testing.T) {
		t.Parallel()
		got, err := ruleRel(&tar.Header{Name: "semgrep-rules-x/terraform/aws/a.yaml", Typeflag: tar.TypeReg})
		require.NoError(t, err)
		assert.Equal(t, "terraform/aws/a.yaml", got)
	})

	for name, h := range map[string]*tar.Header{
		"symlink": {Name: "r/terraform/a.yaml", Typeflag: tar.TypeSymlink},
		"親へ出る":    {Name: "r/terraform/../../a.yaml", Typeflag: tar.TypeReg},
		"途中に ..":  {Name: "r/terraform/x/../a.yaml", Typeflag: tar.TypeReg},
		"絶対パスになる": {Name: "r//terraform/a.yaml", Typeflag: tar.TypeReg},
		"ディレクトリ":  {Name: "r/terraform/a.yaml", Typeflag: tar.TypeDir},
		"文字デバイス":  {Name: "r/terraform/a.yaml", Typeflag: tar.TypeChar},
	} {
		t.Run(name+"は errInput", func(t *testing.T) {
			t.Parallel()
			_, err := ruleRel(h)
			require.ErrorIs(t, err, errInput)
		})
	}
}

func Test_writeRule(t *testing.T) {
	t.Parallel()

	t.Run("中身を書き、sha256 を返す", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "terraform", "a.yaml")
		sum, n, err := writeRule(strings.NewReader("rules: []\n"), p)
		require.NoError(t, err)
		assert.Equal(t, "e0dfa70eb69d47fe9cb2be8a4fcd53e74cf7fe26fcbb1f06912b57a9c028e4e0", sum)
		assert.Equal(t, int64(10), n)
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "rules: []\n", string(raw))
	})

	t.Run("上限ちょうどは書ける", func(t *testing.T) {
		t.Parallel()
		_, n, err := writeRule(strings.NewReader(strings.Repeat("a", maxRuleSize)), filepath.Join(t.TempDir(), "a.yaml"))
		require.NoError(t, err)
		assert.Equal(t, int64(maxRuleSize), n)
	})

	t.Run("上限を1バイト超えると errInput", func(t *testing.T) {
		t.Parallel()
		_, _, err := writeRule(strings.NewReader(strings.Repeat("a", maxRuleSize+1)), filepath.Join(t.TempDir(), "a.yaml"))
		require.ErrorIs(t, err, errInput)
		assert.Contains(t, err.Error(), "バイトを超えています")
	})

	t.Run("既に在るファイルは上書きせずに errInput", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"a.yaml": "old"})
		_, _, err := writeRule(strings.NewReader("new"), filepath.Join(dir, "a.yaml"))
		require.ErrorIs(t, err, errInput)
		raw, err := os.ReadFile(filepath.Join(dir, "a.yaml"))
		require.NoError(t, err)
		assert.Equal(t, "old", string(raw))
	})
}

func Test_rulesDigest(t *testing.T) {
	t.Parallel()

	sums := map[string]string{
		"terraform/aws/a.yaml": "e0dfa70eb69d47fe9cb2be8a4fcd53e74cf7fe26fcbb1f06912b57a9c028e4e0",
		"terraform/aws/B.yml":  "d09edadd173a8bbc233d47dcafc30cc876af2fb569812f2bbe0e92a1905bbce8",
		"terraform/z/c.yaml":   "c865f6c5ab8d1b0bcd383a5e1e3879d22681c96bf462c269b7581d523fbe70ab",
	}
	assert.Equal(t, rulesSHA256, rulesDigest(sums), "大文字が先に来るバイト順で並べる")
	sums["terraform/z/c.yaml"] = strings.Repeat("0", 64)
	assert.NotEqual(t, rulesSHA256, rulesDigest(sums))
}
