// Command genicons renders the app, tray, installer and Store icons from code
// so they are reproducible. Run: go run ./tools/genicons
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

var (
	green1 = color.NRGBA{0x2b, 0xb6, 0x73, 0xff}
	green2 = color.NRGBA{0x0f, 0x7a, 0x45, 0xff}
	white  = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// render draws the logo at size s (supersampled 4x) with an optional status dot.
func render(s int, dot color.Color, pad float64) *image.NRGBA {
	const ss = 4
	S := s * ss
	big := image.NewNRGBA(image.Rect(0, 0, S, S))
	fs := float64(S)
	r := fs * 0.22 // corner radius
	inset := fs * pad
	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			if !inRoundRect(px, py, inset, inset, fs-inset, fs-inset, r*(1-2*pad)) {
				continue
			}
			t := (px + py) / (2 * fs)
			big.SetNRGBA(x, y, lerp(green1, green2, t))
			// Speech bubble.
			cx, cy, br := fs*0.5, fs*0.47, fs*0.30
			inBubble := dist(px, py, cx, cy) <= br
			// Tail (triangle bottom-left).
			if pointInTri(px, py, fs*0.27, fs*0.62, fs*0.40, fs*0.72, fs*0.22, fs*0.82) {
				inBubble = true
			}
			if inBubble {
				big.SetNRGBA(x, y, white)
				// Forward arrow inside the bubble.
				if inArrow(px/fs, py/fs) {
					big.SetNRGBA(x, y, lerp(green1, green2, 0.6))
				}
			}
		}
	}
	if dot != nil {
		dc := color.NRGBAModel.Convert(dot).(color.NRGBA)
		cx, cy, dr := fs*0.78, fs*0.78, fs*0.22
		for y := 0; y < S; y++ {
			for x := 0; x < S; x++ {
				d := dist(float64(x)+0.5, float64(y)+0.5, cx, cy)
				if d <= dr {
					big.SetNRGBA(x, y, white)
				}
				if d <= dr*0.75 {
					big.SetNRGBA(x, y, dc)
				}
			}
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, s, s))
	draw.CatmullRom.Scale(out, out.Bounds(), big, big.Bounds(), draw.Over, nil)
	return out
}

func inArrow(x, y float64) bool {
	// Shaft.
	if x >= 0.36 && x <= 0.55 && y >= 0.435 && y <= 0.505 {
		return true
	}
	// Head.
	return pointInTri(x, y, 0.53, 0.36, 0.53, 0.58, 0.66, 0.47)
}

func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return dist(x, y, cx, cy) <= r
}

func dist(x, y, a, b float64) float64 { return math.Hypot(x-a, y-b) }

func pointInTri(px, py, ax, ay, bx, by, cx, cy float64) bool {
	s := func(x1, y1, x2, y2, x3, y3 float64) float64 { return (x1-x3)*(y2-y3) - (x2-x3)*(y1-y3) }
	d1, d2, d3 := s(px, py, ax, ay, bx, by), s(px, py, bx, by, cx, cy), s(px, py, cx, cy, ax, ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 0xff}
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		log.Fatal(err)
	}
	return b.Bytes()
}

// writeICO writes a PNG-compressed multi-size .ico.
func writeICO(path string, imgs []*image.NRGBA) {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	var data [][]byte
	for _, im := range imgs {
		d := pngBytes(im)
		data = append(data, d)
		w := im.Bounds().Dx()
		bw := uint8(w)
		if w >= 256 {
			bw = 0
		}
		binary.Write(&buf, binary.LittleEndian, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{bw, bw, 0, 0, 1, 32, uint32(len(d)), uint32(offset)})
		offset += len(d)
	}
	for _, d := range data {
		buf.Write(d)
	}
	must(os.WriteFile(path, buf.Bytes(), 0o644))
}

func writePNG(path string, img image.Image) { must(os.WriteFile(path, pngBytes(img), 0o644)) }

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// wide renders the logo centered on a transparent canvas of w×h.
func wide(w, h int) *image.NRGBA {
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	s := h * 7 / 10
	logo := render(s, nil, 0)
	off := image.Pt((w-s)/2, (h-s)/2)
	draw.Draw(canvas, logo.Bounds().Add(off), logo, image.Point{}, draw.Over)
	return canvas
}

// tall renders the logo centered on a brand-green poster of w×h.
func tall(w, h int) *image.NRGBA {
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		c := lerp(green1, green2, float64(y)/float64(h))
		for x := 0; x < w; x++ {
			canvas.SetNRGBA(x, y, c)
		}
	}
	s := w * 6 / 10
	logo := render(s, nil, 0)
	off := image.Pt((w-s)/2, (h-s)/2)
	draw.Draw(canvas, logo.Bounds().Add(off), logo, image.Point{}, draw.Over)
	return canvas
}

func main() {
	root := "assets"
	icons := filepath.Join(root, "icons")
	msix := filepath.Join(root, "msix")
	for _, d := range []string{icons, msix} {
		must(os.MkdirAll(d, 0o755))
	}
	var app []*image.NRGBA
	for _, s := range []int{16, 20, 24, 32, 40, 48, 64, 128, 256} {
		app = append(app, render(s, nil, 0))
	}
	writeICO(filepath.Join(icons, "app.ico"), app)
	writePNG(filepath.Join(icons, "app-256.png"), render(256, nil, 0))
	writePNG(filepath.Join(icons, "app-512.png"), render(512, nil, 0))

	dots := map[string]color.Color{
		"green":  color.NRGBA{0x22, 0xc5, 0x5e, 0xff},
		"yellow": color.NRGBA{0xf5, 0x9e, 0x0b, 0xff},
		"red":    color.NRGBA{0xef, 0x44, 0x44, 0xff},
		"gray":   color.NRGBA{0x9c, 0xa3, 0xaf, 0xff},
	}
	for name, c := range dots {
		writeICO(filepath.Join(icons, "tray-"+name+".ico"), []*image.NRGBA{render(16, c, 0), render(20, c, 0), render(24, c, 0), render(32, c, 0), render(48, c, 0)})
	}

	// MSIX / Store assets (plated, with 1/8 padding like system tiles).
	writePNG(filepath.Join(msix, "Square44x44Logo.png"), render(44, nil, 0))
	writePNG(filepath.Join(msix, "Square44x44Logo.targetsize-24_altform-unplated.png"), render(24, nil, 0))
	writePNG(filepath.Join(msix, "Square44x44Logo.targetsize-48_altform-unplated.png"), render(48, nil, 0))
	writePNG(filepath.Join(msix, "Square44x44Logo.targetsize-256_altform-unplated.png"), render(256, nil, 0))
	writePNG(filepath.Join(msix, "Square150x150Logo.png"), wide(150, 150))
	writePNG(filepath.Join(msix, "Wide310x150Logo.png"), wide(310, 150))
	writePNG(filepath.Join(msix, "StoreLogo.png"), render(50, nil, 0))
	writePNG(filepath.Join(msix, "SplashScreen.png"), wide(620, 300))

	// Partner Center listing artwork (uploaded by hand, not packaged).
	store := filepath.Join(root, "store")
	must(os.MkdirAll(store, 0o755))
	writePNG(filepath.Join(store, "app-icon-300.png"), render(300, nil, 0))
	writePNG(filepath.Join(store, "box-art-1080.png"), render(1080, nil, 0.12))
	writePNG(filepath.Join(store, "poster-720x1080.png"), tall(720, 1080))
	log.Println("icons written to", root)
}
