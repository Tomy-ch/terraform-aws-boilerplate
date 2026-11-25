package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// composeService は、生成する compose.yaml の唯一の service です。
const composeService = "scan"

// imageTool は、上流のイメージで走る道具の起動の定義です。command・environment・workingDir はコンテナの中の値で、
// 生成する compose.yaml に書きます。canary と本走査は同じ定義で起動します。
type imageTool struct {
	imageStep   string
	command     []string
	environment map[string]string
	workingDir  string
}

type composeFile struct {
	Name     string                        `yaml:"name"`
	Services map[string]composeServiceSpec `yaml:"services"`
}

type composeServiceSpec struct {
	Image       string            `yaml:"image"`
	NetworkMode string            `yaml:"network_mode"`
	CapDrop     []string          `yaml:"cap_drop"`
	SecurityOpt []string          `yaml:"security_opt"`
	PidsLimit   int               `yaml:"pids_limit"`
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
}

// composePidsLimit は、コンテナが持てるプロセス（スレッドを含む）の上限です。
const composePidsLimit = 1024

type runner struct {
	tool    toolFunc
	environ []string
}

var pinnedImage = regexp.MustCompile(`^[^\s@]+@sha256:[0-9a-f]{64}$`)

// lookupImage は、docker-compose.yaml の service が宣言するイメージを引きます（版と digest の正本はそこにある）。
// docker compose はカレントディレクトリの docker-compose.yaml を読み、-base を見ません。make が常にリポジトリ直下で
// 起動することが前提です。docker compose を起動できなければ errTool、0 以外で終われば errImage、出力がちょうど1行の、
// digest で固定した参照でなければ errImage です。
func lookupImage(r runner, step string) (string, error) {
	res, err := toolrun.Run(r.tool(context.Background(), "", withoutPrefix(r.environ, "COMPOSE_"), step))
	switch {
	case err != nil:
		return "", xerrors.Wrapf(err, "%s", step)
	case res.Code != 0:
		return "", xerrors.Wrapf(errImage, "%s: 終了コード %d: %s", step, res.Code, strings.TrimSpace(res.Combined))
	}
	lines := strings.Split(strings.TrimSuffix(res.Stdout, "\n"), "\n")
	if len(lines) != 1 || !pinnedImage.MatchString(lines[0]) {
		return "", xerrors.Wrapf(errImage, "%s: digest で固定したイメージがちょうど1行ではありません（%q）", step, res.Stdout)
	}

	return lines[0], nil
}

// writeCompose は、dir に compose.yaml を書きます。project 名は dir の絶対パスから決め、作業ディレクトリごとに分けます。
// compose は値の `$` を補間するので、`$$` にして渡します。
func writeCompose(dir, image string, t imageTool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return xerrors.Wrapf(errInput, "%s: %v", dir, err)
	}
	sum := sha256.Sum256([]byte(abs))
	escape := func(s string) string { return strings.ReplaceAll(s, "$", "$$") }
	spec := composeServiceSpec{
		Image:       image,
		NetworkMode: "none",
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		PidsLimit:   composePidsLimit,
		WorkingDir:  escape(t.workingDir),
	}
	for _, c := range t.command {
		spec.Command = append(spec.Command, escape(c))
	}
	if len(t.environment) > 0 {
		spec.Environment = map[string]string{}
		for k, v := range t.environment {
			spec.Environment[k] = escape(v)
		}
	}
	raw, err := yaml.Marshal(composeFile{
		Name:     "iac-scan-" + hex.EncodeToString(sum[:])[:16],
		Services: map[string]composeServiceSpec{composeService: spec},
	})
	if err != nil {
		return xerrors.Wrapf(errInput, "compose.yaml の組み立て: %v", err)
	}

	return writeText(filepath.Join(dir, "compose.yaml"), string(raw))
}

