package transform

import (
	"bytes"
	"context"
	"fmt"
	"io"
)

// Params are transform configuration values (from config, overridable per-request).
type Params map[string]any

// Transformer converts an input stream to an output stream. One call per request;
// implementations must hold no shared mutable state so they are safe under -race.
type Transformer interface {
	Transform(ctx context.Context, in io.Reader, out io.Writer, p Params) (contentType string, err error)
}

// Factory builds a Transformer from its config params.
type Factory func(p Params) (Transformer, error)

var registry = map[string]Factory{}

// Register adds a transformer factory under name (called from each backend's init()).
func Register(name string, f Factory) { registry[name] = f }

// Stage is one configured pipeline step.
type Stage struct {
	Type      string
	Params    Params
	Overrides map[string]string // paramName -> request source key (header or query)
}

type builtStage struct {
	name      string
	t         Transformer
	base      Params
	overrides map[string]string
}

// Pipeline is an ordered list of transformers.
type Pipeline struct{ stages []builtStage }

// Build constructs a Pipeline, returning an error for unknown transform types.
func Build(stages []Stage) (*Pipeline, error) {
	if len(stages) == 0 {
		return nil, fmt.Errorf("pipeline has no stages")
	}
	p := &Pipeline{}
	for _, s := range stages {
		f, ok := registry[s.Type]
		if !ok {
			return nil, fmt.Errorf("unknown transform type %q", s.Type)
		}
		t, err := f(s.Params)
		if err != nil {
			return nil, fmt.Errorf("transform %q: %w", s.Type, err)
		}
		p.stages = append(p.stages, builtStage{name: s.Type, t: t, base: s.Params, overrides: s.Overrides})
	}
	return p, nil
}

// Resolve computes the effective params for each stage given a request value lookup.
func (p *Pipeline) Resolve(get func(key string) (string, bool)) []Params {
	out := make([]Params, len(p.stages))
	for i, s := range p.stages {
		eff := Params{}
		for k, v := range s.base {
			eff[k] = v
		}
		for param, src := range s.overrides {
			if val, ok := get(src); ok {
				eff[param] = val
			}
		}
		out[i] = eff
	}
	return out
}

// Run executes the pipeline. eff must have one Params per stage (from Resolve).
// The returned content-type is the last stage's; empty means "use the upstream type".
func (p *Pipeline) Run(ctx context.Context, in io.Reader, out io.Writer, eff []Params) (string, error) {
	var ct string
	cur := in
	for i, s := range p.stages {
		last := i == len(p.stages)-1
		w := out
		var buf *bytes.Buffer
		if !last {
			buf = &bytes.Buffer{}
			w = buf
		}
		c, err := s.t.Transform(ctx, cur, w, eff[i])
		if err != nil {
			return "", fmt.Errorf("transform %q: %w", s.name, err)
		}
		ct = c
		if !last {
			cur = bytes.NewReader(buf.Bytes())
		}
	}
	return ct, nil
}
