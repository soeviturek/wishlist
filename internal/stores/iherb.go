package stores

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"wishlist-tracker/internal/models"

	"github.com/PuerkitoBio/goquery"
)

// iherbProductRe extracts the numeric product ID from an iHerb URL.
// e.g. /pr/now-foods-l-citrulline-750-mg-180-veg-capsules/14209 → 14209
var iherbProductRe = regexp.MustCompile(`/pr/[^/]+/(\d+)`)

// IHerb scrapes products from iherb.com (including au.iherb.com etc.).
// Product data is extracted from the JSON-LD structured data embedded in the page.
type IHerb struct{}

func (i *IHerb) Match(url string) bool {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "iherb.com")
}

func (i *IHerb) Name() string {
	return "iHerb"
}

// iherbLDProduct mirrors the schema.org/Product JSON-LD embedded in iHerb pages.
type iherbLDProduct struct {
	Type  string     `json:"@type"`
	Name  string     `json:"name"`
	Image string     `json:"image"`
	Offer iherbOffer `json:"offers"`
}

type iherbOffer struct {
	Price    string `json:"price"`
	Currency string `json:"priceCurrency"`
}

func (i *IHerb) GetProduct(url string) (*models.Product, error) {
	client := &http.Client{Timeout: 20 * time.Second}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

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

	// Extract product data from JSON-LD script tags.
	var product iherbLDProduct
	var found bool

	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		if found {
			return
		}
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(s.Text()), &raw); err != nil {
			return
		}
		var ld iherbLDProduct
		if err := json.Unmarshal(raw, &ld); err != nil {
			return
		}
		if ld.Type == "Product" && ld.Name != "" {
			product = ld
			found = true
		}
	})

	if !found {
		return nil, fmt.Errorf("could not find product JSON-LD data")
	}

	price, err := strconv.ParseFloat(product.Offer.Price, 64)
	if err != nil || price == 0 {
		return nil, fmt.Errorf("could not parse price %q: %w", product.Offer.Price, err)
	}

	// Prefer og:image for a cleaner image URL, fall back to JSON-LD.
	imageURL := product.Image
	if ogImg, exists := doc.Find(`meta[property="og:image"]`).First().Attr("content"); exists && ogImg != "" {
		imageURL = ogImg
	}

	return &models.Product{
		Name:     product.Name,
		Price:    price,
		ImageURL: imageURL,
	}, nil
}
