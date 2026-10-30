## .editorconfig の宣言とファイルの一致の検査
##
## 引数を渡さないと git が追跡するファイルをすべて見る。.editorconfig-checker.json は空に保つ
## —— 除外を足すと、検査対象が黙って縮む。
##
## 抑止は範囲を開く印と閉じる印で囲んだ範囲に限り、開く印に理由と撤回条件を書く
## （ADR-0501 決定13）。ファイルや行の単位で外さない。書き方は scripts/ の *_test.go にある実例に倣う。
## **散文で印に触れるときは綴りを書かない。** editorconfig-checker は印を行の部分文字列として読むので、
## 引用しただけでそのファイルの残りが検査の外に出る。editorconfig-directive-lint がそれを落とす。

.PHONY: editorconfig-check ## ファイルが .editorconfig の宣言に一致することを検査
editorconfig-check: editorconfig-check-canary editorconfig-directive-canary editorconfig-directive-lint
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

.PHONY: editorconfig-directive-lint ## editorconfig-checker の無効化の印が、理由つきの閉じた範囲だけであることを検査
editorconfig-directive-lint:
	@set -eu; mkdir -p tmp; trap 'rm -f tmp/editorconfig-directive-lint.files' EXIT; \
	git -c core.quotepath=false ls-files | while IFS= read -r f; do [ -e "$$f" ] && printf '%s\n' "$$f"; done \
		> tmp/editorconfig-directive-lint.files; \
	$(call RUN_SCRIPT,editorconfig-directive-lint,-files tmp/editorconfig-directive-lint.files)

## editorconfig-directive-lint が写している前提 —— 範囲を開く印を閉じなければ、そのファイルの残りは
## 検査されない —— を、本物の editorconfig-checker で確かめる。道具の版を上げて読み方が変われば
## ここが落ち、lint の分類を見直す合図になる。印の綴りはこのファイルに書けないので連結で組む。
.PHONY: editorconfig-directive-canary ## editorconfig-checker が閉じない無効化の印の後ろを検査しないことを検査
editorconfig-directive-canary:
	@set -eu; mkdir -p tmp; dir=$$(mktemp -d tmp/editorconfig-directive-canary.XXXXXX); \
	trap 'rm -rf "$$dir"' EXIT; \
	cp .editorconfig .editorconfig-checker.json "$$dir/"; \
	printf '# %s%s\na:\n\tb: c\n' 'editorconfig-' 'checker-disable' > "$$dir/canary.yaml"; \
	printf 'a:\n\tb: c\n' > "$$dir/control.yaml"; \
	$(CANARY_GIT) -C "$$dir" init -q; $(CANARY_GIT) -C "$$dir" add -A; \
	rc=0; out=$$($(GO_TOOL) sh -c 'cd "$$1" && exec editorconfig-checker' sh "$$dir" 2>&1) || rc=$$?; \
	case "$$rc:$$out" in \
	0:*) echo "❌ editorconfig-directive-canary: 印の無い対照の違反でも落ちませんでした。走査が働いていません" >&2; exit 1 ;; \
	*canary.yaml*) echo "❌ editorconfig-directive-canary: 閉じない印の後ろの違反を検査しました。editorconfig-directive-lint の分類を見直す" >&2; \
		echo "$$out" >&2; exit 1 ;; \
	*control.yaml*) ;; \
	*) echo "❌ editorconfig-directive-canary: 想定外の失敗です (rc=$$rc)" >&2; echo "$$out" >&2; exit 1 ;; \
	esac; \
	echo "✅ editorconfig-directive-canary: 閉じない印の後ろは検査されませんでした（editorconfig-directive-lint の前提どおり）" >&2
