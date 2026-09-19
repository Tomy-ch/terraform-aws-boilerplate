# ADR-0301: 最小権限をdefaultとし、安全な設定をopt-inにしない

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0201, ADR-0203, ADR-0204, ADR-0206, ADR-0207, ADR-0401

## Context

Least Privilegeは一般にIAMの原則として語られるが、実際の権限境界はIAMだけでは決まらない。ネットワーク到達性、resource policy、暗号鍵のkey policy、secretへのアクセス経路、subnet配置のいずれかが緩ければ、IAMが厳密であっても境界は成立しない。

また、安全な設定をopt-inにすると、既定の構成は安全ではない構成になる。利用者が明示的に有効化しなかった箇所が、そのまま脆弱な状態として残る。boilerplateが「安全な構成をエンコードした参照実装」であること（ADR-0101）と、安全性がopt-inであることは両立しない。

さらに、権限拡張の入力を任意のpolicy JSONとして受け取ると、拡張の内容がboilerplateの検証対象外になる。最小権限であるかどうかを機械的に判定できなくなる。

## Decision

### 適用範囲

1. Least Privilegeを、IAMに限定せずAWSアーキテクチャ全体へ適用する。適用対象は少なくとも以下とする。

   - IAM Policy / IAM Role
   - Resource Policy
   - Security Group
   - Network ACLを使用する場合の通信範囲
   - Subnet配置および public / private 境界
   - KMS Key Policy
   - Secretへのアクセス
   - S3 Bucket Policy
   - Queue / Topic へのアクセス
   - Service-to-Service communication

### 基本原則

2. 安全な設定をopt-inにしない。安全な状態をdefaultとし、緩和が必要な場合にのみ明示的な入力を要求する。
3. 緩和のための入力も、任意のPolicy JSON等ではなく、意味論的な型付き契約とする（ADR-0201、ADR-0203）。
4. Secure by Default の対象として、少なくとも以下をユースケースごとに検討し、公式推奨（ADR-0207）に従って設定する。

   - encryption at rest
   - encryption in transit
   - private network placement
   - public access blocking
   - access logging
   - TLSの安全なversion
   - least privilege IAM
   - resource policyによる制限
   - deletion protection
   - backup / retention
   - audit logging
   - secret management
   - instance metadata service等のサービス固有の安全設定

5. 全AWSサービスへ機械的に同一設定を適用しない。各ユースケースにおける公式推奨を確認したうえで決定する。

### IAM

6. IAM policy documentはboilerplate内部で生成する。利用者からpolicy JSONまたはpolicy statementを受け取らない（ADR-0204）。
7. 権限は意味論で受け取り、必要なActionを内部で導出する（ADR-0201 決定4）。語彙（例: `read` / `write`）の意味は、ユースケースのADRで定義し、対応するAction集合をPolicy Testで固定する。
8. Action wildcard（`service:*` および `*`）を使用しない。
9. Resource wildcard（`"*"`）は、AWS APIがresource-level権限をサポートしないactionに限り許容する。この場合も以下を満たすこと。

   - 当該actionを個別のstatementへ分離する
   - 可能な限りConditionで対象を限定する
   - 許容箇所と根拠を当該ユースケースのADRまたは設計記録へ記載する

10. AWS managed policyは原則として採用しない。service-linked role、およびAWSが機能上その使用を要求する場合に限り許容し、根拠を記録する。それ以外はcustomer managed policyを生成する。
11. ARNが事前に確定しない場合は、まずresource参照による依存順序の解決を試みる。解決できない場合に限り、prefixベースのARN patternをConditionで限定して使用し、根拠を記録する。

### ネットワーク

