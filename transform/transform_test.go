package transform_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/c0ze/bumblebee/transform"
	_ "github.com/c0ze/bumblebee/transform/passthrough"
)

// upper is a tiny test transformer that upper-cases and tags content-type.
type upper struct{ suffix string }

func newUpper(p transform.Params) (transform.Transformer, error) {
	s, _ := p["suffix"].(string)
	return &upper{suffix: s}, nil
}

func (u *upper) Transform(_ context.Context, in io.Reader, out io.Writer, p transform.Params) (string, error) {
	b, err := io.ReadAll(in)
	if err != nil {
		return "", err
	}
	s := strings.ToUpper(string(b))
	if v, ok := p["suffix"].(string); ok {
		s += v
	}
	_, err = io.WriteString(out, s)
	return "text/test", err
}

func TestPipelineRunAndResolve(t *testing.T) {
	transform.Register("upper", newUpper)
	p, err := transform.Build([]transform.Stage{
		{Type: "upper", Params: transform.Params{"suffix": "!"}, Overrides: map[string]string{"suffix": "X-Suffix"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	eff := p.Resolve(func(k string) (string, bool) {
		if k == "X-Suffix" {
			return "?", true
		}
		return "", false
	})
	if eff[0]["suffix"] != "?" {
		t.Fatalf("override not applied: %v", eff[0])
	}

	var out bytes.Buffer
	ct, err := p.Run(context.Background(), strings.NewReader("hi"), &out, eff)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "HI?" {
		t.Fatalf("got %q", out.String())
	}
	if ct != "text/test" {
		t.Fatalf("content-type %q", ct)
	}
}

func TestBuildUnknownType(t *testing.T) {
	if _, err := transform.Build([]transform.Stage{{Type: "nope"}}); err == nil {
		t.Fatal("expected error for unknown transform type")
	}
}

func TestPassthrough(t *testing.T) {
	p, err := transform.Build([]transform.Stage{{Type: "passthrough"}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ct, err := p.Run(context.Background(), strings.NewReader("abc"), &out, p.Resolve(func(string) (string, bool) { return "", false }))
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "abc" || ct != "" {
		t.Fatalf("got %q ct=%q", out.String(), ct)
	}
}
