## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml）
##
## 資格情報は OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の profile に置く。
## terraform と aws は local.mk と同じ TF_LOCAL_ENV で起動し、AWS_* を外した環境で profile だけを使う。
## 公開されるログに terraform の出力を出さない。全文は plan の出力の Bucket に置き、ログには終了コード・
## 件数・object の key・SHA-256・版 ID だけを出す（ADR-0602 決定13）。apply の口は持たない（#205）。
##
## 入力は環境変数で受ける。CI_ACCOUNT_ID（environment の secret）と CI_PR（Pull Request の番号）は
## workflow が渡し、GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT・RUNNER_TEMP・GITHUB_STEP_SUMMARY と OIDC の
## 要求先は GitHub Actions が渡す。tf-local は sh -c の中で動くので、引数に使う値は export する。

CI_PLAN_DIR ?= $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/ci-plan
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

# $(1)=env $(2)=root。plan は lock を取らない（ADR-0604 決定8）。plan が失敗しても、Bucket に書けるなら
# 全文を置いてから失敗する。
define CI_PLAN
set -eu -o pipefail; umask 077; d="$(CI_PLAN_DIR)"; mkdir -p "$$d"; dir="$(call TF_LOCAL_DIR,$(1),$(2))"; root="$(1)-$(2)"; \
out="$$d/$$root.txt"; json="$$d/$$root.json"; tfplan="$$d/$$root.tfplan"; rm -f "$$out" "$$json" "$$tfplan"; \
export dir root json CI_PR GITHUB_RUN_ID GITHUB_RUN_ATTEMPT; \
unset_env=$$($(call TF_LOCAL,env-unset)); \
header=$$($(call TF_LOCAL,plan-header)); echo "$$header" | tee "$$out"; \
region=$$($(call TF_LOCAL,region "$$dir")); export region; \
identity=$$($(TF_LOCAL_ENV) $(AWS_BIN) sts get-caller-identity --region "$$region" --query '[Account,Arn]' --output text); export identity; \
bucket=$$($(call TF_LOCAL,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -region "$$region" -identity "$$identity")); \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" init -input=false -no-color -lockfile=readonly -backend-config="bucket=$$bucket" >> "$$out" 2>&1 \
|| { echo "❌ init が失敗しました。全文はランナーの外に出しません" >&2; exit 1; }; \
plan_bucket=$$($(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" output -raw plan_output_bucket_name 2>> "$$out") \
|| { echo "❌ plan の出力の Bucket の名前を state から読めません" >&2; exit 1; }; \
rc=0; $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" plan -lock=false -input=false -no-color -detailed-exitcode -out="$$tfplan" >> "$$out" 2>&1 || rc=$$?; \
echo "plan の終了コード: $$rc"; export rc; \
test "$$rc" = 1 || $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" show -json "$$tfplan" > "$$json" 2>> "$$out" || rc=1; \
key=$$($(call TF_LOCAL,plan-key -pr "$$CI_PR" -run "$$GITHUB_RUN_ID" -attempt "$$GITHUB_RUN_ATTEMPT" -root "$$root")); \
sha=$$(openssl dgst -sha256 -binary "$$out" | base64); echo "plan の全文: key=$$key sha256=$$sha"; \
version=$$($(TF_LOCAL_ENV) $(AWS_BIN) s3api put-object --region "$$region" --bucket "$$plan_bucket" --key "$$key" --body "$$out" \
--checksum-sha256 "$$sha" --expected-bucket-owner "$${identity%%[[:space:]]*}" --query VersionId --output text); \
echo "plan の全文の版 ID: $$version"; rm -f "$$tfplan" "$$out"; \
test "$$rc" != 1 || { echo "❌ plan が失敗しました。全文は上の key にあります" >&2; exit 1; }; \
summary=$$($(call TF_LOCAL,plan-summary -exitcode "$$rc" "$$json")); echo "$$summary"; \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n%s\n\n- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n' "$$root" "$$summary" "$$key" "$$sha" "$$version" >> "$$GITHUB_STEP_SUMMARY"
endef

.PHONY: ci-aws-credentials ## [CI] OIDC で CI_ENV の CI_ROLE の role を assume し、profile に資格情報を置く
ci-aws-credentials: SHELL := /bin/bash
ci-aws-credentials:
	@$(call CI_AWS_CREDENTIALS,$(CI_ENV),$(CI_ROLE))

.PHONY: ci-plan-admin-identity ## [CI] 管理アカウントの identity root を plan し、全文を plan の出力の Bucket に置く
ci-plan-admin-identity: SHELL := /bin/bash
ci-plan-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-plan-admin-identity:
	@$(call CI_PLAN,admin,identity)
