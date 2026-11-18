// Package transfer moves files over the data channel with the same framing
// the web client uses: file-metadata, then one file-chunk-info message
// followed by one binary frame per chunk, then file-complete.
package transfer

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
)

// Conn is what a transfer needs from the session underneath.
type Conn interface {
	Send(msg protocol.Message) error
	SendBinary(data []byte) error
	BufferedAmount() uint64
	Connected() bool
}

// Progress is one progress update, for either direction.
type Progress struct {
	ID       string
	Name     string
	Moved    int64
	Total    int64
	Speed    float64 // bytes per second
	Incoming bool
}

// Fraction returns the progress in 0..1.
func (p Progress) Fraction() float64 {
	if p.Total <= 0 {
		return 0
	}
	f := float64(p.Moved) / float64(p.Total)
	if f > 1 {
		return 1
	}
	return f
}

// Events are the transfer callbacks. All may be nil.
type Events struct {
	// Progress fires while a file moves, throttled to a few times a second.
	Progress func(Progress)
	// Started fires when the peer announces a file to receive.
	Started func(protocol.FileMetadata)
	// Sent fires when the peer acknowledged the end of a file we sent.
	Sent func(protocol.FileMetadata)
	// Finished fires when a file is on disk (or handed to the browser).
	Finished func(protocol.FileMetadata, string)
	// Log receives diagnostic lines.
	Log func(string)
	// Error receives failures.
	Error func(error)
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

// Sink decides where received files are written.
type Sink interface {
	// Open prepares a destination for meta.
	Open(meta protocol.FileMetadata) (Receiver, error)
}

// Receiver is one file being written.
type Receiver interface {
	// Write stores the chunk with this index.
	Write(index int, data []byte) error
	// Commit publishes the file and returns where it landed.
	Commit() (string, error)
	// Abort discards whatever was written.
	Abort()
}

type incoming struct {
	meta     protocol.FileMetadata
	receiver Receiver
	moved    int64
	started  time.Time
	mark     time.Time
	markLen  int64
	speed    float64
}

// Manager sends and receives files for one session.
type Manager struct {
	conn Conn
	sink Sink
	ev   Events

	mu       sync.Mutex
	incoming map[string]*incoming
	expected *protocol.FileChunkInfo
	sending  bool
}

// New builds a manager. conn may be nil until the channel opens as long as
// nothing is sent before then.
func New(conn Conn, sink Sink, ev Events) *Manager {
	return &Manager{conn: conn, sink: sink, ev: ev, incoming: map[string]*incoming{}}
}

// Sending reports whether a send is in flight.
func (m *Manager) Sending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sending
}

// newFileID builds an ID in the web client's shape: file_<ms>_<rand36>.
func newFileID() string {
	return "file_" + strconv.FormatInt(time.Now().UnixMilli(), 10) + "_" + randBase36(9)
}

func randBase36(n int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

// Source is one file to send, read on demand so that large files never sit
// in memory whole.
type Source interface {
	Name() string
	Size() int64
	Type() string
	// ReadAt reads up to length bytes at offset, returning fewer only at
	// the end of the file.
	ReadAt(offset int64, length int) ([]byte, error)
	Close() error
}

// Send streams each source in turn and returns the first error.
func (m *Manager) Send(ctx context.Context, sources []Source) error {
	if len(sources) == 0 {
		return nil
	}
	m.mu.Lock()
	if m.sending {
		m.mu.Unlock()
		return errors.New("已有文件正在发送")
	}
	m.sending = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.sending = false
		m.mu.Unlock()
	}()

	if !m.conn.Connected() {
		return errors.New("P2P 连接尚未建立")
	}

	for _, src := range sources {
		if err := m.sendSource(ctx, src); err != nil {
			_ = src.Close()
			m.ev.fail(err)
			return err
		}
		_ = src.Close()
	}
	return nil
}

func (m *Manager) sendSource(ctx context.Context, src Source) error {
	size := src.Size()
	total := protocol.TotalChunks(size)

	meta := protocol.FileMetadata{
		ID:   newFileID(),
		Name: SanitizeName(src.Name()),
		Size: size,
		Type: src.Type(),
	}
	if err := m.conn.Send(protocol.MustMessage(protocol.MsgFileMetadata, protocol.ChannelFile, meta)); err != nil {
		return err
	}
	m.ev.log("开始发送 %s（%s，%d 块）", meta.Name, HumanBytes(size), total)

	mark := time.Now()
	var markLen int64
	lastReport := mark

	for index := 0; index < total; index++ {
		if err := m.waitDrain(ctx); err != nil {
			return err
		}
		offset := int64(index) * int64(protocol.ChunkSize)
		length := protocol.ChunkSize
		if remaining := size - offset; remaining < int64(length) {
			length = int(remaining)
		}
		chunk, err := src.ReadAt(offset, length)
		if err != nil {
			return fmt.Errorf("读取 %s 失败：%w", meta.Name, err)
		}
		if len(chunk) == 0 {
			break
		}

		info := protocol.FileChunkInfo{FileID: meta.ID, ChunkIndex: index, TotalChunks: total}
		if err := m.conn.Send(protocol.MustMessage(protocol.MsgFileChunkInfo, protocol.ChannelFile, info)); err != nil {
			return err
		}
		if err := m.conn.SendBinary(chunk); err != nil {
			return err
		}

		moved := offset + int64(len(chunk))
		now := time.Now()
		if now.Sub(lastReport) >= 200*time.Millisecond || index == total-1 {
			speed := 0.0
			if elapsed := now.Sub(mark).Seconds(); elapsed > 0 {
				speed = float64(moved-markLen) / elapsed
			}
			if m.ev.Progress != nil {
				m.ev.Progress(Progress{ID: meta.ID, Name: meta.Name, Moved: moved, Total: size, Speed: speed})
			}
			lastReport = now
			if now.Sub(mark) >= 2*time.Second {
				mark, markLen = now, moved
			}
		}
	}

	if err := m.conn.Send(protocol.MustMessage(protocol.MsgFileComplete, protocol.ChannelFile, protocol.FileComplete{FileID: meta.ID})); err != nil {
		return err
	}
	m.ev.log("发送完成：%s", meta.Name)
	if m.ev.Sent != nil {
		m.ev.Sent(meta)
	}
	return nil
}

