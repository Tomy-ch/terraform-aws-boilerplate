// Package ghfiles は `uses:` を書ける GitHub Actions の定義ファイルの列挙を 1 箇所へ集める。
//
// 呼び出し側は 2 つある。`uses: owner/repo@<sha>` を SHA へ固定する pin-actions と、
// `uses: docker://<image>:<tag>` を digest へ固定する pin-images である。同じ行が両者から
// 見えている必要は無いが、**同じファイル集合が見えている必要はある**——片方だけが見る
// ディレクトリができると、そこに置かれた参照がどちらの網からも外れる。
//
// composite action は `uses: ./.github/actions/<group>/<name>` のように入れ子へ置けるため走査は
// 再帰する。1 階層で打ち切ると入れ子の定義が検査されないまま通る。
package ghfiles

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var actionFilenames = []string{"action.yml", "action.yaml"}

// actionsDir は root からの相対で composite action 定義を置くディレクトリ。
const actionsDir = ".github/actions"

// workflowPatterns は root からの相対で workflow 定義を指すパターン。
var workflowPatterns = []string{".github/workflows/*.yaml", ".github/workflows/*.yml"}

// Patterns は Collect が集めるファイル集合を、root からの相対の glob で返す。`**` は任意の深さの
// ディレクトリを表す。走査範囲を別の宣言（hook の glob）と突き合わせる側が読む。
func Patterns() []string {
	patterns := slices.Clone(workflowPatterns)
	for _, name := range actionFilenames {
		patterns = append(patterns, actionsDir+"/**/"+name)
	}
	return patterns
}

// Collect は workflow 定義とリポジトリ内 composite action 定義のパスを名前順で返す。
// `.github/actions` が無いリポジトリは空として扱い、それ以外の走査失敗は握り潰さず返す。
func Collect(root string) ([]string, error) {
	var files []string
	for _, pat := range workflowPatterns {
		m, err := filepath.Glob(filepath.Join(root, pat))
		if err != nil {
			return nil, xerrors.Wrap(err, "glob "+pat)
		}
		files = append(files, m...)
	}

	err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(actionsDir)), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && slices.Contains(actionFilenames, d.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil && !xerrors.Is(err, os.ErrNotExist) {
		return nil, xerrors.Wrap(err, "walk .github/actions")
	}

	sort.Strings(files)
	return files, nil
}
