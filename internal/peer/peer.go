package peer

import (
	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// ICEServer is one STUN or TURN server of the peer configuration, as the
// browser's RTCConfiguration takes it.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// DefaultICEServers mirrors the public STUN servers the web client uses.
// Cross-network transfers need one of them to find a path; two devices on
// the same LAN connect through host candidates without them.
func DefaultICEServers() []ICEServer {
	return []ICEServer{
		{URLs: []string{"stun:stun.l.google.com:19302"}},
		{URLs: []string{"stun:stun1.l.google.com:19302"}},
		{URLs: []string{"stun:stun.cloudflare.com:3478"}},
	}
}

// Config describes the connection to build.
type Config struct {
	// Role is this side's role in the room. The sender creates the data
	// channel and the offer; the receiver answers.
	Role protocol.Role
	// ICEServers default to DefaultICEServers when empty.
	ICEServers []ICEServer

	// OnICE receives this side's candidates as they are gathered.
	OnICE func(protocol.ICECandidate)
	// OnOpen fires when the data channel is open and messages can flow.
	OnOpen func()
	// OnMessage receives text and binary messages from the peer.
	OnMessage func(binary bool, data []byte)
	// OnClose fires once when the connection goes away.
	OnClose func()
	// OnError receives non-fatal errors worth showing.
	OnError func(error)
}

// notifyClose calls OnClose at most once.
func (c Config) notifyClose() {
	if c.OnClose != nil {
		c.OnClose()
	}
}

// reportError shows err if anyone listens.
func (c Config) reportError(err error) {
	if err != nil && c.OnError != nil {
		c.OnError(err)
	}
}
