## 人が手元から Terraform を動かす入口
##
## gates.mk の検査と違い、ホストの ~/.aws の tabp-bootstrap の profile の資格情報を使う（ADR-0504 決定1）。
## 呼び出し元の環境変数の資格情報は使わない。apply と、-target を通せる
## 口は持たない（ADR-0601 決定3-5、ADR-0202 決定7）。root を足す変更が、その root の init / plan を足す。
##
## 値の算出と検証、aws と terraform の起動は scripts/tf-local が段ごとに持ち、ここは段を起動する順だけを持つ
## （ADR-0702 決定3・24・25）。段の間の値は tmp/local/ の state のファイルで渡す。

AWS_BIN ?= aws
TF_PLAN_LOG_DIR ?= tmp/plan
TF_LOCAL_STATE_DIR = tmp/local
TF_LOCAL_PROJECT := tabp
TF_LOCAL_PROFILE := tabp-bootstrap
# state の鍵の alias が無いときに init を失敗させる（fail）か、鍵を渡さずに SSE-S3 で書く（sse-s3）か。
# sse-s3 は段2-4 で人が明示して選び、Bucket の既定が AES256 のときだけ効く（docs/setup/bootstrap.md）。
TF_LOCAL_MISSING_STATE_KEY ?= fail
TF_LOCAL = $(call RUN_SCRIPT_HOST,tf-local,$(1))

# $(1)=env $(2)=identity|workload（ADR-0604 決定5）
define TF_LOCAL_INIT
@$(call TF_LOCAL,local-prepare -project $(TF_LOCAL_PROJECT) -profile $(TF_LOCAL_PROFILE) -env $(1) -root $(2) -base "$(TF_BASE)" -missing $(TF_LOCAL_MISSING_STATE_KEY) -out "$(TF_LOCAL_STATE_DIR)/$(1)-$(2).json")
@$(call TF_LOCAL,local-init -profile $(TF_LOCAL_PROFILE) -state "$(TF_LOCAL_STATE_DIR)/$(1)-$(2).json")
endef

# plan は lock を取らない（ADR-0604 決定8）。log はアカウント ID を含むので、tf-local が所有者だけが読める権限で
# 毎回作り直す。
define TF_LOCAL_PLAN
@mkdir -p "$(TF_PLAN_LOG_DIR)"
@$(call TF_LOCAL,plan-header) > "$(TF_PLAN_LOG_DIR)/$(1)-$(2).header"
@$(call TF_LOCAL,local-plan -profile $(TF_LOCAL_PROFILE) -env $(1) -root $(2) -base "$(TF_BASE)" -header "$(TF_PLAN_LOG_DIR)/$(1)-$(2).header" -log "$(TF_PLAN_LOG_DIR)/$(1)-$(2).log")
endef

.PHONY: aws-login ## bootstrap のプロファイル（tabp-bootstrap）で aws login する
aws-login:
	@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); env $$unset_env $(AWS_BIN) login --profile $(TF_LOCAL_PROFILE)

.PHONY: init-admin-identity ## 管理アカウントの identity root を state Bucket の backend で init する（要 make aws-login）
init-admin-identity:
	$(call TF_LOCAL_INIT,admin,identity)

.PHONY: plan-admin-identity ## 管理アカウントの identity root を lock を取らずに plan し、出力を tmp/plan/ に残す（要 make aws-login）
plan-admin-identity: init-admin-identity
	$(call TF_LOCAL_PLAN,admin,identity)

.PHONY: bootstrap-check-account ## アカウントの準備（1-0 から 2-4）を一時 IAM user の資格情報で読んで確かめる（要 make aws-login）
bootstrap-check-account:
	@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/admin/identity")); env $$unset_env AWS_PROFILE=$(TF_LOCAL_PROFILE) AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,bootstrap-check) account -project $(TF_LOCAL_PROJECT) -region "$$region"

