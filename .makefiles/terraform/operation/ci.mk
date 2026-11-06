## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml の plan と、terraform-apply.yaml の apply）
##
## 資格情報は OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の profile に置く。
## terraform と aws は local.mk と同じ TF_LOCAL_ENV で起動し、AWS_* を外した環境で profile だけを使う。
## ここは道具を起動する順序と、その間で値を運ぶことだけを持ち、値の算出と判定は tf-local が持つ
## （ADR-0702 決定3・24）。plan と apply には ADR-0604 決定11 の Policy（plan.stateful_protection）を掛ける。
## 公開されるログに terraform の出力を出さない。plan の全文は plan の出力の Bucket に置き、ログには終了コード・
## 件数・object の key・SHA-256・版 ID と Policy の違反の文だけを出す（ADR-0602 決定13）。apply の全文は
## ランナーの外に出さない。
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
set -eu -o pipefail; umask 077; d="$(CI_PLAN_DIR)"; mkdir -p "$$d"; token="$$d/oidc-token"; resp="$$d/oidc-response"; \
trap 'rm -f "$$token" "$$resp"' EXIT; export token resp CI_ACCOUNT_ID; \
url=$$($(call TF_LOCAL,oidc-url)); \
unset_env=$$($(call TF_LOCAL,env-unset)); \
region=$$($(call TF_LOCAL,region "$(call TF_LOCAL_DIR,$(1),identity)")); \
curl -fsS --retry 3 -H "Authorization: bearer $$ACTIONS_ID_TOKEN_REQUEST_TOKEN" -o "$$resp" "$$url"; \
$(call TF_LOCAL,oidc-token "$$resp") > "$$token"; \
sub=$$($(call TF_LOCAL,oidc-sub "$$token")); echo "OIDC の sub: $$sub"; \
role=$$($(call TF_LOCAL,role-arn -project $(TF_LOCAL_PROJECT) -env $(1) -role $(2) -account "$$CI_ACCOUNT_ID")); \
creds=$$(env $$unset_env AWS_EC2_METADATA_DISABLED=true $(AWS_BIN) sts assume-role-with-web-identity --region "$$region" \
--role-arn "$$role" --role-session-name "gha-$$GITHUB_RUN_ID-$$GITHUB_RUN_ATTEMPT" --web-identity-token "file://$$token" \
--query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' --output text); \
read -r key secret session <<<"$$creds"; \
for v in "$$key" "$$secret" "$$session"; do echo "::add-mask::$$v"; done; \
cfg="env $$unset_env $(AWS_BIN) configure set --profile $(call CI_PROFILE,$(1),$(2))"; \
$$cfg aws_access_key_id "$$key"; $$cfg aws_secret_access_key "$$secret"; $$cfg aws_session_token "$$session"; $$cfg region "$$region"; \
echo "✅ $(call CI_PROFILE,$(1),$(2)) の profile に資格情報を置きました"
endef

# CI_PLAN と CI_APPLY が共有する。root の名前（$(1)-$(2)）は、decl.json の allow の鍵と同じになる。
# 全文、plan、その JSON と Policy の結果は、成否にかかわらず終了時に消す。$(1)=env $(2)=root
define CI_PREPARE
set -eu -o pipefail; umask 077; d="$(CI_PLAN_DIR)"; mkdir -p "$$d"; dir="$(call TF_LOCAL_DIR,$(1),$(2))"; root="$(1)-$(2)"; \
out="$$d/$$root.txt"; json="$$d/$$root.json"; tfplan="$$d/$$root.tfplan"; pol="$$d/$$root.policy.json"; \
rm -f "$$out" "$$json" "$$tfplan" "$$pol"; trap 'rm -f "$$out" "$$json" "$$tfplan" "$$pol"' EXIT; export dir root out json pol; \
unset_env=$$($(call TF_LOCAL,env-unset)); \
header=$$($(call TF_LOCAL,plan-header)); echo "$$header" | tee "$$out"; \
region=$$($(call TF_LOCAL,region "$$dir")); export region; \
identity=$$($(TF_LOCAL_ENV) $(AWS_BIN) sts get-caller-identity --region "$$region" --query '[Account,Arn]' --output text); export identity; \
account=$$($(call TF_LOCAL,account -identity "$$identity")); \
bucket=$$($(call TF_LOCAL,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -region "$$region" -identity "$$identity")); \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" init -input=false -no-color -lockfile=readonly -backend-config="bucket=$$bucket" >> "$$out" 2>&1 \
|| { echo "❌ init が失敗しました。全文はランナーの外に出しません" >&2; exit 1; }
endef

# plan を JSON にし、終了コードと突き合わせた要約を出す。plan が失敗していれば show も失敗し、判定は
# plan-summary が持つ。$(1)=plan の追加の引数
define CI_TERRAFORM_PLAN
rc=0; $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" plan $(1)-input=false -no-color -detailed-exitcode -out="$$tfplan" >> "$$out" 2>&1 || rc=$$?; \
echo "plan の終了コード: $$rc"; export rc; \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" show -json "$$tfplan" > "$$json" 2>> "$$out" || :
endef

# $$json の plan に Policy を掛け、違反か評価の欠けがあれば失敗する。
define CI_POLICY
$(GO_TOOL) $(CONFTEST_BIN) test --combine --no-color -o json --namespace plan.stateful_protection --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DECL) "$$json" > "$$pol" || :; \
$(call TF_LOCAL,policy-result -namespace plan.stateful_protection -rego $(POLICY_PLAN_DIR)/stateful_protection.rego "$$pol") \
|| { echo "❌ ADR-0604 決定11・12 の Policy を通りません（理由は上の行）" >&2; exit 1; }
endef

