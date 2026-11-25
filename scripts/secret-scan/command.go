package main

import (
	"context"
	"os/exec"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitrev"
)

// toolFunc は、ディレクトリ dir で段 step の道具を起動するコマンドを作ります。rev は範囲の段だけが使う、
// resolveFrom が解決した完全な sha です。テストは差し替えます。
type toolFunc func(dir string, env []string, step, rev string) *exec.Cmd

// runner は、走査器と git の起動、子プロセスへ渡す env の元、リポジトリ直下です。
type runner struct {
	tool    toolFunc
	git     toolFunc
	environ []string
	root    string
}

const (
	stepResolve      = "git-resolve"
	stepNumstatRange = "git-numstat-range"
	stepNumstatHead  = "git-numstat-head"
	stepLsFiles      = "git-ls-files"
	stepInit         = "git-init"
	stepCommitBase   = "git-commit-base"
	stepAddCanary    = "git-add-canary"
	stepCommitCanary = "git-commit-canary"

	stepGitleaksRange   = "gitleaks-range"
	stepGitleaksHistory = "gitleaks-history"

	stepTrufflehogRange          = "trufflehog-range-text"
	stepTrufflehogRangeJSON      = "trufflehog-range-json"
	stepTrufflehogHistory        = "trufflehog-history-text"
	stepTrufflehogHistoryJSON    = "trufflehog-history-json"
	stepTrufflehogFilesystem     = "trufflehog-filesystem-text"
	stepTrufflehogFilesystemJSON = "trufflehog-filesystem-json"

	stepTrivyCanary = "trivy-canary"
	stepTrivyTable  = "trivy-table"
	stepTrivyJSON   = "trivy-json"
	stepTrivySARIF  = "trivy-sarif"
)

// 走査器が作業ディレクトリから読む、固定名のファイルです。canary は同じ名前の写しを canary のディレクトリへ置きます。
const (
	gitleaksConfig = ".gitleaks.toml"
	gitleaksIgnore = ".gitleaksignore"
	trivyIgnore    = ".trivyignore.yaml"

	trufflehogRangeExclude   = "tmp/secret-scan/trufflehog-range/exclude-paths.txt"
	trufflehogHistoryExclude = "tmp/secret-scan/trufflehog-history/exclude-paths.txt"
)

