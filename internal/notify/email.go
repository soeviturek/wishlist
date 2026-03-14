package notify

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
	"strings"

	"wishlist-tracker/internal/config"
	"wishlist-tracker/internal/models"
)

// Notifier is the interface for sending price-change alerts.
// Emailer is the default implementation; others (e.g. Slack, webhook)
// can be added by implementing this interface.
type Notifier interface {
	SendPriceAlert(alert PriceDropAlert) error
	SendDigest(to string, alerts []PriceDropAlert) error
}

// Emailer sends email notifications via SMTP.
type Emailer struct {
	cfg config.SMTPConfig
}

// compile-time check: *Emailer satisfies Notifier.
var _ Notifier = (*Emailer)(nil)

// NewEmailer creates a new email sender.
func NewEmailer(cfg config.SMTPConfig) *Emailer {
	return &Emailer{cfg: cfg}
}

// PriceDropAlert holds data for a price-drop email.
type PriceDropAlert struct {
	To           string
	ProductName  string
	ProductURL   string
	ImageURL     string
	OldPrice     float64
	NewPrice     float64
	IsTarget     bool // true if the alert is because target price was reached
	PriceHistory []models.PriceHistory
	ChartPNG     []byte // optional PNG chart of price history
}

// SendPriceAlert sends a price-drop or target-price email.
func (e *Emailer) SendPriceAlert(alert PriceDropAlert) error {
	if e.cfg.Host == "" || e.cfg.Username == "" {
		log.Printf("[email] SMTP not configured — skipping email to %s for %s", alert.To, alert.ProductName)
		return nil
	}

	subject := "📉 Price Drop Alert"
	if alert.IsTarget {
		subject = "🎯 Target Price Reached!"
	}

	msg, err := buildMIMEMessage(e.cfg.From, alert, subject)
	if err != nil {
		return fmt.Errorf("build email: %w", err)
	}

	auth := smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host)
	addr := fmt.Sprintf("%s:%d", e.cfg.Host, e.cfg.Port)

	if err := smtp.SendMail(addr, auth, e.cfg.From, []string{alert.To}, msg); err != nil {
		return fmt.Errorf("send email to %s: %w", alert.To, err)
	}

	log.Printf("[email] Sent price alert to %s for %s ($%.2f -> $%.2f)", alert.To, alert.ProductName, alert.OldPrice, alert.NewPrice)
	return nil
}

// SendDigest sends a single consolidated email containing multiple price alerts.
// This is used by the poller so a user gets ONE email per poll cycle, not N.
func (e *Emailer) SendDigest(to string, alerts []PriceDropAlert) error {
	if len(alerts) == 0 {
		return nil
	}
	// If only one alert, just send a normal single-item email.
	if len(alerts) == 1 {
		alerts[0].To = to
		return e.SendPriceAlert(alerts[0])
	}

	if e.cfg.Host == "" || e.cfg.Username == "" {
		log.Printf("[email] SMTP not configured — skipping digest to %s (%d alerts)", to, len(alerts))
		return nil
	}

	subject := fmt.Sprintf("📉 %d Price Alerts for You!", len(alerts))

	htmlBody := buildDigestHTML(alerts, subject)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	boundary := writer.Boundary()

	fmt.Fprintf(&buf, "From: %s\r\n", e.cfg.From)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/related; boundary=\"%s\"\r\n", boundary)
	fmt.Fprintf(&buf, "\r\n")

	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", "text/html; charset=UTF-8")
	htmlHeader.Set("Content-Transfer-Encoding", "7bit")
	htmlPart, _ := writer.CreatePart(htmlHeader)
	htmlPart.Write([]byte(htmlBody))
	writer.Close()

	auth := smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host)
	addr := fmt.Sprintf("%s:%d", e.cfg.Host, e.cfg.Port)

	if err := smtp.SendMail(addr, auth, e.cfg.From, []string{to}, buf.Bytes()); err != nil {
		return fmt.Errorf("send digest to %s: %w", to, err)
	}

	log.Printf("[email] Sent digest to %s with %d alerts", to, len(alerts))
	return nil
}

