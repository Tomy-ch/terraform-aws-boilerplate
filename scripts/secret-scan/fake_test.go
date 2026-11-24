package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// scanner は走査器の偽物である。段 step の振る舞いは、制御用のディレクトリの `<段>.out`（stdout）・`<段>.err`
// （stderr）・`<段>.rc`（終了コード）・`<段>.kill`（自身をシグナルで止める）で決まり、`<段>@<作業ディレクトリの名前>.*`
// はその作業ディレクトリでだけ段の既定に勝つ。起動ごとに「作業ディレクトリの名前|段|rev|GIT_DIR」を calls.log へ
// 控え、作業ディレクトリに次のものがあれば制御用のディレクトリへ写す。
//
//	tmp/secret-scan/trufflehog-{range,history}/exclude-paths.txt  ->  exclude-<段>@<名前>
//	.gitleaks.toml と .gitleaksignore を連結したもの               ->  config-<段>@<名前>
//	作業ディレクトリの下のファイルの一覧（filesystem の段だけ）    ->  files-<段>@<名前>
//	tmp/secret-scan/trivy が在るか（yes / no）                     ->  trivy-work-<段>
//	TRIVY_PROBE の値                                               ->  trivy-env-<段>
type scanner struct {
	t   *testing.T
	dir string
}

func newScanner(t *testing.T) *scanner {
	t.Helper()
	s := &scanner{t: t, dir: t.TempDir()}
	body := `#!/bin/sh
ctl='` + s.dir + `'
step="$1"
name=$(basename "$(pwd -P)")
echo "$name|$step|${FAKE_REV:-}|${GIT_DIR:-}" >> "$ctl/calls.log"
if [ -e "$ctl/$step.kill" ]; then kill -9 $$; fi
for x in tmp/secret-scan/trufflehog-range/exclude-paths.txt tmp/secret-scan/trufflehog-history/exclude-paths.txt; do
	if [ -f "$x" ]; then cp "$x" "$ctl/exclude-$step@$name"; fi
done
if [ -f .gitleaks.toml ] && [ -f .gitleaksignore ]; then cat .gitleaks.toml .gitleaksignore > "$ctl/config-$step@$name"; fi
case "$step" in trufflehog-filesystem-*) find . \( -type f -o -type l \) | LC_ALL=C sort > "$ctl/files-$step@$name" ;; esac
if [ -e tmp/secret-scan/trivy ]; then echo yes > "$ctl/trivy-work-$step"; else echo no > "$ctl/trivy-work-$step"; fi
echo "${TRIVY_PROBE:-}" > "$ctl/trivy-env-$step"
pick() { if [ -e "$ctl/$step@$name.$1" ]; then echo "$ctl/$step@$name.$1"; elif [ -e "$ctl/$step.$1" ]; then echo "$ctl/$step.$1"; fi; }
f=$(pick out); if [ -n "$f" ]; then cat "$f"; fi
f=$(pick err); if [ -n "$f" ]; then cat "$f" >&2; fi
f=$(pick rc); if [ -n "$f" ]; then exit "$(cat "$f")"; fi
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(s.dir, "tool"), []byte(body), 0o600))

	return s
}

// tool は、偽物を起動する toolFunc である。スクリプトは exec せず /bin/sh に読ませる（並行のテストが書いたばかりの
// スクリプトの exec は ETXTBSY で落ちうる。tooltest.Fake.Tool と同じ）。rev は FAKE_REV で渡す。
func (s *scanner) tool() toolFunc {
	script := filepath.Join(s.dir, "tool")
	return func(dir string, env []string, step, rev string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), "/bin/sh", script, step)
		c.Dir, c.Env = dir, append(slices.Clone(env), "FAKE_REV="+rev)
		return c
	}
}

// set は、制御用のディレクトリに `<key>.<suffix>` を body で書く。key は段か `<段>@<名前>` である。
func (s *scanner) set(key, suffix, body string) {
	s.t.Helper()
	require.NoError(s.t, os.WriteFile(filepath.Join(s.dir, key+"."+suffix), []byte(body), 0o600))
}

func (s *scanner) read(name string) string {
	s.t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.dir, name))
	require.NoError(s.t, err)

	return string(raw)
}

type call struct {
	dir, step, rev, gitDir string
}

// calls は、起動を順に返す。1度も起動していなければ nil である。
func (s *scanner) calls() []call {
	s.t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(s.t, err)
	var out []call
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		f := strings.Split(line, "|")
		require.Len(s.t, f, 4, line)
		out = append(out, call{f[0], f[1], f[2], f[3]})
	}

	return out
}

// steps は、起動ごとの「作業ディレクトリの名前 段」である。
func (s *scanner) steps() []string {
	var out []string
	for _, c := range s.calls() {
		out = append(out, c.dir+" "+c.step)
	}

	return out
}

// behave は、作業ディレクトリ name で段 step が stdout に out を出し、終了コード rc で終わるようにする。
func (s *scanner) behave(step, name, out, rc string) {
	s.set(step+"@"+name, "out", out)
	s.set(step+"@"+name, "rc", rc)
}

const (
	gitleaksHit   = `[{"RuleID": "aws-access-token"}]`
	trufflehogHit = `{"DetectorName":"AWS","SourceMetadata":{}}` + "\n"
	trivyHit      = `{"Results":[{"Target":"canary.txt","Secrets":[{"RuleID":"github-pat"}]}]}`
)

// passingCanaries は、すべての canary が検出するように偽物を置く。gitleaks は canary で走査した数を1件と報告する
// （canary のリポジトリは、数える commit を1つだけ持つ）。
func (s *scanner) passingCanaries() {
	for _, step := range []string{stepGitleaksRange, stepGitleaksHistory} {
		s.behave(step, "canary", gitleaksHit, "1")
		s.set(step+"@canary", "err", "INF 1 commits scanned.\n")
	}
	for _, step := range []string{stepTrufflehogRangeJSON, stepTrufflehogHistoryJSON} {
		s.behave(step, "canary-git", trufflehogHit, "183")
	}
	s.behave(stepTrufflehogFilesystemJSON, "canary-fs", trufflehogHit, "183")
	s.behave(stepTrivyCanary, "canary", trivyHit, "0")
}

// repo は走査の対象のリポジトリ（root）である。設定と抑止のファイルを持ち、tmp/ を ignore する。
type repo struct {
	*fixture
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	f := newFixture(t)
	f.write(".gitignore", "tmp/\n")
	f.write(gitleaksConfig, "# config\n")
	f.write(gitleaksIgnore, "# ignore\n")
	f.write(trufflehogExclude, "# base\n^base-excluded$\n")
	f.write(trufflehogExcludeHistory, "# history\n^history-excluded$\n")
	f.commit("config")

	return &repo{f}
}

// name は、偽物が控える root の作業ディレクトリの名前である。
func (r *repo) name() string {
	return filepath.Base(r.dir)
}

func (r *repo) runner(s *scanner) runner {
	return runner{tool: s.tool(), git: command, environ: os.Environ(), root: r.dir}
}

// leaked は、外側のリポジトリ outer を指す GIT_DIR と GIT_INDEX_FILE を足した environ である。
func leaked(outer string) []string {
	return append(os.Environ(), "GIT_DIR="+filepath.Join(outer, ".git"), "GIT_INDEX_FILE="+filepath.Join(outer, ".git", "index"))
}

func noWork(t *testing.T, root, name string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, "tmp", "secret-scan", name))
	require.ErrorIs(t, err, os.ErrNotExist, "作業ディレクトリ %s が残っている", name)
}
