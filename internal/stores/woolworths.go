package stores

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"time"

	"wishlist-tracker/internal/models"
)

const (
	woolworthsBaseURL = "https://www.woolworths.com.au"
	woolworthsAPIPath = "/apis/ui/product/detail/"
	woolworthsUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// stockcodeRe extracts the numeric stockcode from a Woolworths product URL.
// e.g. /shop/productdetails/708119/monster-energy-juice-mango-loco → 708119
var stockcodeRe = regexp.MustCompile(`/productdetails/(\d+)`)

// Woolworths fetches product data from woolworths.com.au using their
// internal JSON API. The site is a JS SPA so HTML scraping does not
// return prices — instead we:
//  1. GET the homepage to obtain session cookies (Akamai bot-management).
//  2. GET /apis/ui/product/detail/{stockcode} with those cookies.
type Woolworths struct{}

func (w *Woolworths) Match(url string) bool {
	return strings.Contains(strings.ToLower(url), "woolworths.com.au")
}

func (w *Woolworths) Name() string {
	return "Woolworths"
}

// extractStockcode pulls the numeric product ID from a Woolworths URL.
func extractStockcode(url string) (string, error) {
	m := stockcodeRe.FindStringSubmatch(url)
	if len(m) < 2 {
		return "", fmt.Errorf("could not extract stockcode from URL: %s", url)
	}
	return m[1], nil
}

// newWoolworthsClient creates an HTTP client with a cookie jar and
// performs an initial request to the homepage to pick up session cookies
// needed by the API.
func newWoolworthsClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}

	// Force HTTP/1.1 — Woolworths sometimes resets HTTP/2 streams.
	transport := &http.Transport{
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}
	client := &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   20 * time.Second,
	}

	// Warm-up: visit the homepage to collect Akamai cookies.
	req, err := http.NewRequest("GET", woolworthsBaseURL+"/", nil)
	if err != nil {
		return nil, fmt.Errorf("create warmup request: %w", err)
	}
	req.Header.Set("User-Agent", woolworthsUA)
	req.Header.Set("Accept", "text/html,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("warmup request: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	return client, nil
}

// woolworthsAPIResponse represents the top-level JSON returned by the
// product detail API.
type woolworthsAPIResponse struct {
	Product woolworthsProduct `json:"Product"`
}

type woolworthsProduct struct {
	Stockcode       int     `json:"Stockcode"`
	Name            string  `json:"Name"`
	DisplayName     string  `json:"DisplayName"`
	Price           float64 `json:"Price"`
	WasPrice        float64 `json:"WasPrice"`
	IsOnSpecial     bool    `json:"IsOnSpecial"`
	IsInStock       bool    `json:"IsInStock"`
	IsAvailable     bool    `json:"IsAvailable"`
	SmallImageFile  string  `json:"SmallImageFile"`
	MediumImageFile string  `json:"MediumImageFile"`
	LargeImageFile  string  `json:"LargeImageFile"`
}

func (w *Woolworths) GetProduct(productURL string) (*models.Product, error) {
	stockcode, err := extractStockcode(productURL)
	if err != nil {
		return nil, err
	}

	client, err := newWoolworthsClient()
	if err != nil {
		return nil, fmt.Errorf("init client: %w", err)
	}

	apiURL := woolworthsBaseURL + woolworthsAPIPath + stockcode
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create api request: %w", err)
	}
	req.Header.Set("User-Agent", woolworthsUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", woolworthsBaseURL+"/shop/productdetails/"+stockcode)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp woolworthsAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode api response: %w", err)
	}

	p := apiResp.Product
	if p.Name == "" && p.DisplayName == "" {
		return nil, fmt.Errorf("product not found for stockcode %s", stockcode)
	}
	if p.Price == 0 && !p.IsInStock {
		return nil, fmt.Errorf("product %q is unavailable (not in stock, price=0)", p.DisplayName)
	}

	name := p.DisplayName
	if name == "" {
		name = p.Name
	}

	// Pick the best image available (large > medium > small).
	imageURL := p.LargeImageFile
	if imageURL == "" {
		imageURL = p.MediumImageFile
	}
	if imageURL == "" {
		imageURL = p.SmallImageFile
	}

	return &models.Product{
		Name:     name,
		Price:    p.Price,
		ImageURL: imageURL,
	}, nil
}