// buildDigestHTML generates a single HTML email containing multiple product alerts.
func buildDigestHTML(alerts []PriceDropAlert, subject string) string {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head>`)
	sb.WriteString(`<body style="margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f1f5f9;">`)
	sb.WriteString(`<div style="max-width:600px;margin:20px auto;">`)

	// Header
	sb.WriteString(`<div style="background:#2563eb;padding:24px 30px;color:white;border-radius:12px 12px 0 0;">`)
	sb.WriteString(fmt.Sprintf(`<h1 style="margin:0;font-size:22px;">%s</h1>`, subject))
	sb.WriteString(fmt.Sprintf(`<p style="margin:6px 0 0;opacity:.8;font-size:14px;">%d of your tracked products have price changes</p>`, len(alerts)))
	sb.WriteString(`</div>`)

	// Each alert as a card
	for i, a := range alerts {
		savings := a.OldPrice - a.NewPrice
		savingsPct := float64(0)
		if a.OldPrice > 0 {
			savingsPct = (savings / a.OldPrice) * 100
		}

		bgColor := "#ffffff"
		if i%2 == 1 {
			bgColor = "#f8fafc"
		}
		borderTop := ""
		if i > 0 {
			borderTop = "border-top:1px solid #e2e8f0;"
		}

		badge := `<span style="background:#2563eb;color:white;font-size:11px;padding:2px 8px;border-radius:4px;">PRICE DROP</span>`
		if a.IsTarget {
			badge = `<span style="background:#16a34a;color:white;font-size:11px;padding:2px 8px;border-radius:4px;">🎯 TARGET REACHED</span>`
		}

		sb.WriteString(fmt.Sprintf(`<div style="background:%s;padding:20px 30px;%s">`, bgColor, borderTop))

		// Product row: image + info + prices
		sb.WriteString(`<div style="display:flex;align-items:center;gap:14px;">`)
		if a.ImageURL != "" {
			sb.WriteString(fmt.Sprintf(`<img src="%s" alt="" style="width:60px;height:60px;object-fit:contain;border-radius:8px;border:1px solid #e2e8f0;flex-shrink:0;">`, a.ImageURL))
		}
		sb.WriteString(`<div style="flex:1;min-width:0;">`)
		sb.WriteString(fmt.Sprintf(`<div style="margin-bottom:4px;">%s</div>`, badge))
		sb.WriteString(fmt.Sprintf(`<div style="font-weight:600;font-size:15px;color:#1e293b;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">%s</div>`, a.ProductName))
		sb.WriteString(fmt.Sprintf(`<div style="font-size:13px;color:#64748b;margin-top:2px;"><s style="color:#dc2626;">$%.2f</s> → <strong style="color:#16a34a;">$%.2f</strong>`, a.OldPrice, a.NewPrice))
		if savings > 0 {
			sb.WriteString(fmt.Sprintf(` <span style="color:#16a34a;font-size:12px;">(save $%.2f / %.0f%%)</span>`, savings, savingsPct))
		}
		sb.WriteString(`</div>`)
		sb.WriteString(`</div>`) // end info
		sb.WriteString(fmt.Sprintf(`<a href="%s" style="flex-shrink:0;background:#2563eb;color:white;padding:8px 14px;border-radius:6px;text-decoration:none;font-size:12px;font-weight:600;">View →</a>`, a.ProductURL))
		sb.WriteString(`</div>`) // end row
		sb.WriteString(`</div>`) // end card
	}

	// Footer
	sb.WriteString(`<div style="padding:16px 30px;background:#ffffff;border-top:1px solid #e2e8f0;border-radius:0 0 12px 12px;text-align:center;font-size:12px;color:#94a3b8;">Wishlist Price Tracker — You're receiving this because you registered these products for tracking.</div>`)
	sb.WriteString(`</div></body></html>`)

	return sb.String()
}

