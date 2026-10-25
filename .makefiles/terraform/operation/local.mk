## 人が手元から Terraform を動かす入口
##
## gates.mk の検査と違い、ホストの AWS の資格情報を使う（ADR-0504 決定1）。呼び出し元の AWS_ の
## 環境変数はすべて外し、ターゲットごとに決めた profile だけを渡す。apply と、-target を通せる口は
## 持たない（ADR-0601 決定3-5、ADR-0202 決定7）。root を足す変更が、その root の init / plan を足す。

AWS_BIN ?= aws
TF_PLAN_LOG_DIR ?= tmp/plan
TF_LOCAL_PROJECT := tabp
# $(1)=env $(2)=identity|workload（ADR-0604 決定5）
TF_LOCAL_DIR = $(TF_BASE)/env/$(1)$(if $(filter identity,$(2)),/identity)
TF_LOCAL_ENV = env $(AWS_ENV_UNSET) AWS_PROFILE=$(1) AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1

# $(1)=env $(2)=root $(3)=profile
#
# backend block は式を持てないので、Bucket 名の規則を写して組む。正本は modules/s3 と
# usecases/state-backend で、写しのずれは tf-local-test が env/admin/identity のテストの期待値と
# 突き合わせて見る。region は backend の値を使う（state Bucket は provider と同じ region に在る）。
define TF_LOCAL_INIT
set -euo pipefail; dir="$(call TF_LOCAL_DIR,$(1),$(2))"; \
if [ ! -f "$$dir/backend.tf" ]; then echo "❌ $$dir/backend.tf がありません" >&2; exit 1; fi; \
region=$$(sed -n 's/^[[:space:]]*region[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$$dir/backend.tf"); \
if [ "$$(printf '%s\n' "$$region" | grep -c .)" -ne 1 ] || [[ ! $$region =~ ^[a-z]{2}(-[a-z]+)+-[0-9]$$ ]]; then \
	echo "❌ $$dir/backend.tf から region を1つに決められません: '$$region'" >&2; exit 1; \
fi; \
identity=$$($(call TF_LOCAL_ENV,$(3)) $(AWS_BIN) sts get-caller-identity --region "$$region" --query '[Account,Arn]' --output text); \
account=$${identity%%[[:space:]]*}; arn=$${identity##*[[:space:]]}; \
if [[ ! $$account =~ ^[0-9]{12}$$ ]]; then echo "❌ アカウント ID を取得できません: '$$account'（make aws-login を実行してください）" >&2; exit 1; fi; \
if [[ $$arn == *:root ]]; then echo "❌ root の資格情報では実行しません: $$arn" >&2; exit 1; fi; \
$(call TF_LOCAL_ENV,$(3)) $(TF_BIN) -chdir="$$dir" init -input=false -lockfile=readonly \
	-backend-config="bucket=$(TF_LOCAL_PROJECT)-$(1)-tfstate-$$account-$$region-an"
endef

# plan は lock を取らない（ADR-0604 決定8）。どの commit の plan かを log の1行目に残す（ADR-0602 決定10）。
define TF_LOCAL_PLAN
set -euo pipefail; mkdir -p "$(TF_PLAN_LOG_DIR)"; \
dirty=""; if [ -n "$$(git status --porcelain)" ]; then dirty=" （作業ツリーに未コミットの変更あり）"; fi; \
{ echo "commit: $$(git rev-parse HEAD)$$dirty"; \
	$(call TF_LOCAL_ENV,$(3)) $(TF_BIN) -chdir="$(call TF_LOCAL_DIR,$(1),$(2))" plan -lock=false -input=false -no-color 2>&1; \
} | tee "$(TF_PLAN_LOG_DIR)/$(1)-$(2).log"
endef

.PHONY: aws-login ## bootstrap のプロファイル（tabp-bootstrap）で aws login する
aws-login:
	@env $(AWS_ENV_UNSET) $(AWS_BIN) login --profile tabp-bootstrap

.PHONY: init-admin-identity ## 管理アカウントの identity root を state Bucket の backend で init する（要 make aws-login）
init-admin-identity: SHELL := /bin/bash
init-admin-identity:
	@$(call TF_LOCAL_INIT,admin,identity,tabp-bootstrap)

.PHONY: plan-admin-identity ## 管理アカウントの identity root を lock を取らずに plan し、出力を tmp/plan/ に残す（要 make aws-login）
plan-admin-identity: SHELL := /bin/bash
plan-admin-identity: init-admin-identity
	@$(call TF_LOCAL_PLAN,admin,identity,tabp-bootstrap)
