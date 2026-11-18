//go:build !js

package netlink

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WS is the client's connection to the signaling server.
type WS struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// Dial opens the WebSocket at rawURL and waits for the handshake.
func Dial(ctx context.Context, rawURL string) (*WS, error) {
	d := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conn, resp, err := d.DialContext(ctx, rawURL, nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("连接信令服务器失败（HTTP %d）：%w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("连接信令服务器失败：%w", err)
	}
	conn.SetReadLimit(32 << 20)
	return &WS{conn: conn}, nil
}

// SendJSON writes v as a text frame.
func (w *WS) SendJSON(v any) error {
	data, err := EncodeJSON(v)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.TextMessage, data)
}

// SendBinary writes a binary frame.
func (w *WS) SendBinary(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, data)
}

// Recv reads the next frame, blocking until one arrives or the connection
// fails.
func (w *WS) Recv() (Frame, error) {
	typ, data, err := w.conn.ReadMessage()
	if err != nil {
		return Frame{}, err
	}
	return Frame{Binary: typ == websocket.BinaryMessage, Data: data}, nil
}

// Close shuts the connection down.
func (w *WS) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.Close()
}
