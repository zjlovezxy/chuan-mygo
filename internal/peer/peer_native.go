//go:build !js

package peer

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/pion/webrtc/v4"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// Peer is one end of a WebRTC connection carrying a single ordered data
// channel, as the browser client does.
type Peer struct {
	cfg Config
	pc  *webrtc.PeerConnection

	mu         sync.Mutex
	dc         *webrtc.DataChannel
	remoteSet  bool
	pendingICE []webrtc.ICECandidateInit

	closed    bool
	closeOnce sync.Once
}

// New builds the connection. The sender creates the data channel right away,
// so that its offer carries the channel's SCTP stream.
func New(cfg Config) (*Peer, error) {
	if !cfg.Role.Valid() {
		return nil, errors.New("未知角色")
	}
	servers := cfg.ICEServers
	if len(servers) == 0 {
		servers = DefaultICEServers()
	}
	conf := webrtc.Configuration{}
	for _, s := range servers {
		conf.ICEServers = append(conf.ICEServers, webrtc.ICEServer{
			URLs:       s.URLs,
			Username:   s.Username,
			Credential: s.Credential,
		})
	}

	pc, err := webrtc.NewPeerConnection(conf)
	if err != nil {
		return nil, err
	}

	p := &Peer{cfg: cfg, pc: pc}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil || p.cfg.OnICE == nil {
			return
		}
		init := c.ToJSON()
		p.cfg.OnICE(protocol.ICECandidate{
			Candidate:        init.Candidate,
			SDPMid:           init.SDPMid,
			SDPMLineIndex:    init.SDPMLineIndex,
			UsernameFragment: init.UsernameFragment,
		})
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		switch s {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			p.notifyClose()
		}
	})

	if cfg.Role == protocol.RoleSender {
		dc, err := pc.CreateDataChannel(protocol.DataChannelLabel, nil)
		if err != nil {
			_ = pc.Close()
			return nil, err
		}
		p.attach(dc)
	} else {
		pc.OnDataChannel(func(dc *webrtc.DataChannel) { p.attach(dc) })
	}

	return p, nil
}

// attach wires one data channel's events to the configuration.
func (p *Peer) attach(dc *webrtc.DataChannel) {
	p.mu.Lock()
	p.dc = dc
	p.mu.Unlock()

	dc.OnOpen(func() {
		if p.cfg.OnOpen != nil {
			p.cfg.OnOpen()
		}
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if p.cfg.OnMessage != nil {
			p.cfg.OnMessage(!msg.IsString, msg.Data)
		}
	})
	dc.OnClose(func() { p.notifyClose() })
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
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return "", err
	}
	if err := p.pc.SetLocalDescription(offer); err != nil {
		return "", err
	}
	return offer.SDP, nil
}

// Accept applies the sender's offer and returns this side's answer.
func (p *Peer) Accept(sdp string) (string, error) {
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		return "", err
	}
	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return "", err
	}
	if err := p.pc.SetLocalDescription(answer); err != nil {
		return "", err
	}
	p.remoteDescriptionSet()
	return answer.SDP, nil
}

// SetAnswer applies the receiver's answer.
func (p *Peer) SetAnswer(sdp string) error {
	if err := p.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		return err
	}
	p.remoteDescriptionSet()
	return nil
}

// remoteDescriptionSet flushes the candidates that arrived early.
func (p *Peer) remoteDescriptionSet() {
	p.mu.Lock()
	p.remoteSet = true
	pending := p.pendingICE
	p.pendingICE = nil
	p.mu.Unlock()

	for _, c := range pending {
		_ = p.pc.AddICECandidate(c)
	}
}

// AddICECandidate applies one candidate from the peer, buffering it until
// the remote description is in place.
func (p *Peer) AddICECandidate(c protocol.ICECandidate) error {
	init := webrtc.ICECandidateInit{
		Candidate:        c.Candidate,
		SDPMid:           c.SDPMid,
		SDPMLineIndex:    c.SDPMLineIndex,
		UsernameFragment: c.UsernameFragment,
	}

	p.mu.Lock()
	ready := p.remoteSet
	if !ready {
		p.pendingICE = append(p.pendingICE, init)
	}
	p.mu.Unlock()

	if !ready {
		return nil
	}
	return p.pc.AddICECandidate(init)
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
	if dc == nil {
		return errors.New("数据通道未就绪")
	}
	return dc.SendText(s)
}

// SendBinary writes a binary frame carrying file bytes.
func (p *Peer) SendBinary(data []byte) error {
	dc := p.channel()
	if dc == nil {
		return errors.New("数据通道未就绪")
	}
	return dc.Send(data)
}

// BufferedAmount is how many bytes are queued and not yet sent: the sender's
// backpressure signal.
func (p *Peer) BufferedAmount() uint64 {
	dc := p.channel()
	if dc == nil {
		return 0
	}
	return dc.BufferedAmount()
}

// Open reports whether the data channel is open.
func (p *Peer) Open() bool {
	dc := p.channel()
	return dc != nil && dc.ReadyState() == webrtc.DataChannelStateOpen
}

func (p *Peer) channel() *webrtc.DataChannel {
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
	p.mu.Unlock()

	err := p.pc.Close()
	p.notifyClose()
	return err
}
