## 人が手元から Terraform を動かす入口
##
## gates.mk の検査と違い、ホストの ~/.aws の tabp-bootstrap の profile の資格情報を使う（ADR-0504 決定1）。
## 呼び出し元の環境変数の資格情報は使わない。apply と、-target を通せる
## 口は持たない（ADR-0601 決定3-5、ADR-0202 決定7）。root を足す変更が、その root の init / plan を足す。
##
## 値の算出と検証はすべて scripts/tf-local が持ち、ここは分岐を持たない（ADR-0702 決定3）。
## tf-local は sh -c の中で動くので、値は export した変数で渡す。export は代入と分けて書く ——
## `export x=$(...)` は代入の失敗を export の成功で隠す。

AWS_BIN ?= aws
TF_PLAN_LOG_DIR ?= tmp/plan
TF_LOCAL_PROJECT := tabp
TF_LOCAL_PROFILE := tabp-bootstrap
# state の鍵の alias が無いときに init を失敗させる（fail）か、鍵を渡さずに SSE-S3 で書く（sse-s3）か。
# sse-s3 は段2-4 で人が明示して選び、Bucket の既定が AES256 のときだけ効く（docs/setup/bootstrap.md）。
TF_LOCAL_MISSING_STATE_KEY ?= fail
TF_LOCAL = $(call RUN_SCRIPT_HOST,tf-local,$(1))
# $(1)=env $(2)=identity|workload（ADR-0604 決定5）
TF_LOCAL_DIR = $(TF_BASE)/env/$(1)$(if $(filter identity,$(2)),/identity)
# 直前に unset_env=$(tf-local env-unset) を代入してから使う。
# TF_CLI_CONFIG_FILE=/dev/null は dev_overrides による provider の差し替えを封じる（docs/setup/bootstrap.md 2-0b）。
TF_LOCAL_ENV = env $$unset_env AWS_PROFILE=$(TF_LOCAL_PROFILE) TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1

# init に渡す state の鍵の引数を kms に置く。鍵が無ければ空になり、$$kms は引用せずに展開して消える。
# 直前に unset_env・region・identity・account・bucket を代入してから使う。$(1)=env $(2)=fail|sse-s3
define TF_STATE_KEY
aliases=$$($(TF_LOCAL_ENV) $(AWS_BIN) kms list-aliases --region "$$region" --output json); export aliases; \
encryption=$$($(TF_LOCAL_ENV) $(AWS_BIN) s3api get-bucket-encryption --region "$$region" --bucket "$$bucket" --expected-bucket-owner "$$account" --output json); export encryption; \
kms=$$($(call TF_LOCAL,state-key -project $(TF_LOCAL_PROJECT) -env $(1) -region "$$region" -identity "$$identity" -aliases "$$aliases" -encryption "$$encryption" -missing $(2)))
endef

# $(1)=env $(2)=root
define TF_LOCAL_INIT
set -eu; dir="$(call TF_LOCAL_DIR,$(1),$(2))"; export dir; \
unset_env=$$($(call TF_LOCAL,env-unset)); \
region=$$($(call TF_LOCAL,region "$$dir")); export region; \
identity=$$($(TF_LOCAL_ENV) $(AWS_BIN) sts get-caller-identity --region "$$region" --query '[Account,Arn]' --output text); export identity; \
account=$$($(call TF_LOCAL,account -identity "$$identity")); \
bucket=$$($(call TF_LOCAL,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -region "$$region" -identity "$$identity")); \
$(call TF_STATE_KEY,$(1),$(TF_LOCAL_MISSING_STATE_KEY)); \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" init -input=false -lockfile=readonly -backend-config="bucket=$$bucket" $$kms
endef

