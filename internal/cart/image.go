package cart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"wishlist-tracker/internal/models"
	brand "wishlist-tracker/web"

	imagedraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
	_ "golang.org/x/image/webp"
)

var ErrImageTooLarge = errors.New("this cart is too long for a single image; please export the text version")

const imageWidth = 900

var (
	blue      = color.RGBA{37, 99, 235, 255}
	dark      = color.RGBA{15, 23, 42, 255}
	gray      = color.RGBA{100, 116, 139, 255}
	lightGray = color.RGBA{241, 245, 249, 255}
	green     = color.RGBA{22, 163, 74, 255}
)

type imageFonts struct {
	title, heading, body, small, amount font.Face
}

func newImageFonts() (*imageFonts, error) {
	regular, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	bold, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return nil, err
	}
	var faces []font.Face
	for _, spec := range []struct {
		font *opentype.Font
		size float64
	}{{bold, 42}, {bold, 28}, {regular, 25}, {regular, 21}, {bold, 30}} {
		face, err := opentype.NewFace(spec.font, &opentype.FaceOptions{Size: spec.size, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			for _, face := range faces {
				face.Close()
			}
			return nil, err
		}
		faces = append(faces, face)
	}
	return &imageFonts{faces[0], faces[1], faces[2], faces[3], faces[4]}, nil
}

func (f *imageFonts) close() {
	for _, face := range []font.Face{f.title, f.heading, f.body, f.small, f.amount} {
		face.Close()
	}
}

func textAt(dst draw.Image, face font.Face, ink color.Color, x, y int, text string) {
	d := font.Drawer{Dst: dst, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(x, y)}
	d.DrawString(text)
}

func textRight(dst draw.Image, face font.Face, ink color.Color, x, y int, text string) {
	textAt(dst, face, ink, x-font.MeasureString(face, text).Ceil(), y, text)
}

