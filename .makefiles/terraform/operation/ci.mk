## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml の plan と、terraform-apply.yaml の apply）
##
## 資格情報は OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の profile に置く。
## plan と apply の順序と失敗の扱いは tf-local の ci-plan / ci-apply が持ち、terraform・aws・conftest は
## そこから AWS_* を外した環境で profile だけを使って起動する（ADR-0702 決定3-4 の CI の入口）。
##
## 入力は環境変数で受ける。CI_ACCOUNT_ID（environment の secret）と CI_PR（Pull Request の番号）は
## workflow が渡し、GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT・RUNNER_TEMP・GITHUB_STEP_SUMMARY と OIDC の
## 要求先は GitHub Actions が渡す。tf-local は sh -c の中で動くので、引数に使う値は export する。
## conftest は $(GO_TOOL) を通さずに起動するので、CI と同じ RUNNER_MODE=host で呼ぶ。

CI_PLAN_DIR ?= $(abspath $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/ci-plan)
CI_ENV ?= admin
CI_ROLE ?= plan
# $(1)=env $(2)=役割。role 名と同じ形にする（tf-local role-arn）。
CI_PROFILE = $(TF_LOCAL_PROJECT)-$(1)-ci-$(2)

# $(1)=env $(2)=役割
define CI_AWS_CREDENTIALS
set -eu -o pipefail; umask 077; d="$(CI_PLAN_DIR)"; mkdir -p "$$d"; token="$$d/oidc-token"; export token CI_ACCOUNT_ID; \
test -n "$${ACTIONS_ID_TOKEN_REQUEST_URL:-}" && test -n "$${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" || { echo "❌ OIDC token を要求できません（job に id-token: write がありません）" >&2; exit 1; }; \
unset_env=$$($(call TF_LOCAL,env-unset)); \
region=$$($(call TF_LOCAL,region "$(call TF_LOCAL_DIR,$(1),identity)")); \
curl -fsS --retry 3 -H "Authorization: bearer $$ACTIONS_ID_TOKEN_REQUEST_TOKEN" "$$ACTIONS_ID_TOKEN_REQUEST_URL&audience=sts.amazonaws.com" | jq -er .value > "$$token"; \
sub=$$($(call TF_LOCAL,oidc-sub "$$token")); echo "OIDC の sub: $$sub"; \
role=$$($(call TF_LOCAL,role-arn -project $(TF_LOCAL_PROJECT) -env $(1) -role $(2) -account "$$CI_ACCOUNT_ID")); \
creds=$$(env $$unset_env AWS_EC2_METADATA_DISABLED=true $(AWS_BIN) sts assume-role-with-web-identity --region "$$region" \
--role-arn "$$role" --role-session-name "gha-$$GITHUB_RUN_ID-$$GITHUB_RUN_ATTEMPT" --web-identity-token "file://$$token" \
--query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' --output text); \
rm -f "$$token"; read -r key secret session <<<"$$creds"; \
for v in "$$key" "$$secret" "$$session"; do echo "::add-mask::$$v"; done; \
cfg="env $$unset_env $(AWS_BIN) configure set --profile $(call CI_PROFILE,$(1),$(2))"; \
$$cfg aws_access_key_id "$$key"; $$cfg aws_secret_access_key "$$secret"; $$cfg aws_session_token "$$session"; $$cfg region "$$region"; \
echo "✅ $(call CI_PROFILE,$(1),$(2)) の profile に資格情報を置きました"
endef

# $(1)=plan|apply $(2)=env $(3)=root $(4)=役割
define CI_RUN
$(call TF_LOCAL,ci-$(1) -project $(TF_LOCAL_PROJECT) -env $(2) -root $(3) -profile $(call CI_PROFILE,$(2),$(4)) \
-base "$(TF_BASE)" -dir "$(CI_PLAN_DIR)" -terraform "$(TF_BIN)" -aws "$(AWS_BIN)" -conftest "$(CONFTEST_BIN)" \
-policy "$(POLICY_PLAN_DIR)" -decl "$(POLICY_PLAN_DECL)"$(if $(filter plan,$(1)), -pr "$$CI_PR" -run "$$GITHUB_RUN_ID" -attempt "$$GITHUB_RUN_ATTEMPT"))
endef

.PHONY: ci-aws-credentials ## [CI] OIDC で CI_ENV の CI_ROLE の role を assume し、profile に資格情報を置く
ci-aws-credentials: SHELL := /bin/bash
ci-aws-credentials:
	@$(call CI_AWS_CREDENTIALS,$(CI_ENV),$(CI_ROLE))

.PHONY: ci-plan-admin-identity ## [CI] 管理アカウントの identity root を plan し、全文を plan の出力の Bucket に置く
ci-plan-admin-identity:
	@$(call CI_RUN,plan,admin,identity,plan)

.PHONY: ci-apply-admin-identity ## [CI] 管理アカウントの identity root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-identity:
	@$(call CI_RUN,apply,admin,identity,apply-identity)
