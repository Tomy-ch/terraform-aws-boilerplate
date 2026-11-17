## resource の作成の順序の検査（resource-order.mk）そのものの検査

RO_TEST_DIR = tmp/resource-order-test

# fixture の宣言。リポジトリの decl.json は読まない（ADR-0702 決定17）。
define RO_FIXTURE_DECL
printf '{"resource_order_decl": {"rules": [{"module": "modules/a", "resource": "aws_iam_role.this", "after": "aws_iam_role_policy.first", "adr": "docs/adr/x.md"}]}}\n' > "$$1/decl.json"
endef

# $$2 は aws_iam_role.this の本体。
define RO_FIXTURE_MODULE
mkdir -p "$$1/modules/a/tests"; \
printf 'resource "aws_iam_role_policy" "first" {}\n\nresource "aws_iam_role" "this" {\n%b}\n' "$$2" > "$$1/modules/a/main.tf"; \
printf 'run "ok" {\n  command = plan\n}\n' > "$$1/modules/a/tests/ok.tftest.hcl"
endef

.PHONY: resource-order-check-test ## 順序の検査が、0件・壊れた入力・宣言の欠け・依存の欠け・規則の不在で落ちることを検査
resource-order-check-test:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(RO_TEST_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	decl() { $(RO_FIXTURE_DECL); }; \
	module() { $(RO_FIXTURE_MODULE); }; \
	expect_make() { \
		want="$$1"; case_name="$$2"; target="$$3"; base="$$4"; pattern="$$5"; shift 5; \
		out=$$($(MAKE) -s --no-print-directory "$$target" RO_BASE="$$base" RO_DECL="$$base/decl.json" "$$@" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && [ "$$rc" -ne 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && [ "$$rc" -eq 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ -n "$$pattern" ] && ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	expect() { want="$$1"; case_name="$$2"; base="$$3"; pattern="$$4"; shift 4; expect_make "$$want" "$$case_name" resource-order-detect "$$base" "$$pattern" "$$@"; }; \
	case_dir() { rm -rf "$$root/$$1"; mkdir -p "$$root/$$1/usecases" "$$root/$$1/env"; module "$$root/$$1" '  depends_on = [aws_iam_role_policy.first]\n'; decl "$$root/$$1"; printf '%s' "$$root/$$1"; }; \
	printf '#!/bin/sh\nexit 1\n' > "$$root/conftest-fails"; printf '#!/bin/sh\nexit 0\n' > "$$root/conftest-silent"; chmod +x "$$root/conftest-fails" "$$root/conftest-silent"; \
	d=$$(case_dir ok); \
	expect pass ok "$$d" "違反はありません"; \
	mkdir -p "$$root/no-roots"; decl "$$root/no-roots"; \
	expect fail no-roots "$$root/no-roots" "走査の root が1つもありません"; \
	mkdir -p "$$root/no-files/modules/a"; decl "$$root/no-files"; \
	expect fail no-files "$$root/no-files" "no files found"; \
	d=$$(case_dir no-tf); rm "$$d/modules/a/main.tf"; \
	expect fail no-tf "$$d" "RO002 [対象が0件]"; \
	d=$$(case_dir no-depends-on); module "$$d" ''; \
	expect fail no-depends-on "$$d" "RO005 [順序] modules/a/main.tf の aws_iam_role.this"; \
	d=$$(case_dir other-depends-on); module "$$d" '  depends_on = [aws_iam_role_policy.other]\n'; \
	expect fail other-depends-on "$$d" "RO005 [順序]"; \
	d=$$(case_dir moved-module); mkdir -p "$$d/modules/b"; mv "$$d/modules/a/main.tf" "$$d/modules/b/main.tf"; \
	expect fail moved-module "$$d" "RO003 [module が無い] modules/a"; \
	d=$$(case_dir renamed-resource); printf 'resource "aws_iam_role_policy" "first" {}\n\nresource "aws_iam_role" "renamed" {}\n' > "$$d/modules/a/main.tf"; \
	expect fail renamed-resource "$$d" "RO004 [resource が無い]"; \
	d=$$(case_dir broken); printf 'resource "aws_iam_role" {\n' > "$$d/modules/a/broken.tf"; \
	expect fail broken-hcl "$$d" "broken.tf"; \
	d=$$(case_dir quoted-name); printf '# x\n' > "$$d/modules/a/q\"b\\c.tf"; \
	expect pass quoted-name "$$d" "違反はありません"; \
	d=$$(case_dir tf-json); printf '{"resource": {}}\n' > "$$d/modules/a/override.tf.json"; \
	expect fail tf-json "$$d" "RO006 [未対応の入力] modules/a/override.tf.json"; \
	d=$$(case_dir terraform-cache); mkdir -p "$$d/modules/a/.terraform/modules/x"; \
	printf 'resource "aws_iam_role" "this" {}\n' > "$$d/modules/a/.terraform/modules/x/main.tf"; \
	expect pass terraform-cache "$$d" "違反はありません"; \
	d=$$(case_dir symlink); mkdir -p "$$d/outside"; printf 'resource "aws_iam_role" "x" {}\n' > "$$d/outside/real.tf"; ln -s ../../outside/real.tf "$$d/modules/a/link.tf"; \
	expect fail symlinked-config "$$d" "RO002 [件数の不一致]"; \
	d=$$(case_dir undeclared); printf '{"resource_order_decl": {"rules": []}}\n' > "$$d/decl.json"; \
	expect fail undeclared "$$d" "RO001 [宣言の形]"; \
	d=$$(case_dir no-rules); \
	expect fail no-rules "$$d" "名前空間の規則が1つも評価されていません" RO_NS=no_such_namespace; \
	d=$$(case_dir no-conftest); \
	expect fail no-conftest "$$d" "/nonexistent/conftest" CONFTEST_BIN=/nonexistent/conftest; \
	expect_make pass verify-ok resource-order-verify "$$d" "走って通りました"; \
	expect_make fail verify-fails resource-order-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-fails"; \
	expect_make fail verify-runs-nothing resource-order-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-silent"; \
	expect_make fail check-aggregates resource-order-check "$$d" "❌ resource-order-check が落ちた検査: resource-order-verify resource-order-detect" \
		CONFTEST_BIN="$$root/conftest-fails"; \
	if [ -n "$$failed" ]; then echo "❌ resource-order-check-test が期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ resource-order-check-test: 全ケースが期待どおりでした"
