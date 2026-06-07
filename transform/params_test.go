package transform_test

import (
	"testing"

	"github.com/c0ze/bumblebee/transform"
)

func TestParamsInt(t *testing.T) {
	p := transform.Params{"a": 5, "b": int64(6), "c": float64(7), "d": "8", "e": "x"}
	cases := map[string]int{"a": 5, "b": 6, "c": 7, "d": 8, "e": 9, "missing": 9}
	for k, want := range cases {
		if got := p.Int(k, 9); got != want {
			t.Errorf("Int(%q): got %d want %d", k, got, want)
		}
	}
}

func TestParamsStringAndBool(t *testing.T) {
	p := transform.Params{"s": "hi", "n": 3, "b1": true, "b2": "true", "b3": "1", "b4": "no"}
	if p.String("s", "def") != "hi" || p.String("missing", "def") != "def" || p.String("n", "") != "3" {
		t.Fatalf("String coercion wrong: %v", p)
	}
	if !p.Bool("b1", false) || !p.Bool("b2", false) || !p.Bool("b3", false) {
		t.Fatal("Bool true cases failed")
	}
	if p.Bool("b4", true) != false || p.Bool("missing", true) != true {
		t.Fatal("Bool false/default cases failed")
	}
}
