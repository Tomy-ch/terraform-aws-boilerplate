## CI が Terraform を動かす入口（.github/workflows/terraform-gates.yaml の plan と、terraform-apply.yaml の apply）と、
## state に残った lock を外す入口（terraform-unlock.yaml）と、lock を外さずに残す入口（terraform-lock-hold.yaml）
##
## 資格情報は OIDC の token で role を1回だけ assume し、使い捨てのランナーの ~/.aws の profile に置く。
## profile を置いた後の terraform と aws は local.mk と同じ TF_LOCAL_ENV で起動し、AWS_* を外した環境で profile だけを使う。
## ここは道具を起動する順序と、その間で値を運ぶことだけを持ち、値の算出と判定は tf-local が持つ
## （ADR-0702 決定3・24）。plan と apply には、CI_POLICY_NAMESPACES の Policy を掛ける。
## 公開されるログに terraform の出力を出さない。plan と apply の全文は plan の出力の Bucket か apply の出力の Bucket に置き、ログには終了コード・
## 件数・object の key・SHA-256・版 ID と Policy の違反の文だけを出す（ADR-0602 決定13）。
##
## 入力は環境変数で受ける。CI_ACCOUNT_ID（environment の secret）と CI_PR（Pull Request の番号）は
## workflow が渡し、GITHUB_RUN_ID・GITHUB_RUN_ATTEMPT・RUNNER_TEMP・GITHUB_STEP_SUMMARY・GITHUB_OUTPUT と OIDC の
## 要求先は GitHub Actions が渡す。tf-local に渡す値の export は local.mk 冒頭の注記に従う。

CI_PLAN_DIR ?= $(if $(RUNNER_TEMP),$(RUNNER_TEMP),tmp)/ci-plan
# apply を SIGINT で打ち切るまでの時間。apply の job の timeout-minutes より、init・plan と、打ち切りの後に terraform が
# state を書いて lock を外し、全文を置くまでの分だけ短くする。job の timeout が先に来ると全文を置けない。
CI_APPLY_TIMEOUT ?= 20m
TIMEOUT_BIN ?= timeout
# lock を残す入口が、plan の全文に lock を取った後の行が現れるのを待つ周期と上限（tf-local lock-wait）。
CI_HOLD_INTERVAL ?= 200ms
CI_HOLD_LIMIT ?= 10m
CI_ROLE ?= plan
# $(1)=env $(2)=役割。role 名と同じ形にする（tf-local role-arn）。
CI_PROFILE = $(TF_LOCAL_PROJECT)-$(1)-ci-$(2)

# 拒否されるべき role。plan のほかの役割をすべて並べる。
CI_DENIED_ROLES ?= apply apply-identity unlock-identity unlock

# OIDC の token を job の作業ディレクトリに置く。token と応答と $(2) は、呼び出し元の shell が終わるときに消す。
# $(1)=env $(2)=ほかに消すファイル
define CI_OIDC_TOKEN
set -eu -o pipefail; umask 077; d="$(CI_PLAN_DIR)"; mkdir -p "$$d"; token="$$d/oidc-token"; resp="$$d/oidc-response"; \
trap 'rm -f "$$token" "$$resp"$(if $(2), $(2))' EXIT; export token resp CI_ACCOUNT_ID; \
url=$$($(call TF_LOCAL,oidc-url)); \
unset_env=$$($(call TF_LOCAL,env-unset)); \
region=$$($(call TF_LOCAL,region "$(call TF_LOCAL_DIR,$(1),identity)")); \
curl -fsS --retry 3 -H "Authorization: bearer $$ACTIONS_ID_TOKEN_REQUEST_TOKEN" -o "$$resp" "$$url"; \
$(call TF_LOCAL,oidc-token "$$resp") > "$$token"; \
sub=$$($(call TF_LOCAL,oidc-sub "$$token")); echo "OIDC の sub: $$sub"
endef

# $(1)=env $(2)=役割
define CI_AWS_CREDENTIALS
$(call CI_OIDC_TOKEN,$(1),); \
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

