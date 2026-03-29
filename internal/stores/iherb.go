package stores

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"wishlist-tracker/internal/models"

	"github.com/PuerkitoBio/goquery"
	utls "github.com/refraction-networking/utls"
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

func (i *IHerb) GetProduct(productURL string) (*models.Product, error) {
	// Use a custom transport with utls to impersonate Chrome's TLS fingerprint,
	// which is needed to pass Cloudflare's bot detection from cloud IPs.
	transport := newUTLSTransport()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second}

	req, err := http.NewRequest("GET", productURL, nil)
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

// utlsRoundTripper wraps a utls connection to handle HTTP/1.1 requests
// with a Chrome-like TLS fingerprint.
type utlsRoundTripper struct {
	dialer *net.Dialer
}

func newUTLSTransport() http.RoundTripper {
	return &utlsRoundTripper{
		dialer: &net.Dialer{Timeout: 10 * time.Second},
	}
}

func (u *utlsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(host, port)

	// Get Chrome's TLS hello spec and strip h2 to force HTTP/1.1
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
	if err != nil {
		return nil, fmt.Errorf("get utls spec: %w", err)
	}
	for i, ext := range spec.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			filtered := make([]string, 0, len(alpn.AlpnProtocols))
			for _, proto := range alpn.AlpnProtocols {
				if proto != "h2" {
					filtered = append(filtered, proto)
				}
			}
			alpn.AlpnProtocols = filtered
			spec.Extensions[i] = alpn
			break
		}
	}

	conn, err := u.dialer.DialContext(req.Context(), "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	tlsConn := utls.UClient(conn, &utls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}, utls.HelloCustom)
	if err := tlsConn.ApplyPreset(&spec); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply preset: %w", err)
	}
	if err := tlsConn.Handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}

	// Use a standard HTTP/1.1 transport over the utls connection
	t := &http.Transport{
		DialTLS: func(network, a string) (net.Conn, error) {
			return tlsConn, nil
		},
		DisableKeepAlives: true,
	}
	return t.RoundTrip(req)
}