// buildMIMEMessage constructs a multipart/related email with an HTML body
// and an optional inline chart PNG attachment.
func buildMIMEMessage(from string, alert PriceDropAlert, subject string) ([]byte, error) {
	var buf bytes.Buffer

	writer := multipart.NewWriter(&buf)
	boundary := writer.Boundary()

	// Top-level headers
	buf.Reset() // reset — we'll write headers manually first
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", alert.To)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/related; boundary=\"%s\"\r\n", boundary)
	fmt.Fprintf(&buf, "\r\n")

	// HTML part
	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", "text/html; charset=UTF-8")
	htmlHeader.Set("Content-Transfer-Encoding", "7bit")
	htmlPart, err := writer.CreatePart(htmlHeader)
	if err != nil {
		return nil, err
	}
	htmlBody := buildHTMLBody(alert, subject)
	htmlPart.Write([]byte(htmlBody))

	// Chart image part (inline, referenced by cid:pricechart)
	if len(alert.ChartPNG) > 0 {
		imgHeader := make(textproto.MIMEHeader)
		imgHeader.Set("Content-Type", "image/png")
		imgHeader.Set("Content-Transfer-Encoding", "base64")
		imgHeader.Set("Content-ID", "<pricechart>")
		imgHeader.Set("Content-Disposition", "inline; filename=\"price-chart.png\"")
		imgPart, err := writer.CreatePart(imgHeader)
		if err != nil {
			return nil, err
		}
		encoded := base64.StdEncoding.EncodeToString(alert.ChartPNG)
		// Wrap base64 at 76 chars per line
		for i := 0; i < len(encoded); i += 76 {
			end := i + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			imgPart.Write([]byte(encoded[i:end] + "\r\n"))
		}
	}

	writer.Close()

	return buf.Bytes(), nil
}