# 資格情報はどこにも置かない。assume が通れば、その応答（資格情報）を stdout ごと捨てて失敗する。最初の
# 拒否されなかった役割で終える。
# $(1)=env $(2)=役割の一覧
define CI_ASSUME_DENIED
$(if $(strip $(2)),,$(error CI_DENIED_ROLES が空です)) \
$(call CI_OIDC_TOKEN,$(1),"$$d/assume-stderr"); err="$$d/assume-stderr"; export err; \
for r in $(strip $(2)); do export r; \
role=$$($(call TF_LOCAL,role-arn -project $(TF_LOCAL_PROJECT) -env $(1) -role "$$r" -account "$$CI_ACCOUNT_ID")); \
rc=0; env $$unset_env AWS_EC2_METADATA_DISABLED=true $(AWS_BIN) sts assume-role-with-web-identity --region "$$region" \
--role-arn "$$role" --role-session-name "gha-denied-$$GITHUB_RUN_ID-$$GITHUB_RUN_ATTEMPT" --web-identity-token "file://$$token" \
--output json > /dev/null 2> "$$err" || rc=$$?; export rc; \
$(call TF_LOCAL,assume-denied -role "$$r" -exitcode "$$rc" "$$err"); \
done
endef

# CI_PLAN と CI_APPLY が共有する。root の名前（$(1)-$(2)）は、decl.json の allow の鍵と同じになる。
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

# plan が失敗していれば show も失敗するが、ここでは落とさず、判定は呼び出し側の plan-summary が持つ。
# $(1)=plan の追加の引数
define CI_TERRAFORM_PLAN
rc=0; $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" plan $(1)-input=false -no-color -detailed-exitcode -out="$$tfplan" >> "$$out" 2>&1 || rc=$$?; \
echo "plan の終了コード: $$rc"; export rc; \
$(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" show -json "$$tfplan" > "$$json" 2>> "$$out" || :
endef

# $$json の plan に CI_POLICY_NAMESPACES の Policy を1つずつ掛け、違反か評価の欠けがあれば $(1) を実行してから
# 失敗する。rego のファイルは名前空間から `plan.` を除いた名前である。
# $(1)=失敗する前に実行するコマンド（`;` で終える）
define CI_POLICY
$(if $(strip $(CI_POLICY_NAMESPACES)),,$(error CI_POLICY_NAMESPACES が空です)) \
for ns in $(strip $(CI_POLICY_NAMESPACES)); do export ns; \
$(GO_TOOL) $(CONFTEST_BIN) test --combine --no-color -o json --namespace "$$ns" --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DECL) --data $(POLICY_LAYER_DECL) "$$json" > "$$pol" || :; \
$(call TF_LOCAL,policy-result -namespace "$$ns" -rego "$(POLICY_PLAN_DIR)/$${ns#plan.}.rego" "$$pol") \
|| { $(1)echo "❌ $$ns の Policy を通りません（理由は上の行）" >&2; exit 1; }; \
done
endef

# $(1)=env $(2)=root。plan は lock を取らない（ADR-0604 決定8）。plan が失敗しても、Bucket に書けるなら
# 全文を置いてから失敗する。
define CI_PLAN
$(call CI_PREPARE,$(1),$(2)); export CI_PR GITHUB_RUN_ID GITHUB_RUN_ATTEMPT; \
plan_bucket=$$($(call TF_LOCAL,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -kind tfplan -region "$$region" -identity "$$identity")); \
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

# apply の全文を置く Bucket の種類（tfplan か tfapply）。割当は .github/workflows/README.md の「apply の job」。$(1)=root
CI_APPLY_OUTPUT_KIND = $(if $(filter identity,$(1)),tfplan,$(if $(filter workload,$(1)),tfapply,$(error root が identity か workload ではありません: $(1))))

