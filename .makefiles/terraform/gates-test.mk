## Terraform 側の検査（gates.mk）そのものの検査
##
## fixture は provider を持たない構成で毎回 tmp/ に作り、TF_BASE で差し替える。init がネットワークを
## 要さず、本番の走査（usecases/・modules/・env/）にも混ざらない。tf-lint は ruleset の取得を伴うので、
## 0件以外のケースでは TFLINT_BIN を偽の tflint に差し替え、失敗の集約だけを見る。

TF_GATES_TEST_DIR = tmp/tf-gates-test

# 正しい構成。check や plan の警告ではなく run の assert で判定させる。
define TF_FIXTURE_OK
printf 'variable "name" {\n  type    = string\n  default = "a"\n}\n\noutput "name" {\n  value = var.name\n}\n' > "$$1/main.tf"; \
mkdir -p "$$1/tests"; \
printf 'run "ok" {\n  command = plan\n\n  assert {\n    condition     = output.name == "a"\n    error_message = "name"\n  }\n}\n' > "$$1/tests/ok.tftest.hcl"
endef

# 要らない provider を載せた lock。`-lockfile=readonly` の init だけが、これをネットワーク無しで落とす。
define TF_FIXTURE_STALE_LOCK
printf 'provider "registry.terraform.io/hashicorp/null" {\n  version = "3.2.4"\n  hashes = [\n    "h1:stale",\n  ]\n}\n' > "$$1/.terraform.lock.hcl"
endef

