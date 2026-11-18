package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/zjlovezxy/chuan-mygo/internal/session"
)

// Version is the release the build reports.
const Version = "0.1.0"

// RepoURL is where the source lives.
const RepoURL = "https://github.com/zjlovezxy/chuan-mygo"

func (a *App) settingsPage(c *ui.Context) {
	t := c.Theme()
	top, side := a.pagePadding(c)

	a.mu.RLock()
	server := a.server
	dir := a.downloadDir
	dark := a.dark
	status, statusErr := a.status, a.statusErr
	a.mu.RUnlock()

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(top, side, top, side).Gap(t.Space(4)).Children(func() {
			ui.Text(c, "设置").FontSize(21).Bold().TextColor(colText)
			ui.Text(c, "服务器、下载位置与外观").FontSize(11.5).TextColor(colMuted)

			// Server.
			box := a.cardBox(c)
			box.Children(func() {
				a.sectionLabel(c, "信令服务器")
				ui.Text(c, "客户端通过它交换配对与握手信息，文件本身走 P2P 直连。").FontSize(11).TextColor(colMuted).Wrap()
				ui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
					ui.TextInput(c, &server).Grow(1).Height(t.Space(11)).Radius(t.Space(1.5)).
						Placeholder(session.DefaultServer)
					if ui.Button(c, "保存").Height(t.Space(11)).Clicked() {
						a.saveServer(server)
					}
				})
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
					ui.Text(c, "也可以连官方服务器").FontSize(10).TextColor(colFaint)
					if ui.Button(c, "用官方地址").Height(t.Space(8)).Clicked() {
						a.saveServer(session.DefaultServer)
					}
				})
			})

			// Downloads.
			dl := a.cardBox(c)
			dl.Children(func() {
				a.sectionLabel(c, "接收位置")
				if canChooseFolder() {
					ui.Text(c, "对方发来的文件会保存到这个目录。").FontSize(11).TextColor(colMuted).Wrap()
					ui.Row(c).FillWidth().Gap(t.Space(2)).Children(func() {
						ui.TextInput(c, &dir).Grow(1).Height(t.Space(11)).Radius(t.Space(1.5)).
							Placeholder(defaultDownloadDir())
						if ui.Button(c, "选择…").Height(t.Space(11)).Clicked() {
							if picked, err := chooseFolder(dir); err != nil {
								a.setStatus(err.Error(), true)
							} else if picked != "" {
								a.setDownloadDir(picked)
							}
						}
						if ui.Button(c, "保存").Height(t.Space(11)).Clicked() {
							a.setDownloadDir(dir)
						}
					})
				} else {
					ui.Text(c, "当前在浏览器里运行：收到的文件交给浏览器下载，不落 P2P 之外的地方。").
						FontSize(11).TextColor(colMuted).Wrap()
				}
			})

			// Appearance.
			ap := a.cardBox(c)
			ap.Children(func() {
				a.sectionLabel(c, "外观")
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(3)).Children(func() {
					ui.Column(c).Gap(1).Grow(1).Shrink(1).MinWidth(0).Children(func() {
						ui.Text(c, "深色界面").FontSize(12).TextColor(colText)
						ui.Text(c, "zinc 底色配玫红强调色").FontSize(10.5).TextColor(colFaint).SingleLine()
					})
					on := dark
					sw := ui.Switch(c, &on)
					if on != dark {
						a.setDark(on)
					}
					_ = sw
				})
			})

			// About.
			ab := a.cardBox(c)
			ab.Children(func() {
				a.sectionLabel(c, "关于")
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
					ui.Text(c, "文件快传 "+Version).FontSize(12).TextColor(colText)
					ui.Box(c).Grow(1)
					if ui.Button(c, "项目主页").Height(t.Space(9)).Clicked() {
						if err := mygo.Shell.OpenExternal(RepoURL); err != nil {
							a.setStatus(err.Error(), true)
						}
					}
				})
				ui.Text(c, "协议与网页版 chuan 完全兼容：同一个取件码可以在浏览器和这个客户端之间互传文件。").
					FontSize(10.5).TextColor(colFaint).Wrap()
			})

			if status != "" {
				color := colMuted
				if statusErr {
					color = ui.Hex("#f87171")
				}
				ui.Text(c, status).FontSize(11).TextColor(color).Wrap()
			}
		})
	})
}
