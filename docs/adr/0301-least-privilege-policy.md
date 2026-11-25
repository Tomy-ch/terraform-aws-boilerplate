# ADR-0301: 最小権限をdefaultとし、安全な設定をopt-inにしない

- Status: Accepted
- Date: 2026-09-15
- Scope: repository-wide
- Related: ADR-0201, ADR-0203, ADR-0204, ADR-0206, ADR-0207, ADR-0401

## Context

Least Privilegeは一般にIAMの原則として語られるが、実際の権限境界はIAMだけでは決まらない。ネットワーク到達性、resource policy、暗号鍵のkey policy、secretへのアクセス経路、subnet配置のいずれかが緩ければ、IAMが厳密であっても境界は成立しない。

また、安全な設定をopt-inにすると、既定の構成は安全ではない構成になる。利用者が明示的に有効化しなかった箇所が、そのまま脆弱な状態として残る。boilerplateが「安全な構成をエンコードした参照実装」であること（ADR-0101）と、安全性がopt-inであることは両立しない。

さらに、権限拡張の入力を任意のpolicy JSONとして受け取ると、拡張の内容がboilerplateの検証対象外になる。最小権限であるかどうかを機械的に判定できなくなる。

人の到達経路を IAM Identity Center の1本に限ると、その障害（リージョンの障害、外部 IdP や MFA の設定の誤り、permission set の割当の削除）で、誰もアカウントへ入れなくなる。AWS は、Identity Center を複数のリージョンへ複製していても、Identity Center から独立した緊急の到達経路（break-glass）を持つことを勧めている。本リポジトリは `organization-baseline` で組織と Identity Center の permission set を作るので、その経路を組織の外の運用へ委ねられない。

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
8. 権限を与える statement（`Effect` が `Allow`）で、Action wildcard（`service:*` および `*`）を使用しない。拒否する statement（`Deny`）は、対象の Action を漏れなく覆うために wildcard を使ってよい。
9. Resource wildcard（`"*"`）は、AWS APIがresource-level権限をサポートしないactionに限り許容する。この場合も以下を満たすこと。

   - 当該actionを個別のstatementへ分離する
   - 可能な限りConditionで対象を限定する
   - 許容箇所と根拠を当該ユースケースのADRまたは設計記録へ記載する

10. AWS managed policyは原則として採用しない。service-linked role、およびAWSが機能上その使用を要求する場合に限り許容し、根拠を記録する。それ以外はcustomer managed policyを生成する。

    ただし、人が IAM Identity Center から到達するときの permission set には、AWS managed の職務別 policy を付ける。付けてよい policy と、それを付ける permission set は、`organization-baseline` の設計記録が閉じた一覧として持つ。人の作業は CI の role と違って前もって列挙できず、読み取りの Action を本リポジトリで列挙すると数千に及び、AWS のサービスが増えるたびに追随できないためである。AWS も、職務別の managed policy から始め、使われ方を見て絞ることを勧めている。これらの policy の中の Action wildcard は、本リポジトリが生成する policy document ではないので決定8 の対象外とする。break-glass の role（決定19）も同じ扱いである。
11. ARNが事前に確定しない場合は、まずresource参照による依存順序の解決を試みる。解決できない場合に限り、prefixベースのARN patternをConditionで限定して使用し、根拠を記録する。

    ただし、拒否する statement（`Deny`）の Resource は、ARN が確定していても、それらを覆う prefix ベースの ARN pattern で書いてよく、Condition での限定を要しない。pattern は拒む範囲を広げるだけで、Allow が許す範囲を広げないからである（決定8 が Deny の Action wildcard を許すのと同じ理由）。この但し書きを使う箇所は、次の3つを当該ユースケースの ADR に記録する: 覆うべき ARN のすべてが pattern に入ることを確かめる入力の検証か検査、pattern が覆うべき ARN の外に当てる範囲、列挙せずに pattern で書く理由。

    ARN の region の部分だけを `*` にした pattern は、ARN が事前に確定していても、許可する statement で使ってよい。条件は2つある。region を列挙した ARN では policy の文字数の上限を超えること。同じ role に、その statement の Action を、条件キー `aws:RequestedRegion` が許すリージョンの集合の外にあるときに拒む `Deny` の statement があること。リージョンごとの資源の ARN の region は、要求を受けた endpoint の region と一致するので、許す要求はリージョンを列挙した ARN と同じになる。この但し書きを使う箇所は、許すリージョンの集合とその根拠、`Deny` の Action が `Allow` の Action を覆うことを確かめる検査を、当該ユースケースの ADR に記録する（ADR-0209 決定4）。
