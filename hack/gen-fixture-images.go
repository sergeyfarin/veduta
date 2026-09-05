//go:build ignore

// Generates the tiny, deterministic local images internal/fixtures/showcase.json references,
// so B5's fixture dashboard needs no network access and no real media library. Re-run with
// `go run hack/gen-fixture-images.go` if the fixture ever needs a new image; the filename
// (ref + extension) is the whole contract with internal/fixtures.Load.
package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
)

type spec struct {
	ref     string
	w, h    int
	r, g, b uint8
}

func main() {
	specs := []spec{
		{"v1.poster.dune", 300, 450, 0xb8, 0x86, 0x4b},
		{"v1.poster.arrival", 300, 450, 0x4b, 0x6f, 0x86},
		{"v1.poster.bladerunner", 300, 450, 0x86, 0x4b, 0x5c},
		{"v1.poster.severance", 300, 450, 0x4b, 0x86, 0x63},
		{"v1.poster.andor", 300, 450, 0x6b, 0x4b, 0x86},
		{"v1.photo.p1", 400, 400, 0xc0, 0x6a, 0x4a},
		{"v1.photo.p2", 400, 400, 0x4a, 0xa0, 0xc0},
		{"v1.photo.p3", 400, 400, 0x8a, 0xc0, 0x4a},
		{"v1.photo.p4", 400, 400, 0xc0, 0xb0, 0x4a},
		{"v1.photo.p5", 400, 400, 0xa0, 0x4a, 0xc0},
		{"v1.photo.p6", 400, 400, 0x4a, 0x4a, 0xc0},
	}

	dir := "internal/fixtures/images"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}

	for _, s := range specs {
		img := image.NewRGBA(image.Rect(0, 0, s.w, s.h))
		fill := color.RGBA{s.r, s.g, s.b, 0xff}
		for y := 0; y < s.h; y++ {
			for x := 0; x < s.w; x++ {
				img.Set(x, y, fill)
			}
		}
		path := filepath.Join(dir, s.ref+".jpg")
		f, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 60}); err != nil {
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
		log.Println("wrote", path)
	}
}
