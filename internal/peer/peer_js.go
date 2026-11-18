//go:build js && wasm

package peer

import (
	"encoding/json"
	"errors"
	"sync"
	"syscall/js"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// Peer is one end of a WebRTC connection in the browser, backed by the
// page's RTCPeerConnection. It exposes the same methods as the desktop
// build, so the session code above it is the same on both.
type Peer struct {
	cfg Config
	pc  js.Value

	mu        sync.Mutex
	dc        js.Value
	remoteSet bool
	pending   []protocol.ICECandidate
	closed    bool
	closeOnce sync.Once

	funcs []js.Func
}

// New builds the connection. The sender creates the data channel right away.
func New(cfg Config) (*Peer, error) {
	if !cfg.Role.Valid() {
		return nil, errors.New("未知角色")
	}
	ctor := js.Global().Get("RTCPeerConnection")
	if ctor.Type() != js.TypeFunction {
		return nil, errors.New("当前环境没有 WebRTC")
	}

	p := &Peer{cfg: cfg}

	servers := cfg.ICEServers
	if len(servers) == 0 {
		servers = DefaultICEServers()
	}
	iceList := js.Global().Get("Array").New()
	for _, s := range servers {
		urls := js.Global().Get("Array").New()
		for _, u := range s.URLs {
			urls.Call("push", u)
		}
		entry := js.Global().Get("Object").New()
		entry.Set("urls", urls)
		if s.Username != "" {
			entry.Set("username", s.Username)
			entry.Set("credential", s.Credential)
		}
		iceList.Call("push", entry)
	}
	conf := js.Global().Get("Object").New()
	conf.Set("iceServers", iceList)
	p.pc = ctor.New(conf)

	onICE := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || p.cfg.OnICE == nil {
			return nil
		}
		cand := args[0].Get("candidate")
		if cand.Type() != js.TypeObject {
			return nil
		}
		ice := protocol.ICECandidate{Candidate: cand.Get("candidate").String()}
		if mid := cand.Get("sdpMid"); mid.Type() == js.TypeString {
			v := mid.String()
			ice.SDPMid = &v
		}
		if idx := cand.Get("sdpMLineIndex"); idx.Type() == js.TypeNumber {
			v := uint16(idx.Int())
			ice.SDPMLineIndex = &v
		}
		if uf := cand.Get("usernameFragment"); uf.Type() == js.TypeString {
			v := uf.String()
			ice.UsernameFragment = &v
		}
		p.cfg.OnICE(ice)
		return nil
	})
	p.funcs = append(p.funcs, onICE)
	p.pc.Set("onicecandidate", onICE)

	onState := js.FuncOf(func(js.Value, []js.Value) any {
		switch p.pc.Get("connectionState").String() {
		case "failed", "closed":
			p.notifyClose()
		}
		return nil
	})
	p.funcs = append(p.funcs, onState)
	p.pc.Set("onconnectionstatechange", onState)

	if cfg.Role == protocol.RoleSender {
		init := js.Global().Get("Object").New()
		init.Set("ordered", true)
		p.attach(p.pc.Call("createDataChannel", protocol.DataChannelLabel, init))
	} else {
		onChannel := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			ev := args[0]
			p.attach(ev.Get("channel"))
			return nil
		})
		p.funcs = append(p.funcs, onChannel)
		p.pc.Set("ondatachannel", onChannel)
	}

	return p, nil
}

// attach wires one data channel's events to the configuration.
func (p *Peer) attach(dc js.Value) {
	dc.Set("binaryType", "arraybuffer")

	onOpen := js.FuncOf(func(js.Value, []js.Value) any {
		if p.cfg.OnOpen != nil {
			p.cfg.OnOpen()
		}
		return nil
	})
	onMessage := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || p.cfg.OnMessage == nil {
			return nil
		}
		data := args[0].Get("data")
		if data.Type() == js.TypeString {
			p.cfg.OnMessage(false, []byte(data.String()))
			return nil
		}
		u8 := js.Global().Get("Uint8Array").New(data)
		buf := make([]byte, u8.Length())
		js.CopyBytesToGo(buf, u8)
		p.cfg.OnMessage(true, buf)
		return nil
	})
	onClose := js.FuncOf(func(js.Value, []js.Value) any {
		p.notifyClose()
		return nil
	})
	p.funcs = append(p.funcs, onOpen, onMessage, onClose)
	dc.Set("onopen", onOpen)
	dc.Set("onmessage", onMessage)
	dc.Set("onclose", onClose)

	p.mu.Lock()
	p.dc = dc
	p.mu.Unlock()
}

func (p *Peer) notifyClose() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		p.cfg.notifyClose()
	})
}

