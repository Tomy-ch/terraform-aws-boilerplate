## 層の規則の検査（layer.mk）そのものの検査

LY_TEST_DIR = tmp/layer-check-test

# fixture の宣言。リポジトリの decl.json は読まない（ADR-0702 決定17）。
define LY_FIXTURE_DECL
printf '{"layer_decl": {"attachments": {"aws_s3_bucket_policy": {"parents": {"bucket": ["aws_s3_bucket"]}, "optional": false}}, "not_attachments": {"aws_lb_listener": "子 resource", "aws_s3_bucket": "親になる resource"}}}\n' > "$$1/decl.json"
endef

# env -> usecase -> 一般 module の最小の木。$$2 は aws_s3_bucket_policy の bucket の式。
define LY_FIXTURE_TREE
mkdir -p "$$1/modules/a/tests" "$$1/usecases/u" "$$1/env/dev"; \
printf 'resource "aws_s3_bucket" "this" {}\n\nresource "aws_s3_bucket_policy" "this" {\n  bucket = %s\n}\n' "$$2" > "$$1/modules/a/main.tf"; \
printf 'run "ok" {\n  command = plan\n}\n' > "$$1/modules/a/tests/ok.tftest.hcl"; \
printf 'module "a" {\n  source = "../../modules/a"\n}\n' > "$$1/usecases/u/main.tf"; \
printf 'module "u" {\n  source = "../../usecases/u"\n}\n' > "$$1/env/dev/main.tf"
endef

