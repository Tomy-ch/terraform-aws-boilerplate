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

## 照合の前に、ホストの前置きの canary を同じ起動の中で走らせる（scripts/node-tools）。
node-tools-lock-check:
	@$(call RUN_SCRIPT_HOST,node-tools,lock-check -dir $(NODE_TOOLS_DIR))

pin-pnpm-resolve:
	@$(call RUN_SCRIPT,pin-pnpm,resolve)

pin-pnpm-check:
	@$(call RUN_SCRIPT,pin-pnpm,check)

## pin-pnpm-check と node-tools-lock-check は通信や照合の手前までしか見ない。配布物の sha256 と
## lockfile の integrity の照合は、イメージを組んだときにだけ走る。キャッシュした層では照合が
## 走らないので --no-cache で組む。
node-tools-image-check:
	@docker build --no-cache --target node_tools -f docker/tools/Dockerfile -t node-tools-image-check .

.PHONY: mise-npm-disabled-check ## mise が mise.toml の npm: の道具を入れる対象にしていないか検証

mise-npm-disabled-check:
	@$(call RUN_SCRIPT_HOST,node-tools,mise-npm-disabled)
