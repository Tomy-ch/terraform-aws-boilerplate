package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// stateKeyAliasFormat は、usecases/state-backend の state_key_alias_name（`alias/<project>-<env>-tfstate`）の写し。
const stateKeyAliasFormat = "alias/%s-%s-tfstate"

const (
	missingFail  = "fail"
	missingSSES3 = "sse-s3"
)

var (
	// keyIDPattern は、usecases/deployment-identity の state_key_arn の validation と同じ鍵 ID の形。
	// 多リージョンの鍵（mrk-）は modules/kms が作らないので受けない。
	keyIDPattern    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	aliasARNPattern = regexp.MustCompile(`^arn:(aws[a-z-]*):kms:([a-z0-9-]+):([0-9]{12}):(alias/.+)$`)

	errStateKeyMissing    = xerrors.New("❌ state の鍵の alias がありません（段4 の apply の前に鍵を渡さない init を選ぶ手順は docs/setup/bootstrap.md）")
	errStateKeyAliases    = xerrors.New("❌ alias の一覧を読めません")
	errStateKeyEncryption = xerrors.New("❌ state Bucket の既定の暗号化を読めません")
	errStateKeyMismatch   = xerrors.New("❌ state の鍵の alias と、state Bucket の既定の暗号化の鍵が違います")
	errStateKeyMissingArg = xerrors.New("❌ -missing は fail か sse-s3 です")
)

// Aliases をポインタで受けるのは、キーの無い応答や null を、alias が0件の一覧と区別するためである。
type kmsAliases struct {
	Aliases *[]struct {
		AliasName   string `json:"AliasName"`
		AliasArn    string `json:"AliasArn"`
		TargetKeyID string `json:"TargetKeyId"`
	} `json:"Aliases"`
	Truncated bool `json:"Truncated"`
}

type bucketEncryption struct {
	ServerSideEncryptionConfiguration struct {
		Rules []struct {
			ApplyServerSideEncryptionByDefault struct {
				SSEAlgorithm   string `json:"SSEAlgorithm"`
				KMSMasterKeyID string `json:"KMSMasterKeyID"`
			} `json:"ApplyServerSideEncryptionByDefault"`
		} `json:"Rules"`
	} `json:"ServerSideEncryptionConfiguration"`
}

func runStateKey(args []string) (string, error) {
	fs := flag.NewFlagSet("state-key", flag.ContinueOnError)
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	region := fs.String("region", "", "")
	identity := fs.String("identity", "", "")
	aliases := fs.String("aliases", "", "")
	encryption := fs.String("encryption", "", "")
	missing := fs.String("missing", missingFail, "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", xerrors.Wrap(errUsage, strings.Join(fs.Args(), " "))
	}

	account, err := accountOf(*identity)
	if err != nil {
		return "", err
	}

	value, err := stateKey(*project, *env, *region, account, []byte(*aliases), []byte(*encryption), *missing)
	if err == nil && value == "" {
		log.Print("⚠️ state の鍵が無いので、init に鍵を渡さず SSE-S3 で書きます（-missing sse-s3）")
	}

	return value, err
}

// stateKey は、init に渡す `-backend-config=kms_key_id=<鍵の ARN>` を返します。鍵の ARN は
// `aws kms list-aliases` の state の鍵の alias から組み、`aws s3api get-bucket-encryption` の既定の鍵が
// SSE-KMS ならそれと一致しなければ失敗します（alias だけの付け替えの検出）。alias が無いとき、
// missing が sse-s3 で既定が AES256 の場合に限って空文字を返し、init は鍵を渡さない。
// 失敗の文に入力の値（アカウント ID、鍵 ID）を入れない —— CI の公開されるログに出る。
func stateKey(project, env, region, account string, aliasesJSON, encryptionJSON []byte, missing string) (string, error) {
	if missing != missingFail && missing != missingSSES3 {
		return "", errStateKeyMissingArg
	}

	if !tokenPattern.MatchString(project) || !tokenPattern.MatchString(env) {
		return "", errToken
	}

	if !regionPattern.MatchString(region) {
		return "", errRegionMalformed
	}

	algorithm, defaultKey, err := defaultEncryption(encryptionJSON)
	if err != nil {
		return "", err
	}

	keyARN, found, err := aliasKeyARN(fmt.Sprintf(stateKeyAliasFormat, project, env), region, account, aliasesJSON)
	if err != nil {
		return "", err
	}

	switch {
	case !found && algorithm == "AES256" && missing == missingSSES3:
		return "", nil
	case !found:
		return "", errStateKeyMissing
	case algorithm == "aws:kms" && defaultKey != keyARN:
		return "", errStateKeyMismatch
	}

	return "-backend-config=kms_key_id=" + keyARN, nil
}

// defaultEncryption は、既定の暗号化の規則がちょうど1つで、AES256 か aws:kms であるときに、その方式と鍵を返します。
func defaultEncryption(data []byte) (string, string, error) {
	var enc bucketEncryption
	if err := json.Unmarshal(data, &enc); err != nil {
		return "", "", xerrors.Wrap(errStateKeyEncryption, "JSON ではありません")
	}

	rules := enc.ServerSideEncryptionConfiguration.Rules
	if len(rules) != 1 {
		return "", "", xerrors.Wrap(errStateKeyEncryption, "規則が1つではありません")
	}

	byDefault := rules[0].ApplyServerSideEncryptionByDefault
	switch byDefault.SSEAlgorithm {
	case "AES256":
		return byDefault.SSEAlgorithm, "", nil
	case "aws:kms":
		return byDefault.SSEAlgorithm, byDefault.KMSMasterKeyID, nil
	}

	return "", "", xerrors.Wrap(errStateKeyEncryption, "方式が AES256 でも aws:kms でもありません")
}

// aliasKeyARN は、一覧の中の name の alias が指す鍵の ARN を返します。alias が無ければ found は偽。
// partition は alias の ARN から取り、region とアカウントが alias の ARN と食い違えば失敗します。
func aliasKeyARN(name, region, account string, data []byte) (string, bool, error) {
	var list kmsAliases
	if err := json.Unmarshal(data, &list); err != nil {
		return "", false, xerrors.Wrap(errStateKeyAliases, "JSON ではありません")
	}

	if list.Aliases == nil {
		return "", false, xerrors.Wrap(errStateKeyAliases, "Aliases がありません")
	}

	if list.Truncated {
		return "", false, xerrors.Wrap(errStateKeyAliases, "一覧が途中で切れています")
	}

	for _, a := range *list.Aliases {
		if a.AliasName != name {
			continue
		}

		m := aliasARNPattern.FindStringSubmatch(a.AliasArn)
		if m == nil || m[2] != region || m[3] != account || m[4] != name {
			return "", false, xerrors.Wrap(errStateKeyAliases, "alias の ARN が、region・アカウント・名前と合いません")
		}

		if !keyIDPattern.MatchString(a.TargetKeyID) {
			return "", false, xerrors.Wrap(errStateKeyAliases, "alias が指す鍵 ID の形が不正です")
		}

		return fmt.Sprintf("arn:%s:kms:%s:%s:key/%s", m[1], region, account, a.TargetKeyID), true, nil
	}

	return "", false, nil
}
