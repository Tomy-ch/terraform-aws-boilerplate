## インフラ内コードの検出（infra-code.mk）そのものの検査

IC_TEST_DIR = tmp/infra-code-test

# fixture の宣言。リポジトリの宣言の今日の中身で、検査の検査が通ったり落ちたりしないように、型の分類も
# 許可リストもここで閉じる。$$2 は許可リストの中身。
define IC_FIXTURE_DECL
printf '{"infra_code_decl": {"types": {"resource.aws_s3_bucket": {"executes_code": false}, "data.aws_region": {"executes_code": false}, "resource.aws_instance": {"executes_code": true, "body_args": ["user_data"]}, "data.external": {"executes_code": true, "path_args": ["program"]}}, "providers": {"registry.terraform.io/hashicorp/aws": {"command_blocks": []}}, "languages": {}, "allowlist": [%s]}}\n' "$${2:-}" > "$$1/decl.json"
endef

define IC_FIXTURE_OK
mkdir -p "$$1/tests"; \
printf 'terraform {\n  required_providers {\n    aws = {\n      source = "hashicorp/aws"\n    }\n  }\n}\n\nprovider "aws" {}\n\nvariable "v" {\n  type = string\n}\n\nlocals {\n  x = 1\n}\n\nmodule "m" {\n  source = "./m"\n}\n\nresource "aws_s3_bucket" "b" {}\n\ndata "aws_region" "r" {}\n\nimport {\n  to = aws_s3_bucket.b\n  id = "b"\n}\n\noutput "o" {\n  value = 1\n}\n' > "$$1/main.tf"; \
printf 'mock_provider "aws" {}\n\nrun "ok" {\n  command = plan\n}\n' > "$$1/tests/ok.tftest.hcl"
endef

