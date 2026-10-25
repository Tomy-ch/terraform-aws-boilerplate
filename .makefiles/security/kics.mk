## KICS による Terraform の検査（ADR-0501 決定6）
##
## 所見があれば severity に関わらず落とす（`--fail-on` の既定は5段すべて）。例外はファイルの先頭の
## `# kics-scan disable=<query_id>` で持つ（ADR-0501 決定13）。クラッシュ報告は DISABLE_CRASH_REPORT で止め、
## IMAGE_RUN が network も与えない。終了コードは所見の最も重い severity を表す（INFO 20 〜 CRITICAL 60）。
KICS_WORK := tmp/kics/work
KICS_OUT := tmp/kics/out
KICS_CREATE := -e DISABLE_CRASH_REPORT=0
KICS_FLAGS := scan -p /work/src --type Terraform --disable-secrets --no-progress --ci -o /tmp/out --report-formats json,sarif

.PHONY: kics ## Terraform の誤設定を KICS で検査（所見があれば落ちる）
kics: kics-canary
	@set -eu; rm -rf $(KICS_WORK) $(KICS_OUT); $(call TF_SCAN_STAGE,$(KICS_WORK)); \
	rc=0; $(call IMAGE_RUN,kics_runner,$(KICS_WORK),$(KICS_OUT),$(KICS_CREATE),$(KICS_FLAGS)) || rc=$$?; \
	want=$$(find $(KICS_WORK)/src -name '*.tf' | wc -l | tr -d ' '); \
	n=$$(jq -e '.files_scanned | if type == "number" then . else error("files_scanned が無い") end' $(KICS_OUT)/results.json); \
	if [ "$$n" -eq 0 ] || [ "$$n" -lt "$$want" ]; then \
		echo "❌ kics: 走査したファイル $$n 件が、写した .tf の $$want 件に届きません" >&2; exit 1; fi; \
	bad=$$(jq -e '[.files_scanned, .files_parsed, .queries_failed_to_execute] | if any(type != "number") then error("件数の欄が無い") else (.[0] - .[1]) + .[2] end' $(KICS_OUT)/results.json); \
	if [ "$$bad" -ne 0 ]; then \
		echo "❌ kics: 解釈できなかったファイルか実行できなかった query が $$bad 件あります（KICS は終了コードに出さない）" >&2; exit 1; fi; \
	echo "kics: ファイル $$n 件を走査しました（終了コード $${rc}）" >&2; \
	exit $$rc

.PHONY: kics-canary ## kics が Public Access Block を外した S3 Bucket を落とすことを検査
## 本走査と同じ条件で、Public Access Block の4属性を false にした構成を落とすことを確かめる。
kics-canary:
	@set -eu; dir=tmp/kics-canary; rm -rf "$$dir"; mkdir -p "$$dir/work/src"; \
	printf '%s\n' \
		'resource "aws_s3_bucket" "canary" {' '  bucket = "canary"' '}' \
		'resource "aws_s3_bucket_public_access_block" "canary" {' '  bucket                  = aws_s3_bucket.canary.id' \
		'  block_public_acls       = false' '  block_public_policy     = false' \
		'  ignore_public_acls      = false' '  restrict_public_buckets = false' '}' \
		> "$$dir/work/src/main.tf"; \
	rc=0; $(call IMAGE_RUN,kics_runner,tmp/kics-canary/work,tmp/kics-canary/out,$(KICS_CREATE),$(KICS_FLAGS)) >/dev/null || rc=$$?; \
	case "$$rc:$$(cat "$$dir/out/results.json" 2>/dev/null)" in \
	0:*) echo "❌ kics-canary: Public Access Block を外した構成でゲートが落ちませんでした" >&2; exit 1 ;; \
	*'"query_id": "d0cc8694-fcad-43ff-ac86-32331d7e867f"'*) ;; \
	*) echo "❌ kics-canary: Public Access Block の所見を報告せずに終了しました（終了コード $${rc}）" >&2; exit 1 ;; \
	esac; \
	rm -rf "$$dir"; echo "✅ kics-canary: Public Access Block を外した構成を検出しました" >&2

.PHONY: kics-canary-threshold ## kics が INFO の所見だけでも落ちることを検査
## 閾値が緩められると、INFO の所見は件数0の「所見なし」として通る。INFO の所見（tag 無し）だけが残る
## 構成を作り、本走査と同じ条件で終了コード 20（INFO）で落ちることを確かめる。
kics: kics-canary-threshold
kics-canary-threshold:
	@set -eu; dir=tmp/kics-canary-threshold; rm -rf "$$dir"; mkdir -p "$$dir/work/src"; \
	printf '%s\n' \
		'# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370' \
		'resource "aws_cloudwatch_log_group" "canary" {' '  name              = "canary"' \
		'  retention_in_days = 365' '  kms_key_id        = "arn:aws:kms:us-east-1:111122223333:key/canary"' '}' \
		> "$$dir/work/src/main.tf"; \
	rc=0; $(call IMAGE_RUN,kics_runner,tmp/kics-canary-threshold/work,tmp/kics-canary-threshold/out,$(KICS_CREATE),$(KICS_FLAGS)) >/dev/null || rc=$$?; \
	case "$$rc:$$(cat "$$dir/out/results.json" 2>/dev/null)" in \
	20:*'"query_id": "e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10"'*) ;; \
	0:*) echo "❌ kics-canary-threshold: INFO の所見だけの構成でゲートが落ちませんでした" >&2; exit 1 ;; \
	*) echo "❌ kics-canary-threshold: INFO の所見（e38a8e0a）だけで終了コード 20 になりませんでした（終了コード $${rc}）" >&2; exit 1 ;; \
	esac; \
	rm -rf "$$dir"; echo "✅ kics-canary-threshold: INFO の所見だけの構成で落ちました" >&2
