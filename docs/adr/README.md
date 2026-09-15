# Architecture Decision Records (repository-wide)

このディレクトリは、リポジトリ全体へ適用される設計判断を管理する。

特定ユースケースにのみ適用される設計判断は、ここではなく `modules/<use-case>/docs/adr/` へ配置する。配置・番号体系・昇格条件・レビュー観点は [ADR-0001](0001-adr-process-and-placement.md) に従う。

## 一覧

| ID | タイトル | Status |
| --- | --- | --- |
| [0001](0001-adr-process-and-placement.md) | ADRの採用とADR配置・所有権ポリシー | Accepted |
| [0002](0002-architecture-principles.md) | 保証可能性を自由度より優先する | Accepted |
| [0003](0003-use-case-centric-scope.md) | ユースケース単位でスコープを定義する | Accepted |
| [0004](0004-public-interface-policy.md) | 公開インターフェースは意図を表現し、Provider schemaを露出しない | Accepted |
| [0005](0005-internal-topology-as-implementation-detail.md) | 内部のAWS resource構成を実装詳細として扱う | Accepted |
| [0006](0006-typed-configuration-policy.md) | 公開設定は型付きかつ有限とし、Raw JSONと非型付き設定を禁止する | Accepted |
| [0007](0007-no-generic-escape-hatch.md) | Generic Escape Hatchを提供しない | Accepted |
| [0008](0008-module-dependency-policy.md) | 外部Terraform moduleへ依存しない | Accepted |
| [0009](0009-official-implementations-as-reference.md) | 公式実装を参照実装（Knowledge Source）として利用する | Accepted |
| [0010](0010-default-value-policy.md) | 設定値は公式ベストプラクティスを根拠に決定し、利用者へ公開しない | Accepted |
| [0011](0011-least-privilege-policy.md) | 最小権限をdefaultとし、安全な設定をopt-inにしない | Accepted |
| [0012](0012-testing-policy.md) | テストを第一級の設計要件とする | Accepted |

## 読む順序

ADR-0002 がリポジトリ全体の評価軸と原則を定義し、他のADRはその下位決定にあたる。初読時は次の順序を推奨する。

1. ADR-0002（前提と評価軸）
2. ADR-0003（スコープの単位）
3. ADR-0004 → ADR-0005 → ADR-0006 → ADR-0007（公開契約）
4. ADR-0008 → ADR-0009 → ADR-0010（依存と設定値の根拠）
5. ADR-0011（セキュリティ）
6. ADR-0012（検証）
7. ADR-0001（ADR自体の運用）

## 依存関係

```
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

AWS resourceは設計判断の結果であり、ADR体系の起点ではない。依存の向きはこの一方向に限る（ADR-0001 決定13-15）。

## 新規ADRの追加

1. [template.md](template.md) を複製する。
2. 適用範囲を判断し、配置先を決める（ADR-0001 決定5-9、決定20のチェックリスト）。
3. 代替案を ADR-0002 の評価軸で比較する。
4. 検証方法を記述する。機械検証できない決定は、決定として不完全とみなす（ADR-0012 決定3）。
