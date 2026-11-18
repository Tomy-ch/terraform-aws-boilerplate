output "monitor" {
  description = "monitor の名前と、費用を分ける次元。usecase の Contract Test が、公開入力の届き先を確かめるのに使う。"
  value = {
    name      = aws_ce_anomaly_monitor.this.name
    dimension = aws_ce_anomaly_monitor.this.monitor_dimension
  }
}

output "subscription" {
  description = "subscription の名前・頻度・宛先・閾値の宣言。usecase の Contract Test が、公開入力の届き先を確かめるのに使う。"
  value = {
    name                 = aws_ce_anomaly_subscription.this.name
    frequency            = aws_ce_anomaly_subscription.this.frequency
    subscriber           = aws_ce_anomaly_subscription.this.subscriber
    threshold_expression = aws_ce_anomaly_subscription.this.threshold_expression
  }
}
