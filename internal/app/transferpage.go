package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/zjlovezxy/chuan-mygo/internal/session"
	"github.com/zjlovezxy/chuan-mygo/internal/transfer"
)

func (a *App) transferPage(c *ui.Context) {
	t := c.Theme()
	top, side := a.pagePadding(c)

	a.mu.RLock()
	roomCode := a.roomCode
	state := a.state
	detail := a.detail
	hasSession := a.sess != nil
	server := a.server
	dir := a.downloadDir
	picked := append([]transfer.Source(nil), a.picked...)
	progress := make([]transfer.Progress, 0, len(a.order))
	for _, id := range a.order {
		if p, ok := a.progress[id]; ok {
			progress = append(progress, p)
		}
	}
	logs := append([]string(nil), a.logs...)
	a.mu.RUnlock()
	role := a.roleName()

	if !hasSession && roomCode == "" {
		ui.Column(c).Fill().Center().Gap(t.Space(3)).Children(func() {
			ui.Box(c).Size(t.Space(14), t.Space(14)).Radius(t.Space(4)).Background(colWell).Center().
				Children(func() {
					ui.Icon(c, iconLink).Size(t.Space(7), t.Space(7)).TextColor(colFaint)
				})
			ui.Text(c, "当前没有传输会话").FontSize(14).TextColor(colMuted)
			ui.Text(c, "回到首页生成取件码，或输入对方的号码").FontSize(11).TextColor(colFaint)
			if ui.PrimaryButton(c, "回到首页").Height(t.Space(10)).Clicked() {
				a.setPage(PageHome)
			}
		})
		return
	}

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(top, side, top, side).Gap(t.Space(4)).Children(func() {
			// Status header.
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
				dot := colWarn
				switch state {
				case session.StateConnected:
					dot = colSuccess
				case session.StateFailed:
					dot = ui.Hex("#f87171")
				case session.StateClosed:
					dot = colFaint
				}
				ui.Box(c).Size(t.Space(2.5), t.Space(2.5)).Radius(t.Space(2.5)).Background(dot)
				ui.Column(c).Gap(1).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, stateText(state)).FontSize(18).Bold().TextColor(colText).SingleLine()
					if detail != "" {
						ui.Text(c, detail).FontSize(11).TextColor(colMuted).SingleLine().Ellipsis("…")
					}
				})
				if ui.Button(c, "断开").Height(t.Space(9)).Clicked() {
					a.disconnect()
				}
			})

			// Room facts.
			box := a.cardBox(c)
			box.Children(func() {
				ui.Row(c).FillWidth().Gap(t.Space(4)).Children(func() {
					a.fact(c, "取件码", spaced(roomCode))
					a.fact(c, "身份", role)
					a.fact(c, "服务器", hostOf(server))
				})
				ui.Divider(c)
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
					ui.Icon(c, iconFolder).Size(t.Space(3.6), t.Space(3.6)).TextColor(colFaint)
					ui.Text(c, "保存到 "+dir).FontSize(10.5).TextColor(colFaint).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
					if openFolderButtonAvailable() {
						if ui.Button(c, "打开").Height(t.Space(8)).Clicked() {
							if err := openFolder(dir); err != nil {
								a.setStatus(err.Error(), true)
							}
						}
					}
				})
			})

			// Sender's files.
			if role == "发送方" {
				sendBox := a.cardBox(c)
				sendBox.Children(func() {
					a.sectionLabel(c, "要发送的文件")
					if len(picked) == 0 {
						ui.Text(c, "还没有选择文件。连上之后就能选文件发送。").FontSize(11).TextColor(colMuted).Wrap()
					} else {
						ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
							var total int64
							for _, src := range picked {
								total += src.Size()
								a.pickedRow(c, src)
							}
							ui.Text(c, fmt.Sprintf("共 %d 个文件，%s", len(picked), transfer.HumanBytes(total))).
								FontSize(10.5).TextColor(colFaint)
						})
					}
					ui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
						if ui.Button(c, "选择文件").Height(t.Space(10)).Clicked() {
							a.pickFiles()
						}
						send := ui.PrimaryButton(c, "开始发送").Height(t.Space(10))
						if len(picked) == 0 {
							send.Disabled(true)
						}
						if send.Clicked() && len(picked) > 0 {
							a.sendPicked()
						}
						if len(picked) > 0 {
							if ui.Button(c, "清空").Height(t.Space(10)).Clicked() {
								a.clearPicked()
							}
						}
					})
				})
			}

			// Progress.
			if len(progress) > 0 {
				progBox := a.cardBox(c)
				progBox.Children(func() {
					a.sectionLabel(c, "正在传输")
					for _, p := range progress {
						a.progressRow(c, p)
					}
				})
			}

			// Logs.
			if len(logs) > 0 {
				logBox := a.cardBox(c)
				logBox.Children(func() {
					a.sectionLabel(c, "日志")
					start := 0
					if len(logs) > 40 {
						start = len(logs) - 40
					}
					ui.Column(c).FillWidth().Gap(2).Children(func() {
						for _, line := range logs[start:] {
							ui.Text(c, line).FontSize(10).TextColor(colFaint).SingleLine().Ellipsis("…")
						}
					})
				})
			}
		})
	})
}

func (a *App) fact(c *ui.Context, label, value string) {
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(colFaint)
		ui.Text(c, value).FontSize(12.5).TextColor(colText).SingleLine().Ellipsis("…")
	})
}

func (a *App) pickedRow(c *ui.Context, src transfer.Source) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Icon(c, iconFile).Size(t.Space(3.6), t.Space(3.6)).TextColor(colMuted)
		ui.Text(c, src.Name()).FontSize(11.5).TextColor(colText).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
		ui.Text(c, transfer.HumanBytes(src.Size())).FontSize(10.5).TextColor(colFaint)
	})
}

func (a *App) progressRow(c *ui.Context, p transfer.Progress) {
	t := c.Theme()
	done := p.Fraction() >= 1 && p.Moved >= p.Total
	ui.Column(c).FillWidth().Gap(t.Space(1)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			ui.Text(c, p.Name).FontSize(11.5).TextColor(colText).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
			if done {
				ui.Icon(c, iconCheck).Size(t.Space(3.4), t.Space(3.4)).TextColor(colSuccess)
			}
			ui.Text(c, fmt.Sprintf("%.0f%%", p.Fraction()*100)).FontSize(10.5).TextColor(colMuted)
		})
		ui.Progress(c, float64(p.Fraction())).Height(t.Space(1.5)).Radius(t.Space(1.5))
		parts := fmt.Sprintf("%s / %s", transfer.HumanBytes(p.Moved), transfer.HumanBytes(p.Total))
		if p.Speed > 0 && !done {
			parts += " · " + transfer.HumanBytes(int64(p.Speed)) + "/s"
		}
		if p.Incoming {
			parts += " · 接收"
		} else {
			parts += " · 发送"
		}
		ui.Text(c, parts).FontSize(10).TextColor(colFaint).SingleLine()
	})
}

func roleName(idx int) string {
	if idx == 1 {
		return "接收方"
	}
	return "发送方"
}
