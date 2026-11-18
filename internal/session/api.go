package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultServer is the public chuan instance the app points at until the
// user changes it.
const DefaultServer = "https://transfer.52python.cn"

// RoomStatus is what GET /api/room-info answers.
type RoomStatus struct {
	Success        bool   `json:"success"`
	Exists         bool   `json:"exists"`
	SenderOnline   bool   `json:"sender_online"`
	ReceiverOnline bool   `json:"receiver_online"`
	IsRoomFull     bool   `json:"is_room_full"`
	Message        string `json:"message"`
}

// Ready reports whether the room accepts the given role right now.
func (r RoomStatus) Ready(role string) bool {
	if !r.Success || !r.Exists {
		return false
	}
	if role == "sender" {
		return !r.SenderOnline
	}
	return !r.ReceiverOnline
}

// People counts how many clients are in the room.
func (r RoomStatus) People() int {
	n := 0
	if r.SenderOnline {
		n++
	}
	if r.ReceiverOnline {
		n++
	}
	return n
}

// normalizeBase turns what the user typed into an http(s) origin.
func normalizeBase(server string) (string, error) {
	s := strings.TrimSpace(server)
	if s == "" {
		s = DefaultServer
	}
	if !strings.Contains(s, "://") {
		// Bare hosts are the LAN case: a local server speaks plain HTTP.
		s = "http://" + s
	}
	s = strings.TrimSuffix(s, "/")
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return "", errors.New("服务器地址需要以 http:// 或 https:// 开头")
	}
	return s, nil
}

var httpClient = &http.Client{Timeout: 20 * time.Second}

// CreateRoom asks the server for a new six-digit room code.
func CreateRoom(ctx context.Context, server string) (string, error) {
	base, err := normalizeBase(server)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/create-room", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("无法连接服务器：%w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var out struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("服务器返回了无法解析的内容（HTTP %d）", resp.StatusCode)
	}
	if !out.Success || out.Code == "" {
		if out.Message == "" {
			out.Message = "服务器没有返回房间码"
		}
		return "", errors.New(out.Message)
	}
	return out.Code, nil
}

// FetchRoom reads a room's current occupants.
func FetchRoom(ctx context.Context, server, code string) (RoomStatus, error) {
	var status RoomStatus
	base, err := normalizeBase(server)
	if err != nil {
		return status, err
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return status, errors.New("请先填写取件码")
	}
	url := base + "/api/room-info?code=" + code
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return status, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return status, fmt.Errorf("无法连接服务器：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return status, fmt.Errorf("服务器返回了无法解析的内容（HTTP %d）", resp.StatusCode)
	}
	return status, nil
}
