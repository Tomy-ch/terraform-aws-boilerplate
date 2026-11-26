// Package main は、手順書の手順のうち人が実行するものの、確認と手順の実行を持つツール（ADR-0702 決定1・26・27）。
// 起動する道具は aws だけで、資格情報を変える環境変数の外し方と AWS_PROFILE は make が整える。
// 手順の実行は、資格情報が組織の管理アカウントのものであることと前提と現状を読み取りで確かめ、差分を示し、
// project の名前の打ち込みを受けてから書き込み、読み直して確かめる。
//
//	setup <入口> -project <project> -region <region> [-usecase <dir>] [-env <env>] [-user <user>] [-ids]
//
// 入口（organization-baseline の手順書。-usecase は usecases/organization-baseline）:
//
//	member-emails          3.8 手順3: メンバーアカウントのメールの parameter を置く（-usecase）
//	member-emails-check    3.8 手順3 の確認（-usecase）
//	member-email-update    3.8 末尾: アカウントのメールを AWS の側で変え、parameter を合わせる（-usecase・-env）
//	account-quota-check    3.8 手順2: アカウント数の上限の確認（-usecase）
//	member-accounts-check  3.8 手順4.1: メンバーアカウントが期待する OU に ACTIVE で在る（-usecase。-ids でアカウントの ID も出す）
//	stacksets-access       3.5: StackSets の信頼アクセスを有効にする
//	stacksets-access-check 3.5 の確認
//	stack-instances-check  3.5 の昇格の後: break-glass の StackSet が在り、project の StackSet がメンバーアカウントのそれぞれに CURRENT の stack instance を持つ（-usecase）
//	service-access         3.9: セキュリティのサービスの信頼アクセスを有効にする
//	service-access-check   3.9 の確認
//	delegated-admin-check  3.9 の昇格の後: サービスの主体のそれぞれで、委任管理者が security のアカウントだけ（-usecase）
//	organization-check     3.2: 組織の機能、Root の policy の種類、Root の直下の OU、IAM の組織の機能（-usecase）
//	identity-center-check  3.3 手順1 の確認: 組織の管理アカウントのインスタンスが ACTIVE でただ1つ在る
//	identity-center-user-check 3.3 手順4 の確認: user が在り、その ID を出す。user の名前に環境の接尾辞が無い（-usecase・-user）
//	break-glass-login      3.4 手順1: パスワードの無い break-glass の user に、伏せて打ち込んだパスワードを置く（-usecase）
//	break-glass-mfa        3.4 手順2: MFA の無い break-glass の user に、ハードウェアの TOTP の装置を登録する（-usecase）
//	break-glass-check      3.4 の確認: パスワードとハードウェアの MFA が在り、アクセスキーが無く、password policy が宣言どおり（-usecase）
//	break-glass-usage-check 3.6 の忘れ: break-glass の user のサインインと role の引き受けが45日以内（-usecase）
//
// env の一覧、OU の階層、メールの形、break-glass の user と role と password policy、委任管理者のサービスの主体は、
// -usecase の Terraform から毎回読む。
//
// 終了コードは、0 が完了か確認の一致、1 が確認の NG か、組織の状態が前提に合わず書かなかったこと（OU や
// Root が決まらない、同じ名前のアカウント、使われているメール、在るアカウントのメールの変更）、2 がそれ以外
// （引数の誤り、入力したメールや確認コードの形の誤り、確認の入力の不一致、aws の失敗、読めない応答）。-ids を付けない限り、12桁の数字（アカウント ID）は伏せる。
package main

import (
	"flag"
	"io"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	accountPattern = regexp.MustCompile(`[0-9]{12}`)

	errUsage = xerrors.New("使い方: setup <入口> -project <project> -region <region> [-usecase <dir>] [-env <env>] [-user <user>] [-ids]（入口は scripts/setup/main.go の冒頭）")
)

type deps struct {
	aws      awsFunc
	getenv   func(string) string
	readFile func(string) ([]byte, error)
	stdin    io.Reader
	isTTY    bool
	hidden   func() ([]byte, error)
	begin    func() (func(), error)
	now      func() time.Time
	sleep    func(time.Duration)
}

