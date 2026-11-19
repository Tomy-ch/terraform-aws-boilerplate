## Terraform 側の検査（ADR-0501 決定4・28）
##
## 実行経路は .makefiles/README.md の表が持つ（ADR-0503 決定4・5）。
## terraform を実行する経路は運用機構（scripts/）を経由しない（ADR-0702 決定3）。tf-lint だけは先に
## tool-cooldown-check を呼ぶ。
##
## tf-validate は、terraform validate のエラーに加えて警告でも落とす。validate は警告を出しても
## 0 で終わるためである。判定は scripts/tf-validate-result が持つ。
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
AWS_ENV_UNSET = $$(env | sed -n 's/^\(AWS_[A-Za-z0-9_]*\)=.*/-u \1/p')
TF_ENV = env $(AWS_ENV_UNSET) \
	AWS_SHARED_CREDENTIALS_FILE=/dev/null AWS_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1
# 検査の init は root の .terraform を使わない。手元で実 AWS の backend に init した root
# （make init-admin-identity など）が .terraform に backend の設定を残すと、-backend=false の init が
# その backend の資格情報を探して落ちる。
# terraform は相対の TF_DATA_DIR を -chdir の先から解決するので、絶対パスで渡す。
TF_GATES_DATA = $(abspath $(TF_BASE))/tmp/tf-gates-data
# validate の結果は tf-validate-result が読む（ADR-0702 決定24）。container の経路でも読めるよう、
# 直下からの相対パスで置く。一覧（manifest.tsv）は毎回書き直し、そこに載るファイルだけが読まれる。
TF_VALIDATE_OUT = $(TF_BASE)/tmp/tf-validate
# 警告の抑止の宣言（ADR-0501 決定13）。fixture は自分の宣言を渡す。
TF_VALIDATE_IGNORE ?= .tf-validate-ignore.yaml
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

# パスを1つの名前へ写す。別のディレクトリが同じ名前にならないよう、_ を先にエスケープする。
define TF_DATA_DIR_OF
data_dir() { printf '%s/%s' "$(TF_GATES_DATA)" "$$(printf '%s' "$${1#$(TF_BASE)/}" | sed -e 's/_/_u/g' -e 's|/|__|g')"; }
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
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); $(TF_IS_ROOT); $(TF_DATA_DIR_OF); \
	mkdir -p "$$TF_PLUGIN_CACHE_DIR"; failed=""; \
	for d in $$dirs; do \
		lock=""; if is_root "$$d"; then lock="-lockfile=readonly"; fi; \
		TF_DATA_DIR="$$(data_dir "$$d")" $(TF) -chdir="$$d" init -backend=false -input=false $$lock || failed="$$failed $$d"; \
	done; \
	$(TF_REPORT_FAILED)

.PHONY: tf-validate ## Terraform の構成を検査し、エラーと抑止されない警告で落とす（provider の取得を伴う）
tf-validate: tf-init
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); $(TF_DATA_DIR_OF); \
	out="$(TF_VALIDATE_OUT)"; mkdir -p "$$out"; : > "$$out/manifest.tsv"; i=0; \
	for d in $$dirs; do \
		i=$$((i + 1)); rc=0; \
		TF_DATA_DIR="$$(data_dir "$$d")" $(TF) -chdir="$$d" validate -json > "$$out/$$i.json" 2> "$$out/$$i.err" || rc=$$?; \
		printf '%s\t%s\t%s\t%s\n' "$$d" "$$rc" "$$out/$$i.json" "$$out/$$i.err" >> "$$out/manifest.tsv"; \
	done; \
	$(call RUN_SCRIPT,tf-validate-result,-base $(TF_BASE) -manifest $(TF_VALIDATE_OUT)/manifest.tsv -ignore $(TF_VALIDATE_IGNORE))

