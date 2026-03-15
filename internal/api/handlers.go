package api

import (
	"log"
	"net/http"

	"wishlist-tracker/internal/chart"
	"wishlist-tracker/internal/db"
	"wishlist-tracker/internal/models"
	"wishlist-tracker/internal/notify"
	"wishlist-tracker/internal/stores"

	"github.com/gin-gonic/gin"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	DB       *db.DB
	Notifier notify.Notifier
}

// NewHandler creates a new API handler.
func NewHandler(database *db.DB, notifier notify.Notifier) *Handler {
	return &Handler{DB: database, Notifier: notifier}
}

// RegisterRoutes sets up all API routes.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.POST("/items", h.RegisterItem)
	r.GET("/items", h.ListItems)
	r.GET("/items/:id/history", h.GetPriceHistory)
	r.GET("/items/:id/chart.png", h.GetPriceChart)
	r.POST("/items/:id/check", h.ManualCheck)
	r.PATCH("/items/:id/notify", h.ToggleNotify)
	r.DELETE("/items/:id", h.DeleteItem)
	r.GET("/health", h.HealthCheck)
}

// HealthCheck returns service status.
func (h *Handler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// RegisterItem handles POST /items — registers a new product to track.
func (h *Handler) RegisterItem(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Detect the store
	store, err := stores.Detect(req.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Scrape product info
	product, err := store.GetProduct(req.URL)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "failed to scrape product",
			"details": err.Error(),
		})
		return
	}

	// Store the item
	item, err := h.DB.CreateItem(req.Email, req.URL, store.Name(), product.Name, product.ImageURL, req.TargetPrice)
	if err != nil {
		log.Printf("[api] create item error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save item"})
		return
	}

	// Record the initial price
	if _, err := h.DB.RecordPrice(item.ID, product.Price); err != nil {
		log.Printf("[api] record initial price error: %v", err)
	}

	c.JSON(http.StatusCreated, models.RegisterResponse{
		ItemID:       item.ID,
		Name:         product.Name,
		CurrentPrice: product.Price,
		ImageURL:     product.ImageURL,
	})
}

// ListItems handles GET /items?email=...
func (h *Handler) ListItems(c *gin.Context) {
	email := c.Query("email")
	if email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email query parameter is required"})
		return
	}

	items, err := h.DB.ListItemsByEmail(email)
	if err != nil {
		log.Printf("[api] list items error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list items"})
		return
	}

	// Enrich each item with its latest price
	type ItemWithPrice struct {
		models.Item
		CurrentPrice *float64 `json:"current_price,omitempty"`
	}

	results := make([]ItemWithPrice, 0, len(items))
	for _, item := range items {
		iwp := ItemWithPrice{Item: item}
		if p, err := h.DB.GetLatestPrice(item.ID); err == nil && p != nil {
			iwp.CurrentPrice = &p.Price
		}
		results = append(results, iwp)
	}

	c.JSON(http.StatusOK, results)
}

// GetPriceHistory handles GET /items/:id/history
func (h *Handler) GetPriceHistory(c *gin.Context) {
	id := c.Param("id")

	item, err := h.DB.GetItemByID(id)
	if err != nil {
		log.Printf("[api] get item error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}

	history, err := h.DB.GetPriceHistory(id)
	if err != nil {
		log.Printf("[api] get price history error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get price history"})
		return
	}

	if history == nil {
		history = []models.PriceHistory{}
	}

	c.JSON(http.StatusOK, history)
}

// GetPriceChart handles GET /items/:id/chart.png — returns a PNG price chart.
func (h *Handler) GetPriceChart(c *gin.Context) {
	id := c.Param("id")

	item, err := h.DB.GetItemByID(id)
	if err != nil {
		log.Printf("[api] get item error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}

	history, err := h.DB.GetPriceHistory(id)
	if err != nil {
		log.Printf("[api] get price history error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get price history"})
		return
	}

	png, err := chart.Render(history, item.Name)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "image/png", png)
}

// ManualCheck handles POST /items/:id/check — manually triggers a price check.
func (h *Handler) ManualCheck(c *gin.Context) {
	id := c.Param("id")

	item, err := h.DB.GetItemByID(id)
	if err != nil {
		log.Printf("[api] get item error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}

	// Detect store and scrape
	store, err := stores.Detect(item.URL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "store detection failed"})
		return
	}

	product, err := store.GetProduct(item.URL)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   "failed to scrape product",
			"details": err.Error(),
		})
		return
	}

	// Get previous price
	prevPrice, _ := h.DB.GetLatestPrice(item.ID)

	// Record new price
	if _, err := h.DB.RecordPrice(item.ID, product.Price); err != nil {
		log.Printf("[api] record price error: %v", err)
	}

	// Check for price drop and notify
	result := gin.H{
		"item_id":       item.ID,
		"name":          item.Name,
		"current_price": product.Price,
		"notified":      false,
	}

	if prevPrice != nil {
		result["previous_price"] = prevPrice.Price

		shouldNotify := false
		isTarget := false

		// Price drop
		if product.Price < prevPrice.Price {
			shouldNotify = true
		}

		// Target price reached
		if item.TargetPrice != nil && product.Price <= *item.TargetPrice {
			shouldNotify = true
			isTarget = true
		}

		if shouldNotify {
			// Check for duplicate notification
			alreadySent, _ := h.DB.HasNotificationForPrice(item.ID, product.Price)
			if !alreadySent {
				// Fetch price history and generate chart for the email
				history, _ := h.DB.GetPriceHistory(item.ID)
				chartPNG, chartErr := chart.Render(history, item.Name)
				if chartErr != nil {
					log.Printf("[api] chart generation failed: %v", chartErr)
				}

				err := h.Notifier.SendPriceAlert(notify.PriceDropAlert{
					To:           item.Email,
					ProductName:  item.Name,
					ProductURL:   item.URL,
					ImageURL:     product.ImageURL,
					OldPrice:     prevPrice.Price,
					NewPrice:     product.Price,
					IsTarget:     isTarget,
					PriceHistory: history,
					ChartPNG:     chartPNG,
				})
				if err != nil {
					log.Printf("[api] send email error: %v", err)
				} else {
					_ = h.DB.RecordNotification(item.ID, product.Price)
					result["notified"] = true
				}
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// ToggleNotify handles PATCH /items/:id/notify — toggles the notified (muted) flag.
// When notified=true the poller skips the item. The user re-enables it to get alerts again.
func (h *Handler) ToggleNotify(c *gin.Context) {
	id := c.Param("id")

	item, err := h.DB.GetItemByID(id)
	if err != nil {
		log.Printf("[api] get item error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}

	newState := !item.Notified
	if err := h.DB.SetNotified(id, newState); err != nil {
		log.Printf("[api] set notified error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update"})
		return
	}

	status := "active"
	if newState {
		status = "muted"
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "notified": newState, "status": status})
}

// DeleteItem handles DELETE /items/:id
func (h *Handler) DeleteItem(c *gin.Context) {
	id := c.Param("id")

	item, err := h.DB.GetItemByID(id)
	if err != nil {
		log.Printf("[api] get item error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "item not found"})
		return
	}

	if err := h.DB.DeleteItem(id); err != nil {
		log.Printf("[api] delete item error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete item"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "item deleted"})
}
