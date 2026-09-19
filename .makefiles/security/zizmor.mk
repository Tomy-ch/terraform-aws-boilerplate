## workflow の template injection の検査
##
## `${{ }}` はシェルがパースする**前**に置換されるため、`run:` に補間された式は文字列ではなく
## コードになる。shellcheck 系には構造的に見えないので、lint に束ねず横に並べる。
##
## 何も一括で無効にしない。設定は .github/zizmor.yml が持ち、ignore はファイル単位であるため、
## 同じ audit に当たる**新しい** workflow は落ちる。
##
## ゲートは high で切る。**閾値を明示するのは、既定に任せると「助言」までが merge を止めるため**
## である。動作に影響しない書き方の提案でゲートを落とすと、本当に止めるべき指摘が埋もれる。
## 助言を読む用途には zizmor-audit を使う。
ZIZMOR_TARGET := .github/workflows .github/actions
ZIZMOR_GATE_SEVERITY := high

.PHONY: zizmor ## workflow の template injection を high でゲート
zizmor:
	@$(GO_TOOL) zizmor --no-progress --min-severity $(ZIZMOR_GATE_SEVERITY) $(ZIZMOR_TARGET)

.PHONY: zizmor-audit ## 助言まで含めた全所見を表示（ゲートしない）
zizmor-audit:
	@$(GO_TOOL) zizmor --no-progress --persona auditor $(ZIZMOR_TARGET) || true

.PHONY: zizmor-sarif ## 全所見を SARIF で標準出力へ書き出す（code scanning 連携用）
##
## **severity で絞らない。** ゲートが high だけを見る一方で、code scanning には全所見を残す。
## 「落とさないが記録はする」層が、ここで成立する。
##
## 出力は純粋な SARIF でなければ code scanning が受け取れない。レシピ先頭の `@` がエコーを
## 抑えているが、呼び出し側は `make -s` も併せて渡すこと —— `@` が1つ落ちただけで、SARIF の
## 先頭にコマンド文字列が混ざる壊れ方をする。
zizmor-sarif:
	@$(GO_TOOL) zizmor --no-progress --format sarif $(ZIZMOR_TARGET)
