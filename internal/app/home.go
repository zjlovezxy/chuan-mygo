package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/zjlovezxy/chuan-mygo/internal/session"
	"github.com/zjlovezxy/chuan-mygo/internal/transfer"
)

// cardBox is the shared panel look: a rounded surface on the page body.
func (a *App) cardBox(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Column(c).FillWidth().Padding(t.Space(4.5)).Radius(t.Space(3)).
		Background(colCard).Border(1, colLine).Gap(t.Space(3.5))
}

// sectionLabel is a small caps heading above a block.
func (a *App) sectionLabel(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(10.5).Bold().LetterSpacing(0.7).TextColor(colFaint)
}

func (a *App) homePage(c *ui.Context) {
	t := c.Theme()
	top, side := a.pagePadding(c)
	w, _ := c.Size()

	a.mu.RLock()
	code := a.codeInput
	roomCode := a.roomCode
	state := a.state
	detail := a.detail
	busy := a.busy
	picking := a.picking
	status, statusErr := a.status, a.statusErr
	server := a.server
	history := append([]historyItem(nil), a.history...)
	a.mu.RUnlock()
	role := a.roleIdx

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(top, side, top, side).Gap(t.Space(4.5)).Children(func() {
			// Header.
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
				ui.Box(c).Size(t.Space(11), t.Space(11)).Radius(t.Space(3)).
					Background(colAccent).Center().Children(func() {
					ui.Text(c, "传").FontSize(21).Bold().TextColor(ui.Hex("#ffffff"))
				})
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, "文件快传").FontSize(21).Bold().TextColor(colText)
					ui.Text(c, "取件码配对，点对点直传，文件不经过服务器").FontSize(11.5).TextColor(colMuted).Wrap()
				})
				ui.Box(c).Grow(1)
				if w >= 820 {
					ui.Text(c, hostOf(server)).FontSize(10.5).TextColor(colFaint).
						Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Space(5)).
						Background(colWell).Border(1, colLine).SingleLine()
				}
			})

			// Role switch.
			ui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
				a.roleTab(c, "发送文件", "把本机文件发给对方", iconSend, role == 0, func() { a.setRole(0) })
				a.roleTab(c, "接收文件", "用对方的取件码接收", iconReceive, role == 1, func() { a.setRole(1) })
			})

			// Main panel.
			panel := a.cardBox(c)
			panel.Children(func() {
				if role == 0 {
					a.senderPanel(c, roomCode, state, detail, busy)
				} else {
					a.receiverPanel(c, code, busy)
				}

				if status != "" {
					color := colMuted
					if statusErr {
						color = ui.Hex("#f87171")
					}
					ui.Text(c, status).FontSize(11.5).TextColor(color).Wrap()
				}
			})

			if picking {
				ui.Text(c, "正在等待文件选择…").FontSize(11).TextColor(colMuted)
			}

			// History.
			if len(history) > 0 {
				ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
					a.sectionLabel(c, "最近传输")
					box := a.cardBox(c)
					box.Padding(t.Space(2)).Children(func() {
						for _, item := range history {
							a.historyRow(c, item)
						}
					})
				})
			}

			// How it works.
			ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
				a.sectionLabel(c, "怎么用")
				box := a.cardBox(c)
				box.Children(func() {
					a.step(c, "1", "发送方点「生成取件码」")
					a.step(c, "2", "把 6 位号码告诉对方")
					a.step(c, "3", "对方输入号码后即可互传文件")
				})
			})
		})
	})
}

func (a *App) roleTab(c *ui.Context, title, subtitle string, icon *ui.SVG, selected bool, onClick func()) {
	t := c.Theme()
	row := ui.ButtonBase(c).Key("role_"+title).Label(title).
		Grow(1).FillWidth().Justify(ui.Start).AlignItems(ui.Center).
		Padding(t.Space(3), t.Space(3.5)).Gap(t.Space(2.5)).Radius(t.Space(2.5)).
		Background(colCard).Border(1, colLine)
	if selected {
		row.Background(colAccentDim).Border(1, colAccent)
	} else if row.Hovered() {
		row.Background(colWell)
	}
	row.Children(func() {
		tile := colWell
		tint := colMuted
		if selected {
			tile = colAccent
			tint = ui.Hex("#ffffff")
		}
		ui.Box(c).Size(t.Space(8), t.Space(8)).Radius(t.Space(2)).Background(tile).Center().
			Children(func() {
				ui.Icon(c, icon).Size(t.Space(4.5), t.Space(4.5)).TextColor(tint)
			})
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(13).Bold().TextColor(colText).SingleLine()
			ui.Text(c, subtitle).FontSize(10.5).TextColor(colMuted).SingleLine()
		})
	})
	if row.Clicked() {
		onClick()
	}
}

