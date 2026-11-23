# ADR-0201: アカウント単位の安全側の設定は、CI の apply の role の外から止めさせず、opt-in のリージョンは誰にも有効にさせない

- Status: Accepted
- Date: 2026-11-24
- Scope: account-baseline
- Related: repository-wide/ADR-0101, account-baseline/ADR-0101, organization-baseline/ADR-0101

## Context

このユースケースが置く設定は、アカウントかリージョンに1つしか置けず、メンバーの中で権限を持つ主体なら誰でも止められる。止めた後の apply が戻すまで、Security Hub の EC2.7・IAM.28・Config.1・S3.1・EC2.172・SSM.6・SSM.7・EMR.2 は外れたままになる。Well-Architected の FND01-BP01・FND02-BP01 は、検知と統制の停止を SCP で拒むことを勧める。

SCP の付け先は `organization-baseline` が持つ（organization-baseline/ADR-0101 (organizational-units-and-policies) 決定5）。何を拒むかは、守る設定の持ち主が決める。組織の trail・GuardDuty・Inspector は `security-tooling` が、監査ログの保管庫と鍵は `audit-evidence` が持ち、このユースケースが決めるのは自分の設定の分である。

account-baseline/ADR-0101 (default-regions-per-partition) 決定4 は、リージョンの集合を閉じるために opt-in のリージョンの有効化を SCP で拒むとした。

SCP は管理アカウントに効かない。break-glass の role を SCP の例外にするのは `deny-state-write` だけである（organization-baseline/ADR-0101 (organizational-units-and-policies) 決定9）。

## Decision

1. このユースケースの設定を止める・変える Action を、`organization-baseline` の SCP `deny-baseline-stop` で拒む。拒む Action と、それが守る設定は次のとおりとする。

   | Action | 守る設定 |
   | --- | --- |
   | `config:StopConfigurationRecorder`・`config:DeleteConfigurationRecorder`・`config:PutConfigurationRecorder` | Config の recorder（停止、削除、記録の範囲と role の変更） |
   | `config:DeleteDeliveryChannel`・`config:PutDeliveryChannel` | Config の delivery channel（削除、配信先の変更） |
   | `config:DeleteRetentionConfiguration`・`config:PutRetentionConfiguration` | Config の構成の履歴の保持日数 |
   | `access-analyzer:DeleteAnalyzer` | 外部アクセスの analyzer |
   | `access-analyzer:CreateArchiveRule`・`access-analyzer:UpdateArchiveRule` | analyzer の検出（archive rule で検出を隠すこと） |
   | `ec2:DisableEbsEncryptionByDefault` | EBS の既定の暗号化 |
   | `ec2:ModifyVpcBlockPublicAccessOptions`・`ec2:CreateVpcBlockPublicAccessExclusion`・`ec2:ModifyVpcBlockPublicAccessExclusion` | VPC Block Public Access（向きの変更と、例外の作成と変更） |
   | `s3:PutAccountPublicAccessBlock` | アカウントの S3 Public Access Block |
   | `elasticmapreduce:PutBlockPublicAccessConfiguration` | EMR の block public access |
   | `ssm:UpdateServiceSetting`・`ssm:ResetServiceSetting` | SSM document の公開共有と Automation のログの出力先 |

2. `deny-baseline-stop` の例外は、メンバーの env（security・log-archive・sandbox・dev・stg・prd）ごとの CI の apply の role（`<project>-<env>-ci-apply`）だけとし、名前を env を固定して名指す。このユースケースを apply するのはその role であり、設定を戻す経路もそれである。
3. break-glass の role を `deny-baseline-stop` の例外にしない。break-glass を例外にする SCP を `deny-state-write` に限る決定（organization-baseline/ADR-0101 (organizational-units-and-policies) 決定9）と、それを守る `PLAN-ORG-05` に揃える。設定が壊れたときの回復は、CI の apply の経路で行う。CI の経路が使えないときは、break-glass で state と CI の経路を直してから、CI の apply で戻す。
4. opt-in のリージョンの有効化（`account:EnableRegion`）を、`organization-baseline` の SCP `deny-enable-region` で、例外なしに拒む。opt-in のリージョンを使うときは、account-baseline/ADR-0101 (default-regions-per-partition) の集合を変える変更と同じ変更で、この SCP の文を改める。CI の apply の role にも開けると、集合を変える前にリージョンが有効になり、account-baseline/ADR-0101 (default-regions-per-partition) 決定3 の検出でそのアカウントの plan が止まる。
5. どちらの SCP も、organization-baseline/ADR-0101 (organizational-units-and-policies) 決定4 に従い PolicyStaging に付けて始め、確かめた後で Security・Workloads・Sandbox へ広げる。Sandbox にも付けるのは、sandbox も Config の外の設定を常駐させるからである。
6. 管理アカウントには SCP が効かない。管理アカウントの設定の停止と opt-in のリージョンの有効化は、どちらの SCP も拒めない。opt-in のリージョンは account-baseline/ADR-0101 (default-regions-per-partition) 決定3 の plan の検出だけが事後に見る。

## 検討した代替案

### 案A: break-glass の role も例外にする

CI の経路が使えないときにも、break-glass で設定を直せる。しかし break-glass は `AdministratorAccess` を持ち、例外にすると設定を止められる主体が1つ増える。break-glass を例外にする SCP を `deny-state-write` に限る決定と `PLAN-ORG-05` を変えることになる。

### 案B: `deny-enable-region` に CI の apply の role の例外を置く

