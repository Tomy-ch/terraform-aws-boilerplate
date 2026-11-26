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
# TFLint の AWS ruleset の置き場（取り方は .tflint.hcl）。ホストは mise の install 先、コンテナはイメージが置いた先で、
# 経路ごとに違う。
TFLINT_RULESET_host = $(shell mise where github:terraform-linters/tflint-ruleset-aws 2>/dev/null)/tflint-ruleset-aws
TFLINT_RULESET_container = /usr/local/bin/tflint-ruleset-aws
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
tf-init: tf-lock-check
	@mkdir -p "$$TF_PLUGIN_CACHE_DIR"
	@$(call TF_GATES,init,-data "$(TF_GATES_DATA)")

# provider の版の正本（ADR-0205 決定9）。root の lock はその写しで、写しとずれの判定は tf-gates が持つ。
TF_LOCK_FLAGS = -providers providers -mise mise.toml

.PHONY: tf-lock-check ## root の lock が provider の版の正本（providers/）と同じかを検査
tf-lock-check:
	@$(call TF_GATES,lock-check,$(TF_LOCK_FLAGS))

.PHONY: tf-lock-apply ## provider の版の正本（providers/）を root の lock へ写す
tf-lock-apply:
	@$(call TF_GATES,lock-apply,$(TF_LOCK_FLAGS))

.PHONY: tf-providers-lock ## providers/providers.toml から正本の lock を作り直し、root の lock へ写す（Terraform Registry から取得する）
tf-providers-lock:
	@mkdir -p "$$TF_PLUGIN_CACHE_DIR"
	@$(call RUN_SCRIPT_HOST,tf-gates,lock-generate $(TF_LOCK_FLAGS) -work tmp/tf-providers-lock)
	@$(call TF_GATES,lock-apply,$(TF_LOCK_FLAGS))

.PHONY: tf-validate ## Terraform の構成を検査し、エラーと抑止されない警告で落とす（provider の取得を伴う）
tf-validate: tf-init
	@$(call TF_GATES,validate,-data "$(TF_GATES_DATA)" -out "$(TF_VALIDATE_OUT)" $(TF_VALIDATE_CANARY_FLAGS))
	@$(call RUN_SCRIPT,tf-gates,result -base $(TF_BASE) -manifest $(TF_VALIDATE_OUT)/manifest.tsv -ignore $(TF_VALIDATE_IGNORE))

.PHONY: tf-test ## terraform test を実行（*.tftest.hcl を持つディレクトリ）
tf-test: tf-init
	@$(call TF_GATES,test,-data "$(TF_GATES_DATA)")

.PHONY: tf-lint ## TFLint で Terraform を検査
tf-lint: tool-cooldown-check
	@$(call RUN_SCRIPT,tf-gates,lint -base "$(TF_BASE)" -config .tflint.hcl -ruleset "$(TFLINT_RULESET_$(RUNNER_MODE))" -mise mise.toml $(TF_ROOTS))

# .lefthook.yaml の skip 条件。対象が0件のときだけ成功する。
.PHONY: tf-no-targets
tf-no-targets:
	@$(call TF_GATES,none) >/dev/null 2>&1
