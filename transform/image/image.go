// Package image provides a pure-Go image transformer (resize/recompress).
package image

import (
	"context"
	"fmt"
	"io"

	"github.com/disintegration/imaging"

	"github.com/c0ze/bumblebee/transform"
)

func init() { transform.Register("image", New) }

type imageT struct{}

// New builds the image transformer (stateless; params read at transform time).
func New(_ transform.Params) (transform.Transformer, error) { return imageT{}, nil }

// Transform decodes an image, optionally resizes it within max_width/max_height
// (preserving aspect, never upscaling), and re-encodes it.
// Params: format (jpeg|png|gif), max_width, max_height, quality (jpeg).
func (imageT) Transform(_ context.Context, in io.Reader, out io.Writer, p transform.Params) (string, error) {
	img, err := imaging.Decode(in, imaging.AutoOrientation(true))
	if err != nil {
		return "", fmt.Errorf("image decode: %w", err)
	}
	maxW, maxH := p.Int("max_width", 0), p.Int("max_height", 0)
	b := img.Bounds()
	switch {
	case maxW > 0 && maxH > 0:
		img = imaging.Fit(img, maxW, maxH, imaging.Lanczos) // never upscales
	case maxW > 0 && maxW < b.Dx():
		img = imaging.Resize(img, maxW, 0, imaging.Lanczos)
	case maxH > 0 && maxH < b.Dy():
		img = imaging.Resize(img, 0, maxH, imaging.Lanczos)
	}
	format, ct, err := formatOf(p.String("format", "jpeg"))
	if err != nil {
		return "", err
	}
	var opts []imaging.EncodeOption
	if format == imaging.JPEG {
		opts = append(opts, imaging.JPEGQuality(p.Int("quality", 82)))
	}
	if err := imaging.Encode(out, img, format, opts...); err != nil {
		return "", fmt.Errorf("image encode: %w", err)
	}
	return ct, nil
}

func formatOf(name string) (imaging.Format, string, error) {
	switch name {
	case "jpeg", "jpg":
		return imaging.JPEG, "image/jpeg", nil
	case "png":
		return imaging.PNG, "image/png", nil
	case "gif":
		return imaging.GIF, "image/gif", nil
	default:
		return 0, "", fmt.Errorf("unsupported image format %q (jpeg, png, gif; webp needs the webp build tag)", name)
	}
}
