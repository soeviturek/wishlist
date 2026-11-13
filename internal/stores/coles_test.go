package stores

import (
	"strings"
	"testing"
)

const colesRosemaryHTML = `<html><head>
<meta property="og:image" content="https://cdn.productimages.coles.com.au/productimages/3/3214780.jpg">
<script id="__NEXT_DATA__" type="application/json">
{"props":{"pageProps":{"product":{"id":3214780,"name":"Herb Punnets Rosemary","brand":"Coles","size":"10g","pricing":{"now":3.5,"was":4,"unit":{"price":35}}},"initialState":{"recommendations":[{"id":999,"name":"Another product","pricing":{"now":99}}]}}}}
</script></head></html>`

func TestParseColesProduct(t *testing.T) {
	for _, html := range []string{
		colesRosemaryHTML,
		strings.Replace(colesRosemaryHTML, `"name":"Herb Punnets Rosemary"`, `"name":"Coles Herb Punnets Rosemary 10g"`, 1),
	} {
		product, err := parseColesProduct(strings.NewReader(html), "3214780")
		if err != nil {
			t.Fatal(err)
		}
		if product.Name != "Coles Herb Punnets Rosemary 10g" {
			t.Errorf("unexpected product name: %q", product.Name)
		}
		if product.Price != 3.5 {
			t.Errorf("expected current price, not original, unit or recommendation price: got %v", product.Price)
		}
		if product.ImageURL != "https://cdn.productimages.coles.com.au/productimages/3/3214780.jpg" {
			t.Errorf("unexpected product image: %q", product.ImageURL)
		}
	}
}

func TestParseColesProductRejectsUnavailableData(t *testing.T) {
	for _, tc := range []struct {
		name string
		html string
		want string
	}{
		{"blocked page", `<iframe src="/_Incapsula_Resource?error=1"></iframe>`, "blocked"},
		{"missing product data", `<html><h1>Product not found</h1></html>`, "product data"},
		{"invalid data", `<script id="__NEXT_DATA__">{"props":</script>`, "decode Coles product data"},
		{"wrong product", strings.Replace(colesRosemaryHTML, `"id":3214780`, `"id":123`, 1), "matching product"},
		{"missing name", strings.Replace(colesRosemaryHTML, `"name":"Herb Punnets Rosemary"`, `"name":""`, 1), "product name"},
		{"missing current price", strings.Replace(colesRosemaryHTML, `"now":3.5`, `"now":null`, 1), "current price"},
		{"zero price", strings.Replace(colesRosemaryHTML, `"now":3.5`, `"now":0`, 1), "current price"},
		{"negative price", strings.Replace(colesRosemaryHTML, `"now":3.5`, `"now":-1`, 1), "current price"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			product, err := parseColesProduct(strings.NewReader(tc.html), "3214780")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got product=%+v, error=%v", tc.want, product, err)
			}
			if product != nil {
				t.Error("unavailable product data must not produce a usable product")
			}
		})
	}
}

func TestExtractColesProductID(t *testing.T) {
	for _, rawURL := range []string{
		"https://www.coles.com.au/product/coles-herb-punnets-rosemary-10g-3214780",
		"https://www.coles.com.au/product/coles-herb-punnets-rosemary-10g-3214780/?utm_source=test",
	} {
		id, err := extractColesProductID(rawURL)
		if err != nil || id != "3214780" {
			t.Errorf("extract ID from %q: got %q, %v", rawURL, id, err)
		}
	}
	for _, rawURL := range []string{
		"https://www.coles.com.au/browse/fruit",
		"https://www.coles.com.au/product/rosemary",
		"https://www.coles.com.au/?url=/product/rosemary-3214780",
		"https://www.coles.com.au.invalid/product/rosemary-3214780",
		"https://www.coles.com.au@localhost/product/rosemary-3214780",
	} {
		if _, err := extractColesProductID(rawURL); err == nil {
			t.Errorf("invalid product URL %q must be rejected", rawURL)
		}
	}
}
