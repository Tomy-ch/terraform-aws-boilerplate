// Package main は、手順書の手順のうち人が実行するものの、確認と手順の実行を持つツール（ADR-0702 決定1・26・27）。
// 起動する道具は aws だけで、資格情報を変える環境変数の外し方と AWS_PROFILE は make が整える。
// 手順の実行は、読み取りで前提と現状を確かめ、差分を示し、project の名前の打ち込みを受けてから書き込み、読み直して確かめる。
//
//	setup <入口> -project <project> -region <region> [-accounts <accounts.tf>] [-env <env>] [-ids]
//
// 入口（organization-baseline の手順書）:
//
//	member-emails          3.8 手順3: メンバーアカウントのメールの parameter を置く（-accounts）
//	member-emails-check    3.8 手順3 の確認（-accounts）
//	member-email-update    3.8 末尾: アカウントのメールを AWS の側で変え、parameter を合わせる（-accounts・-env）
//	account-quota-check    3.8 手順2: アカウント数の上限の確認（-accounts）
//	member-accounts-check  3.8 手順4.1: メンバーアカウントが期待する OU に ACTIVE で在る（-accounts。-ids でアカウントの ID も出す）
//	stacksets-access       3.5: StackSets の信頼アクセスを有効にする
//	stacksets-access-check 3.5 の確認
//	stack-instances-check  3.5 の昇格の後: project の名前で始まる StackSet の stack instance がすべて CURRENT である
//	service-access         3.9: セキュリティのサービスの信頼アクセスを有効にする
//	service-access-check   3.9 の確認
//
// 終了コードは、0 が完了か確認の一致、1 が確認の NG か前提が崩れていて書かなかったこと、2 がそれ以外
// （引数の誤り、aws の失敗、確認の入力の不一致を含む）。-ids を付けない限り、12桁の数字（アカウント ID）は伏せる。
package main

import (
	"flag"
	"io"
	"os"
	"regexp"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	accountPattern = regexp.MustCompile(`[0-9]{12}`)

	errUsage = xerrors.New("使い方: setup <入口> -project <project> -region <region> [-accounts <accounts.tf>] [-env <env>] [-ids]（入口は scripts/setup/main.go の冒頭）")
)

type deps struct {
	aws      awsFunc
	getenv   func(string) string
	readFile func(string) ([]byte, error)
	stdin    io.Reader
	isTTY    bool
}

type options struct {
	entry    string
	project  string
	region   string
	accounts string
	env      string
	showIDs  bool
}

// entry は、1つの入口。writes はその入口が起動してよい書き込みの操作（"<service> <operation>"）で、確認は空。
type entry struct {
	writes   []string
	members  bool
	needsEnv bool
	run      func(o options, c *client, t *terminal, members []memberAccount) error
}

var entries = map[string]entry{
	"member-emails": {
		writes: []string{"ssm put-parameter"}, members: true,
		run: putMemberEmails,
	},
	"member-emails-check": {
		members: true,
		run: func(o options, c *client, t *terminal, m []memberAccount) error {
			return checkMemberEmails(o, c, t.out, m)
		},
	},
	"member-email-update": {
		writes:  []string{"organizations enable-aws-service-access", "account start-primary-email-update", "account accept-primary-email-update", "ssm put-parameter"},
		members: true, needsEnv: true,
		run: updateMemberEmail,
	},
	"account-quota-check": {
		members: true,
		run: func(o options, c *client, t *terminal, m []memberAccount) error {
			return checkAccountQuota(o, c, t.out, m)
		},
	},
	"member-accounts-check": {
		members: true,
		run: func(o options, c *client, t *terminal, m []memberAccount) error {
			return checkMemberAccounts(o, c, t.out, m)
		},
	},
	"stacksets-access": {
		writes: []string{"cloudformation activate-organizations-access"},
		run: func(o options, c *client, t *terminal, _ []memberAccount) error {
			return enableStackSetsAccess(o, c, t)
		},
	},
	"stack-instances-check": {
		run: func(o options, c *client, t *terminal, _ []memberAccount) error {
			return checkStackInstances(o, c, t.out)
		},
	},
	"stacksets-access-check": {
		run: func(_ options, c *client, t *terminal, _ []memberAccount) error {
			return checkStackSetsAccess(c, t.out)
		},
	},
	"service-access": {
		writes: []string{"organizations enable-aws-service-access"},
		run: func(o options, c *client, t *terminal, _ []memberAccount) error {
			return enableServiceAccess(o, c, t)
		},
	},
	"service-access-check": {
		run: func(_ options, c *client, t *terminal, _ []memberAccount) error {
			return checkServiceAccess(c, t.out)
		},
	},
}