type options struct {
	entry   string
	project string
	region  string
	usecase string
	env     string
	user    string
	showIDs bool

	readFile func(string) ([]byte, error)
	now      func() time.Time
	sleep    func(time.Duration)
}

// entry は、1つの入口。writes はその入口が起動してよい書き込みの操作（"<service> <operation>"）で、確認は空。
// usecase は、その入口が organization-baseline の Terraform を読むか。
type entry struct {
	writes    []string
	usecase   bool
	needsEnv  bool
	needsUser bool
	run       func(o options, c *client, t *terminal, org organization) error
}

var entries = map[string]entry{
	"member-emails": {
		writes: []string{"ssm put-parameter"}, usecase: true,
		run: putMemberEmails,
	},
	"member-emails-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, org organization) error {
			return checkMemberEmails(o, c, t.out, org)
		},
	},
	"member-email-update": {
		writes:  []string{"organizations enable-aws-service-access", "account start-primary-email-update", "account accept-primary-email-update", "ssm put-parameter"},
		usecase: true, needsEnv: true,
		run: updateMemberEmail,
	},
	"account-quota-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, org organization) error {
			return checkAccountQuota(o, c, t.out, org)
		},
	},
	"member-accounts-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, org organization) error {
			return checkMemberAccounts(o, c, t.out, org)
		},
	},
	"stacksets-access": {
		writes: []string{"cloudformation activate-organizations-access"},
		run: func(o options, c *client, t *terminal, _ organization) error {
			return enableStackSetsAccess(o, c, t)
		},
	},
	"stacksets-access-check": {
		run: func(_ options, c *client, t *terminal, _ organization) error {
			return checkStackSetsAccess(c, t.out)
		},
	},
	"stack-instances-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, org organization) error {
			return checkStackInstances(o, c, t.out, org)
		},
	},
	"service-access": {
		writes: []string{"organizations enable-aws-service-access"},
		run: func(o options, c *client, t *terminal, _ organization) error {
			return enableServiceAccess(o, c, t)
		},
	},
	"service-access-check": {
		run: func(_ options, c *client, t *terminal, _ organization) error {
			return checkServiceAccess(c, t.out)
		},
	},
	"delegated-admin-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, _ organization) error {
			return checkDelegatedAdmin(o, c, t.out)
		},
	},
	"organization-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, _ organization) error {
			return checkOrganization(o, c, t.out)
		},
	},
	"identity-center-check": {
		run: func(o options, c *client, t *terminal, _ organization) error {
			return checkIdentityCenter(o, c, t.out)
		},
	},
	"identity-center-user-check": {
		usecase: true, needsUser: true,
		run: func(o options, c *client, t *terminal, org organization) error {
			return checkIdentityCenterUser(o, c, t.out, org)
		},
	},
	"break-glass-login": {
		writes: []string{"iam create-login-profile"}, usecase: true,
		run: func(o options, c *client, t *terminal, _ organization) error {
			return setBreakGlassLogin(o, c, t)
		},
	},
	"break-glass-mfa": {
		writes: []string{"iam enable-mfa-device"}, usecase: true,
		run: func(o options, c *client, t *terminal, _ organization) error {
			return setBreakGlassMFA(o, c, t)
		},
	},
	"break-glass-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, _ organization) error {
			return checkBreakGlass(o, c, t.out)
		},
	},
	"break-glass-usage-check": {
		usecase: true,
		run: func(o options, c *client, t *terminal, _ organization) error {
			return checkBreakGlassUsage(o, c, t.out)
		},
	},
}

func main() {
	begin, hidden := passwordReader(os.Stdin)

	os.Exit(execute(os.Args[1:], os.Stdout, deps{
		aws: hostAWS("aws"), getenv: os.Getenv, readFile: os.ReadFile, stdin: os.Stdin, isTTY: isTerminal(os.Stdin),
		hidden: hidden, begin: begin, now: time.Now, sleep: time.Sleep,
	}))
}

// errTerminalState は、端末の状態を読めないこと。
var errTerminalState = xerrors.New("端末の状態を読めません")

