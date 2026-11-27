# メンバーの形の instance を付け替えた入力で評価し直す plan.deployment_identity_instances が、with で真にする。宣言には置かない。
# 真のとき、管理アカウントにだけ在る構成物（統制の印、メンバーへの引き受け）の存在の要求を外す（deployment-identity/ADR-0201 決定22）。
package plan.lib

default member_form := false
