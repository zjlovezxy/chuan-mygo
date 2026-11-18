// Package netlink is the WebSocket the client keeps with the signaling
// server: plain messages in, plain messages out.
//
// The desktop build dials with gorilla/websocket; the WebAssembly build
// dials the browser's own WebSocket through syscall/js, because Go's net
// package has no sockets there.
package netlink

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

// Frame is one message received from the server.
type Frame struct {
	// Binary is true for a binary frame, false for a text one.
	Binary bool
	Data   []byte
}

// JoinURL builds the signaling URL of a room from the server's base URL,
// which may be an http(s) origin or already a ws(s) URL.
//
// The path is "/api/ws/webrtc", the one the chuan server exposes, with the
// room code and the client's role.
func JoinURL(base, path, code, role string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http", "":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", errors.New("服务器地址需要以 http、https、ws 或 wss 开头")
	}
	if u.Host == "" {
		return "", errors.New("服务器地址缺少主机名")
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	q := u.Query()
	q.Set("code", code)
	q.Set("role", role)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// EncodeJSON marshals v for a text frame.
func EncodeJSON(v any) ([]byte, error) { return json.Marshal(v) }
