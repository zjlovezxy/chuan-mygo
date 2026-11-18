package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

func newTestServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	s := New(Options{RoomTTL: time.Minute})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, s
}

func dialWS(t *testing.T, base, path string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(base, "http") + path
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readSignal(t *testing.T, conn *websocket.Conn) protocol.Signal {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var sig protocol.Signal
	if err := conn.ReadJSON(&sig); err != nil {
		t.Fatalf("read signal: %v", err)
	}
	return sig
}

func TestCreateRoomAndStatus(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Post(ts.URL+"/api/create-room", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var created struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create: %v (%s)", err, body)
	}
	if !created.Success || len(created.Code) != 6 {
		t.Fatalf("unexpected create response: %s", body)
	}

	statusResp, err := http.Get(ts.URL + "/api/room-info?code=" + created.Code)
	if err != nil {
		t.Fatalf("room info: %v", err)
	}
	defer statusResp.Body.Close()
	var status struct {
		Exists       bool `json:"exists"`
		SenderOnline bool `json:"sender_online"`
		RoomFull     bool `json:"is_room_full"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Exists || status.SenderOnline || status.RoomFull {
		t.Fatalf("unexpected status: %+v", status)
	}

	missingResp, err := http.Get(ts.URL + "/api/room-info?code=ZZZZZZ")
	if err != nil {
		t.Fatalf("missing room info: %v", err)
	}
	defer missingResp.Body.Close()
	var missing struct {
		Exists  bool   `json:"exists"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(missingResp.Body).Decode(&missing)
	if missing.Exists || missing.Message == "" {
		t.Fatalf("missing room should not exist: %+v", missing)
	}
}

func TestSignallingForwardsOfferAndAnswer(t *testing.T) {
	ts, s := newTestServer(t)
	code, err := s.NewRoom()
	if err != nil {
		t.Fatalf("new room: %v", err)
	}

	sender := dialWS(t, ts.URL, "/api/ws/webrtc?code="+code+"&role=sender")

	// The receiver joins; the sender learns about it, and the receiver learns
	// the sender is there.
	receiver := dialWS(t, ts.URL, "/api/ws/webrtc?code="+code+"&role=receiver")

	joined := readSignal(t, sender)
	if joined.Type != protocol.SignalPeerJoined {
		t.Fatalf("sender got %q, want peer-joined", joined.Type)
	}
	var payload struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(joined.Payload, &payload)
	if payload.Role != string(protocol.RoleReceiver) {
		t.Fatalf("sender saw role %q, want receiver", payload.Role)
	}

	offer := protocol.Signal{Type: protocol.SignalOffer, Payload: json.RawMessage(`{"type":"offer","sdp":"v=0"}`)}
	sender.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := sender.WriteJSON(offer); err != nil {
		t.Fatalf("send offer: %v", err)
	}

	// Skip the receiver's own peer-joined frames until the offer arrives.
	deadline := time.Now().Add(3 * time.Second)
	for {
		receiver.SetReadDeadline(deadline)
		var sig protocol.Signal
		if err := receiver.ReadJSON(&sig); err != nil {
			t.Fatalf("receiver read: %v", err)
		}
		if sig.Type != protocol.SignalOffer {
			continue
		}
		if sig.From == "" {
			t.Fatal("offer should carry the server's client id")
		}
		var description protocol.Description
		if err := json.Unmarshal(sig.Payload, &description); err != nil || description.SDP != "v=0" {
			t.Fatalf("offer payload mangled: %s", sig.Payload)
		}
		break
	}
}

func TestRoomRejectsThirdClient(t *testing.T) {
	ts, s := newTestServer(t)
	code, _ := s.NewRoom()

	dialWS(t, ts.URL, "/api/ws/webrtc?code="+code+"&role=sender")
	dialWS(t, ts.URL, "/api/ws/webrtc?code="+code+"&role=receiver")

	third := dialWS(t, ts.URL, "/api/ws/webrtc?code="+code+"&role=receiver")
	third.SetReadDeadline(time.Now().Add(3 * time.Second))
	var msg struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := third.ReadJSON(&msg); err != nil {
		t.Fatalf("third client read: %v", err)
	}
	if msg.Type != string(protocol.SignalError) || !strings.Contains(msg.Message, "人数已满") {
		t.Fatalf("third client got %+v", msg)
	}
}

func TestDisconnectNotifiesPeer(t *testing.T) {
	ts, s := newTestServer(t)
	code, _ := s.NewRoom()

	sender := dialWS(t, ts.URL, "/api/ws/webrtc?code="+code+"&role=sender")
	receiver := dialWS(t, ts.URL, "/api/ws/webrtc?code="+code+"&role=receiver")
	readSignal(t, sender) // peer-joined

	receiver.Close()

	deadline := time.Now().Add(3 * time.Second)
	for {
		sender.SetReadDeadline(deadline)
		var sig protocol.Signal
		if err := sender.ReadJSON(&sig); err != nil {
			t.Fatalf("sender should hear the receiver leave: %v", err)
		}
		if sig.Type == protocol.SignalDisconnect {
			break
		}
	}
}
