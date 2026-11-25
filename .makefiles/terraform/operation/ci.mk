## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml の plan と、terraform-apply.yaml の apply）と、
## state に残った lock を外す入口（terraform-unlock.yaml）と、lock を外さずに残す入口（terraform-lock-hold.yaml）
##
## ここは scripts/tf-local の段を起動する順序だけを持つ。段は1回の起動で1つの目的の道具だけを固定の引数で起動し、
## 値の算出と判定を持ち、段の間の値は CI_STATE のファイルで運ぶ（ADR-0702 決定3・24）。資格情報は ci-credentials が
## OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の profile に置く。後の段は AWS_* を外した
## 環境で、その profile だけを使う。plan と apply には、入口の CI_POLICY_NAMESPACES の Policy を掛ける。
## 公開されるログに terraform の出力を出さない。全文は plan の出力の Bucket か apply の出力の Bucket に置き、ログには
## 終了コード・件数・object の key・SHA-256・版 ID と Policy の違反の文だけを出す（ADR-0602 決定13）。
##
## 入力は環境変数で受ける。CI_ACCOUNT_ID（environment の secret）と CI_PR（Pull Request の番号）は workflow が渡し、
## GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT・RUNNER_TEMP・GITHUB_STEP_SUMMARY・GITHUB_OUTPUT と OIDC の要求先は
## GitHub Actions が渡す。

CI_PLAN_DIR ?= $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/ci-plan
CI_ROLE ?= plan
# $(1)=env $(2)=役割。role 名と同じ形にする（tf-local ci-credentials が置く profile の名前）。
CI_PROFILE = $(TF_LOCAL_PROJECT)-$(1)-ci-$(2)

# 段の state のファイル。root の名前（$(1)-$(2)）は、decl.json の allow の鍵と同じになる。$(1)=env $(2)=root
CI_STATE = $(CI_PLAN_DIR)/$(1)-$(2).json

# plan と apply の入口の段（tf-local の ci-*）。prepare と init が落ちればそこで止まる。それより後の段は `-` で続け、
# 前の段の失敗を state で知って何もしない。upload は必ず走り、finish が state の判定で落とす。
# $(1)=plan|apply $(2)=env $(3)=root
define CI_STEPS_PLAN
@mkdir -p "$(CI_PLAN_DIR)"
-@$(call TF_LOCAL,plan-header) > "$(CI_PLAN_DIR)/$(2)-$(3).header"
@$(call TF_LOCAL,ci-prepare -mode $(1) -project $(TF_LOCAL_PROJECT) -env $(2) -root $(3) -profile $(TF_LOCAL_PROFILE) -base $(TF_BASE) -header "$(CI_PLAN_DIR)/$(2)-$(3).header" -state "$(call CI_STATE,$(2),$(3))")
@$(call TF_LOCAL,ci-init -state "$(call CI_STATE,$(2),$(3))")
-@$(call TF_LOCAL,ci-plan -state "$(call CI_STATE,$(2),$(3))")
-@$(call TF_LOCAL,ci-policy -state "$(call CI_STATE,$(2),$(3))" -work tmp/ci-policy -policy $(POLICY_PLAN_DIR) -decl $(POLICY_PLAN_DECL) -layer $(POLICY_LAYER_DECL) $(foreach ns,$(CI_POLICY_NAMESPACES),-namespace $(ns)))
endef

define CI_STEPS_FINISH
-@$(call TF_LOCAL,ci-upload -state "$(call CI_STATE,$(1),$(2))")
@$(call TF_LOCAL,ci-finish -state "$(call CI_STATE,$(1),$(2))")
endef

# $(1)=env $(2)=root。plan は lock を取らず、全文を plan の出力の Bucket に置く。
define CI_PLAN
$(call CI_STEPS_PLAN,plan,$(1),$(2))
$(call CI_STEPS_FINISH,$(1),$(2))
endef

# $(1)=env $(2)=root。apply する commit で plan し直し、Policy を通った plan だけを apply する（ADR-0601 決定6、
# ADR-0604 決定11）。変更の無い plan も apply し、経路と lock の取得を毎回同じにする。
define CI_APPLY
$(call CI_STEPS_PLAN,apply,$(1),$(2))
-@$(call TF_LOCAL,ci-apply -state "$(call CI_STATE,$(1),$(2))")
$(call CI_STEPS_FINISH,$(1),$(2))
endef

# $(1)=env $(2)=root。apply の job の timeout を超えて残った lock だけを、読んだ版の ETag を条件に消す。
# terraform は呼ばない。
define CI_UNLOCK
@$(call TF_LOCAL,ci-unlock -project $(TF_LOCAL_PROJECT) -env $(1) -root $(2) -profile $(TF_LOCAL_PROFILE) -base $(TF_BASE))
endef

