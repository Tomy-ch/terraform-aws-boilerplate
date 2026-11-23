package main

import (
	"context"
	"os/exec"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// toolFunc は、ディレクトリ dir で段 step の道具を起動するコマンドを作ります。テストは差し替えます。
type toolFunc func(dir string, env []string, step string) *exec.Cmd

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

// trivySeverities は、trivy-config の閾値として受ける値です。どれも、ある重大度とそれより重いものすべてを並べます。
var trivySeverities = []string{
	"CRITICAL",
	"CRITICAL,HIGH",
	"CRITICAL,HIGH,MEDIUM",
	"CRITICAL,HIGH,MEDIUM,LOW",
	"CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN",
}

// trivyFormats は、trivy-config の本走査の出力の形として受ける値です。canary は json で走ります。
var trivyFormats = []string{"table", "json", "sarif"}

// trivyStep は、閾値と出力の形を一覧と照らし、trivy-config の段の名前を返します。
func trivyStep(severity, format string) (string, error) {
	if !slices.Contains(trivySeverities, severity) {
		return "", xerrors.Wrapf(errUsage, "-severity %q は受けない値です（%s）", severity, strings.Join(trivySeverities, " | "))
	}
	if !slices.Contains(trivyFormats, format) {
		return "", xerrors.Wrapf(errUsage, "-format %q は受けない値です（%s）", format, strings.Join(trivyFormats, " | "))
	}

	return "trivy-config/" + severity + "/" + format, nil
}

// command は toolFunc の実物です。引数は段ごとに固定で、可変の値は作業ディレクトリと、そこに置いたファイル
// （compose.yaml・work/・out/・.trivyignore.yaml）で渡します。イメージの段だけはリポジトリ直下の docker-compose.yaml を読みます。
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
	case "trivy-config/CRITICAL/table":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "table", ".")
	case "trivy-config/CRITICAL/json":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-config/CRITICAL/sarif":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "sarif", ".")
	case "trivy-config/CRITICAL,HIGH/table":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "table", ".")
	case "trivy-config/CRITICAL,HIGH/json":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-config/CRITICAL,HIGH/sarif":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "sarif", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM/table":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "table", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM/json":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM/sarif":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "sarif", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW/table":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "table", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW/json":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW/sarif":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "sarif", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN/table":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "table", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN/json":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "json", ".")
	case "trivy-config/CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN/sarif":
		cmd = exec.CommandContext(ctx, "trivy", "config", "--severity", "CRITICAL,HIGH,MEDIUM,LOW,UNKNOWN", "--ignorefile", trivyIgnore, "--exit-code", "1", "--skip-version-check", "--skip-dirs", "tmp", "--format", "sarif", ".")
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
