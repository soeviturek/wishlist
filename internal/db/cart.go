package db

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"wishlist-tracker/internal/models"

	"github.com/google/uuid"
)

var (
	ErrCartItemNotFound = errors.New("cart item not found")
	ErrCartQuantity     = errors.New("quantity must be between 1 and 999")
)

const productColumns = `id, email, url, store, name, image_url, current_price, price_updated_at, created_at`

func (d *DB) migrateCart() error {
	_, err := d.conn.Exec(`
		CREATE TABLE IF NOT EXISTS products (
			id               TEXT PRIMARY KEY,
			email            TEXT NOT NULL,
			url              TEXT NOT NULL,
			store            TEXT NOT NULL,
			name             TEXT NOT NULL,
			image_url        TEXT NOT NULL DEFAULT '',
			current_price    REAL CHECK (current_price >= 0),
			price_updated_at TIMESTAMP,
			created_at       TIMESTAMP NOT NULL,
			UNIQUE (email, url)
		);
		CREATE TABLE IF NOT EXISTS cart_items (
			product_id TEXT PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
			quantity   INTEGER NOT NULL CHECK (quantity BETWEEN 1 AND 999),
			created_at TIMESTAMP NOT NULL
		);

		INSERT INTO products (` + productColumns + `)
		SELECT i.id, i.email, i.url, i.store, i.name, i.image_url,
			(SELECT price FROM prices WHERE item_id = i.id ORDER BY recorded_at DESC, rowid DESC LIMIT 1),
			(SELECT recorded_at FROM prices WHERE item_id = i.id ORDER BY recorded_at DESC, rowid DESC LIMIT 1),
			i.created_at
		FROM items i WHERE true
		ORDER BY i.created_at DESC, i.rowid DESC
		ON CONFLICT (email, url) DO NOTHING;
	`)
	if err != nil {
		return fmt.Errorf("migrate product library and cart: %w", err)
	}
	return nil
}

type productWriter interface {
	QueryRow(string, ...any) *sql.Row
}

type rowScanner interface {
	Scan(...any) error
}

func scanProduct(row rowScanner) (*models.SavedProduct, error) {
	var product models.SavedProduct
	var priceUpdatedAt sql.NullString
	var createdAt string
	err := row.Scan(&product.ID, &product.Email, &product.URL, &product.Store, &product.Name,
		&product.ImageURL, &product.CurrentPrice, &priceUpdatedAt, &createdAt)
	if err != nil {
		return nil, err
	}
	product.CreatedAt = parseTime(createdAt)
	if priceUpdatedAt.Valid {
		t := parseTime(priceUpdatedAt.String)
		product.PriceUpdatedAt = &t
	}
	return &product, nil
}

