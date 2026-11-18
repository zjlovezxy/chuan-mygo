// Command genicon draws the app icon: two arrows (send / receive) on the
// accent square. It writes resources/icon.png for mygo and the Android
// launcher icon, so no binary artwork has to live in the repository.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const size = 1024

func main() {
	img := draw()

	if err := write("resources/icon.png", img); err != nil {
		log.Fatal(err)
	}
	if err := write("android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png", img); err != nil {
		log.Fatal(err)
	}
	log.Println("icons written")
}

func write(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func draw() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	accent := color.NRGBA{0xE1, 0x1D, 0x48, 0xFF}
	white := color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF}

	const radius = 220.0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			dx := math.Max(math.Max(radius-fx, fx-(size-radius)), 0)
			dy := math.Max(math.Max(radius-fy, fy-(size-radius)), 0)
			if math.Hypot(dx, dy) <= radius {
				img.Set(x, y, accent)
			}
		}
	}

	// arrow draws an up-pointing arrow at cx, mirrored when flip is set.
	arrow := func(cx float64, flip bool) {
		const tip, headEnd, shaftEnd, shaftHalf, headExtra = 258.0, 430.0, 766.0, 46.0, 132.0
		for y := 0; y < size; y++ {
			fy := float64(y) + 0.5
			yy := fy
			if flip {
				yy = size - fy
			}
			half := -1.0
			switch {
			case yy >= headEnd && yy <= shaftEnd:
				half = shaftHalf
			case yy >= tip && yy < headEnd:
				t := (yy - tip) / (headEnd - tip)
				half = shaftHalf + t*headExtra
			}
			if half <= 0 {
				continue
			}
			for x := 0; x < size; x++ {
				if math.Abs(float64(x)+0.5-cx) <= half {
					img.Set(x, y, white)
				}
			}
		}
	}
	arrow(360, false) // up: send
	arrow(664, true)  // down: receive

	return img
}
