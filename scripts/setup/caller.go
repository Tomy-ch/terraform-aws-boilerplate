package main

import (
	"fmt"
	"io"
	"regexp"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// assumedRoleARN は、引き受けた role の session の ARN。role の名前（path を持たない）を取り出す。
var assumedRoleARN = regexp.MustCompile(`^arn:[a-z-]+:sts::[0-9]{12}:assumed-role/([^/]+)/[^/]+$`)

// requireAccountCaller は、資格情報が IAM user か引き受けた role のものであることを求め、書き込む先として主体の名前と
// アカウント ID の末尾4桁を出す。各アカウントで行う手順なので、組織の管理アカウントであることは求めない。
// root と federated user の資格情報では書かない。
func requireAccountCaller(c *client, profile string, w io.Writer) error {
	var caller struct {
		Account string `json:"Account"`
		Arn     string `json:"Arn"`
	}

	if err := c.read(&caller, "sts", "get-caller-identity"); err != nil {
		return err
	}

	if caller.Arn == "" || !accountID.MatchString(caller.Account) {
		return xerrors.Wrap(errAWSResponse, "資格情報の ARN かアカウント ID を読めません")
	}

	var who string

	switch {
	case iamUserARN.MatchString(caller.Arn):
		who = "IAM user " + iamUserARN.FindStringSubmatch(caller.Arn)[1]
	case assumedRoleARN.MatchString(caller.Arn):
		who = "role " + assumedRoleARN.FindStringSubmatch(caller.Arn)[1]
	default:
		return xerrors.Wrap(errPrecondition, "profile "+profile+" の資格情報は IAM user か role のものではありません")
	}

	// 人が profile の取り違えに気づけるよう、主体の名前とアカウント ID の末尾を出す（12桁は出力で伏せる）。
	fmt.Fprintf(w, "# 書き込む先: profile %s、region %s、%s、アカウント ID の末尾 %s\n", profile, c.region, who, caller.Account[len(caller.Account)-4:])

	return nil
}
