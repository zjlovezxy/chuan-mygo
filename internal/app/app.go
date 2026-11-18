// Package app is the Chuan desktop client: the mygo native interface, the
// session it drives, and the files it moves.
package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
	"github.com/zjlovezxy/chuan-mygo/internal/session"
	"github.com/zjlovezxy/chuan-mygo/internal/transfer"
)

// Page is the navigation state of the window.
type Page int

const (
	// PageHome creates and joins rooms.
	PageHome Page = iota
	// PageTransfer shows the running session.
	PageTransfer
	// PageSettings is the settings screen.
	PageSettings
)

// historyItem is one finished transfer, kept for the home page's list.
type historyItem struct {
	Name     string
	Size     int64
	Incoming bool
	At       time.Time
	Path     string
}

// App is the whole client state. The view reads it on the main thread; every
// goroutine changes it under the mutex and then asks for a frame.
type App struct {
	mu  sync.RWMutex
	win *mygo.Window

	// Settings.
	server      string
	downloadDir string
	dark        bool

	page      Page
	roleIdx   int // 0: send, 1: receive
	codeInput string
	status    string
	statusErr bool

	// The running session.
	roomCode string
	sess     *session.Session
	mgr      *transfer.Manager
	cancel   context.CancelFunc
	state    session.State
	detail   string

	picked  []transfer.Source
	picking bool
	busy    bool

	progress map[string]transfer.Progress
	order    []string

	history []historyItem
	logs    []string
}

// New builds the app with the saved settings.
func New() *App {
	cfg := loadSettings()
	a := &App{
		server:      cfg.Server,
		downloadDir: cfg.DownloadDir,
		dark:        cfg.Dark,
		state:       session.StateClosed,
		progress:    map[string]transfer.Progress{},
	}
	if strings.TrimSpace(a.server) == "" {
		a.server = session.DefaultServer
	}
	if strings.TrimSpace(a.downloadDir) == "" {
		a.downloadDir = defaultDownloadDir()
	}
	return a
}

// SetWindow lets the app repaint the window it draws into.
func (a *App) SetWindow(w *mygo.Window) { a.win = w }

// Init applies the saved appearance.
func (a *App) Init() {
	a.applyAppearance()
	a.refresh()
}

func (a *App) applyAppearance() {
	a.mu.RLock()
	dark := a.dark
	a.mu.RUnlock()
	if dark {
		mygo.Theme.SetSource(mygo.ThemeDark)
	} else {
		mygo.Theme.SetSource(mygo.ThemeLight)
	}
}

// refresh asks for one frame. Safe from any goroutine.
func (a *App) refresh() {
	if w := a.win; w != nil {
		w.Invalidate()
	}
}

func (a *App) setStatus(msg string, isErr bool) {
	a.mu.Lock()
	a.status, a.statusErr = msg, isErr
	a.mu.Unlock()
	a.refresh()
}

func (a *App) addLog(format string, args ...any) {
	line := time.Now().Format("15:04:05") + "  " + fmt.Sprintf(format, args...)
	a.mu.Lock()
	a.logs = append(a.logs, line)
	if len(a.logs) > 120 {
		a.logs = a.logs[len(a.logs)-120:]
	}
	a.mu.Unlock()
	a.refresh()
}

// manager is the transfer manager of the running session, or nil.
func (a *App) manager() *transfer.Manager {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mgr
}

// View draws the whole window.
func (a *App) View(c *ui.Context) {
	c.SetTheme(a.theme(c))

	w, _ := c.Size()
	if w < 760 {
		ui.Column(c).Fill().Background(colPanel).Children(func() {
			ui.Column(c).FillWidth().Grow(1).Children(func() { a.pageView(c) })
			a.tabBar(c)
		})
		return
	}

	ui.Row(c).Fill().Background(colAppBg).Children(func() {
		ui.Column(c).Width(236).FillHeight().Background(colAppBg).Children(func() { a.sidebar(c) })
		ui.Column(c).Grow(1).FillHeight().Background(colPanel).Children(func() { a.pageView(c) })
	})
}

