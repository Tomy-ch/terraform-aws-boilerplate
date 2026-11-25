package main

import (
	"io"
	"os"
	"path/filepath"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// prepareWork は、root の下の作業ディレクトリ tmp/secret-scan/<name> を workdir.Recreate で作り直し、そのパスと、
// それを消す後片付けを返します。後片付けは消せなければ log へ警告を書きます。外を指せば errUsage です。
func prepareWork(root, name string, log io.Writer) (string, func(), error) {
	work := "tmp/secret-scan/" + name
	dir, err := workdir.Recreate(root, work)
	if xerrors.Is(err, workdir.ErrInvalid) {
		return "", nil, xerrors.Wrapf(errUsage, "作業ディレクトリ: %v", err)
	}
	if err != nil {
		return "", nil, xerrors.Wrapf(errInput, "作業ディレクトリの作り直し: %v", err)
	}

	return dir, func() { workdir.Cleanup(root, work, log) }, nil
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
