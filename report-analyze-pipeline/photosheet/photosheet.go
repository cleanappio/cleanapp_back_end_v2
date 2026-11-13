// Package photosheet turns the several photos of one report into the single
// image the analysis pipeline works with.
//
// Every model call in this service (the main analysis, the location pass and
// the evidence classifier, across three providers) takes exactly one image.
// Rather than change all of those interfaces, the photos are laid out as a
// contact sheet: a grid, in the order the reporter added them, left to right
// and top to bottom. The model sees every photo; nothing else has to know
// that a report can have more than one.
//
// The sheet is used for analysis only. It is never stored and never shown;
// the report page serves the original photos.
package photosheet

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // decode PNG photos

	"fmt"
)

const (
	// MaxPhotos is how many photos one sheet holds.
	MaxPhotos = 4
	// cell is the side of one grid cell in pixels; a 2x2 sheet is 2048 wide.
	cell   = 1024
	gutter = 8
)

// Compose returns a JPEG contact sheet of the photos. With one photo it
// returns that photo unchanged. Photos that cannot be decoded are skipped; if
// fewer than two remain, the first usable photo is returned as it was, so a
// damaged extra photo never costs the report its analysis.
func Compose(photos [][]byte) ([]byte, error) {
	if len(photos) == 0 {
		return nil, nil
	}
	if len(photos) > MaxPhotos {
		photos = photos[:MaxPhotos]
	}
	var decoded []image.Image
	var firstUsable []byte
	for _, photo := range photos {
		img, _, err := image.Decode(bytes.NewReader(photo))
		if err != nil {
			continue
		}
		if firstUsable == nil {
			firstUsable = photo
		}
		decoded = append(decoded, img)
	}
	if len(decoded) < 2 {
		if firstUsable != nil {
			return firstUsable, nil
		}
		return photos[0], nil
	}

	columns := 2
	rows := (len(decoded) + 1) / 2
	sheet := image.NewRGBA(image.Rect(0, 0, columns*cell+(columns+1)*gutter, rows*cell+(rows+1)*gutter))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for i, img := range decoded {
		x := gutter + (i%columns)*(cell+gutter)
		y := gutter + (i/columns)*(cell+gutter)
		fit(sheet, image.Rect(x, y, x+cell, y+cell), img)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, sheet, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("encode contact sheet: %w", err)
	}
	return out.Bytes(), nil
}

// fit draws src into the middle of area, scaled down to fit with its aspect
// ratio kept. Small photos are not enlarged. Each destination pixel averages
// the source pixels it covers (a box filter), which keeps text in screenshots
// legible when a photo is reduced.
func fit(dst *image.RGBA, area image.Rectangle, src image.Image) {
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	if sw <= 0 || sh <= 0 {
		return
	}
	dw, dh := sw, sh
	if dw > area.Dx() || dh > area.Dy() {
		if sw*area.Dy() >= sh*area.Dx() { // wider than the cell
			dw, dh = area.Dx(), max(1, sh*area.Dx()/sw)
		} else {
			dw, dh = max(1, sw*area.Dy()/sh), area.Dy()
		}
	}
	offsetX := area.Min.X + (area.Dx()-dw)/2
	offsetY := area.Min.Y + (area.Dy()-dh)/2
	for y := 0; y < dh; y++ {
		sy0 := bounds.Min.Y + y*sh/dh
		sy1 := max(sy0+1, bounds.Min.Y+(y+1)*sh/dh)
		for x := 0; x < dw; x++ {
			sx0 := bounds.Min.X + x*sw/dw
			sx1 := max(sx0+1, bounds.Min.X+(x+1)*sw/dw)
			var r, g, b, n uint32
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					pr, pg, pb, _ := src.At(sx, sy).RGBA()
					r, g, b, n = r+pr>>8, g+pg>>8, b+pb>>8, n+1
				}
			}
			dst.SetRGBA(offsetX+x, offsetY+y, color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: 255})
		}
	}
}

// Note is the sentence added to the analysis input when a sheet was made, so
// the model reads the grid as separate photos of one report.
func Note(count int) string {
	if count < 2 {
		return ""
	}
	return fmt.Sprintf("The attached image is a contact sheet of %d photos the reporter submitted together for this one report, in order from left to right and top to bottom. Consider all of them.", count)
}