12. KMS の鍵の暗号操作（`kms:Decrypt`・`kms:Encrypt`・`kms:GenerateDataKey` など）と `kms:CreateGrant` は、IAM policy で与えない。その鍵の Key Policy が、使う principal を名指して許す。Key Policy がアカウントへ委任するのは鍵の管理の Action に限り、暗号操作と `kms:CreateGrant` を含めない。鍵を使う主体の一覧が、鍵ごとに Key Policy の1箇所に揃い、IAM の Allow を足しても鍵を使えるようにはならない。

### ネットワーク

13. ネットワークの既定をdeny とする。必要な通信のみを許可する。
14. ingressは、可能な限りsource security groupの参照で限定する。CIDRによる許可は、security group参照が不可能な場合に限る。
15. `0.0.0.0/0` と `::/0` からのingressは、インターネット公開点として設計されたコンポーネント（ロードバランサ、CDN等）に限る。workload本体への直接ingressを許可しない。
16. egressについても、ユースケースごとに必要な範囲を定義する。全開放を既定としない。
17. 利用者が通信経路を追加する必要がある場合は、接続先を意味論的に指定する契約として定義する（ADR-0204 決定7）。

### 越境

18. cross-account accessを既定で許容しない。必要な場合は、明示的なユースケースとして設計し、信頼関係の条件（account、Condition、外部ID等）を型付き契約として受け取る。
19. 人の通常の到達経路（IAM Identity Center）が使えないときの緊急の到達経路（break-glass）を、組織を作るユースケース（`organization-baseline`）が提供する。形を次に限る。

    - 置き場は管理アカウントとする。管理アカウントは組織のすべてのアカウントへ届く権限をもともと持つので、そこへ置いても到達できる範囲は増えない。ほかのアカウントへ置くと、そのアカウントが全アカウントへ届く経路を新たに持つ
    - 管理アカウントに break-glass の IAM user を2つ置く（1つが使えなくなったときの予備）。コンソールへのサインインだけを持ち、アクセスキーを持たず、自身にアクセスキーを作ることを拒否する。MFA はハードウェアの MFA とする。パスワードと MFA は Terraform で作らず、人が設定して保管する。名前は個人を表さない
    - IAM user が自身に持つ権限は、各アカウントの break-glass の role を引き受けることと、自身の MFA とパスワードの管理だけとする。管理アカウントの中の作業も、管理アカウントの break-glass の role を経由する
    - すべてのアカウント（管理アカウントを含む）に break-glass の role を置く。信頼条件は、管理アカウントの break-glass の IAM user に限り、MFA で認証したことを要求する。権限は AWS managed policy の `AdministratorAccess` とする。これは決定8・10 の例外である。緊急時に要る作業を前もって列挙できないためであり、例外は break-glass の role に限る
    - メンバーアカウントの break-glass の role を SCP の拒否から外すのは、回復を妨げる拒否（state の書き込みの拒否。ADR-0602 決定11）に限り、外すのは break-glass の role の ARN に限る。検知を止めることの拒否と、監査ログとその鍵を守る拒否からは外さない。緊急の作業にそれらは要らず、外すと管理アカウントの側が自分の記録を読めなくする経路になる
    - break-glass の IAM user のサインインと、break-glass の role の引き受けを、平時には起きない事象として必ず通知する（`ops-notification` の Topic）。管理アカウントの root の使用と `sts:AssumeRoot` の呼び出しも同じく通知する
    - 管理アカウントの root と、メンバーアカウントの root の集中管理（`sts:AssumeRoot`）は、break-glass が使えないときの最後の手段として残す。管理アカウントで `sts:AssumeRoot` を許す主体は、break-glass の role に限る（管理アカウントには SCP が効かないので、IAM で限る）

