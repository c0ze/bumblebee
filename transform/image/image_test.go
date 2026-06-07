package image_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/c0ze/bumblebee/transform"
	_ "github.com/c0ze/bumblebee/transform/image"
)

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestImageResizeToJPEG(t *testing.T) {
	p, err := transform.Build([]transform.Stage{
		{Type: "image", Params: transform.Params{"format": "jpeg", "max_width": 50, "quality": 80}},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := bytes.NewReader(pngBytes(200, 160))
	var out bytes.Buffer
	ct, err := p.Run(context.Background(), in, &out, p.Resolve(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/jpeg" {
		t.Fatalf("content-type %q", ct)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("output not a valid image: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("format %q", format)
	}
	if cfg.Width != 50 {
		t.Fatalf("width = %d, want 50", cfg.Width)
	}
}

func TestImageUnknownFormatErrors(t *testing.T) {
	p, _ := transform.Build([]transform.Stage{{Type: "image", Params: transform.Params{"format": "tiff-nope"}}})
	_, err := p.Run(context.Background(), bytes.NewReader(pngBytes(10, 10)), &bytes.Buffer{},
		p.Resolve(func(string) (string, bool) { return "", false }))
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}
