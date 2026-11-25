package main

import (
	"os"
	"path/filepath"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// prepareWork は、root の下の作業ディレクトリ tmp/secret-scan/<name> を workdir.Resolve で確かめてから作り直し、
// そのパスを返します。外を指せば errUsage です。
func prepareWork(root, name string) (string, error) {
	dir, err := workdir.Resolve(root, "tmp/secret-scan/"+name)
	if err != nil {
		return "", xerrors.Wrapf(errUsage, "作業ディレクトリ: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", xerrors.Wrapf(errInput, "%s の削除: %v", dir, err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", xerrors.Wrapf(errInput, "%s の作成: %v", dir, err)
	}

	return dir, nil
}

// readFile は p を読みます。読めなければ errInput です。
func readFile(p string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return "", xerrors.Wrapf(errInput, "%v", err)
	}

	return string(raw), nil
}

// writeFile は、親のディレクトリを作って p に body を書きます。書けなければ errInput です。
func writeFile(p, body string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return xerrors.Wrapf(errInput, "%v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		return xerrors.Wrapf(errInput, "%v", err)
	}

	return nil
}

// copyInto は、root の name を dir の同じ名前へ写します。複写元が無ければ errInput です。
func copyInto(root, dir, name string) error {
	body, err := readFile(filepath.Join(root, name))
	if err != nil {
		return err
	}

	return writeFile(filepath.Join(dir, filepath.FromSlash(name)), body)
}
