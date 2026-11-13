package stores

import "testing"

func TestDetectProductURLHost(t *testing.T) {
	for _, raw := range []string{
		"https://www.woolworths.com.au/shop/productdetails/123/milk",
		"https://www.chemistwarehouse.com.au/buy/123/product",
		"https://au.iherb.com/pr/product/123",
		"https://www.chemistwarehouse.com.au:443/buy/123/product",
	} {
		if _, err := Detect(raw); err != nil {
			t.Errorf("supported URL %q was rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"https://example.com/?url=woolworths.com.au",
		"https://chemistwarehouse.com.au.invalid/buy/123/product",
		"https://notiherb.com/pr/product/123",
		"https://woolworths.com.au@example.com/product",
		"file://www.chemistwarehouse.com.au/product",
		"https://www.chemistwarehouse.com.au:8080/buy/123/product",
	} {
		if _, err := Detect(raw); err == nil {
			t.Errorf("unsupported URL %q must be rejected", raw)
		}
	}
}
