// Package chart renders price-history sparkline PNGs that can be
// embedded in emails or served via the API.
package chart

import (
	"bytes"
	"fmt"
	"time"

	"wishlist-tracker/internal/models"

	gochart "github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

// Render generates a PNG price-history chart and returns the raw bytes.
// The chart is a clean sparkline-style line graph suitable for embedding
// in HTML emails (via CID) or serving from an API endpoint.
func Render(history []models.PriceHistory, productName string) ([]byte, error) {
	if len(history) < 2 {
		return nil, fmt.Errorf("need at least 2 data points, got %d", len(history))
	}

	xValues := make([]time.Time, 0, len(history))
	yValues := make([]float64, 0, len(history))

	for _, h := range history {
		t, err := time.Parse("2006-01-02", h.Date)
		if err != nil {
			continue
		}
		xValues = append(xValues, t)
		yValues = append(yValues, h.Price)
	}

	if len(xValues) < 2 {
		return nil, fmt.Errorf("not enough valid data points after parsing")
	}

	// Find min/max for Y axis padding
	minY, maxY := yValues[0], yValues[0]
	for _, v := range yValues {
		if v < minY {
			minY = v
		}
		if v > maxY {
			maxY = v
		}
	}
	padding := (maxY - minY) * 0.15
	if padding < 0.5 {
		padding = 0.5
	}

	// Latest price for the annotation
	lastIdx := len(yValues) - 1

	priceSeries := gochart.TimeSeries{
		Name: "Price",
		Style: gochart.Style{
			StrokeColor: drawing.ColorFromHex("2563eb"),
			StrokeWidth: 2.5,
			DotColor:    drawing.ColorFromHex("2563eb"),
			DotWidth:    3,
		},
		XValues: xValues,
		YValues: yValues,
	}

	// Annotation on the latest price
	lastPriceAnnotation := gochart.LastValueAnnotationSeries(priceSeries)
	lastPriceAnnotation.Style = gochart.Style{
		FontSize:    10,
		FontColor:   drawing.ColorWhite,
		FillColor:   drawing.ColorFromHex("16a34a"),
		StrokeColor: drawing.ColorFromHex("16a34a"),
		Padding: gochart.Box{
			Top:    4,
			Left:   6,
			Right:  6,
			Bottom: 4,
		},
	}
	_ = lastIdx

	graph := gochart.Chart{
		Title:  productName,
		Width:  600,
		Height: 280,
		TitleStyle: gochart.Style{
			FontSize: 12,
		},
		Background: gochart.Style{
			FillColor: drawing.ColorWhite,
			Padding: gochart.Box{
				Top:    20,
				Left:   10,
				Right:  30,
				Bottom: 10,
			},
		},
		Canvas: gochart.Style{
			FillColor: drawing.ColorFromHex("f8fafc"),
		},
		XAxis: gochart.XAxis{
			Style: gochart.Style{
				FontSize: 8,
			},
			ValueFormatter: gochart.TimeDateValueFormatter,
		},
		YAxis: gochart.YAxis{
			Style: gochart.Style{
				FontSize: 9,
			},
			ValueFormatter: func(v interface{}) string {
				return fmt.Sprintf("$%.2f", v)
			},
			Range: &gochart.ContinuousRange{
				Min: minY - padding,
				Max: maxY + padding,
			},
		},
		Series: []gochart.Series{
			priceSeries,
			lastPriceAnnotation,
		},
	}

	var buf bytes.Buffer
	if err := graph.Render(gochart.PNG, &buf); err != nil {
		return nil, fmt.Errorf("render chart: %w", err)
	}

	return buf.Bytes(), nil
}