func main() {
	stat, err := os.Stdin.Stat()
	isTTY := err == nil && stat.Mode()&os.ModeCharDevice != 0

	os.Exit(execute(os.Args[1:], os.Stdout, deps{
		aws: hostAWS("aws"), getenv: os.Getenv, readFile: os.ReadFile, stdin: os.Stdin, isTTY: isTTY,
	}))
}

// execute は、出力を伏せて stdout へ書き、終了コードを返す。問いは入力を待つ前に人へ見えるよう、
// 伏せる処理を通して行ごとに書く。
func execute(args []string, stdout io.Writer, d deps) int {
	o, _, perr := parseArgs(args)
	w := &maskWriter{w: stdout, mask: perr != nil || !o.showIDs}

	err := run(args, w, d)
	if err != nil {
		_, _ = io.WriteString(w, "❌ "+err.Error()+"\n")
	}

	return exitCode(err)
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errNG), xerrors.Is(err, errPrecondition):
		return 1
	default:
		return 2
	}
}

// maskWriter は、書かれたものの12桁の数字を伏せる。問いの行は改行で終わらないので、行に分けずにそのまま伏せる。
type maskWriter struct {
	w    io.Writer
	mask bool
}

func (m *maskWriter) Write(p []byte) (int, error) {
	out := p
	if m.mask {
		out = accountPattern.ReplaceAll(p, []byte("<account>"))
	}

	if _, err := m.w.Write(out); err != nil {
		return 0, err
	}

	return len(p), nil
}

func parseArgs(args []string) (options, entry, error) {
	if len(args) == 0 {
		return options{}, entry{}, errUsage
	}

	o := options{entry: args[0]}

	e, ok := entries[o.entry]
	if !ok {
		return options{}, entry{}, xerrors.Wrap(errUsage, "入口 "+o.entry+" はありません")
	}

	fs := flag.NewFlagSet(o.entry, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.project, "project", "", "")
	fs.StringVar(&o.region, "region", "", "")
	fs.StringVar(&o.accounts, "accounts", "", "")
	fs.StringVar(&o.env, "env", "", "")
	fs.BoolVar(&o.showIDs, "ids", false, "")

	if err := fs.Parse(args[1:]); err != nil {
		return options{}, entry{}, xerrors.Wrap(errUsage, err.Error())
	}

	switch {
	case fs.NArg() != 0, o.project == "", o.region == "":
		return options{}, entry{}, errUsage
	case e.members != (o.accounts != ""):
		return options{}, entry{}, xerrors.Wrap(errUsage, "-accounts は env の一覧を使う入口にだけ渡します")
	case e.needsEnv != (o.env != ""):
		return options{}, entry{}, xerrors.Wrap(errUsage, "-env は member-email-update にだけ渡します")
	case o.showIDs && o.entry != "member-accounts-check":
		return options{}, entry{}, xerrors.Wrap(errUsage, "-ids は member-accounts-check にだけ渡します")
	}

	return o, e, nil
}

func run(args []string, out io.Writer, d deps) error {
	o, e, err := parseArgs(args)
	if err != nil {
		return err
	}

	if _, err := awsProfile(d.getenv); err != nil {
		return err
	}

	var members []memberAccount

	if e.members {
		src, err := d.readFile(o.accounts)
		if err != nil {
			return xerrors.Join(errHCL, err)
		}

		if members, err = readMemberAccounts(src, o.accounts); err != nil {
			return err
		}
	}

	c := &client{aws: d.aws, region: o.region, writes: e.writes}
	t := newTerminal(d.stdin, out, d.isTTY)

	return e.run(o, c, t, members)
}
