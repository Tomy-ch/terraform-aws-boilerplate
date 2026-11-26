// Package anomaly は、Cost Explorer を有効にしたときに AWS が作る AWS services monitor と、その通知の subscription を
// ちょうど1つに決めます。bootstrap の確認（bootstrap-check）と手順の実行（setup）が同じ決め方を使います。
package anomaly

import (
	"fmt"
	"slices"
)

// Monitor は、get-anomaly-monitors の AnomalyMonitors の1件。
type Monitor struct {
	Arn       string `json:"MonitorArn"`
	Dimension string `json:"MonitorDimension"`
}

// Subscription は、get-anomaly-subscriptions の AnomalySubscriptions の1件。
type Subscription struct {
	Arn      string   `json:"SubscriptionArn"`
	Monitors []string `json:"MonitorArnList"`
}

// ServiceMonitor は、MonitorDimension が SERVICE の monitor の ARN。ちょうど1つに決まらなければ、ARN の代わりに
// 理由を返す。
func ServiceMonitor(monitors []Monitor) (arn, reason string) {
	var arns []string

	for _, m := range monitors {
		if m.Dimension == "SERVICE" {
			arns = append(arns, m.Arn)
		}
	}

	return exactlyOne(arns, "AWS services monitor")
}

// MonitorSubscription は、MonitorArnList に monitor を含む subscription の ARN。ちょうど1つに決まらなければ、
// ARN の代わりに理由を返す。
func MonitorSubscription(subs []Subscription, monitor string) (arn, reason string) {
	var arns []string

	for _, s := range subs {
		if slices.Contains(s.Monitors, monitor) {
			arns = append(arns, s.Arn)
		}
	}

	return exactlyOne(arns, "その monitor の subscription")
}

func exactlyOne(arns []string, what string) (string, string) {
	if len(arns) != 1 {
		return "", fmt.Sprintf("%s が1つではない（%d 個）", what, len(arns))
	}

	if arns[0] == "" {
		return "", what + " の ARN を読めない"
	}

	return arns[0], ""
}
