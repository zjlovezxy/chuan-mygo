// Package server is a signalling server compatible with chuan's public
// server: same room endpoints, same WebSocket paths, same message shapes.
// Files never pass through it — only SDP/ICE and, when peers cannot reach
// each other, relayed bytes.
package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// Options tunes the server.
type Options struct {
	// RoomTTL is how long a room stays joinable after creation.
	RoomTTL time.Duration
	// StaticDir, when set, is served at / for the browser build.
	StaticDir string
}

// Server holds every live room.
type Server struct {
	opts     Options
	mu       sync.Mutex
	rooms    map[string]*room
	upgrader websocket.Upgrader

	relayOnce  sync.Once
	relayRooms *relayState
}

type room struct {
	code    string
	created time.Time
	peers   map[protocol.Role]*client
}

type client struct {
	id     string
	role   protocol.Role
	conn   *websocket.Conn
	writeM sync.Mutex
}

// New builds a server. Pass a zero Options for defaults (1 hour rooms).
func New(opts Options) *Server {
	if opts.RoomTTL <= 0 {
		opts.RoomTTL = time.Hour
	}
	s := &Server{
		opts:  opts,
		rooms: make(map[string]*room),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(*http.Request) bool { return true },
		},
	}
	go s.cleanupLoop()
	return s
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/create-room", s.handleCreateRoom)
	mux.HandleFunc("/api/room-info", s.handleRoomInfo)
	mux.HandleFunc("/api/webrtc-room-status", s.handleRoomInfo)
	mux.HandleFunc("/api/ws/webrtc", s.handleSignal)
	mux.HandleFunc("/ws/webrtc", s.handleSignal)
	mux.HandleFunc("/api/ws/relay", s.handleRelay)
	mux.HandleFunc("/ws/relay", s.handleRelay)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "rooms": s.RoomCount()})
	})
	if s.opts.StaticDir != "" {
		fileServer := http.FileServer(http.Dir(s.opts.StaticDir))
		mux.Handle("/", fileServer)
	}
	return mux
}

// RoomCount is how many rooms are alive.
func (s *Server) RoomCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rooms)
}

// NewRoom creates a room with a fresh six character code.
func (s *Server) NewRoom() (string, error) {
	for attempt := 0; attempt < 20; attempt++ {
		code, err := newCode()
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		if _, taken := s.rooms[code]; !taken {
			s.rooms[code] = &room{code: code, created: time.Now(), peers: map[protocol.Role]*client{}}
			s.mu.Unlock()
			return code, nil
		}
		s.mu.Unlock()
	}
	return "", fmt.Errorf("取件码空间已满，请稍后再试")
}

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"success": false, "message": "只支持 POST"})
		return
	}
	code, err := s.NewRoom()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"success": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"code":    code,
		"message": "房间创建成功",
	})
}

func (s *Server) handleRoomInfo(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	status := s.roomStatus(code)
	writeJSON(w, http.StatusOK, status)
}

// roomStatus mirrors the shapes the web client reads.
func (s *Server) roomStatus(code string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	rm := s.rooms[code]
	if rm == nil {
		return map[string]any{
			"success": false,
			"exists":  false,
			"message": "房间不存在或已过期",
		}
	}
	sender, receiver := rm.peers[protocol.RoleSender], rm.peers[protocol.RoleReceiver]
	return map[string]any{
		"success":         true,
		"exists":          true,
		"sender_online":   sender != nil,
		"receiver_online": receiver != nil,
		"is_room_full":    sender != nil && receiver != nil,
		"created_at":      rm.created,
	}
}

// handleSignal accepts the WebRTC signalling socket.
func (s *Server) handleSignal(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	role := protocol.Role(r.URL.Query().Get("role"))
	if code == "" || !role.Valid() {
		http.Error(w, "连接参数无效", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	rm := s.rooms[code]
	if rm == nil {
		s.mu.Unlock()
		writeSocketError(w, r, s, "房间不存在或已过期")
		return
	}
	if rm.peers[role] != nil || len(rm.peers) >= 2 {
		s.mu.Unlock()
		writeSocketError(w, r, s, "当前房间人数已满，正在传输中无法加入")
		return
	}
	s.mu.Unlock()

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[signal] 升级失败: %v", err)
		return
	}
	conn.SetReadLimit(32 << 20)

	c := &client{id: newClientID(), role: role, conn: conn}

	s.mu.Lock()
	rm = s.rooms[code]
	if rm == nil {
		s.mu.Unlock()
		conn.Close()
		return
	}
	if rm.peers[role] != nil {
		// Lost a race with another connection for the same seat.
		s.mu.Unlock()
		conn.Close()
		return
	}
	rm.peers[role] = c
	peer := rm.peers[role.Peer()]
	s.mu.Unlock()

	defer func() {
		s.dropClient(code, c)
		conn.Close()
	}()

	// Tell each side who just walked in, exactly like the web server does.
	if peer != nil {
		s.sendSignal(peer, protocol.Signal{Type: protocol.SignalPeerJoined, From: c.id,
			Payload: mustRaw(map[string]any{"role": string(role)})})
		s.sendSignal(c, protocol.Signal{Type: protocol.SignalPeerJoined, From: peer.id,
			Payload: mustRaw(map[string]any{"role": string(peer.role)})})
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var sig protocol.Signal
		if err := json.Unmarshal(data, &sig); err != nil {
			continue
		}
		sig.From = c.id
		s.mu.Lock()
		rm := s.rooms[code]
		var target *client
		if rm != nil {
			target = rm.peers[role.Peer()]
		}
		s.mu.Unlock()
		if target == nil {
			continue
		}
		s.sendSignal(target, sig)
	}
}

// dropClient removes a peer and tells the other side the room is dead.
func (s *Server) dropClient(code string, c *client) {
	s.mu.Lock()
	rm := s.rooms[code]
	if rm == nil {
		s.mu.Unlock()
		return
	}
	if rm.peers[c.role] == c {
		delete(rm.peers, c.role)
	}
	peer := rm.peers[c.role.Peer()]
	empty := len(rm.peers) == 0
	if empty {
		delete(s.rooms, code)
	}
	s.mu.Unlock()

	if peer != nil {
		s.sendSignal(peer, protocol.Signal{Type: protocol.SignalDisconnect, From: c.id,
			Payload: mustRaw(map[string]any{"role": string(c.role), "message": "对方已停止传输"})})
	}
}

func (s *Server) sendSignal(c *client, sig protocol.Signal) {
	c.writeM.Lock()
	defer c.writeM.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteJSON(sig); err != nil {
		log.Printf("[signal] 转发失败: %v", err)
	}
}

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for code, rm := range s.rooms {
			if now.Sub(rm.created) > s.opts.RoomTTL {
				delete(s.rooms, code)
			}
		}
		s.mu.Unlock()
	}
}

// writeSocketError upgrades, sends one error frame and closes: browsers show
// this text directly.
func writeSocketError(w http.ResponseWriter, r *http.Request, s *Server, msg string) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.WriteJSON(map[string]any{"type": string(protocol.SignalError), "message": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func mustRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

// newCode is six characters from an unambiguous alphabet (no 0/O/1 confusion
// beyond the digits we keep), matching chuan's generator.
func newCode() (string, error) {
	const alphabet = "123456789ABCDEFGHIJKLMNPQRSTUVWXYZ"
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 6)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}

func newClientID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("chuan_client_%x", buf)
}
