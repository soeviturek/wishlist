package scheduler

import (
	"math"
	"path/filepath"
	"testing"

	"wishlist-tracker/internal/db"
	"wishlist-tracker/internal/models"
	"wishlist-tracker/internal/notify"
	"wishlist-tracker/internal/stores"
)

type pollerPriceStore struct {
	url   string
	price float64
}

func (s *pollerPriceStore) Match(raw string) bool { return raw == s.url }
func (s *pollerPriceStore) Name() string          { return "Price Fixture" }
func (s *pollerPriceStore) GetProduct(string) (*models.Product, error) {
	return &models.Product{Name: "Whey Protein", Price: s.price}, nil
}

type pollerNotifier struct {
	alerts []notify.PriceDropAlert
}

func (n *pollerNotifier) SendPriceAlert(alert notify.PriceDropAlert) error {
	n.alerts = append(n.alerts, alert)
	return nil
}

func (n *pollerNotifier) SendDigest(_ string, alerts []notify.PriceDropAlert) error {
	n.alerts = append(n.alerts, alerts...)
	return nil
}

func TestPollerPriceAlerts(t *testing.T) {
	target := 90.0
	sentPrice := 89.99
	for _, tc := range []struct {
		name       string
		previous   float64
		current    float64
		target     *float64
		notified   bool
		sentPrice  *float64
		wantAlert  bool
		wantTarget bool
	}{
		{name: "same-day-drop", previous: 109.99, current: 89.99, wantAlert: true},
		{name: "one-cent-drop", previous: 90, current: 89.99, wantAlert: true},
		{name: "unchanged", previous: 89.99, current: 89.99},
		{name: "subcent-drop", previous: math.Nextafter(89.99, math.Inf(1)), current: 89.99},
		{name: "target-without-drop", previous: 89.99, current: 89.99, target: &target, wantAlert: true, wantTarget: true},
		{name: "already-sent-at-same-cent", previous: 89.99, current: math.Nextafter(89.99, math.Inf(1)), target: &target, sentPrice: &sentPrice},
		{name: "subcent-rise-stays-muted", previous: 89.99, current: math.Nextafter(89.99, math.Inf(1)), notified: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := db.New(filepath.Join(t.TempDir(), "poller.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			store := &pollerPriceStore{url: "https://poller-price.test/" + t.Name(), price: tc.current}
			stores.Register(store)
			item, err := database.CreateItem("shopper@example.test", store.url, store.Name(), "Whey Protein", "", tc.target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.RecordPrice(item.ID, tc.previous); err != nil {
				t.Fatal(err)
			}
			if err := database.SetNotified(item.ID, tc.notified); err != nil {
				t.Fatal(err)
			}
			if tc.sentPrice != nil {
				if err := database.RecordNotification(item.ID, *tc.sentPrice); err != nil {
					t.Fatal(err)
				}
			}
			notifier := &pollerNotifier{}
			NewPoller(database, notifier).RunNow()
			if tc.wantAlert {
				if len(notifier.alerts) != 1 {
					t.Fatalf("expected one alert, got %d", len(notifier.alerts))
				}
				alert := notifier.alerts[0]
				if alert.OldPrice != tc.previous || alert.NewPrice != tc.current || alert.IsTarget != tc.wantTarget {
					t.Errorf("incorrect alert: old=%.17g new=%.17g target=%v; want old=%.17g new=%.17g target=%v", alert.OldPrice, alert.NewPrice, alert.IsTarget, tc.previous, tc.current, tc.wantTarget)
				}
			} else if len(notifier.alerts) != 0 {
				t.Errorf("unexpected same-price or duplicate alerts: %d", len(notifier.alerts))
			}
			saved, err := database.GetItemByID(item.ID)
			if err != nil || saved == nil {
				t.Fatalf("read checked item: %+v, %v", saved, err)
			}
			if saved.Notified != (tc.notified || tc.wantAlert) {
				t.Errorf("unexpected notification state after checking price: %+v", saved)
			}
		})
	}
}
