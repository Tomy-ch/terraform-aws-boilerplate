# ADR-0101: OU の木と、SCP・RCP・declarative policy の中身と付け先を内部で固定する

- Status: Accepted
- Date: 2026-11-19
- Scope: organization-baseline
- Related: repository-wide/ADR-0204, repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0402, repository-wide/ADR-0602, organization-baseline/ADR-0001, organization-baseline/ADR-0201, account-baseline/ADR-0201

## Context

OU は SCP・RCP・declarative policy を付ける単位であり、どのアカウントにどの禁止を掛けるかで分ける。AWS の whitepaper「Organizing Your AWS Environment Using Multiple Accounts」は Security・Infrastructure・Workloads（Prod・NonProd）・Sandbox・Policy Staging・Suspended などの OU を挙げ、要るものだけを作ればよいとする。SRA のチェックリストは Infrastructure の OU を MUST とし、Well-Architected（2026-11-16 の版）の FND02-BP06 は SCP と RCP でデータ境界を作ること、FND02-BP01・FND01-BP01 はメンバーの root と検知の停止を SCP で縛ることを勧める。

SCP と RCP は管理アカウントに効かない。SCP はメンバーの root にも効き、service-linked role には効かない。root に付けた policy はすべてのアカウントに効くので、AWS は新しい policy を root に付ける前に小さな範囲で試すことを勧める。

## Decision

1. OU の木を内部で固定する。Root の直下に Security・Workloads・Sandbox・PolicyStaging、Workloads の下に Prod・NonProd を置く。管理アカウントは OU に入れない。
2. policy の中身と付け先は内部で生成し、入力にしない（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定1、repository-wide/ADR-0301 決定6）。`FullAWSAccess` と `RCPFullAWSAccess` に触らない（deny-list の方式）。
3. Root に付ける policy は、組織からの離脱とアカウントの閉鎖の拒否（`deny-leave-and-close`）だけとする。AWS が API で作った組織には付かない既定の統制と同じ文である。
4. 新しい policy は PolicyStaging の OU に付けて作り、確かめた後で最終の付け先へ広げる。SCP は確かめる間だけ sandbox のアカウントを PolicyStaging へ移して効き目を見る。RCP は resource の側に付くので PolicyStaging では本番の文を試せない。Policy Test で文書を見て、付けた直後に読み取りで確かめる。RCP を PolicyStaging から最終の付け先へ広げる変更は、他の変更と同じ昇格に混ぜない。混ぜると、RCP が止めた到達と他の変更が壊した到達を見分けられない。
5. SCP の一覧と最終の付け先を次とする。

   | policy | 拒むもの | 最終の付け先 |
   | --- | --- | --- |
   | `deny-leave-and-close` | 組織からの離脱、アカウントの閉鎖 | Root |
   | `deny-bootstrap-role` | `OrganizationAccountAccessRole` のすべての要求 | 立ち上げの中でない各メンバーアカウント |
   | `deny-state-write` | メンバーの state Bucket の object の書き込み。CI の apply 系と lock の解除の role と break-glass の role を除く（repository-wide/ADR-0602 決定11） | Security・Workloads・Sandbox |
   | `protect-governance-roles` | break-glass の role の変更。StackSets の実行の role（`stacksets-exec-*`）を除く（organization-baseline/ADR-0202） | Security・Workloads・Sandbox |
   | `deny-root-user` | `aws:AssumedRoot` の無い root の要求 | Security・Workloads・Sandbox |
   | `deny-iam-user` | IAM user と長期の資格情報の作成 | Security・Workloads |
   | `deny-guardduty-stop` | GuardDuty の停止と削除 | Sandbox |
   | `deny-baseline-stop` | account-baseline のアカウント単位の安全側の設定の停止と変更（Config の recorder と delivery channel の停止と削除、Access Analyzer の analyzer の削除、EBS の既定の暗号化の無効化、アカウントの Public Access Block・VPC Block Public Access・EMR の block public access・SSM の設定の変更）。各アカウントの CI の apply の role を除く | Security・Workloads・Sandbox |
   | `deny-enable-region` | opt-in のリージョンの有効化（account:EnableRegion） | Security・Workloads・Sandbox |
   | `data-perimeter-prod-resources` | Prod の OU に在る resource への操作（role の引き受けを含む） | NonProd・Sandbox |

   アカウント単位の安全側の設定の停止の拒否と、opt-in のリージョンの有効化の拒否（Security・Workloads・Sandbox）の中身は `account-baseline` が決める。組織の trail・GuardDuty・Inspector を守る拒否の中身は `security-tooling` が決める。監査ログの保管庫と鍵を守る拒否（Security）の中身は `audit-evidence` が決める。この usecase はそれらを付ける。

6. RCP の一覧と最終の付け先を次とする。どちらも S3・KMS・Secrets Manager の resource と role の引き受け（`sts:AssumeRole`）を対象とし、AWS のサービスの主体（`aws:PrincipalIsAWSService`）を外す。Web Identity と SAML の引き受けは対象にしない。GitHub の OIDC と Identity Center の到達がそれを使う。

   | policy | 拒むもの | 最終の付け先 |
   | --- | --- | --- |
   | `data-perimeter-org-principals` | 組織の外の主体 | Security・Workloads・Sandbox |
   | `data-perimeter-prod-principals` | Prod の OU の外の主体。role の引き受けだけは管理アカウントの主体に開ける | Prod |