### 検証

20. 上記のうち機械判定可能なものは、Policy Testのinvariantとして実装する。invariantを持たない安全性の主張を、保証として扱わない。

### IAM の付与の破棄

21. role に付けた managed policy は、role と policy の破棄と置換で、policy より先に外れる。付与1件ごとに、破棄で付与を外す resource（`aws_iam_role_policy_attachment`）を持たせ、付ける policy を参照させる。帯域外の付与を剥がす排他の resource（`aws_iam_role_policy_attachments_exclusive`）は、破棄で付与を外さない。そのため付与の持ち主にせず、同じ policy を載せて併用する。

## 検討した代替案

### 案A: 安全な設定をopt-inとし、defaultはAWS / Provider既定に従う

利用者の初期導入は容易だが、既定構成が安全でない状態になる。ADR-0101の前提と両立しない。

### 案B: 最小権限をIAMに限定して適用する

実装範囲は明確だが、ネットワークやresource policyの緩さによって境界が破れるため、実効的な保証にならない。

### 案C: 安全なdefaultを提供しつつ、policy JSONによる拡張を許容する

拡張要求へ即応できるが、拡張内容が検証対象外となり、最小権限であるかを機械判定できない。ADR-0204に該当する。

### 案D: KMS の鍵の Key Policy でアカウントへ `kms:*` を委任し、暗号操作を IAM で与える（決定12 に対して）

AWS の既定の Key Policy と、console が作る key users の文（`kms:CreateGrant` を `kms:GrantIsForAWSResource` で限る形）はこの形である。鍵を使える主体が、アカウントの IAM の Allow のどこからでも増え、鍵ごとの一覧が Key Policy に残らない。AWS の「Least-privilege permissions」も、`kms:*` の委任は IAM で更に絞るときに限るとし、Key Policy で principal を名指すことを挙げる。決定12 は後者を採り、前者からの逸脱として次を記録する（ADR-0207 (default-value-policy) 決定11）。

- どのユースケース上の制約によるか: ユースケースの制約に由来しない。repository-wide の原則である
- 外れる対象: 既定の Key Policy の `kms:*` のアカウントへの委任、IAM で暗号操作を与える形
- 理由: 鍵を使う主体を鍵ごとに Key Policy の1箇所で名指し、IAM の Allow や grant から鍵を使う経路を作らない
- 影響: security は、IAM の Allow を足しても鍵を使えない。availability は、鍵を使う主体を足すたびに Key Policy の変更が要り、AWS のサービスが grant を要る消費者（RDS・ECR など）は `kms:CreateGrant` を `kms:ViaService` と `kms:GrantIsForAWSResource` で限った文を Key Policy に足す変更が要る。Key Policy は 32,768 文字が上限である。cost は変わらない
- 代替案: 本案D
- 見直し条件: 下の「見直し条件」

### 案E: break-glass を提供せず、責務外とする（決定19 に対して）

長期の資格情報を増やさない。ただし本リポジトリが組織と Identity Center を作るので、組織の外の運用へ委ねる先が無い。残るのは管理アカウントの root と `sts:AssumeRoot` だけで、`sts:AssumeRoot` で行えるのは5つの task policy（root の資格情報の監査・削除、root のパスワードの作成、S3 の Bucket Policy と SQS の Queue Policy の解除）に限られ、アカウントを運用する権限にならない。AWS は root を root でしか行えない作業以外に使わないよう勧めている。

