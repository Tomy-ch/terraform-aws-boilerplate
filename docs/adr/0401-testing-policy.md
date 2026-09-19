# ADR-0401: テストを第一級の設計要件とする

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0101, ADR-0102, ADR-0201, ADR-0202, ADR-0203, ADR-0207, ADR-0301

## Context

Infrastructure as Codeでは、構文的に正しいことと、期待するインフラが構築されることは一致しない。`terraform plan` の成功は、Terraformとして妥当な構成であることしか示さない。

さらに、AWS上でresourceが正常に作成できることと、次が成立することも別問題である。

- セキュリティ要件を満たす
- boilerplateとしての契約を満たす
- 意図したネットワーク境界を維持する
- 最小権限になっている
- 変更により既存ユースケースが破壊されない

これらは異なる故障モードであり、単一のテスト手段では検出できない。

一方、実AWS環境を用いるテストは、実行時間・費用・環境の後始末の面でコストが高い。すべての検証を実環境で行うと、変更のたびに実行できず、結果としてテストが形骸化する。

したがって、故障モードごとに検出手段を割り当て、実環境を必要とする範囲を最小化する必要がある。

## Decision

### 原則

1. Terraformコードは「`terraform plan` が成功する」ことのみをもって正しいと判断しない。
2. 異なる故障モードは、異なる種類のテストで検出する。
3. テスト方法を説明できない設定・機能を公開しない。検証できない主張を保証として扱わない（ADR-0101 決定9、ADR-0301 決定19）。

### レイヤーと責務

4. テストレイヤーとその責務を以下とする。

   | レイヤー | 検出する故障モード | 実AWS |
   | --- | --- | --- |
   | Static Analysis | 構文・書式の逸脱、既知の誤設定パターン、本リポジトリの規約違反（`any` の使用、`extra_*` variable、外部module source等） | 不要 |
   | Contract Test | 公開variable / outputのschemaの意図しない変更（ADR-0202 決定4） | 不要 |
   | Policy Test | security / reliability invariantの違反（ADR-0301 決定19） | 不要 |
   | Unit Test | 入力から構成を導出するロジックの誤り（条件分岐、意味論的入力からの導出） | 不要 |
   | Integration Test | resource間の結合、AWS API側の制約（作成順序、属性の相互排他、置換条件） | 必要 |
   | E2E Test | 利用者視点の振る舞い（到達性、実行結果、権限境界の実効性） | 必要 |

5. AWS実環境を必要とするのは Integration Test と E2E Test に限る。
6. Static Analysis / Contract Test / Policy Test / Unit Test は、AWS credentialsなしで実行可能であることを要件とする。Providerのmock機能を用いて実現する。

### テスト対象としない領域

7. 以下をテスト対象としない。

   - AWS Provider自体の挙動（Providerがschemaどおりに動作すること）
   - AWSサービス自体の可用性・性能
   - 利用者のアプリケーションコード
   - サポート対象外と宣言した構成（ADR-0102）

### 実行範囲

8. 実行契機ごとの実行範囲を以下とする。

   | 契機 | 実行するレイヤー |
   | --- | --- |
   | Pull Request | Static Analysis / Contract Test / Policy Test / Unit Test |
   | 定期実行（nightly等） | 上記に加えて Integration Test |
   | Release | 全レイヤー（E2E Test を含む） |
   | Provider major version 更新 | 全レイヤー |

9. Pull RequestでAWS実環境を必須としない。実環境テストの失敗により、実環境に依存しない検証がブロックされる状態を避ける。
10. 変更が特定ユースケースに閉じる場合、Integration / E2E Test の対象を当該ユースケースへ限定してよい。ただしReleaseでは全ユースケースを対象とする。

### fixtureとexample

11. 各サポート対象ユースケースは、最低1つのexampleを持つ。
12. exampleはdocumentationであると同時にtest fixtureとして扱う。テストから参照されないexampleを置かない。
13. exampleは、そのユースケースの推奨構成を表現する。サポート対象外の構成をexampleとして示さない。

### 実環境テストの扱い

14. Integration / E2E Test は、使い捨ての環境を作成・破棄する形で実行する。永続環境を前提としない。
15. 作成するresourceには、実行識別子を含むtagを付与する。
16. 破棄の失敗を検出し、残留resourceを特定できる手段を用意する。破棄失敗を検出できないテスト構成を採用しない。
17. コストを理由に実環境テストを削る場合、削ることで検出できなくなる故障モードを明示する（ADR-0207 決定14と同じ判断様式）。

### Policy as Code

18. security invariantの検証はPolicy Testとして実装し、レビューによる目視確認に依存しない。
19. Policy Testは、Terraformのplan出力（機械可読形式）を対象とする。実装形式の選定は本ADRで固定しない。
20. ADR-0301 決定19に該当するinvariantは、Policy Testとして実装されて初めて「保証されている」と扱う。

## 検討した代替案

### 案A: `terraform validate` と `plan` の成功のみを検証する

実行コストは最小だが、security invariant、公開契約、実際の振る舞いのいずれも検証できない。ADR-0101・ADR-0301が主張する保証を裏付けられない。

### 案B: 実環境テスト（Integration / E2E）中心に統一する

実挙動を直接検証できるが、Pull Requestごとの実行が現実的でなく、フィードバックが遅い。契約の破壊やsecurity invariantの違反を、実環境の失敗としてしか検出できない。

### 案C: 全レイヤーを全契機で実行する

網羅性は最大だが、費用と実行時間により実行頻度が下がり、結果的にテストが迂回される。

### 評価

| 評価軸 | 案A: plan成功のみ | 案B: 実環境中心 | 案C: 全契機で全実行 | 採用案: 層別 + 契機別 |
| --- | --- | --- | --- | --- |
| Security | 低 | 中（検出が遅い） | 高 | 高 |
| Testability | 低 | 中 | 高 | 高 |
| Contract Clarity | 低 | 中 | 高 | 高 |
| Reproducibility | 低 | 中 | 高 | 高 |
| 実行コスト | 低 | 高 | 高 | 中 |
| フィードバック速度 | 高 | 低 | 低 | 高 |

## 意図的に捨てるもの

- Pull Requestの時点で実環境の挙動まで検証すること
- AWS Provider自体およびAWSサービス自体の検証
- サポート対象外構成に対する検証

## 保証範囲

- 保証すること: サポート対象ユースケースについて、公開契約・security invariant・導出ロジック・実環境での振る舞いが、それぞれ対応するレイヤーで検証されていること
- 保証しないこと: 実環境テストを実行していない時点での、実環境における最新の挙動

## 検証方法

本ADR自体の遵守は、以下で検査する。

- 各サポート対象ユースケースにexampleが存在し、テストから参照されていること
- 各サポート対象ユースケースにContract TestおよびPolicy Testが存在すること
- Pull Request契機のテストが、AWS credentialsなしで完了すること

## 影響

- 新しい公開設定の追加は、検証手段の追加を伴う。
- 実環境テストの実行は契機が限定されるため、Provider更新時のレビュー範囲が重要になる（ADR-0206 決定12）。
- exampleの追加・変更はテスト資産の変更として扱われる。

## 見直し条件

- 実環境を必要とせずにIntegration相当の検証が可能な手段が実用化された場合
- Pull Request契機の検証で検出できない故障が、Release契機で反復して発見された場合
- 実環境テストのコストが、定期実行を維持できないレベルに達した場合
