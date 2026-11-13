package stores

import (
	"fmt"
	"net/url"
	"strings"

	"wishlist-tracker/internal/models"
)

func matchesDomain(rawURL, domain string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return false
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443" || u.Scheme == "http" && port == "80") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// Store is the interface every retailer scraper must implement.
type Store interface {
	// Match returns true if the URL belongs to this store.
	Match(url string) bool
	// Name returns the human-readable store name.
	Name() string
	// GetProduct scrapes the product page and returns name + price.
	GetProduct(url string) (*models.Product, error)
}

// registry holds all registered store scrapers.
var registry []Store

// Register adds a store scraper to the registry.
func Register(s Store) {
	registry = append(registry, s)
}

// Detect finds the appropriate store scraper for a URL.
func Detect(url string) (Store, error) {
	for _, s := range registry {
		if s.Match(url) {
			return s, nil
		}
	}
	return nil, fmt.Errorf("unsupported store for URL: %s", url)
}

// init registers all built-in store scrapers.
func init() {
	Register(&ChemistWarehouse{})
	Register(&Woolworths{})
	Register(&Coles{})
	Register(&IHerb{})
}
