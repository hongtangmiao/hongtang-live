package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// fetchJSONOnce：单次请求，超时、非 2xx、空响应都算失败；顶层是数组时放在 "_" 里
func fetchJSONOnce(rawURL string, hdr map[string]string, timeout time.Duration) (M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, errors.New("HTTP " + resp.Status)
	}
	if strings.TrimSpace(string(b)) == "" {
		return nil, errors.New("empty response")
	}
	var v any
	if err := decodeJSON(b, &v); err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]any); ok {
		return m, nil
	}
	return M{"_": v}, nil
}