# $(1)=env $(2)=root。plan は lock を取らない（ADR-0604 決定8）。plan が失敗しても、Bucket に書けるなら
# 全文を置いてから失敗する。
define CI_PLAN
$(call CI_PREPARE,$(1),$(2)); export CI_PR GITHUB_RUN_ID GITHUB_RUN_ATTEMPT; \
plan_bucket=$$($(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" output -raw plan_output_bucket_name 2>> "$$out") \
|| { echo "❌ plan の出力の Bucket の名前を state から読めません" >&2; exit 1; }; \
$(call CI_TERRAFORM_PLAN,-lock=false ); \
key=$$($(call TF_LOCAL,plan-key -pr "$$CI_PR" -run "$$GITHUB_RUN_ID" -attempt "$$GITHUB_RUN_ATTEMPT" -root "$$root")); \
sha=$$($(call TF_LOCAL,sha256 "$$out")); \
echo "plan の全文: key=$$key sha256=$$sha"; \
version=$$($(TF_LOCAL_ENV) $(AWS_BIN) s3api put-object --region "$$region" --bucket "$$plan_bucket" --key "$$key" --body "$$out" \
--checksum-sha256 "$$sha" --expected-bucket-owner "$$account" --query VersionId --output text); \
echo "plan の全文の版 ID: $$version"; \
summary=$$($(call TF_LOCAL,plan-summary -exitcode "$$rc" "$$json")) || { echo "❌ 全文は上の key にあります" >&2; exit 1; }; echo "$$summary"; \
$(CI_POLICY); \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n%s\n\n- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n' "$$root" "$$summary" "$$key" "$$sha" "$$version" >> "$$GITHUB_STEP_SUMMARY"
endef

# $(1)=env $(2)=root。apply する commit で plan し直し、Policy を通った plan だけを apply する（ADR-0601 決定6、
# ADR-0604 決定11）。plan は lock を取る —— ADR-0604 決定8 の plan は plan の job の plan である。
# 変更の無い plan も apply し、経路と lock の取得を毎回同じにする。
define CI_APPLY
$(call CI_PREPARE,$(1),$(2)); \
$(call CI_TERRAFORM_PLAN,); \
summary=$$($(call TF_LOCAL,plan-summary -exitcode "$$rc" "$$json")); echo "$$summary"; \
$(CI_POLICY); \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" apply -input=false -no-color "$$tfplan" >> "$$out" 2>&1 \
|| { echo "❌ apply が失敗しました。全文はランナーの外に出しません" >&2; exit 1; }; \
echo "✅ apply が終わりました"; \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n%s\n\napply が終わりました\n' "$$root" "$$summary" >> "$$GITHUB_STEP_SUMMARY"
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

.PHONY: ci-apply-admin-identity ## [CI] 管理アカウントの identity root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-identity: SHELL := /bin/bash
ci-apply-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,apply-identity)
ci-apply-admin-identity:
	@$(call CI_APPLY,admin,identity)
