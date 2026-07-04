package main

// go run ./cmd/tainer-menu -icon-preview <out.png> renders a contact
// sheet of every icon state on light and dark strips — a dev tool for
// reviewing the menu bar art without launching the app.

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

func init() {
	if len(os.Args) == 3 && os.Args[1] == "-icon-preview" {
		writePreview(os.Args[2])
		os.Exit(0)
	}
	if len(os.Args) == 3 && os.Args[1] == "-icon-raw" {
		_ = os.WriteFile(os.Args[2], buildIconsStyle("").active, 0644)
		os.Exit(0)
	}
}

func writePreview(path string) {
	set := buildIconsStyle("")
	// row: current (halo), plate experiment, template silhouette
	states := [][]byte{
		set.active, set.idle, set.failed,
		renderIconPlate(1.0, 1, 1, iconBlue, iconOrange, iconTeal, false),
		renderIconPlate(0.45, 1, 1, iconBlue, iconOrange, iconTeal, false),
		renderIconPlate(0.55, 0, 0, iconGrey, iconGrey, iconGrey, true),
		renderIconTemplate(1.0, 0.55, 0.55, false),
	}

	cell := 64
	W := cell * len(states)
	H := cell * 2
	sheet := image.NewNRGBA(image.Rect(0, 0, W, H))
	// light strip (menu bar light ≈ #F6F6F6), dark strip (≈ #2A2A2C)
	draw.Draw(sheet, image.Rect(0, 0, W, cell), &image.Uniform{color.NRGBA{0xF6, 0xF6, 0xF6, 0xFF}}, image.Point{}, draw.Src)
	draw.Draw(sheet, image.Rect(0, cell, W, H), &image.Uniform{color.NRGBA{0x2A, 0x2A, 0x2C, 0xFF}}, image.Point{}, draw.Src)

	for i, b := range states {
		img, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			continue
		}
		off := image.Pt(i*cell+(cell-img.Bounds().Dx())/2, (cell-img.Bounds().Dy())/2)
		draw.Draw(sheet, img.Bounds().Add(off), img, image.Point{}, draw.Over)
		off2 := image.Pt(i*cell+(cell-img.Bounds().Dx())/2, cell+(cell-img.Bounds().Dy())/2)
		draw.Draw(sheet, img.Bounds().Add(off2), img, image.Point{}, draw.Over)
	}
	f, _ := os.Create(path)
	defer f.Close()
	_ = png.Encode(f, sheet)
}
