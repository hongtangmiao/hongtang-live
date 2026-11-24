package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
)

func send(w http.ResponseWriter, code int, body []byte, hdr map[string]string) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	for k, v := range hdr {
		h.Set(k, v)
	}
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func sendText(w http.ResponseWriter, code int, s string) {
	send(w, code, []byte(s), map[string]string{"Content-Type": "text/plain; charset=utf-8"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	send(w, code, jsonBytes(v), map[string]string{"Content-Type": "application/json; charset=utf-8"})
}

// crossOrigin：带了 Origin 且和 Host 不一样就拒绝，免得被别的站点当免费代理
func crossOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	oh := ""
	if u, err := url.Parse(origin); err == nil {
		oh = u.Host
	}
	return oh != "" && oh != r.Host
}

var errBodyTooLarge = errors.New("body too large")

func readBody(r *http.Request, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errBodyTooLarge
	}
	return b, nil
}
