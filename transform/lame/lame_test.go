package lame_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/c0ze/bumblebee/transform"
	_ "github.com/c0ze/bumblebee/transform/lame"
)

// pcm16 builds little-endian 16-bit mono PCM of a sine wave.
func pcm16(samples, rate int) []byte {
	buf := new(bytes.Buffer)
	for i := 0; i < samples; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/float64(rate)) * 30000)
		binary.Write(buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}

func TestLameEncodesMP3(t *testing.T) {
	p, err := transform.Build([]transform.Stage{
		{Type: "lame", Params: transform.Params{
			"sample_rate": 16000, "quality": 5, "channels": 1,
			"in_sample_rate": 16000, "in_bits": 16, "in_channels": 1, "in_big_endian": false,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := bytes.NewReader(pcm16(16000, 16000)) // 1s of audio
	var out bytes.Buffer
	ct, err := p.Run(context.Background(), in, &out, p.Resolve(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatal(err)
	}
	if ct != "audio/mpeg" {
		t.Fatalf("content-type %q", ct)
	}
	if out.Len() == 0 {
		t.Fatal("no MP3 output produced")
	}
	b := out.Bytes()
	if !(b[0] == 0xFF && (b[1]&0xE0) == 0xE0) && string(b[0:3]) != "ID3" {
		t.Fatalf("output does not look like MP3: % x", b[0:4])
	}
}
