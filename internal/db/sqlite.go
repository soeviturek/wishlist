package db

import (
	"database/sql"
	"fmt"
	"time"

	"wishlist-tracker/internal/models"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO/GCC needed)
)

// sqliteTimeFmt is the format used to store timestamps in SQLite.
// modernc.org/sqlite stores Go time.Time in a format that SQLite's
// DATE()/TIME() functions cannot parse. We use ISO 8601 strings instead.
const sqliteTimeFmt = "2006-01-02 15:04:05"

// fmtTime formats a time.Time for SQLite storage.
func fmtTime(t time.Time) string { return t.UTC().Format(sqliteTimeFmt) }

// parseTime parses a SQLite timestamp string back to time.Time.
func parseTime(s string) time.Time {
	// Try our standard format first
	if t, err := time.Parse(sqliteTimeFmt, s); err == nil {
		return t
	}
	// Try RFC3339 (how Go's time.Time marshals by default)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try with fractional seconds
	if t, err := time.Parse("2006-01-02 15:04:05.999999999", s); err == nil {
		return t
	}
	// Try SQLite's CURRENT_TIMESTAMP format
	if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
		return t
	}
	return time.Time{}
}

// DB wraps the SQLite database connection.
type DB struct {
	conn *sql.DB
}

// New opens the SQLite database and creates tables if needed.
func New(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS items (
		id          TEXT PRIMARY KEY,
		email       TEXT NOT NULL,
		url         TEXT NOT NULL,
		store       TEXT NOT NULL,
		name        TEXT NOT NULL,
		target_price REAL,
		created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS prices (
		id          TEXT PRIMARY KEY,
		item_id     TEXT NOT NULL,
		price       REAL NOT NULL,
		recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS notifications (
		id      TEXT PRIMARY KEY,
		item_id TEXT NOT NULL,
		price   REAL NOT NULL,
		sent_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_items_email ON items(email);
	CREATE INDEX IF NOT EXISTS idx_prices_item_id ON prices(item_id);
	CREATE INDEX IF NOT EXISTS idx_notifications_item_id ON notifications(item_id);
	`
	_, err := d.conn.Exec(schema)
	if err != nil {
		return err
	}

	// Add notified column if it doesn't exist (migration for existing DBs).
	d.conn.Exec(`ALTER TABLE items ADD COLUMN notified INTEGER NOT NULL DEFAULT 0`)
	d.conn.Exec(`ALTER TABLE items ADD COLUMN image_url TEXT NOT NULL DEFAULT ''`)

	return nil
}

// ---------------------------------------------------------------------------
// Items
// ---------------------------------------------------------------------------

// CreateItem inserts a new tracked item and returns it.
func (d *DB) CreateItem(email, url, store, name, imageURL string, targetPrice *float64) (*models.Item, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	_, err := d.conn.Exec(
		`INSERT INTO items (id, email, url, store, name, image_url, target_price, notified, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		id, email, url, store, name, imageURL, targetPrice, fmtTime(now),
	)
	if err != nil {
		return nil, fmt.Errorf("insert item: %w", err)
	}

	return &models.Item{
		ID:          id,
		Email:       email,
		URL:         url,
		Store:       store,
		Name:        name,
		ImageURL:    imageURL,
		TargetPrice: targetPrice,
		CreatedAt:   now,
	}, nil
}

// GetItemByID retrieves a single item by ID.
func (d *DB) GetItemByID(id string) (*models.Item, error) {
	row := d.conn.QueryRow(
		`SELECT id, email, url, store, name, image_url, target_price, notified, created_at FROM items WHERE id = ?`, id,
	)

	var item models.Item
	var createdAt string
	err := row.Scan(&item.ID, &item.Email, &item.URL, &item.Store, &item.Name, &item.ImageURL, &item.TargetPrice, &item.Notified, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	item.CreatedAt = parseTime(createdAt)
	return &item, nil
}

// ListItemsByEmail returns all items for a given email.
func (d *DB) ListItemsByEmail(email string) ([]models.Item, error) {
	rows, err := d.conn.Query(
		`SELECT id, email, url, store, name, image_url, target_price, notified, created_at FROM items WHERE email = ? ORDER BY created_at DESC`, email,
	)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	var items []models.Item
	for rows.Next() {
		var item models.Item
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Email, &item.URL, &item.Store, &item.Name, &item.ImageURL, &item.TargetPrice, &item.Notified, &createdAt); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetAllItems returns every tracked item (used by the poller).
func (d *DB) GetAllItems() ([]models.Item, error) {
	rows, err := d.conn.Query(
		`SELECT id, email, url, store, name, image_url, target_price, notified, created_at FROM items WHERE notified = 0 ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("get all items: %w", err)
	}
	defer rows.Close()

	var items []models.Item
	for rows.Next() {
		var item models.Item
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Email, &item.URL, &item.Store, &item.Name, &item.ImageURL, &item.TargetPrice, &item.Notified, &createdAt); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

// DeleteItem removes an item and its associated prices/notifications (cascade).
func (d *DB) DeleteItem(id string) error {
	_, err := d.conn.Exec(`DELETE FROM items WHERE id = ?`, id)
	return err
}

// UpdateTargetPrice updates the target price for an item. Pass nil to remove the target.
func (d *DB) UpdateTargetPrice(id string, targetPrice *float64) error {
	_, err := d.conn.Exec(`UPDATE items SET target_price = ? WHERE id = ?`, targetPrice, id)
	return err
}

// SetNotified sets the notified flag on an item (true = muted, false = active).
func (d *DB) SetNotified(id string, notified bool) error {
	val := 0
	if notified {
		val = 1
	}
	_, err := d.conn.Exec(`UPDATE items SET notified = ? WHERE id = ?`, val, id)
	return err
}

// ---------------------------------------------------------------------------
// Prices
// ---------------------------------------------------------------------------

// RecordPrice records a price snapshot for an item.
// Only one price per item per day is kept — if a record already exists
// for today it is updated with the latest price.
func (d *DB) RecordPrice(itemID string, price float64) (*models.Price, error) {
	now := time.Now().UTC()
	nowStr := fmtTime(now)
	today := now.Format("2006-01-02")

	// Check if we already have a price for this item today.
	var existingID string
	err := d.conn.QueryRow(
		`SELECT id FROM prices WHERE item_id = ? AND SUBSTR(recorded_at, 1, 10) = ?`,
		itemID, today,
	).Scan(&existingID)

	if err == nil {
		// Update existing record for today
		_, err = d.conn.Exec(
			`UPDATE prices SET price = ?, recorded_at = ? WHERE id = ?`,
			price, nowStr, existingID,
		)
		if err != nil {
			return nil, fmt.Errorf("update price: %w", err)
		}
		return &models.Price{
			ID:         existingID,
			ItemID:     itemID,
			Price:      price,
			RecordedAt: now,
		}, nil
	}

	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("check existing price: %w", err)
	}

	// Insert new record
	id := uuid.New().String()
	_, err = d.conn.Exec(
		`INSERT INTO prices (id, item_id, price, recorded_at) VALUES (?, ?, ?, ?)`,
		id, itemID, price, nowStr,
	)
	if err != nil {
		return nil, fmt.Errorf("insert price: %w", err)
	}

	return &models.Price{
		ID:         id,
		ItemID:     itemID,
		Price:      price,
		RecordedAt: now,
	}, nil
}

// GetLatestPrice returns the most recent price for an item.
func (d *DB) GetLatestPrice(itemID string) (*models.Price, error) {
	row := d.conn.QueryRow(
		`SELECT id, item_id, price, recorded_at FROM prices WHERE item_id = ? ORDER BY recorded_at DESC LIMIT 1`,
		itemID,
	)

	var p models.Price
	var recordedAt string
	err := row.Scan(&p.ID, &p.ItemID, &p.Price, &recordedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest price: %w", err)
	}
	p.RecordedAt = parseTime(recordedAt)
	return &p, nil
}

// GetPriceHistory returns all recorded prices for an item (one per day, oldest first).
func (d *DB) GetPriceHistory(itemID string) ([]models.PriceHistory, error) {
	rows, err := d.conn.Query(
		`SELECT price, SUBSTR(recorded_at, 1, 10) AS day
		 FROM prices WHERE item_id = ?
		 GROUP BY day ORDER BY day ASC`,
		itemID,
	)
	if err != nil {
		return nil, fmt.Errorf("get price history: %w", err)
	}
	defer rows.Close()

	var history []models.PriceHistory
	for rows.Next() {
		var price float64
		var day string
		if err := rows.Scan(&price, &day); err != nil {
			return nil, fmt.Errorf("scan price: %w", err)
		}
		history = append(history, models.PriceHistory{
			Date:  day,
			Price: price,
		})
	}
	return history, rows.Err()
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

// RecordNotification logs that a notification was sent.
func (d *DB) RecordNotification(itemID string, price float64) error {
	id := uuid.New().String()
	_, err := d.conn.Exec(
		`INSERT INTO notifications (id, item_id, price, sent_at) VALUES (?, ?, ?, ?)`,
		id, itemID, price, fmtTime(time.Now()),
	)
	return err
}

// HasNotificationForPrice checks if a notification was already sent for this price.
func (d *DB) HasNotificationForPrice(itemID string, price float64) (bool, error) {
	row := d.conn.QueryRow(
		`SELECT COUNT(*) FROM notifications WHERE item_id = ? AND price = ?`,
		itemID, price,
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
