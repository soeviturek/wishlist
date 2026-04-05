package db

import (
	"os"
	"path/filepath"
	"testing"
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
