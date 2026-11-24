# ADR-0002: workload root の usecase が前提にする service-linked role を、identity root で先に作る

- Status: Accepted
- Date: 2026-11-25
- Scope: deployment-identity
- Related: repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0102, repository-wide/ADR-0204, repository-wide/ADR-0301, deployment-identity/ADR-0201, ops-notification/ADR-0201, account-baseline/ADR-0103

## Context

Chatbot の service-linked role `AWSServiceRoleForAWSChatbot`（信頼先 `management.chatbot.amazonaws.com`）は、Chatbot の最初の構成の作成で AWS が作る（Chatbot Administrator Guide「Service-linked role for performing operations on Amazon SNS topics and CloudWatch Logs」）。管理アカウントでは、apply の role がこの role の作成と Chatbot の構成の作成を1秒差で行い、構成の作成は成功で返ったが、provider の region の Topic への購読が作られなかった（#472）。

repository-wide/ADR-0602 決定1 は、この種の構成物をアカウントの前提の構成物と定義し、identity root が bootstrap の構成物を持つ usecase に置く（repository-wide/ADR-0604 決定3）。この usecase は各アカウントの identity root が呼ぶ。use-case が自分の resource に ARN を渡すために同じ apply で作る service-linked role（account-baseline/ADR-0103 の Config の SLR）は、これに当たらない。

## Decision

1. Chatbot の service-linked role を、この usecase の `internal/service-linked-roles` が `aws_iam_service_linked_role` で持つ。identity root の apply で apply-identity の role が作り、新しいアカウントでは段4 の apply が作る（repository-wide/ADR-0602 決定1・9）。作るのは partition が `aws` のときだけとする。Chatbot の service-linked role は AWS GovCloud (US) で使えない（同 Administrator Guide の頁の表）。
2. サービスの集合を入力で受けない。この usecase が固定で持つ（repository-wide/ADR-0204）。workload root の usecase が新しく service-linked role を前提にするときは、この決定に足し、`conftest/plan/decl.json` の `deployment_identity.workload.service_linked_roles` に足す。
3. `description` は、AWS が作成のときに付ける値と同じ literal にする。tag を付けない。`prevent_destroy` を付け、型は `plan.stateful_protection` の保護の対象である。削除の権限はどの role にも与えない（deployment-identity/ADR-0201 決定3）。description の更新の権限は apply-identity の role にだけ与え、人がコンソールで変えた description を次の apply が宣言の値へ戻す。usecase から外すときは `removed` block（`destroy = false`）と、その root の `plan.stateful_protection` の許可の宣言で手放す。
4. identity root の apply より前にアカウントに在る service-linked role は、そのアカウントの identity root の `import` block で取り込み、state を書く前に plan で突合する（repository-wide/ADR-0602 決定6）。取り込みの apply の後に、その `import` block を外す。新しく立ち上がるアカウントには取り込む実体が無く、残すと段2 の plan が落ちるからである。
5. apply の role に残す `iam:CreateServiceLinkedRole` の理由と撤回の条件は、deployment-identity/ADR-0201 決定14 が持つ。
6. 宣言したサービスの service-linked role は、identity root の bootstrap の usecase の下にだけ置き、workload root の usecase の下に置かない。Policy Test（`plan.deployment_identity_apply_role`）が、すべての reference の plan で見る。apply の role の Create が残る間、作成の権限だけでは workload root の usecase が作ることを止められないからである。

## 検討した代替案

### 案A: 新しい usecase（例 `account-prerequisites`）を identity root から呼ぶ

resource が1つの usecase になる。usecase の追加（repository-wide/ADR-0102 決定5・7）と、root-check の usecase の区分の追加が要る。前提の service-linked role が増えたときは、こちらが自然になる（見直し条件）。

### 案B: account-baseline に置く

workload root の usecase なので、新しいアカウントでは ops-notification と同じ apply になり、#472 の競合が残る。account-baseline/ADR-0103 決定3 も持たないとする。

### 案C: ops-notification 自身が作る

\#472 の競合そのものである。

### 案D: 人が CLI で作る

Terraform で作れる構成物を CLI で作らない（repository-wide/ADR-0602 決定5）。

## 保証範囲

- 保証すること: aws の partition の identity root の plan が、この role を1つ持ち、aws でない partition では持たないこと（Unit Test）。この role の削除・置換・管理からの除外が、許可の宣言なしに apply へ進まないこと（`plan.stateful_protection`）。宣言したサービスの service-linked role が workload root の usecase の reference の plan に無いこと（Policy Test）
- 保証しないこと: identity root の apply より前に人が作った role の検出（apply が名前の重複で落ちる）。workload root の usecase の apply が identity root の apply の後に走ること（workload root の usecase の data source が plan で落とす）。env の root の plan（`make tf-policy` は reference の plan だけを読む）

## 検証方法

- Unit Test: `internal/service-linked-roles/tests/`
- Policy Test（`make tf-policy`）: `plan.stateful_protection`（型の保護）、`plan.deployment_identity_apply_role`（`PLAN-APPLY-02` の分類 `service_linked`、決定6 の置き場）

## 見直し条件

- workload root の usecase が前提にする service-linked role が2つ以上になったとき（案A）
- AWS が、Chatbot の構成の作成と同時の service-linked role の作成で購読が欠けないことを示したとき