.PHONY: tf-test ## terraform test を実行（*.tftest.hcl を持つディレクトリ）
tf-test: tf-init
	@set -eu; dirs=$(TF_LIST_DIRS); $(TF_REQUIRE_DIRS); $(TF_DATA_DIR_OF); tested=""; failed=""; \
	has_tests() { for f in "$$1"/*.tftest.hcl "$$1"/tests/*.tftest.hcl; do if [ -e "$$f" ]; then return 0; fi; done; return 1; }; \
	for d in $$dirs; do \
		if has_tests "$$d"; then \
			tested="$$tested $$d"; TF_DATA_DIR="$$(data_dir "$$d")" $(TF) -chdir="$$d" test || failed="$$failed $$d"; \
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

# tf-gates-test の fixture は provider を持たず、警告を Terraform 本体（非推奨の variable）に出させる。
# 実物の AWS Provider の版は state-backend の reference の lock に揃え、plugin cache に無ければ取得する。
# 抑止の宣言の detail は、この canary の出力から取る（版で文言が変わるため）。
TF_VALIDATE_CANARY_DIR = tmp/tf-validate-canary

define TF_VALIDATE_AWS_FIXTURE
mkdir -p "$$1/usecases/x/reference"; \
cp usecases/state-backend/reference/versions.tf usecases/state-backend/reference/.terraform.lock.hcl "$$1/usecases/x/reference/"; \
printf 'resource "aws_iam_role" "this" {\n  name               = "canary"\n  assume_role_policy = "{}"\n%b}\n' "$$2" > "$$1/usecases/x/reference/main.tf"
endef

.PHONY: tf-validate-canary ## tf-validate が AWS Provider の非推奨の引数で落ち、宣言に当たる警告と非推奨の無い構成で通ることを検査
tf-validate-canary:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(TF_VALIDATE_CANARY_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	fixture() { $(TF_VALIDATE_AWS_FIXTURE); }; \
	printf 'warnings: []\n' > "$$root/ignore-empty.yaml"; \
	expect() { \
		want="$$1"; case_name="$$2"; base="$$3"; ignore="$$4"; pattern="$$5"; \
		out=$$($(MAKE) -s --no-print-directory tf-validate TF_BASE="$$base" TF_VALIDATE_IGNORE="$$ignore" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && { [ "$$rc" -ne 0 ] || ! printf '%s' "$$out" | grep -qF -- "$$pattern"; }; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && { [ "$$rc" -eq 0 ] || ! printf '%s' "$$out" | grep -qF -- "$$pattern"; }; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	fixture "$$root/clean" ''; \
	expect pass clean "$$root/clean" "$$root/ignore-empty.yaml" "1 ディレクトリを validate し"; \
	fixture "$$root/deprecated" '  managed_policy_arns = []\n'; \
	expect fail deprecated "$$root/deprecated" "$$root/ignore-empty.yaml" \
		'usecases/x/reference/main.tf:4:25: [warning] Argument is deprecated（resource "aws_iam_role" "this"）'; \
	detail=$$(jq -er '[.diagnostics[] | select(.severity == "warning")] | if length == 1 then .[0].detail else error("警告がちょうど1件ではありません") end' \
		"$$root/deprecated/tmp/tf-validate/1.json") || { failed="$$failed detail"; detail=""; }; \
	jq -n --arg d "$$detail" '{warnings: [{summary: "Argument is deprecated", path: "usecases/x/reference/main.tf", block: "resource \"aws_iam_role\" \"this\"", detail: $$d, occurrences: 1, reason: "canary", withdrawal: "canary"}]}' \
		> "$$root/ignore-aws.yaml"; \
	expect pass ignored "$$root/deprecated" "$$root/ignore-aws.yaml" "抑止 1 件"; \
	if [ -n "$$failed" ]; then echo "❌ tf-validate の canary が期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ tf-validate は AWS Provider の非推奨の引数で落ち、宣言に当たる警告と非推奨の無い構成で通る"
