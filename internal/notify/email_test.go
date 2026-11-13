package notify

import (
	"math"
	"strings"
	"testing"
)

func TestPriceAlertRendering(t *testing.T) {
	for _, tc := range []struct {
		name     string
		previous float64
		current  float64
		isTarget bool
	}{
		{"price-drop", 109.99, 89.99, false},
		{"unchanged-target", 89.99, 89.99, true},
		{"subcent-target", math.Nextafter(89.99, math.Inf(1)), 89.99, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alert := PriceDropAlert{
				ProductName: "Whey Protein",
				ProductURL:  "https://example.test/protein",
				OldPrice:    tc.previous,
				NewPrice:    tc.current,
				IsTarget:    tc.isTarget,
			}
			for name, body := range map[string]string{
				"single": buildHTMLBody(alert, "Price alert"),
				"digest": buildDigestHTML([]PriceDropAlert{alert}, "Price alerts"),
			} {
				t.Run(name, func(t *testing.T) {
					if !tc.isTarget {
						if !strings.Contains(body, "$109.99") || !strings.Contains(body, "$89.99") || !strings.Contains(body, "$20.00") {
							t.Error("real price drop must display $109.99 to $89.99 with $20.00 savings")
						}
						return
					}
					if strings.Count(body, "$89.99") != 1 || !strings.Contains(body, "Current Price") {
						t.Errorf("unchanged target alert should show Current Price once: price count=%d, current label=%v", strings.Count(body, "$89.99"), strings.Contains(body, "Current Price"))
					}
					for _, misleading := range []string{"<s ", "Old Price", "New Price", "save $0.00"} {
						if strings.Contains(body, misleading) {
							t.Errorf("unchanged target alert contains %q", misleading)
						}
					}
				})
			}
		})
	}
}