// passwordReader は、f の端末について newHiddenReader を組む。
func passwordReader(f *os.File) (func() (func(), error), func() ([]byte, error)) {
	fd := int(f.Fd())

	return newHiddenReader(
		func() (func() error, error) {
			state, err := term.GetState(fd)
			if err != nil {
				return nil, err
			}

			return func() error { return term.Restore(fd, state) }, nil
		},
		func() ([]byte, error) { return term.ReadPassword(fd) },
	)
}

// newHiddenReader は、端末の状態を控えて戻す関数を返す begin と、打ち込みを表示せずに1行を読む read を組む。
// 戻した後の read は読まない（中断の後に表示を止め直さない）。read が戻されていないことを確かめてから
// readPassword が表示を止めるまでの間に中断が済むと、表示は止まったまま残る。readPassword の中を挟めないので、
// その窓は閉じられない。
func newHiddenReader(getState func() (func() error, error), readPassword func() ([]byte, error)) (func() (func(), error), func() ([]byte, error)) {
	var (
		mu      sync.Mutex
		stopped bool
	)

	begin := func() (func(), error) {
		restoreState, err := getState()
		if err != nil {
			return nil, xerrors.Join(errTerminalState, err)
		}

		mu.Lock()
		stopped = false
		mu.Unlock()

		return func() {
			mu.Lock()
			stopped = true
			mu.Unlock()

			// 中断の後始末で、戻せなくても続きの終了は止めない。
			_ = restoreState()
		}, nil
	}

	read := func() ([]byte, error) {
		mu.Lock()
		s := stopped
		mu.Unlock()

		if s {
			return nil, errInterrupted
		}

		return readPassword()
	}

	return begin, read
}

// isTerminal は、f が端末か。文字デバイスでも /dev/null は端末として扱わない（< /dev/null で起動した書き込みを
// 端末からの入力と取り違えない）。
func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil || stat.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	null, err := os.Stat(os.DevNull)

	return err == nil && !os.SameFile(stat, null)
}

// execute は、出力を伏せて stdout へ書き、終了コードを返す。問いは入力を待つ前に人へ見えるよう、
// 伏せる処理を通して書いたものから順に出す。
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
	case xerrors.Is(err, errNG), xerrors.Is(err, errPrecondition), xerrors.Is(err, errEmailInUse), xerrors.Is(err, errEmailChangeNeedsUpdate),
		xerrors.Is(err, errAccountName), xerrors.Is(err, errOUNotFound), xerrors.Is(err, errNoRoot):
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
	fs.StringVar(&o.usecase, "usecase", "", "")
	fs.StringVar(&o.env, "env", "", "")
	fs.StringVar(&o.user, "user", "", "")
	fs.BoolVar(&o.showIDs, "ids", false, "")

	if err := fs.Parse(args[1:]); err != nil {
		return options{}, entry{}, xerrors.Wrap(errUsage, err.Error())
	}

	switch {
	case fs.NArg() != 0:
		return options{}, entry{}, xerrors.Wrap(errUsage, "余分な引数があります")
	case o.project == "", o.region == "":
		return options{}, entry{}, xerrors.Wrap(errUsage, "-project と -region は必須です")
	case e.usecase != (o.usecase != ""):
		return options{}, entry{}, xerrors.Wrap(errUsage, "-usecase は organization-baseline の Terraform を読む入口にだけ渡します")
	case e.needsEnv != (o.env != ""):
		return options{}, entry{}, xerrors.Wrap(errUsage, "-env は member-email-update にだけ渡します")
	case e.needsUser != (o.user != ""):
		return options{}, entry{}, xerrors.Wrap(errUsage, "-user は identity-center-user-check にだけ渡します")
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

	profile, err := awsProfile(d.getenv)
	if err != nil {
		return err
	}

	o.readFile, o.now, o.sleep = d.readFile, d.now, d.sleep

	var org organization

	if e.usecase {
		if org, err = readOrganization(d.readFile, o.usecase, o.project); err != nil {
			return err
		}
	}

	c := &client{aws: d.aws, region: o.region, writes: e.writes}
	t := newTerminal(d.stdin, out, d.isTTY)
	t.hidden, t.begin = d.hidden, d.begin

	if len(e.writes) > 0 {
		if err := requireManagementAccount(c, profile, out); err != nil {
			return err
		}
	}

	return e.run(o, c, t, org)
}