# plan は lock を取らない（ADR-0604 決定8）。log はアカウント ID を含むので、所有者だけが読める権限で
# 毎回作り直す。
define TF_LOCAL_PLAN
set -eu -o pipefail; umask 077; log="$(TF_PLAN_LOG_DIR)/$(1)-$(2).log"; mkdir -p "$(TF_PLAN_LOG_DIR)"; rm -f "$$log"; \
unset_env=$$($(call TF_LOCAL,env-unset)); \
header=$$($(call TF_LOCAL,plan-header)); \
{ echo "$$header"; $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$(call TF_LOCAL_DIR,$(1),$(2))" plan -lock=false -input=false -no-color 2>&1; } | tee "$$log"
endef

.PHONY: aws-login ## bootstrap のプロファイル（tabp-bootstrap）で aws login する
aws-login:
	@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); env $$unset_env $(AWS_BIN) login --profile $(TF_LOCAL_PROFILE)

.PHONY: init-admin-identity ## 管理アカウントの identity root を state Bucket の backend で init する（要 make aws-login）
init-admin-identity:
	@$(call TF_LOCAL_INIT,admin,identity)

.PHONY: plan-admin-identity ## 管理アカウントの identity root を lock を取らずに plan し、出力を tmp/plan/ に残す（要 make aws-login）
plan-admin-identity: SHELL := /bin/bash
plan-admin-identity: init-admin-identity
	@$(call TF_LOCAL_PLAN,admin,identity)

.PHONY: bootstrap-check-account ## アカウントの準備（1-0 から 2-4）を一時 IAM user の資格情報で読んで確かめる（要 make aws-login）
bootstrap-check-account:
	@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/admin/identity")); env $$unset_env AWS_PROFILE=$(TF_LOCAL_PROFILE) AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,bootstrap-check) account -project $(TF_LOCAL_PROJECT) -region "$$region"

# 環境変数から取らず、コマンドラインで渡されたときだけ値を持つ。
STAGE :=
BOOTSTRAP_EXECUTOR :=
POLICY_SOURCE :=

.PHONY: bootstrap-verify ## bootstrap の段の確認を実環境を読んで行う（STAGE=<stage1|before-apply|stage4|permissions|stage5|output-policy>、段4 は BOOTSTRAP_EXECUTOR=、output-policy は POLICY_SOURCE=）
bootstrap-verify:
	@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/admin/identity")); env $$unset_env AWS_PROFILE=$(TF_LOCAL_PROFILE) AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,bootstrap-check) verify -stage "$(STAGE)" -project $(TF_LOCAL_PROJECT) -region "$$region"$(if $(BOOTSTRAP_EXECUTOR), -executor "$(BOOTSTRAP_EXECUTOR)")$(if $(POLICY_SOURCE), -policy-source "$(POLICY_SOURCE)")

# terraform show は AWS を呼ばないので、環境変数は引き継がず、
# terraform が動くのに要る PATH と HOME だけを渡す。
# bootstrap-check へはホストの terraform の出力を標準入力で渡し、plan のパスはホストの絶対パスなので、
# RUN_SCRIPT_HOST で起動する。
# PARTIAL は環境変数から取らず、make の引数でだけ渡せる。
PARTIAL :=
BOOTSTRAP_CHECK_OUT = tmp/bootstrap-check

.PHONY: bootstrap-check-plan ## 保存した plan を段2-4 の期待値と突き合わせる（PLAN=<plan のファイル>、段4 のやり直しは PARTIAL=-partial）
bootstrap-check-plan: SHELL := /bin/bash
bootstrap-check-plan:
	@umask 077; mkdir -p "$(BOOTSTRAP_CHECK_OUT)"
	@umask 077; $(call TF_LOCAL,plan-header) > "$(BOOTSTRAP_CHECK_OUT)/header.txt"
	@set -o pipefail; env -i PATH="$$PATH" HOME="$$HOME" TF_CLI_CONFIG_FILE=/dev/null CHECKPOINT_DISABLE=1 $(TF_BIN) -chdir="$(TF_BASE)/env/admin/identity" show -json "$(abspath $(PLAN))" | $(call RUN_SCRIPT_HOST,bootstrap-check,plan $(PARTIAL) -header-file "$(BOOTSTRAP_CHECK_OUT)/header.txt" -)
