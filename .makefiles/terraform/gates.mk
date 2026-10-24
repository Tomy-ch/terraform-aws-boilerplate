## Terraform 側の検査（ADR-0501 決定4・28）
##
## 実行経路は .makefiles/README.md の表が持つ（ADR-0503 決定4・5）。
## terraform を実行する経路は運用機構（scripts/）を経由しない（ADR-0702 決定3）。tf-lint だけは先に
## tool-cooldown-check を呼ぶ。
##
## 走査の対象が0件なら失敗する。Terraform コードの無いツリーで pre-commit が通るのは、
## .lefthook.yaml が tf-no-targets で skip を明示するからであり、ここが成功を返すからではない。
##
## init は provider を取得するのでネットワークを要する（mock_provider も provider の schema を要る）。
## root（env/<env>、env/<env>/identity、usecases/<uc>/reference）では lock を readonly にし、
## 追跡している .terraform.lock.hcl を書き換えない（ADR-0205 決定9）。lock の更新は人が root で行う。
##
## tf-validate と tf-test は tf-init を共有するので、1回の make で一緒に呼ぶ。別々の make を
## 並行させると、同じディレクトリの init と provider のキャッシュが競合する。

TF_BASE ?= .
TF_SCAN_DIRS = $(TF_BASE)/usecases $(TF_BASE)/modules $(TF_BASE)/env
export TF_PLUGIN_CACHE_DIR ?= $(CURDIR)/tmp/terraform-plugin-cache
# plugin の置き場の階層は OS / arch を含まないので、経路ごとに分ける。
TFLINT_PLUGIN_DIR_REL = tmp/tflint-plugins/$(RUNNER_MODE)
# 資格情報を持つ手元でも、持たない CI と同じ条件で走らせる（ADR-0401 決定6）。SDK が資格情報を
# 読む環境変数は名前を列挙しきれないので、AWS_ で始まるものをすべて外してから与える。
TF_ENV = env $$(env | sed -n 's/^\(AWS_[A-Za-z0-9_]*\)=.*/-u \1/p') \
	AWS_SHARED_CREDENTIALS_FILE=/dev/null AWS_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1
TF_BIN ?= terraform
TFLINT_BIN ?= tflint
TF = $(TF_ENV) $(TF_BIN)

# tests/ の下の setup module は列挙しない。親の terraform test が読む。
# find の失敗はパイプの後ろで消えるので、出力を受けてから終了コードを見る。
define TF_LIST_DIRS
$$(scan=""; for d in $(TF_SCAN_DIRS); do if [ -d "$$d" ]; then scan="$$scan $$d"; fi; done; \
	if [ -n "$$scan" ]; then \
		found=$$(find $$scan -name '*.tf' -not -path '*/.terraform/*' -not -path '*/tests/*') \
			|| { echo "❌ Terraform の走査に失敗しました（$$scan）" >&2; exit 1; }; \
		printf '%s\n' "$$found" | sed -e '/^$$/d' -e 's|/[^/]*$$||' | sort -u; \
	fi)
endef

define TF_REQUIRE_DIRS
if [ -z "$$dirs" ]; then \
	echo "❌ Terraform の対象ディレクトリが0件です（走査: $(TF_SCAN_DIRS)）"; exit 1; \
fi
endef

# root かどうかは、TF_BASE からの相対パスの形で決まる（ADR-0604 決定5、ADR-0102 決定12）。
define TF_IS_ROOT
is_root() ( \
	IFS=/; set -- $${1#$(TF_BASE)/}; \
	case "$$#:$$1:$${3:-}" in "2:env:"|"3:env:identity"|"3:usecases:reference") exit 0 ;; esac; \
	exit 1 \
)
endef

define TF_REPORT_FAILED
if [ -n "$$failed" ]; then echo "❌ $@ が落ちたディレクトリ:$$failed"; exit 1; fi
endef

.PHONY: tf-fmt ## Terraform の書式を整える
tf-fmt:
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); \
	for d in $(TF_SCAN_DIRS); do if [ -d "$$d" ]; then $(TF) fmt -recursive "$$d"; fi; done

.PHONY: tf-fmt-check ## Terraform の書式を検査
tf-fmt-check:
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); \
	failed=""; \
	for d in $(TF_SCAN_DIRS); do \
		if [ -d "$$d" ]; then $(TF) fmt -check -diff -recursive "$$d" || failed="$$failed $$d"; fi; \
	done; \
	$(TF_REPORT_FAILED)

.PHONY: tf-init
tf-init:
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); $(TF_IS_ROOT); \
	mkdir -p "$$TF_PLUGIN_CACHE_DIR"; failed=""; \
	for d in $$dirs; do \
		lock=""; if is_root "$$d"; then lock="-lockfile=readonly"; fi; \
		$(TF) -chdir="$$d" init -backend=false -input=false $$lock || failed="$$failed $$d"; \
	done; \
	$(TF_REPORT_FAILED)

.PHONY: tf-validate ## Terraform の構成を検査（provider の取得を伴う）
tf-validate: tf-init
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); failed=""; \
	for d in $$dirs; do $(TF) -chdir="$$d" validate || failed="$$failed $$d"; done; \
	$(TF_REPORT_FAILED)

.PHONY: tf-test ## terraform test を実行（*.tftest.hcl を持つディレクトリ）
tf-test: tf-init
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); tested=""; failed=""; \
	has_tests() { for f in "$$1"/*.tftest.hcl "$$1"/tests/*.tftest.hcl; do if [ -e "$$f" ]; then return 0; fi; done; return 1; }; \
	for d in $$dirs; do \
		if has_tests "$$d"; then \
			tested="$$tested $$d"; $(TF) -chdir="$$d" test || failed="$$failed $$d"; \
		fi; \
	done; \
	if [ -z "$$tested" ]; then echo "❌ *.tftest.hcl を持つディレクトリが0件です（走査: $(TF_SCAN_DIRS)）"; exit 1; fi; \
	$(TF_REPORT_FAILED)

.PHONY: tf-lint ## TFLint で Terraform を検査（初回は ruleset の取得を伴う）
tf-lint:
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); \
	$(MAKE) -s --no-print-directory tool-cooldown-check; \
	$(TF_ENV) $(GO_TOOL) sh -c 'set -eu; export TFLINT_PLUGIN_DIR="$$PWD/$(TFLINT_PLUGIN_DIR_REL)"; \
		mkdir -p "$$TFLINT_PLUGIN_DIR"; $(TFLINT_BIN) --init --config="$$PWD/.tflint.hcl"; failed=""; \
		for d in "$$@"; do $(TFLINT_BIN) --chdir="$$d" --config="$$PWD/.tflint.hcl" || failed="$$failed $$d"; done; \
		if [ -n "$$failed" ]; then echo "❌ tf-lint が落ちたディレクトリ:$$failed"; exit 1; fi' _ $$dirs

# .lefthook.yaml の skip 条件。
.PHONY: tf-no-targets
tf-no-targets:
	@dirs=$(TF_LIST_DIRS) && test -z "$$dirs"