// buildHTMLBody generates a styled HTML email with price info, history table,
// and an embedded chart image.
func buildHTMLBody(alert PriceDropAlert, subject string) string {
	savings := alert.OldPrice - alert.NewPrice
	savingsPct := float64(0)
	if alert.OldPrice > 0 {
		savingsPct = (savings / alert.OldPrice) * 100
	}

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html><html><head><meta charset="UTF-8"></head>`)
	sb.WriteString(`<body style="margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#f1f5f9;">`)
	sb.WriteString(`<div style="max-width:600px;margin:20px auto;background:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 2px 8px rgba(0,0,0,0.08);">`)

	// Header banner
	bannerColor := "#2563eb"
	if alert.IsTarget {
		bannerColor = "#16a34a"
	}
	sb.WriteString(fmt.Sprintf(`<div style="background:%s;padding:24px 30px;color:white;">`, bannerColor))
	sb.WriteString(fmt.Sprintf(`<h1 style="margin:0;font-size:22px;">%s</h1>`, subject))
	sb.WriteString(`</div>`)

	// Product info section
	sb.WriteString(`<div style="padding:24px 30px;">`)

	// Product image + name row
	if alert.ImageURL != "" {
		sb.WriteString(`<div style="display:flex;align-items:center;gap:16px;margin-bottom:20px;">`)
		sb.WriteString(fmt.Sprintf(`<img src="%s" alt="" style="width:80px;height:80px;object-fit:contain;border-radius:8px;border:1px solid #e2e8f0;">`, alert.ImageURL))
		sb.WriteString(fmt.Sprintf(`<h2 style="margin:0;font-size:18px;color:#1e293b;">%s</h2>`, alert.ProductName))
		sb.WriteString(`</div>`)
	} else {
		sb.WriteString(fmt.Sprintf(`<h2 style="margin:0 0 20px;font-size:18px;color:#1e293b;">%s</h2>`, alert.ProductName))
	}

	// Price cards
	sb.WriteString(`<div style="display:flex;gap:12px;margin-bottom:24px;">`)
	sb.WriteString(fmt.Sprintf(`<div style="flex:1;background:#fee2e2;border-radius:8px;padding:14px;text-align:center;"><div style="font-size:12px;color:#991b1b;text-transform:uppercase;font-weight:600;">Old Price</div><div style="font-size:26px;font-weight:700;color:#dc2626;text-decoration:line-through;">$%.2f</div></div>`, alert.OldPrice))
	sb.WriteString(fmt.Sprintf(`<div style="flex:1;background:#dcfce7;border-radius:8px;padding:14px;text-align:center;"><div style="font-size:12px;color:#166534;text-transform:uppercase;font-weight:600;">New Price</div><div style="font-size:26px;font-weight:700;color:#16a34a;">$%.2f</div></div>`, alert.NewPrice))
	sb.WriteString(`</div>`)

	// Savings badge
	if savings > 0 {
		sb.WriteString(fmt.Sprintf(`<div style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:8px;padding:12px;text-align:center;margin-bottom:24px;font-size:15px;color:#166534;font-weight:600;">💰 You save $%.2f (%.0f%% off)</div>`, savings, savingsPct))
	}

	// Chart image
	if len(alert.ChartPNG) > 0 {
		sb.WriteString(`<div style="margin-bottom:24px;">`)
		sb.WriteString(`<h3 style="margin:0 0 12px;font-size:14px;color:#64748b;text-transform:uppercase;letter-spacing:0.5px;">Price History</h3>`)
		sb.WriteString(`<img src="cid:pricechart" alt="Price History Chart" style="width:100%;border-radius:8px;border:1px solid #e2e8f0;">`)
		sb.WriteString(`</div>`)
	}

	// Price history table
	if len(alert.PriceHistory) > 0 {
		sb.WriteString(`<h3 style="margin:0 0 12px;font-size:14px;color:#64748b;text-transform:uppercase;letter-spacing:0.5px;">Recent Prices</h3>`)
		sb.WriteString(`<table style="width:100%;border-collapse:collapse;font-size:14px;margin-bottom:24px;">`)
		sb.WriteString(`<tr style="background:#f8fafc;"><th style="text-align:left;padding:8px 12px;border-bottom:2px solid #e2e8f0;color:#475569;">Date</th><th style="text-align:right;padding:8px 12px;border-bottom:2px solid #e2e8f0;color:#475569;">Price</th><th style="text-align:right;padding:8px 12px;border-bottom:2px solid #e2e8f0;color:#475569;">Change</th></tr>`)

		// Show last 14 entries max (most recent first)
		start := 0
		if len(alert.PriceHistory) > 14 {
			start = len(alert.PriceHistory) - 14
		}
		entries := alert.PriceHistory[start:]
		// Reverse to show newest first
		for i := len(entries) - 1; i >= 0; i-- {
			h := entries[i]
			rowBg := "#ffffff"
			if (len(entries)-1-i)%2 == 1 {
				rowBg = "#f8fafc"
			}

			// entries are ascending (oldest first); compare with prior entry
			changeStr := "—"
			if i > 0 {
				diff := h.Price - entries[i-1].Price
				if diff < 0 {
					changeStr = fmt.Sprintf(`<span style="color:#16a34a;">▼ $%.2f</span>`, -diff)
				} else if diff > 0 {
					changeStr = fmt.Sprintf(`<span style="color:#dc2626;">▲ $%.2f</span>`, diff)
				} else {
					changeStr = `<span style="color:#94a3b8;">—</span>`
				}
			}

			sb.WriteString(fmt.Sprintf(`<tr style="background:%s;"><td style="padding:8px 12px;border-bottom:1px solid #f1f5f9;">%s</td><td style="text-align:right;padding:8px 12px;border-bottom:1px solid #f1f5f9;font-weight:600;">$%.2f</td><td style="text-align:right;padding:8px 12px;border-bottom:1px solid #f1f5f9;">%s</td></tr>`, rowBg, h.Date, h.Price, changeStr))
		}
		sb.WriteString(`</table>`)
	}

	// CTA button
	sb.WriteString(fmt.Sprintf(`<a href="%s" style="display:block;text-align:center;background:%s;color:white;padding:14px;border-radius:8px;text-decoration:none;font-weight:600;font-size:15px;">View Product →</a>`, alert.ProductURL, bannerColor))

	sb.WriteString(`</div>`) // end content
	// Footer
	sb.WriteString(`<div style="padding:16px 30px;background:#f8fafc;border-top:1px solid #e2e8f0;text-align:center;font-size:12px;color:#94a3b8;">Wishlist Price Tracker — You're receiving this because you registered this product for tracking.</div>`)
	sb.WriteString(`</div></body></html>`)

	return sb.String()
}
