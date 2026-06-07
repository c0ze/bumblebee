package router

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/c0ze/bumblebee/cache"
	"github.com/c0ze/bumblebee/transform"
)

// deriveKey builds the content-addressed cache key for a request.
func deriveKey(rt *route, r *http.Request, body []byte, eff []transform.Params) cache.Key {
	h := sha256.New()
	writeField := func(s string) { io.WriteString(h, s); h.Write([]byte{0}) }

	writeField("route:" + rt.path)
	if strings.Contains(rt.upstreamURL, "{path}") {
		writeField("path:" + r.URL.Path)
	}
	for _, k := range rt.keyHeaders {
		writeField("h:" + k + "=" + r.Header.Get(k))
	}
	for _, k := range rt.keyQuery {
		writeField("q:" + k + "=" + r.URL.Query().Get(k))
	}
	for i, p := range eff {
		for _, pk := range sortedKeys(p) {
			writeField(fmt.Sprintf("p%d:%s=%v", i, pk, p[pk]))
		}
	}
	if rt.forwardBody {
		h.Write(body)
	}
	return cache.Key(hex.EncodeToString(h.Sum(nil)))
}

func sortedKeys(p transform.Params) []string {
	ks := make([]string, 0, len(p))
	for k := range p {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