# $(1)=env $(2)=root。apply する commit で plan し直し、Policy を通った plan だけを apply する（ADR-0601 決定6、
# ADR-0604 決定11）。plan は lock を取る —— ADR-0604 決定8 の plan は plan の job の plan である。
# 変更の無い plan も apply し、経路と lock の取得を毎回同じにする。
# Bucket の名前と key を得た後は、plan・Policy・apply のどこで失敗しても全文を置いてから失敗する。
define CI_APPLY
$(call CI_PREPARE,$(1),$(2),failure); export GITHUB_RUN_ID GITHUB_RUN_ATTEMPT; \
plan_bucket=$$($(call TF_LOCAL,bucket -project $(TF_LOCAL_PROJECT) -env $(1) -kind $(call CI_APPLY_OUTPUT_KIND,$(2)) -region "$$region" -identity "$$identity")); \
key=$$($(call TF_LOCAL,apply-key -run "$$GITHUB_RUN_ID" -attempt "$$GITHUB_RUN_ATTEMPT" -root "$$root")); \
upload() { prev=$$stage; test "$$stage" != applied || stage=upload; sha=$$($(call TF_LOCAL,sha256 "$$out")); echo "apply の全文: key=$$key sha256=$$sha"; \
version=$$($(TF_LOCAL_ENV) $(AWS_BIN) s3api put-object --region "$$region" --bucket "$$plan_bucket" --key "$$key" --body "$$out" \
--checksum-sha256 "$$sha" --expected-bucket-owner "$$account" --query VersionId --output text); \
echo "apply の全文の版 ID: $$version"; stage=$$prev; }; \
stage=plan; $(call CI_TERRAFORM_PLAN,); \
stage=$$($(call TF_LOCAL,failure-kind -step plan -exitcode "$$rc" "$$out")) || stage=plan; \
summary=$$($(call TF_LOCAL,plan-summary -exitcode "$$rc" "$$json")) || { upload; echo "❌ 全文は上の key にあります" >&2; exit 1; }; echo "$$summary"; \
stage=policy; $(call CI_POLICY,upload; ); \
stage=apply; arc=0; $(TIMEOUT_BIN) -s INT $(CI_APPLY_TIMEOUT) $(TF_LOCAL_ENV) $(TF_BIN) -chdir="$$dir" apply -input=false -no-color "$$tfplan" >> "$$out" 2>&1 || arc=$$?; export arc; echo "apply の終了コード: $$arc"; \
stage=$$($(call TF_LOCAL,failure-kind -step apply -exitcode "$$arc" "$$out")) || stage=apply; \
upload; \
result=$$($(call TF_LOCAL,apply-result -exitcode "$$arc")) || { echo "❌ 全文は上の key にあります" >&2; exit 1; }; echo "$$result"; \
test -z "$${GITHUB_STEP_SUMMARY:-}" || printf '### %s\n\n%s\n\napply が終わりました\n\n- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n' "$$root" "$$summary" "$$key" "$$sha" "$$version" >> "$$GITHUB_STEP_SUMMARY"; \
test -z "$${GITHUB_OUTPUT:-}" || printf 'plan=%s\n' "$$summary" >> "$$GITHUB_OUTPUT"
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
ci-aws-credentials: SHELL := /bin/bash
ci-aws-credentials:
	@$(if $(CI_ENV),,$(error CI_ENV が空です))$(call CI_AWS_CREDENTIALS,$(CI_ENV),$(CI_ROLE))

.PHONY: ci-assume-denied ## [CI] CI_ENV の plan の job の OIDC token で、CI_DENIED_ROLES の role の assume がすべて拒否されることを確かめる
ci-assume-denied: SHELL := /bin/bash
ci-assume-denied:
	@$(if $(CI_ENV),,$(error CI_ENV が空です))$(call CI_ASSUME_DENIED,$(CI_ENV),$(CI_DENIED_ROLES))

.PHONY: ci-plan-admin-identity ## [CI] 管理アカウントの identity root を plan し、全文を plan の出力の Bucket に置く
ci-plan-admin-identity: SHELL := /bin/bash
ci-plan-admin-identity: CI_POLICY_NAMESPACES := plan.stateful_protection plan.kms_key_policy plan.resource_policy_exclusive
ci-plan-admin-identity: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-plan-admin-identity:
	@$(call CI_PLAN,admin,identity)

.PHONY: ci-apply-admin-identity ## [CI] 管理アカウントの identity root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-identity: SHELL := /bin/bash
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
ci-plan-admin-workload: SHELL := /bin/bash
ci-plan-admin-workload: CI_POLICY_NAMESPACES := plan.stateful_protection
ci-plan-admin-workload: TF_LOCAL_PROFILE := $(call CI_PROFILE,admin,plan)
ci-plan-admin-workload:
	@$(call CI_PLAN,admin,workload)

.PHONY: ci-apply-admin-workload ## [CI] 管理アカウントの workload root を plan し直し、Policy を通った plan を apply する
ci-apply-admin-workload: SHELL := /bin/bash
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