// waitDrain blocks until the channel's outgoing queue is below the high
// water mark, so a fast disk cannot outrun the network.
func (m *Manager) waitDrain(ctx context.Context) error {
	for m.conn.BufferedAmount() > protocol.BufferHighWater {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return nil
}

// HandleMessage processes one control message from the peer.
func (m *Manager) HandleMessage(msg protocol.Message) {
	switch msg.Type {
	case protocol.MsgFileMetadata:
		var meta protocol.FileMetadata
		if err := msg.Decode(&meta); err != nil {
			m.ev.log("文件元信息解析失败：%v", err)
			return
		}
		if m.sink == nil {
			m.ev.fail(errors.New("没有配置下载目录"))
			return
		}
		receiver, err := m.sink.Open(meta)
		if err != nil {
			m.ev.fail(err)
			return
		}
		m.mu.Lock()
		m.incoming[meta.ID] = &incoming{
			meta:     meta,
			receiver: receiver,
			started:  time.Now(),
			mark:     time.Now(),
		}
		m.mu.Unlock()
		m.ev.log("对方开始发送 %s（%s）", meta.Name, HumanBytes(meta.Size))
		if m.ev.Started != nil {
			m.ev.Started(meta)
		}

	case protocol.MsgFileChunkInfo:
		var info protocol.FileChunkInfo
		if err := msg.Decode(&info); err != nil {
			return
		}
		m.mu.Lock()
		m.expected = &info
		m.mu.Unlock()

	case protocol.MsgFileComplete:
		var done protocol.FileComplete
		if err := msg.Decode(&done); err != nil {
			return
		}
		m.mu.Lock()
		inc := m.incoming[done.FileID]
		delete(m.incoming, done.FileID)
		m.mu.Unlock()
		if inc == nil {
			return
		}
		path, err := inc.receiver.Commit()
		if err != nil {
			m.ev.fail(fmt.Errorf("保存 %s 失败：%w", inc.meta.Name, err))
			return
		}
		m.ev.log("已接收 %s → %s", inc.meta.Name, path)
		if m.ev.Finished != nil {
			m.ev.Finished(inc.meta, path)
		}

	case protocol.MsgFileRequest:
		var req protocol.FileRequest
		_ = msg.Decode(&req)
		m.ev.log("对方请求文件：%s", req.FileName)

	case protocol.MsgFileList:
		m.ev.log("收到对方的文件列表")

	case protocol.MsgFileChunkAck:
		// Older clients acknowledge each chunk; SCTP already guarantees
		// delivery, so there is nothing to do.

	default:
		m.ev.log("未处理的传输消息：%s", msg.Type)
	}
}

// HandleBinary stores one chunk that follows a file-chunk-info.
func (m *Manager) HandleBinary(data []byte) {
	m.mu.Lock()
	expected := m.expected
	m.expected = nil
	var inc *incoming
	if expected != nil {
		inc = m.incoming[expected.FileID]
	}
	m.mu.Unlock()

	if expected == nil || inc == nil {
		return
	}
	if err := inc.receiver.Write(expected.ChunkIndex, data); err != nil {
		m.ev.fail(fmt.Errorf("写入 %s 第 %d 块失败：%w", inc.meta.Name, expected.ChunkIndex, err))
		return
	}

	m.mu.Lock()
	inc.moved += int64(len(data))
	moved := inc.moved
	now := time.Now()
	var speed float64
	if elapsed := now.Sub(inc.mark).Seconds(); elapsed >= 1 {
		speed = float64(moved-inc.markLen) / elapsed
		inc.mark, inc.markLen = now, moved
	}
	meta := inc.meta
	m.mu.Unlock()

	if m.ev.Progress != nil {
		m.ev.Progress(Progress{ID: meta.ID, Name: meta.Name, Moved: moved, Total: meta.Size, Speed: speed, Incoming: true})
	}
}

// SanitizeName strips anything that must not reach a filesystem: path
// separators, control characters, and empty names.
func SanitizeName(name string) string {
	// Peers may send either separator; keep only the last component.
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == ':' || r == '\\' || r == '/' {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "未命名文件"
	}
	return name
}

// HumanBytes formats a byte count the way the web UI does.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// mimeByName guesses a MIME type from a file extension, for the metadata the
// browser side uses to name the download.
func mimeByName(path string) string {
	switch filepath.Ext(path) {
	case ".txt", ".md", ".log":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".mp4":
		return "video/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}
