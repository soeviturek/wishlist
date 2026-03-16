package scheduler

import (
	"log"
	"time"

	"wishlist-tracker/internal/chart"
	"wishlist-tracker/internal/db"
	"wishlist-tracker/internal/notify"
	"wishlist-tracker/internal/stores"

	"github.com/go-co-op/gocron"
)

// Poller periodically checks all tracked items for price changes.
type Poller struct {
	db        *db.DB
	notifier  notify.Notifier
	scheduler *gocron.Scheduler
}

// NewPoller creates a new price-checking scheduler.
func NewPoller(database *db.DB, notifier notify.Notifier) *Poller {
	s := gocron.NewScheduler(time.UTC)
	return &Poller{
		db:        database,
		notifier:  notifier,
		scheduler: s,
	}
}

// Start begins the polling schedule.
func (p *Poller) Start(cronExpr string) error {
	_, err := p.scheduler.Cron(cronExpr).Do(p.pollAll)
	if err != nil {
		return err
	}

	p.scheduler.StartAsync()
	log.Printf("[scheduler] Started polling with cron: %s", cronExpr)
	return nil
}

// Stop gracefully shuts down the scheduler.
func (p *Poller) Stop() {
	p.scheduler.Stop()
	log.Println("[scheduler] Stopped")
}

// RunNow triggers an immediate poll of all items (useful for testing).
func (p *Poller) RunNow() {
	p.pollAll()
}

func (p *Poller) pollAll() {
	log.Println("[scheduler] Starting price check for all items...")

	items, err := p.db.GetAllItems()
	if err != nil {
		log.Printf("[scheduler] Error fetching items: %v", err)
		return
	}

	log.Printf("[scheduler] Checking %d items", len(items))

	checked, drops, errors := 0, 0, 0

	// Collect alerts per email so we send ONE digest email per user.
	type pendingAlert struct {
		alert  notify.PriceDropAlert
		itemID string
		price  float64
	}
	alertsByEmail := make(map[string][]pendingAlert)

	for _, item := range items {
		checked++

		store, err := stores.Detect(item.URL)
		if err != nil {
			log.Printf("[scheduler] Store detection failed for %s: %v", item.URL, err)
			errors++
			continue
		}

		product, err := store.GetProduct(item.URL)
		if err != nil {
			log.Printf("[scheduler] Scrape failed for %s (%s): %v", item.Name, item.URL, err)
			errors++
			continue
		}

		// Get previous price before recording new one
		prevPrice, err := p.db.GetLatestPrice(item.ID)
		if err != nil {
			log.Printf("[scheduler] Error getting latest price for %s: %v", item.ID, err)
		}

		// Record new price
		if _, err := p.db.RecordPrice(item.ID, product.Price); err != nil {
			log.Printf("[scheduler] Error recording price for %s: %v", item.ID, err)
			errors++
			continue
		}

		// Compare and decide if notification is needed
		if prevPrice == nil {
			continue // first price, nothing to compare
		}

		shouldNotify := false
		isTarget := false

		if product.Price < prevPrice.Price {
			shouldNotify = true
			drops++
		}

		if item.TargetPrice != nil && product.Price <= *item.TargetPrice {
			shouldNotify = true
			isTarget = true
		}

		if !shouldNotify {
			continue
		}

		// Spam prevention
		alreadySent, err := p.db.HasNotificationForPrice(item.ID, product.Price)
		if err != nil {
			log.Printf("[scheduler] Error checking notification for %s: %v", item.ID, err)
			continue
		}
		if alreadySent {
			log.Printf("[scheduler] Already notified for %s at $%.2f — skipping", item.Name, product.Price)
			continue
		}

		// Fetch price history and generate chart
		history, _ := p.db.GetPriceHistory(item.ID)
		chartPNG, chartErr := chart.Render(history, item.Name)
		if chartErr != nil {
			log.Printf("[scheduler] Chart generation failed for %s: %v", item.Name, chartErr)
		}

		// Use the original (first) price for the email, not yesterday's price
		originalPrice := prevPrice.Price
		if first, err := p.db.GetFirstPrice(item.ID); err == nil && first != nil {
			originalPrice = first.Price
		}

		alertsByEmail[item.Email] = append(alertsByEmail[item.Email], pendingAlert{
			alert: notify.PriceDropAlert{
				ProductName:  item.Name,
				ProductURL:   item.URL,
				ImageURL:     product.ImageURL,
				OldPrice:     originalPrice,
				NewPrice:     product.Price,
				IsTarget:     isTarget,
				PriceHistory: history,
				ChartPNG:     chartPNG,
			},
			itemID: item.ID,
			price:  product.Price,
		})
	}

	// Send one digest email per user
	for email, pending := range alertsByEmail {
		alerts := make([]notify.PriceDropAlert, len(pending))
		for i, p := range pending {
			alerts[i] = p.alert
		}

		err := p.notifier.SendDigest(email, alerts)
		if err != nil {
			log.Printf("[scheduler] Digest email error for %s: %v", email, err)
			errors++
			continue
		}

		// Record all notifications and mute items to prevent duplicates
		for _, pa := range pending {
			if err := p.db.RecordNotification(pa.itemID, pa.price); err != nil {
				log.Printf("[scheduler] Error recording notification for %s: %v", pa.itemID, err)
			}
			if err := p.db.SetNotified(pa.itemID, true); err != nil {
				log.Printf("[scheduler] Error setting notified for %s: %v", pa.itemID, err)
			}
		}
	}

	log.Printf("[scheduler] Done. Checked: %d, Drops: %d, Errors: %d", checked, drops, errors)
}