func (a *App) pageView(c *ui.Context) {
	switch a.page {
	case PageTransfer:
		a.transferPage(c)
	case PageSettings:
		a.settingsPage(c)
	default:
		a.homePage(c)
	}
}

// pagePadding is the page's inner spacing, tighter on narrow windows.
func (a *App) pagePadding(c *ui.Context) (top, side float32) {
	t := c.Theme()
	w, _ := c.Size()
	if w < 760 {
		return t.Space(4), t.Space(4)
	}
	return t.Space(5), t.Space(6)
}

// ---------------------------------------------------------------- navigation

func (a *App) setPage(p Page) {
	a.mu.Lock()
	a.page = p
	a.mu.Unlock()
	a.refresh()
}

func (a *App) sidebar(c *ui.Context) {
	t := c.Theme()
	a.mu.RLock()
	page := a.page
	roomCode := a.roomCode
	state := a.state
	a.mu.RUnlock()

	ui.Column(c).FillHeight().
		Padding(t.Space(5), t.Space(3), t.Space(3), t.Space(3)).
		Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2.5)).
				Padding(t.Space(1), t.Space(2), t.Space(5), t.Space(2)).
				Children(func() {
					ui.Box(c).Size(t.Space(8), t.Space(8)).Radius(t.Space(2)).
						Background(colAccent).Center().Children(func() {
						ui.Text(c, "传").FontSize(17).Bold().TextColor(ui.Hex("#ffffff"))
					})
					ui.Column(c).Gap(1).Children(func() {
						ui.Text(c, "文件快传").FontSize(15).Bold().TextColor(colText).SingleLine()
						ui.Text(c, "Chuan").FontSize(10).TextColor(colFaint).SingleLine()
					})
				})

			ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
				a.navItem(c, "首页", iconHome, page == PageHome, func() { a.setPage(PageHome) })
				a.navItem(c, "传输", iconSend, page == PageTransfer, func() { a.setPage(PageTransfer) })
				a.navItem(c, "设置", iconSettings, page == PageSettings, func() { a.setPage(PageSettings) })
			})

			ui.Box(c).Grow(1)

			a.sessionCard(c, roomCode, state)
		})
}

func (a *App) navItem(c *ui.Context, label string, icon *ui.SVG, selected bool, onClick func()) {
	t := c.Theme()
	row := ui.ButtonBase(c).Key("nav_"+label).Label(label).
		FillWidth().Justify(ui.Start).AlignItems(ui.Center).
		Padding(t.Space(2), t.Space(2.5)).Gap(t.Space(2.5)).Radius(t.Radius).
		Background(ui.Color{}).TextColor(colMuted)
	if selected {
		row.Background(colLine.Alpha(0.9)).TextColor(colText)
	} else if row.Hovered() {
		row.Background(colLine.Alpha(0.45))
	}
	row.Children(func() {
		ui.Icon(c, icon).Size(t.Space(4), t.Space(4)).TextColor(colMuted)
		ui.Text(c, label).FontSize(12.5).SingleLine()
	})
	if row.Clicked() {
		onClick()
	}
}

func (a *App) tabBar(c *ui.Context) {
	t := c.Theme()
	a.mu.RLock()
	page := a.page
	a.mu.RUnlock()

	ui.Row(c).FillWidth().Height(t.Space(15)).
		Background(colAppBg).Border(1, colLine).
		Padding(t.Space(1.5), t.Space(3)).Gap(t.Space(2)).
		Children(func() {
			a.tabItem(c, "首页", iconHome, page == PageHome, PageHome)
			a.tabItem(c, "传输", iconSend, page == PageTransfer, PageTransfer)
			a.tabItem(c, "设置", iconSettings, page == PageSettings, PageSettings)
		})
}

