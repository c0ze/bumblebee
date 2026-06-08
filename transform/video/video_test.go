package video_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/c0ze/bumblebee/transform"
	_ "github.com/c0ze/bumblebee/transform/video"
)

func ffmpegOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

// genClip produces a tiny mp4 with ffmpeg's testsrc generator.
func genClip(t *testing.T) []byte {
	t.Helper()
	out := t.TempDir() + "/in.mp4"
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=10", "-pix_fmt", "yuv420p", out)
	if err := cmd.Run(); err != nil {
		t.Skipf("could not generate test clip: %v", err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVideoTranscode(t *testing.T) {
	ffmpegOrSkip(t)
	clip := genClip(t)

	p, err := transform.Build([]transform.Stage{
		{Type: "video", Params: transform.Params{"format": "mp4", "codec": "libx264", "crf": 28, "scale": "32:-2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ct, err := p.Run(context.Background(), bytes.NewReader(clip), &out, p.Resolve(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatalf("transcode: %v", err)
	}
	if ct != "video/mp4" {
		t.Fatalf("content-type %q", ct)
	}
	if out.Len() == 0 {
		t.Fatal("no output")
	}
	// an "ftyp" box near the start identifies an MP4/ISO-BMFF container.
	if !bytes.Contains(out.Bytes()[:min(64, out.Len())], []byte("ftyp")) {
		t.Fatalf("output does not look like mp4: % x", out.Bytes()[:min(16, out.Len())])
	}
}
