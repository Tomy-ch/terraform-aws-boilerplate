package main

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

// canaryOut は、canary の前提どおりの gofmt -l の出力です。
const canaryOut = "sub/unformatted.go\nunformatted.go\n"

func Test_runCanary(t *testing.T) {
	t.Parallel()

	t.Run("整形されていないファイルだけを挙げれば通り、fixture の中で起動し、終わりに消す", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t, tooltest.WithPrelude(`find . -name '*.go' | sort > "$ctl/seen.txt"`))
		f.Set(stepCanary, "out", canaryOut)
		work := filepath.Join(t.TempDir(), "work")
		var out bytes.Buffer
		require.NoError(t, runCanary(work, &out, f.Tool()))
		assert.Equal(t, "./formatted.go\n./sub/unformatted.go\n./unformatted.go\n", f.Read("seen.txt"), "道具の起動時に fixture の3つのファイルが在る")
		assert.Contains(t, out.String(), "✅ gofmt canary")
		calls := f.Calls()
		require.Len(t, calls, 1)
		parent, err := filepath.EvalSymlinks(filepath.Dir(work))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(parent, "work")+" "+stepCanary+" ", calls[0])
		assert.NoDirExists(t, work)
	})

	for name, c := range map[string]struct{ out, rc string }{
		"何も挙げない（gofmt が検査をやめた）": {},
		"何も挙げず rc≠0（構文の誤りに当たる）": {rc: "2"},
		"直下だけを挙げる（入れ子へ再帰しない）":   {out: unformattedFile + "\n"},
		"整形済みのファイルも挙げる":         {out: canaryOut + formattedFile + "\n"},
		"整形済みのファイルだけを挙げる":       {out: formattedFile + "\n"},
		"挙げるべきものを挙げたうえで rc≠0":   {out: canaryOut, rc: "2"},
	} {
		t.Run(name+"なら errCanary で、出力と rc を出し、成功を出さず、fixture を消す", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			f.Set(stepCanary, "out", c.out)
			f.Set(stepCanary, "err", "STDERR-ONLY\n")
			if c.rc != "" {
				f.Set(stepCanary, "rc", c.rc)
			}
			work := filepath.Join(t.TempDir(), "w")
			var out bytes.Buffer
			require.ErrorIs(t, runCanary(work, &out, f.Tool()), errCanary)
			assert.Contains(t, out.String(), "--- gofmt -l .（rc=")
			assert.Contains(t, out.String(), "STDERR-ONLY")
			assert.NotContains(t, out.String(), "✅")
			assert.NoDirExists(t, work)
		})
	}

	t.Run("道具を起動できなければ errTool で、fixture を消す", func(t *testing.T) {
		t.Parallel()
		work := filepath.Join(t.TempDir(), "w")
		require.ErrorIs(t, runCanary(work, new(bytes.Buffer), tooltest.Missing), toolrun.ErrTool)
		assert.NoDirExists(t, work)
	})

	t.Run("fixture を建てられなければ道具を起動せずに errRead", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		require.ErrorIs(t, runCanary(filepath.Join(file, "w"), new(bytes.Buffer), f.Tool()), errRead)
		assert.Nil(t, f.Calls())
	})
}

func Test_buildFixture(t *testing.T) {
	t.Parallel()

	t.Run("前の中身を消し、直下と入れ子の整形されていないファイルと整形済みのファイルを置く", func(t *testing.T) {
		t.Parallel()
		work := filepath.Join(t.TempDir(), "w")
		require.NoError(t, os.MkdirAll(work, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(work, "stale.go"), nil, 0o600))
		require.NoError(t, buildFixture(work))
		var got []string
		require.NoError(t, filepath.WalkDir(work, func(p string, d os.DirEntry, err error) error {
			require.NoError(t, err)
			if !d.IsDir() {
				rel, err := filepath.Rel(work, p)
				require.NoError(t, err)
				got = append(got, filepath.ToSlash(rel))
			}
			return nil
		}))
		assert.ElementsMatch(t, []string{unformattedFile, nestedUnformattedFile, formattedFile}, got)
		for name, body := range map[string]string{unformattedFile: unformattedBody, nestedUnformattedFile: unformattedBody, formattedFile: formattedBody} {
			raw, err := os.ReadFile(filepath.Join(work, filepath.FromSlash(name)))
			require.NoError(t, err)
			assert.Equal(t, body, string(raw), name)
		}
	})

	t.Run("整形されていない本文は gofmt が書き換え、整形済みの本文は書き換えない", func(t *testing.T) {
		t.Parallel()
		got, err := format.Source([]byte(unformattedBody))
		require.NoError(t, err, "整形されていない本文が構文として正しい")
		assert.NotEqual(t, unformattedBody, string(got))
		got, err = format.Source([]byte(formattedBody))
		require.NoError(t, err)
		assert.Equal(t, formattedBody, string(got))
	})

	t.Run("作れない場所なら errRead", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		require.ErrorIs(t, buildFixture(filepath.Join(file, "w")), errRead)
	})
}

func Test_sameLines(t *testing.T) {
	t.Parallel()

	want := []string{"sub/a.go", "a.go"}
	for name, c := range map[string]struct {
		out  string
		want bool
	}{
		"順が違っても一致する":    {"a.go\nsub/a.go\n", true},
		"先頭の ./ と空行を外す": {"\n./sub/a.go\n\n./a.go\n", true},
		"空":             {"", false},
		"足りない":          {"a.go\n", false},
		"余分な行":          {"a.go\nsub/a.go\nb.go\n", false},
		"同じ名前が2回で別の名前が欠ける": {"a.go\na.go\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, sameLines(c.out, want))
		})
	}
}

func Test_lines(t *testing.T) {
	t.Parallel()

	t.Run("空でない行を、行末の CR と先頭の ./ を外して順に返し、名前の前後の空白は残す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"a.go", "lib/b.go", " c.go "}, lines("./a.go\r\n\nlib/b.go\n c.go \n"))
	})

	t.Run("空なら nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, lines("\n\n"))
	})
}