func roundedRect(dst draw.Image, rect image.Rectangle, radius int, ink color.Color) {
	w, h, r := float32(rect.Dx()), float32(rect.Dy()), float32(radius)
	mask := image.NewAlpha(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	z := vector.NewRasterizer(rect.Dx(), rect.Dy())
	z.MoveTo(r, 0)
	z.LineTo(w-r, 0)
	z.QuadTo(w, 0, w, r)
	z.LineTo(w, h-r)
	z.QuadTo(w, h, w-r, h)
	z.LineTo(r, h)
	z.QuadTo(0, h, 0, h-r)
	z.LineTo(0, r)
	z.QuadTo(0, 0, r, 0)
	z.ClosePath()
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	draw.DrawMask(dst, rect, image.NewUniform(ink), image.Point{}, mask, image.Point{}, draw.Over)
}

func containImage(dst draw.Image, rect image.Rectangle, source image.Image) {
	bounds := source.Bounds()
	scale := math.Min(float64(rect.Dx())/float64(bounds.Dx()), float64(rect.Dy())/float64(bounds.Dy()))
	w, h := int(float64(bounds.Dx())*scale), int(float64(bounds.Dy())*scale)
	x, y := rect.Min.X+(rect.Dx()-w)/2, rect.Min.Y+(rect.Dy()-h)/2
	imagedraw.CatmullRom.Scale(dst, image.Rect(x, y, x+w, y+h), source, bounds, draw.Over, nil)
}

func wrapText(face font.Face, text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if font.MeasureString(face, candidate).Ceil() <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for _, char := range word {
			candidate = line + string(char)
			if line != "" && font.MeasureString(face, candidate).Ceil() > width {
				lines = append(lines, line)
				line = string(char)
			} else {
				line = candidate
			}
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

type imageRow struct {
	item      models.CartItem
	lines     []string
	y, height int
}

type imageGroup struct {
	group storeGroup
	y     int
	rows  []imageRow
}

func RenderPNG(ctx context.Context, items []models.CartItem, now time.Time) ([]byte, int, error) {
	fonts, err := newImageFonts()
	if err != nil {
		return nil, 0, fmt.Errorf("load shopping list fonts: %w", err)
	}
	defer fonts.close()
	logo, err := png.Decode(bytes.NewReader(brand.BearLogo))
	if err != nil {
		return nil, 0, fmt.Errorf("load brand logo: %w", err)
	}

	groups := groupItems(items)
	total := summarize(items)
	var layout []imageGroup
	y := 236
	for _, group := range groups {
		section := imageGroup{group: group, y: y}
		y += 80
		for _, item := range group.items {
			lines := wrapText(fonts.body, item.Name, 430)
			height := max(132, len(lines)*32+74)
			section.rows = append(section.rows, imageRow{item: item, lines: lines, y: y, height: height})
			y += height + 12
			if y > 29000 {
				return nil, 0, ErrImageTooLarge
			}
		}
		layout = append(layout, section)
		y += 20
	}
	totalY := y
	totalHeight := 118
	if total.unknown > 0 {
		totalHeight += 30
	}
	height := max(1100, totalY+totalHeight+185)
	if height > 30000 {
		return nil, 0, ErrImageTooLarge
	}

	photos := fetchProductImages(ctx, items)
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, imageWidth, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(lightGray), image.Point{}, draw.Src)
	roundedRect(canvas, image.Rect(32, 32, 868, 212), 24, blue)
	containImage(canvas, image.Rect(56, 55, 136, 135), logo)
	textAt(canvas, fonts.title, color.White, 158, 91, "Shopping list")
	textAt(canvas, fonts.small, color.White, 160, 133,
		fmt.Sprintf("%d products  |  %d items  |  %d stores", len(items), total.quantity, len(groups)))
	textAt(canvas, fonts.small, color.RGBA{219, 234, 254, 255}, 56, 179, now.Format("02 Jan 2006, 15:04 MST"))

	missingPhotos := 0
	for _, section := range layout {
		textAt(canvas, fonts.heading, dark, 40, section.y+30, section.group.store)
		subtotal := summarize(section.group.items)
		textAt(canvas, fonts.small, gray, 40, section.y+60, fmt.Sprintf("%d items", subtotal.quantity))
		amount := money(subtotal.cents)
		if subtotal.unknown > 0 {
			amount += " + unknown"
		}
		textRight(canvas, fonts.body, gray, 860, section.y+37, amount)
		for _, row := range section.rows {
			roundedRect(canvas, image.Rect(40, row.y, 860, row.y+row.height), 16, color.White)
			roundedRect(canvas, image.Rect(56, row.y+54, 80, row.y+78), 5, color.RGBA{203, 213, 225, 255})
			roundedRect(canvas, image.Rect(59, row.y+57, 77, row.y+75), 3, color.White)
			photoRect := image.Rect(96, row.y+22, 184, row.y+110)
			if photo := photos[row.item.ID]; photo != nil {
				containImage(canvas, photoRect, photo)
			} else {
				missingPhotos++
				roundedRect(canvas, photoRect, 10, lightGray)
				textAt(canvas, fonts.small, gray, 111, row.y+73, "Photo")
			}
			for i, line := range row.lines {
				textAt(canvas, fonts.body, dark, 204, row.y+37+i*32, line)
			}
			unitPrice, linePrice := "Price unavailable", "Unknown"
			if row.item.CurrentPrice != nil {
				unitPrice = money(int64(math.Round(*row.item.CurrentPrice*100))) + " each"
				linePrice = money(lineCents(row.item))
			}
			textAt(canvas, fonts.small, gray, 204, row.y+39+len(row.lines)*32, unitPrice)
			textRight(canvas, fonts.amount, green, 836, row.y+41, linePrice)
			textRight(canvas, fonts.body, gray, 836, row.y+86, fmt.Sprintf("x%d", row.item.Quantity))
		}
	}
	roundedRect(canvas, image.Rect(40, totalY, 860, totalY+totalHeight), 18, color.RGBA{219, 234, 254, 255})
	textAt(canvas, fonts.heading, dark, 64, totalY+42, totalLabel(total))
	textRight(canvas, fonts.amount, blue, 836, totalY+43, money(total.cents))
	textAt(canvas, fonts.small, gray, 64, totalY+82, fmt.Sprintf("%d products  |  %d items", len(items), total.quantity))
	if total.unknown > 0 {
		textAt(canvas, fonts.small, gray, 64, totalY+116, fmt.Sprintf("%d item(s) have no recorded price and are not included.", total.unknown))
	}
	brandWidth := 84 + font.MeasureString(fonts.heading, "Wishlist Tracker").Ceil()
	brandX := (imageWidth - brandWidth) / 2
	containImage(canvas, image.Rect(brandX, height-156, brandX+64, height-92), logo)
	textAt(canvas, fonts.small, gray, brandX+84, height-134, "Generated by")
	textAt(canvas, fonts.heading, dark, brandX+84, height-101, "Wishlist Tracker")
	note := "Prices are last recorded estimates and may change in store."
	textAt(canvas, fonts.small, gray, (imageWidth-font.MeasureString(fonts.small, note).Ceil())/2, height-57, note)
	if missingPhotos > 0 {
		note = fmt.Sprintf("%d product photo(s) unavailable; all items are included.", missingPhotos)
		textAt(canvas, fonts.small, gray, (imageWidth-font.MeasureString(fonts.small, note).Ceil())/2, height-27, note)
	}

	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, 0, fmt.Errorf("encode shopping list: %w", err)
	}
	return output.Bytes(), missingPhotos, nil
}

func safeImageURL(raw string) (string, error) {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return "", fmt.Errorf("unsupported product photo URL")
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"woolworths.media", "woolworths.com.au", "chemistwarehouse.com.au", "images-iherb.com", "iherb.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return u.String(), nil
		}
	}
	return "", fmt.Errorf("product photo is not hosted by a supported retailer")
}

func loadProductImage(ctx context.Context, client *http.Client, rawURL, productURL string) (image.Image, error) {
	source, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if !source.IsAbs() && source.Host == "" {
		base, err := url.Parse(productURL)
		if err != nil {
			return nil, err
		}
		source = base.ResolveReference(source)
	}
	safeURL, err := safeImageURL(source.String())
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, safeURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/124.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("photo request returned HTTP %d", resp.StatusCode)
	}
	const maxBytes = 5 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("product photo exceeds 5 MB")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 16000000 {
		return nil, fmt.Errorf("product photo dimensions are too large")
	}
	photo, _, err := image.Decode(bytes.NewReader(data))
	return photo, err
}

func fetchProductImages(ctx context.Context, items []models.CartItem) map[string]image.Image {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many product photo redirects")
			}
			_, err := safeImageURL(req.URL.String())
			return err
		},
	}
	photos := make(map[string]image.Image)
	jobs := make(chan models.CartItem, len(items))
	for _, item := range items {
		if item.ImageURL != "" {
			jobs <- item
		}
	}
	close(jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for range min(4, len(items)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				photo, err := loadProductImage(ctx, client, item.ImageURL, item.URL)
				if err != nil {
					log.Printf("[cart] product photo unavailable for %s: %v", item.ID, err)
					continue
				}
				mu.Lock()
				photos[item.ID] = photo
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return photos
}
