## Policy Test（ADR-0402、ADR-0501 決定4・8）
##
## 入力の作り方は conftest/plan/README.md の「入力」。terraform はホストで、opa と conftest はツールランナーで実行する。
## Rego の形の検査、opa と Conftest の内蔵の OPA の版の突き合わせ、Rego のテストの件数の突き合わせ、reference の
## root ごとの plan の取得と取り出しは scripts/tf-policy が、非推奨の書ける引数の取り出しは scripts/provider-schema が、
## 適用の結果の評価の数の突き合わせは tf-local policy-result が持つ。ここが持つのは段を起動する順だけである
## （ADR-0702 決定24・25）。opa と Conftest の終了コードは捨て、合否は結果を読む段が決める。
## Rego のテストは opa test で走らせる。conftest verify は、ケースごとの上限（OPA の既定の 5s）を選べない。
##
## init は gates.mk の tf-init を共有するので、tf-validate / tf-test と1回の make で一緒に呼ぶ。

POLICY_PLAN_DIR = conftest/plan
POLICY_PLAN_DECL = conftest/plan/decl.json
# ADR-0402 決定17 の引数の一部は、ADR-0102 決定18 の宣言（層の検査の宣言）から導く（conftest/plan/README.md の「入力」）。
POLICY_LAYER_DECL = conftest/hcl/layer/decl.json
CONFTEST_BIN ?= conftest
OPA_BIN ?= opa
# Rego のテストのケースごとの上限（opa test --timeout）。暴走した評価を、job の打ち切りより前に、どのテストかを
# 名指して落とすための値で、最も重いケースより1桁以上大きく置く（余裕は verify-result が出す遅いケースの時間で読む）。
# 上げても違反のテストが通るようにはならない。
POLICY_TEST_TIMEOUT = 60s
# opa と conftest はツールランナーの中で走り、リポジトリは /app に mount される。パスは相対で渡す。tf-policy plan の -out は
# リポジトリ直下の tmp/ の下に限るので、走査の基点（TF_BASE）とは切り離す。
TF_POLICY_INPUT = tmp/tf-policy
# canary は tf-policy plan が本走査の前に同じ起動の中で走らせる（ADR-0702 決定25）。実物の terraform・opa・Conftest・
# mock provider で段を通しで走らせ、terraform・opa・Conftest はホストの PATH のものを起動する。provider-schema は
# ビルドした置き場を PATH の先頭に置いて起動させる。aws の fixture は state-backend の reference の版の AWS Provider を使う。
TF_POLICY_CANARY_DIR = tmp/tf-policy-canary
TF_POLICY_CANARY_FLAGS = -canary-data "$(abspath $(TF_POLICY_CANARY_DIR))/data" -canary-work "$(TF_POLICY_CANARY_DIR)/work" \
	-policy $(POLICY_PLAN_DIR) -decl $(POLICY_PLAN_DECL) -layer $(POLICY_LAYER_DECL) -reference usecases/state-backend/reference

.PHONY: tf-policy ## Policy Test を実行（Rego のテストと、reference の plan への適用）
tf-policy: tf-init
	@$(call RUN_SCRIPT,tf-policy,rego-check -policy $(POLICY_PLAN_DIR))
	@mkdir -p $(TF_POLICY_INPUT)
	-@$(GO_TOOL) $(CONFTEST_BIN) --version > $(TF_POLICY_INPUT)/conftest-version.txt
	-@$(GO_TOOL) $(OPA_BIN) version > $(TF_POLICY_INPUT)/opa-version.txt
	@$(call RUN_SCRIPT,tf-policy,opa-parity -conftest $(TF_POLICY_INPUT)/conftest-version.txt -opa $(TF_POLICY_INPUT)/opa-version.txt)
	-@$(GO_TOOL) $(OPA_BIN) test $(POLICY_PLAN_DIR) $(POLICY_LAYER_DECL) -f json --timeout $(POLICY_TEST_TIMEOUT) > $(TF_POLICY_INPUT)/verify.json
	@$(call RUN_SCRIPT,tf-policy,verify-result -policy $(POLICY_PLAN_DIR) -report $(TF_POLICY_INPUT)/verify.json)
	@$(call BUILD_SCRIPT_HOST,provider-schema)
	@PATH="$(CURDIR)/$(SCRIPT_BIN):$$PATH" $(call RUN_SCRIPT_HOST,tf-policy,plan -base "$(TF_BASE)" -data "$(TF_GATES_DATA)" -out "$(TF_POLICY_INPUT)/run" $(TF_POLICY_CANARY_FLAGS))
	@$(call RUN_SCRIPT,tf-policy,extract -out "$(TF_POLICY_INPUT)/run" -decl $(POLICY_PLAN_DECL))
	@$(call RUN_SCRIPT,provider-schema,plan-deprecated -out "$(TF_POLICY_INPUT)/provider-schema.json" -list "$(TF_POLICY_INPUT)/run/plans.txt")
	-@$(GO_TOOL) $(CONFTEST_BIN) test --combine --all-namespaces --no-color -o json --policy $(POLICY_PLAN_DIR) --data $(POLICY_PLAN_DECL) --data $(POLICY_LAYER_DECL) \
		--data "$(TF_POLICY_INPUT)/provider-schema.json" "$(TF_POLICY_INPUT)/run/inputs" > "$(TF_POLICY_INPUT)/result.json"
	@$(call RUN_SCRIPT,tf-local,policy-result -policy $(POLICY_PLAN_DIR) $(TF_POLICY_INPUT)/result.json)
