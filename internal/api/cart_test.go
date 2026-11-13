package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"wishlist-tracker/internal/db"
	"wishlist-tracker/internal/models"
	"wishlist-tracker/internal/stores"

	"github.com/gin-gonic/gin"
)

func setupCartAPI(t *testing.T) (*db.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.New(filepath.Join(t.TempDir(), "cart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	router := gin.New()
	NewHandler(database, nil).RegisterRoutes(router)
	return database, router
}

func cartRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func requireCartStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected HTTP %d, got %d: %s", status, response.Code, response.Body.String())
	}
}

func TestCartWishlistAndLibraryFlow(t *testing.T) {
	database, router := setupCartAPI(t)
	const email = "shopper@example.com"
	const query = "?email=shopper%40example.com"
	tracked, err := database.CreateItem(email, "https://example.com/milk", "Woolworths", "Fresh Milk", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.RecordPrice(tracked.ID, 2.45); err != nil {
		t.Fatal(err)
	}
	requireCartStatus(t, cartRequest(t, router, http.MethodPost, "/cart/items",
		gin.H{"email": "other@example.com", "item_id": tracked.ID}), http.StatusNotFound)
	for range 2 {
		requireCartStatus(t, cartRequest(t, router, http.MethodPost, "/cart/items",
			gin.H{"email": email, "item_id": tracked.ID}), http.StatusCreated)
	}
	response := cartRequest(t, router, http.MethodGet, "/cart/items"+query, nil)
	requireCartStatus(t, response, http.StatusOK)
	var items []models.CartItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Quantity != 2 || *items[0].CurrentPrice != 2.45 {
		t.Fatalf("wishlist additions should merge: %+v", items)
	}
	id := items[0].ID
	requireCartStatus(t, cartRequest(t, router, http.MethodPatch, "/cart/items/"+id,
		gin.H{"email": email, "quantity": 4}), http.StatusOK)
	requireCartStatus(t, cartRequest(t, router, http.MethodPatch, "/cart/items/"+id,
		gin.H{"email": "other@example.com", "quantity": 7}), http.StatusNotFound)
	requireCartStatus(t, cartRequest(t, router, http.MethodDelete, "/cart/items/"+id+"?email=other%40example.com", nil), http.StatusNotFound)
	requireCartStatus(t, cartRequest(t, router, http.MethodDelete, "/cart/items/"+id+query, nil), http.StatusNoContent)
	if err := database.DeleteItem(tracked.ID); err != nil {
		t.Fatal(err)
	}
	response = cartRequest(t, router, http.MethodGet, "/cart/items"+query, nil)
	requireCartStatus(t, response, http.StatusOK)
	if strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("expected empty cart: %s", response.Body.String())
	}
	response = cartRequest(t, router, http.MethodGet, "/products"+query+"&q=mILk", nil)
	requireCartStatus(t, response, http.StatusOK)
	var products []models.SavedProduct
	if err := json.Unmarshal(response.Body.Bytes(), &products); err != nil || len(products) != 1 || products[0].ID != id {
		t.Fatalf("removed products should remain searchable: %s, err=%v", response.Body.String(), err)
	}
	response = cartRequest(t, router, http.MethodGet, "/products?email=other%40example.com&q=milk", nil)
	requireCartStatus(t, response, http.StatusOK)
	if strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatalf("search must stay within the requested email: %s", response.Body.String())
	}
}

type cartFixtureStore struct {
	base  string
	calls int
}

func (s *cartFixtureStore) Match(productURL string) bool {
	return strings.HasPrefix(productURL, s.base+"/")
}
func (s *cartFixtureStore) Name() string { return "Fixture Store" }
func (s *cartFixtureStore) GetProduct(productURL string) (*models.Product, error) {
	s.calls++
	if strings.HasSuffix(productURL, "/fail") {
		return nil, fmt.Errorf("fixture extraction failed")
	}
	return &models.Product{Name: "Link-only Product", Price: 12.34}, nil
}

