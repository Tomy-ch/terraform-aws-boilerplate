# セキュリティポリシー

## 脆弱性の報告

**公開の issue で報告しないでください。** issue は誰でも読めるため、報告そのものが未修正の
脆弱性の公表になります。

報告は GitHub の **Private Vulnerability Reporting** を使ってください。リポジトリの
**Security** タブ → **Report a vulnerability** から、メンテナだけが読める形で送れます。

テンプレートから作ったリポジトリでこの機能が無効な場合は、リポジトリのオーナーへ直接連絡してください。
<!-- = 連絡先: <security@example.com>（作った側で差し替えてください） -->

報告に含めてほしいもの:

- 影響を受けるバージョン、または commit
- 再現手順（最小の再現コードがあれば添えてください）
- 想定される影響（何が読めるか / 何が書けるか / 何が止まるか）

## 対応の流れ

| 段階 | 目安 |
| --- | --- |
| 受領の連絡 | 3 営業日以内 |
| 一次評価（影響範囲と severity の判断） | 7 営業日以内 |
| 修正版の公開 | 評価の結果に応じて調整し、報告者へ都度連絡します |

`high` 以上は 48 時間以内に対応へ着手します。

## サポート対象

| バージョン | サポート |
| --- | --- |
| 最新のリリース | ✅ |
| それ以前 | ❌ |

これは**テンプレートリポジトリ**であり、配信される成果物を持ちません。作った側が自分の運用に
合わせて上の表を書き替えてください。

## このリポジトリが自分に掛けている検査

検出手段の割当は [ADR-0501](docs/adr/0501-development-tooling-composition.md) 決定4 が、秘密の混入を独立した層として扱う判断は [ADR-0302](docs/adr/0302-secret-leak-detection.md) が持ちます。

| 層 | 手段 | どこで走るか |
| --- | --- | --- |
| 秘密の混入 | gitleaks（権威）+ Trivy secret | pre-push hook と CI（Pull Request は作業ツリー、週次で履歴全体） |
| 依存の脆弱性 | Trivy fs | CI。Pull Request では報告のみで、ブロックは昇格時に判定する |
| **この Pull Request が増やした依存** | Dependency Review | CI（Pull Request の差分だけを見る） |
| 構成の誤設定 | Trivy config | CI（CRITICAL / HIGH でブロック） |
| ワークフロー定義 | zizmor（high でブロック）/ actionlint / shellcheck | pre-commit hook と CI |
| ランナーの外向き通信 | harden-runner（`egress-policy: block`） | 全 job。許可リストの SSOT は `.github/egress.toml` |
| 供給網の新しすぎる版 | go-cooldown / tool-cooldown | Pull Request と週次 |
| 参照の固定 | pin-actions / pin-images | CI（action は commit、イメージは digest へ固定） |
| 依存の更新 | Dependabot + cooldown | 週次 |

それ以外は所見を見せるだけにしています。赤が常態になると、赤を見て手を止める習慣のほうが先に壊れるためです。

**検出を許容する場合は、抑止ファイルに理由と撤回条件を書きます。** 一括無効化はしません
（[ADR-0501](docs/adr/0501-development-tooling-composition.md) 決定13）。