// command は toolFunc の実物です。引数は段ごとに固定で、範囲の段だけが rev を置きます。rev が完全な sha の形で
// なければ、範囲の段は道具を起動せずに落ちます。知らない段も同じです。
//
// gitleaks の設定と抑止は明示で渡します。省くと gitleaks は環境変数 GITLEAKS_CONFIG と走査先の .gitleaks.toml を
// 探し、本走査の設定が環境で黙って差し替わります。`--ignore-gitleaks-allow` と trufflehog の `--no-ignore-tag` は、
// 行に置く無視の印を効かせません（ADR-0302 決定9）。trufflehog は候補を発行元の API へ送って確かめず
// （`--no-verification`。同 決定2）、`--fail-on-scan-errors` を外しません —— 外すと、範囲の起点が解決できないときも
// 0件の走査として 0 で終わります。
func command(dir string, env []string, step, rev string) *exec.Cmd {
	ctx := context.Background()
	var cmd *exec.Cmd
	ranged := step == stepNumstatRange || step == stepGitleaksRange || step == stepTrufflehogRange || step == stepTrufflehogRangeJSON
	if ranged && !gitrev.IsHash(rev) {
		step = ""
	}
	switch step {
	case stepResolve:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "cat-file", "--batch-check=%(objectname) %(objecttype)")
	case stepNumstatRange:
		//nolint:gosec // G204: rev は resolveFrom が git で commit へ解決し、gitrev.IsHash で確かめた完全な sha だけ（上の分岐）
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "log", "--no-merges", "-M", "--diff-filter=d", "--format=commit %H", "--numstat", "-z", rev+"..HEAD")
	case stepNumstatHead:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "log", "--no-merges", "-M", "--diff-filter=d", "--format=commit %H", "--numstat", "-z", "HEAD")
	case stepLsFiles:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "core.quotepath=false", "ls-files", "-z", "-co", "--exclude-standard")
	case stepInit:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "init", "-q")
	case stepCommitBase:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "user.name=canary", "-c", "user.email=canary@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "base")
	case stepAddCanary:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "add", "--", canaryCredentials)
	case stepCommitCanary:
		cmd = exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "user.name=canary", "-c", "user.email=canary@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "canary")
	case stepGitleaksRange:
		//nolint:gosec // G204: rev は resolveFrom が git で commit へ解決し、gitrev.IsHash で確かめた完全な sha だけ（上の分岐）
		cmd = exec.CommandContext(ctx, "gitleaks", "git", "--redact", "--no-banner", "--ignore-gitleaks-allow", "--config", gitleaksConfig, "--gitleaks-ignore-path", ".", "--report-format", "json", "--report-path", "-", "--log-opts="+rev+"..HEAD", ".")
	case stepGitleaksHistory:
		cmd = exec.CommandContext(ctx, "gitleaks", "git", "--redact", "--no-banner", "--ignore-gitleaks-allow", "--config", gitleaksConfig, "--gitleaks-ignore-path", ".", "--report-format", "json", "--report-path", "-", ".")
	case stepTrufflehogRange:
		//nolint:gosec // G204: rev は resolveFrom が git で commit へ解決し、gitrev.IsHash で確かめた完全な sha だけ（上の分岐）
		cmd = exec.CommandContext(ctx, "trufflehog", "git", "file://.", "--since-commit", rev, "--branch", "HEAD", "--no-verification", "--no-update", "--fail", "--fail-on-scan-errors", "--no-ignore-tag", "--results=verified,unverified,unknown", "--exclude-paths="+trufflehogRangeExclude)
	case stepTrufflehogRangeJSON:
		//nolint:gosec // G204: rev は resolveFrom が git で commit へ解決し、gitrev.IsHash で確かめた完全な sha だけ（上の分岐）
		cmd = exec.CommandContext(ctx, "trufflehog", "git", "file://.", "--since-commit", rev, "--branch", "HEAD", "--no-verification", "--no-update", "--fail", "--fail-on-scan-errors", "--no-ignore-tag", "--results=verified,unverified,unknown", "--exclude-paths="+trufflehogRangeExclude, "--json")
	case stepTrufflehogHistory:
		cmd = exec.CommandContext(ctx, "trufflehog", "git", "file://.", "--no-verification", "--no-update", "--fail", "--fail-on-scan-errors", "--no-ignore-tag", "--results=verified,unverified,unknown", "--exclude-paths="+trufflehogHistoryExclude)
	case stepTrufflehogHistoryJSON:
		cmd = exec.CommandContext(ctx, "trufflehog", "git", "file://.", "--no-verification", "--no-update", "--fail", "--fail-on-scan-errors", "--no-ignore-tag", "--results=verified,unverified,unknown", "--exclude-paths="+trufflehogHistoryExclude, "--json")
	case stepTrufflehogFilesystem:
		cmd = exec.CommandContext(ctx, "trufflehog", "filesystem", ".", "--no-verification", "--no-update", "--fail", "--fail-on-scan-errors", "--no-ignore-tag", "--results=verified,unverified,unknown")
	case stepTrufflehogFilesystemJSON:
		cmd = exec.CommandContext(ctx, "trufflehog", "filesystem", ".", "--no-verification", "--no-update", "--fail", "--fail-on-scan-errors", "--no-ignore-tag", "--results=verified,unverified,unknown", "--json")
	case stepTrivyCanary:
		cmd = exec.CommandContext(ctx, "trivy", "fs", "--scanners", "secret", "--exit-code", "0", "--skip-version-check", "--quiet", "--format", "json", ".")
	case stepTrivyTable:
		cmd = exec.CommandContext(ctx, "trivy", "fs", "--scanners", "secret", "--exit-code", "1", "--ignorefile", trivyIgnore, "--skip-version-check", "--format", "table", ".")
	case stepTrivyJSON:
		cmd = exec.CommandContext(ctx, "trivy", "fs", "--scanners", "secret", "--exit-code", "1", "--ignorefile", trivyIgnore, "--skip-version-check", "--format", "json", ".")
	case stepTrivySARIF:
		cmd = exec.CommandContext(ctx, "trivy", "fs", "--scanners", "secret", "--exit-code", "1", "--ignorefile", trivyIgnore, "--skip-version-check", "--format", "sarif", ".")
	default:
		cmd = exec.CommandContext(ctx, "false")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}
