//go:build js && wasm

package transfer

import (
	"errors"
	"time"

	"syscall/js"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// BrowserSink collects a file in memory and hands it to the browser's
// download manager when it is complete. A page cannot write to the disk
// itself, so this is the only destination the WASM build offers.
type BrowserSink struct {
	// MaxSize caps a single file, in bytes. Buffering in memory is the
	// constraint, not the transfer protocol.
	MaxSize int64
}

// NewBrowserSink builds a sink with a 2 GiB cap.
func NewBrowserSink() *BrowserSink {
	return &BrowserSink{MaxSize: 2 << 30}
}

// Open reserves the buffer for one file.
func (s *BrowserSink) Open(meta protocol.FileMetadata) (Receiver, error) {
	if meta.Size < 0 {
		return nil, errors.New("对方发来的文件大小无效")
	}
	limit := s.MaxSize
	if limit <= 0 {
		limit = 2 << 30
	}
	if meta.Size > limit {
		return nil, errors.New("文件超过 2GB，浏览器版本无法接收")
	}
	return &browserReceiver{meta: meta, buf: make([]byte, meta.Size)}, nil
}

type browserReceiver struct {
	meta    protocol.FileMetadata
	buf     []byte
	written int
}

func (r *browserReceiver) Write(index int, data []byte) error {
	off := index * protocol.ChunkSize
	if off < 0 || off+len(data) > len(r.buf) {
		return errors.New("文件块与元信息声明的大小不一致")
	}
	copy(r.buf[off:], data)
	if end := off + len(data); end > r.written {
		r.written = end
	}
	return nil
}

// Commit turns the buffer into a Blob and clicks a download link.
func (r *browserReceiver) Commit() (string, error) {
	if r.buf == nil {
		return "", errors.New("没有可保存的数据")
	}
	name := SanitizeName(r.meta.Name)

	u8 := js.Global().Get("Uint8Array").New(r.written)
	js.CopyBytesToJS(u8, r.buf[:r.written])

	parts := js.Global().Get("Array").New()
	parts.Call("push", u8)

	opts := js.Global().Get("Object").New()
	if r.meta.Type != "" {
		opts.Set("type", r.meta.Type)
	}
	blob := js.Global().Get("Blob").New(parts, opts)

	url := js.Global().Get("URL").Call("createObjectURL", blob)

	// A host that cannot use the download manager (the Android shell writes
	// into its own downloads directory) takes the blob itself.
	if bridge := js.Global().Get("chuanBridge"); bridge.Type() == js.TypeObject {
		if save := bridge.Get("saveBlob"); save.Type() == js.TypeFunction {
			if save.Invoke(url, name).Bool() {
				r.buf = nil
				return name, nil
			}
		}
	}

	doc := js.Global().Get("document")
	link := doc.Call("createElement", "a")
	link.Set("href", url)
	link.Set("download", name)
	link.Set("style", "display:none")
	doc.Get("body").Call("appendChild", link)
	link.Call("click")
	link.Call("remove")

	go func() {
		time.Sleep(10 * time.Second)
		js.Global().Get("URL").Call("revokeObjectURL", url)
	}()

	r.buf = nil
	return name, nil
}

func (r *browserReceiver) Abort() {
	r.buf = nil
}