func (a *App) tabItem(c *ui.Context, label string, icon *ui.SVG, selected bool, p Page) {
	t := c.Theme()
	item := ui.ButtonBase(c).Key("tab_" + label).Label(label).
		Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(t.Space(1)).
		Padding(t.Space(1.5)).Radius(t.Radius).Background(ui.Color{})
	color := colMuted
	if selected {
		color = colAccent
	}
	item.Children(func() {
		ui.Icon(c, icon).Size(t.Space(4.5), t.Space(4.5)).TextColor(color)
		ui.Text(c, label).FontSize(10.5).TextColor(color).SingleLine()
	})
	if item.Clicked() {
		a.setPage(p)
	}
}

// sessionCard summarises the running session at the bottom of the sidebar.
func (a *App) sessionCard(c *ui.Context, roomCode string, state session.State) {
	t := c.Theme()
	if roomCode == "" {
		ui.Column(c).FillWidth().Padding(t.Space(3)).Radius(t.Radius).
			Background(colCard).Border(1, colLine).Gap(t.Space(1)).
			Children(func() {
				ui.Text(c, "还没有连接").FontSize(11).TextColor(colMuted)
				ui.Text(c, "新建取件码，或输入对方的号码").FontSize(10).TextColor(colFaint).Wrap()
			})
		return
	}

	dot := colFaint
	switch state {
	case session.StateConnected:
		dot = colSuccess
	case session.StateWaiting, session.StateConnecting:
		dot = colWarn
	case session.StateFailed:
		dot = ui.Hex("#f87171")
	}

	ui.Column(c).FillWidth().Padding(t.Space(3)).Radius(t.Radius).
		Background(colCard).Border(1, colLine).Gap(t.Space(1.5)).
		Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(t.Space(1.5)).Children(func() {
				ui.Box(c).Size(t.Space(1.5), t.Space(1.5)).Radius(t.Space(1.5)).Background(dot)
				ui.Text(c, stateText(state)).FontSize(11).TextColor(colMuted).SingleLine()
			})
			ui.Text(c, spaced(roomCode)).FontSize(15).Bold().TextColor(colText).SingleLine()
		})
}

// ------------------------------------------------------------------ actions

// createRoom asks the server for a code and starts a sender session.
func (a *App) createRoom() {
	a.mu.Lock()
	if a.busy || a.sess != nil {
		a.mu.Unlock()
		return
	}
	a.busy = true
	a.status = ""
	server := a.server
	a.mu.Unlock()
	a.refresh()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		code, err := session.CreateRoom(ctx, server)

		a.mu.Lock()
		a.busy = false
		if err != nil {
			a.status, a.statusErr = err.Error(), true
			a.mu.Unlock()
			a.refresh()
			return
		}
		a.roomCode = code
		a.codeInput = code
		a.roleIdx = 0
		a.page = PageTransfer
		a.mu.Unlock()
		a.refresh()

		if err := a.startSession(protocol.RoleSender, code, server); err != nil {
			a.setStatus(err.Error(), true)
		}
	}()
}

// joinRoom checks the code and starts a receiver session.
func (a *App) joinRoom(code string) {
	a.mu.Lock()
	if a.busy || a.sess != nil {
		a.mu.Unlock()
		return
	}
	code = strings.TrimSpace(code)
	server := a.server
	if code == "" {
		a.status, a.statusErr = "请先输入对方给的取件码", true
		a.mu.Unlock()
		a.refresh()
		return
	}
	a.busy = true
	a.status = ""
	a.mu.Unlock()
	a.refresh()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		room, err := session.FetchRoom(ctx, server, code)

		problem := ""
		switch {
		case err != nil:
			problem = err.Error()
		case !room.Exists:
			problem = "取件码不存在或已过期，请向对方确认"
		case !room.Ready("receiver"):
			problem = "房间里已经有一台接收端了，请让对方重新生成取件码"
		}

		a.mu.Lock()
		a.busy = false
		if problem != "" {
			a.status, a.statusErr = problem, true
			a.mu.Unlock()
			a.refresh()
			return
		}
		a.roomCode = code
		a.roleIdx = 1
		a.page = PageTransfer
		a.mu.Unlock()
		a.refresh()

		if err := a.startSession(protocol.RoleReceiver, code, server); err != nil {
			a.setStatus(err.Error(), true)
		}
	}()
}

