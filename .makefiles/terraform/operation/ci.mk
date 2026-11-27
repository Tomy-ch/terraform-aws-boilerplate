## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml の plan と、terraform-apply.yaml の apply と、
## terraform-drift.yaml の drift の検出）と、state に残った lock を外す入口（terraform-unlock.yaml）と、lock を外さずに残す
## 入口（terraform-lock-hold.yaml）
##
## ここは scripts/tf-local の段を起動する順序だけを持つ。段は1回の起動で1つの目的の道具だけを固定の引数で起動し、
## 値の算出と判定を持ち、段の間の値は CI_STATE のファイルで運ぶ（ADR-0702 決定3・24）。資格情報は ci-credentials が
## OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の profile に置く。後の段は AWS_* を外した
## 環境で、その profile だけを使う。plan と apply には、入口の CI_POLICY_NAMESPACES の Policy を掛ける。
## 公開されるログに terraform の出力を出さない。全文は plan の出力の Bucket か apply の出力の Bucket に置き、ログには
## 終了コード・件数・object の key・SHA-256・版 ID と Policy の違反の文だけを出す（ADR-0602 決定13）。drift の入口は、
## 変わる resource の伏せた address と最上位の属性の名前を、ログではなく job summary にだけ出す。
##
## 入力は環境変数で受ける。CI_ACCOUNT_ID（environment の secret）と CI_PR（Pull Request の番号）と CI_NEEDS（drift の報告の
## job が受ける needs の JSON）は workflow が渡し、
## GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT・RUNNER_TEMP・GITHUB_STEP_SUMMARY・GITHUB_OUTPUT と OIDC の要求先は
## GitHub Actions が渡す。

CI_PLAN_DIR ?= $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/ci-plan
# drift の報告の入力（開いている issue の一覧。workflow の step が gh で書く）と、issue の本文の置き場。
CI_DRIFT_ISSUES ?= $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/drift-issues.json
CI_DRIFT_BODY ?= $(CI_PLAN_DIR)/drift-report.md
CI_ROLE ?= plan
# $(1)=env $(2)=役割。role 名と同じ形にする（tf-local ci-credentials が置く profile の名前）。
CI_PROFILE = $(TF_LOCAL_PROJECT)-$(1)-ci-$(2)

# 段の state のファイル。root の名前（$(1)-$(2)）は、decl.json の allow の鍵と同じになる。$(1)=env $(2)=root
CI_STATE = $(CI_PLAN_DIR)/$(1)-$(2).json

# plan と apply の入口の段（tf-local の ci-*）。prepare と init が落ちればそこで止まる。それより後の段は `-` で続け、
# 前の段の失敗を state で知って何もしない。upload は必ず走り、finish が state の判定で落とす。
# CI_AFTER_POLICY は、Policy の後に足す段の macro の名前（入口の target が与える。空なら足さない）。
# $(1)=plan|apply $(2)=env $(3)=root
define CI_STEPS_PLAN
@mkdir -p "$(CI_PLAN_DIR)"
-@$(call TF_LOCAL,plan-header) > "$(CI_PLAN_DIR)/$(2)-$(3).header"
@$(call TF_LOCAL,ci-prepare -mode $(1) -project $(TF_LOCAL_PROJECT) -env $(2) -root $(3) -profile $(TF_LOCAL_PROFILE) -base $(TF_BASE) -header "$(CI_PLAN_DIR)/$(2)-$(3).header" -state "$(call CI_STATE,$(2),$(3))")
@$(call TF_LOCAL,ci-init -state "$(call CI_STATE,$(2),$(3))")
-@$(call TF_LOCAL,ci-plan -state "$(call CI_STATE,$(2),$(3))")
-@$(call TF_LOCAL,ci-policy -state "$(call CI_STATE,$(2),$(3))" -work tmp/ci-policy -policy $(POLICY_PLAN_DIR) -decl $(POLICY_PLAN_DECL) -layer $(POLICY_LAYER_DECL) $(foreach ns,$(CI_POLICY_NAMESPACES),-namespace $(ns)))$(call $(CI_AFTER_POLICY),$(2),$(3))
endef

# $(1)=env $(2)=root。CI の role の権限を評価する段（tf-local の ci-permissions）。段が要るかは prepare が入口の値から
# 決めて state に残し、finish がそれで落とす。bootstrap-check を build できなければ、ci-permissions の段が失敗として残る。
# 先頭の空行は、Policy の段の行の後に改行を置くためのもの。
define CI_PERMISSIONS

-@$(call BUILD_SCRIPT_HOST,bootstrap-check)
-@$(call TF_LOCAL,ci-permissions -state "$(call CI_STATE,$(1),$(2))" -work tmp/ci-permissions)
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