// CreateOffer starts negotiation: the sender's first move.
func (p *Peer) CreateOffer() (string, error) {
	offer, err := await(p.pc.Call("createOffer"))
	if err != nil {
		return "", err
	}
	if _, err := await(p.pc.Call("setLocalDescription", offer)); err != nil {
		return "", err
	}
	return p.pc.Get("localDescription").Get("sdp").String(), nil
}

// Accept applies the sender's offer and returns this side's answer.
func (p *Peer) Accept(sdp string) (string, error) {
	desc := js.Global().Get("Object").New()
	desc.Set("type", "offer")
	desc.Set("sdp", sdp)
	if _, err := await(p.pc.Call("setRemoteDescription", desc)); err != nil {
		return "", err
	}
	answer, err := await(p.pc.Call("createAnswer"))
	if err != nil {
		return "", err
	}
	if _, err := await(p.pc.Call("setLocalDescription", answer)); err != nil {
		return "", err
	}
	p.remoteDescriptionSet()
	return answer.Get("sdp").String(), nil
}

// SetAnswer applies the receiver's answer.
func (p *Peer) SetAnswer(sdp string) error {
	desc := js.Global().Get("Object").New()
	desc.Set("type", "answer")
	desc.Set("sdp", sdp)
	if _, err := await(p.pc.Call("setRemoteDescription", desc)); err != nil {
		return err
	}
	p.remoteDescriptionSet()
	return nil
}

func (p *Peer) remoteDescriptionSet() {
	p.mu.Lock()
	p.remoteSet = true
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()

	for _, c := range pending {
		_ = p.AddICECandidate(c)
	}
}

// AddICECandidate applies one candidate from the peer, buffering it until
// the remote description is in place.
func (p *Peer) AddICECandidate(c protocol.ICECandidate) error {
	p.mu.Lock()
	ready := p.remoteSet
	if !ready {
		p.pending = append(p.pending, c)
	}
	p.mu.Unlock()
	if !ready {
		return nil
	}

	obj := js.Global().Get("Object").New()
	obj.Set("candidate", c.Candidate)
	if c.SDPMid != nil {
		obj.Set("sdpMid", *c.SDPMid)
	} else {
		obj.Set("sdpMid", js.Null())
	}
	if c.SDPMLineIndex != nil {
		obj.Set("sdpMLineIndex", int(*c.SDPMLineIndex))
	}
	if c.UsernameFragment != nil {
		obj.Set("usernameFragment", *c.UsernameFragment)
	}
	_, err := await(p.pc.Call("addIceCandidate", obj))
	return err
}

// SendJSON writes one control message as a text frame.
func (p *Peer) SendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return p.SendText(string(data))
}

// SendText writes a text frame.
func (p *Peer) SendText(s string) error {
	dc := p.channel()
	if dc.IsUndefined() || dc.IsNull() {
		return errors.New("数据通道未就绪")
	}
	dc.Call("send", s)
	return nil
}

// SendBinary writes a binary frame carrying file bytes.
func (p *Peer) SendBinary(data []byte) error {
	dc := p.channel()
	if dc.IsUndefined() || dc.IsNull() {
		return errors.New("数据通道未就绪")
	}
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	dc.Call("send", u8)
	return nil
}

// BufferedAmount is how many bytes are queued and not yet sent.
func (p *Peer) BufferedAmount() uint64 {
	dc := p.channel()
	if dc.IsUndefined() || dc.IsNull() {
		return 0
	}
	return uint64(dc.Get("bufferedAmount").Float())
}

// Open reports whether the data channel is open.
func (p *Peer) Open() bool {
	dc := p.channel()
	if dc.IsUndefined() || dc.IsNull() {
		return false
	}
	return dc.Get("readyState").String() == "open"
}

func (p *Peer) channel() js.Value {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dc
}

// Close tears the connection down.
func (p *Peer) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	dc := p.dc
	funcs := p.funcs
	p.funcs = nil
	p.mu.Unlock()

	if !dc.IsUndefined() && !dc.IsNull() {
		dc.Set("onopen", js.Null())
		dc.Set("onmessage", js.Null())
		dc.Set("onclose", js.Null())
	}
	p.pc.Set("onicecandidate", js.Null())
	p.pc.Set("ondatachannel", js.Null())
	p.pc.Set("onconnectionstatechange", js.Null())
	p.pc.Call("close")

	for _, f := range funcs {
		f.Release()
	}

	p.cfg.notifyClose()
	return nil
}

// await blocks until the JavaScript promise settles.
func await(v js.Value) (js.Value, error) {
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
		msg := "浏览器拒绝了 WebRTC 请求"
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