func saveProduct(writer productWriter, email, url, store, name, imageURL string, price *float64, now time.Time) (*models.SavedProduct, error) {
	if strings.TrimSpace(name) == "" || (price != nil && (math.IsNaN(*price) || math.IsInf(*price, 0) || *price < 0)) {
		return nil, fmt.Errorf("invalid product name or price")
	}
	var priceUpdatedAt any
	if price != nil {
		priceUpdatedAt = fmtTime(now)
	}
	product, err := scanProduct(writer.QueryRow(`
		INSERT INTO products (`+productColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (email, url) DO UPDATE SET
			store = excluded.store,
			name = excluded.name,
			image_url = CASE WHEN excluded.image_url = '' THEN products.image_url ELSE excluded.image_url END,
			current_price = COALESCE(excluded.current_price, products.current_price),
			price_updated_at = CASE WHEN excluded.current_price IS NULL
				THEN products.price_updated_at ELSE excluded.price_updated_at END
		RETURNING `+productColumns,
		uuid.New().String(), email, url, store, name, imageURL, price, priceUpdatedAt, fmtTime(now),
	))
	if err != nil {
		return nil, fmt.Errorf("save product: %w", err)
	}
	return product, nil
}

func (d *DB) SaveProduct(email, url, store, name, imageURL string, price *float64) (*models.SavedProduct, error) {
	return saveProduct(d.conn, email, url, store, name, imageURL, price, time.Now().UTC())
}

func (d *DB) GetSavedProduct(email, id string) (*models.SavedProduct, error) {
	product, err := scanProduct(d.conn.QueryRow(`SELECT `+productColumns+` FROM products WHERE email = ? AND id = ?`, email, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved product: %w", err)
	}
	return product, nil
}

func (d *DB) GetSavedProductByURL(email, url string) (*models.SavedProduct, error) {
	product, err := scanProduct(d.conn.QueryRow(`SELECT `+productColumns+` FROM products WHERE email = ? AND url = ?`, email, url))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved product by URL: %w", err)
	}
	return product, nil
}

func (d *DB) SearchProducts(email, query string) ([]models.SavedProduct, error) {
	rows, err := d.conn.Query(`SELECT `+productColumns+` FROM products
		WHERE email = ? AND instr(lower(name), lower(?)) > 0
		ORDER BY name COLLATE NOCASE, id LIMIT 30`, email, strings.TrimSpace(query))
	if err != nil {
		return nil, fmt.Errorf("search products: %w", err)
	}
	defer rows.Close()

	products := make([]models.SavedProduct, 0)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("scan saved product: %w", err)
		}
		products = append(products, *product)
	}
	return products, rows.Err()
}

func scanCartItem(row rowScanner) (*models.CartItem, error) {
	var item models.CartItem
	var priceUpdatedAt sql.NullString
	var createdAt, addedAt string
	err := row.Scan(&item.ID, &item.Email, &item.URL, &item.Store, &item.Name,
		&item.ImageURL, &item.CurrentPrice, &priceUpdatedAt, &createdAt, &item.Quantity, &addedAt)
	if err != nil {
		return nil, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.AddedAt = parseTime(addedAt)
	if priceUpdatedAt.Valid {
		t := parseTime(priceUpdatedAt.String)
		item.PriceUpdatedAt = &t
	}
	return &item, nil
}

const cartSelect = `SELECT p.id, p.email, p.url, p.store, p.name, p.image_url,
	p.current_price, p.price_updated_at, p.created_at, c.quantity, c.created_at
	FROM cart_items c JOIN products p ON p.id = c.product_id`

func (d *DB) ListCartItems(email string) ([]models.CartItem, error) {
	rows, err := d.conn.Query(cartSelect+` WHERE p.email = ? ORDER BY p.store COLLATE NOCASE, c.created_at, p.name, p.id`, email)
	if err != nil {
		return nil, fmt.Errorf("list cart: %w", err)
	}
	defer rows.Close()

	items := make([]models.CartItem, 0)
	for rows.Next() {
		item, err := scanCartItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cart item: %w", err)
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (d *DB) getCartItem(email, id string) (*models.CartItem, error) {
	item, err := scanCartItem(d.conn.QueryRow(cartSelect+` WHERE p.email = ? AND p.id = ?`, email, id))
	if err == sql.ErrNoRows {
		return nil, ErrCartItemNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get cart item: %w", err)
	}
	return item, nil
}

func (d *DB) AddCartItem(email, productID string, quantity int) (*models.CartItem, error) {
	if quantity < 1 || quantity > models.MaxCartQuantity {
		return nil, ErrCartQuantity
	}
	product, err := d.GetSavedProduct(email, productID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, ErrCartItemNotFound
	}

	result, err := d.conn.Exec(`
		INSERT INTO cart_items (product_id, quantity, created_at) VALUES (?, ?, ?)
		ON CONFLICT (product_id) DO UPDATE SET quantity = cart_items.quantity + excluded.quantity
		WHERE cart_items.quantity + excluded.quantity <= ?`,
		productID, quantity, fmtTime(time.Now()), models.MaxCartQuantity)
	if err != nil {
		return nil, fmt.Errorf("add cart item: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrCartQuantity
	}
	return d.getCartItem(email, productID)
}

func (d *DB) SetCartQuantity(email, id string, quantity int) (*models.CartItem, error) {
	if quantity < 1 || quantity > models.MaxCartQuantity {
		return nil, ErrCartQuantity
	}
	result, err := d.conn.Exec(`UPDATE cart_items SET quantity = ?
		WHERE product_id IN (SELECT id FROM products WHERE email = ? AND id = ?)`, quantity, email, id)
	if err != nil {
		return nil, fmt.Errorf("set cart quantity: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrCartItemNotFound
	}
	return d.getCartItem(email, id)
}

func (d *DB) DeleteCartItem(email, id string) error {
	result, err := d.conn.Exec(`DELETE FROM cart_items
		WHERE product_id IN (SELECT id FROM products WHERE email = ? AND id = ?)`, email, id)
	if err != nil {
		return fmt.Errorf("delete cart item: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrCartItemNotFound
	}
	return nil
}
