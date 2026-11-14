## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml の plan と、terraform-apply.yaml の apply）と、
## state に残った lock を外す入口（terraform-unlock.yaml）
##
## 資格情報は OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の profile に置く。
## profile を置いた後の terraform と aws は local.mk と同じ TF_LOCAL_ENV で起動し、AWS_* を外した環境で profile だけを使う。
## ここは道具を起動する順序と、その間で値を運ぶことだけを持ち、値の算出と判定は tf-local が持つ
## （ADR-0702 決定3・24）。plan と apply には ADR-0604 決定11・12 の Policy（plan.stateful_protection）を掛ける。
## 公開されるログに terraform の出力を出さない。plan と apply の全文は plan の出力の Bucket に置き、ログには終了コード・
## 件数・object の key・SHA-256・版 ID と Policy の違反の文だけを出す（ADR-0602 決定13）。
##
## 入力は環境変数で受ける。CI_ACCOUNT_ID（environment の secret）と CI_PR（Pull Request の番号）は
## workflow が渡し、GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT・RUNNER_TEMP・GITHUB_STEP_SUMMARY と OIDC の
## 要求先は GitHub Actions が渡す。tf-local に渡す値の export は local.mk 冒頭の注記に従う。

CI_PLAN_DIR ?= $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/ci-plan
CI_ENV ?= admin
# apply を SIGINT で打ち切るまでの時間。apply の job の timeout-minutes より、init・plan と、打ち切りの後に terraform が
# state を書いて lock を外し、全文を置くまでの分だけ短くする。job の timeout が先に来ると全文を置けない。
CI_APPLY_TIMEOUT ?= 20m
TIMEOUT_BIN ?= timeout
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
# $(1)=env $(2)=root
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
$(call TF_STATE_KEY,$(1),fail); \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" init -input=false -no-color -lockfile=readonly -backend-config="bucket=$$bucket" $$kms >> "$$out" 2>&1 \
|| { echo "❌ init が失敗しました。全文はランナーの外に出しません" >&2; exit 1; }
endef

# plan が失敗していれば show も失敗するが、ここでは落とさず、判定は呼び出し側の plan-summary が持つ。
# $(1)=plan の追加の引数
define CI_TERRAFORM_PLAN
rc=0; $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" plan $(1)-input=false -no-color -detailed-exitcode -out="$$tfplan" >> "$$out" 2>&1 || rc=$$?; \
echo "plan の終了コード: $$rc"; export rc; \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" show -json "$$tfplan" > "$$json" 2>> "$$out" || :
endef

# $$json の plan に Policy を掛け、違反か評価の欠けがあれば $(1) を実行してから失敗する。
# $(1)=失敗する前に実行するコマンド（`;` で終える）
define CI_POLICY
$(GO_TOOL) $(CONFTEST_BIN) test --combine --no-color -o json --namespace plan.stateful_protection --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DECL) "$$json" > "$$pol" || :; \
$(call TF_LOCAL,policy-result -namespace plan.stateful_protection -rego $(POLICY_PLAN_DIR)/stateful_protection.rego "$$pol") \
|| { $(1)echo "❌ plan.stateful_protection の Policy を通りません（理由は上の行）" >&2; exit 1; }
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
$(call CI_POLICY,); \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n%s\n\n- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n' "$$root" "$$summary" "$$key" "$$sha" "$$version" >> "$$GITHUB_STEP_SUMMARY"
endef

# $(1)=env $(2)=root。apply する commit で plan し直し、Policy を通った plan だけを apply する（ADR-0601 決定6、
# ADR-0604 決定11）。plan は lock を取る —— ADR-0604 決定8 の plan は plan の job の plan である。
# 変更の無い plan も apply し、経路と lock の取得を毎回同じにする。plan の出力の Bucket の apply/ に書けるのは
# identity root の apply の role（apply-identity）だけなので、全文を置くのはこの入口だけである（ADR-0602 決定13）。
# workload root の apply は、この define を使えない。
# Bucket の名前と key を得た後は、plan・Policy・apply のどこで失敗しても全文を置いてから失敗する。打ち切りは
# CI_APPLY_TIMEOUT（短くする理由はその注記）。
define CI_APPLY
$(call CI_PREPARE,$(1),$(2)); export GITHUB_RUN_ID GITHUB_RUN_ATTEMPT; \
plan_bucket=$$($(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" output -raw plan_output_bucket_name 2>> "$$out") \
|| { echo "❌ plan の出力の Bucket の名前を state から読めません" >&2; exit 1; }; \
test -n "$$plan_bucket" || { echo "❌ plan の出力の Bucket の名前が空です" >&2; exit 1; }; \
key=$$($(call TF_LOCAL,apply-key -run "$$GITHUB_RUN_ID" -attempt "$$GITHUB_RUN_ATTEMPT" -root "$$root")); \
upload() { sha=$$($(call TF_LOCAL,sha256 "$$out")); echo "apply の全文: key=$$key sha256=$$sha"; \
version=$$($(TF_LOCAL_ENV) $(AWS_BIN) s3api put-object --region "$$region" --bucket "$$plan_bucket" --key "$$key" --body "$$out" \
--checksum-sha256 "$$sha" --expected-bucket-owner "$$account" --query VersionId --output text); \
echo "apply の全文の版 ID: $$version"; }; \
$(call CI_TERRAFORM_PLAN,); \
summary=$$($(call TF_LOCAL,plan-summary -exitcode "$$rc" "$$json")) || { upload; echo "❌ 全文は上の key にあります" >&2; exit 1; }; echo "$$summary"; \
$(call CI_POLICY,upload; ); \
arc=0; $(TIMEOUT_BIN) -s INT $(CI_APPLY_TIMEOUT) $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" apply -input=false -no-color "$$tfplan" >> "$$out" 2>&1 || arc=$$?; export arc; echo "apply の終了コード: $$arc"; \
upload; \
result=$$($(call TF_LOCAL,apply-result -exitcode "$$arc")) || { echo "❌ 全文は上の key にあります" >&2; exit 1; }; echo "$$result"; \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n%s\n\napply が終わりました\n\n- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n' "$$root" "$$summary" "$$key" "$$sha" "$$version" >> "$$GITHUB_STEP_SUMMARY"
endef

# $(1)=env $(2)=root。apply の job の timeout を超えて残った lock だけを、読んだ版の ETag を条件に消す。
# terraform は呼ばない。lock の有無と古さの判定は tf-local の lock-etag と lock-stale が持ち、ここは lock-etag が
# 空を返したときに後続の道具を呼ばずに成功で終えることだけを決める（ADR-0702 決定24 の、空を条件に起動を省く順序）。
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

.PHONY: ci-unlock-admin-identity ## [CI] 管理アカウントの identity root の state に、apply の job の timeout を超えて残った lock を外す
ci-unlock-admin-identity: SHELL := /bin/bash
ci-unlock-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,unlock-identity)
ci-unlock-admin-identity:
	@$(call CI_UNLOCK,admin,identity)
