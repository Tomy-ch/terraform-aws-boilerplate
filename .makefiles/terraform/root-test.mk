## root の構成と backend 設定の不変条件の検査（root.mk）そのものの検査

RT_TEST_DIR = tmp/root-check-test

# fixture の宣言。リポジトリの decl.json は読まない（ADR-0702 決定17）。
define RT_FIXTURE_DECL
printf '{"root_decl": {"management_env": "mgmt", "usecases": {"b": "bootstrap", "c": "control"}}}\n' > "$$1/decl.json"
endef

# $$1 の root に、key が $$2 の backend を置く。$$3 と $$4 は use_lockfile と encrypt の値、$$5 は足す属性の行。
define RT_FIXTURE_BACKEND
printf 'terraform {\n  backend "s3" {\n    key          = "%s"\n    region       = "ap-northeast-1"\n    use_lockfile = %s\n    encrypt      = %s\n%s\n  }\n}\n' "$$2" "$${3:-true}" "$${4:-true}" "$${5:-}" > "$$1/backend.tf"
endef

# 違反の無い木。
define RT_FIXTURE_TREE
mkdir -p "$$1/usecases/b" "$$1/usecases/c" "$$1/usecases/w" "$$1/env/mgmt/identity" "$$1/env/mem/identity"; \
printf 'resource "aws_s3_bucket" "this" {}\n' > "$$1/usecases/b/main.tf"; \
printf 'resource "aws_organizations_organization" "this" {}\n' > "$$1/usecases/c/main.tf"; \
printf 'resource "aws_sqs_queue" "this" {}\n' > "$$1/usecases/w/main.tf"; \
printf 'module "b" {\n  source = "../../../usecases/b"\n}\n\nmodule "c" {\n  source = "../../../usecases/c"\n}\n' > "$$1/env/mgmt/identity/main.tf"; \
printf 'module "b" {\n  source = "../../../usecases/b"\n}\n' > "$$1/env/mem/identity/main.tf"; \
printf 'module "w" {\n  source = "../../usecases/w"\n}\n' > "$$1/env/mem/main.tf"; \
backend "$$1/env/mgmt/identity" identity/terraform.tfstate; \
backend "$$1/env/mem/identity" identity/terraform.tfstate; \
backend "$$1/env/mem" workload/terraform.tfstate
endef

