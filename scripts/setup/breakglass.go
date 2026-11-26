package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	noSuchEntity = "NoSuchEntity"
	// unusedDays は、使われていない資格情報を所見にする日数。Security Hub の IAM.22（CIS 1.12）は45日以上を所見にする。
	unusedDays = 45
)

var (
	errPasswordMismatch = xerrors.New("2回の打ち込みが一致しないので、書き込みません")
	errMFAInput         = xerrors.New("MFA の装置のシリアルかコードの形が誤っています")

	// hardwareSerial は、ハードウェアの TOTP の装置のシリアル（IAM の SerialNumber の文字と長さ）。仮想 MFA と FIDO の
	// ARN は受けない。
	hardwareSerial = regexp.MustCompile(`^[\w+=/:,.@-]{9,256}$`)
	totpCode       = regexp.MustCompile(`^[0-9]{6}$`)
)

func readBreakGlass(o options) (breakGlassShape, error) {
	return readBreakGlassShape(o.readFile, o.usecase, o.project)
}

func userExists(c *client, name string) (bool, error) {
	var resp struct {
		User struct {
			UserName string `json:"UserName"`
		} `json:"User"`
	}

	found, err := c.readOptional(noSuchEntity, &resp, "iam", "get-user", "--user-name", name)
	if err != nil {
		return false, err
	}

	if found && resp.User.UserName != name {
		return false, xerrors.Wrap(errAWSResponse, "iam get-user: "+name+" の UserName がありません")
	}

	return found, nil
}

// requireUsers は、宣言の break-glass の user がすべて在ることを求める。無ければ apply の前なので書かない。
func requireUsers(c *client, users []string) error {
	for _, u := range users {
		found, err := userExists(c, u)
		if err != nil {
			return err
		}

		if !found {
			return xerrors.Wrap(errPrecondition, "IAM user "+u+" がありません。organization-baseline の apply の後に行います")
		}
	}

	return nil
}

func hasLoginProfile(c *client, user string) (bool, error) {
	var resp struct {
		LoginProfile struct {
			UserName string `json:"UserName"`
		} `json:"LoginProfile"`
	}

	found, err := c.readOptional(noSuchEntity, &resp, "iam", "get-login-profile", "--user-name", user)
	if err != nil {
		return false, err
	}

	if found && resp.LoginProfile.UserName != user {
		return false, xerrors.Wrap(errAWSResponse, "iam get-login-profile: "+user+" の UserName がありません")
	}

	return found, nil
}

func mfaSerials(c *client, user string) ([]string, error) {
	var resp struct {
		MFADevices *[]struct {
			SerialNumber string `json:"SerialNumber"`
		} `json:"MFADevices"`
	}

	if err := c.read(&resp, "iam", "list-mfa-devices", "--user-name", user); err != nil {
		return nil, err
	}

	if resp.MFADevices == nil {
		return nil, xerrors.Wrap(errAWSResponse, "iam list-mfa-devices: MFADevices がありません")
	}

	var out []string

	for _, d := range *resp.MFADevices {
		if d.SerialNumber == "" {
			return nil, xerrors.Wrap(errAWSResponse, "iam list-mfa-devices: SerialNumber の欠けた装置があります")
		}

		out = append(out, d.SerialNumber)
	}

	return out, nil
}

// isVirtualMFA は、シリアルが仮想 MFA の ARN（arn:<partition>:iam::<account>:mfa/<name>）か。
func isVirtualMFA(serial string) bool {
	return strings.HasPrefix(serial, "arn:") && strings.Contains(serial, ":mfa/")
}

// hasHardwareMFA は、仮想でない MFA の装置が1つ以上在り、仮想の MFA が無いか。
func hasHardwareMFA(serials []string) bool {
	if len(serials) == 0 {
		return false
	}

	for _, s := range serials {
		if isVirtualMFA(s) {
			return false
		}
	}

	return true
}

// setBreakGlassLogin は、3.4 手順1: login profile の無い break-glass の user に、伏せて2回打ち込んだパスワードを置く。
// 初回のサインインでの変更は求めない（保管するパスワードを本人が決める）。
func setBreakGlassLogin(o options, c *client, t *terminal) error {
	bg, err := readBreakGlass(o)
	if err != nil {
		return err
	}

	if err := requireUsers(c, bg.users); err != nil {
		return err
	}

	var missing []string

	for _, u := range bg.users {
		found, err := hasLoginProfile(c, u)
		if err != nil {
			return err
		}

		fmt.Fprintf(t.out, "  %s: パスワード %s\n", u, map[bool]string{true: "在る", false: "無い"}[found])

		if !found {
			missing = append(missing, u)
		}
	}

	if len(missing) == 0 {
		fmt.Fprintln(t.out, "✅ すべての break-glass の user にパスワードが在ります。書き込みません")

		return nil
	}

	fmt.Fprintf(t.out, "パスワードを置く user: %s（password policy に合わないパスワードは AWS が拒みます）\n", strings.Join(missing, "・"))

	if err := t.confirm(o.project); err != nil {
		return err
	}

	for _, u := range missing {
		p, err := t.secret(u + " のパスワード（表示しません）: ")
		if err != nil {
			return err
		}

		again, err := t.secret(u + " のパスワードをもう一度: ")
		if err != nil {
			return err
		}

		if p == "" || p != again {
			return xerrors.Wrap(errPasswordMismatch, u)
		}

		if err := c.writeSecret(map[string]any{"UserName": u, "Password": p, "PasswordResetRequired": false}, "iam", "create-login-profile"); err != nil {
			return err
		}

		if err := reread(o, func() (bool, error) { return hasLoginProfile(c, u) }); err != nil {
			return xerrors.Wrap(err, "置いた後に "+u+" の login profile を読めません。break-glass-check で確かめてください")
		}

		fmt.Fprintf(t.out, "✅ %s にパスワードを置きました\n", u)
	}

	return nil
}

