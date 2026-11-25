package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitrev"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// canaryCredentials は、canary のリポジトリに commit する偽の資格情報のファイル名です。
const canaryCredentials = "credentials"

// gitOutput は、dir で git の段 step を起動し、stdout を返します。stdin が空でなければ git の stdin へ渡します。
// 0 以外で終われば errInput です。
func gitOutput(r runner, env []string, dir, step, rev, stdin string) (string, error) {
	cmd := r.git(dir, env, step, rev)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	res, err := toolrun.Run(cmd)
	if err != nil {
		return "", xerrors.Wrapf(err, "git（%s）", step)
	}
	if res.Code != 0 {
		return "", xerrors.Wrapf(errInput, "git（%s）が終了コード %d で終わりました: %s", step, res.Code, strings.TrimSpace(res.Combined))
	}

	return res.Stdout, nil
}

// resolveFrom は、範囲の起点 from を dir のリポジトリで commit へ解決し、完全な sha を返します（取り決めは
// [gitrev]）。空は errFromEmpty、解決できないものは errFromUnresolved です。
func resolveFrom(r runner, env []string, dir, from string) (string, error) {
	unresolved := xerrors.Wrapf(errFromUnresolved, "走査範囲の起点 %q が解決できません", from)
	stdin, err := gitrev.Query(from)
	if errors.Is(err, gitrev.ErrEmpty) {
		return "", errFromEmpty
	}
	if err != nil {
		return "", unresolved
	}
	out, err := gitOutput(r, env, dir, stepResolve, "", stdin)
	if err != nil {
		return "", err
	}
	sha, err := gitrev.Parse(out)
	if err != nil {
		return "", unresolved
	}

	return sha, nil
}

// numstat は、dir のリポジトリの範囲 rev..HEAD（rev が空なら HEAD の全履歴）の、runCount が読む numstat を返します。
func numstat(r runner, env []string, dir, rev string) ([]byte, error) {
	step := stepNumstatHead
	if rev != "" {
		step = stepNumstatRange
	}
	out, err := gitOutput(r, env, dir, step, rev, "")

	return []byte(out), err
}

// buildCanaryRepo は、dir にリポジトリを建て、空の base の commit と、偽の資格情報 credentials を足す commit を
// 積みます。base の commit の sha（範囲の起点）を返します。
func buildCanaryRepo(r runner, env []string, dir, credentials string) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", xerrors.Wrapf(errInput, "canary のリポジトリの作成: %v", err)
	}
	for _, step := range []string{stepInit, stepCommitBase} {
		if _, err := gitOutput(r, env, dir, step, "", ""); err != nil {
			return "", err
		}
	}
	from, err := resolveFrom(r, env, dir, "HEAD")
	if err != nil {
		return "", err
	}
	if err := writeFile(filepath.Join(dir, canaryCredentials), credentials); err != nil {
		return "", err
	}
	for _, step := range []string{stepAddCanary, stepCommitCanary} {
		if _, err := gitOutput(r, env, dir, step, "", ""); err != nil {
			return "", err
		}
	}

	return from, nil
}

// fakeAWSCredentials は、AWS の検出器が拾う形の偽の資格情報を乱数で作ります。検出器は ID と secret のエントロピーに
// 閾値を持つので、固定の値を使いません。
func fakeAWSCredentials() string {
	return fmt.Sprintf("[default]\naws_access_key_id = %s%s\naws_secret_access_key = %s\n",
		"AK"+"IA", randomString("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", 16),
		randomString("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789/+", 40))
}

func fakeGitHubToken() string {
	return fmt.Sprintf("token = %q\n", "gh"+"p_"+randomString("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", 36))
}

// randomString は、alphabet の文字を n 個、乱数で並べます。偏りは検出器のエントロピーの閾値に届く限り問いません。
func randomString(alphabet string, n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand.Read は失敗しない（Go 1.24 以降）。
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}

	return string(b)
}
