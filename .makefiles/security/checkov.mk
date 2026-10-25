## Checkov による Terraform の検査（ADR-0501 決定6）
##
## Checkov は severity を返さないので、所見が1件でもあれば落とす。例外は resource の中の
## `#checkov:skip=<ID>:<理由>` で持つ（ADR-0501 決定13）。`--skip-download` で Prisma Cloud からの
## 取得を止め、IMAGE_RUN が network も与えない。
CHECKOV_WORK := tmp/checkov/work
CHECKOV_OUT := tmp/checkov/out
CHECKOV_FLAGS := --framework terraform --skip-download --compact --quiet -o cli -o json -o sarif --output-file-path console,/tmp/out/results_json.json,/tmp/out/results_sarif.sarif
# results_json.json は framework が1つなら object、複数なら array になる。
CHECKOV_RESOURCE_COUNT := jq '[(if type == "array" then .[] else . end) | .summary.resource_count] | add // 0'

.PHONY: checkov ## Terraform の誤設定を Checkov で検査（所見があれば落ちる）
checkov: checkov-canary
	@set -eu; rm -rf $(CHECKOV_WORK) $(CHECKOV_OUT); $(call TF_SCAN_STAGE,$(CHECKOV_WORK)); \
	rc=0; $(call IMAGE_RUN,checkov_runner,$(CHECKOV_WORK),$(CHECKOV_OUT),,-d /work/src $(CHECKOV_FLAGS)) || rc=$$?; \
	n=$$($(CHECKOV_RESOURCE_COUNT) $(CHECKOV_OUT)/results_json.json); \
	if [ "$$n" -eq 0 ]; then echo "❌ checkov: 走査した resource が0件です" >&2; exit 1; fi; \
	echo "checkov: resource $$n 件を走査しました（終了コード $${rc}）" >&2; \
	exit $$rc

.PHONY: checkov-canary ## checkov が ACL を有効にした S3 Bucket を落とすことを検査
## 本走査と同じ条件で、ACL を有効にした構成（Trivy に検査が無い）を CKV2_AWS_65 で落とすことを確かめる。
checkov-canary:
	@set -eu; dir=tmp/checkov-canary; rm -rf "$$dir"; mkdir -p "$$dir/work/src"; \
	printf '%s\n' \
		'resource "aws_s3_bucket" "canary" {' '  bucket = "canary"' '}' \
		'resource "aws_s3_bucket_ownership_controls" "canary" {' '  bucket = aws_s3_bucket.canary.id' \
		'  rule {' '    object_ownership = "ObjectWriter"' '  }' '}' \
		'resource "aws_s3_bucket_acl" "canary" {' '  bucket = aws_s3_bucket.canary.id' '  acl    = "private"' '}' \
		> "$$dir/work/src/main.tf"; \
	rc=0; $(call IMAGE_RUN,checkov_runner,tmp/checkov-canary/work,tmp/checkov-canary/out,,-d /work/src $(CHECKOV_FLAGS)) >/dev/null || rc=$$?; \
	case "$$rc:$$(cat "$$dir/out/results_json.json" 2>/dev/null)" in \
	0:*) echo "❌ checkov-canary: ACL を有効にした構成でゲートが落ちませんでした" >&2; exit 1 ;; \
	*'"check_id": "CKV2_AWS_65"'*) ;; \
	*) echo "❌ checkov-canary: CKV2_AWS_65 を報告せずに終了しました（終了コード $${rc}）" >&2; exit 1 ;; \
	esac; \
	rm -rf "$$dir"; echo "✅ checkov-canary: ACL を有効にした構成を CKV2_AWS_65 で検出しました" >&2
