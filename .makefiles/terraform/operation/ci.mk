## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml の plan と、terraform-apply.yaml の apply）と、
## state に残った lock を外す入口（terraform-unlock.yaml）と、lock を外さずに残す入口（terraform-lock-hold.yaml）
##
## 資格情報は tf-local ci-credentials が OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の
## profile に置く。profile を置いた後の terraform と aws は TF_LOCAL_ENV で起動し、AWS_* を外した環境で profile だけを使う。
## ここは道具を起動する順序と、その間で値を運ぶことだけを持ち、値の算出と判定は tf-local が持つ
## （ADR-0702 決定3・24）。plan と apply には、CI_POLICY_NAMESPACES の Policy を掛ける。
## 公開されるログに terraform の出力を出さない。plan と apply の全文は plan の出力の Bucket か apply の出力の Bucket に置き、ログには終了コード・
## 件数・object の key・SHA-256・版 ID と Policy の違反の文だけを出す（ADR-0602 決定13）。
##
## 入力は環境変数で受ける。CI_ACCOUNT_ID（environment の secret）と CI_PR（Pull Request の番号）は
## workflow が渡し、GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT・RUNNER_TEMP・GITHUB_STEP_SUMMARY・GITHUB_OUTPUT と OIDC の
## 要求先は GitHub Actions が渡す。tf-local は sh -c の中で動くので、値は export した変数で渡す。export は代入と
## 分けて書く —— `export x=$(...)` は代入の失敗を export の成功で隠す。

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

CI_PLAN_DIR ?= $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/ci-plan
# lock を残す入口が、plan の全文に lock を取った後の行が現れるのを待つ周期と上限（tf-local lock-wait）。
CI_HOLD_INTERVAL ?= 200ms
CI_HOLD_LIMIT ?= 10m
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

# CI_HOLD が使う。root の名前（$(1)-$(2)）は、decl.json の allow の鍵と同じになる。
# $(3) が空でなければ、失敗して終わるときに、その時点の段（$$stage）を GITHUB_OUTPUT の failure へ書く。段の名前の
# 意味は scripts/apply-revert の revertKinds と skipKinds が持つ。
# $(1)=env $(2)=root $(3)=失敗の段を書くか
define CI_PREPARE
set -eu -o pipefail; umask 077; d="$(CI_PLAN_DIR)"; mkdir -p "$$d"; dir="$(call TF_LOCAL_DIR,$(1),$(2))"; root="$(1)-$(2)"; \
out="$$d/$$root.txt"; json="$$d/$$root.json"; tfplan="$$d/$$root.tfplan"; pol="$$d/$$root.policy.json"; \
rm -f "$$out" "$$json" "$$tfplan" "$$pol"; stage=prepare; \
$(if $(3),trap 'rc=$$?; rm -f "$$out" "$$json" "$$tfplan" "$$pol"; test "$$rc" = 0 || test -z "$${GITHUB_OUTPUT:-}" || printf "failure=%s\n" "$$stage" >> "$$GITHUB_OUTPUT"' EXIT,trap 'rm -f "$$out" "$$json" "$$tfplan" "$$pol"' EXIT); \
export dir root out json pol; \
unset_env=$$($(call TF_LOCAL,env-unset)); \
header=$$($(call TF_LOCAL,plan-header)); echo "$$header" | tee "$$out"; \
region=$$($(call TF_LOCAL,region "$$dir")); export region; \
identity=$$($(TF_LOCAL_ENV) $(AWS_BIN) sts get-caller-identity --region "$$region" --query '[Account,Arn]' --output text); export identity; \
account=$$($(call TF_LOCAL,account -identity "$$identity")); \
bucket=$$($(call TF_LOCAL,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -region "$$region" -identity "$$identity")); \
$(call TF_STATE_KEY,$(1),fail); \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" init -input=false -no-color -lockfile=readonly -backend-config="bucket=$$bucket" $$kms >> "$$out" 2>&1 \
|| { echo "❌ init が失敗しました。全文はランナーの外に出しません" >&2; exit 1; }
endef

