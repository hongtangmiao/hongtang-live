package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sync"
)

/* 托盘图标：夜蓝底、白框、像素爱心（金 = 正常，灰 = 暂停 / 没配对，红 = 出错），和网站一个风格 */

var heart = []string{
	".XX.XX.",
	"XXXXXXX",
	"XXXXXXX",
	".XXXXX.",
	"..XXX..",
	"...X...",
}

var (
	iconMu    sync.Mutex
	iconCache = map[string][]byte{}
)

func hexColor(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

func drawIcon(state string, size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg, line := hexColor(0x0c, 0x15, 0x33), hexColor(0xf4, 0xf4, 0xf0)
	fg := hexColor(0xff, 0xd2, 0x3f)
	switch state {
	case "pause", "off":
		fg = hexColor(0x7c, 0x86, 0xb0)
	case "err":
		fg = hexColor(0xff, 0x7a, 0x6b)
	}
	b := size / 16
	if b < 1 {
		b = 1
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := bg
			if x < b || y < b || x >= size-b || y >= size-b {
				c = line
			}
			img.Set(x, y, c)
		}
	}
	scale := (size - 4*b) / 8
	if scale < 1 {
		scale = 1
	}
	w, h := len(heart[0])*scale, len(heart)*scale
	ox, oy := (size-w)/2, (size-h)/2+scale/3
	for ry, row := range heart {
		for rx, ch := range row {
			if ch != 'X' {
				continue
			}
			if state == "off" && !(ry == 0 || rx == 0 || rx == len(row)-1 || heartEdge(rx, ry)) {
				continue // 没配对：只画轮廓
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set(ox+rx*scale+dx, oy+ry*scale+dy, fg)
				}
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func heartEdge(x, y int) bool {
	at := func(x, y int) bool {
		return y >= 0 && y < len(heart) && x >= 0 && x < len(heart[0]) && heart[y][x] == 'X'
	}
	return !at(x-1, y) || !at(x+1, y) || !at(x, y-1) || !at(x, y+1)
}

// pngToICO：Vista 以后的 ICO 可以直接装 PNG
func pngToICO(p []byte, size int) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, []uint16{0, 1, 1})
	s := byte(size)
	if size >= 256 {
		s = 0
	}
	buf.Write([]byte{s, s, 0, 0})
	_ = binary.Write(&buf, binary.LittleEndian, []uint16{1, 32})
	_ = binary.Write(&buf, binary.LittleEndian, []uint32{uint32(len(p)), 22})
	buf.Write(p)
	return buf.Bytes()
}

func iconFor(state string) []byte {
	iconMu.Lock()
	defer iconMu.Unlock()
	if b, ok := iconCache[state]; ok {
		return b
	}
	var b []byte
	if runtime.GOOS == "windows" {
		b = pngToICO(drawIcon(state, 32), 32)
	} else {
		b = drawIcon(state, 64)
	}
	iconCache[state] = b
	return b
}