func TestCartURLImportAndReuse(t *testing.T) {
	database, router := setupCartAPI(t)
	store := &cartFixtureStore{base: "https://cart-api.test/" + t.Name()}
	stores.Register(store)
	const email = "shopper@example.com"
	response := cartRequest(t, router, http.MethodPost, "/cart/items",
		gin.H{"email": email, "url": store.base + "/product"})
	requireCartStatus(t, response, http.StatusCreated)
	var added models.CartItem
	if err := json.Unmarshal(response.Body.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	if added.Name != "Link-only Product" || added.Quantity != 1 || added.CurrentPrice == nil || *added.CurrentPrice != 12.34 {
		t.Fatalf("URL extraction did not populate the cart: %+v", added)
	}
	tracked, err := database.GetAllItems()
	if err != nil || len(tracked) != 0 {
		t.Fatalf("link-only cart products must not be subscribed to polling: %+v, %v", tracked, err)
	}
	requireCartStatus(t, cartRequest(t, router, http.MethodDelete, "/cart/items/"+added.ID+"?email="+url.QueryEscape(email), nil), http.StatusNoContent)
	response = cartRequest(t, router, http.MethodGet, "/products?email="+url.QueryEscape(email)+"&q=link-only", nil)
	requireCartStatus(t, response, http.StatusOK)
	if !strings.Contains(response.Body.String(), added.ID) {
		t.Fatalf("link-only product should remain in the library: %s", response.Body.String())
	}
	response = cartRequest(t, router, http.MethodPost, "/cart/items",
		gin.H{"email": email, "product_id": added.ID, "quantity": 2})
	requireCartStatus(t, response, http.StatusCreated)
	if store.calls != 1 {
		t.Fatalf("saved product additions must not scrape again, got %d calls", store.calls)
	}
	requireCartStatus(t, cartRequest(t, router, http.MethodPost, "/cart/items",
		gin.H{"email": email, "url": store.base + "/fail"}), http.StatusUnprocessableEntity)
	requireCartStatus(t, cartRequest(t, router, http.MethodPost, "/cart/items",
		gin.H{"email": email, "url": "https://unsupported.example.com/product"}), http.StatusBadRequest)
}

func TestCartValidationAndExports(t *testing.T) {
	database, router := setupCartAPI(t)
	const email = "shopper@example.com"
	const query = "?email=shopper%40example.com"
	price := 1.10
	product, err := database.SaveProduct(email, "https://example.com/milk", "Woolworths", "Bob's Milk & Friends", "", &price)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/cart/items", "/products", "/cart/export.txt", "/cart/export.png"} {
		requireCartStatus(t, cartRequest(t, router, http.MethodGet, path, nil), http.StatusBadRequest)
	}
	requireCartStatus(t, cartRequest(t, router, http.MethodGet,
		"/products"+query+"&q="+url.QueryEscape(strings.Repeat("\u725b", 100)), nil), http.StatusOK)
	requireCartStatus(t, cartRequest(t, router, http.MethodGet,
		"/products"+query+"&q="+url.QueryEscape(strings.Repeat("\u725b", 201)), nil), http.StatusBadRequest)
	for _, body := range []gin.H{
		{"email": email},
		{"email": email, "item_id": "id", "product_id": product.ID},
		{"email": "invalid", "product_id": product.ID},
		{"email": email, "product_id": product.ID, "quantity": 0},
		{"email": email, "product_id": product.ID, "quantity": -1},
		{"email": email, "product_id": product.ID, "quantity": 1000},
		{"email": email, "product_id": product.ID, "quantity": 1.5},
	} {
		requireCartStatus(t, cartRequest(t, router, http.MethodPost, "/cart/items", body), http.StatusBadRequest)
	}
	requireCartStatus(t, cartRequest(t, router, http.MethodGet, "/cart/export.png"+query, nil), http.StatusBadRequest)
	requireCartStatus(t, cartRequest(t, router, http.MethodGet, "/cart/export.txt"+query, nil), http.StatusBadRequest)
	requireCartStatus(t, cartRequest(t, router, http.MethodPost, "/cart/items",
		gin.H{"email": email, "product_id": product.ID, "quantity": 2}), http.StatusCreated)
	for _, quantity := range []any{0, -1, 1000, 1.5} {
		requireCartStatus(t, cartRequest(t, router, http.MethodPatch, "/cart/items/"+product.ID,
			gin.H{"email": email, "quantity": quantity}), http.StatusBadRequest)
	}
	response := cartRequest(t, router, http.MethodGet, "/cart/export.txt"+query, nil)
	requireCartStatus(t, response, http.StatusOK)
	for _, expected := range []string{"Woolworths", "[ ] Bob's Milk & Friends x2", "$2.20", "Generated by Wishlist Tracker"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("text export missing %q: %s", expected, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), email) || response.Header().Get("Cache-Control") != "no-store" {
		t.Error("exports must not include the owner's email or be cached")
	}
	response = cartRequest(t, router, http.MethodGet, "/cart/export.png"+query, nil)
	requireCartStatus(t, response, http.StatusOK)
	if response.Header().Get("Content-Type") != "image/png" || response.Header().Get("X-Cart-Missing-Photos") != "1" {
		t.Fatalf("unexpected image response headers: %v", response.Header())
	}
	exported, err := png.Decode(bytes.NewReader(response.Body.Bytes()))
	if err != nil || exported.Bounds().Dy() <= exported.Bounds().Dx() {
		t.Fatalf("image export must be a valid portrait PNG: err=%v", err)
	}
}
