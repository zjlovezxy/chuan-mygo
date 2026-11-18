// Package session runs one side of a transfer room: it holds the signaling
// WebSocket, negotiates the WebRTC connection, and hands the data channel to
// the transfer layer.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/zjlovezxy/chuan-mygo/internal/netlink"
	"github.com/zjlovezxy/chuan-mygo/internal/peer"
	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// State is where a session is in its life.
type State string

const (
	// StateSignaling: dialing the signaling server.
	StateSignaling State = "signaling"
	// StateWaiting: in the room, waiting for the other side.
	StateWaiting State = "waiting"
	// StateConnecting: both sides in the room, negotiating the P2P link.
	StateConnecting State = "connecting"
	// StateConnected: the data channel is open.
	StateConnected State = "connected"
	// StateClosed: closed by either side.
	StateClosed State = "closed"
	// StateFailed: the session broke and will not recover.
	StateFailed State = "failed"
)

// Events are the callbacks a UI or the transfer layer subscribes to. Every
// callback may be nil, and is called from the session's goroutines.
type Events struct {
	// State fires on every state change, with a line for the user.
	State func(State, string)
	// Log receives diagnostic lines.
	Log func(string)
	// Open fires when the data channel is ready.
	Open func()
	// Close fires once when the channel or the connection goes away.
	Close func()
	// Message receives every control message from the peer.
	Message func(protocol.Message)
	// Binary receives every binary frame from the peer.
	Binary func([]byte)
	// Error receives errors worth showing.
	Error func(error)
}

func (e Events) state(s State, detail string) {
	if e.State != nil {
		e.State(s, detail)
	}
}

func (e Events) log(format string, args ...any) {
	if e.Log != nil {
		e.Log(fmt.Sprintf(format, args...))
	}
}

func (e Events) fail(err error) {
	if err != nil && e.Error != nil {
		e.Error(err)
	}
}

// Config describes the room to join.
type Config struct {
	Server string
	Code   string
	Role   protocol.Role
	// ICEServers default to the public STUN servers.
	ICEServers []peer.ICEServer
	Events     Events
}

// Session is one side of a room.
type Session struct {
	cfg Config

	mu     sync.Mutex
	state  State
	ws     *netlink.WS
	pr     *peer.Peer
	closer context.CancelFunc
	done   chan struct{}
	closed bool
}

// New prepares a session. Call Start to join the room.
func New(cfg Config) *Session {
	return &Session{cfg: cfg, state: StateSignaling, done: make(chan struct{})}
}

// Role is this side's role in the room.
func (s *Session) Role() protocol.Role {
	return s.cfg.Role
}

// State is the current state.
func (s *Session) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Session) setState(state State, detail string) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	s.cfg.Events.state(state, detail)
}

// Start joins the room: it dials the signaling server and starts the
// negotiation loop.
func (s *Session) Start(parent context.Context) error {
	if !s.cfg.Role.Valid() {
		return errors.New("未知角色")
	}
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	if s.ws != nil {
		s.mu.Unlock()
		cancel()
		return errors.New("会话已启动")
	}
	s.closer = cancel
	s.mu.Unlock()

	url, err := netlink.JoinURL(s.cfg.Server, "/api/ws/webrtc", s.cfg.Code, string(s.cfg.Role))
	if err != nil {
		cancel()
		s.setState(StateFailed, err.Error())
		return err
	}

	s.setState(StateSignaling, "正在连接信令服务器…")
	s.cfg.Events.log("信令服务器：%s", url)

	ws, err := netlink.Dial(ctx, url)
	if err != nil {
		cancel()
		s.setState(StateFailed, err.Error())
		return err
	}

	s.mu.Lock()
	s.ws = ws
	s.mu.Unlock()

	s.setState(StateWaiting, "已进入房间，等待对方加入…")
	go s.readLoop(ctx, ws)
	return nil
}