.PHONY: infra-code-gate-test ## インフラ内コードの検出が、0件・壊れた入力・未分類・許可リスト外で落ちることを検査
infra-code-gate-test:
	@set -u; $(GIT_ISOLATE); mkdir -p tmp; root=$$(mktemp -d "$(IC_TEST_DIR).XXXXXX") || exit 1; trap 'rm -rf "$$root"' EXIT; failed=""; \
	ok() { $(IC_FIXTURE_OK); }; \
	decl() { $(IC_FIXTURE_DECL); }; \
	expect_make() { \
		want="$$1"; case_name="$$2"; target="$$3"; base="$$4"; pattern="$$5"; shift 5; \
		if [ -d "$$base/.git" ] && [ ! -e "$$base/.untracked" ]; then git -C "$$base" add -A >/dev/null 2>&1; fi; \
		out=$$($(MAKE) -s --no-print-directory "$$target" IC_BASE="$$base" IC_DECL="$$base/decl.json" "$$@" 2>&1); rc=$$?; \
		if [ "$$want" = pass ] && [ "$$rc" -ne 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ "$$want" = fail ] && [ "$$rc" -eq 0 ]; then failed="$$failed $$case_name"; echo "$$out"; \
		elif [ -n "$$pattern" ] && ! printf '%s' "$$out" | grep -qF -- "$$pattern"; then failed="$$failed $$case_name"; echo "$$out"; \
		fi; \
	}; \
	expect() { want="$$1"; case_name="$$2"; base="$$3"; pattern="$$4"; shift 4; expect_make "$$want" "$$case_name" infra-code-detect "$$base" "$$pattern" "$$@"; }; \
	repo() { mkdir -p "$$1/usecases" "$$1/modules" "$$1/env"; printf '.terraform/\n' > "$$1/.gitignore"; git -C "$$1" init -q; }; \
	case_dir() { rm -rf "$$root/$$1"; repo "$$root/$$1"; ok "$$root/$$1/modules/a"; decl "$$root/$$1"; printf '%s' "$$root/$$1"; }; \
	printf '#!/bin/sh\nexit 1\n' > "$$root/conftest-fails"; printf '#!/bin/sh\nexit 0\n' > "$$root/conftest-silent"; \
	printf '#!/bin/sh\nprintf "%%s\\n" "$$@" >> "%s"\nexec conftest "$$@"\n' "$$root/conftest-args" > "$$root/conftest-recording"; \
	printf '#!/bin/sh\nprintf "{\\"fixtures\\": {}}\\n" > "$$2"\n' > "$$root/fixtures-empty"; \
	chmod +x "$$root/conftest-fails" "$$root/conftest-silent" "$$root/conftest-recording" "$$root/fixtures-empty"; \
	stub() { printf 'IC_HCL_INPUT_CMD=cp %s $(IC_OUT)/hcl-input.json' "$$1"; }; \
	printf '"x"\n' > "$$root/not-object.json"; printf '{"files": ' > "$$root/malformed.json"; \
	printf '{"files": [], "blocks": []}\n' > "$$root/no-files.json"; \
	printf '#!/bin/sh\nprintf "data.infra_code.test_IC010_deny_unclassified_type: FAIL (1ms)\\n"\nexit 1\n' > "$$root/conftest-fail-line"; chmod +x "$$root/conftest-fail-line"; \
	mkdir -p "$$root/no-roots"; decl "$$root/no-roots"; \
	expect fail no-roots "$$root/no-roots" "走査の root が1つもありません"; \
	rm -rf "$$root/missing-root"; d=$$(case_dir missing-root); rmdir "$$d/env"; \
	expect fail missing-root "$$d" "走査の root がありません"; \
	mkdir -p "$$root/empty/modules/a"; repo "$$root/empty"; decl "$$root/empty"; \
	expect fail empty "$$root/empty" "Terraform の構成ファイル（.tf）が1件もありません"; \
	d=$$(case_dir ok); : > "$$d/modules/a/empty.tf"; printf '# コメントだけ\n' > "$$d/modules/a/comments.tf"; \
	expect pass ok "$$d" "構成ファイル 4 件について検出の規則が評価され"; \
	d=$$(case_dir untracked); : > "$$d/.untracked"; \
	expect fail untracked "$$d" "追跡しているファイルが1件もありません"; \
	d=$$(case_dir dot-terraform); mkdir -p "$$d/modules/a/.terraform/modules/x"; \
	printf 'resource "aws_instance" "i" {\n  user_data = "x"\n}\n' > "$$d/modules/a/.terraform/modules/x/evil.tf"; : > "$$d/modules/a/.terraform.lock.hcl"; \
	expect pass dot-terraform "$$d" "検出の規則が評価され"; \
	git -C "$$d" add -f modules/a/.terraform/modules/x/evil.tf; \
	expect fail tracked-terraform-dir "$$d" "の下のファイルを追跡しています"; \
	d=$$(case_dir outside-root); mkdir -p "$$d/docs"; printf 'resource "aws_s3_bucket" "x" {}\n' > "$$d/docs/x.tf"; \
	expect fail tracked-outside-root "$$d" "走査の root（usecases modules env）の外に在ります"; \
	d=$$(case_dir broken); printf 'resource "aws_s3_bucket" {\n' > "$$d/modules/a/broken.tf"; \
	expect fail broken-hcl "$$d" "broken.tf"; \
	d=$$(case_dir json); printf '{"resource": {}}\n' > "$$d/modules/a/x.tf.json"; \
	expect fail tf-json "$$d" "IC002 [未対応の入力] modules/a/x.tf.json"; \
	d=$$(case_dir tfvars); printf 'v = "a"\n' > "$$d/modules/a/terraform.tfvars"; \
	expect fail tfvars "$$d" "IC001 [対象外の入力] modules/a/terraform.tfvars"; \
	d=$$(case_dir unparsed-extension); mkdir -p "$$d/modules/a/src"; printf 'print("x")\n' > "$$d/modules/a/src/handler.py"; \
	expect fail ic001-unparsed-extension "$$d" "IC001 [対象外の入力] modules/a/src/handler.py"; \
	d=$$(case_dir usecase-tree); mkdir -p "$$d/usecases/u/tests" "$$d/usecases/u/docs/adr" "$$d/usecases/u/reference"; \
	printf 'resource "aws_s3_bucket" "b" {}\n' > "$$d/usecases/u/main.tf"; printf 'run "a" {}\n' > "$$d/usecases/u/tests/a.tftest.hcl"; \
	printf '# u\n' > "$$d/usecases/u/README.md"; printf '# 0001\n' > "$$d/usecases/u/docs/adr/0001-x.md"; \
	printf 'provider "registry.terraform.io/hashicorp/aws" {\n  version = "6.70.0"\n}\n' > "$$d/usecases/u/reference/.terraform.lock.hcl"; \
	expect pass ic001-usecase-tree-clean "$$d" "検出の規則が評価され"; \
	d=$$(case_dir unknown-block); printf 'future_block "x" {}\n' > "$$d/modules/a/f.tf"; \
	expect fail unknown-block "$$d" "IC003 [未知のブロック] modules/a/f.tf のブロック future_block"; \
	d=$$(case_dir broken-tftest); printf 'run {\n' > "$$d/modules/a/tests/broken.tftest.hcl"; \
	expect fail broken-tftest "$$d" "modules/a/tests/broken.tftest.hcl"; \
	d=$$(case_dir block-count); printf 'locals {\n  x = <<EOT\nresource "a" "b" {\nEOT\n}\n' > "$$d/modules/a/h.tf"; \
	expect fail block-count-mismatch "$$d" "IC005 [入力の形] modules/a/h.tf のトップレベルの block を 1 個読みましたが、行頭の数えは 2 個です"; \
	d=$$(case_dir unclassified); printf 'resource "aws_not_classified" "c" {}\n' > "$$d/modules/a/canary.tf"; \
	expect fail unclassified "$$d" "IC010 [未分類の型]"; \
	d=$$(case_dir user-data); printf 'resource "aws_instance" "i" {\n  user_data = "x"\n}\n' > "$$d/modules/a/ec2.tf"; \
	expect fail user-data "$$d" "modules/a#resource.aws_instance.i"; \
	d=$$(case_dir provisioner); printf 'resource "aws_s3_bucket" "p" {\n  dynamic "provisioner" {\n    for_each = []\n    content {}\n  }\n}\n' > "$$d/modules/a/p.tf"; \
	expect fail dynamic-provisioner "$$d" "modules/a#resource.aws_s3_bucket.p（provisioner）"; \
	d=$$(case_dir check-external); printf 'check "c" {\n  data "external" "e" {\n    program = ["sh"]\n  }\n  assert {\n    condition     = true\n    error_message = "x"\n  }\n}\n' > "$$d/modules/a/check.tf"; \
	expect fail check-external "$$d" "modules/a#data.external.e"; \
	d=$$(case_dir tftest-exec); printf 'provider "aws" {\n  dynamic "exec" {\n    for_each = [1]\n    content {}\n  }\n}\n' > "$$d/modules/a/tests/exec.tftest.hcl"; \
	expect fail tftest-exec "$$d" "modules/a/tests/exec.tftest.hcl#provider.aws（provider の exec）"; \
	d=$$(case_dir stale); decl "$$d" '{"id": "modules/a#resource.aws_instance.gone", "owner": "external", "repository": "github.com/o/r"}'; \
	expect fail stale "$$d" "IC020 [当たらない項目] 許可リストの項目 modules/a#resource.aws_instance.gone"; \
	d=$$(case_dir listed); printf 'resource "aws_instance" "i" {}\n' > "$$d/modules/a/ec2.tf"; \
	decl "$$d" '{"id": "modules/a#resource.aws_instance.i", "owner": "external", "repository": "github.com/o/r"}'; \
	expect pass listed "$$d" "検出の規則が評価され"; \
	d=$$(case_dir removed); printf 'removed {\n  from = aws_s3_bucket.old\n  lifecycle {\n    destroy = false\n  }\n  provisioner "local-exec" {\n    when    = destroy\n    command = "x"\n  }\n}\n' > "$$d/modules/a/removed.tf"; \
	expect fail removed-provisioner "$$d" "modules/a#removed.aws_s3_bucket.old（provisioner）"; \
	d=$$(case_dir provider-source); mkdir -p "$$d/modules/b"; printf 'terraform {\n  required_providers {\n    aws = {\n      source = "evil/aws"\n    }\n  }\n}\n\nresource "aws_s3_bucket" "b" {}\n' > "$$d/modules/b/main.tf"; \
	expect fail provider-source "$$d" "IC013 [未分類の provider]"; \
	d=$$(case_dir symlink); mkdir -p "$$d/outside"; printf 'resource "aws_s3_bucket" "s" {}\n' > "$$d/outside/real.tf"; ln -s ../../outside/real.tf "$$d/modules/a/link.tf"; \
	expect fail symlinked-config "$$d" "modules/a/link.tf: 走査の root の下に symlink があります"; \
	d=$$(case_dir hcl-input-fails); \
	expect fail hcl-input-fails "$$d" "hcl-input が失敗したので、Conftest を起動しません" IC_HCL_INPUT_CMD="$$root/conftest-fails"; \
	expect fail hcl-input-stale "$$d" "hcl-input が成功を返しましたが、$(IC_OUT)/hcl-input.json を書いていません" IC_HCL_INPUT_CMD=:; \
	expect fail input-not-object "$$d" "IC005 [入力の形] 入力がオブジェクトではありません" "$$(stub "$$root/not-object.json")"; \
	expect fail input-malformed "$$d" "unexpected end of JSON input" "$$(stub "$$root/malformed.json")"; \
	expect fail input-missing-file "$$d" "IC005 [入力の形] modules/a/main.tf を構文として読んでいません（find は列挙しています）" "$$(stub "$$root/no-files.json")"; \
	d=$$(case_dir json-only); : > "$$root/conftest-args"; \
	expect pass conftest-input-json-only "$$d" "検出の規則が評価され" CONFTEST_BIN="$$root/conftest-recording"; \
	if grep -qxE 'usecases|modules|env|.*\.tf|.*\.tftest\.hcl' "$$root/conftest-args" || [ "$$(grep -c 'hcl-input\.json$$' "$$root/conftest-args")" != 1 ]; then \
		failed="$$failed conftest-input-json-only"; cat "$$root/conftest-args"; fi; \
	d=$$(case_dir no-detect-rules); \
	expect fail no-detect-rules "$$d" "検出の名前空間 \"no_such_namespace\" の規則が1つも評価されていません" IC_NS=no_such_namespace; \
	d=$$(case_dir no-conftest); \
	expect fail no-conftest "$$d" "/nonexistent/conftest" CONFTEST_BIN=/nonexistent/conftest; \
	d=$$(case_dir verify-fails); \
	expect_make pass verify-ok infra-code-verify "$$d" "走って通りました"; \
	expect_make fail verify-fails infra-code-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-fails"; \
	expect_make fail verify-runs-nothing infra-code-verify "$$d" "テストが1件も走っていません" CONFTEST_BIN="$$root/conftest-silent"; \
	expect_make fail verify-test-fails infra-code-verify "$$d" "テスト data.infra_code.test_IC010_deny_unclassified_type が FAIL です" CONFTEST_BIN="$$root/conftest-fail-line"; \
	mkdir -p "$$root/badfx/fixtures"; printf -- '-- m/a.tf --\nresource {\n' > "$$root/badfx/fixtures/a.txtar"; \
	expect_make fail verify-fixtures-broken infra-code-verify "$$d" "の fixture を JSON にできません" IC_FIXTURES="$$root/badfx/fixtures"; \
	expect_make fail verify-fixtures-missing infra-code-verify "$$d" "のケースは 0 件です" HCL_FIXTURES_CMD="$$root/fixtures-empty"; \
	expect_make fail decl-fails infra-code-decl-check "$$d" "no-such-decl.json" IC_DECL=no-such-decl.json; \
	expect_make fail gate-aggregates infra-code-gate "$$d" "❌ infra-code-gate が落ちた検査: infra-code-verify infra-code-decl-check infra-code-detect" \
		CONFTEST_BIN="$$root/conftest-fails" IC_DECL=no-such-decl.json; \
	d=$$(case_dir sibling-decl); printf 'resource "aws_not_classified" "c" {}\n' > "$$d/modules/a/c.tf"; \
	printf '{"infra_code_decl": {"types": {"resource.aws_not_classified": {"executes_code": false}}}}\n' > "$$d/injected.json"; \
	expect fail sibling-decl-is-not-read "$$d" "IC010 [未分類の型]"; \
	if [ -n "$$failed" ]; then echo "❌ infra-code-gate-test が期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ infra-code-gate-test: 全ケースが期待どおりでした"
