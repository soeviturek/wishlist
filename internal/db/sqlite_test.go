package db

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"wishlist-tracker/internal/models"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	d, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() {
		d.Close()
		os.Remove(dbPath)
	})
	return d
}

func TestItemExistsByEmailAndURL_NoDuplicate(t *testing.T) {
	d := setupTestDB(t)

	exists, err := d.ItemExistsByEmailAndURL("test@example.com", "https://example.com/product/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected no duplicate, but got exists=true")
	}
}

func TestItemExistsByEmailAndURL_Duplicate(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.CreateItem("test@example.com", "https://example.com/product/1", "TestStore", "Product 1", "", nil)
	if err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	exists, err := d.ItemExistsByEmailAndURL("test@example.com", "https://example.com/product/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected duplicate, but got exists=false")
	}
}

func TestItemExistsByEmailAndURL_DifferentEmail(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.CreateItem("user1@example.com", "https://example.com/product/1", "TestStore", "Product 1", "", nil)
	if err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	// Same URL but different email should not be a duplicate
	exists, err := d.ItemExistsByEmailAndURL("user2@example.com", "https://example.com/product/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("different email should not be considered a duplicate")
	}
}

func TestItemExistsByEmailAndURL_DifferentURL(t *testing.T) {
	d := setupTestDB(t)

	_, err := d.CreateItem("test@example.com", "https://example.com/product/1", "TestStore", "Product 1", "", nil)
	if err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	// Same email but different URL should not be a duplicate
	exists, err := d.ItemExistsByEmailAndURL("test@example.com", "https://example.com/product/2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("different URL should not be considered a duplicate")
	}
}

