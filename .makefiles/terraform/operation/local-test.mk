## 人が手元から Terraform を動かす入口（local.mk）の検査
##
## 実 AWS もネットワークも使わない。偽の aws / terraform を AWS_BIN / TF_BIN で差し替え、受け取った
## 引数と AWS_ の環境変数をファイルへ書かせる。fixture の root は TF_BASE で差し替える。

TF_LOCAL_TEST_DIR = tmp/tf-local-test

# 偽の aws。aws.out の中身を stdout へ出し、aws.rc の値で終わる。
define TF_LOCAL_FAKE_AWS
printf '#!/bin/sh\nd=$$(dirname "$$0")\nprintf "%%s\\n" "$$*" > "$$d/aws.args"\nenv | grep "^AWS_" | sort > "$$d/aws.env" || true\nif [ -f "$$d/aws.out" ]; then cat "$$d/aws.out"; fi\nexit $$(cat "$$d/aws.rc" 2>/dev/null || echo 0)\n' > "$$1/aws"; \
chmod +x "$$1/aws"
endef

# 偽の terraform。呼ばれるたびに引数を1行追記する。tf.fail-plan があれば plan だけ失敗する。
define TF_LOCAL_FAKE_TERRAFORM
printf '#!/bin/sh\nd=$$(dirname "$$0")\nprintf "%%s\\n" "$$*" >> "$$d/tf.args"\nenv | grep -E "^(AWS_|TF_CLI_ARGS|TF_WORKSPACE=|TF_DATA_DIR=)" | sort > "$$d/tf.env" || true\necho "fake terraform output"\ncase " $$* " in *" plan "*) if [ -f "$$d/tf.fail-plan" ]; then exit 1; fi;; esac\nexit 0\n' > "$$1/terraform"; \
chmod +x "$$1/terraform"
endef

