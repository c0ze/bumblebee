// Package video provides an ffmpeg-backed video transformer. The argv is built
// only from typed params (no shell, no raw passthrough), and I/O goes through
// temp files. The process is killed on context cancellation.
package video

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/c0ze/bumblebee/transform"
)

func init() { transform.Register("video", New) }

type videoT struct{}

func New(_ transform.Params) (transform.Transformer, error) { return videoT{}, nil }

// Transform transcodes the input video. Params: format (mp4|webm), codec, crf,
// scale (e.g. "640:-2"), preset, bitrate.
func (videoT) Transform(ctx context.Context, in io.Reader, out io.Writer, p transform.Params) (string, error) {
	inF, err := os.CreateTemp("", "bb-vin-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(inF.Name())
	if _, err := io.Copy(inF, in); err != nil {
		inF.Close()
		return "", err
	}
	inF.Close()

	format, ct, err := containerOf(p.String("format", "mp4"))
	if err != nil {
		return "", err
	}
	outPath := inF.Name() + ".out." + format
	defer os.Remove(outPath)

	args := []string{"-y", "-i", inF.Name()}
	if codec := p.String("codec", ""); codec != "" {
		args = append(args, "-c:v", codec)
	}
	if crf := p.Int("crf", -1); crf >= 0 {
		args = append(args, "-crf", fmt.Sprintf("%d", crf))
	}
	if preset := p.String("preset", ""); preset != "" {
		args = append(args, "-preset", preset)
	}
	if bitrate := p.String("bitrate", ""); bitrate != "" {
		args = append(args, "-b:v", bitrate)
	}
	if scale := p.String("scale", ""); scale != "" {
		args = append(args, "-vf", "scale="+scale)
	}
	args = append(args, "-f", format, outPath)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg: %w: %s", err, tail(stderr.String(), 500))
	}
	f, err := os.Open(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(out, f); err != nil {
		return "", err
	}
	return ct, nil
}

// containerOf maps a format name to ffmpeg's muxer name + content-type.
func containerOf(name string) (format, contentType string, err error) {
	switch name {
	case "mp4":
		return "mp4", "video/mp4", nil
	case "webm":
		return "webm", "video/webm", nil
	default:
		return "", "", fmt.Errorf("unsupported video format %q (mp4, webm)", name)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
