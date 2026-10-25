## Semgrep CE による Terraform の検査（ADR-0501 決定6・19）
##
## ルールは semgrep-rules の commit で固定し、リポジトリに置かずに実行時に取得する（Semgrep Rules
## License v1.0 はルールの配布を許さない）。取得はホストで行い、走査そのものには network を与えない。
##
## 所見があれば severity に関わらず落とす（`--error`）。例外は該当行の直前の
## `# nosemgrep: <rule-id>` で持つ（ADR-0501 決定13）。rule-id は `terraform.` から始まる。
## 既定の .semgrepignore は tests/ を外すので、空の .semgrepignore を置いて Trivy と同じ面を見る。
SEMGREP_RULES_COMMIT := 16b9e6d45f10649f6a531ec4838ed9924372c3bc
SEMGREP_RULES_TGZ := tmp/semgrep-rules-$(SEMGREP_RULES_COMMIT).tar.gz
SEMGREP_WORK := tmp/semgrep/work
SEMGREP_OUT := tmp/semgrep/out
SEMGREP_FLAGS := semgrep scan --config terraform --metrics off --disable-version-check --error \
	--json-output=/tmp/out/results.json --sarif-output=/tmp/out/results.sarif /work/src

# $(1) の cfg/ へ固定した commit の terraform/ の rule を写す。
define SEMGREP_STAGE_RULES
rules=$$(mktemp -d tmp/semgrep-rules.XXXXXX); \
tar -xzf $(SEMGREP_RULES_TGZ) -C "$$rules" --strip-components 1; \
mkdir -p "$(1)/cfg"; \
(cd "$$rules" && find terraform -name '*.yaml' | tar -cf - -T -) | tar -xf - -C "$(1)/cfg"; \
rm -rf "$$rules"; \
[ -n "$$(find "$(1)/cfg/terraform" -name '*.yaml' | head -1)" ] \
	|| { echo "❌ $@: semgrep-rules $(SEMGREP_RULES_COMMIT) に terraform/ の rule がありません" >&2; exit 1; }; \
: > "$(1)/src/.semgrepignore"
endef

$(SEMGREP_RULES_TGZ):
	@mkdir -p tmp
	@curl -fsSL "https://codeload.github.com/semgrep/semgrep-rules/tar.gz/$(SEMGREP_RULES_COMMIT)" -o "$@.part"
	@mv "$@.part" "$@"

.PHONY: semgrep ## Terraform の誤設定を Semgrep CE で検査（所見があれば落ちる）
semgrep: semgrep-canary
	@set -eu; rm -rf $(SEMGREP_WORK) $(SEMGREP_OUT); $(call TF_SCAN_STAGE,$(SEMGREP_WORK)); \
	$(call SEMGREP_STAGE_RULES,$(SEMGREP_WORK)); \
	rc=0; $(call IMAGE_RUN,semgrep_runner,$(SEMGREP_WORK),$(SEMGREP_OUT),-w /work/cfg,$(SEMGREP_FLAGS)) || rc=$$?; \
	n=$$(jq '.paths.scanned // [] | length' $(SEMGREP_OUT)/results.json); \
	if [ "$$n" -eq 0 ]; then echo "❌ semgrep: 走査したファイルが0件です" >&2; exit 1; fi; \
	echo "semgrep: ファイル $$n 件を走査しました（終了コード $${rc}）" >&2; \
	exit $$rc

.PHONY: semgrep-canary ## semgrep が誰でも読み書きできる S3 Bucket を落とすことを検査
## 本走査と同じ条件で、ACL を public-read-write にした構成を落とすことを確かめる。固定した rule の取得と
## `--error` の両方が効いていることの証明でもある。
semgrep-canary: $(SEMGREP_RULES_TGZ)
	@set -eu; dir=tmp/semgrep-canary; rm -rf "$$dir"; mkdir -p "$$dir/work/src"; \
	printf '%s\n' 'resource "aws_s3_bucket" "canary" {' '  bucket = "canary"' '  acl    = "public-read-write"' '}' \
		> "$$dir/work/src/main.tf"; \
	$(call SEMGREP_STAGE_RULES,$$dir/work); \
	rc=0; $(call IMAGE_RUN,semgrep_runner,tmp/semgrep-canary/work,tmp/semgrep-canary/out,-w /work/cfg,$(SEMGREP_FLAGS)) >/dev/null || rc=$$?; \
	case "$$rc:$$(cat "$$dir/out/results.json" 2>/dev/null)" in \
	0:*) echo "❌ semgrep-canary: public-read-write の構成でゲートが落ちませんでした" >&2; exit 1 ;; \
	*'"check_id":"terraform.lang.security.s3-public-rw-bucket"'*) ;; \
	*) echo "❌ semgrep-canary: s3-public-rw-bucket を報告せずに終了しました（終了コード $${rc}）" >&2; exit 1 ;; \
	esac; \
	rm -rf "$$dir"; echo "✅ semgrep-canary: public-read-write の構成を検出しました" >&2
