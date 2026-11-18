//go:build js && wasm

package netlink

import (
	"context"
	"errors"
	"sync"
	"syscall/js"
	"time"
)

// WS is the client's connection to the signaling server, backed by the
// browser's WebSocket.
type WS struct {
	sock  js.Value
	funcs []js.Func

	mu    sync.Mutex
	queue []Frame
	wake  chan struct{}
	done  chan struct{}
	once  sync.Once
	err   error
}

// Dial opens the WebSocket at rawURL and waits for the browser's open event.
func Dial(ctx context.Context, rawURL string) (*WS, error) {
	ctor := js.Global().Get("WebSocket")
	if ctor.Type() != js.TypeFunction {
		return nil, errors.New("当前环境没有 WebSocket")
	}

	w := &WS{
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	sock := ctor.New(rawURL)
	sock.Set("binaryType", "arraybuffer")
	w.sock = sock

	opened := make(chan struct{})
	var openOnce sync.Once

	onOpen := js.FuncOf(func(js.Value, []js.Value) any {
		openOnce.Do(func() { close(opened) })
		return nil
	})
	// Browsers report a failed handshake through the close event rather than
	// error, and give no reason, so the message stays general.
	onError := js.FuncOf(func(js.Value, []js.Value) any {
		w.fail(errors.New("无法连接信令服务器：地址不可达或服务未启动"))
		return nil
	})
	onClose := js.FuncOf(func(js.Value, []js.Value) any {
		w.fail(errors.New("信令连接已关闭"))
		return nil
	})
	onMessage := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		data := args[0].Get("data")
		if data.Type() == js.TypeString {
			w.push(Frame{Data: []byte(data.String())})
			return nil
		}
		u8 := js.Global().Get("Uint8Array").New(data)
		buf := make([]byte, u8.Length())
		js.CopyBytesToGo(buf, u8)
		w.push(Frame{Binary: true, Data: buf})
		return nil
	})

	w.funcs = append(w.funcs, onOpen, onError, onClose, onMessage)
	sock.Set("onopen", onOpen)
	sock.Set("onerror", onError)
	sock.Set("onclose", onClose)
	sock.Set("onmessage", onMessage)

	select {
	case <-opened:
		return w, nil
	case <-w.done:
		w.Close()
		return nil, w.failure()
	case <-ctx.Done():
		w.Close()
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		w.Close()
		return nil, errors.New("连接信令服务器超时")
	}
}

// push queues a frame. It never blocks: it runs inside the browser's event
// loop, which the whole runtime shares.
func (w *WS) push(f Frame) {
	w.mu.Lock()
	w.queue = append(w.queue, f)
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *WS) fail(err error) {
	w.once.Do(func() {
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
		close(w.done)
	})
}

func (w *WS) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return w.err
	}
	return errors.New("信令连接已关闭")
}

// SendJSON writes v as a text frame.
func (w *WS) SendJSON(v any) error {
	data, err := EncodeJSON(v)
	if err != nil {
		return err
	}
	return w.sendString(string(data))
}

func (w *WS) sendString(s string) error {
	if rs := w.sock.Get("readyState"); rs.Int() != 1 {
		return errors.New("信令连接未就绪")
	}
	w.sock.Call("send", s)
	return nil
}

// SendBinary writes a binary frame.
func (w *WS) SendBinary(data []byte) error {
	if rs := w.sock.Get("readyState"); rs.Int() != 1 {
		return errors.New("信令连接未就绪")
	}
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	w.sock.Call("send", u8)
	return nil
}

// Recv returns the next frame, blocking until one arrives or the connection
// fails.
func (w *WS) Recv() (Frame, error) {
	for {
		w.mu.Lock()
		if len(w.queue) > 0 {
			f := w.queue[0]
			w.queue = w.queue[1:]
			w.mu.Unlock()
			return f, nil
		}
		w.mu.Unlock()

		select {
		case <-w.wake:
		case <-w.done:
			w.mu.Lock()
			empty := len(w.queue) == 0
			w.mu.Unlock()
			if empty {
				return Frame{}, w.failure()
			}
		}
	}
}

// Close shuts the connection down and releases the JavaScript callbacks.
func (w *WS) Close() error {
	defer func() { _ = recover() }()
	if w.sock.Type() == js.TypeObject && w.sock.Get("readyState").Int() < 2 {
		w.sock.Call("close")
	}
	w.fail(errors.New("信令连接已关闭"))
	for _, f := range w.funcs {
		f.Release()
	}
	w.funcs = nil
	return nil
}