// setRole switches between sending (0) and receiving (1).
func (a *App) setRole(idx int) {
	a.mu.Lock()
	a.roleIdx = idx
	a.status, a.statusErr = "", false
	a.mu.Unlock()
	a.refresh()
}

// startSession wires a session and its transfer manager.
func (a *App) startSession(role protocol.Role, code, server string) error {
	a.mu.Lock()
	a.closeLocked()
	if role == protocol.RoleReceiver {
		a.roleIdx = 1
	} else {
		a.roleIdx = 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	sess := session.New(session.Config{
		Server: server,
		Code:   code,
		Role:   role,
		Events: session.Events{
			State: func(state session.State, detail string) {
				a.mu.Lock()
				a.state = state
				a.detail = detail
				a.mu.Unlock()
				if detail != "" {
					a.addLog("%s", detail)
				}
				a.refresh()
			},
			Log: func(line string) { a.addLog("%s", line) },
			Close: func() {
				a.mu.Lock()
				a.state = session.StateClosed
				a.detail = "连接已断开"
				a.mu.Unlock()
				a.refresh()
			},
			Message: func(msg protocol.Message) { a.onPeerMessage(msg) },
			Binary:  func(data []byte) { a.onPeerBinary(data) },
			Error:   func(err error) { a.setStatus(err.Error(), true) },
		},
	})

	mgr := transfer.New(sess, newSink(a.downloadDir), transfer.Events{
		Progress: func(p transfer.Progress) {
			a.mu.Lock()
			if _, seen := a.progress[p.ID]; !seen {
				a.order = append(a.order, p.ID)
			}
			a.progress[p.ID] = p
			a.mu.Unlock()
			a.refresh()
		},
		Started: func(meta protocol.FileMetadata) {
			a.addLog("正在接收 %s（%s）", meta.Name, transfer.HumanBytes(meta.Size))
		},
		Sent: func(meta protocol.FileMetadata) {
			a.record(historyItem{Name: meta.Name, Size: meta.Size, At: time.Now()})
		},
		Finished: func(meta protocol.FileMetadata, path string) {
			a.record(historyItem{Name: meta.Name, Size: meta.Size, Incoming: true, At: time.Now(), Path: path})
			a.setStatus("已收到 "+meta.Name, false)
		},
		Log:   func(line string) { a.addLog("%s", line) },
		Error: func(err error) { a.setStatus(err.Error(), true) },
	})

	a.sess, a.mgr = sess, mgr
	a.state, a.detail = session.StateSignaling, "正在连接信令服务器…"
	a.progress = map[string]transfer.Progress{}
	a.order = nil
	a.mu.Unlock()
	a.refresh()

	if err := sess.Start(ctx); err != nil {
		a.closeSession()
		return err
	}
	return nil
}

func (a *App) record(item historyItem) {
	a.mu.Lock()
	a.history = append([]historyItem{item}, a.history...)
	if len(a.history) > 30 {
		a.history = a.history[:30]
	}
	a.mu.Unlock()
	a.refresh()
}

func (a *App) onPeerMessage(msg protocol.Message) {
	mgr := a.manager()
	if mgr == nil {
		return
	}
	switch msg.Channel {
	case protocol.ChannelFile, "":
		mgr.HandleMessage(msg)
	case protocol.ChannelText:
		if msg.Type == protocol.MsgTextSync {
			var payload protocol.TextSync
			if err := msg.Decode(&payload); err == nil && payload.Text != "" {
				a.addLog("对方发来一段文本（%d 字）", len([]rune(payload.Text)))
			}
		}
	default:
		a.addLog("收到 %s 通道的 %s 消息", msg.Channel, msg.Type)
	}
}

func (a *App) onPeerBinary(data []byte) {
	if mgr := a.manager(); mgr != nil {
		mgr.HandleBinary(data)
	}
}

// closeSession drops the current session, if any.
func (a *App) closeSession() {
	a.mu.Lock()
	a.closeLocked()
	a.mu.Unlock()
	a.refresh()
}

func (a *App) closeLocked() {
	for _, src := range a.picked {
		_ = src.Close()
	}
	a.picked = nil
	if a.sess != nil {
		_ = a.sess.Close()
	}
	if a.cancel != nil {
		a.cancel()
	}
	a.sess, a.mgr, a.cancel = nil, nil, nil
	a.state, a.detail = session.StateClosed, ""
	a.roomCode = ""
	a.progress = map[string]transfer.Progress{}
	a.order = nil
}

// disconnect is the button on the transfer page.
func (a *App) disconnect() {
	a.closeSession()
	a.setPage(PageHome)
}

// pickFiles lets the user choose files to send.
func (a *App) pickFiles() {
	a.mu.Lock()
	if a.picking {
		a.mu.Unlock()
		return
	}
	a.picking = true
	a.mu.Unlock()
	a.refresh()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		sources, err := transfer.PickSources(ctx)

		a.mu.Lock()
		a.picking = false
		if err != nil {
			a.status, a.statusErr = err.Error(), true
			a.mu.Unlock()
			a.refresh()
			return
		}
		a.picked = append(a.picked, sources...)
		a.status = ""
		a.mu.Unlock()
		a.refresh()
	}()
}