// runImage は、dir の work/ を workTar でコンテナの /work へ渡して t を起動し、コンテナの /tmp/out を dir の out/ へ取り出し、道具の
// 終了コードを返します。docker compose の出力は log へ流します。docker compose を起動できないかシグナルで終わった段は
// errTool、up 以外の段が 0 以外で終われば errDocker（copy-out は errOutput）です。up の終了コードは道具の終了コードとして
// 返します。create を試みた後は、失敗の経路でもコンテナを消します。
func runImage(dir, image string, t imageTool, r runner, log io.Writer) (rc int, err error) {
	if err := writeCompose(dir, image, t); err != nil {
		return 0, err
	}
	input, err := workTar(filepath.Join(dir, "work"))
	if err != nil {
		return 0, err
	}
	env := withoutPrefix(r.environ, "COMPOSE_")
	runStep := func(step string) (int, error) {
		cmd := r.tool(context.Background(), dir, env, step)
		cmd.Stdout, cmd.Stderr = log, log
		if step == stepCopyIn {
			cmd.Stdin = bytes.NewReader(input)
		}
		code, err := toolrun.Exec(cmd)
		if err != nil {
			return 0, xerrors.Wrapf(err, "%s: %s", dir, step)
		}

		return code, nil
	}
	defer func() {
		code, derr := runStep(stepDown)
		switch {
		case err != nil:
		case derr != nil:
			err = derr
		case code != 0:
			err = xerrors.Wrapf(errDocker, "%s: コンテナを消せません（終了コード %d）", dir, code)
		}
	}()
	for _, step := range []string{stepCreate, stepCopyIn} {
		code, err := runStep(step)
		if err != nil {
			return 0, err
		}
		if code != 0 {
			return 0, xerrors.Wrapf(errDocker, "%s: %s が終了コード %d で終わりました", dir, step, code)
		}
	}
	if rc, err = runStep(stepUp); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o750); err != nil {
		return 0, xerrors.Wrapf(errOutput, "%s の作成: %v", filepath.Join(dir, "out"), err)
	}
	code, err := runStep(stepCopyOut)
	if err != nil {
		return rc, err
	}
	if code != 0 {
		return rc, xerrors.Wrapf(errOutput, "%s（道具の終了コード %d）: copy-out が終了コード %d で終わりました", dir, rc, code)
	}

	return rc, nil
}

// workTar は、src の下を `work/` の下に置いた tar を返します。entry は uid・gid 0 で、ディレクトリは 0700、ファイルは
// 0600 です —— コンテナの root が所有者になり、capability 無しで読めます。ディレクトリと通常のファイルのほかは errInput
// です。
func workTar(src string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	root, err := os.OpenRoot(src)
	if err == nil {
		defer func() { _ = root.Close() }()
		fsys := root.FS()
		err = fs.WalkDir(fsys, ".", func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			h := &tar.Header{Name: path.Join("work", p), Uid: 0, Gid: 0, Format: tar.FormatPAX}
			switch {
			case e.IsDir():
				h.Typeflag, h.Name, h.Mode = tar.TypeDir, h.Name+"/", 0o700

				return tw.WriteHeader(h)
			case !e.Type().IsRegular():
				return xerrors.Newf("%s は通常のファイルではありません（%s）", p, e.Type())
			}
			raw, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			h.Typeflag, h.Mode, h.Size = tar.TypeReg, 0o600, int64(len(raw))
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			_, err = tw.Write(raw)

			return err
		})
	}
	if err == nil {
		err = tw.Close()
	}
	if err != nil {
		return nil, xerrors.Wrapf(errInput, "%s を tar にできません: %v", src, err)
	}

	return buf.Bytes(), nil
}

// resolveWork は、workdir.Resolve の失敗を使い方の誤りにして返します。
func resolveWork(base, work string) (string, error) {
	dir, err := workdir.Resolve(base, work)
	if err != nil {
		return "", xerrors.Wrapf(errUsage, "-base %q の作業ディレクトリ: %v", base, err)
	}

	return dir, nil
}

// prepareWork は、base の下の作業ディレクトリ work を resolveWork で確かめてから作り直し、work/src を作ってそのパスを
// 返します。
func prepareWork(base, work string) (string, error) {
	dir, err := resolveWork(base, work)
	if err != nil {
		return "", err
	}
	if _, err := workdir.Recreate(base, work); err != nil {
		return "", xerrors.Wrapf(errInput, "%s の作り直し: %v", dir, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "work", "src"), 0o750); err != nil {
		return "", xerrors.Wrapf(errInput, "%s の作成: %v", dir, err)
	}

	return dir, nil
}

// runCanary は、作業ディレクトリ work に fixture の main.tf と、prepare（nil でなければ）が置く残りの入力を用意して t を
// 起動し、judge で判定します。canary の出力は、落ちたときだけ log へ出します。通れば作業ディレクトリを消します。
func runCanary(name, base, work, fixture, image string, t imageTool, r runner, log io.Writer,
	judge func(rc int, out string) error, prepare func(dir string) error,
) error {
	dir, err := prepareWork(base, work)
	if err != nil {
		return err
	}
	if err := writeText(filepath.Join(dir, "work", "src", "main.tf"), fixture); err != nil {
		return err
	}
	if prepare != nil {
		if err := prepare(dir); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	rc, err := runImage(dir, image, t, r, &buf)
	if err == nil {
		err = judge(rc, filepath.Join(dir, "out"))
	}
	if err != nil {
		fmt.Fprintf(log, "--- %s の出力:\n%s\n", name, buf.String())

		return err
	}
	if err := workdir.Remove(base, work); err != nil {
		return xerrors.Wrapf(errInput, "%s の削除: %v", dir, err)
	}

	return nil
}

func writeText(p, body string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return xerrors.Wrapf(errInput, "%s の作成: %v", filepath.Dir(p), err)
	}
	if err := atomicwrite.Apply(map[string]string{p: body}, 0o600); err != nil {
		return xerrors.Wrapf(errInput, "%s の書き込み: %v", p, err)
	}

	return nil
}
