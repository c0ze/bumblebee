package passthrough

import (
	"context"
	"io"

	"github.com/c0ze/bumblebee/transform"
)

func init() { transform.Register("passthrough", New) }

type passthrough struct{}

// New builds a passthrough transformer (copies input to output unchanged).
func New(_ transform.Params) (transform.Transformer, error) { return &passthrough{}, nil }

// Transform copies in->out and returns an empty content-type so the router falls
// back to the upstream response's Content-Type.
func (passthrough) Transform(_ context.Context, in io.Reader, out io.Writer, _ transform.Params) (string, error) {
	_, err := io.Copy(out, in)
	return "", err
}
