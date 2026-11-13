package api

import (
	"encoding/json"
	"math"
	"net/http"
	"path/filepath"
	"testing"

	"wishlist-tracker/internal/db"
	"wishlist-tracker/internal/models"
	"wishlist-tracker/internal/notify"
	"wishlist-tracker/internal/stores"

	"github.com/gin-gonic/gin"
)

type checkPriceStore struct {
	url          string
	price        float64
	beforeReturn func()
}

func (s *checkPriceStore) Match(raw string) bool { return raw == s.url }
func (s *checkPriceStore) Name() string          { return "Price Fixture" }
func (s *checkPriceStore) GetProduct(string) (*models.Product, error) {
	if s.beforeReturn != nil {
		s.beforeReturn()
	}
	return &models.Product{Name: "Whey Protein", Price: s.price}, nil
}

type checkNotifier struct {
	alerts []notify.PriceDropAlert
}

func (n *checkNotifier) SendPriceAlert(alert notify.PriceDropAlert) error {
	n.alerts = append(n.alerts, alert)
	return nil
}

func (n *checkNotifier) SendDigest(_ string, alerts []notify.PriceDropAlert) error {
	n.alerts = append(n.alerts, alerts...)
	return nil
}

func TestManualCheckPriceAlerts(t *testing.T) {
	target := 90.0
	sentPrice := 89.99
	for _, tc := range []struct {
		name       string
		previous   float64
		current    float64
		target     *float64
		sentPrice  *float64
		wantAlert  bool
		wantTarget bool
		closeDB    bool
		wantStatus int
	}{
		{name: "same-day-drop", previous: 109.99, current: 89.99, wantAlert: true},
		{name: "one-cent-drop", previous: 90, current: 89.99, wantAlert: true},
		{name: "unchanged", previous: 89.99, current: 89.99},
		{name: "subcent-drop", previous: math.Nextafter(89.99, math.Inf(1)), current: 89.99},
		{name: "target-without-drop", previous: 89.99, current: 89.99, target: &target, wantAlert: true, wantTarget: true},
		{name: "already-sent-at-same-cent", previous: 89.99, current: math.Nextafter(89.99, math.Inf(1)), target: &target, sentPrice: &sentPrice},
		{name: "unreadable-previous-price", previous: 109.99, current: 89.99, closeDB: true, wantStatus: http.StatusInternalServerError},
		{name: "cannot-record-price", previous: 109.99, current: math.NaN(), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database, err := db.New(filepath.Join(t.TempDir(), "check.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			store := &checkPriceStore{url: "https://manual-price.test/" + t.Name(), price: tc.current}
			stores.Register(store)
			item, err := database.CreateItem("shopper@example.test", store.url, store.Name(), "Whey Protein", "", tc.target)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := database.RecordPrice(item.ID, tc.previous); err != nil {
				t.Fatal(err)
			}
			if tc.sentPrice != nil {
				if err := database.RecordNotification(item.ID, *tc.sentPrice); err != nil {
					t.Fatal(err)
				}
			}
			notifier := &checkNotifier{}
			gin.SetMode(gin.TestMode)
			router := gin.New()
			NewHandler(database, notifier).RegisterRoutes(router)
			if tc.closeDB {
				store.beforeReturn = func() {
					if err := database.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			response := cartRequest(t, router, http.MethodPost, "/items/"+item.ID+"/check", nil)
			if tc.wantStatus != 0 {
				requireCartStatus(t, response, tc.wantStatus)
				if len(notifier.alerts) != 0 {
					t.Error("failed price lookup or recording must not send an alert")
				}
				return
			}
			requireCartStatus(t, response, http.StatusOK)
			var result struct {
				Notified bool `json:"notified"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Notified != tc.wantAlert {
				t.Errorf("notified=%v, want %v", result.Notified, tc.wantAlert)
			}
			if tc.wantAlert {
				if len(notifier.alerts) != 1 {
					t.Fatalf("expected one alert, got %d", len(notifier.alerts))
				}
				alert := notifier.alerts[0]
				if alert.OldPrice != tc.previous || alert.NewPrice != tc.current || alert.IsTarget != tc.wantTarget {
					t.Errorf("incorrect alert: old=%.17g new=%.17g target=%v", alert.OldPrice, alert.NewPrice, alert.IsTarget)
				}
			} else if len(notifier.alerts) != 0 {
				t.Errorf("unexpected same-price or duplicate alerts: %d", len(notifier.alerts))
			}
		})
	}
}
