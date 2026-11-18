package server

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// relayRoom is the fallback path for peers that cannot punch through NAT: the
// server forwards frames untouched, so the DataChannel semantics are faked with
// relay-data / relay-binary envelopes on the client side.
type relayRoom struct {
	peers map[protocol.Role]*client
}

type relayState struct {
	mu    sync.Mutex
	rooms map[string]*relayRoom
}

func (s *Server) relay() *relayState {
	s.relayOnce.Do(func() { s.relayRooms = &relayState{rooms: make(map[string]*relayRoom)} })
	return s.relayRooms
}

// handleRelay binds a browser/app to the relay channel of a room.
func (s *Server) handleRelay(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	role := protocol.Role(r.URL.Query().Get("role"))
	if code == "" || !role.Valid() {
		writeSocketError(w, r, s, "连接参数无效")
		return
	}
	if status := s.roomStatus(code); status["exists"] != true {
		writeSocketError(w, r, s, "房间不存在或已过期")
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(32 << 20)

	c := &client{id: newClientID(), role: role, conn: conn}
	state := s.relay()

	state.mu.Lock()
	rm := state.rooms[code]
	if rm == nil {
		rm = &relayRoom{peers: map[protocol.Role]*client{}}
		state.rooms[code] = rm
	}
	old := rm.peers[role]
	rm.peers[role] = c
	peer := rm.peers[role.Peer()]
	state.mu.Unlock()

	if old != nil {
		old.conn.Close()
	}

	writeRaw(c, map[string]any{
		"type":           "relay-ready",
		"role":           string(role),
		"client_id":      c.id,
		"peer_connected": peer != nil,
		"timestamp":      time.Now().UnixMilli(),
	})
	if peer != nil {
		writeRaw(peer, map[string]any{"type": "relay-peer-joined", "peer_role": string(role)})
	}

	defer func() {
		state.mu.Lock()
		rm := state.rooms[code]
		if rm != nil {
			if rm.peers[role] == c {
				delete(rm.peers, role)
			}
			if len(rm.peers) == 0 {
				delete(state.rooms, code)
			}
			if other := rm.peers[role.Peer()]; other != nil {
				writeRaw(other, map[string]any{"type": "relay-peer-left", "peer_role": string(role)})
			}
		}
		state.mu.Unlock()
		conn.Close()
	}()

	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		state.mu.Lock()
		rm := state.rooms[code]
		var target *client
		if rm != nil {
			target = rm.peers[role.Peer()]
		}
		state.mu.Unlock()
		if target == nil {
			continue
		}
		target.writeM.Lock()
		target.conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		err = target.conn.WriteMessage(kind, data)
		target.writeM.Unlock()
		if err != nil {
			log.Printf("[relay] 转发失败: %v", err)
			return
		}
	}
}

func writeRaw(c *client, v any) {
	c.writeM.Lock()
	defer c.writeM.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.WriteJSON(v); err != nil {
		log.Printf("[relay] 写入失败: %v", err)
	}
}