### 案F: break-glass の IAM user をセキュリティのアカウントに置く（決定19 に対して）

AWS Well-Architected の旧 Security Pillar（SEC10-BP05）が示した形である。2026-11-16 の改版の後の Framework は、専用の非常用アカウントを置くことを勧める（FND05-BP02）。ただし本リポジトリのセキュリティのアカウントは、検知を調べる日常の作業を受け持つ（`docs/v1.0-plan/scope.md` 第10.1節）。そこへ全アカウントへ届く経路を置くと、日常の出入りがあるアカウントを抜かれた被害が組織全体へ広がる。

### 案G: 専用の非常用アカウントへ、外部 IdP から SAML で直接入る（決定19 に対して）

IAM Identity Center の文書の手順である。外部 IdP を前提とし、Identity Center の directory を使う構成では成り立たない。外部 IdP の障害には効かない。アカウントが1つ増える。

### 案H: 人の permission set も customer managed policy で列挙する（決定10 の例外に対して）

決定10 の原則どおりで、許す Action が本リポジトリの宣言に揃う。ただし、調べる作業に要る読み取りは AWS の全サービスに及び、列挙は数千の Action になる。サービスが増えるたびに追随の変更が要り、漏れは「見えない」故障として現れる。組織の管理者の権限は列挙しても実質 `*` になり、決定8 と正面から当たる。

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
- 人の到達経路を SSO の1本に限ること（break-glass の2つの IAM user が例外として残る）
- 権限拡張の即時対応

## 保証範囲

- 保証すること: boilerplateが生成する権限・通信経路が、意味論的な入力から導出された集合に一致すること
- 保証すること: break-glass の経路の形（IAM user の数と資格情報の種類、role の信頼条件、決定8・10 の例外の範囲、SCP からの除外の範囲、通知の経路）が宣言どおりであること
- 保証すること: 人の permission set に付く AWS managed policy が、設計記録の一覧に載ったものだけであること。組織の管理者の permission set が管理アカウントにだけ、ユーザーへ直接割り当てられていること
- 保証しないこと: boilerplate外部で付与された権限、および利用者アカウント側のIAM運用
- 保証すること: role への managed policy の付与（`aws_iam_role_policy_attachment`）が、同じ module の `aws_iam_policy` を参照するか、AWS managed policy の ARN を付けること（決定21）。参照が欠けると、破棄と置換で policy の削除が付け外しより先に走り、削除が失敗する
- 保証しないこと: 人の permission set に付く AWS managed policy の中身。AWS が改訂すると、宣言を変えずに人の権限が変わる
- 保証しないこと: break-glass のパスワードと MFA の保管。メンテナが1人の間は、パスワードと MFA を別の人に分けて持てない（AWS は管理アカウントの root について、1人が両方を持たないことを勧めている）。break-glass で実際に入れること（MFA が要るので自動では確かめられない。人が定期的に試す）

## 検証方法