# $(1)=env $(2)=root。apply の job の timeout を超えて残った lock だけを、読んだ版の ETag を条件に消す。
# terraform は呼ばない。lock-etag が空を返したときは、後続の道具を呼ばずに成功で終える
# （判定は tf-local が持つ。冒頭と ADR-0702 決定24）。
define CI_UNLOCK
set -eu -o pipefail; umask 077; d="$(CI_PLAN_DIR)"; mkdir -p "$$d"; dir="$(call TF_LOCAL_DIR,$(1),$(2))"; root="$(1)-$(2)"; \
list="$$d/$$root.lock-list.json"; body="$$d/$$root.lock.json"; meta="$$d/$$root.lock-meta.json"; \
rm -f "$$list" "$$body" "$$meta"; trap 'rm -f "$$list" "$$body" "$$meta"' EXIT; export dir list body meta; \
unset_env=$$($(call TF_LOCAL,env-unset)); \
region=$$($(call TF_LOCAL,region "$$dir")); export region; \
identity=$$($(TF_LOCAL_ENV) $(AWS_BIN) sts get-caller-identity --region "$$region" --query '[Account,Arn]' --output text); export identity; \
account=$$($(call TF_LOCAL,account -identity "$$identity")); \
bucket=$$($(call TF_LOCAL,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -region "$$region" -identity "$$identity")); \
key=$$($(call TF_LOCAL,lock-key "$$dir")); export key; \
$(TF_LOCAL_ENV) $(AWS_BIN) s3api list-objects-v2 --region "$$region" --bucket "$$bucket" --prefix "$$key" --expected-bucket-owner "$$account" --no-paginate --output json > "$$list"; \
etag=$$($(call TF_LOCAL,lock-etag -key "$$key" "$$list")); export etag; \
test -n "$$etag" || { echo "✅ lock はありません。何も消しません"; test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\nlock はありません\n' "$$root" >> "$$GITHUB_STEP_SUMMARY"; exit 0; }; \
$(TF_LOCAL_ENV) $(AWS_BIN) s3api get-object --region "$$region" --bucket "$$bucket" --key "$$key" --if-match "$$etag" --expected-bucket-owner "$$account" --output json "$$body" > "$$meta"; \
lock=$$($(call TF_LOCAL,lock-stale -etag "$$etag" -meta "$$meta" "$$body")); echo "$$lock"; \
marker=$$($(TF_LOCAL_ENV) $(AWS_BIN) s3api delete-object --region "$$region" --bucket "$$bucket" --key "$$key" --if-match "$$etag" --expected-bucket-owner "$$account" --query VersionId --output text); \
echo "✅ lock を外しました（delete marker の版 ID: $${marker}）"; \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n```text\n%s\n```\n\nlock を外しました（delete marker の版 ID: `%s`）\n' "$$root" "$$lock" "$$marker" >> "$$GITHUB_STEP_SUMMARY"
endef

# $(1)=env $(2)=root。lock の解除を障害の前に試すために、apply の role で root の lock を取り、外さずに残す
# （ADR-0604 決定8）。lock は terraform 自身に取らせる —— lock の解除が相手にする lock と同じ形にするためである。
# plan を裏で起動し、lock を取った後の行が全文に現れたら SIGKILL で止める。SIGINT と SIGTERM では terraform が
# lock を外してから止まる。lock-wait が空を返したときは後続を省いて失敗する
# （判定は tf-local が持つ。冒頭と ADR-0702 決定24）。
define CI_HOLD
$(call CI_PREPARE,$(1),$(2)); \
list="$$d/$$root.lock-list.json"; body="$$d/$$root.lock.json"; meta="$$d/$$root.lock-meta.json"; hold="$$d/$$root.hold.txt"; pid=; started=; \
rm -f "$$list" "$$body" "$$meta" "$$hold"; \
trap 'rc=$$?; test -z "$$pid" || kill -KILL "$$pid" 2>/dev/null || :; rm -f "$$out" "$$json" "$$tfplan" "$$pol" "$$list" "$$body" "$$meta" "$$hold"; test "$$rc" = 0 || test -z "$$started" || echo "❌ plan を起動した後に失敗しました。lock が残っていれば、lock の解除の閾値を過ぎてから Terraform Unlock で外してください" >&2' EXIT; \
export list body meta hold; key=$$($(call TF_LOCAL,lock-key "$$dir")); export key; \
$(TF_LOCAL_ENV) $(AWS_BIN) s3api list-objects-v2 --region "$$region" --bucket "$$bucket" --prefix "$$key" --expected-bucket-owner "$$account" --no-paginate --output json > "$$list"; \
$(call TF_LOCAL,lock-etag -key "$$key" -want absent "$$list") > /dev/null; \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" plan -input=false -no-color -parallelism=1 > "$$hold" 2>&1 & pid=$$!; started=1; export pid; \
held=$$($(call TF_LOCAL,lock-wait -pid "$$pid" -interval $(CI_HOLD_INTERVAL) -limit $(CI_HOLD_LIMIT) "$$hold")); \
kill -KILL "$$pid" 2>/dev/null || :; wait "$$pid" 2>/dev/null || :; pid=; \
test -n "$$held" || { echo "❌ plan が lock を取った後の行を出さずに終わりました。全文はランナーの外に出しません" >&2; exit 1; }; \
$(TF_LOCAL_ENV) $(AWS_BIN) s3api list-objects-v2 --region "$$region" --bucket "$$bucket" --prefix "$$key" --expected-bucket-owner "$$account" --no-paginate --output json > "$$list"; \
etag=$$($(call TF_LOCAL,lock-etag -key "$$key" -want present "$$list")); export etag; \
$(TF_LOCAL_ENV) $(AWS_BIN) s3api get-object --region "$$region" --bucket "$$bucket" --key "$$key" --if-match "$$etag" --expected-bucket-owner "$$account" --output json "$$body" > "$$meta"; \
lock=$$($(call TF_LOCAL,lock-held -etag "$$etag" -meta "$$meta" "$$body")); echo "$$lock"; \
echo "✅ lock を残しました。外すのは Terraform Unlock です"; \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n```text\n%s\n```\n\nlock を残しました。外すのは Terraform Unlock です\n' "$$root" "$$lock" >> "$$GITHUB_STEP_SUMMARY"
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
ci-unlock-admin-identity: SHELL := /bin/bash
ci-unlock-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,unlock-identity)
ci-unlock-admin-identity:
	@$(call CI_UNLOCK,admin,identity)

.PHONY: ci-hold-admin-identity ## [CI] 管理アカウントの identity root の state に lock を取り、外さずに残す（bootstrap 段5 の lock の解除の試行）
ci-hold-admin-identity: SHELL := /bin/bash
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
ci-unlock-admin-workload: SHELL := /bin/bash
ci-unlock-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,unlock)
ci-unlock-admin-workload:
	@$(call CI_UNLOCK,admin,workload)

.PHONY: ci-hold-admin-workload ## [CI] 管理アカウントの workload root の state に lock を取り、外さずに残す（bootstrap 段5 の lock の解除の試行）
ci-hold-admin-workload: SHELL := /bin/bash
ci-hold-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,apply)
ci-hold-admin-workload:
	@$(call CI_HOLD,admin,workload)
