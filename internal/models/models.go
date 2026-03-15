package models

import "time"

// Item represents a tracked product.
type Item struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	URL         string    `json:"url"`
	Store       string    `json:"store"`
	Name        string    `json:"name"`
	ImageURL    string    `json:"image_url,omitempty"`
	TargetPrice *float64  `json:"target_price,omitempty"`
	Notified    bool      `json:"notified"`
	CreatedAt   time.Time `json:"created_at"`
}

// Price represents a single price snapshot for an item.
type Price struct {
	ID         string    `json:"id"`
	ItemID     string    `json:"item_id"`
	Price      float64   `json:"price"`
	RecordedAt time.Time `json:"recorded_at"`
}

// PriceHistory is the API-friendly version returned by the history endpoint.
type PriceHistory struct {
	Date  string  `json:"date"`
	Price float64 `json:"price"`
}

// Notification records an alert that was sent.
type Notification struct {
	ID     string    `json:"id"`
	ItemID string    `json:"item_id"`
	Price  float64   `json:"price"`
	SentAt time.Time `json:"sent_at"`
}

// RegisterRequest is the body for POST /items.
type RegisterRequest struct {
	Email       string   `json:"email" binding:"required,email"`
	URL         string   `json:"url" binding:"required,url"`
	TargetPrice *float64 `json:"target_price,omitempty"`
}

// RegisterResponse is the response for POST /items.
type RegisterResponse struct {
	ItemID       string  `json:"item_id"`
	Name         string  `json:"name"`
	CurrentPrice float64 `json:"current_price"`
	ImageURL     string  `json:"image_url,omitempty"`
}

// Product is what a store scraper returns.
type Product struct {
	Name     string
	Price    float64
	ImageURL string
}