リージョンを足す変更が SCP を書き換えずに済む。しかし apply の role で集合の外のリージョンを有効にすると、そのアカウントの plan が account-baseline/ADR-0101 (default-regions-per-partition) 決定3 の検出で止まり、集合を変える変更と有効化の順序が守られない。

### 案C: 設定の中身ごとに SCP を分ける（Config・analyzer・EBS などで1つずつ）

拒む主体の例外が同じなので、分けても読む文が増えるだけである。付け先ごとに直接付けられる SCP の数の上限（10。AWS Organizations の quota の文書）にも早く届く。PolicyStaging には、この変更の後で、外さない既定の `FullAWSAccess` を含めて9つの SCP が直接付く。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中（止められる主体が増える） | 中 | 高 | 高 |
| Testability | 中 | 中 | 中 | 高（`PLAN-ORG-05`・`PLAN-ORG-09`） |
| Contract Clarity | 低（例外の規則が2つになる） | 低 | 中 | 高 |
| Maintainability | 中 | 高 | 低（付け先の上限） | 中（リージョンを足すと SCP も変える） |
| Scope Control | 中 | 低 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- break-glass で、CI の経路を通さずにこのユースケースの設定を直すこと
- メンバーで opt-in のリージョンを有効にすること

## 保証範囲

- 保証すること: `deny-baseline-stop` が決定1 の Action を拒み、例外がメンバーの env の CI の apply の role だけであること。`deny-enable-region` が `account:EnableRegion` を条件なしに拒むこと
- 保証しないこと: 管理アカウントでの設定の停止と opt-in のリージョンの有効化。SCP は管理アカウントに効かない
- 保証しないこと: PolicyStaging から Security・Workloads・Sandbox へ広げるまでの、メンバーでの拒否
- 保証しないこと: CI の apply の role を書き換えられる主体（メンバーの apply-identity の role、break-glass の role）が、その role を経て設定を止めること
- 保証しないこと: メンバーで role を作れるか信頼を書き換えられる主体が、`<project>-<env>-ci-apply` の名前の role を作って例外に入ること。例外は名前で当て、アカウントと env の組を問わない
- 保証しないこと: 代替の連絡先と password policy の変更を拒むこと。決定1 の Action に含めない
- 保証しないこと: SCP が実際に要求を拒むこと。PolicyStaging に sandbox を移した間の人の確認だけが見る

## 検証方法

- Unit Test: `usecases/organization-baseline/internal/organization/tests/organization.tftest.hcl` の `baseline_scps` が、`deny-baseline-stop` の Action の一覧と例外の条件、`deny-enable-region` の文を固定する
- Policy Test（`make tf-policy`）: `PLAN-ORG-09` が、`deny-baseline-stop` が決定1 の Action のすべてを Resource `*` について拒み、例外が `ArnNotLike` の `aws:PrincipalArn` でメンバーの env の CI の apply の role だけであることと、`deny-enable-region` が条件なしに拒むことを見る。`PLAN-ORG-05` が、break-glass の role を例外にする SCP が `deny-state-write` だけであることを見る。`PLAN-ORG-01` が付け先を見る
- 実環境: PolicyStaging に sandbox を移した間に、CI の apply の role の外の主体で決定1 の Action と `account:EnableRegion` が拒まれ、CI の apply の role で設定を変えられることを人が確かめ、結果を Pull Request に記録する

## 未確認

AWS の公式の文書で次を確かめきれていない。PolicyStaging での確認で拒まれることを見る。

- `access-analyzer:DeleteAnalyzer`・`access-analyzer:CreateArchiveRule`・`access-analyzer:UpdateArchiveRule` の IAM の Action 名。API の操作があることは確かめたが、IAM の Action 名を載せた頁は読めていない
- アカウントの Public Access Block を消す操作（`DeletePublicAccessBlock`）に要る権限。`s3:PutAccountPublicAccessBlock` が要ることは、設定の操作（`PutPublicAccessBlock`）についてだけ文書で確かめた。消す操作に別の Action が要るなら、決定1 は削除を拒まない

確かめたもの: config・ec2（`DisableEbsEncryptionByDefault`・`CreateVpcBlockPublicAccessExclusion`。`ModifyVpcBlockPublicAccessOptions`・`ModifyVpcBlockPublicAccessExclusion` は VPC の IAM のポリシーの例）・elasticmapreduce・ssm・account の Action は、Service Authorization Reference と各 API の文書で確かめた。

## 影響

- このユースケースに設定を足す変更は、その設定を止める Action を `deny-baseline-stop` に足し、`PLAN-ORG-09` と `baseline_scps` の一覧を同じ変更で改める
- メンバーの CI の apply の role の名前を変える変更は、`deny-baseline-stop` の例外も改める
- sandbox の掃除の CI（#76）の role は例外にしないので、掃除の設定は常駐の設定を対象から外す必要がある
- EBS の既定の暗号化の鍵を CMK にする変更は、`ec2:ModifyEbsDefaultKmsKeyId`・`ec2:ResetEbsDefaultKmsKeyId` を決定1 に足す

## 見直し条件

- opt-in のリージョンを使う利用者が現れたとき（account-baseline/ADR-0101 (default-regions-per-partition) の見直しと同時）
- break-glass を例外にする SCP の決定（organization-baseline/ADR-0101 (organizational-units-and-policies) 決定9）が変わったとき
- 未確認の Action 名が文書か PolicyStaging の確認で違うと分かったとき
- 付け先ごとの SCP の数が上限に近づいたとき
