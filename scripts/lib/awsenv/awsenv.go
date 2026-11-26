// Package awsenv は、aws を起動する運用機構が、profile を迂回する資格情報の指定を環境に見つけるための一覧を持ちます。
//
// 資格情報を変える環境変数は make が `tf-local env-unset` で外します（ADR-0702 決定26）。ここは、外されないまま
// 起動されたときに aws を起動せずに止めるための、運用機構の側の二重の確かめです。
package awsenv

// StaticCredentials は、AWS_PROFILE より優先されるか、profile の読み先を変える環境変数です。
var StaticCredentials = []string{
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE",
	"AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_SHARED_CREDENTIALS_FILE", "AWS_CONFIG_FILE",
}

// Found は、StaticCredentials のうち getenv が空でない値を返すものを、一覧の順で返します。
func Found(getenv func(string) string) []string {
	var found []string

	for _, k := range StaticCredentials {
		if getenv(k) != "" {
			found = append(found, k)
		}
	}

	return found
}
