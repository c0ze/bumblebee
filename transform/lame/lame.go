// Package lame provides a PCM/L16 -> MP3 transformer backed by libmp3lame (CGo).
package lame

import (
	"context"
	"io"

	golame "github.com/sunicy/go-lame"

	"github.com/c0ze/bumblebee/transform"
)

func init() { transform.Register("lame", New) }

type lameT struct{}

// New builds the lame transformer. Params (with request overrides) are read at
// transform time, so the transformer itself is stateless.
func New(_ transform.Params) (transform.Transformer, error) { return lameT{}, nil }

// Transform encodes raw PCM read from in into MP3 written to out.
// Params: sample_rate, quality (0=best..9), channels, in_sample_rate, in_bits,
// in_channels, in_big_endian.
func (lameT) Transform(_ context.Context, in io.Reader, out io.Writer, p transform.Params) (string, error) {
	wr, err := golame.NewWriter(out)
	if err != nil {
		return "", err
	}
	wr.InBigEndian = p.Bool("in_big_endian", false)
	wr.InSampleRate = p.Int("in_sample_rate", 16000)
	wr.InBitsPerSample = p.Int("in_bits", 16)
	wr.InNumChannels = p.Int("in_channels", 1)
	wr.OutSampleRate = p.Int("sample_rate", 16000)
	wr.OutQuality = p.Int("quality", 5)
	if p.Int("channels", 1) >= 2 {
		wr.OutMode = golame.MODE_STEREO
	} else {
		wr.OutMode = golame.MODE_MONO
	}
	if _, err := io.Copy(wr, in); err != nil {
		wr.Close()
		return "", err
	}
	if err := wr.Close(); err != nil {
		return "", err
	}
	return "audio/mpeg", nil
}
