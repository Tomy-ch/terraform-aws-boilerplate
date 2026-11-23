package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
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
	Command     []string          `yaml:"command"`
	Environment map[string]string `yaml:"environment,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
}

var pinnedImage = regexp.MustCompile(`^[^\s@]+@sha256:[0-9a-f]{64}$`)

// lookupImage は、docker-compose.yaml の service が宣言するイメージを引きます（版と digest の正本はそこにある）。
// 出力がちょうど1行の、digest で固定した参照でなければ失敗します。
func lookupImage(tool toolFunc, step string) (string, error) {
	cmd := tool("", withoutPrefix(os.Environ(), "COMPOSE_"), step)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", xerrors.Wrapf(errImage, "%s: %v: %s", step, err, strings.TrimSpace(errOut.String()))
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 1 || !pinnedImage.MatchString(lines[0]) {
		return "", xerrors.Wrapf(errImage, "%s: digest で固定したイメージがちょうど1行ではありません（%q）", step, out.String())
	}

	return lines[0], nil
}

// writeCompose は、dir に compose.yaml を書きます。project 名は dir の絶対パスから決め、作業ディレクトリごとに分けます。
// network は与えません。compose は値の `$` を補間するので、`$$` にして渡します。
func writeCompose(dir, image string, t imageTool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return xerrors.Wrapf(err, "%s", dir)
	}
	sum := sha256.Sum256([]byte(abs))
	escape := func(s string) string { return strings.ReplaceAll(s, "$", "$$") }
	spec := composeServiceSpec{Image: image, NetworkMode: "none", WorkingDir: escape(t.workingDir)}
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
		return xerrors.Wrap(err, "compose.yaml の組み立て")
	}

	return writeText(filepath.Join(dir, "compose.yaml"), string(raw))
}

// runImage は、dir の work/ を入力にして t をコンテナで起動し、コンテナの /tmp/out を dir の out/ へ取り出し、道具の
// 終了コードを返します。docker compose の出力は log へ流します。create を試みた後は、失敗の経路でもコンテナを消します。
func runImage(dir, image string, t imageTool, tool toolFunc, log io.Writer) (rc int, err error) {
	if err := writeCompose(dir, image, t); err != nil {
		return 0, err
	}
	env := withoutPrefix(os.Environ(), "COMPOSE_")
	start := func(step string) *exec.Cmd {
		cmd := tool(dir, env, step)
		cmd.Stdout, cmd.Stderr = log, log

		return cmd
	}
	defer func() {
		if derr := start(stepDown).Run(); derr != nil && err == nil {
			err = xerrors.Wrapf(errDocker, "%s: コンテナを消せません: %v", dir, derr)
		}
	}()
	for _, step := range []string{stepCreate, stepCopyIn} {
		if err := start(step).Run(); err != nil {
			return 0, xerrors.Wrapf(errDocker, "%s: %s: %v", dir, step, err)
		}
	}
	if err := start(stepUp).Run(); err != nil {
		var exit *exec.ExitError
		if !xerrors.As(err, &exit) {
			return 0, xerrors.Wrapf(errDocker, "%s: %s: %v", dir, stepUp, err)
		}
		rc = exit.ExitCode()
	}
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o750); err != nil {
		return 0, xerrors.Wrapf(err, "%s の作成", filepath.Join(dir, "out"))
	}
	if err := start(stepCopyOut).Run(); err != nil {
		return rc, xerrors.Wrapf(errOutput, "%s（道具の終了コード %d）: %v", dir, rc, err)
	}

	return rc, nil
}

func prepareWork(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return xerrors.Wrapf(err, "%s の削除", dir)
	}
	if err := os.MkdirAll(filepath.Join(dir, "work", "src"), 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", dir)
	}

	return nil
}

// runCanary は、dir の work/src に fixture の main.tf を置き、prepare（nil でなければ）で残りの入力を置いて t を起動し、
// judge で判定します。canary の出力は、落ちたときだけ log へ出します。通れば dir を消します。
func runCanary(name, dir, fixture, image string, t imageTool, tool toolFunc, log io.Writer,
	judge func(rc int, out string) error, prepare func() error,
) error {
	if err := prepareWork(dir); err != nil {
		return err
	}
	if err := writeText(filepath.Join(dir, "work", "src", "main.tf"), fixture); err != nil {
		return err
	}
	if prepare != nil {
		if err := prepare(); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	rc, err := runImage(dir, image, t, tool, &buf)
	if err == nil {
		err = judge(rc, filepath.Join(dir, "out"))
	}
	if err != nil {
		fmt.Fprintf(log, "--- %s の出力:\n%s\n", name, buf.String())

		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return xerrors.Wrapf(err, "%s の削除", dir)
	}

	return nil
}

func writeText(p, body string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", filepath.Dir(p))
	}
	if err := atomicwrite.Apply(map[string]string{p: body}, 0o600); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", p)
	}

	return nil
}
