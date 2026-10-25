## 人が手元から Terraform を動かす入口
##
## gates.mk の検査と違い、ホストの AWS の資格情報を使う（ADR-0504 決定1）。呼び出し元の AWS_ の
## 環境変数はすべて外し、ターゲットごとに決めた profile だけを渡す。apply と、-target を通せる口は
## 持たない（ADR-0601 決定3-5、ADR-0202 決定7）。root を足す変更が、その root の init / plan を足す。

AWS_BIN ?= aws
TF_PLAN_LOG_DIR ?= tmp/plan
TF_LOCAL_PROJECT := tabp
TF_LOCAL_PROFILE := tabp-bootstrap
# $(1)=env $(2)=identity|workload（ADR-0604 決定5）
TF_LOCAL_DIR = $(TF_BASE)/env/$(1)$(if $(filter identity,$(2)),/identity)
# 引数・入力・対象の state・provider を変える TF_ の環境変数と、資格情報を含みうる trace を出す TF_LOG も外す
# （-target を環境変数から通させない、plan の log に trace を残さない）。
TF_LOCAL_ENV = env $(AWS_ENV_UNSET) \
	$$(env | sed -n -e 's/^\(TF_CLI_ARGS[A-Za-z0-9_]*\)=.*/-u \1/p' -e 's/^\(TF_VAR_[A-Za-z0-9_]*\)=.*/-u \1/p' -e 's/^\(TF_LOG[A-Za-z0-9_]*\)=.*/-u \1/p') \
	-u TF_WORKSPACE -u TF_DATA_DIR -u TF_CLI_CONFIG_FILE -u TF_REATTACH_PROVIDERS \
	AWS_PROFILE=$(1) AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1

# $(1)=env $(2)=root $(3)=profile
#
# 値の算出は scripts/tf-local（ADR-0702 決定3）。tf-local は sh -c の中で動くので、値は export した変数で渡す。
define TF_LOCAL_INIT
set -euo pipefail; export dir="$(call TF_LOCAL_DIR,$(1),$(2))"; \
export region; region=$$($(call RUN_SCRIPT_HOST,tf-local,region "$$dir")); \
bucket=$$($(call TF_LOCAL_ENV,$(3)) $(AWS_BIN) sts get-caller-identity --region "$$region" --query '[Account,Arn]' --output text \
	| $(call RUN_SCRIPT_HOST,tf-local,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -region "$$region")); \
$(call TF_LOCAL_ENV,$(3)) $(TF_BIN) -chdir="$$dir" init -input=false -lockfile=readonly -backend-config="bucket=$$bucket"
endef

# plan は lock を取らない（ADR-0604 決定8）。どの commit の plan かを log の1行目に残す（ADR-0602 決定10）。
# log はアカウント ID を含むので、所有者だけが読める権限で作る。
define TF_LOCAL_PLAN
set -euo pipefail; umask 077; mkdir -p "$(TF_PLAN_LOG_DIR)"; rm -f "$(TF_PLAN_LOG_DIR)/$(1)-$(2).log"; \
commit=$$(git rev-parse HEAD); status=$$(git status --porcelain); \
dirty=""; if [ -n "$$status" ]; then dirty=" （作業ツリーに未コミットの変更あり）"; fi; \
{ echo "commit: $$commit$$dirty"; \
	$(call TF_LOCAL_ENV,$(3)) $(TF_BIN) -chdir="$(call TF_LOCAL_DIR,$(1),$(2))" plan -lock=false -input=false -no-color 2>&1; \
} | tee "$(TF_PLAN_LOG_DIR)/$(1)-$(2).log"
endef

.PHONY: aws-login ## bootstrap のプロファイル（tabp-bootstrap）で aws login する
aws-login:
	@env $(AWS_ENV_UNSET) $(AWS_BIN) login --profile $(TF_LOCAL_PROFILE)

.PHONY: init-admin-identity ## 管理アカウントの identity root を state Bucket の backend で init する（要 make aws-login）
init-admin-identity: SHELL := /bin/bash
init-admin-identity:
	@$(call TF_LOCAL_INIT,admin,identity,$(TF_LOCAL_PROFILE))

.PHONY: plan-admin-identity ## 管理アカウントの identity root を lock を取らずに plan し、出力を tmp/plan/ に残す（要 make aws-login）
plan-admin-identity: SHELL := /bin/bash
plan-admin-identity: init-admin-identity
	@$(call TF_LOCAL_PLAN,admin,identity,$(TF_LOCAL_PROFILE))
