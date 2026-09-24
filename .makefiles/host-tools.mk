## ホストで実行する道具の導入。**`mise install` は mise.toml の全エントリを入れる** —— ここが
## 入れるのはその部分集合、手元での実行がホストである道具だけである。
##
## terraform は ADR-0503 決定4 の表の「ホスト」行に当たり、理由は同 ADR 決定5 が持つ。AWS CLI は
## 同表に行が無く、ホストで実行する理由は ADR-0504 が持つ。検査ツールは同表の第2行
## （既定の経路はコンテナ）なので含めない。

.PHONY: host-tools-install ## ホストで実行する道具を mise.toml の版で導入

host-tools-install:
	@command -v mise >/dev/null 2>&1 \
		|| { echo "❌ mise が PATH にありません。README「使い方」を見てください。"; exit 1; }
	@mise install "aqua:hashicorp/terraform@1.16.2"
	@mise install "aqua:aws/aws-cli@2.36.40"
	@mise reshim
	@echo "✅ ホストで実行する道具を導入しました。"
