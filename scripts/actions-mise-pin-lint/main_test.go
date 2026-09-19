package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const consistent = `
    - uses: actions/cache@abc
      with:
        key: mise-linux-x64-2026.7.12-dad54e0b
    - env:
        MISE_VERSION: 2026.7.12
        MISE_SHA256: dad54e0b843908324282b8673f9c0ebc3a4da0c49ad2da309a49bfbc918ba180
`

func writeAction(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "action.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("三点が一致していれば通る", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, run([]string{"-action", writeAction(t, consistent)}, &out))
		assert.Contains(t, out.String(), "三点が一致しています")
	})

	// 退化した入力の pin。読めないものを「違反なし」と報告しない。
	t.Run("action を読めなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := run([]string{"-action", filepath.Join(t.TempDir(), "missing.yaml")}, &out)
		require.Error(t, err)
		assert.True(t, xerrors.Is(err, errUnreadable))
	})
}

func Test_findViolations(t *testing.T) {
	t.Parallel()

	const digest = "dad54e0b843908324282b8673f9c0ebc3a4da0c49ad2da309a49bfbc918ba180"

	tests := map[string]struct {
		pin   misePin
		wants []string
	}{
		"一致": {
			pin: misePin{version: "1.2.3", digest: digest, cacheKey: "mise-1.2.3-dad54e0b"},
		},
		"版が読めない": {
			pin:   misePin{digest: digest, cacheKey: "k"},
			wants: []string{"MISE_VERSION を読み取れません"},
		},
		"digest が読めない": {
			pin:   misePin{version: "1.2.3", cacheKey: "k"},
			wants: []string{"MISE_SHA256 を読み取れません"},
		},
		"キーが読めない": {
			pin:   misePin{version: "1.2.3", digest: digest},
			wants: []string{"キャッシュの key を読み取れません"},
		},
		"キーが版を含まない": {
			// 版だけを上げてキーを据え置くと、キャッシュは古いバイナリを新しい版として返し続ける。
			pin:   misePin{version: "1.2.3", digest: digest, cacheKey: "mise-dad54e0b"},
			wants: []string{"キャッシュキーが版を含んでいません"},
		},
		"キーが digest の先頭を含まない": {
			pin:   misePin{version: "1.2.3", digest: digest, cacheKey: "mise-1.2.3"},
			wants: []string{"キャッシュキーが digest の先頭 8 桁を含んでいません"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := findViolations(tt.pin)
			require.Len(t, got, len(tt.wants))
			for i, want := range tt.wants {
				assert.Contains(t, got[i], want)
			}
		})
	}
}

func Test_readPin(t *testing.T) {
	t.Parallel()
	got := readPin(consistent)
	assert.Equal(t, "2026.7.12", got.version)
	assert.Equal(t, "dad54e0b843908324282b8673f9c0ebc3a4da0c49ad2da309a49bfbc918ba180", got.digest)
	assert.Equal(t, "mise-linux-x64-2026.7.12-dad54e0b", got.cacheKey)
}

func Test_readYAMLValue(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		key    string
		want   string
	}{
		"素の値":       {source: "  MISE_VERSION: 1.2.3\n", key: "MISE_VERSION", want: "1.2.3"},
		"前後の空白を落とす": {source: "  MISE_VERSION:   1.2.3  \n", key: "MISE_VERSION", want: "1.2.3"},
		"最初の一致を採る":  {source: "  key: a\n  key: b\n", key: "key", want: "a"},
		"値が無い":      {source: "  MISE_VERSION:\n", key: "MISE_VERSION", want: ""},
		"キーが無い":     {source: "  OTHER: x\n", key: "MISE_VERSION", want: ""},
		"前方一致する別キー": {source: "  MISE_VERSION_OLD: 9\n  MISE_VERSION: 1\n", key: "MISE_VERSION", want: "1"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, readYAMLValue(tt.source, tt.key))
		})
	}
}

func Test_run_違反の出力(t *testing.T) {
	t.Parallel()

	t.Run("不一致は本文へ出す", func(t *testing.T) {
		t.Parallel()
		// 版を上げて digest とキーを据え置いた形。キャッシュは古いバイナリを新しい版として返し続ける。
		body := "        key: mise-1.0.0-dad54e0b\n        MISE_VERSION: 2.0.0\n        MISE_SHA256: dad54e0b843908324282b8673f9c0ebc3a4da0c49ad2da309a49bfbc918ba180\n"
		var out bytes.Buffer
		err := run([]string{"-action", writeAction(t, body)}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "キャッシュキーが版を含んでいません")
	})

	t.Run("読み取れない項目は不一致より先に報告する", func(t *testing.T) {
		t.Parallel()
		// 3点のどれかが欠けた状態は、検証があるように見えて効いていない状態である。
		var out bytes.Buffer
		err := run([]string{"-action", writeAction(t, "        key: k\n")}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "MISE_VERSION を読み取れません")
		assert.Contains(t, out.String(), "MISE_SHA256 を読み取れません")
		assert.NotContains(t, out.String(), "キャッシュキーが版を含んでいません")
	})

	t.Run("digest が8桁未満なら違反として報告する", func(t *testing.T) {
		t.Parallel()
		// 短い digest で添字が範囲外になると、検査が panic して「実行されなかった」状態になる。
		// 素通りさせるのも同じく誤りで、キーが digest を含むことを確かめようがない。
		body := "        key: k-1.0.0\n        MISE_VERSION: 1.0.0\n        MISE_SHA256: abc\n"
		var out bytes.Buffer
		err := run([]string{"-action", writeAction(t, body)}, &out)
		require.Error(t, err)
		assert.Contains(t, out.String(), "MISE_SHA256 が短すぎます")
	})
}

// ここから下は輸入した検査項目。

func Test_digestPrefixLength(t *testing.T) {
	t.Parallel()
	// キャッシュキーへ埋める桁数。変えるとキーの形が変わり、既存のキャッシュが全て外れる。
	assert.Equal(t, 8, digestPrefixLength)
}

func Test_readPin_輸入したケース(t *testing.T) {
	t.Parallel()

	t.Run("読み取れない値を空で返す", func(t *testing.T) {
		t.Parallel()
		got := readPin("        other: x\n")
		assert.Empty(t, got.version)
		assert.Empty(t, got.digest)
		assert.Empty(t, got.cacheKey)
	})

	t.Run("空の値を空で返す", func(t *testing.T) {
		t.Parallel()
		got := readPin("        MISE_VERSION:\n        MISE_SHA256:   \n        key:\n")
		assert.Empty(t, got.version)
		assert.Empty(t, got.digest)
		assert.Empty(t, got.cacheKey)
	})
}

func Test_findViolations_読み取れない値をそれぞれ報告(t *testing.T) {
	t.Parallel()

	// 3点のどれかが欠けた状態は、検証があるように見えて効いていない状態である。
	// 1件だけ報告すると、直した後にもう1件が出てくることになる。
	got := findViolations(misePin{})
	require.Len(t, got, 3)
	assert.Contains(t, got[0], "MISE_VERSION")
	assert.Contains(t, got[1], "MISE_SHA256")
	assert.Contains(t, got[2], "キャッシュの key")
}