12. ネットワークの既定をdeny とする。必要な通信のみを許可する。
13. ingressは、可能な限りsource security groupの参照で限定する。CIDRによる許可は、security group参照が不可能な場合に限る。
14. `0.0.0.0/0` からのingressは、インターネット公開点として設計されたコンポーネント（ロードバランサ、CDN等）に限る。workload本体への直接ingressを許可しない。
15. egressについても、ユースケースごとに必要な範囲を定義する。全開放を既定としない。
16. 利用者が通信経路を追加する必要がある場合は、接続先を意味論的に指定する契約として定義する（ADR-0204 決定7）。

### 越境

17. cross-account accessを既定で許容しない。必要な場合は、明示的なユースケースとして設計し、信頼関係の条件（account、Condition、外部ID等）を型付き契約として受け取る。
18. 緊急時の権限昇格（break-glass）の経路をboilerplateとして提供しない。これは本リポジトリの責務外とし、組織のIAM運用側で扱う。この判断はユースケースのunsupported一覧へ記載する。

### 検証

19. 上記のうち機械判定可能なものは、Policy Testのinvariantとして実装する。invariantを持たない安全性の主張を、保証として扱わない。

## 検討した代替案

### 案A: 安全な設定をopt-inとし、defaultはAWS / Provider既定に従う

利用者の初期導入は容易だが、既定構成が安全でない状態になる。ADR-0101の前提と両立しない。

### 案B: 最小権限をIAMに限定して適用する

実装範囲は明確だが、ネットワークやresource policyの緩さによって境界が破れるため、実効的な保証にならない。

### 案C: 安全なdefaultを提供しつつ、policy JSONによる拡張を許容する

拡張要求へ即応できるが、拡張内容が検証対象外となり、最小権限であるかを機械判定できない。ADR-0204に該当する。

### 評価

| 評価軸 | 案A: opt-in | 案B: IAM限定 | 案C: JSON拡張可 | 採用案: 全面default |
| --- | --- | --- | --- | --- |
| Security | 低 | 中（境界が破れる） | 低 | 高 |
| Testability | 中 | 中 | 低 | 高（invariant化） |
| Contract Clarity | 中 | 中 | 低 | 高 |
| Type Safety | 中 | 中 | 低 | 高 |
| Cognitive Load | 高 | 中 | 高 | 低 |
| 拡張の即応性 | 高 | 中 | 高 | 低（意図的） |

## 意図的に捨てるもの

- 任意のIAM policyおよびネットワークルールを追加できること
- 既定でcross-account accessを利用できること
- break-glass経路の提供
- 権限拡張の即時対応

## 保証範囲

- 保証すること: boilerplateが生成する権限・通信経路が、意味論的な入力から導出された集合に一致すること
- 保証しないこと: boilerplate外部で付与された権限、および利用者アカウント側のIAM運用

## 検証方法

- Policy Test（必須）: 以下をinvariantとして検証する。
  - IAM policyに Action wildcard が存在しないこと
  - Resource wildcard が、許可リストに登録されたaction以外で使用されていないこと
  - `0.0.0.0/0` からのingressが、インターネット公開点として宣言されたresource以外に存在しないこと
  - 暗号化・public access block・secure transport等のSecure by Default設定が有効であること
  - 意味論的権限指定から生成されたActionの集合が、期待集合と完全一致すること
- Unit Test: 権限語彙と入力の組合せに対する導出結果を検証する。
- Integration / E2E Test: 意図した通信のみが成立し、意図しない通信が拒否されることを実環境で検証する。
- Static Analysis: policy JSONを受け取るvariableが存在しないことを検査する（ADR-0204と共通）。

## 影響

- 権限語彙の拡張は、Policy Testの期待集合の更新を伴う。
- 実環境での権限境界の検証が、E2E Testの必須項目になる。
- 利用者が必要とする権限がboilerplateの語彙に存在しない場合、ADR-0102 決定5の3択で判断される。

## 見直し条件

- AWS側にresource-level権限が追加され、Resource wildcardの許容箇所が解消できるようになった場合
- cross-accountを必要とするユースケースが複数発生した場合
- 意味論的な権限語彙では表現できない権限要求が反復した場合
