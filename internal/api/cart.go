package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"wishlist-tracker/internal/cart"
	"wishlist-tracker/internal/db"
	"wishlist-tracker/internal/models"
	"wishlist-tracker/internal/stores"

	"github.com/gin-gonic/gin"
)

func cartEmail(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "no-store")
	var query struct {
		Email string `form:"email" binding:"required,email"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid email query parameter is required"})
		return "", false
	}
	return query.Email, true
}

func cartError(c *gin.Context, err error, message string) {
	switch {
	case errors.Is(err, db.ErrCartItemNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "cart item not found"})
	case errors.Is(err, db.ErrCartQuantity):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		log.Printf("[api] %s: %v", message, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": message})
	}
}

func (h *Handler) SearchProducts(c *gin.Context) {
	email, ok := cartEmail(c)
	if !ok {
		return
	}
	query := c.Query("q")
	if utf8.RuneCountInString(query) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "search must be 200 characters or fewer"})
		return
	}
	products, err := h.DB.SearchProducts(email, query)
	if err != nil {
		cartError(c, err, "failed to search saved products")
		return
	}
	c.JSON(http.StatusOK, products)
}

func (h *Handler) ListCart(c *gin.Context) {
	email, ok := cartEmail(c)
	if !ok {
		return
	}
	items, err := h.DB.ListCartItems(email)
	if err != nil {
		cartError(c, err, "failed to load shopping cart")
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) AddCartItem(c *gin.Context) {
	var req models.AddCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	sources := 0
	for _, source := range []string{req.URL, req.ItemID, req.ProductID} {
		if source != "" {
			sources++
		}
	}
	if sources != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide exactly one of url, item_id, or product_id"})
		return
	}
	quantity := 1
	if req.Quantity != nil {
		quantity = *req.Quantity
	}

	var product *models.SavedProduct
	var err error
	switch {
	case req.ItemID != "":
		item, getErr := h.DB.GetItemByID(req.ItemID)
		if getErr != nil {
			cartError(c, getErr, "failed to load wishlist item")
			return
		}
		if item == nil || item.Email != req.Email {
			c.JSON(http.StatusNotFound, gin.H{"error": "wishlist item not found"})
			return
		}
		product, err = h.DB.GetSavedProductByURL(req.Email, item.URL)
	case req.ProductID != "":
		product, err = h.DB.GetSavedProduct(req.Email, req.ProductID)
	default:
		store, detectErr := stores.Detect(req.URL)
		if detectErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": detectErr.Error()})
			return
		}
		scraped, scrapeErr := store.GetProduct(req.URL)
		if scrapeErr != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "failed to extract product", "details": scrapeErr.Error()})
			return
		}
		product, err = h.DB.SaveProduct(req.Email, req.URL, store.Name(), scraped.Name, scraped.ImageURL, &scraped.Price)
	}
	if err != nil {
		cartError(c, err, "failed to save product")
		return
	}
	if product == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "saved product not found"})
		return
	}
	item, err := h.DB.AddCartItem(req.Email, product.ID, quantity)
	if err != nil {
		cartError(c, err, "failed to add product to shopping cart")
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateCartQuantity(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required,email"`
		Quantity int    `json:"quantity" binding:"required,min=1,max=999"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.DB.SetCartQuantity(req.Email, c.Param("id"), req.Quantity)
	if err != nil {
		cartError(c, err, "failed to update shopping cart")
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) DeleteCartItem(c *gin.Context) {
	email, ok := cartEmail(c)
	if !ok {
		return
	}
	if err := h.DB.DeleteCartItem(email, c.Param("id")); err != nil {
		cartError(c, err, "failed to remove product from shopping cart")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) exportItems(c *gin.Context) ([]models.CartItem, bool) {
	email, ok := cartEmail(c)
	if !ok {
		return nil, false
	}
	items, err := h.DB.ListCartItems(email)
	if err != nil {
		cartError(c, err, "failed to load shopping cart")
		return nil, false
	}
	if len(items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "add products to your cart before exporting"})
		return nil, false
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	return items, true
}

func (h *Handler) ExportCartImage(c *gin.Context) {
	items, ok := h.exportItems(c)
	if !ok {
		return
	}
	image, missingPhotos, err := cart.RenderPNG(c.Request.Context(), items, time.Now().UTC())
	if errors.Is(err, cart.ErrImageTooLarge) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		cartError(c, err, "failed to generate shopping list image")
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="shopping-list-%s.png"`, time.Now().UTC().Format("2006-01-02")))
	c.Header("X-Cart-Missing-Photos", strconv.Itoa(missingPhotos))
	c.Data(http.StatusOK, "image/png", image)
}

func (h *Handler) ExportCartText(c *gin.Context) {
	items, ok := h.exportItems(c)
	if !ok {
		return
	}
	now := time.Now().UTC()
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="shopping-list-%s.txt"`, now.Format("2006-01-02")))
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(cart.RenderText(items, now)))
}
