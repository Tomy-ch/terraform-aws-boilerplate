package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/awsenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// errNoProfile: 資格情報を変える環境変数を外し、profile を渡すのは make である。
	errNoProfile         = xerrors.New("AWS_PROFILE が空です。make の setup- の入口から起動してください")
	errStaticCredentials = xerrors.New("環境に profile を迂回する資格情報の指定が在ります。make の setup- の入口から起動してください（make が外します）")
	// errAWSStart は、aws を起動できないこと（PATH に無いなど）。
	errAWSStart    = xerrors.New("aws を起動できません")
	errAWSFailed   = xerrors.New("aws の呼び出しが失敗しました")
	errAWSResponse = xerrors.New("aws の応答を読めません")
	// errNotAllowed は、入口の一覧に無い書き込みの操作か、読み取りでも書き込みでもない操作を起動しようとしたこと。
	errNotAllowed = xerrors.New("この入口が起動してよい aws の操作ではありません")

	readOnlyPrefixes = []string{"get-", "list-", "describe-"}
)

// awsProfile は、aws に使わせる profile。静的な資格情報が環境に在るか、AWS_PROFILE が空なら、aws を起動せずに止める。
func awsProfile(getenv func(string) string) (string, error) {
	if found := awsenv.Found(getenv); len(found) > 0 {
		return "", xerrors.Wrap(errStaticCredentials, strings.Join(found, "・"))
	}

	profile := getenv("AWS_PROFILE")
	if profile == "" {
		return "", errNoProfile
	}

	return profile, nil
}

// awsResult は、aws の1回の起動の結果。err は終了の失敗（*exec.ExitError）か起動の失敗。
type awsResult struct {
	stdout []byte
	stderr string
	err    error
}

// awsFunc は aws を引数どおりに起動する。環境は呼び出し元（make）が整えたものを引き継ぐ。
type awsFunc func(args ...string) awsResult

// hostAWS は、bin（名前なら PATH から探す）を子プロセスとして起動する awsFunc を返す。
func hostAWS(bin string) awsFunc {
	return func(args ...string) awsResult {
		path, err := exec.LookPath(bin)
		if err != nil {
			return awsResult{err: err}
		}

		var stdout, stderr bytes.Buffer

		cmd := &exec.Cmd{Path: path, Args: append([]string{bin}, args...), Stdout: &stdout, Stderr: &stderr}
		err = cmd.Run()

		return awsResult{stdout: stdout.Bytes(), stderr: stderr.String(), err: err}
	}
}

// client は、aws に --output json と --region を付けて起動する。読み取りの操作（get-・list-・describe-）は
// どれも起動してよく、書き込みの操作は writes に在るものだけを起動する（ADR-0702 決定26・27）。
type client struct {
	aws    awsFunc
	region string
	writes []string
	notify notifier
}

// op は、"<service> <operation>" の形の操作名。
func op(args []string) string {
	if len(args) < 2 {
		return strings.Join(args, " ")
	}

	return args[0] + " " + args[1]
}

func isRead(args []string) bool {
	return len(args) >= 2 && slices.ContainsFunc(readOnlyPrefixes, func(p string) bool { return strings.HasPrefix(args[1], p) })
}

// call は、許された操作だけを起動する。region が空でなければ、client の region の代わりに使う
// （us-east-1 に固定されたサービスのため）。
func (c *client) call(region string, args ...string) (awsResult, error) {
	if !isRead(args) && !slices.Contains(c.writes, op(args)) {
		return awsResult{}, xerrors.Wrap(errNotAllowed, "aws "+op(args))
	}

	if region == "" {
		region = c.region
	}

	r := c.aws(append(append([]string{}, args...), "--output", "json", "--region", region)...)

	var exitErr *exec.ExitError

	switch {
	case r.err == nil:
		return r, nil
	case xerrors.As(r.err, &exitErr):
		return r, xerrors.Wrap(errAWSFailed, "aws "+op(args)+": "+firstLine(r.stderr))
	default:
		return r, xerrors.Join(errAWSStart, r.err)
	}
}

// read は、読み取りの操作を起動し、応答の JSON を v へ読む。
func (c *client) read(v any, args ...string) error {
	return c.readIn("", v, args...)
}

func (c *client) readIn(region string, v any, args ...string) error {
	if !isRead(args) {
		return xerrors.Wrap(errNotAllowed, "aws "+op(args))
	}

	r, err := c.call(region, args...)
	if err != nil {
		return err
	}

	// null と object でない応答は、読む先をゼロ値のまま成功させるので受け入れない。
	if b := bytes.TrimSpace(r.stdout); len(b) == 0 || b[0] != '{' {
		return xerrors.Wrap(errAWSResponse, "aws "+op(args)+": JSON の object ではありません")
	}

	if err := json.Unmarshal(r.stdout, v); err != nil {
		return xerrors.Join(errAWSResponse, xerrors.Wrap(err, "aws "+op(args)))
	}

	return nil
}

// readOptional は read と同じだが、stderr が notFound の error code を含む失敗を「無い」（false）として返す。
func (c *client) readOptional(notFound string, v any, args ...string) (bool, error) {
	err := c.read(v, args...)
	if err == nil {
		return true, nil
	}

	if xerrors.Is(err, errAWSFailed) && strings.Contains(err.Error(), notFound) {
		return false, nil
	}

	return false, err
}

// write は、書き込みの操作を起動する。応答は読まない（確かめは後の読み取りが持つ）。
func (c *client) write(args ...string) error {
	if isRead(args) {
		return xerrors.Wrap(errNotAllowed, "aws "+op(args))
	}

	_, err := c.call("", args...)

	return err
}

// writeSecret は write と同じだが、secret を引数に載せず、所有者だけが読める一時ファイルの JSON として
// --cli-input-json で渡す（ADR-0702 決定27）。一時ファイルは起動の後に消す。
func (c *client) writeSecret(secret map[string]any, args ...string) error {
	if isRead(args) {
		return xerrors.Wrap(errNotAllowed, "aws "+op(args))
	}

	body, err := json.Marshal(secret)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "setup-*.json")
	if err != nil {
		return err
	}

	// 中断のシグナルで既定どおりに終わると、defer が走らず秘密がファイルに残る。消すまでシグナルを預かる。
	sigs := make(chan os.Signal, 1)
	defer c.notify.start(sigs)()
	defer func() { _ = os.Remove(f.Name()) }()

	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()

		return err
	}

	if _, err := f.Write(body); err != nil {
		_ = f.Close()

		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	_, err = c.call("", append(append([]string{}, args...), "--cli-input-json", "file://"+f.Name())...)

	select {
	case sig := <-sigs:
		return xerrors.Wrap(errInterrupted, sig.String())
	default:
		return err
	}
}

// firstLine は、空白だけでない最初の行。無ければ「不明」。
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimRight(line, "\r")
		}
	}

	return "不明"
}