- Policy Test（必須）: 以下をinvariantとして検証する。
  - policy document の Allow の statement に Action wildcard が存在しないこと
  - Resource wildcard が、許可リストに登録されたaction以外で使用されていないこと。同じ範囲を与える `NotResource` の Allow が無いこと。許可リストに、使われていない action が残っていないこと
  - `0.0.0.0/0` と `::/0` からのingressが、インターネット公開点として宣言されたresource以外に存在しないこと
  - 意味論的権限指定から生成されたActionの集合が、期待集合と完全一致すること
  - KMS Key Policy のアカウントへの委任に暗号操作と `kms:CreateGrant` が無いこと。identity-based policy（role・user・group の inline policy と customer managed policy）の Allow に KMS の暗号操作と `kms:CreateGrant` が無いこと
  - break-glass の IAM user がちょうど2つで、Terraform がアクセスキーと login profile を持たないこと。IAM user の Allow が break-glass の role の `sts:AssumeRole` と自身の MFA・パスワードの管理に限られ、長期の資格情報の作成（`iam:CreateAccessKey`・`iam:CreateServiceSpecificCredential`・`iam:UploadSSHPublicKey`・`iam:UploadSigningCertificate`）を、条件なしで全 Resource について Deny していること
  - break-glass の role の信頼条件が、break-glass の IAM user の ARN と `aws:MultiFactorAuthPresent` を要求すること
  - 決定8・10 の例外（`AdministratorAccess`）が break-glass の role にだけ付いていること
  - SCP の除外が、回復を妨げる拒否についての break-glass の role の ARN に限られること
  - 管理アカウントで `sts:AssumeRoot` を許す主体が break-glass の role に限られること
  - permission set に付く AWS managed policy が、設計記録の一覧（permission set ごとの許可リスト）に一致すること。一覧に無い policy の attachment が無いこと、一覧に使われていない項目が残っていないこと
  - `AdministratorAccess` を付ける permission set の割当が、管理アカウントだけ、かつ `principal_type = "USER"` だけであること。メンバーアカウントへの割当に、`AdministratorAccess` を付けた permission set が無いこと
  - 排他の resource（`aws_iam_role_policy_attachments_exclusive`）が載せる policy ごとに、同じ role への attachment がちょうど1つ在ること。排他の resource に載らない attachment が無いこと（決定21。`plan.iam_role_attachment_pairing`、`PLAN-ATTACH-*`）
- Security（ADR-0501 決定6 の道具）: 暗号化・public access block・secure transport 等の Secure by Default の設定が有効であること。道具の既定ルールが本リポジトリの書き方を検出しない設定は、ADR-0402 決定6 の表の下の文により Policy Test が持つ。
- Unit Test: 権限語彙と入力の組合せに対する導出結果を検証する。
- 人の手順: break-glass で実際にサインインして role を引き受け、通知が届くことを定期的に試す。手順と頻度は `organization-baseline` の README の検証方法が持つ。
- Integration / E2E Test: 意図した通信のみが成立し、意図しない通信が拒否されることを実環境で検証する。
- Static Analysis（Conftest、HCL のソース。`conftest/hcl/layer/` の `LY309`）: `aws_iam_role_policy_attachment` の `policy_arn` が、`local` と `each` を辿った先で同じ module の `aws_iam_policy` だけを参照するか、partition の部分だけを `data.aws_partition` から組む AWS managed policy の ARN であること（決定21）。Policy Test の入力は構成を持たないので、参照は HCL のソースで見る
- Static Analysis: policy JSONを受け取るvariableが存在しないことを検査する（ADR-0204と共通）。

## 影響

- 権限語彙の拡張は、Policy Testの期待集合の更新を伴う。
- 実環境での権限境界の検証が、E2E Testの必須項目になる。
- 利用者が必要とする権限がboilerplateの語彙に存在しない場合、ADR-0102 決定5の3択で判断される。

## 見直し条件

- AWS側にresource-level権限が追加され、Resource wildcardの許容箇所が解消できるようになった場合
- cross-accountを必要とするユースケースが複数発生した場合
- 意味論的な権限語彙では表現できない権限要求が反復した場合
- IAM Access Analyzer で、人の permission set が実際に使う Action が十分に集まった場合（customer managed policy へ絞る）。AWS managed policy の改訂で、人の権限が意図せず広がった場合
- メンテナが2人以上になった場合（パスワードと MFA を分けて持つ形、IAM user を人ごとに名付ける形）。外部 IdP を identity source にした場合（案G の再評価）。AWS が Identity Center から独立した緊急の到達の仕組みを提供した場合
- IAM で暗号操作を与えないと成り立たない利用者（Key Policy で名指せない主体）が現れた場合。1つの鍵の Key Policy が名指す主体と文で 32,768 文字の上限に近づいた場合。grant を要るサービスの消費者が、Key Policy の文の追加では成り立たなかった場合
