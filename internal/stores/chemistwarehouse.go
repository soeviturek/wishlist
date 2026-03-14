package stores

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"wishlist-tracker/internal/models"

	"github.com/PuerkitoBio/goquery"
)

// cwPriceRe extracts the sale price from CW's embedded JSON:
//
//	"price":{"value":{"amount":14.99,"currencyCode":"AUD"},...}
var cwPriceRe = regexp.MustCompile(`"price"\s*:\s*\{\s*"value"\s*:\s*\{\s*"amount"\s*:\s*([\d.]+)`)

// cwRRPRe extracts the RRP (recommended retail price) as a fallback:
//
//	"rrp":{"amount":17,"currencyCode":"AUD"}
var cwRRPRe = regexp.MustCompile(`"rrp"\s*:\s*\{\s*"amount"\s*:\s*([\d.]+)`)

// ChemistWarehouse scrapes products from chemistwarehouse.com.au.
// The site is now a JS SPA — prices are not in visible HTML elements
// but are embedded as JSON in a <script> tag. We extract them with regex.
type ChemistWarehouse struct{}

func (c *ChemistWarehouse) Match(url string) bool {
	return strings.Contains(strings.ToLower(url), "chemistwarehouse.com.au")
}

func (c *ChemistWarehouse) Name() string {
	return "Chemist Warehouse"
}

func (c *ChemistWarehouse) GetProduct(url string) (*models.Product, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	// --- Name ---
	// 1. Try <h1> first (cleanest)
	name := strings.TrimSpace(doc.Find("h1").First().Text())
	// 2. Fallback to og:title (strip " | Chemist Warehouse" suffix)
	if name == "" {
		ogTitle, _ := doc.Find(`meta[property="og:title"]`).Attr("content")
		ogTitle = strings.TrimSpace(ogTitle)
		if idx := strings.Index(ogTitle, " online at "); idx > 0 {
			ogTitle = ogTitle[:idx]
		}
		if strings.HasPrefix(ogTitle, "Buy ") {
			ogTitle = ogTitle[4:]
		}
		name = ogTitle
	}
	if name == "" {
		return nil, fmt.Errorf("could not find product name")
	}

	// --- Price ---
	// CW embeds product data as JSON in script tags.
	// Grab the full HTML text to regex-match the price.
	fullHTML, _ := doc.Html()

	var price float64
	if m := cwPriceRe.FindStringSubmatch(fullHTML); len(m) >= 2 {
		price, err = strconv.ParseFloat(m[1], 64)
		if err != nil {
			return nil, fmt.Errorf("parse embedded price %q: %w", m[1], err)
		}
	}

	// Fallback: try RRP if sale price was 0
	if price == 0 {
		if m := cwRRPRe.FindStringSubmatch(fullHTML); len(m) >= 2 {
			price, _ = strconv.ParseFloat(m[1], 64)
		}
	}

	// Fallback: try old-school HTML selectors and meta tags
	if price == 0 {
		priceStr := ""
		for _, sel := range []string{
			"span.product__price", "span.Price", "span[class*='price']",
			".product-price .price", ".price-amount",
		} {
			text := strings.TrimSpace(doc.Find(sel).First().Text())
			if text != "" {
				priceStr = text
				break
			}
		}
		if priceStr == "" {
			priceStr, _ = doc.Find(`meta[property="product:price:amount"]`).Attr("content")
		}
		if priceStr != "" {
			price, err = parsePrice(priceStr)
			if err != nil {
				return nil, fmt.Errorf("parse price %q: %w", priceStr, err)
			}
		}
	}

	if price == 0 {
		return nil, fmt.Errorf("could not find product price")
	}

	// --- Image ---
	imageURL, _ := doc.Find(`meta[property="og:image"]`).Attr("content")
	if imageURL == "" {
		imageURL, _ = doc.Find(`img.product-image, img.product__image, .product-image img`).First().Attr("src")
	}

	return &models.Product{Name: name, Price: price, ImageURL: imageURL}, nil
}

// parsePrice extracts a float64 from a price string like "$18.50" or "18.50".
func parsePrice(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)

	if s == "" {
		return 0, fmt.Errorf("empty price string")
	}

	return strconv.ParseFloat(s, 64)
}
