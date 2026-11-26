## organization-baseline の手順書（docs/setup/organization-baseline.md）の、人が実行する確認と手順の実行
## （.makefiles/README.md の `setup/`、ADR-0702 決定26・27）。
## 端末からの入力を読むので、ホストで起動する（ADR-0504 決定1・3）。

SETUP_USECASE = usecases/organization-baseline

# PROFILE と MEMBER_ENV は人が渡す値なので、レシピの本文に埋めず、字面のまま環境変数で渡す。本文に埋めると、
# 値の中の空白や `;` が、外した資格情報の変数を足し直す引数や別のコマンドとして読まれる。
setup-organization-baseline-%: export SETUP_PROFILE = $(call literal_value,PROFILE)
setup-organization-baseline-%: export SETUP_MEMBER_ENV = $(call literal_value,MEMBER_ENV)

# $(1)=入口 $(2)=入口に足す引数
define SETUP_RUN
@set -eu; unset_env=$$($(call TF_LOCAL,env-unset)); region=$$($(call TF_LOCAL,region "$(TF_BASE)/env/admin/identity")); env $$unset_env AWS_PROFILE="$$SETUP_PROFILE" AWS_EC2_METADATA_DISABLED=true $(call RUN_SCRIPT_HOST_ARGS,setup) $(1) -project $(TF_LOCAL_PROJECT) -region "$$region" $(2)
endef

.PHONY: setup-organization-baseline-member-emails ## 3.8 手順3: メンバーアカウントのメールを env ごとに入力して parameter に置く（PROFILE=<管理アカウントの profile>）
setup-organization-baseline-member-emails:
	$(call SETUP_RUN,member-emails,-usecase "$(SETUP_USECASE)")

.PHONY: setup-organization-baseline-member-emails-check ## 3.8 手順3 の確認: メールの parameter が env の一覧と在るアカウントに合う（PROFILE=<profile>）
setup-organization-baseline-member-emails-check:
	$(call SETUP_RUN,member-emails-check,-usecase "$(SETUP_USECASE)")

.PHONY: setup-organization-baseline-member-email-update ## 3.8 末尾: アカウントのメールを AWS の側で変え、parameter を合わせる（PROFILE=<profile> MEMBER_ENV=<env>）
setup-organization-baseline-member-email-update:
	$(call SETUP_RUN,member-email-update,-usecase "$(SETUP_USECASE)" -env "$$SETUP_MEMBER_ENV")

.PHONY: setup-organization-baseline-account-quota-check ## 3.8 手順2: アカウント数の上限が、在るアカウントと作るアカウントの計以上である（PROFILE=<profile>）
setup-organization-baseline-account-quota-check:
	$(call SETUP_RUN,account-quota-check,-usecase "$(SETUP_USECASE)")

.PHONY: setup-organization-baseline-member-accounts-check ## 3.8 手順4.1: メンバーアカウントが期待する OU に ACTIVE で在る（PROFILE=<profile>、取り込みの ID は IDS=1）
setup-organization-baseline-member-accounts-check:
	$(call SETUP_RUN,member-accounts-check,-usecase "$(SETUP_USECASE)"$(if $(filter 1,$(IDS)), -ids))

.PHONY: setup-organization-baseline-stacksets-access ## 3.5: StackSets の信頼アクセスを有効にする（PROFILE=<profile>）
setup-organization-baseline-stacksets-access:
	$(call SETUP_RUN,stacksets-access,)

.PHONY: setup-organization-baseline-stacksets-access-check ## 3.5 の確認: StackSets の信頼アクセスと、その service-linked role が在る（PROFILE=<profile>）
setup-organization-baseline-stacksets-access-check:
	$(call SETUP_RUN,stacksets-access-check,)

.PHONY: setup-organization-baseline-stack-instances-check ## 3.5 の昇格の後: break-glass の StackSet が在り、メンバーアカウントのそれぞれに CURRENT の stack instance が在る（PROFILE=<profile>）
setup-organization-baseline-stack-instances-check:
	$(call SETUP_RUN,stack-instances-check,-usecase "$(SETUP_USECASE)")

.PHONY: setup-organization-baseline-service-access ## 3.9: セキュリティのサービスの信頼アクセスを有効にする（PROFILE=<profile>）
setup-organization-baseline-service-access:
	$(call SETUP_RUN,service-access,)

.PHONY: setup-organization-baseline-service-access-check ## 3.9 の確認: セキュリティのサービスの信頼アクセスがすべて有効である（PROFILE=<profile>）
setup-organization-baseline-service-access-check:
	$(call SETUP_RUN,service-access-check,)