// readLoop forwards signaling messages to the negotiation.
func (s *Session) readLoop(ctx context.Context, ws *netlink.WS) {
	defer func() {
		close(s.done)
		s.setState(StateClosed, "连接已结束")
	}()

	for {
		frame, err := ws.Recv()
		if err != nil {
			if ctx.Err() == nil {
				s.cfg.Events.fail(fmt.Errorf("信令连接中断：%w", err))
			}
			return
		}
		if frame.Binary {
			continue
		}
		var msg protocol.Signal
		if err := json.Unmarshal(frame.Data, &msg); err != nil {
			s.cfg.Events.log("忽略无法解析的信令：%s", string(frame.Data))
			continue
		}
		s.handleSignal(ctx, ws, msg)
	}
}

func (s *Session) handleSignal(ctx context.Context, ws *netlink.WS, msg protocol.Signal) {
	switch msg.Type {
	case protocol.SignalPeerJoined:
		var joined protocol.PeerJoined
		_ = json.Unmarshal(msg.Payload, &joined)
		s.cfg.Events.log("对方已加入房间（%s）", joined.Role)
		s.setState(StateConnecting, "对方已进入房间，正在建立 P2P 连接…")
		if s.cfg.Role == protocol.RoleSender {
			p, err := s.ensurePeer()
			if err != nil {
				s.cfg.Events.fail(err)
				return
			}
			sdp, err := p.CreateOffer()
			if err != nil {
				s.cfg.Events.fail(fmt.Errorf("创建 offer 失败：%w", err))
				return
			}
			s.sendSignal(ws, protocol.SignalOffer, protocol.Description{Type: "offer", SDP: sdp})
		} else {
			if _, err := s.ensurePeer(); err != nil {
				s.cfg.Events.fail(err)
			}
		}

	case protocol.SignalOffer:
		var desc protocol.Description
		if err := json.Unmarshal(msg.Payload, &desc); err != nil {
			s.cfg.Events.log("offer 解析失败：%v", err)
			return
		}
		p, err := s.ensurePeer()
		if err != nil {
			s.cfg.Events.fail(err)
			return
		}
		answer, err := p.Accept(desc.SDP)
		if err != nil {
			s.cfg.Events.fail(fmt.Errorf("处理 offer 失败：%w", err))
			return
		}
		s.sendSignal(ws, protocol.SignalAnswer, protocol.Description{Type: "answer", SDP: answer})

	case protocol.SignalAnswer:
		var desc protocol.Description
		if err := json.Unmarshal(msg.Payload, &desc); err != nil {
			s.cfg.Events.log("answer 解析失败：%v", err)
			return
		}
		s.mu.Lock()
		p := s.pr
		s.mu.Unlock()
		if p == nil {
			s.cfg.Events.fail(errors.New("收到 answer 但连接尚未创建"))
			return
		}
		if err := p.SetAnswer(desc.SDP); err != nil {
			s.cfg.Events.fail(fmt.Errorf("处理 answer 失败：%w", err))
		}

	case protocol.SignalICECandidate:
		var cand protocol.ICECandidate
		if err := json.Unmarshal(msg.Payload, &cand); err != nil {
			return
		}
		if cand.Candidate == "" {
			return
		}
		s.mu.Lock()
		p := s.pr
		s.mu.Unlock()
		if p == nil {
			return
		}
		if err := p.AddICECandidate(cand); err != nil {
			s.cfg.Events.log("加入 ICE 候选失败：%v", err)
		}

	case protocol.SignalDisconnect:
		s.cfg.Events.log("对方主动断开了连接")
		s.cfg.Events.state(StateClosed, "对方主动断开了连接")

	case protocol.SignalPeerLeft:
		s.cfg.Events.log("对方已离开房间")
		s.cfg.Events.state(StateClosed, "对方已离开房间")

	case protocol.SignalError:
		reason := msg.Message
		if reason == "" {
			reason = "服务器报告了一个错误"
		}
		s.cfg.Events.fail(errors.New(reason))
		s.setState(StateFailed, reason)

	case protocol.SignalSyncRequest:
		// The peer asks for the current file list; the transfer layer
		// answers through OnSync deeplink when it has one. Nothing to do
		// here: it re-announces its own files when it connects.

	case protocol.SignalRelayRequest:
		s.cfg.Events.log("对方请求走服务器中继（当前版本暂不支持自动中继）")

	default:
		s.cfg.Events.log("收到未知信令：%s", msg.Type)
	}
}