// sendPicked streams the chosen files over the data channel.
func (a *App) sendPicked() {
	a.mu.Lock()
	mgr := a.mgr
	sources := a.picked
	a.picked = nil
	a.mu.Unlock()
	if mgr == nil || len(sources) == 0 {
		return
	}
	go func() {
		if err := mgr.Send(context.Background(), sources); err != nil {
			a.setStatus(err.Error(), true)
		}
	}()
}

func (a *App) clearPicked() {
	a.mu.Lock()
	for _, src := range a.picked {
		_ = src.Close()
	}
	a.picked = nil
	a.mu.Unlock()
	a.refresh()
}

func (a *App) copyCode() {
	a.mu.RLock()
	code := a.roomCode
	a.mu.RUnlock()
	if code == "" {
		return
	}
	mygo.Clipboard.WriteText(code)
	a.setStatus("取件码已复制", false)
}

func (a *App) saveServer(value string) {
	a.mu.Lock()
	a.server = strings.TrimSpace(value)
	cfg := a.settingsLocked()
	a.mu.Unlock()
	saveSettings(cfg)
}

func (a *App) setDark(dark bool) {
	a.mu.Lock()
	a.dark = dark
	cfg := a.settingsLocked()
	a.mu.Unlock()
	saveSettings(cfg)
	a.applyAppearance()
	a.refresh()
}

func (a *App) settingsLocked() Settings {
	return Settings{Server: a.server, DownloadDir: a.downloadDir, Dark: a.dark}
}

func (a *App) setDownloadDir(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	a.mu.Lock()
	a.downloadDir = dir
	cfg := a.settingsLocked()
	a.mu.Unlock()
	saveSettings(cfg)
	a.setStatus("下载目录已更新", false)
}

// stateText is the user-facing name of a session state.
// roleName reports the role of the running session, or the one picked on the
// home page before a session starts.
func (a *App) roleName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.sess != nil {
		if a.sess.Role() == protocol.RoleReceiver {
			return "接收方"
		}
		return "发送方"
	}
	return roleName(a.roleIdx)
}

func stateText(state session.State) string {
	switch state {
	case session.StateSignaling:
		return "连接信令服务器"
	case session.StateWaiting:
		return "等待对方加入"
	case session.StateConnecting:
		return "正在建立 P2P 连接"
	case session.StateConnected:
		return "已连接"
	case session.StateFailed:
		return "连接失败"
	default:
		return "未连接"
	}
}

// spaced puts a gap between the characters of a code, so it reads as a code
// and not as a number.
func spaced(code string) string {
	runes := []rune(code)
	if len(runes) <= 3 {
		return string(runes)
	}
	var b strings.Builder
	for i, r := range runes {
		if i > 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
