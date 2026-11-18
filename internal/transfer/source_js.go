//go:build js && wasm

package transfer

import (
	"context"
	"errors"
	"syscall/js"
)

// bridge is the page-side helper that owns the File objects the user picked
// (see web/bridge.js). Go cannot hold a File, only ask it for bytes.
func bridge() js.Value { return js.Global().Get("chuanBridge") }

// available reports whether the page provides the file bridge.
func available() bool { return bridge().Type() == js.TypeObject }

// PickSources opens the page's file chooser and returns what the user
// picked.
func PickSources(ctx context.Context) ([]Source, error) {
	if !available() {
		return nil, errors.New("当前页面缺少文件选择组件")
	}
	res, err := await(bridge().Call("pickFiles"))
	if err != nil {
		return nil, err
	}
	n := res.Length()
	sources := make([]Source, 0, n)
	for i := 0; i < n; i++ {
		f := res.Index(i)
		sources = append(sources, &BridgeSource{
			id:   f.Get("id").String(),
			name: f.Get("name").String(),
			size: int64(f.Get("size").Float()),
			mime: f.Get("type").String(),
		})
	}
	return sources, nil
}

// BridgeSource is one File held by the page.
type BridgeSource struct {
	id   string
	name string
	size int64
	mime string
}

func (s *BridgeSource) Name() string { return s.name }

func (s *BridgeSource) Size() int64 { return s.size }

func (s *BridgeSource) Type() string {
	if s.mime != "" && s.mime != "application/octet-stream" {
		return s.mime
	}
	return mimeByName(s.name)
}

func (s *BridgeSource) ReadAt(offset int64, length int) ([]byte, error) {
	res, err := await(bridge().Call("read", s.id, offset, length))
	if err != nil {
		return nil, err
	}
	u8 := js.Global().Get("Uint8Array").New(res)
	buf := make([]byte, u8.Length())
	js.CopyBytesToGo(buf, u8)
	return buf, nil
}

func (s *BridgeSource) Close() error {
	if available() {
		bridge().Call("release", s.id)
	}
	return nil
}

// await blocks until the JavaScript promise settles.
func await(v js.Value) (js.Value, error) {
	if v.Type() != js.TypeObject || v.Get("then").Type() != js.TypeFunction {
		return v, nil
	}
	type result struct {
		v   js.Value
		err error
	}
	ch := make(chan result, 1)

	var then, catch js.Func
	then = js.FuncOf(func(_ js.Value, args []js.Value) any {
		var out js.Value
		if len(args) > 0 {
			out = args[0]
		}
		ch <- result{v: out}
		return nil
	})
	catch = js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "页面操作失败"
		if len(args) > 0 {
			if m := args[0].Get("message"); m.Type() == js.TypeString {
				msg = m.String()
			}
		}
		ch <- result{err: errors.New(msg)}
		return nil
	})

	v.Call("then", then, catch)
	res := <-ch
	then.Release()
	catch.Release()
	return res.v, res.err
}