// ensurePeer creates this side's PeerConnection once.
func (s *Session) ensurePeer() (*peer.Peer, error) {
	s.mu.Lock()
	if s.pr != nil {
		p := s.pr
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	cfg := peer.Config{
		Role:       s.cfg.Role,
		ICEServers: s.cfg.ICEServers,
		OnICE: func(c protocol.ICECandidate) {
			s.mu.Lock()
			ws := s.ws
			s.mu.Unlock()
			if ws == nil {
				return
			}
			s.sendSignal(ws, protocol.SignalICECandidate, c)
		},
		OnOpen: func() {
			s.setState(StateConnected, "P2P 已连通，可以开始传输")
			s.cfg.Events.log("数据通道已打开")
			if s.cfg.Events.Open != nil {
				s.cfg.Events.Open()
			}
		},
		OnMessage: func(binary bool, data []byte) {
			if binary {
				if s.cfg.Events.Binary != nil {
					s.cfg.Events.Binary(data)
				}
				return
			}
			var msg protocol.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				s.cfg.Events.log("忽略无法解析的消息：%s", string(data))
				return
			}
			if s.cfg.Events.Message != nil {
				s.cfg.Events.Message(msg)
			}
		},
		OnClose: func() {
			s.setState(StateClosed, "P2P 连接已断开")
			if s.cfg.Events.Close != nil {
				s.cfg.Events.Close()
			}
		},
		OnError: func(err error) { s.cfg.Events.fail(err) },
	}

	p, err := peer.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("建立 P2P 连接失败：%w", err)
	}

	s.mu.Lock()
	s.pr = p
	s.mu.Unlock()
	return p, nil
}

func (s *Session) sendSignal(ws *netlink.WS, typ string, payload any) {
	sig, err := protocol.NewSignal(typ, payload)
	if err != nil {
		s.cfg.Events.log("构造信令失败：%v", err)
		return
	}
	if err := ws.SendJSON(sig); err != nil {
		s.cfg.Events.log("发送信令失败：%v", err)
	}
}

// Send writes a control message over the data channel.
func (s *Session) Send(msg protocol.Message) error {
	s.mu.Lock()
	p := s.pr
	s.mu.Unlock()
	if p == nil {
		return errors.New("连接尚未建立")
	}
	return p.SendJSON(msg)
}

// SendBinary writes one binary frame over the data channel.
func (s *Session) SendBinary(data []byte) error {
	s.mu.Lock()
	p := s.pr
	s.mu.Unlock()
	if p == nil {
		return errors.New("连接尚未建立")
	}
	return p.SendBinary(data)
}

// BufferedAmount is the data channel's outgoing queue length.
func (s *Session) BufferedAmount() uint64 {
	s.mu.Lock()
	p := s.pr
	s.mu.Unlock()
	if p == nil {
		return 0
	}
	return p.BufferedAmount()
}

// Connected reports whether the data channel is open.
func (s *Session) Connected() bool {
	s.mu.Lock()
	p := s.pr
	s.mu.Unlock()
	return p != nil && p.Open()
}

// Close tells the peer we are leaving and tears everything down.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ws, p, cancel := s.ws, s.pr, s.closer
	s.mu.Unlock()

	if ws != nil {
		sig, _ := protocol.NewSignal(protocol.SignalDisconnect, map[string]string{"reason": "用户主动断开"})
		_ = ws.SendJSON(sig)
		_ = ws.Close()
	}
	if p != nil {
		_ = p.Close()
	}
	if cancel != nil {
		cancel()
	}
	s.setState(StateClosed, "已断开连接")
	return nil
}