.PHONY: tf-local-test ## 手元の init / plan の入口が、資格情報と Bucket 名を誤ったまま terraform を呼ばないことを検査
tf-local-test:
	@set -u; mkdir -p tmp; root=$$(mktemp -d "$(TF_LOCAL_TEST_DIR).XXXXXX") || exit 1; root="$$PWD/$$root"; \
	trap 'rm -rf "$$root"' EXIT; failed=""; \
	bucket=tabp-admin-tfstate-123456789012-ap-northeast-1-an; \
	new() { \
		c="$$root/$$1"; mkdir -p "$$c/bin" "$$c/base/env/admin/identity"; \
		printf 'terraform {\n  backend "s3" {\n    key    = "identity/terraform.tfstate"\n    region = "ap-northeast-1"\n  }\n}\n' > "$$c/base/env/admin/identity/backend.tf"; \
		printf 'provider "aws" {\n  region = "ap-northeast-1"\n}\n' > "$$c/base/env/admin/identity/providers.tf"; \
		set -- "$$c/bin"; $(TF_LOCAL_FAKE_AWS); $(TF_LOCAL_FAKE_TERRAFORM); \
		printf '123456789012\tarn:aws:iam::123456789012:user/bootstrap\n' > "$$c/bin/aws.out"; \
	}; \
	run() { \
		c="$$root/$$1"; shift; \
		out=$$($(MAKE) -s --no-print-directory "$$@" TF_BASE="$$c/base" AWS_BIN="$$c/bin/aws" TF_BIN="$$c/bin/terraform" TF_PLAN_LOG_DIR="$$c/log" 2>&1); rc=$$?; \
	}; \
	bad() { failed="$$failed $$1"; echo "--- $$1"; echo "$$out"; }; \
	no_terraform() { if [ -e "$$root/$$1/bin/tf.args" ]; then bad "$$1:terraform-called"; fi; }; \
	no_aws() { if [ -e "$$root/$$1/bin/aws.args" ]; then bad "$$1:aws-called"; fi; }; \
	new ok; run ok init-admin-identity; \
	if [ "$$rc" -ne 0 ]; then bad ok:rc; fi; \
	if [ "$$(cat "$$root/ok/bin/aws.args" 2>/dev/null)" != "sts get-caller-identity --region ap-northeast-1 --query [Account,Arn] --output text" ]; then bad ok:aws-args; fi; \
	if [ "$$(cat "$$root/ok/bin/tf.args" 2>/dev/null)" != "-chdir=$$root/ok/base/env/admin/identity init -input=false -lockfile=readonly -backend-config=bucket=$$bucket" ]; then bad ok:terraform-args; fi; \
	if ! grep -qF -- "== \"$$bucket\"" env/admin/identity/tests/imports.tftest.hcl; then bad ok:bucket-name-drift; fi; \
	for v in empty short; do \
		new "account-$$v"; \
		case "$$v" in empty) : > "$$root/account-$$v/bin/aws.out";; short) printf '12345678901\tarn:aws:iam::12345678901:user/bootstrap\n' > "$$root/account-$$v/bin/aws.out";; esac; \
		run "account-$$v" init-admin-identity; if [ "$$rc" -eq 0 ]; then bad "account-$$v:rc"; fi; no_terraform "account-$$v"; \
	done; \
	new aws-fails; echo 255 > "$$root/aws-fails/bin/aws.rc"; : > "$$root/aws-fails/bin/aws.out"; \
	run aws-fails init-admin-identity; if [ "$$rc" -eq 0 ]; then bad aws-fails:rc; fi; no_terraform aws-fails; \
	new root-user; printf '123456789012\tarn:aws:iam::123456789012:root\n' > "$$root/root-user/bin/aws.out"; \
	run root-user init-admin-identity; if [ "$$rc" -eq 0 ]; then bad root-user:rc; fi; no_terraform root-user; \
	new env; \
	c="$$root/env"; out=$$(AWS_ACCESS_KEY_ID=x AWS_PROFILE=other AWS_CONTAINER_CREDENTIALS_FULL_URI=http://127.0.0.1/ \
		TF_CLI_ARGS=-x TF_CLI_ARGS_init=-x TF_CLI_ARGS_plan=-target=a.b TF_WORKSPACE=w TF_DATA_DIR=/tmp/x \
		$(MAKE) -s --no-print-directory plan-admin-identity TF_BASE="$$c/base" AWS_BIN="$$c/bin/aws" TF_BIN="$$c/bin/terraform" TF_PLAN_LOG_DIR="$$c/log" 2>&1) || bad env:rc; \
	want=$$(printf 'AWS_EC2_METADATA_DISABLED=true\nAWS_PROFILE=tabp-bootstrap'); \
	for f in aws.env tf.env; do if [ "$$(cat "$$c/bin/$$f" 2>/dev/null)" != "$$want" ]; then bad "env:$$f"; fi; done; \
	new no-root; rm -rf "$$root/no-root/base/env/admin/identity"; \
	run no-root init-admin-identity; if [ "$$rc" -eq 0 ]; then bad no-root:rc; fi; no_aws no-root; \
	for v in missing twice malformed; do \
		new "region-$$v"; f="$$root/region-$$v/base/env/admin/identity/backend.tf"; \
		case "$$v" in \
			missing) printf 'terraform {\n  backend "s3" {}\n}\n' > "$$f";; \
			twice) printf 'terraform {\n  backend "s3" {\n    region = "ap-northeast-1"\n    region = "us-east-1"\n  }\n}\n' > "$$f";; \
			malformed) printf 'terraform {\n  backend "s3" {\n    region = "Tokyo"\n  }\n}\n' > "$$f";; \
		esac; \
		run "region-$$v" init-admin-identity; if [ "$$rc" -eq 0 ]; then bad "region-$$v:rc"; fi; no_aws "region-$$v"; \
	done; \
	new plan; mkdir -p "$$root/plan/log"; : > "$$root/plan/log/admin-identity.log"; chmod 644 "$$root/plan/log/admin-identity.log"; run plan plan-admin-identity; \
	if [ "$$rc" -ne 0 ]; then bad plan:rc; fi; \
	if [ "$$(sed -n 2p "$$root/plan/bin/tf.args" 2>/dev/null)" != "-chdir=$$root/plan/base/env/admin/identity plan -lock=false -input=false -no-color" ]; then bad plan:terraform-args; fi; \
	if ! sed -n 1p "$$root/plan/bin/tf.args" 2>/dev/null | grep -q ' init '; then bad plan:init-first; fi; \
	log="$$root/plan/log/admin-identity.log"; \
	if ! sed -n 1p "$$log" 2>/dev/null | grep -q '^commit: [0-9a-f]\{40\}'; then bad plan:log-commit; fi; \
	if ! grep -qF 'fake terraform output' "$$log" 2>/dev/null; then bad plan:log-output; fi; \
	if [ "$$(ls -l "$$log" 2>/dev/null | cut -c1-10)" != "-rw-------" ]; then bad plan:log-mode; fi; \
	new plan-fails; touch "$$root/plan-fails/bin/tf.fail-plan"; run plan-fails plan-admin-identity; \
	if [ "$$rc" -eq 0 ]; then bad plan-fails:rc; fi; \
	if ! grep -qF 'fake terraform output' "$$root/plan-fails/log/admin-identity.log" 2>/dev/null; then bad plan-fails:log-output; fi; \
	new no-git; c="$$root/no-git"; printf '#!/bin/sh\nexit 128\n' > "$$c/bin/git"; chmod +x "$$c/bin/git"; \
	out=$$(PATH="$$c/bin:$$PATH" $(MAKE) -s --no-print-directory plan-admin-identity TF_BASE="$$c/base" AWS_BIN="$$c/bin/aws" TF_BIN="$$c/bin/terraform" TF_PLAN_LOG_DIR="$$c/log" 2>&1); rc=$$?; \
	if [ "$$rc" -eq 0 ]; then bad no-git:rc; fi; \
	if grep -q ' plan ' "$$c/bin/tf.args" 2>/dev/null; then bad no-git:plan-ran; fi; \
	new login; c="$$root/login"; out=$$(AWS_PROFILE=other AWS_ACCESS_KEY_ID=x \
		$(MAKE) -s --no-print-directory aws-login AWS_BIN="$$c/bin/aws" 2>&1) || bad login:rc; \
	if [ "$$(cat "$$c/bin/aws.args" 2>/dev/null)" != "login --profile tabp-bootstrap" ]; then bad login:aws-args; fi; \
	if [ -s "$$c/bin/aws.env" ]; then bad login:aws-env; fi; \
	if [ -n "$$failed" ]; then echo "❌ tf-local-test: 期待どおりでなかったケース:$$failed"; exit 1; fi; \
	echo "✅ tf-local-test: 全ケースが期待どおりでした"