7. EC2 の declarative policy（`ec2-defaults`）で、serial console の無効、AMI の公開共有の新規の拒否（`block_new_sharing`）、EBS snapshot の公開の拒否（`block_all_sharing`）、IMDSv2 の強制（`http_tokens = required`、hop の上限 2、`http_tokens_enforced = enabled`）を固定する。VPC Block Public Access、許す AMI、VPC の暗号化の制御、費用の配分は付けない。最終の付け先は Security・Workloads・Sandbox で、付ける前に人が account status report で影響を見る。
8. `deny-root-user` は、IAM の条件キーの文書が示す形（`ArnLike aws:PrincipalArn = arn:aws:iam::*:root` と `Null aws:AssumedRoot = true`）で書く。`sts:AssumeRoot` の特権のセッションは break-glass が使えないときの回復の経路（repository-wide/ADR-0301 決定19）であり、どの SCP も、その task policy（S3UnlockBucketPolicy・SQSUnlockQueuePolicy）の Action を root のセッションに拒まない。
9. break-glass の role を SCP の例外にするのは `deny-state-write` だけとする（repository-wide/ADR-0301 決定19）。
10. `deny-state-write` の例外は、メンバーの env（security・log-archive・sandbox・dev・stg・prd）ごとの CI の apply 系と lock の解除の role の名前を、env を固定して名指す。名前の一部だけが一致する role を例外にしない。メンバーの state Bucket は、立ち上げの間は管理アカウントから書かれるので、`deny-state-write` を最終の付け先へ広げるのは、そのアカウントの立ち上げが終わった後とする。

## 検討した代替案

### 案A: Infrastructure の OU を作る（SRA のチェックリストの形）

共有のネットワークや ECR を置く専用のアカウントが無く、空の OU を抱える。OU は後から足し、アカウントを移せる。

### 案B: allow-list の SCP（`FullAWSAccess` を外し、使うサービスを列挙する）を併用する

使うサービスを usecase が増えるたびに SCP へ足すことになり、足し忘れが CI と人の作業を止める。deny-list で拒むものを列挙する方が、拒んでいることを Policy Test で見られる。

### 案C: policy を Root に付けてすべてのアカウントに効かせる

試験なしに Root へ付けると、誤りが組織のすべてのアカウントを止める。Sandbox の掃除を妨げる拒否と、Workloads に掛ける拒否を分けられない。

### 案D: VPC Block Public Access を declarative policy で固定する

インターネットゲートウェイを要るユースケース（公開の ALB など）が在り、除外の設計が先に要る。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 同 | 高 | 中（試さずに全体へ） | 高 | 高 |
| Testability | 同 | 低（拒むものが列挙されない） | 低 | 中 | 高（PolicyStaging と Policy Test） |
| Contract Clarity | 低（空の OU） | 低 | 中 | 中 | 高 |
| Maintainability | 中 | 低（usecase ごとに SCP を変える） | 中 | 低（除外の管理） | 高 |
| Scope Control | 低 | 低 | 中 | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 利用者が SCP・RCP・declarative policy を足し引きする口
- 使っていないサービスを SCP で丸ごと拒むこと
- Prod の外の主体に、管理アカウントからの role の引き受けを拒むこと

## 保証範囲

- 保証すること: policy の文書の形、Root と各 OU への付け先が宣言と一致すること（`plan.organization_baseline_scp`）
- 保証すること: どの SCP も、`sts:AssumeRoot` の回復の Action を root のセッションに拒まないこと（同）
- 保証しないこと: SCP・RCP が実際に要求を拒むこと（AWS の仕様。ライブで逆流を試さない）。付けた直後の確認は人が行う
- 保証しないこと: 管理アカウントの主体の操作。SCP と RCP は管理アカウントに効かない
- 保証しないこと: service-linked role の操作。SCP と RCP は service-linked role に効かない
- 保証しないこと: メンバーの中で CI の role を変えられる主体（メンバーの apply-identity の role、break-glass の role）が、CI の role を書き換えて state へ書くこと

## 検証方法

- Unit Test: `internal/organization/tests/organization.tftest.hcl` が OU の木、policy の一覧と種類、文書、付け先、文書の大きさを固定する
- Policy Test: `conftest/plan/organization_baseline_scp.rego`（`PLAN-ORG-*`）が、mock の plan の組織・Root・OU の値から組んだ path と ID で、文書と付け先を見る。実 plan には掛けない
- Policy Test: `PLAN-ORG-09` が、`deny-baseline-stop` が account-baseline の設定を止める・変える Action のすべてを Resource `*` について拒み、例外がメンバーの env の CI の apply の role だけであることと、`deny-enable-region` が `account:EnableRegion` を条件なしに拒むことを見る。拒む Action の一覧の正本は account-baseline/ADR-0201 (baseline-settings-changed-only-by-ci-apply) で、Unit Test も同じ一覧を固定する
- 実環境: PolicyStaging に sandbox を移した間に、拒まれるべき操作が拒まれ、通るべき到達（Identity Center、GitHub の OIDC、break-glass）が通ることを人が確かめ、結果を Pull Request に記録する

## 影響

policy を足す、付け先を広げる変更は、この usecase の変更になる。付け先を広げる変更は、PolicyStaging で確かめた結果を Pull Request に書いてから merge する。

## 見直し条件

- 共有のネットワークや ECR のアカウントを作ると決めたとき（Infrastructure の OU）
- 閉じる運用が始まったとき（Suspended の OU）
- dev と stg に掛けたい禁止が分かれたとき
- 公開の経路を持つユースケースが VPC Block Public Access の除外を設計したとき
- AWS が SCP・RCP の評価の対象（管理アカウント、service-linked role、AssumeRoot のセッション）を変えたとき
