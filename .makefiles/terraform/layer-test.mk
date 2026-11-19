## 層の規則の検査（layer.mk）そのものの検査

LY_TEST_DIR = tmp/layer-check-test

# fixture の宣言。リポジトリの decl.json は読まない（ADR-0702 決定17）。
define LY_FIXTURE_DECL
printf '{"layer_decl": {"attachments": {"aws_s3_bucket_policy": {"parents": {"bucket": ["aws_s3_bucket"]}, "optional": false}, "aws_guardduty_malware_protection_plan": {"parents": {"protected_resource.s3_bucket.bucket_name": ["aws_s3_bucket"]}, "optional": false}}, "not_attachments": {"aws_lb_listener": "子 resource", "aws_s3_bucket": "親になる resource"}}}\n' > "$$1/decl.json"
endef

# env -> usecase -> 一般 module の最小の木。$$2 は aws_s3_bucket_policy の bucket の式。
define LY_FIXTURE_TREE
mkdir -p "$$1/modules/a/tests" "$$1/usecases/u" "$$1/env/dev"; \
printf 'resource "aws_s3_bucket" "this" {}\n\nresource "aws_s3_bucket_policy" "this" {\n  bucket = %s\n}\n' "$$2" > "$$1/modules/a/main.tf"; \
printf 'run "ok" {\n  command = plan\n}\n' > "$$1/modules/a/tests/ok.tftest.hcl"; \
printf 'module "a" {\n  source = "../../modules/a"\n}\n' > "$$1/usecases/u/main.tf"; \
printf 'module "u" {\n  source = "../../usecases/u"\n}\n' > "$$1/env/dev/main.tf"
endef

# 親の引数が入れ子の block の中に在る attachment 型（dotted な引数）。$$2 は bucket_name の式。
define LY_FIXTURE_NESTED
printf 'resource "aws_guardduty_malware_protection_plan" "this" {\n  protected_resource {\n    s3_bucket {\n      bucket_name = %s\n    }\n  }\n}\n' "$$2" > "$$1/modules/a/plan.tf"
endef

# aws_s3_bucket_policy の bucket を each.value にする。$$2 は for_each の式。
define LY_FIXTURE_FOR_EACH
printf 'resource "aws_s3_bucket" "this" {}\n\nresource "aws_s3_bucket_policy" "this" {\n  for_each = %s\n  bucket   = each.value\n}\n' "$$2" > "$$1/modules/a/main.tf"
endef

define LY_FIXTURE_DYNAMIC
printf 'resource "aws_guardduty_malware_protection_plan" "this" {\n  dynamic "protected_resource" {\n    for_each = [1]\n    content {\n      s3_bucket {\n        bucket_name = %s\n      }\n    }\n  }\n}\n' "$$2" > "$$1/modules/a/plan.tf"
endef

# LY301 の文言を、違反の葉の並び（$$1）まで含めて返す。別の理由で落ちたケースを通さない。
define LY_EXPECT_LY301
printf 'LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: %s（' "$$1"
endef