.PHONY: root-check-test ## root の検査が、0件・壊れた入力・宣言の欠け・構成と backend の違反・規則の不在で落ちることを検査
root-check-test:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(RT_TEST_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	decl() { $(RT_FIXTURE_DECL); }; \
	backend() { $(RT_FIXTURE_BACKEND); }; \
	tree() { $(RT_FIXTURE_TREE); }; \
	expect_make() { \
		want="$$1"; case_name="$$2"; target="$$3"; base="$$4"; pattern="$$5"; shift 5; \
		out=$$($(MAKE) -s --no-print-directory "$$target" RT_BASE="$$base" RT_DECL="$$base/decl.json" "$$@" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && [ "$$rc" -ne 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && [ "$$rc" -eq 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ -n "$$pattern" ] && ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	expect() { want="$$1"; case_name="$$2"; base="$$3"; pattern="$$4"; shift 4; expect_make "$$want" "$$case_name" root-detect "$$base" "$$pattern" "$$@"; }; \
	case_dir() { rm -rf "$$root/$$1"; mkdir -p "$$root/$$1"; tree "$$root/$$1"; decl "$$root/$$1"; printf '%s' "$$root/$$1"; }; \
	printf '#!/bin/sh\nexit 1\n' > "$$root/conftest-fails"; printf '#!/bin/sh\nexit 0\n' > "$$root/conftest-silent"; \
	printf '#!/bin/sh\nconftest "$$@"\nexit 1\n' > "$$root/conftest-passes-then-fails"; \
	chmod +x "$$root/conftest-fails" "$$root/conftest-silent" "$$root/conftest-passes-then-fails"; \
	d=$$(case_dir ok); \
	expect pass ok "$$d" "違反はありません"; \
	mkdir -p "$$root/no-roots"; decl "$$root/no-roots"; \
	expect fail no-roots "$$root/no-roots" "走査の root が1つもありません"; \
	mkdir -p "$$root/no-files/env/mem"; decl "$$root/no-files"; \
	expect fail no-files "$$root/no-files" "no files found"; \
	d=$$(case_dir no-tf); find "$$d/usecases" "$$d/env" -name '*.tf' -exec rm {} +; mkdir -p "$$d/env/mem/tests"; \
	printf 'run "ok" {\n  command = plan\n}\n' > "$$d/env/mem/tests/ok.tftest.hcl"; \
	expect fail no-tf "$$d" "RT002 [対象が0件]"; \
	d=$$(case_dir no-root); rm -rf "$$d/env/mgmt" "$$d/env/mem"; mkdir -p "$$d/env/mem/tests"; \
	printf 'run "ok" {\n  command = plan\n}\n' > "$$d/env/mem/tests/ok.tftest.hcl"; \
	expect fail no-root "$$d" "RT002 [root が0件]"; \
	d=$$(case_dir broken); printf 'module "x" {\n' > "$$d/env/mem/broken.tf"; \
	expect fail broken-hcl "$$d" "Unclosed configuration block"; \
	d=$$(case_dir decl-missing); \
	expect fail decl-missing "$$d" "missing.json" RT_DECL="$$d/missing.json"; \
	d=$$(case_dir decl-malformed); printf '{"root_decl": x}\n' > "$$d/decl.json"; \
	expect fail decl-malformed "$$d" "invalid character"; \
	d=$$(case_dir empty-decl); printf '{"root_decl": {"management_env": "mgmt", "usecases": {}}}\n' > "$$d/decl.json"; \
	expect fail empty-decl "$$d" "RT001 [宣言の形]"; \
	d=$$(case_dir decl-unknown-usecase); printf '{"root_decl": {"management_env": "mgmt", "usecases": {"b": "bootstrap", "c": "control", "typo": "control"}}}\n' > "$$d/decl.json"; \
	expect fail decl-unknown-usecase "$$d" "RT004 [宣言が実体を指さない] usecases.typo"; \
	d=$$(case_dir dangling-symlink); ln -s ../../nowhere.tf "$$d/env/mem/dangling.tf"; \
	expect fail dangling-symlink "$$d" "env/mem/dangling.tf の module block を数えられません"; \
	d=$$(case_dir quoted-name); printf '# x\n' > "$$d/env/mem/q\"b\\c.tf"; \
	expect pass quoted-name "$$d" "違反はありません"; \
	d=$$(case_dir symlink); mkdir -p "$$d/outside"; printf 'locals {}\n' > "$$d/outside/real.tf"; ln -s ../../outside/real.tf "$$d/env/mem/link.tf"; \
	expect fail symlinked-config "$$d" "RT002 [件数の不一致] env/mem/link.tf"; \
	d=$$(case_dir module-one-line); printf 'module "w2" { source = "../../usecases/w" }\n' > "$$d/env/mem/extra.tf"; \
	expect pass module-one-line "$$d" "違反はありません"; \
	d=$$(case_dir module-count); printf 'locals {\n  doc = <<EOT\nmodule "fake" {\nEOT\n}\n' > "$$d/env/mem/doc.tf"; \
	expect fail module-count-mismatch "$$d" "RT002 [件数の不一致] env/mem/doc.tf の module block を 0 件読みましたが、独立の走査は 1 件です"; \
	d=$$(case_dir tf-json); printf '{"locals": {}}\n' > "$$d/env/mem/extra.tf.json"; \
	expect fail tf-json "$$d" "RT003 [未対応の入力] env/mem/extra.tf.json"; \
	d=$$(case_dir override); printf 'locals {}\n' > "$$d/env/mem/override.tf"; \
	expect fail override "$$d" "RT003 [未対応の入力] env/mem/override.tf"; \
	d=$$(case_dir terraform-cache); mkdir -p "$$d/env/mem/.terraform/modules/x"; \
	printf 'module "y" {\n  source = "git::https://example.com/m.git"\n}\n' > "$$d/env/mem/.terraform/modules/x/main.tf"; \
	expect pass terraform-cache "$$d" "違反はありません"; \
	d=$$(case_dir identity-calls-workload-usecase); printf 'module "w" {\n  source = "../../../usecases/w"\n}\n' > "$$d/env/mem/identity/main.tf"; \
	expect fail identity-calls-workload-usecase "$$d" "RT101 [identity root が呼ぶ先] env/mem/identity/main.tf"; \
	d=$$(case_dir member-calls-control); printf 'module "c" {\n  source = "../../../usecases/c"\n}\n' >> "$$d/env/mem/identity/main.tf"; \
	expect fail member-calls-control "$$d" "RT102 [統制の usecase] env/mem/identity/main.tf"; \
	d=$$(case_dir workload-calls-bootstrap); printf 'module "b" {\n  source = "../../usecases/b"\n}\n' >> "$$d/env/mem/main.tf"; \
	expect fail workload-calls-bootstrap "$$d" "RT103 [workload root が呼ぶ先] env/mem/main.tf"; \
	d=$$(case_dir remote-source); printf 'module "w" {\n  source = "git::https://example.com/m.git"\n}\n' > "$$d/env/mem/main.tf"; \
	expect fail remote-source "$$d" "RT104 [source が解けない] env/mem/main.tf"; \
	d=$$(case_dir backend-missing); rm "$$d/env/mem/backend.tf"; \
	expect fail backend-missing "$$d" "RT201 [backend の数] root env/mem の backend が 0 件です"; \
	d=$$(case_dir backend-outside-backend-tf); mv "$$d/env/mem/backend.tf" "$$d/env/mem/versions.tf"; \
	expect fail backend-outside-backend-tf "$$d" "RT201 [backend の置き場] env/mem/versions.tf"; \
	d=$$(case_dir wrong-key); backend "$$d/env/mem" identity/terraform.tfstate; \
	expect fail wrong-key "$$d" "RT202 [backend の key] env/mem/backend.tf"; \
	d=$$(case_dir lockfile-string); backend "$$d/env/mem" workload/terraform.tfstate '"true"'; \
	expect fail lockfile-string "$$d" "RT203 [lock file] env/mem/backend.tf"; \
	d=$$(case_dir encrypt-false); backend "$$d/env/mem" workload/terraform.tfstate true false; \
	expect fail encrypt-false "$$d" "RT204 [暗号化] env/mem/backend.tf"; \
	d=$$(case_dir dynamodb); backend "$$d/env/mem" workload/terraform.tfstate true true '    dynamodb_table = "locks"'; \
	expect fail dynamodb "$$d" "RT205 [DynamoDB の lock] env/mem/backend.tf"; \
	d=$$(case_dir kms-in-hcl); backend "$$d/env/mem" workload/terraform.tfstate true true '    kms_key_id = "x"'; \
	expect fail kms-in-hcl "$$d" "RT206 [鍵の書き方] env/mem/backend.tf"; \
	d=$$(case_dir workload-calls-subpath); mkdir -p "$$d/usecases/b/internal/x"; printf 'locals {}\n' > "$$d/usecases/b/internal/x/main.tf"; \
	printf 'module "x" {\n  source = "../../usecases/b/internal/x"\n}\n' >> "$$d/env/mem/main.tf"; \
	expect fail workload-calls-subpath "$$d" "RT105 [usecase でない先] env/mem/main.tf の module.x が usecases/b/internal/x"; \
	d=$$(case_dir duplicate-backend); cat "$$d/env/mem/backend.tf" "$$d/env/mem/backend.tf" > "$$d/b2" && mv "$$d/b2" "$$d/env/mem/backend.tf"; \
	expect fail duplicate-backend "$$d" "RT201 [backend の数] root env/mem の backend が 2 件です"; \
	d=$$(case_dir backend-in-hidden-dir); mkdir -p "$$d/env/mem/.alt"; backend "$$d/env/mem/.alt" workload/terraform.tfstate; \
	expect fail backend-in-hidden-dir "$$d" "RT106 [root の外の backend] env/mem/.alt/backend.tf"; \
	d=$$(case_dir module-unquoted-label); printf 'module w2 {\n  source = "../../usecases/w"\n}\n' > "$$d/env/mem/extra.tf"; \
	expect pass module-unquoted-label "$$d" "違反はありません"; \
	d=$$(case_dir module-tab-separator); printf 'module\t"w2" {\n  source = "../../usecases/w"\n}\n' > "$$d/env/mem/extra.tf"; \
	expect pass module-tab-separator "$$d" "違反はありません"; \
	d=$$(case_dir dir-named-tf); mkdir -p "$$d/env/mem/x.tf"; \
	expect fail dir-named-tf "$$d" "env/mem/x.tf の module block を数えられません"; \
	d=$$(case_dir broken-tf-json); printf '{' > "$$d/env/mem/extra.tf.json"; \
	expect fail broken-tf-json "$$d" "extra.tf.json"; \
	d=$$(case_dir decl-no-root-key); printf '{}\n' > "$$d/decl.json"; \
	expect fail decl-no-root-key "$$d" "RT001 [宣言の形] data.root_decl がオブジェクトではありません"; \
	d=$$(case_dir no-usecases-dir); find "$$d/usecases" -name '*.tf' -exec rm {} +; \
	expect fail no-usecases-tf "$$d" "RT004 [宣言が実体を指さない] usecases.b"; \
	d=$$(case_dir detect-conftest-silent); \
	expect fail detect-conftest-silent "$$d" "結果を解釈できません" CONFTEST_BIN="$$root/conftest-silent"; \
	expect fail detect-conftest-fails "$$d" "結果を解釈できません" CONFTEST_BIN="$$root/conftest-fails"; \
	d=$$(case_dir no-rules); \
	expect fail no-rules "$$d" "名前空間の規則が1つも評価されていません" RT_NS=no_such_namespace; \
	d=$$(case_dir no-conftest); \
	expect fail no-conftest "$$d" "/nonexistent/conftest" CONFTEST_BIN=/nonexistent/conftest; \
	expect_make pass verify-ok root-verify "$$d" "走って通りました"; \
	expect_make fail verify-fails root-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-fails"; \
	expect_make fail verify-runs-nothing root-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-silent"; \
	expect_make fail verify-propagates-exit root-verify "$$d" ": PASS" CONFTEST_BIN="$$root/conftest-passes-then-fails"; \
	expect_make fail check-aggregates root-check "$$d" "❌ root-check が落ちた検査: root-verify root-detect" \
		CONFTEST_BIN="$$root/conftest-fails"; \
	d=$$(case_dir check-detect-only); rm "$$d/env/mem/backend.tf"; \
	expect_make fail check-detect-only root-check "$$d" "落ちた検査: root-detect"; \
	if [ -n "$$failed" ]; then echo "❌ root-check-test が期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ root-check-test: 全ケースが期待どおりでした"
