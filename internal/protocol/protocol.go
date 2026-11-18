// Package protocol holds the wire formats chuan speaks: the signaling
// messages exchanged over the WebSocket, and the messages carried by the
// RTCDataChannel.
//
// It mirrors the JavaScript client field for field, so a native client and a
// browser can join the same room and exchange files.
package protocol

import (
	"encoding/json"
	"fmt"
)

// Roles of a room. A room has one sender and one receiver.
type Role string

const (
	RoleSender   Role = "sender"
	RoleReceiver Role = "receiver"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleSender || r == RoleReceiver }

// Peer returns the role on the other side of the room.
func (r Role) Peer() Role {
	if r == RoleSender {
		return RoleReceiver
	}
	return RoleSender
}

// Logical channels multiplexed on the single data channel. Every message
// names the channel it belongs to in its "channel" field.
const (
	ChannelFile = "file-transfer"
	ChannelText = "text-transfer"
	ChannelChat = "chat"
)

// The data channel both sides negotiate. Only the sender creates it; the
// receiver picks it up from the PeerConnection.
const (
	DataChannelLabel = "shared-channel"
	// ChunkSize is the payload of one binary frame carrying file bytes.
	ChunkSize = 256 * 1024
	// BufferHighWater is the sender's backpressure threshold: it waits for
	// BufferedAmount to fall below it before queueing another chunk.
	BufferHighWater = 2 * 1024 * 1024
)

// Signaling message types.
const (
	SignalOffer        = "offer"
	SignalAnswer       = "answer"
	SignalICECandidate = "ice-candidate"
	SignalPeerJoined   = "peer-joined"
	SignalPeerLeft     = "peer-left"
	SignalSyncRequest  = "sync-request"
	SignalRelayRequest = "relay-request"
	SignalDisconnect   = "disconnection"
	SignalError        = "error"
)

// Signal is one message on the signaling WebSocket. The server fills in From
// with the sender's client ID and forwards the message to the peer.
type Signal struct {
	Type    string          `json:"type"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// Message carries the reason of an "error" signal.
	Message string `json:"message,omitempty"`
}

// NewSignal marshals payload into the signal.
func NewSignal(typ string, payload any) (Signal, error) {
	s := Signal{Type: typ}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return s, err
		}
		s.Payload = raw
	}
	return s, nil
}

// PeerJoined is the payload of a "peer-joined" signal.
type PeerJoined struct {
	Role Role `json:"role"`
}

// Description is an SDP offer or answer, as WebRTC's RTCSessionDescription.
type Description struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// ICECandidate is one trickled candidate, as WebRTC's RTCIceCandidateInit.
type ICECandidate struct {
	Candidate        string  `json:"candidate"`
	SDPMid           *string `json:"sdpMid,omitempty"`
	SDPMLineIndex    *uint16 `json:"sdpMLineIndex,omitempty"`
	UsernameFragment *string `json:"usernameFragment,omitempty"`
}

// Message is the envelope of every data channel message: a type, the logical
// channel it belongs to, and its JSON payload. Binary frames carry raw file
// bytes and no envelope; a preceding file-chunk-info says where they go.
type Message struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewMessage builds a message with payload marshaled into it.
func NewMessage(typ, channel string, payload any) (Message, error) {
	m := Message{Type: typ, Channel: channel}
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return m, err
		}
		m.Payload = raw
	}
	return m, nil
}

// MustMessage is NewMessage for payloads that cannot fail to marshal.
func MustMessage(typ, channel string, payload any) Message {
	m, err := NewMessage(typ, channel, payload)
	if err != nil {
		panic(err)
	}
	return m
}

// Decode unmarshals the payload into v.
func (m Message) Decode(v any) error {
	if len(m.Payload) == 0 {
		return fmt.Errorf("message %q has no payload", m.Type)
	}
	return json.Unmarshal(m.Payload, v)
}

// File transfer message types.
const (
	MsgFileMetadata  = "file-metadata"
	MsgFileChunkInfo = "file-chunk-info"
	MsgFileComplete  = "file-complete"
	MsgFileList      = "file-list"
	MsgFileRequest   = "file-request"
	MsgFileChunkAck  = "file-chunk-ack"
)

// Text transfer message types.
const (
	MsgTextSync   = "text-sync"
	MsgTextTyping = "text-typing"
)

// FileMetadata announces a file the sender is about to stream.
type FileMetadata struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// FileChunkInfo precedes each binary frame and says which chunk it carries.
type FileChunkInfo struct {
	FileID      string `json:"fileId"`
	ChunkIndex  int    `json:"chunkIndex"`
	TotalChunks int    `json:"totalChunks"`
}

// FileComplete closes a stream; the receiver flushes it to disk.
type FileComplete struct {
	FileID string `json:"fileId"`
}

// FileRequest asks the sender for a file by ID, by name.
type FileRequest struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
}

// FileItem is one entry of a file-list, and one row of the transfer list.
type FileItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// FileList is the payload of a file-list message.
type FileList []FileItem

// TextSync carries the text editor's contents.
type TextSync struct {
	Text string `json:"text"`
}

// TextTyping reports whether the peer is typing.
type TextTyping struct {
	Typing bool `json:"typing"`
}

// TotalChunks returns how many chunks a file of size bytes needs.
func TotalChunks(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + ChunkSize - 1) / ChunkSize)
}
