package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

/* 服务器接口：docs/device-api.md */

type Hints struct {
	Name      string `json:"name"`
	ReportSec int    `json:"report_sec"`
	AWDaily   bool   `json:"aw_daily"`
	Writer    bool   `json:"writer"`
	Timezone  string `json:"timezone"`
}

type APIError struct {
	Code int
	Msg  string
}

func (e *APIError) Error() string { return fmt.Sprintf("HTTP %d：%s", e.Code, e.Msg) }

func isAuthErr(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Code == 401
}

type Client struct {
	Base  string
	Token string
	HC    *http.Client
}

func newClient(base, token string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Token: token, HC: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) do(method, path string, body io.Reader, hdr map[string]string, out any) error {
	req, err := http.NewRequest(method, c.Base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "hl-agent/"+version)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.HC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(b, &env)
	if resp.StatusCode != 200 {
		msg := env.Error
		if msg == "" {
			msg = strings.TrimSpace(cut(string(b), 200))
		}
		return &APIError{resp.StatusCode, msg}
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("服务器返回的不是 JSON：%w", err)
		}
	}
	return nil
}

func (c *Client) postJSON(path string, v any, out any) error {
	b, _ := json.Marshal(v)
	return c.do("POST", path, bytes.NewReader(b), map[string]string{"Content-Type": "application/json"}, out)
}

func (c *Client) Pair(code, name, kind, host string) (string, DeviceInfo, Hints, error) {
	var out struct {
		Token  string     `json:"token"`
		Device DeviceInfo `json:"device"`
		Server Hints      `json:"server"`
	}
	err := c.postJSON("/api/v1/pair", M{"code": code, "name": name, "kind": kind, "host": host, "app_version": "hl-agent/" + version}, &out)
	if err == nil && out.Token == "" {
		err = errors.New("服务器没给口令")
	}
	return out.Token, out.Device, out.Server, err
}

func (c *Client) Device() (DeviceInfo, Hints, error) {
	var out struct {
		Device DeviceInfo `json:"device"`
		Server Hints      `json:"server"`
	}
	err := c.do("GET", "/api/v1/device", nil, nil, &out)
	return out.Device, out.Server, err
}

type PartResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error"`
	Status int    `json:"status"`
	Label  string `json:"label"`
	Cat    string `json:"cat"`
	Idle   bool   `json:"idle"`
}

func (c *Client) Report(body M) (map[string]PartResult, Hints, error) {
	var out struct {
		Results map[string]PartResult `json:"results"`
		Server  Hints                 `json:"server"`
	}
	err := c.postJSON("/api/v1/report", body, &out)
	return out.Results, out.Server, err
}

func (c *Client) UploadAW(raw []byte) (bool, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	var out struct {
		ReportExists bool `json:"report_exists"`
	}
	old := c.HC.Timeout
	c.HC.Timeout = 3 * time.Minute
	defer func() { c.HC.Timeout = old }()
	err := c.do("POST", "/api/v1/aw", &buf, map[string]string{"Content-Type": "application/json", "Content-Encoding": "gzip"}, &out)
	return out.ReportExists, err
}

func (c *Client) Unpair() error { return c.postJSON("/api/v1/unpair", M{}, nil) }
