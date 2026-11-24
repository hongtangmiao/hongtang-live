package main

import (
	"bufio"
	"bytes"
	"net"
	"net/url"
	"strings"
	"time"
)

/*
 * Go 的 http 库遇到坏的百分号编码（比如 /%E0%A4%A），在进 handler 之前就回一个纯文本 400。
 * 旧版回的是 JSON 400，前端和监控都按 JSON 处理，所以在连接层先看一眼请求行。
 * 只处理每个连接的第一个请求（浏览器对这种地址不会复用连接），其余原样交给 http.Server。
 */

type badURLListener struct{ net.Listener }

func (l badURLListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &peekConn{Conn: c, br: bufio.NewReaderSize(c, 4096)}, nil
}

type peekConn struct {
	net.Conn
	br      *bufio.Reader
	checked bool
}

func (c *peekConn) Read(p []byte) (int, error) {
	if !c.checked {
		c.checked = true
		_ = c.Conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		line, err := c.br.Peek(c.br.Buffered())
		if len(line) == 0 || bytes.IndexByte(line, '\n') < 0 {
			if _, e := c.br.Peek(1); e == nil { // 至少等到第一批数据
				line, _ = c.br.Peek(c.br.Buffered())
			}
		}
		_ = c.Conn.SetReadDeadline(time.Time{})
		if i := bytes.IndexByte(line, '\n'); i > 0 {
			f := strings.Fields(string(line[:i]))
			if len(f) >= 2 && strings.HasPrefix(f[1], "/") {
				path := f[1]
				if q := strings.IndexByte(path, '?'); q >= 0 {
					path = path[:q]
				}
				if _, e := url.PathUnescape(path); e != nil {
					body := []byte(`{"ok":false,"error":"bad request"}`)
					_, _ = c.Conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\n" +
						"Content-Type: application/json; charset=utf-8\r\nConnection: close\r\nContent-Length: " +
						itoa(int64(len(body))) + "\r\n\r\n"))
					_, _ = c.Conn.Write(body)
					_ = c.Conn.Close()
					return 0, net.ErrClosed
				}
			}
		}
		_ = err
	}
	return c.br.Read(p)
}