# $(1)=env $(2)=root。drift の検出（ADR-0601 決定12）。ci-drift-lock を ci-plan の前と後に置き、lock file が在れば apply と
# 重なったとして判定しない。lock と Policy を使わない理由は .github/workflows/README.md の「drift の job」。
define CI_DRIFT
@mkdir -p "$(CI_PLAN_DIR)"
-@$(call TF_LOCAL,plan-header) > "$(CI_PLAN_DIR)/$(1)-$(2).header"
@$(call TF_LOCAL,ci-prepare -mode drift -project $(TF_LOCAL_PROJECT) -env $(1) -root $(2) -profile $(TF_LOCAL_PROFILE) -base $(TF_BASE) -header "$(CI_PLAN_DIR)/$(1)-$(2).header" -state "$(call CI_STATE,$(1),$(2))")
@$(call TF_LOCAL,ci-init -state "$(call CI_STATE,$(1),$(2))")
-@$(call TF_LOCAL,ci-drift-lock -at before -state "$(call CI_STATE,$(1),$(2))")
-@$(call TF_LOCAL,ci-plan -state "$(call CI_STATE,$(1),$(2))")
-@$(call TF_LOCAL,ci-drift-lock -at after -state "$(call CI_STATE,$(1),$(2))")
-@$(call TF_LOCAL,ci-drift -state "$(call CI_STATE,$(1),$(2))")
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
	@$(if $(CI_ENV),,$(error CI_ENV が空です))$(call TF_LOCAL,ci-credentials -project $(TF_LOCAL_PROJECT) -env $(CI_ENV) -role $(CI_ROLE) -base $(TF_BASE) -work tmp/ci-assume)

.PHONY: ci-assume-denied ## [CI] CI_ENV の plan の job の OIDC token で、pull_request で起動しない種別の role の assume がすべて拒否されることを確かめる
ci-assume-denied:
	@$(if $(CI_ENV),,$(error CI_ENV が空です))$(call TF_LOCAL,ci-assume-denied -project $(TF_LOCAL_PROJECT) -env $(CI_ENV) -base $(TF_BASE) -work tmp/ci-assume)

.PHONY: ci-target-drift ## [CI] CI_ENV のデプロイブランチの先頭の commit と、その木が drift の入口を持つか（昇格の済み）を GITHUB_OUTPUT に書く
ci-target-drift:
	@$(if $(CI_ENV),,$(error CI_ENV が空です))$(call TF_LOCAL,ci-drift-target -env $(CI_ENV))

.PHONY: ci-report-drift ## [CI] drift の job の結果（CI_NEEDS）と開いている issue から、issue の本文・開閉・通知を決める
ci-report-drift:
	@$(call TF_LOCAL,ci-drift-report -issues "$(CI_DRIFT_ISSUES)" -body "$(CI_DRIFT_BODY)")

.PHONY: ci-plan-admin-identity ## [CI] 管理アカウントの identity root を plan し、全文を plan の出力の Bucket に置く
ci-plan-admin-identity: CI_POLICY_NAMESPACES := plan.stateful_protection plan.kms_key_policy plan.resource_policy_exclusive plan.s3_bucket_namespace plan.state_backend_write_deny
ci-plan-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-plan-admin-identity: CI_AFTER_POLICY := CI_PERMISSIONS
ci-plan-admin-identity:
	@$(call CI_PLAN,admin,identity)

.PHONY: ci-apply-admin-identity ## [CI] 管理アカウントの identity root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-identity: CI_POLICY_NAMESPACES := plan.stateful_protection plan.kms_key_policy plan.resource_policy_exclusive plan.s3_bucket_namespace plan.state_backend_write_deny
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
ci-plan-admin-workload: CI_POLICY_NAMESPACES := plan.stateful_protection plan.s3_bucket_namespace
ci-plan-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-plan-admin-workload:
	@$(call CI_PLAN,admin,workload)

.PHONY: ci-apply-admin-workload ## [CI] 管理アカウントの workload root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-workload: CI_POLICY_NAMESPACES := plan.stateful_protection plan.s3_bucket_namespace
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

.PHONY: ci-drift-admin-identity ## [CI] 管理アカウントの identity root をデプロイブランチの木で plan し、drift を判定する
ci-drift-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-drift-admin-identity:
	@$(call CI_DRIFT,admin,identity)

.PHONY: ci-drift-admin-workload ## [CI] 管理アカウントの workload root をデプロイブランチの木で plan し、drift を判定する
ci-drift-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-drift-admin-workload:
	@$(call CI_DRIFT,admin,workload)
