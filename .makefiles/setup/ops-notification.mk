## ops-notification の手順書（docs/setup/ops-notification.md）の、人が実行する確認と手順の実行
## （.makefiles/README.md の `setup/`、ADR-0702 決定26・27）。各アカウントで、そのアカウントの profile で行う。
## 端末からの入力を読むので、ホストで起動する（ADR-0504 決定1・3）。

SETUP_OPS_USECASE = usecases/ops-notification

# PROFILE・ENV は人が渡す値（.makefiles/README.md の `setup/`）。region は ENV の workload root（env/<ENV>）の provider の region。
setup-ops-notification-%: export SETUP_PROFILE = $(call literal_value,PROFILE)
setup-ops-notification-%: export SETUP_ENV = $(call literal_value,ENV)

# $(1)=入口 $(2)=入口に足す引数
define SETUP_OPS_RUN
@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/$$SETUP_ENV")); env $$unset_env AWS_PROFILE="$$SETUP_PROFILE" AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,setup) $(1) -project $(TF_LOCAL_PROJECT) -region "$$region" -env "$$SETUP_ENV" $(2)
endef

.PHONY: setup-ops-notification-chatbot-role-check ## 1 の前提: Chatbot の service-linked role が在る（PROFILE=<そのアカウントの profile> ENV=<env>）
setup-ops-notification-chatbot-role-check:
	$(call SETUP_OPS_RUN,ops-chatbot-role-check,)

.PHONY: setup-ops-notification-workspace-check ## 3.2 手順4・3.3 手順1: ENABLED の Slack の workspace が在り、その team ID を出す（PROFILE=<profile> ENV=<env>）
setup-ops-notification-workspace-check:
	$(call SETUP_OPS_RUN,ops-workspace-check,-usecase "$(SETUP_OPS_USECASE)")

.PHONY: setup-ops-notification-subscriptions-check ## 3.4 手順1: provider の region と us-east-1 の Topic のそれぞれに Chatbot の購読がちょうど1つ在る（PROFILE=<profile> ENV=<env>）
setup-ops-notification-subscriptions-check:
	$(call SETUP_OPS_RUN,ops-subscriptions-check,-usecase "$(SETUP_OPS_USECASE)")

.PHONY: setup-ops-notification-resubscribe ## 3.4 手順2: 購読の無い Topic を Chatbot の構成から外して保存し、足し直して保存する（PROFILE=<profile> ENV=<env>）
setup-ops-notification-resubscribe:
	$(call SETUP_OPS_RUN,ops-resubscribe,-usecase "$(SETUP_OPS_USECASE)")
