## 上流（registry と GitHub）へ問い合わせるので、必須検査には載せず、週次の報告と手元から起動する。

PARTITIONS_DRIFT_DIR = usecases/deployment-identity/internal/github-oidc

.PHONY: partitions-drift ## aws_partitions の写しが provider の endpoints の一覧と一致するかを報告（ずれと確認不能で失敗）
## make は非0の終了コードを区別しないので、ずれと確認不能を分けるときは STATE= の判定のファイルを読む。
partitions-drift:
	@$(call RUN_SCRIPT,partitions-drift,-lock providers/.terraform.lock.hcl -copy $(PARTITIONS_DRIFT_DIR)/tests/permissions.tftest.hcl $(if $(OUT),-summary-out $(OUT),) $(if $(STATE),-state-out $(STATE),))
