## node_tools 段の pnpm と、それが入れる道具の lockfile（ADR-0503 決定14・17・18）
##
## lockfile の生成と照合は**ホストの pnpm で実行する**（理由は同 決定4）。pnpm は
## make host-tools-install が mise.toml の版で入れる。

.PHONY: node-tools-lock ## docker/tools/node の lockfile を package.json から作り直す
.PHONY: node-tools-lock-check ## docker/tools/node の lockfile が package.json と供給網の窓に合うか検証（書き換えなし・CI/hook用）
.PHONY: node-tools-image-check ## node_tools のイメージを組み、pnpm の照合と lockfile の frozen install を実際に通す
.PHONY: pin-pnpm-resolve ## mise.toml の pnpm の版と配布物の sha256 を Dockerfile へ書く
.PHONY: pin-pnpm-check ## Dockerfile の pnpm の版と照合値が宣言どおりか検証（書き換えなし・CI/hook用）

node-tools-lock:
	@pnpm -C $(NODE_TOOLS_DIR) install --lockfile-only

## 供給網の窓（pnpm-workspace.yaml の minimumReleaseAge）も、--frozen-lockfile がここで照らす。
node-tools-lock-check:
	@pnpm -C $(NODE_TOOLS_DIR) install --frozen-lockfile --lockfile-only

pin-pnpm-resolve:
	@$(call RUN_SCRIPT,pin-pnpm,resolve)

pin-pnpm-check:
	@$(call RUN_SCRIPT,pin-pnpm,check)

## pin-pnpm-check と node-tools-lock-check は通信や照合の手前までしか見ない。配布物の sha256 と
## lockfile の integrity の照合は、イメージを組んだときにだけ走る。キャッシュした層では照合が
## 走らないので --no-cache で組む。
node-tools-image-check:
	@docker build --no-cache --target node_tools -f docker/tools/Dockerfile -t node-tools-image-check .

.PHONY: node-tools-host-canary ## ホストの npm の道具の前置きが、lockfile のずれと integrity の不一致で起動前に落ち、PATH へ退かないか検証
.PHONY: mise-npm-disabled-check ## mise が mise.toml の npm: の道具を入れる対象にしていないか検証

## 写しの package.json の版をずらしたものと、lockfile の integrity を壊したものを NODE_TOOLS_DIR に
## 差し替え、RUNNER_MODE=host の make md-lint を起動する —— 前置きの定義ではなく、検査が実際に通る
## NODE_TOOL を当てるため。どちらも pnpm の該当のエラーで落ち、PATH の先頭に置いた同名の偽の道具が
## 起動されないことを要求する。integrity の側は tarball を取得するので registry へ通信する。
node-tools-host-canary:
	@set -eu; mkdir -p tmp; root=$$(mktemp -d "$(CURDIR)/tmp/node-tools-host-canary.XXXXXX"); \
	trap 'rm -rf "$$root"' EXIT; \
	mkdir "$$root/bin"; \
	printf '#!/bin/sh\necho NODE_TOOLS_HOST_CANARY_FALLBACK\n' > "$$root/bin/markdownlint-cli2"; \
	chmod +x "$$root/bin/markdownlint-cli2"; \
	for c in drift integrity; do \
		mkdir "$$root/$$c"; \
		cp $(NODE_TOOLS_DIR)/package.json $(NODE_TOOLS_DIR)/pnpm-lock.yaml $(NODE_TOOLS_DIR)/pnpm-workspace.yaml "$$root/$$c/"; \
	done; \
	sed -i.orig 's/"markdownlint-cli2": "[^"]*"/"markdownlint-cli2": "0.0.0"/' "$$root/drift/package.json"; \
	sed -i.orig '/^  markdownlint-cli2@/{n;s/integrity: sha512-..../integrity: sha512-AAAA/;}' "$$root/integrity/pnpm-lock.yaml"; \
	sed -i.orig '/^  markdownlint-cli2@/{n;s/integrity: sha512-AAAA/integrity: sha512-BBBB/;}' "$$root/integrity/pnpm-lock.yaml"; \
	for c in drift:package.json:ERR_PNPM_OUTDATED_LOCKFILE integrity:pnpm-lock.yaml:ERR_PNPM_TARBALL_INTEGRITY; do \
		name=$${c%%:*}; rest=$${c#*:}; file=$${rest%%:*}; want=$${rest#*:}; \
		if cmp -s "$$root/$$name/$$file" "$(NODE_TOOLS_DIR)/$$file"; then \
			echo "❌ node-tools-host-canary: $$name の写しを書き換えられませんでした（$$file の形が変わった）" >&2; exit 1; \
		fi; \
		rel=tmp/$${root##*/}; \
		rc=0; out=$$(PATH="$$root/bin:$$PATH" $(MAKE) -s --no-print-directory md-lint RUNNER_MODE=host \
			NODE_TOOLS_DIR="$$rel/$$name" NODE_INSTALL_LOCK="$$rel/lock" 2>&1) || rc=$$?; \
		case "$$rc:$$out" in \
		*NODE_TOOLS_HOST_CANARY_FALLBACK*) echo "❌ node-tools-host-canary: $$name で PATH の同名の道具へ退きました" >&2; exit 1 ;; \
		0:*) echo "❌ node-tools-host-canary: $$name の写しで道具が起動しました（frozen の install が外れた）" >&2; exit 1 ;; \
		*"$$want"*) ;; \
		*) echo "❌ node-tools-host-canary: $$name が $$want 以外の理由で落ちました（終了コード $$rc）" >&2; printf '%s\n' "$$out" >&2; exit 1 ;; \
		esac; \
	done; \
	echo "✅ node-tools-host-canary: lockfile のずれと integrity の不一致で、道具の起動の前に落ちました" >&2

## mise.toml が npm: の道具を宣言していることと、外す集合がその道具と一致することは
## make versions-check が見る。ここが見るのは、mise がその宣言を実際に守るかだけである。
mise-npm-disabled-check:
	@set -eu; \
	json=$$(mise ls --current --local --json); \
	[ "$$(printf '%s' "$$json" | jq 'keys | length')" -gt 0 ] || { echo "❌ mise-npm-disabled-check: mise ls が道具を1つも返しません" >&2; exit 1; }; \
	active=$$(printf '%s' "$$json" | jq -r 'keys[] | select(startswith("npm:"))'); \
	[ -z "$$active" ] || { echo "❌ mise-npm-disabled-check: mise が npm: の道具を入れる対象にしています: $$active" >&2; exit 1; }; \
	echo "✅ mise-npm-disabled-check: mise は npm: の道具を入れる対象にしていません" >&2
