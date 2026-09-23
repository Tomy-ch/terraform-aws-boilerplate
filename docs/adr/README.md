# Architecture Decision Records (repository-wide)

このディレクトリは、リポジトリ全体へ適用される設計判断を管理する。

特定ユースケースにのみ適用される設計判断は、ここではなく `modules/<use-case>/docs/adr/` へ配置する。配置・番号体系・昇格条件・レビュー観点は [ADR-0001](0001-adr-process-and-placement.md) に従う。

## 一覧

**番号の前二桁は群、後二桁は群内のスロットである**（ADR-0001 決定10-11）。群は読む順序と関心事の境界を表し、
新しい ADR はその関心事の群へ次のスロットで入る。

### 00 ADR の運用

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-adr-process-and-placement.md) | ADRの採用と配置・所有権ポリシー（番号は群とスロット、identityはslug） | Accepted |

### 01 アーキテクチャ原則とスコープ

| ID | タイトル | Status |
| --- | --- | --- |
| [0101](0101-architecture-principles.md) | 保証可能性を自由度より優先する | Accepted |
| [0102](0102-use-case-centric-scope.md) | ユースケース単位でスコープを定義する | Accepted |

### 02 公開契約と設計方針

| ID | タイトル | Status |
| --- | --- | --- |
| [0201](0201-public-interface-policy.md) | 公開インターフェースは意図を表現し、Provider schemaを露出しない | Accepted |
| [0202](0202-internal-topology-as-implementation-detail.md) | 内部のAWS resource構成を実装詳細として扱う | Accepted |
| [0203](0203-typed-configuration-policy.md) | 公開設定は型付きかつ有限とし、Raw JSONと非型付き設定を禁止する | Accepted |
| [0204](0204-no-generic-escape-hatch.md) | Generic Escape Hatchを提供しない | Accepted |
| [0205](0205-module-dependency-policy.md) | 外部Terraform moduleへ依存しない | Accepted |
| [0206](0206-official-implementations-as-reference.md) | 公式実装を参照実装（Knowledge Source）として利用する | Accepted |
| [0207](0207-default-value-policy.md) | 設定値は公式ベストプラクティスを根拠に決定し、利用者へ公開しない | Accepted |

### 03 セキュリティ

| ID | タイトル | Status |
| --- | --- | --- |
| [0301](0301-least-privilege-policy.md) | 最小権限をdefaultとし、安全な設定をopt-inにしない | Accepted |
| [0302](0302-secret-leak-detection.md) | 履歴への秘密の混入を独立した検証レイヤーとして扱う | Accepted |

### 04 テスト

| ID | タイトル | Status |
| --- | --- | --- |
| [0401](0401-testing-policy.md) | テストを第一級の設計要件とする | Accepted |
| [0402](0402-policy-test-scope-and-verification.md) | Policy Testの適用範囲と、その検証方法を定める | Accepted |

### 05 道具と実行環境

| ID | タイトル | Status |
| --- | --- | --- |
| [0501](0501-development-tooling-composition.md) | 検証と運用の道具を既存ツールの組み合わせで構成する | Accepted |
| [0502](0502-execution-engine-selection.md) | 実行エンジンにTerraformを採用し、OpenTofuを採らない | Accepted |
| [0503](0503-tool-execution-form.md) | 道具の宣言を1つに保ち、実行環境を3経路に分ける | Accepted |
| [0504](0504-credential-handling-tool-execution.md) | 実AWSの資格情報を扱う道具をホストで実行する | Accepted |

### 06 変更の経路と CI

| ID | タイトル | Status |
| --- | --- | --- |
| [0601](0601-change-delivery-path.md) | 変更経路をGitに限定し、applyをmerge後のCI/CDで行う | Accepted |
| [0602](0602-bootstrap-and-ci-authentication.md) | bootstrapをCLIとimportで立ち上げ、CI認証をOIDCとする | Accepted |
| [0603](0603-branch-protection-and-required-checks.md) | 分岐のパターンと保護設定を単一の宣言で管理し、required checkの基準を定める | Accepted |

### 07 文書と運用機構

| ID | タイトル | Status |
| --- | --- | --- |
| [0701](0701-documentation-ownership-and-language.md) | 文書の所有と言語を定める | Accepted |
| [0702](0702-repository-operations-substrate.md) | リポジトリ運用機構を独立した区分とし、Goで実装する | Accepted |

## 読む順序

ADR-0101 がリポジトリ全体の評価軸と原則を定義し、他のADRはその下位決定にあたる。初読時は次の順序を推奨する。

1. ADR-0101（前提と評価軸）
2. ADR-0102（スコープの単位）
3. ADR-0201 → ADR-0202 → ADR-0203 → ADR-0204（公開契約）
4. ADR-0205 → ADR-0206 → ADR-0207（依存と設定値の根拠）
5. ADR-0301（セキュリティ）
6. ADR-0401 → ADR-0501 → ADR-0402 → ADR-0302（検証のレイヤー、それを担う道具、検査そのものの検証、履歴に対する検査）
7. ADR-0601 → ADR-0602 → ADR-0603（変更が実環境へ届く経路、それを成立させるbootstrap、mergeを守るゲート）
8. ADR-0502（実行エンジンの選定）
9. ADR-0702 → ADR-0503 → ADR-0504（Terraformの外側にある運用の機構、道具の宣言・実行環境、そして資格情報を扱う道具の実行環境）
10. ADR-0001 → ADR-0701（ADRと文書自体の運用）

## 依存関係

```text
repository-wide ADR
        |
        v
use-case ADR  (modules/<use-case>/docs/adr/)
        |
        v
Terraform implementation
        |
        v
AWS resources
```

AWS resourceは設計判断の結果であり、ADR体系の起点ではない。依存の向きはこの一方向に限る（ADR-0001 決定17-19）。

## 新規ADRの追加

1. [template.md](template.md) を複製する。
2. 適用範囲を判断し、配置先を決める（ADR-0001 決定5-9、決定20のチェックリスト）。
3. 代替案を ADR-0101 の評価軸で比較する。
4. 検証方法を記述する。機械検証できない決定は、決定として不完全とみなす（ADR-0401 決定3）。