.PHONY: tf-gates-test ## Terraform 側の検査が0件と壊れた入力で落ちることを検査
tf-gates-test:
	@set -u; root="$(TF_GATES_TEST_DIR)"; rm -rf "$$root"; failed=""; \
	ok() { mkdir -p "$$1"; $(TF_FIXTURE_OK); }; \
	stale_lock() { $(TF_FIXTURE_STALE_LOCK); }; \
	expect() { \
		want="$$1"; case_name="$$2"; base="$$3"; target="$$4"; pattern="$${5:-}"; \
		out=$$($(MAKE) -s --no-print-directory "$$target" TF_BASE="$$base" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && [ "$$rc" -ne 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && [ "$$rc" -eq 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ -n "$$pattern" ] && ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	failed_line() { printf '❌ %s が落ちたディレクトリ: %s' "$$1" "$$2"; }; \
	mkdir -p "$$root/empty"; \
	for t in tf-fmt-check tf-validate tf-test tf-lint; do \
		expect fail "empty:$$t" "$$root/empty" "$$t" "対象ディレクトリが0件"; \
	done; \
	expect pass "empty:tf-no-targets" "$$root/empty" tf-no-targets; \
	mkdir -p "$$root/setup-only/modules/a/tests/setup"; printf 'variable "x" {\n  type = string\n}\n' > "$$root/setup-only/modules/a/tests/setup/main.tf"; \
	expect pass "setup-only:tf-no-targets" "$$root/setup-only" tf-no-targets; \
	mkdir -p "$$root/no-tests/modules/a"; printf 'variable "x" {\n  type = string\n}\n' > "$$root/no-tests/modules/a/main.tf"; \
	expect fail "no-tests:tf-test" "$$root/no-tests" tf-test "*.tftest.hcl を持つディレクトリが0件"; \
	expect fail "no-tests:tf-no-targets" "$$root/no-tests" tf-no-targets; \
	ok "$$root/ok/modules/a"; ok "$$root/ok/usecases/b"; ok "$$root/ok/env/dev"; \
	for t in tf-fmt-check tf-validate tf-test; do expect pass "ok:$$t" "$$root/ok" "$$t"; done; \
	ok "$$root/direct-test/modules/a"; mv "$$root/direct-test/modules/a/tests/ok.tftest.hcl" "$$root/direct-test/modules/a/"; \
	rmdir "$$root/direct-test/modules/a/tests"; \
	expect pass "direct-test:tf-test" "$$root/direct-test" tf-test; \
	for pos in first last; do \
		for t in fmt validate test; do ok "$$root/$$pos-$$t/modules/a"; ok "$$root/$$pos-$$t/usecases/b"; done; \
		if [ "$$pos" = first ]; then dir=modules/a; scan=modules; else dir=usecases/b; scan=usecases; fi; \
		printf 'variable   "extra"   {\n}\n' >> "$$root/$$pos-fmt/$$dir/main.tf"; \
		printf 'output "missing" {\n  value = var.missing\n}\n' >> "$$root/$$pos-validate/$$dir/main.tf"; \
		sed 's/== "a"/== "b"/' "$$root/$$pos-test/$$dir/tests/ok.tftest.hcl" > "$$root/$$pos-test/$$dir/tests/ok.new"; \
		mv "$$root/$$pos-test/$$dir/tests/ok.new" "$$root/$$pos-test/$$dir/tests/ok.tftest.hcl"; \
		expect fail "broken-$$pos:tf-fmt-check" "$$root/$$pos-fmt" tf-fmt-check "$$(failed_line tf-fmt-check "$$root/$$pos-fmt/$$scan")"; \
		expect fail "broken-$$pos:tf-validate" "$$root/$$pos-validate" tf-validate "$$(failed_line tf-validate "$$root/$$pos-validate/$$dir")"; \
		expect fail "broken-$$pos:tf-test" "$$root/$$pos-test" tf-test "$$(failed_line tf-test "$$root/$$pos-test/$$dir")"; \
	done; \
	for t in validate test; do ok "$$root/both-$$t/modules/a"; ok "$$root/both-$$t/usecases/b"; done; \
	for d in modules/a usecases/b; do \
		printf 'output "missing" {\n  value = var.missing\n}\n' >> "$$root/both-validate/$$d/main.tf"; \
		sed 's/== "a"/== "b"/' "$$root/both-test/$$d/tests/ok.tftest.hcl" > "$$root/both-test/$$d/tests/ok.new"; \
		mv "$$root/both-test/$$d/tests/ok.new" "$$root/both-test/$$d/tests/ok.tftest.hcl"; \
	done; \
	expect fail "broken-both:tf-validate" "$$root/both-validate" tf-validate "$$(failed_line tf-validate "$$root/both-validate/modules/a $$root/both-validate/usecases/b")"; \
	expect fail "broken-both:tf-test" "$$root/both-test" tf-test "$$(failed_line tf-test "$$root/both-test/modules/a $$root/both-test/usecases/b")"; \
	ok "$$root/init/modules/a"; printf 'terraform {\n  required_version = "< 0.1.0"\n}\n' > "$$root/init/modules/a/versions.tf"; \
	expect fail "broken:tf-init" "$$root/init" tf-validate "$$(failed_line tf-init "$$root/init/modules/a")"; \
	for d in env/dev env/dev/identity usecases/x/reference; do \
		rm -rf "$$root/root"; ok "$$root/root/$$d"; stale_lock "$$root/root/$$d"; cp "$$root/root/$$d/.terraform.lock.hcl" "$$root/lock.orig"; \
		expect fail "root-readonly:$$d" "$$root/root" tf-validate "$$(failed_line tf-init "$$root/root/$$d")"; \
		if ! cmp -s "$$root/lock.orig" "$$root/root/$$d/.terraform.lock.hcl"; then failed="$$failed root-lock-unchanged:$$d"; fi; \
	done; \
	for d in modules/a usecases/x env/dev/other; do \
		rm -rf "$$root/nonroot"; ok "$$root/nonroot/$$d"; stale_lock "$$root/nonroot/$$d"; \
		expect pass "non-root-writable:$$d" "$$root/nonroot" tf-validate; \
	done; \
	mkdir -p "$$root/bin"; \
	printf '#!/bin/sh\nleft=$$(env | grep "^AWS_" | grep -vE "^AWS_(SHARED_CREDENTIALS_FILE|CONFIG_FILE|EC2_METADATA_DISABLED)=")\nif [ -n "$$left" ]; then echo "AWS の環境変数が残っています: $$left"; exit 1; fi\n' > "$$root/bin/terraform"; \
	chmod +x "$$root/bin/terraform"; \
	out=$$(AWS_ACCESS_KEY_ID=x AWS_CONTAINER_CREDENTIALS_FULL_URI=http://127.0.0.1/ AWS_PROFILE=p \
		$(MAKE) -s --no-print-directory tf-validate TF_BASE="$$root/ok" TF_BIN="$$PWD/$$root/bin/terraform" 2>&1) \
		|| { failed="$$failed aws-env-stripped"; echo "$$out"; }; \
	printf '#!/bin/sh\nfor a in "$$@"; do case "$$a" in --chdir=*) d="$${a#--chdir=}";; esac; done\nif [ -n "$${d:-}" ] && [ -e "$$d/lint-bad" ]; then echo "lint-bad: $$d"; exit 2; fi\n' > "$$root/bin/tflint"; \
	chmod +x "$$root/bin/tflint"; \
	lint() { \
		want="$$1"; case_name="$$2"; base="$$3"; pattern="$${4:-}"; \
		out=$$($(MAKE) -s --no-print-directory tf-lint TF_BASE="$$base" TFLINT_BIN="$$root/bin/tflint" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && [ "$$rc" -ne 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && [ "$$rc" -eq 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ -n "$$pattern" ] && ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	lint pass "ok:tf-lint" "$$root/ok"; \
	for pos in first last both; do \
		ok "$$root/lint-$$pos/modules/a"; ok "$$root/lint-$$pos/usecases/b"; \
		case "$$pos" in first) bad=modules/a;; last) bad=usecases/b;; both) bad="modules/a usecases/b";; esac; \
		want=""; for d in $$bad; do touch "$$root/lint-$$pos/$$d/lint-bad"; want="$$want $$root/lint-$$pos/$$d"; done; \
		lint fail "broken-$$pos:tf-lint" "$$root/lint-$$pos" "$$(failed_line tf-lint "$${want# }")"; \
	done; \
	if [ -n "$$failed" ]; then echo "❌ tf-gates-test: 期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ tf-gates-test: 全ケースが期待どおりでした"
