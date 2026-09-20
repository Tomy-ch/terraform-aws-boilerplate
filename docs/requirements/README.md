# 要件定義書

このディレクトリは、v1.0 で何を実装し何を実装しないかを決める要件定義書を管理する。

ADR との関係は次のとおりである。**ADR は決定そのものを所有し、要件定義書は v1.0 の範囲を所有する。** 要件定義書は Accepted ADR を上書きしない。両者が食い違った場合は ADR が上位であり、要件定義書の側を直すか、ADR-0001 決定2-3 に従って supersede する。

## 一覧

| ファイル | 内容 |
| --- | --- |
| [v0.2.md](v0.2.md) | v1.0 採用ユースケース、接続シナリオ、横断要件、検証戦略、受入条件、残る判断 |
| [attachment-parent-args.md](attachment-parent-args.md) | AWS Provider の attachment 型 resource と「親を指す引数」の対応表。v0.2 第3.1節の層検査(3)が判定に使う |

`attachment-parent-args.md` は Policy Test の data になる表である。Policy Test を配線したとき、そちらへ移す。
