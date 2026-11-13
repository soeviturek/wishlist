package stores

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"wishlist-tracker/internal/models"

	"github.com/PuerkitoBio/goquery"
)

type Coles struct{}

var colesProductPath = regexp.MustCompile(`^/product/[^/]+-([0-9]+)/?$`)

func (c *Coles) Match(rawURL string) bool {
	return matchesDomain(rawURL, "coles.com.au")
}

func (c *Coles) Name() string {
	return "Coles"
}

func extractColesProductID(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !matchesDomain(rawURL, "coles.com.au") {
		return "", fmt.Errorf("invalid Coles product URL")
	}
	match := colesProductPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return "", fmt.Errorf("expected a Coles product URL ending in its product ID")
	}
	return match[1], nil
}

func (c *Coles) GetProduct(productURL string) (*models.Product, error) {
	productID, err := extractColesProductID(productURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, productURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create Coles request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-AU,en;q=0.9")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch Coles product: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Coles returned HTTP %d", resp.StatusCode)
	}
	return parseColesProduct(resp.Body, productID)
}

func parseColesProduct(reader io.Reader, productID string) (*models.Product, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, fmt.Errorf("read Coles product page: %w", err)
	}
	if doc.Find("iframe[src*='_Incapsula_Resource']").Length() > 0 {
		return nil, fmt.Errorf("Coles blocked automated access; please try again later")
	}
	raw := doc.Find("script#__NEXT_DATA__").First().Text()
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("Coles did not return product data")
	}
	var data struct {
		Props struct {
			PageProps struct {
				Product struct {
					ID      json.Number `json:"id"`
					Name    string      `json:"name"`
					Brand   string      `json:"brand"`
					Size    string      `json:"size"`
					Pricing struct {
						Now *float64 `json:"now"`
					} `json:"pricing"`
				} `json:"product"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("decode Coles product data: %w", err)
	}
	product := data.Props.PageProps.Product
	if product.ID.String() != productID {
		return nil, fmt.Errorf("Coles returned no matching product details for %s", productID)
	}
	name := strings.TrimSpace(product.Name)
	if name == "" {
		return nil, fmt.Errorf("Coles did not return a product name")
	}
	if product.Pricing.Now == nil || *product.Pricing.Now <= 0 {
		return nil, fmt.Errorf("Coles did not return a current price for this product")
	}
	brand, size := strings.TrimSpace(product.Brand), strings.TrimSpace(product.Size)
	if brand != "" && !strings.EqualFold(name, brand) && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(brand)+" ") {
		name = brand + " " + name
	}
	if size != "" && !strings.HasSuffix(strings.ToLower(name), strings.ToLower(size)) {
		name += " " + size
	}
	imageURL, _ := doc.Find("meta[property='og:image']").First().Attr("content")
	return &models.Product{
		Name:     name,
		Price:    *product.Pricing.Now,
		ImageURL: strings.TrimSpace(imageURL),
	}, nil
}
