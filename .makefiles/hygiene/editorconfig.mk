## .editorconfig の宣言とファイルの一致の検査
##
## 引数を渡さないと git が追跡するファイルをすべて見る。.editorconfig-checker.json は空に保つ
## —— 除外を足すと、検査対象が黙って縮む。レシピがそれを強制する。
##
## 抑止は `// editorconfig-checker-disable` / `-enable` で囲んだ範囲に限り、印ごとに理由と撤回条件を
## 書く（ADR-0501 決定13）。ファイルや形式の単位で外さない。

.PHONY: editorconfig-check ## ファイルが .editorconfig の宣言に一致することを検査
editorconfig-check: editorconfig-check-canary
	@set -eu; test "$$(tr -d '[:space:]' < .editorconfig-checker.json)" = '{}' || { \
		echo "❌ editorconfig-check: .editorconfig-checker.json が空ではありません" >&2; exit 2; }; \
	n=$$($(GO_TOOL) editorconfig-checker -dry-run | grep -c .) || n=0; \
	test "$$n" -gt 0 || { echo "❌ editorconfig-check: 走査対象のファイルが0件です" >&2; exit 2; }; \
	echo "editorconfig-check: $$n ファイル" >&2; \
	$(GO_TOOL) editorconfig-checker

## 本走査と同じ探索経路（git の追跡ファイル）で違反が落ちることを確かめる。
.PHONY: editorconfig-check-canary ## editorconfig-checker が追跡ファイルの違反で落ちることを検査
editorconfig-check-canary:
	@set -eu; mkdir -p tmp; dir=$$(mktemp -d tmp/editorconfig-canary.XXXXXX); \
	trap 'rm -rf "$$dir"' EXIT; \
	cp .editorconfig .editorconfig-checker.json "$$dir/"; \
	printf 'a:\n\tb: c\n' > "$$dir/canary.yaml"; \
	$(CANARY_GIT) -C "$$dir" init -q; $(CANARY_GIT) -C "$$dir" add -A; \
	rc=0; out=$$($(GO_TOOL) sh -c 'cd "$$1" && exec editorconfig-checker' sh "$$dir" 2>&1) || rc=$$?; \
	case "$$rc:$$out" in \
	0:*) echo "❌ editorconfig-check-canary: tab で字下げした YAML で落ちませんでした" >&2; exit 1 ;; \
	*canary.yaml*'wrong indent'*) ;; \
	*) echo "❌ editorconfig-check-canary: 想定外の失敗です (rc=$$rc)" >&2; echo "$$out" >&2; exit 1 ;; \
	esac; \
	echo "✅ editorconfig-check-canary: tab で字下げした YAML で落ちました" >&2
