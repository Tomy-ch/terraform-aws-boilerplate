## bootstrap の手順書（docs/setup/bootstrap.md）の、人が実行する手順の実行（.makefiles/README.md の `setup/`、
## ADR-0702 決定26・27）。資格情報は bootstrap の一時 IAM user の profile（TF_LOCAL_PROFILE）で、組織より前に行う。
## 端末からの入力を読むので、ホストで起動する（ADR-0504 決定1・3）。

# ENVIRONMENTS は人が渡す値（.makefiles/README.md の `setup/`）。
setup-bootstrap-%: export SETUP_ENVIRONMENTS = $(call literal_value,ENVIRONMENTS)

# $(1)=入口 $(2)=入口に足す引数
define SETUP_BOOTSTRAP_RUN
@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/admin/identity")); env $$unset_env AWS_PROFILE=$(TF_LOCAL_PROFILE) AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,setup) $(1) -project $(TF_LOCAL_PROJECT) -region "$$region" $(2)
endef

.PHONY: setup-bootstrap-contacts ## 2-1: 主の連絡先と、BILLING・OPERATIONS・SECURITY の代替連絡先を入力して置く
setup-bootstrap-contacts:
	$(call SETUP_BOOTSTRAP_RUN,bootstrap-contacts,)

.PHONY: setup-bootstrap-anomaly-tag ## 2-4: AWS services monitor とその通知に tag <project>:bootstrap を付ける
setup-bootstrap-anomaly-tag:
	$(call SETUP_BOOTSTRAP_RUN,bootstrap-anomaly-tag,)

.PHONY: setup-bootstrap-env-secrets ## 段4・段5: environment に secret AWS_ACCOUNT_ID（管理アカウントの ID）を置く（ENVIRONMENTS=<env>[,<env>...]）
setup-bootstrap-env-secrets:
	$(call SETUP_BOOTSTRAP_RUN,bootstrap-env-secrets,-environments "$$SETUP_ENVIRONMENTS")
