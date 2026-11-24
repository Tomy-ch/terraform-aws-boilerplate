package main

import (
	"context"
	"os/exec"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

type toolFunc = toolrun.Func

const (
	stepImageCheckov = "image-checkov"
	stepImageKics    = "image-kics"
	stepImageSemgrep = "image-semgrep"
	stepCreate       = "create"
	stepCopyIn       = "copy-in"
	stepUp           = "up"
	stepCopyOut      = "copy-out"
	stepDown         = "down"
)

// trivyIgnore は、trivy-config が読む抑止の宣言です（作業ディレクトリからの相対パス）。trivy config は misconfiguration
// 専用で、fs --scanners misconfig と違って lang-pkgs の空 Result が混ざりません。.trivyignore.yaml は自動検出されないので
// --ignorefile で明示し、その結果として抑止が他のスキャナへ暗黙に波及しません。
const trivyIgnore = ".trivyignore.yaml"

// trivySeverities は、trivy-config の閾値として受ける値です。どれも、ある重大度とそれより重いものすべてを並べ、
// canary の MEDIUM を含みます。
var trivySeverities = []string{
	"CRITICAL,HIGH,MEDIUM",
	"CRITICAL,HIGH,MEDIUM,LOW",
	"CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN",
}

// trivyFormats は、trivy-config の本走査の出力の形として受ける値です。走査は常に json で走り、ほかの形は
// その結果から trivy convert で作ります。
var trivyFormats = []string{"table", "json", "sarif"}

const stepTrivyConvertSARIF = "trivy-convert/sarif"

const trivyResult = "result.json"

// trivyStep は、閾値と出力の形を一覧と照らし、trivy-config の走査の段と、json 以外の形へ変える段（json なら空）を
// 返します。
func trivyStep(severity, format string) (scan, convert string, err error) {
	if !slices.Contains(trivySeverities, severity) {
		return "", "", xerrors.Wrapf(errUsage, "-severity %q は受けない値です（%s）", severity, strings.Join(trivySeverities, " | "))
	}
	if !slices.Contains(trivyFormats, format) {
		return "", "", xerrors.Wrapf(errUsage, "-format %q は受けない値です（%s）", format, strings.Join(trivyFormats, " | "))
	}
	if format != "json" {
		convert = "trivy-convert/" + format
	}

	return "trivy-config/" + severity, convert, nil
}

// command は toolFunc の実物です。引数は段ごとに固定で、可変の値は作業ディレクトリと、そこに置いたファイルで渡します。
// イメージの段だけはリポジトリ直下の docker-compose.yaml を読みます。
// 知らない段は道具を起動せずに落ちます。
func command(dir string, env []string, step string) *exec.Cmd {
	ctx := context.Background()
	var cmd *exec.Cmd
	switch step {
	case stepImageCheckov:
		cmd = exec.CommandContext(ctx, "docker", "compose", "config", "--images", "checkov_runner")
	case stepImageKics:
		cmd = exec.CommandContext(ctx, "docker", "compose", "config", "--images", "kics_runner")
	case stepImageSemgrep:
		cmd = exec.CommandContext(ctx, "docker", "compose", "config", "--images", "semgrep_runner")
	case stepCreate:
		cmd = exec.CommandContext(ctx, "docker", "compose", "create")
	case stepCopyIn:
		cmd = exec.CommandContext(ctx, "docker", "compose", "cp", "work/.", composeService+":/work")
	case stepUp:
		// 起動を待ち、道具の終了コードを返す。compose wait は、先に終わったコンテナを待てない。
		cmd = exec.CommandContext(ctx, "docker", "compose", "up", "--no-recreate", "--no-build", "--pull", "never",
			"--no-log-prefix", "--exit-code-from", composeService, composeService)
	case stepCopyOut:
		cmd = exec.CommandContext(ctx, "docker", "compose", "cp", composeService+":/tmp/out/.", "out")
	case stepDown:
		cmd = exec.CommandContext(ctx, "docker", "compose", "down")
	case "trivy-config/CRITICAL,HIGH,MEDIUM":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-convert/table":
		// 抑止は走査で済んでいるので、ここでは抑止のファイルを読まない。
		cmd = exec.CommandContext(ctx, "trivy", "convert", "--format", "table", "--scanners", "misconfig", "--ignorefile", "", trivyResult)
	case stepTrivyConvertSARIF:
		cmd = exec.CommandContext(ctx, "trivy", "convert", "--format", "sarif", "--ignorefile", "", trivyResult)
	default:
		cmd = exec.CommandContext(ctx, "false")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// withoutPrefix は、environ から prefix で始まる名前の変数を外します。docker compose には COMPOSE_ を、trivy には
// TRIVY_ を渡しません —— 呼び出した人の環境が、compose のファイルや project 名、走査の閾値や抑止を差し替えないため。
func withoutPrefix(environ []string, prefix string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}

	return out
}
