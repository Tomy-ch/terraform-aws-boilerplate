## Terraform 側の検査（ADR-0501 決定4・28）
##
## 実行経路は .makefiles/README.md の表が持つ（ADR-0503 決定4・5）。
## 対象のディレクトリの選び方、root の判定、ディレクトリごとの起動と失敗の集約、validate の判定は scripts/tf-gates が
## 持つ。ここが持つのは段を起動する順だけである（ADR-0702 決定24・25）。terraform を起動する段はホストで走る
## （terraform は提供段のコンテナに無い）。tf-lint は TFLint のあるツールランナーで走る。
##
## 走査の対象が0件なら失敗する（tf-no-targets を除く）。
##
## tf-validate と tf-test は tf-init を共有するので、1回の make で一緒に呼ぶ。別々の make を
## 並行させると、同じディレクトリの init と provider のキャッシュが競合する。

TF_BASE ?= .
TF_ROOTS = usecases modules env
export TF_PLUGIN_CACHE_DIR ?= $(CURDIR)/tmp/terraform-plugin-cache
# plugin の置き場の階層は OS / arch を含まないので、経路ごとに分ける。
TFLINT_PLUGIN_DIR_REL = tmp/tflint-plugins/$(RUNNER_MODE)
# 資格情報を持つ手元でも、持たない CI と同じ条件で走らせる（ADR-0401 決定6）。SDK が資格情報を
# 読む環境変数は名前を列挙しきれないので、AWS_ で始まるものをすべて外してから与える。Go の道具は lib/tfexec で同じ環境を自分で組む。
AWS_ENV_UNSET = $$(env | sed -n 's/^\(AWS_[A-Za-z0-9_]*\)=.*/-u \1/p')
TF_ENV = env $(AWS_ENV_UNSET) \
	AWS_SHARED_CREDENTIALS_FILE=/dev/null AWS_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1
# terraform は相対の TF_DATA_DIR を作業ディレクトリから解決するので、絶対パスで渡す。
TF_GATES_DATA = $(abspath $(TF_BASE))/tmp/tf-gates-data
# validate の結果は tf-gates result が読む。container の経路でも読めるよう、直下からの相対パスで置く。
TF_VALIDATE_OUT = $(TF_BASE)/tmp/tf-validate
TF_VALIDATE_IGNORE ?= .tf-validate-ignore.yaml
TF_BIN ?= terraform
TF = $(TF_ENV) $(TF_BIN)
# 無い root を飛ばすのは、root の一部だけを持つ fixture で検査を走らせる canary だけである（-allow-missing-roots）。
TF_GATES_ROOTS_FLAG ?=
# validate の canary は実物の AWS Provider を使い、版は state-backend の reference の lock に揃える（plugin cache に
# 無ければ取得する）。tf-gates validate が本走査の前に同じ起動の中で走らせる（ADR-0702 決定25）。
TF_VALIDATE_CANARY_DIR = tmp/tf-validate-canary
TF_VALIDATE_CANARY_FLAGS = -canary-data "$(abspath $(TF_VALIDATE_CANARY_DIR))/data" -canary-work "$(TF_VALIDATE_CANARY_DIR)/work" -canary-reference usecases/state-backend/reference
TF_GATES = $(call RUN_SCRIPT_HOST,tf-gates,$(1) -base "$(TF_BASE)" $(TF_GATES_ROOTS_FLAG) $(2) $(TF_ROOTS))

.PHONY: tf-fmt ## Terraform の書式を整える
tf-fmt:
	@mkdir -p "$$TF_PLUGIN_CACHE_DIR"
	@$(call TF_GATES,fmt)

.PHONY: tf-fmt-check ## Terraform の書式を検査
tf-fmt-check:
	@mkdir -p "$$TF_PLUGIN_CACHE_DIR"
	@$(call TF_GATES,fmt,-check)

.PHONY: tf-init
tf-init:
	@mkdir -p "$$TF_PLUGIN_CACHE_DIR"
	@$(call TF_GATES,init,-data "$(TF_GATES_DATA)")

.PHONY: tf-validate ## Terraform の構成を検査し、エラーと抑止されない警告で落とす（provider の取得を伴う）
tf-validate: tf-init
	@$(call TF_GATES,validate,-data "$(TF_GATES_DATA)" -out "$(TF_VALIDATE_OUT)" $(TF_VALIDATE_CANARY_FLAGS))
	@$(call RUN_SCRIPT,tf-gates,result -base $(TF_BASE) -manifest $(TF_VALIDATE_OUT)/manifest.tsv -ignore $(TF_VALIDATE_IGNORE))

.PHONY: tf-test ## terraform test を実行（*.tftest.hcl を持つディレクトリ）
tf-test: tf-init
	@$(call TF_GATES,test,-data "$(TF_GATES_DATA)")

.PHONY: tf-lint ## TFLint で Terraform を検査（初回は ruleset の取得を伴う）
tf-lint: tool-cooldown-check
	@$(call RUN_SCRIPT,tf-gates,lint -base "$(TF_BASE)" -config .tflint.hcl -plugin-dir $(TFLINT_PLUGIN_DIR_REL) $(TF_ROOTS))

# .lefthook.yaml の skip 条件。対象が0件のときだけ成功する。
.PHONY: tf-no-targets
tf-no-targets:
	@$(call TF_GATES,none) >/dev/null 2>&1