# STAGE・BOOTSTRAP_EXECUTOR・POLICY_SOURCE と、下の RUN・ATTEMPT・MEMBER_PROFILE・RAW・PARTIAL は、環境変数から取らず、
# コマンドラインで渡されたときだけ値を持つ。
STAGE :=
BOOTSTRAP_EXECUTOR :=
POLICY_SOURCE :=

.PHONY: bootstrap-verify ## bootstrap の段の確認を実環境を読んで行う（STAGE=<stage1|before-apply|stage4|permissions|stage5|output-policy>、段4 は BOOTSTRAP_EXECUTOR=、output-policy は POLICY_SOURCE=）
bootstrap-verify:
	@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/admin/identity")); env $$unset_env AWS_PROFILE=$(TF_LOCAL_PROFILE) AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,bootstrap-check) verify -stage "$(STAGE)" -project $(TF_LOCAL_PROJECT) -region "$$region"$(if $(BOOTSTRAP_EXECUTOR), -executor "$(BOOTSTRAP_EXECUTOR)")$(if $(POLICY_SOURCE), -policy-source "$(POLICY_SOURCE)")$(if $(filter reference,$(POLICY_SOURCE)), -reference-dir "$(TF_POLICY_INPUT)/run")

# ci-trail は root の資格情報を拒むので、TF_LOCAL_PROFILE と分けた読み取り専用の主体の profile で引く
# （docs/setup/bootstrap.md の「CloudTrail で拒否を引く」）。RAW=1 の出力は人の端末だけで読む（会話・issue・Pull
# Request へ写すのは既定の出力だけ）。
TRAIL_PROFILE := tabp-trail
RUN :=
ATTEMPT :=
MEMBER_PROFILE :=
RAW :=

.PHONY: ci-trail ## CI の run が AWS で拒まれた・失敗した呼び出しを CloudTrail から引く（RUN=<run ID> ATTEMPT=<n>、メンバーは MEMBER_PROFILE=、読み取り専用の主体の profile tabp-trail で）
ci-trail:
	@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/admin/identity")); env $$unset_env AWS_PROFILE=$(TRAIL_PROFILE) AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,ci-trail) -run "$(RUN)" -attempt "$(ATTEMPT)" -root-region "$$region" -regions-file "$(TF_BASE)/modules/default-regions/outputs.tf"$(if $(MEMBER_PROFILE), -member-profile "$(MEMBER_PROFILE)")$(if $(filter 1,$(RAW)), -raw)

# terraform show は AWS を呼ばないので、環境変数は引き継がず、
# terraform が動くのに要る PATH と HOME だけを渡す。
# bootstrap-check へはホストの terraform の出力を標準入力で渡し、plan のパスはホストの絶対パスなので、
# RUN_SCRIPT_HOST で起動する。
PARTIAL :=
BOOTSTRAP_CHECK_OUT = tmp/bootstrap-check

.PHONY: bootstrap-check-plan ## 保存した plan を段2-4 の期待値と突き合わせる（PLAN=<plan のファイル>、段4 のやり直しは PARTIAL=-partial）
bootstrap-check-plan: SHELL := /bin/bash
bootstrap-check-plan:
	@umask 077; mkdir -p "$(BOOTSTRAP_CHECK_OUT)"
	@umask 077; $(call TF_LOCAL,plan-header) > "$(BOOTSTRAP_CHECK_OUT)/header.txt"
	@set -o pipefail; env -i PATH="$$PATH" HOME="$$HOME" TF_CLI_CONFIG_FILE=/dev/null CHECKPOINT_DISABLE=1 $(TF_BIN) -chdir="$(TF_BASE)/env/admin/identity" show -json "$(abspath $(PLAN))" | $(call RUN_SCRIPT_HOST,bootstrap-check,plan $(PARTIAL) -header-file "$(BOOTSTRAP_CHECK_OUT)/header.txt" -)