# $(1)=env $(2)=root。lock の解除を障害の前に試すために、apply の role で root の lock を取り、外さずに残す
# （ADR-0604 決定8）。どの段が落ちても、そこで止まる。
define CI_HOLD
@mkdir -p "$(CI_PLAN_DIR)"
-@$(call TF_LOCAL,plan-header) > "$(CI_PLAN_DIR)/$(1)-$(2).header"
@$(call TF_LOCAL,ci-prepare -mode hold -project $(TF_LOCAL_PROJECT) -env $(1) -root $(2) -profile $(TF_LOCAL_PROFILE) -base $(TF_BASE) -header "$(CI_PLAN_DIR)/$(1)-$(2).header" -state "$(call CI_STATE,$(1),$(2))")
@$(call TF_LOCAL,ci-init -state "$(call CI_STATE,$(1),$(2))")
@$(call TF_LOCAL,ci-hold-absent -state "$(call CI_STATE,$(1),$(2))")
@$(call TF_LOCAL,ci-hold-plan -state "$(call CI_STATE,$(1),$(2))")
@$(call TF_LOCAL,ci-hold-verify -state "$(call CI_STATE,$(1),$(2))")
endef

.PHONY: ci-aws-credentials ## [CI] OIDC で CI_ENV の CI_ROLE の role を assume し、profile に資格情報を置く
ci-aws-credentials:
	@$(if $(CI_ENV),,$(error CI_ENV が空です))$(call TF_LOCAL,ci-credentials -project $(TF_LOCAL_PROJECT) -env $(CI_ENV) -role $(CI_ROLE) -base $(TF_BASE) -work $(CI_PLAN_DIR))

.PHONY: ci-assume-denied ## [CI] CI_ENV の plan の job の OIDC token で、pull_request で起動しない種別の role の assume がすべて拒否されることを確かめる
ci-assume-denied:
	@$(if $(CI_ENV),,$(error CI_ENV が空です))$(call TF_LOCAL,ci-assume-denied -project $(TF_LOCAL_PROJECT) -env $(CI_ENV) -base $(TF_BASE) -work $(CI_PLAN_DIR))

.PHONY: ci-plan-admin-identity ## [CI] 管理アカウントの identity root を plan し、全文を plan の出力の Bucket に置く
ci-plan-admin-identity: CI_POLICY_NAMESPACES := plan.stateful_protection plan.kms_key_policy plan.resource_policy_exclusive
ci-plan-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-plan-admin-identity:
	@$(call CI_PLAN,admin,identity)

.PHONY: ci-apply-admin-identity ## [CI] 管理アカウントの identity root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-identity: CI_POLICY_NAMESPACES := plan.stateful_protection plan.kms_key_policy plan.resource_policy_exclusive
ci-apply-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,apply-identity)
ci-apply-admin-identity:
	@$(call CI_APPLY,admin,identity)

.PHONY: ci-unlock-admin-identity ## [CI] 管理アカウントの identity root の state に、apply の job の timeout を超えて残った lock を外す
ci-unlock-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,unlock-identity)
ci-unlock-admin-identity:
	@$(call CI_UNLOCK,admin,identity)

.PHONY: ci-hold-admin-identity ## [CI] 管理アカウントの identity root の state に lock を取り、外さずに残す（bootstrap 段5 の lock の解除の試行）
ci-hold-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,apply-identity)
ci-hold-admin-identity:
	@$(call CI_HOLD,admin,identity)

.PHONY: ci-plan-admin-workload ## [CI] 管理アカウントの workload root を plan し、全文を plan の出力の Bucket に置く
ci-plan-admin-workload: CI_POLICY_NAMESPACES := plan.stateful_protection
ci-plan-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-plan-admin-workload:
	@$(call CI_PLAN,admin,workload)

.PHONY: ci-apply-admin-workload ## [CI] 管理アカウントの workload root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-workload: CI_POLICY_NAMESPACES := plan.stateful_protection
ci-apply-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,apply)
ci-apply-admin-workload:
	@$(call CI_APPLY,admin,workload)

.PHONY: ci-unlock-admin-workload ## [CI] 管理アカウントの workload root の state に、apply の job の timeout を超えて残った lock を外す
ci-unlock-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,unlock)
ci-unlock-admin-workload:
	@$(call CI_UNLOCK,admin,workload)

.PHONY: ci-hold-admin-workload ## [CI] 管理アカウントの workload root の state に lock を取り、外さずに残す（bootstrap 段5 の lock の解除の試行）
ci-hold-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,apply)
ci-hold-admin-workload:
	@$(call CI_HOLD,admin,workload)
