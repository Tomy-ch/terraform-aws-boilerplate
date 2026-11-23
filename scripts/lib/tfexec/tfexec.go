// Package tfexec は、Terraform の検査の道具（tf-gates・tf-policy）が terraform を起動するときの、共有の取り決めを
// 持ちます。同じ規則を2つの道具に書くと、片方だけが変わったときに、一方が作った TF_DATA_DIR を他方が見失います。
package tfexec

import (
	"path/filepath"
	"strings"
)

// DataDir は、走査の基点からの相対パス rel のディレクトリの TF_DATA_DIR を、data の下に返します。別のディレクトリが
// 同じ名前にならないよう、_ を先にエスケープし、/ を __ にします。検査の init は root の .terraform を使わない ——
// 手元で実 AWS の backend に init した root は .terraform に backend の設定を残し、-backend=false の init が
// その資格情報を探して落ちる。
func DataDir(data, rel string) string {
	return filepath.Join(data, strings.NewReplacer("_", "_u", "/", "__").Replace(rel))
}

// Env は、environ から AWS_ で始まる変数を外し、SDK が資格情報を探す先を塞いだ環境に extra を足して返します。
// 資格情報を持つ手元でも、持たない CI と同じ条件で走らせるためです（ADR-0401 決定6）。SDK が読む変数は名前を
// 列挙しきれないので、接頭辞で外します。
func Env(environ []string, extra ...string) []string {
	env := make([]string, 0, len(environ)+4+len(extra))
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "AWS_") {
			env = append(env, kv)
		}
	}
	env = append(env, "AWS_SHARED_CREDENTIALS_FILE=/dev/null", "AWS_CONFIG_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true", "CHECKPOINT_DISABLE=1")

	return append(env, extra...)
}
