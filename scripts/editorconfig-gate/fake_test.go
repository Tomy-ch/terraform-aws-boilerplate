package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

// lsCopyPrelude は、`<段>.ls` があれば作業ディレクトリの一覧をそこへ書き、`<段>.copy` があれば作業ディレクトリの
// 下の *.yaml を `copied-<名前>` へ写します。起動ごとに git の設定の環境変数を env.log へ1行控えます。
const lsCopyPrelude = `if [ -e "$ctl/$step.ls" ]; then ls -A > "$ctl/$step.ls"; fi
if [ -e "$ctl/$step.copy" ]; then find . -name '*.yaml' -type f | while read -r y; do cp "$y" "$ctl/copied-$(basename "$y")"; done; fi
echo "$(basename "$cwd") $step c=${GIT_CONFIG_COUNT:-} k0=${GIT_CONFIG_KEY_0:-} v0=${GIT_CONFIG_VALUE_0:-} k1=${GIT_CONFIG_KEY_1:-} p=${GIT_CONFIG_PARAMETERS:-}" >> "$ctl/env.log"`

func newFake(t *testing.T) *tooltest.Fake {
	t.Helper()
	return tooltest.New(t, tooltest.WithPrelude(lsCopyPrelude))
}

func writeFile(t *testing.T, p, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}
