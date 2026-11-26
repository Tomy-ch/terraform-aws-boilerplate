package anomaly

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceMonitor(t *testing.T) {
	t.Parallel()

	t.Run("SERVICE の monitor がちょうど1つなら、その ARN", func(t *testing.T) {
		t.Parallel()

		got, reason := ServiceMonitor([]Monitor{{Arn: "a", Dimension: "SERVICE"}, {Arn: "b"}})
		assert.Empty(t, reason)
		assert.Equal(t, "a", got)
	})

	for name, tc := range map[string]struct {
		ms   []Monitor
		want string
	}{
		"0件なら理由を返す":          {nil, "AWS services monitor が1つではない（0 個）"},
		"SERVICE が無ければ理由を返す": {[]Monitor{{Arn: "b"}}, "AWS services monitor が1つではない（0 個）"},
		"SERVICE が2つなら理由を返す": {[]Monitor{{Arn: "a", Dimension: "SERVICE"}, {Arn: "b", Dimension: "SERVICE"}}, "AWS services monitor が1つではない（2 個）"},
		"ARN が空なら理由を返す":      {[]Monitor{{Dimension: "SERVICE"}}, "AWS services monitor の ARN を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			arn, reason := ServiceMonitor(tc.ms)
			assert.Empty(t, arn)
			assert.Equal(t, tc.want, reason)
		})
	}
}

func TestMonitorSubscription(t *testing.T) {
	t.Parallel()

	t.Run("monitor を含む subscription がちょうど1つなら、その ARN", func(t *testing.T) {
		t.Parallel()

		got, reason := MonitorSubscription([]Subscription{{Arn: "s", Monitors: []string{"x", "m"}}, {Arn: "t", Monitors: []string{"x"}}}, "m")
		assert.Empty(t, reason)
		assert.Equal(t, "s", got)
	})

	for name, tc := range map[string]struct {
		subs []Subscription
		want string
	}{
		"0件なら理由を返す":            {nil, "その monitor の subscription が1つではない（0 個）"},
		"monitor を含まなければ理由を返す": {[]Subscription{{Arn: "s", Monitors: []string{"x"}}}, "その monitor の subscription が1つではない（0 個）"},
		"2つなら理由を返す":            {[]Subscription{{Arn: "s", Monitors: []string{"m"}}, {Arn: "t", Monitors: []string{"m"}}}, "その monitor の subscription が1つではない（2 個）"},
		"ARN が空なら理由を返す":        {[]Subscription{{Monitors: []string{"m"}}}, "その monitor の subscription の ARN を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			arn, reason := MonitorSubscription(tc.subs, "m")
			assert.Empty(t, arn)
			assert.Equal(t, tc.want, reason)
		})
	}
}

func Test_exactlyOne(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in          []string
		arn, reason string
	}{
		"1つなら、その ARN":    {[]string{"a"}, "a", ""},
		"0個なら理由を返す":      {nil, "", "X が1つではない（0 個）"},
		"2個なら理由を返す":      {[]string{"a", "b"}, "", "X が1つではない（2 個）"},
		"空の ARN なら理由を返す": {[]string{""}, "", "X の ARN を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			arn, reason := exactlyOne(tc.in, "X")
			assert.Equal(t, tc.arn, arn)
			assert.Equal(t, tc.reason, reason)
		})
	}
}