.PHONY: layer-check-test ## 層の検査が、0件・壊れた入力・宣言の欠け・HCL の入力の欠け・層の違反・規則の不在で落ちることを検査
layer-check-test:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(LY_TEST_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	decl() { $(LY_FIXTURE_DECL); }; \
	tree() { $(LY_FIXTURE_TREE); }; \
	nested() { $(LY_FIXTURE_NESTED); }; \
	dyn() { $(LY_FIXTURE_DYNAMIC); }; \
	foreach() { $(LY_FIXTURE_FOR_EACH); }; \
	ly301() { $(LY_EXPECT_LY301); }; \
	stub() { printf 'LY_HCL_INPUT_CMD=cp %s $(LY_OUT)/hcl-input.json' "$$1"; }; \
	attachment() { d=$$(case_dir "$$2"); tree "$$d" "$$3"; if [ "$$1" = pass ]; then expect pass "$$2" "$$d" "違反はありません"; else expect fail "$$2" "$$d" "$$(ly301 "$$4")"; fi; }; \
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
	cp "$(LY_OUT)/hcl-input.json" "$$root/ok.json"; \
	sed '/^ *"env\/dev\/main.tf",$$/d' "$$root/ok.json" > "$$root/missing-file.json"; \
	printf '{"hcl_input": {"files": [], "blocks": []}}\n' > "$$root/empty.json"; \
	printf '{"hcl_input": []}\n' > "$$root/not-object.json"; \
	printf '{"hcl_input": x}\n' > "$$root/malformed.json"; \
	mkdir -p "$$root/no-roots"; decl "$$root/no-roots"; \
	expect fail no-roots "$$root/no-roots" "走査の root が1つもありません"; \
	mkdir -p "$$root/no-files/modules/a"; decl "$$root/no-files"; \
	expect fail no-files "$$root/no-files" "Terraform の構成ファイル（.tf）が1件もありません"; \
	d=$$(case_dir no-tf); rm "$$d/modules/a/main.tf" "$$d/usecases/u/main.tf" "$$d/env/dev/main.tf"; \
	expect fail no-tf "$$d" "Terraform の構成ファイル（.tf）が1件もありません"; \
	expect fail no-tf-rego "$$d" "LY002 [対象が0件]" "$$(stub "$$root/empty.json")"; \
	d=$$(case_dir broken); printf 'resource "aws_s3_bucket" {\n' > "$$d/modules/a/broken.tf"; \
	expect fail broken-hcl "$$d" "Unclosed configuration block"; \
	d=$$(case_dir decl-missing); \
	expect fail decl-missing "$$d" "missing.json" LY_DECL="$$d/missing.json"; \
	d=$$(case_dir decl-malformed); printf '{"layer_decl": x}\n' > "$$d/decl.json"; \
	expect fail decl-malformed "$$d" "invalid character"; \
	d=$$(case_dir dangling-symlink); ln -s ../../nowhere.tf "$$d/modules/a/dangling.tf"; \
	expect fail dangling-symlink "$$d" "modules/a/dangling.tf: 走査の root の下に symlink があります"; \
	d=$$(case_dir quoted-name); printf '# x\n' > "$$d/modules/a/q\"b\\c.tf"; \
	expect pass quoted-name "$$d" "違反はありません"; \
	d=$$(case_dir tf-json); printf '{"resource": {}}\n' > "$$d/modules/a/override.tf.json"; \
	expect fail tf-json "$$d" "modules/a/override.tf.json: .tf.json は読みません: 読まない形の入力があります"; \
	expect fail tf-json-rego "$$d" "LY003 [未対応の入力] modules/a/override.tf.json" "$$(stub "$$root/ok.json")"; \
	d=$$(case_dir terraform-cache); mkdir -p "$$d/modules/a/.terraform/modules/x"; \
	printf 'resource "aws_sqs_queue" "x" {}\n' > "$$d/modules/a/.terraform/modules/x/main.tf"; \
	expect pass terraform-cache "$$d" "違反はありません"; \
	d=$$(case_dir symlink); mkdir -p "$$d/outside"; printf 'resource "aws_s3_bucket" "x" {}\n' > "$$d/outside/real.tf"; ln -s ../../outside/real.tf "$$d/modules/a/link.tf"; \
	expect fail symlinked-config "$$d" "modules/a/link.tf: 走査の root の下に symlink があります"; \
	expect fail symlinked-config-rego "$$d" "LY002 [件数の不一致]" "$$(stub "$$root/ok.json")"; \
	d=$$(case_dir hcl-input-fails); \
	expect fail hcl-input-fails "$$d" "hcl-input が失敗したので、Conftest を起動しません" LY_HCL_INPUT_CMD="$$root/conftest-fails"; \
	expect fail hcl-input-stale "$$d" "hcl-input が成功を返しましたが、$(LY_OUT)/hcl-input.json を書いていません" LY_HCL_INPUT_CMD=:; \
	expect fail hcl-input-empty "$$d" "LY005 [HCL の入力の突き合わせ] modules/a/main.tf の resource aws_s3_bucket.this を hcl-input が出していません" "$$(stub "$$root/empty.json")"; \
	expect fail hcl-input-missing-file "$$d" "LY002 [件数の不一致] env/dev/main.tf を hcl-input が読んでいません" "$$(stub "$$root/missing-file.json")"; \
	expect fail hcl-input-not-object "$$d" "LY004 [HCL の入力の形]" "$$(stub "$$root/not-object.json")"; \
	expect fail hcl-input-malformed "$$d" "invalid character" "$$(stub "$$root/malformed.json")"; \
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
	attachment fail escape1-quote-pair 'coalesce(aws_s3_bucket.this.id, "a$$", var.z, module.m.x, "b")' 'module.m, var.z'; \
	attachment fail escape2-brace-in-interpolation '"$${aws_s3_bucket.this.id != "}" ? var.x : var.y}"' 'var.x, var.y'; \
	attachment fail escape3-template-directive '"$${aws_s3_bucket.this.id}%{ if var.a }y%{ endif }"' 'var.a'; \
	attachment fail escape4-concatenated-dollar '"$${aws_s3_bucket.this.id}$${"$$"}$${var.x}"' 'var.x'; \
	attachment fail escape5-comment-in-expression '(aws_s3_bucket.this.id /* " */ == var.x ? aws_s3_bucket.this.id : aws_s3_bucket.this.id /* " */)' 'var.x'; \
	attachment fail escape5-comment-in-expression-line "$$(printf '[aws_s3_bucket.this.id # "\n, var.x # "\n][1]')" 'var.x'; \
	attachment fail escape6-iterator-shadowing 'concat([for var in aws_s3_bucket.this : var.id], [var.y])[0]' 'var.y'; \
	attachment fail escape6-iterator-shadowing-resource-type 'concat([for aws_iam_role in [aws_s3_bucket.this.id] : aws_iam_role], [aws_iam_role.r.arn])[0]' 'aws_iam_role.r'; \
	d=$$(case_dir escape7-object-key); tree "$$d" 'one(keys(local.m))'; \
	printf 'locals {\n  m = { (module.m.x) = aws_s3_bucket.this.id }\n}\n' > "$$d/modules/a/locals.tf"; \
	expect fail escape7-object-key "$$d" "$$(ly301 'module.m')"; \
	attachment fail escaped-only '"$$$${aws_s3_bucket.this.id}"' '参照がありません'; \
	attachment fail deep-nesting '"$${aws_s3_bucket.this.id}$${ {a = {b = {c = {d = module.x.id}}}} }"' 'module.x'; \
	attachment pass cond-in-interpolation '"$${aws_s3_bucket.this.id != "" ? aws_s3_bucket.this.id : aws_s3_bucket.this.bucket}"'; \
	attachment pass comment-in-expression 'aws_s3_bucket.this.id /* " */'; \
	attachment pass template-directive-parent '"$${aws_s3_bucket.this.id}%{ if true }x%{ endif }"'; \
	attachment pass escaped-literal-then-parent '"$$$${x}-$${aws_s3_bucket.this.id}"'; \
	attachment pass index-after-reference 'aws_s3_bucket.this[module.x.key].id'; \
	attachment fail index-after-attribute 'aws_s3_bucket.this.tags["a"][module.m.x]' 'module.m'; \
	attachment fail second-index-after-reference 'aws_s3_bucket.this[0][module.m.x]' 'module.m'; \
	attachment pass splat-parent 'aws_s3_bucket.this[*].id[0]'; \
	d=$$(case_dir for-each-module); foreach "$$d" '{ k = module.m.x }'; \
	expect fail for-each-module "$$d" "$$(ly301 'module.m')"; \
	d=$$(case_dir for-each-parent); foreach "$$d" '{ k = aws_s3_bucket.this.id }'; \
	expect pass for-each-parent "$$d" "違反はありません"; \
	d=$$(case_dir nested-parent-ok); nested "$$d" 'aws_s3_bucket.this.bucket'; \
	expect pass nested-parent-ok "$$d" "違反はありません"; \
	d=$$(case_dir nested-parent-module); nested "$$d" 'module.b.name'; \
	expect fail nested-parent-module "$$d" "LY301 [親の同居] modules/a/plan.tf の aws_guardduty_malware_protection_plan.this の protected_resource.s3_bucket.bucket_name が、同じ module の親（aws_s3_bucket）だけを参照していません: module.b（"; \
	d=$$(case_dir dynamic-parent-ok); dyn "$$d" 'aws_s3_bucket.this.bucket'; \
	expect pass dynamic-parent-ok "$$d" "違反はありません"; \
	d=$$(case_dir dynamic-parent-module); dyn "$$d" 'module.b.name'; \
	expect fail dynamic-parent-module "$$d" "LY301 [親の同居] modules/a/plan.tf の aws_guardduty_malware_protection_plan.this の protected_resource.s3_bucket.bucket_name が、同じ module の親（aws_s3_bucket）だけを参照していません: module.b（"; \
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
