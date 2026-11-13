package photosheet

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func solid(t *testing.T, w, h int, c color.RGBA, asPNG bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	var out bytes.Buffer
	var err error
	if asPNG {
		err = png.Encode(&out, img)
	} else {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 95})
	}
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func near(got color.Color, want color.RGBA) bool {
	r, g, b, _ := got.RGBA()
	d := func(a uint32, b uint8) bool { x := int(a>>8) - int(b); return x > -24 && x < 24 }
	return d(r, want.R) && d(g, want.G) && d(b, want.B)
}

func TestOnePhotoIsReturnedUnchanged(t *testing.T) {
	photo := solid(t, 40, 30, color.RGBA{R: 200}, false)
	got, err := Compose([][]byte{photo})
	if err != nil || !bytes.Equal(got, photo) {
		t.Fatal("a single photo must pass through byte for byte")
	}
	if got, _ := Compose(nil); got != nil {
		t.Fatal("no photos must give no image")
	}
	if Note(1) != "" || Note(3) == "" {
		t.Fatal("the note belongs to sheets only")
	}
}

func TestFourPhotosLandInReadingOrder(t *testing.T) {
	red, green, blue, yellow := color.RGBA{R: 220}, color.RGBA{G: 200}, color.RGBA{B: 220}, color.RGBA{R: 220, G: 220}
	sheetBytes, err := Compose([][]byte{
		solid(t, 3000, 2000, red, false), // larger than a cell: scaled down
		solid(t, 300, 300, green, true),  // smaller than a cell: not enlarged
		solid(t, 500, 1500, blue, false), // tall
		solid(t, 800, 800, yellow, true),
	})
	if err != nil {
		t.Fatal(err)
	}
	sheet, format, err := image.Decode(bytes.NewReader(sheetBytes))
	if err != nil || format != "jpeg" {
		t.Fatalf("sheet is not a JPEG: %v", err)
	}
	if w, h := sheet.Bounds().Dx(), sheet.Bounds().Dy(); w != 2*cell+3*gutter || h != 2*cell+3*gutter {
		t.Fatalf("sheet is %dx%d", w, h)
	}
	center := func(i int) (int, int) {
		return gutter + (i%2)*(cell+gutter) + cell/2, gutter + (i/2)*(cell+gutter) + cell/2
	}
	for i, want := range []color.RGBA{red, green, blue, yellow} {
		x, y := center(i)
		if !near(sheet.At(x, y), want) {
			t.Errorf("photo %d is not in cell %d", i+1, i+1)
		}
	}
	// The 300px photo was not enlarged: the corner of its cell is background.
	if !near(sheet.At(gutter+cell+gutter+20, gutter+20), color.RGBA{R: 255, G: 255, B: 255}) {
		t.Error("small photo was enlarged to fill its cell")
	}
	// The 3:2 photo kept its shape: the top of its cell is background.
	if !near(sheet.At(gutter+cell/2, gutter+20), color.RGBA{R: 255, G: 255, B: 255}) {
		t.Error("wide photo was stretched to fill its cell")
	}
}

func TestTwoAndThreePhotosAndTheLimit(t *testing.T) {
	photo := solid(t, 100, 100, color.RGBA{R: 200}, false)
	for count, wantRows := range map[int]int{2: 1, 3: 2, 6: 2} {
		photos := make([][]byte, count)
		for i := range photos {
			photos[i] = photo
		}
		sheetBytes, err := Compose(photos)
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(sheetBytes))
		if err != nil || cfg.Height != wantRows*cell+(wantRows+1)*gutter {
			t.Errorf("%d photos: height %d, err %v", count, cfg.Height, err)
		}
	}
}

func TestDamagedExtraPhotoNeverCostsTheAnalysisItsImage(t *testing.T) {
	good := solid(t, 100, 100, color.RGBA{G: 200}, true)
	got, err := Compose([][]byte{good, []byte("not an image")})
	if err != nil || !bytes.Equal(got, good) {
		t.Fatal("with one usable photo, that photo must be returned unchanged")
	}
	got, err = Compose([][]byte{[]byte("broken"), good, good})
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(got)); err != nil || cfg.Height != cell+2*gutter {
		t.Fatal("two usable photos out of three must still make a sheet")
	}
	// Nothing usable at all: hand back the original bytes rather than nothing.
	if got, _ := Compose([][]byte{[]byte("a"), []byte("b")}); string(got) != "a" {
		t.Fatal("undecodable input must fall back to the first photo's bytes")
	}
}
