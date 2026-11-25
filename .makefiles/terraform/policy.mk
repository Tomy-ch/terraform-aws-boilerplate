## Policy Test（ADR-0402、ADR-0501 決定4・8）
##
## 入力の作り方は conftest/plan/README.md の「入力」。terraform はホストで、conftest はツールランナーで実行する。
## Rego の形の検査、Rego のテストの件数の突き合わせ、reference の root ごとの plan の取得と取り出しは
## scripts/tf-policy が、非推奨の書ける引数の取り出しは scripts/provider-schema が、適用の結果の評価の数の
## 突き合わせは tf-local policy-result が持つ。ここが持つのは段を起動する順だけである（ADR-0702 決定24・25）。
## Conftest の終了コードは捨て、合否は結果を読む段が決める。
##
## init は gates.mk の tf-init を共有するので、tf-validate / tf-test と1回の make で一緒に呼ぶ。

POLICY_PLAN_DIR = conftest/plan
POLICY_PLAN_DECL = conftest/plan/decl.json
# ADR-0402 決定17 の引数の一部は、ADR-0102 決定18 の宣言（層の検査の宣言）から導く（conftest/plan/README.md の「入力」）。
POLICY_LAYER_DECL = conftest/hcl/layer/decl.json
CONFTEST_BIN ?= conftest
# conftest はツールランナーの中で走り、リポジトリは /app に mount される。パスは相対で渡す。tf-policy plan の -out は
# リポジトリ直下の tmp/ の下に限るので、走査の基点（TF_BASE）とは切り離す。
TF_POLICY_INPUT = tmp/tf-policy
# canary は tf-policy plan が本走査の前に同じ起動の中で走らせる（ADR-0702 決定25）。実物の terraform・Conftest・
# mock provider で段を通しで走らせ、terraform と Conftest はホストの PATH のものを起動する。provider-schema は
# ビルドした置き場を PATH の先頭に置いて起動させる。aws の fixture は state-backend の reference の版の AWS Provider を使う。
TF_POLICY_CANARY_DIR = tmp/tf-policy-canary
TF_POLICY_CANARY_FLAGS = -canary-data "$(abspath $(TF_POLICY_CANARY_DIR))/data" -canary-work "$(TF_POLICY_CANARY_DIR)/work" \
	-policy $(POLICY_PLAN_DIR) -decl $(POLICY_PLAN_DECL) -layer $(POLICY_LAYER_DECL) -reference usecases/state-backend/reference

.PHONY: tf-policy ## Policy Test を実行（Rego のテストと、reference の plan への適用）
tf-policy: tf-init
	@$(call RUN_SCRIPT,tf-policy,rego-check -policy $(POLICY_PLAN_DIR))
	@mkdir -p $(TF_POLICY_INPUT)
	-@$(GO_TOOL) $(CONFTEST_BIN) verify --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DECL) --data $(POLICY_LAYER_DECL) -o json > $(TF_POLICY_INPUT)/verify.json
	@$(call RUN_SCRIPT,tf-policy,verify-result -policy $(POLICY_PLAN_DIR) -report $(TF_POLICY_INPUT)/verify.json)
	@$(call BUILD_SCRIPT_HOST,provider-schema)
	@PATH="$(CURDIR)/$(SCRIPT_BIN):$$PATH" $(call RUN_SCRIPT_HOST,tf-policy,plan -base "$(TF_BASE)" -data "$(TF_GATES_DATA)" -out "$(TF_POLICY_INPUT)/run" $(TF_POLICY_CANARY_FLAGS))
	@$(call RUN_SCRIPT,tf-policy,extract -out "$(TF_POLICY_INPUT)/run" -decl $(POLICY_PLAN_DECL))
	@$(call RUN_SCRIPT,provider-schema,plan-deprecated -out "$(TF_POLICY_INPUT)/provider-schema.json" -list "$(TF_POLICY_INPUT)/run/plans.txt")
	-@$(GO_TOOL) $(CONFTEST_BIN) test --combine --all-namespaces --no-color -o json --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DECL) --data $(POLICY_LAYER_DECL) \
		--data "$(TF_POLICY_INPUT)/provider-schema.json" "$(TF_POLICY_INPUT)/run/inputs" > "$(TF_POLICY_INPUT)/result.json"
	@$(call RUN_SCRIPT,tf-local,policy-result -policy $(POLICY_PLAN_DIR) $(TF_POLICY_INPUT)/result.json)