.PHONY: layer-check-test ## 層の検査が、0件・壊れた入力・宣言の欠け・層の違反・規則の不在で落ちることを検査
layer-check-test:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(LY_TEST_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	decl() { $(LY_FIXTURE_DECL); }; \
	tree() { $(LY_FIXTURE_TREE); }; \
	expect_make() { \
		want="$$1"; case_name="$$2"; target="$$3"; base="$$4"; pattern="$$5"; shift 5; \
		out=$$($(MAKE) -s --no-print-directory "$$target" LY_BASE="$$base" LY_DECL="$$base/decl.json" "$$@" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && [ "$$rc" -ne 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && [ "$$rc" -eq 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ -n "$$pattern" ] && ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	expect() { want="$$1"; case_name="$$2"; base="$$3"; pattern="$$4"; shift 4; expect_make "$$want" "$$case_name" layer-detect "$$base" "$$pattern" "$$@"; }; \
	case_dir() { rm -rf "$$root/$$1"; mkdir -p "$$root/$$1"; tree "$$root/$$1" 'aws_s3_bucket.this.id'; decl "$$root/$$1"; printf '%s' "$$root/$$1"; }; \
	printf '#!/bin/sh\nexit 1\n' > "$$root/conftest-fails"; printf '#!/bin/sh\nexit 0\n' > "$$root/conftest-silent"; \
	printf '#!/bin/sh\nconftest "$$@"\nexit 1\n' > "$$root/conftest-passes-then-fails"; \
	chmod +x "$$root/conftest-fails" "$$root/conftest-silent" "$$root/conftest-passes-then-fails"; \
	d=$$(case_dir ok); \
	expect pass ok "$$d" "違反はありません"; \
	mkdir -p "$$root/no-roots"; decl "$$root/no-roots"; \
	expect fail no-roots "$$root/no-roots" "走査の root が1つもありません"; \
	mkdir -p "$$root/no-files/modules/a"; decl "$$root/no-files"; \
	expect fail no-files "$$root/no-files" "no files found"; \
	d=$$(case_dir no-tf); rm "$$d/modules/a/main.tf" "$$d/usecases/u/main.tf" "$$d/env/dev/main.tf"; \
	expect fail no-tf "$$d" "LY002 [対象が0件]"; \
	d=$$(case_dir broken); printf 'resource "aws_s3_bucket" {\n' > "$$d/modules/a/broken.tf"; \
	expect fail broken-hcl "$$d" "Unclosed configuration block"; \
	d=$$(case_dir decl-missing); \
	expect fail decl-missing "$$d" "missing.json" LY_DECL="$$d/missing.json"; \
	d=$$(case_dir decl-malformed); printf '{"layer_decl": x}\n' > "$$d/decl.json"; \
	expect fail decl-malformed "$$d" "invalid character"; \
	d=$$(case_dir dangling-symlink); ln -s ../../nowhere.tf "$$d/modules/a/dangling.tf"; \
	expect fail dangling-symlink "$$d" "nowhere.tf: no such file or directory"; \
	d=$$(case_dir quoted-name); printf '# x\n' > "$$d/modules/a/q\"b\\c.tf"; \
	expect pass quoted-name "$$d" "違反はありません"; \
	d=$$(case_dir tf-json); printf '{"resource": {}}\n' > "$$d/modules/a/override.tf.json"; \
	expect fail tf-json "$$d" "LY003 [未対応の入力] modules/a/override.tf.json"; \
	d=$$(case_dir terraform-cache); mkdir -p "$$d/modules/a/.terraform/modules/x"; \
	printf 'resource "aws_sqs_queue" "x" {}\n' > "$$d/modules/a/.terraform/modules/x/main.tf"; \
	expect pass terraform-cache "$$d" "違反はありません"; \
	d=$$(case_dir symlink); mkdir -p "$$d/outside"; printf 'resource "aws_s3_bucket" "x" {}\n' > "$$d/outside/real.tf"; ln -s ../../outside/real.tf "$$d/modules/a/link.tf"; \
	expect fail symlinked-config "$$d" "LY002 [件数の不一致]"; \
	d=$$(case_dir empty-decl); printf '{"layer_decl": {"attachments": {}, "not_attachments": {}}}\n' > "$$d/decl.json"; \
	expect fail empty-decl "$$d" "LY001 [宣言の形]"; \
	d=$$(case_dir usecase-resource); printf 'resource "aws_s3_bucket" "x" {}\n' > "$$d/usecases/u/extra.tf"; \
	expect fail usecase-resource "$$d" "LY101 [usecase の resource] usecases/u/extra.tf"; \
	d=$$(case_dir env-to-module); printf 'module "a" {\n  source = "../../modules/a"\n}\n' > "$$d/env/dev/main.tf"; \
	expect fail env-to-module "$$d" "LY203 [層が許さない先] env/dev/main.tf"; \
	d=$$(case_dir remote-source); printf 'module "a" {\n  source = "git::https://example.com/m.git"\n}\n' > "$$d/usecases/u/main.tf"; \
	expect fail remote-source "$$d" "LY201 [相対パスでない source] usecases/u/main.tf"; \
	d=$$(case_dir attachment-module-ref); tree "$$d" 'module.other.id'; \
	expect fail attachment-module-ref "$$d" "LY301 [親の同居] modules/a/main.tf"; \
	d=$$(case_dir unclassified-type); printf 'resource "aws_sqs_queue" "q" {}\n' > "$$d/modules/a/queue.tf"; \
	expect fail unclassified-type "$$d" "LY303 [分類されていない型] resource の型 aws_sqs_queue"; \
	d=$$(case_dir no-rules); \
	expect fail no-rules "$$d" "名前空間の規則が1つも評価されていません" LY_NS=no_such_namespace; \
	d=$$(case_dir no-conftest); \
	expect fail no-conftest "$$d" "/nonexistent/conftest" CONFTEST_BIN=/nonexistent/conftest; \
	expect_make pass verify-ok layer-verify "$$d" "走って通りました"; \
	expect_make fail verify-fails layer-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-fails"; \
	expect_make fail verify-runs-nothing layer-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-silent"; \
	expect_make fail verify-propagates-exit layer-verify "$$d" ": PASS" CONFTEST_BIN="$$root/conftest-passes-then-fails"; \
	expect_make fail check-aggregates layer-check "$$d" "❌ layer-check が落ちた検査: layer-verify layer-detect" \
		CONFTEST_BIN="$$root/conftest-fails"; \
	d=$$(case_dir check-detect-only); printf 'resource "aws_s3_bucket" "x" {}\n' > "$$d/usecases/u/extra.tf"; \
	expect_make fail check-detect-only layer-check "$$d" "落ちた検査: layer-detect"; \
	if [ -n "$$failed" ]; then echo "❌ layer-check-test が期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ layer-check-test: 全ケースが期待どおりでした"