// setBreakGlassMFA は、3.4 手順2: ハードウェアの MFA の無い break-glass の user に、ハードウェアの TOTP の装置を登録する。
// FIDO のセキュリティキーと passkey は API で登録できないので、コンソールで登録する（手順書）。
func setBreakGlassMFA(o options, c *client, t *terminal) error {
	bg, err := readBreakGlass(o)
	if err != nil {
		return err
	}

	if err := requireUsers(c, bg.users); err != nil {
		return err
	}

	var missing []string

	for _, u := range bg.users {
		serials, err := mfaSerials(c, u)
		if err != nil {
			return err
		}

		fmt.Fprintf(t.out, "  %s: MFA %s\n", u, joinOrNone(serials))

		if len(serials) == 0 {
			missing = append(missing, u)
		}
	}

	if len(missing) == 0 {
		fmt.Fprintln(t.out, "✅ すべての break-glass の user に MFA が在ります。書き込みません（仮想 MFA は break-glass-check が NG にします）")

		return nil
	}

	fmt.Fprintf(t.out, "ハードウェアの TOTP の装置を登録する user: %s\n", strings.Join(missing, "・"))

	if err := t.confirm(o.project); err != nil {
		return err
	}

	for _, u := range missing {
		if err := registerTOTP(o, c, t, u); err != nil {
			return err
		}
	}

	return nil
}

func registerTOTP(o options, c *client, t *terminal, user string) error {
	serial, err := t.ask(user + " の装置のシリアル（裏面の英数字）: ")
	if err != nil {
		return err
	}

	if !hardwareSerial.MatchString(serial) || strings.HasPrefix(serial, "arn:") {
		return xerrors.Wrap(errMFAInput, "シリアル")
	}

	code1, err := t.ask("装置に出た6桁のコード: ")
	if err != nil {
		return err
	}

	code2, err := t.ask("次に出た6桁のコード: ")
	if err != nil {
		return err
	}

	if !totpCode.MatchString(code1) || !totpCode.MatchString(code2) || code1 == code2 {
		return xerrors.Wrap(errMFAInput, "コードは続いて出た別の6桁の2つです")
	}

	if err := c.writeSecret(map[string]any{"UserName": user, "SerialNumber": serial, "AuthenticationCode1": code1, "AuthenticationCode2": code2},
		"iam", "enable-mfa-device"); err != nil {
		return err
	}

	if err := reread(o, func() (bool, error) {
		serials, err := mfaSerials(c, user)

		return slices.Contains(serials, serial), err
	}); err != nil {
		return xerrors.Wrap(err, "登録した後に "+user+" の MFA を読めません。break-glass-check で確かめてください")
	}

	fmt.Fprintf(t.out, "✅ %s に MFA を登録しました\n", user)

	return nil
}

// omittedWhenFalse は、偽のとき get-account-password-policy の応答から省かれうる field。
var omittedWhenFalse = []string{"HardExpiry"}

const (
	rereadTries = 5
	rereadWait  = 2 * time.Second
)

// reread は、書いた後の読み直し。IAM は結果整合なので、見えるまで数回やり直す。見えなければ errAWSResponse。
func reread(o options, seen func() (bool, error)) error {
	for i := range rereadTries {
		ok, err := seen()
		if err != nil {
			return err
		}

		if ok {
			return nil
		}

		if i < rereadTries-1 {
			o.sleep(rereadWait)
		}
	}

	return errAWSResponse
}

// checkBreakGlass は、3.4 の確かめ: path の下の IAM user が宣言の user と一致し、それぞれにパスワードと
// ハードウェアの MFA が在って仮想 MFA とアクセスキーが無い。password policy が宣言の値に一致する。
func checkBreakGlass(o options, c *client, w io.Writer) error {
	bg, err := readBreakGlass(o)
	if err != nil {
		return err
	}

	var listed struct {
		Users []struct {
			UserName string `json:"UserName"`
		} `json:"Users"`
	}

	if err := c.read(&listed, "iam", "list-users", "--path-prefix", bg.path); err != nil {
		return err
	}

	var names []string
	for _, u := range listed.Users {
		names = append(names, u.UserName)
	}

	r := newReport(w, "break-glass の資格情報（3.4）")
	r.check(sameSet(names, bg.users), fmt.Sprintf("path %s の IAM user が %s（今: %s）", bg.path, joinOrNone(bg.users), joinOrNone(names)))

	for _, u := range bg.users {
		if err := checkBreakGlassUser(c, r, u); err != nil {
			return err
		}
	}

	if err := checkPasswordPolicy(c, r, bg.policy); err != nil {
		return err
	}

	return r.result()
}