func TestCartLibraryKeepsProductsAndTracksPrices(t *testing.T) {
	d := setupTestDB(t)
	const email = "shopper@example.com"
	const url = "https://www.woolworths.com.au/shop/productdetails/123/milk"
	tracked, err := d.CreateItem(email, url, "Woolworths", "Fresh Milk", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.RecordPrice(tracked.ID, 2.45); err != nil {
		t.Fatal(err)
	}
	product, err := d.GetSavedProductByURL(email, url)
	if err != nil || product == nil || product.CurrentPrice == nil || *product.CurrentPrice != 2.45 {
		t.Fatalf("wishlist product was not saved: product=%+v err=%v", product, err)
	}
	for range 2 {
		if _, err := d.AddCartItem(email, product.ID, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.RecordPrice(tracked.ID, 3.20); err != nil {
		t.Fatal(err)
	}
	items, err := d.ListCartItems(email)
	if err != nil || len(items) != 1 || items[0].Quantity != 2 || *items[0].CurrentPrice != 3.20 {
		t.Fatalf("cart should merge quantities and reflect new prices: items=%+v err=%v", items, err)
	}
	if item, err := d.SetCartQuantity(email, product.ID, 5); err != nil || item.Quantity != 5 {
		t.Fatalf("quantity was not updated: item=%+v err=%v", item, err)
	}
	if err := d.DeleteCartItem(email, product.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteItem(tracked.ID); err != nil {
		t.Fatal(err)
	}
	products, err := d.SearchProducts(email, "fReSh")
	if err != nil || len(products) != 1 || products[0].ID != product.ID || *products[0].CurrentPrice != 3.20 {
		t.Fatalf("removed products should remain searchable: products=%+v err=%v", products, err)
	}
	items, err = d.ListCartItems(email)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("cart should be an empty array: items=%+v err=%v", items, err)
	}
}

func TestCartProductOwnershipAndQuantityLimits(t *testing.T) {
	d := setupTestDB(t)
	const email = "shopper@example.com"
	const otherEmail = "other@example.com"
	const url = "https://example.com/product/1"
	product, err := d.SaveProduct(email, url, "Test Store", "100% Milk", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0.0
	refreshed, err := d.SaveProduct(email, url, "Test Store", "100% Milk", "", &zero)
	if err != nil || refreshed.ID != product.ID || refreshed.CurrentPrice == nil || *refreshed.CurrentPrice != 0 {
		t.Fatalf("upsert should preserve identity and accept a zero price: product=%+v err=%v", refreshed, err)
	}
	other, err := d.SaveProduct(otherEmail, url, "Test Store", "100% Milk", "", nil)
	if err != nil || other.ID == product.ID {
		t.Fatalf("libraries should be independent: product=%+v err=%v", other, err)
	}
	results, err := d.SearchProducts(email, "%")
	if err != nil || len(results) != 1 || results[0].ID != product.ID {
		t.Fatalf("search should be literal and scoped: products=%+v err=%v", results, err)
	}
	if found, err := d.GetSavedProduct(otherEmail, product.ID); err != nil || found != nil {
		t.Fatalf("another email must not see the product: product=%+v err=%v", found, err)
	}
	if _, err := d.AddCartItem(otherEmail, product.ID, 1); !errors.Is(err, ErrCartItemNotFound) {
		t.Fatalf("another email must not add the product: %v", err)
	}
	if _, err := d.AddCartItem(email, product.ID, 2); err != nil {
		t.Fatal(err)
	}
	for _, quantity := range []int{0, -1, models.MaxCartQuantity + 1} {
		if _, err := d.SetCartQuantity(email, product.ID, quantity); !errors.Is(err, ErrCartQuantity) {
			t.Fatalf("quantity %d should be rejected: %v", quantity, err)
		}
	}
	if _, err := d.AddCartItem(email, product.ID, models.MaxCartQuantity); !errors.Is(err, ErrCartQuantity) {
		t.Fatalf("adding above the limit should fail atomically: %v", err)
	}
	if _, err := d.SetCartQuantity(otherEmail, product.ID, 3); !errors.Is(err, ErrCartItemNotFound) {
		t.Fatalf("another email must not update the cart: %v", err)
	}
	if err := d.DeleteCartItem(otherEmail, product.ID); !errors.Is(err, ErrCartItemNotFound) {
		t.Fatalf("another email must not delete from the cart: %v", err)
	}
	items, err := d.ListCartItems(email)
	if err != nil || len(items) != 1 || items[0].Quantity != 2 {
		t.Fatalf("rejected mutations must not change the cart: items=%+v err=%v", items, err)
	}
	tracked, err := d.GetAllItems()
	if err != nil || len(tracked) != 0 {
		t.Fatalf("cart-only products must not become tracked subscriptions: items=%+v err=%v", tracked, err)
	}
}

func TestCartMigrationImportsExistingWishlist(t *testing.T) {
	d := setupTestDB(t)
	_, err := d.conn.Exec(`
		INSERT INTO items (id, email, url, store, name, created_at)
		VALUES ('legacy', 'legacy@example.com', 'https://example.com/legacy', 'Legacy Store', 'Legacy Product', '2025-01-01 10:00:00');
		INSERT INTO prices (id, item_id, price, recorded_at)
		VALUES ('old-price', 'legacy', 5.00, '2025-01-01 10:00:00'),
		       ('new-price', 'legacy', 4.25, '2025-01-02 10:00:00');
	`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := d.migrateCart(); err != nil {
			t.Fatal(err)
		}
	}
	products, err := d.SearchProducts("legacy@example.com", "legacy")
	if err != nil || len(products) != 1 || products[0].CurrentPrice == nil || *products[0].CurrentPrice != 4.25 {
		t.Fatalf("migration should import the latest price without duplicating products: products=%+v err=%v", products, err)
	}
}

func TestCartPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persistent.db")
	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	product, err := d.SaveProduct("test@example.com", "https://example.com/1", "Store", "Product", "", nil)
	if err != nil {
		d.Close()
		t.Fatal(err)
	}
	if _, err := d.AddCartItem("test@example.com", product.ID, 7); err != nil {
		d.Close()
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err := reopened.ListCartItems("test@example.com")
	if err != nil || len(items) != 1 || items[0].Quantity != 7 {
		t.Fatalf("cart did not survive reopening the database: items=%+v err=%v", items, err)
	}
}
