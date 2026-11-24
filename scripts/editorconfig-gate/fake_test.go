package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

// lsCopyPrelude は、`<段>.ls` があれば作業ディレクトリの一覧をそこへ書き、`<段>.copy` があれば作業ディレクトリの
// *.yaml を `copied-<名前>` へ写します。
const lsCopyPrelude = `if [ -e "$ctl/$step.ls" ]; then ls -A > "$ctl/$step.ls"; fi
if [ -e "$ctl/$step.copy" ]; then for y in *.yaml; do cp "$y" "$ctl/copied-$y"; done; fi`

// newFake は、fixture の中身を控える断片を足した偽の道具です。
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