func checkBreakGlassUser(c *client, r *report, u string) error {
	found, err := userExists(c, u)
	if err != nil {
		return err
	}

	if !found {
		r.ng(u + " が在る")

		return nil
	}

	login, err := hasLoginProfile(c, u)
	if err != nil {
		return err
	}

	r.check(login, u+" にパスワードが在る")

	serials, err := mfaSerials(c, u)
	if err != nil {
		return err
	}

	r.check(hasHardwareMFA(serials), fmt.Sprintf("%s にハードウェアの MFA が在り、仮想 MFA が無い（今: %s）", u, joinOrNone(serials)))

	var keys struct {
		AccessKeyMetadata *[]struct {
			AccessKeyID string `json:"AccessKeyId"`
		} `json:"AccessKeyMetadata"`
	}

	if err := c.read(&keys, "iam", "list-access-keys", "--user-name", u); err != nil {
		return err
	}

	if keys.AccessKeyMetadata == nil {
		return xerrors.Wrap(errAWSResponse, "iam list-access-keys: AccessKeyMetadata がありません")
	}

	r.check(len(*keys.AccessKeyMetadata) == 0, fmt.Sprintf("%s にアクセスキーが無い（今: %d 件）", u, len(*keys.AccessKeyMetadata)))

	return nil
}

// checkPasswordPolicy は、アカウントの password policy の field が、宣言の値の字面に一致するか。
func checkPasswordPolicy(c *client, r *report, want map[string]string) error {
	var resp struct {
		PasswordPolicy map[string]json.RawMessage `json:"PasswordPolicy"`
	}

	found, err := c.readOptional(noSuchEntity, &resp, "iam", "get-account-password-policy")
	if err != nil {
		return err
	}

	if !found {
		r.ng("アカウントの password policy が在る")

		return nil
	}

	for _, field := range slices.Sorted(maps.Keys(want)) {
		got := strings.TrimSpace(string(resp.PasswordPolicy[field]))
		if got == "" && want[field] == "false" && slices.Contains(omittedWhenFalse, field) {
			got = "false"
		}
		r.check(got == want[field], fmt.Sprintf("password policy の %s が %s（今: %s）", field, want[field], map[bool]string{true: got, false: "無い"}[got != ""]))
	}

	return nil
}

// checkBreakGlassUsage は、3.6 の忘れの検出: break-glass の user の最後のサインインと、管理アカウントの break-glass の
// role の最後の引き受けが、unusedDays 日より前でない。一度も無いものも NG。
func checkBreakGlassUsage(o options, c *client, w io.Writer) error {
	bg, err := readBreakGlass(o)
	if err != nil {
		return err
	}

	now := o.now()
	r := newReport(w, fmt.Sprintf("break-glass の試験（3.6。%d 日以上使われていないものを NG）", unusedDays))

	for _, u := range bg.users {
		var resp struct {
			User struct {
				UserName         string     `json:"UserName"`
				PasswordLastUsed *time.Time `json:"PasswordLastUsed"`
			} `json:"User"`
		}

		found, err := c.readOptional(noSuchEntity, &resp, "iam", "get-user", "--user-name", u)
		if err != nil {
			return err
		}

		if !found {
			r.ng(u + " が在る")

			continue
		}

		if resp.User.UserName != u {
			return xerrors.Wrap(errAWSResponse, "iam get-user: "+u+" の UserName がありません")
		}

		r.check(recent(now, resp.User.PasswordLastUsed), u+" の最後のサインイン: "+lastUsed(now, resp.User.PasswordLastUsed))
	}

	var role struct {
		Role struct {
			RoleName     string `json:"RoleName"`
			RoleLastUsed struct {
				LastUsedDate *time.Time `json:"LastUsedDate"`
			} `json:"RoleLastUsed"`
		} `json:"Role"`
	}

	found, err := c.readOptional(noSuchEntity, &role, "iam", "get-role", "--role-name", bg.role)
	if err != nil {
		return err
	}

	switch {
	case !found:
		r.ng("role " + bg.role + " が在る")
	case role.Role.RoleName != bg.role:
		return xerrors.Wrap(errAWSResponse, "iam get-role: "+bg.role+" の RoleName がありません")
	default:
		r.check(recent(now, role.Role.RoleLastUsed.LastUsedDate), "role "+bg.role+" の最後の引き受け: "+lastUsed(now, role.Role.RoleLastUsed.LastUsedDate))
	}

	return r.result()
}

func recent(now time.Time, t *time.Time) bool {
	return t != nil && now.Sub(*t) < unusedDays*24*time.Hour
}

func lastUsed(now time.Time, t *time.Time) string {
	if t == nil {
		return "記録が無い"
	}

	return fmt.Sprintf("%s（%d 日前）", t.UTC().Format(time.DateOnly), int(now.Sub(*t).Hours()/24))
}
