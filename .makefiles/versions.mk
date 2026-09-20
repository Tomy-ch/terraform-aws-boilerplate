## 言語ランタイムの版の写しの同期
##
## 版の正本は mise.toml である（ADR-0501 決定19）。Dockerfile の `FROM golang:` や
## go.mod の `go` ディレクティブは同じ事実の写しであり、写しは放っておくと古くなる。
##
## **ずれてもビルドは通る。** 落ちるのは、ずれた版に無い機能を使ったときだけである。
## だから「ずれている」と「揃っている」を区別する検査が要る。

.PHONY: versions-apply ## mise.toml の版を Dockerfile / go.mod の写しへ反映
.PHONY: versions-check ## 写しが mise.toml 通りか検証（書き換えなし・CI/hook用）

versions-apply:
	@$(call RUN_SCRIPT,versions,apply)

versions-check:
	@$(call RUN_SCRIPT,versions,check)
