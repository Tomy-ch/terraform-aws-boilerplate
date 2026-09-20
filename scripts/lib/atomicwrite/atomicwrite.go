// Package atomicwrite は、複数ファイルの書き換えを1箇所へ集める。
//
// このリポジトリの道具のいくつかは、宣言を正として複数のファイルを書き換える
// （pin-actions の `uses:`、pin-images の `FROM`、egress の allowed-endpoints、
// versions の版の写し）。素朴に `os.WriteFile` を並べると、2つ目で失敗したとき
// 1つ目だけが新しい内容になった作業ツリーが残る。呼び出し側にはエラーしか見えないので、
// **「失敗した」と「一部だけ適用された」が区別できなくなる。**
//
// 一時ファイルへ全部書き切ってから、まとめて rename する。rename の途中で落ちる窓は
// 残るが、write の途中で落ちる窓よりはるかに狭い。消せないので、残った一時ファイルは
// 後始末する。
package atomicwrite

import (
	"io/fs"
	"os"
	"sort"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// tmpSuffix は一時ファイルの接尾辞。作業ツリーへ残っても正体が分かる名前にする。
const tmpSuffix = ".atomicwrite.tmp"

// Apply は changes（パス→新しい内容）をすべてのファイルへ反映します。
//
// 書き込みの順序は安定させる（パスの昇順）。失敗したときにどこまで進んだかが
// 実行ごとに変わると、再現できない。
func Apply(changes map[string]string, perm fs.FileMode) error {
	paths := sortedPaths(changes)
	temps := make(map[string]string, len(paths))

	defer func() {
		for _, tmp := range temps {
			_ = os.Remove(tmp)
		}
	}()

	for _, path := range paths {
		tmp := path + tmpSuffix
		// path は呼び出し側が宣言と走査から組む。利用者の入力は通らない。
		// 撤回条件: 経路が外部入力から決まる形になったとき。
		if err := os.WriteFile(tmp, []byte(changes[path]), modeOf(path, perm)); err != nil { //nolint:gosec // 上のコメントを参照
			return xerrors.Wrap(err, "write "+tmp)
		}

		temps[path] = tmp
	}

	for _, path := range paths {
		if err := os.Rename(temps[path], path); err != nil {
			return xerrors.Wrap(err, "rename "+path)
		}

		delete(temps, path)
	}

	return nil
}

// modeOf は、既存ファイルのモードを返します。無ければ fallback を返します。
//
// `os.WriteFile` は既存ファイルのモードを変えない。一時ファイル経由で置き換えるここでは、
// 引き継がないと書き換えのたびにモードが既定値へ戻ってしまう。
func modeOf(path string, fallback fs.FileMode) fs.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return fallback
	}

	return info.Mode().Perm()
}

// SortedPaths は changes のキーを昇順で返します。呼び出し側が報告の順序を揃えるために使います。
func SortedPaths(changes map[string]string) []string {
	return sortedPaths(changes)
}

func sortedPaths(changes map[string]string) []string {
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	return paths
}