func (a *App) senderPanel(c *ui.Context, roomCode string, state session.State, detail string, busy bool) {
	t := c.Theme()

	if roomCode == "" {
		ui.Text(c, "生成一个 6 位取件码，对方在另一台设备上输入它就能连上你。").FontSize(11.5).TextColor(colMuted).Wrap()
		btn := ui.PrimaryButton(c, "生成取件码").Height(t.Space(11)).Radius(t.Space(2))
		if busy {
			btn.Disabled(true).Label("正在创建…")
		}
		if btn.Clicked() && !busy {
			a.createRoom()
		}
		return
	}

	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, "取件码").FontSize(10.5).TextColor(colFaint)
			ui.Text(c, spaced(roomCode)).FontSize(32).Bold().TextColor(colText).SingleLine()
		})
		ui.Box(c).Grow(1)
		if ui.Button(c, "复制").Height(t.Space(9)).Clicked() {
			a.copyCode()
		}
	})

	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		dot := colWarn
		if state == session.StateConnected {
			dot = colSuccess
		}
		ui.Box(c).Size(t.Space(1.5), t.Space(1.5)).Radius(t.Space(1.5)).Background(dot)
		ui.Text(c, stateText(state)).FontSize(11.5).TextColor(colMuted)
		if detail != "" {
			ui.Text(c, "·").FontSize(11.5).TextColor(colFaint)
			ui.Text(c, detail).FontSize(11.5).TextColor(colFaint).Ellipsis("…").SingleLine()
		}
	})
}

func (a *App) receiverPanel(c *ui.Context, code string, busy bool) {
	t := c.Theme()

	ui.Column(c).Gap(t.Space(1)).Children(func() {
		ui.Text(c, "取件码").FontSize(10.5).TextColor(colFaint)
		ui.TextInput(c, &code).FontSize(24).Height(t.Space(13)).Radius(t.Space(2)).
			Placeholder("6 位数字")
	})
	a.codeInput = code

	btn := ui.PrimaryButton(c, "连接并接收").Height(t.Space(11)).Radius(t.Space(2))
	if busy {
		btn.Disabled(true).Label("正在连接…")
	}
	if btn.Clicked() && !busy {
		a.joinRoom(strings.TrimSpace(code))
	}
}

func (a *App) historyRow(c *ui.Context, item historyItem) {
	t := c.Theme()
	row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2.5)).
		Padding(t.Space(2), t.Space(2)).Radius(t.Radius)
	if row.Hovered() {
		row.Background(colWell)
	}
	row.Children(func() {
		icon := iconFile
		color := colMuted
		if item.Incoming {
			icon = iconReceive
			color = colSuccess
		} else {
			icon = iconSend
			color = colAccent
		}
		ui.Box(c).Size(t.Space(7), t.Space(7)).Radius(t.Space(1.5)).Background(colWell).Center().
			Children(func() {
				ui.Icon(c, icon).Size(t.Space(3.8), t.Space(3.8)).TextColor(color)
			})
		ui.Column(c).Gap(1).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, item.Name).FontSize(12).TextColor(colText).SingleLine().Ellipsis("…")
			ui.Text(c, fmt.Sprintf("%s · %s · %s", direction(item), transfer.HumanBytes(item.Size), item.At.Format("01-02 15:04"))).
				FontSize(10).TextColor(colFaint).SingleLine()
		})
	})
}

func (a *App) step(c *ui.Context, n, text string) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Box(c).Size(t.Space(5.5), t.Space(5.5)).Radius(t.Space(2.75)).Background(colWell).Center().
			Children(func() {
				ui.Text(c, n).FontSize(10.5).Bold().TextColor(colMuted)
			})
		ui.Text(c, text).FontSize(11.5).TextColor(colMuted)
	})
}

func direction(item historyItem) string {
	if item.Incoming {
		return "接收"
	}
	return "发送"
}

// hostOf shows just the host of a server URL, which is all a user needs to
// tell two servers apart.
func hostOf(server string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(server, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "未设置服务器"
	}
	return s
}
