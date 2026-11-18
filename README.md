# 文件快传 · 原生客户端（MyGo）

用 [mygo](https://github.com/egoist/mygo) 的原生 GPU UI 重写的 [file-transfer-go](https://github.com/MatrixSeven/file-transfer-go)（文件快传 / chuan）客户端，一套 Go 代码编出三端：

| 平台 | 形态 | 产物 |
| --- | --- | --- |
| macOS | mygo 原生窗口（Metal 绘制） | `.dmg` / `.app`（arm64 + amd64） |
| Windows | mygo 原生窗口（D3D11 绘制） | 安装器 / `.exe`（x64 + arm64） |
| Android | WebView 外壳加载同一份 wasm（WebGL2 绘制） | `.apk` |
| 浏览器 | 直接打开静态站，可与上面任一端互传 | 静态文件 |

**协议与网页版完全互通**：同一个 6 位取件码，浏览器和这个客户端之间可以互传文件与文本，走 WebRTC DataChannel 直连；P2P 打不通时服务器中继兜底。

## 为什么是「原生 UI + wasm」

mygo 的原生 UI 不依赖 cgo，同一份 `ui` 代码既能编成桌面窗口，也能编成 `js/wasm` 在 Canvas 上绘制，所以 Android 端不需要另写一套 Compose 界面——WebView 里跑的就是桌面端同一个界面，随窗口宽度自动在「左侧栏」和「底部标签栏」之间切换。

## 快速开始

### 直接用

1. 桌面版：打开应用 → 选择「发送文件」→ 生成取件码 → 把码发给对方。
2. Android / 浏览器：选择「接收文件」→ 输入取件码 → 连接后文件自动接收。
3. 接收位置：桌面版默认 `~/Downloads`（可在设置里改）；Android 版存在应用私有目录（`Android/data/dev.chuan.app/files/Download`，保存后会调 MediaScanner 登记），浏览器版交给浏览器下载。

### 连哪台服务器

默认使用官方信令服务器 `https://transfer.52python.cn`，与网页版共用。也可以在「设置」里换成自建的（本仓库自带，见下）。

### 自建信令服务器

```bash
go run ./cmd/chuan-server -addr :8080 -static build/web
```

- `-addr`：监听地址
- `-static`：浏览器版静态目录（可选；带上就能用 `http://<主机>:8080` 直接打开网页版）
- `-ttl`：房间有效期，默认 1 小时

Docker：

```bash
docker build -t chuan-mygo .
docker run -p 8080:8080 chuan-mygo
```

## 本地构建

```bash
# 依赖
export GOPROXY=https://goproxy.cn,direct   # 国内网络
go install github.com/egoist/mygo/cmd/mygo@v0.8.3

# 图标由代码生成（仓库里不放二进制图片）
go run ./tools/genicon

# 桌面（在目标系统上构建）
mygo build -platform "darwin/arm64"        # macOS
mygo build -platform "windows/amd64"       # Windows

# 浏览器 / Android 用的 wasm 包
./scripts/build-web.sh build/web

# Android（需要 JDK 17 与 Gradle 8.11）
./scripts/build-web.sh android/app/src/main/assets/web
cd android && gradle assembleDebug
```

开发时热重载：

```bash
mygo dev                              # 原生窗口
mygo dev -web                         # 浏览器里调试同一套 UI（没有文件选择桥，选文件会提示）
```

页面离屏渲染（不需要显示器也能检查布局，有 DISPLAY 时会顺带产出 PNG）：

```bash
DISPLAY=:0 CHUAN_SHOTS=/tmp/shots go test ./internal/app/ -run TestRenderPages -v
```

## 持续构建与发布

推送到 `main` 触发 `.github/workflows/build.yml`，五个任务并行：

| 任务 | 产物（Artifact） | 内容 |
| --- | --- | --- |
| `go vet + wasm build` | — | `go vet`、`go test`（xvfb 下跑渲染测试）、wasm 编译自查 |
| `macOS` | `chuan-macos` | `文件快传.app` + `.dmg`（arm64、amd64） |
| `Windows` | `chuan-windows` | `文件快传.exe` + NSIS 安装器（x64、arm64） |
| `Browser (js/wasm)` | `chuan-web` | `index.html` + `app.wasm` 静态站，可直接扔到任意静态托管 |
| `Android APK` | `chuan-android` | `chuan-mygo-0.1.0-debug.apk` |

发布：在 Actions 里手动运行 `build`，把 `release` 输入填成 `v0.1.0`（或直接推 `v*` 标签），`Publish release` 会把上面四个产物平铺后挂到 GitHub Releases。

> 本仓库的提交是通过 GitHub API 推送的（本机没有 git 凭据），文件的落盘权限是 `100644`，所以工作流里一律用 `bash scripts/build-web.sh` 调用脚本，没有依赖可执行位。

## 目录

```
cmd/chuan/            桌面客户端入口（mygo 窗口）
cmd/chuan-server/     信令服务器入口
internal/protocol/    信令与数据通道消息（与网页版一致）
internal/netlink/     WebSocket：桌面 gorilla，wasm 用浏览器 WebSocket
internal/peer/        WebRTC：桌面 pion，wasm 用 RTCPeerConnection
internal/transfer/    256 KiB 分块收发、背压、落盘/浏览器保存
internal/session/     房间 HTTP API + 信令状态机
internal/server/      兼容 file-transfer-go 的信令服务器
internal/app/         mygo 原生 UI（首页 / 传输 / 设置）
web/                  浏览器与 Android 共用外壳（index.html + bridge.js）
android/              Kotlin WebView 外壳（本地 asset 服务 + 文件选择/保存）
docs/PROTOCOL.md      协议说明
```

## 实现说明

- **传输**：与网页版同一套 `file-metadata` → `file-chunk-info` + 二进制块 → `file-complete` 流程，256 KiB 一块，`BufferedAmount` 超过 2 MiB 就等，2 秒滑动窗口算速度。
- **不逐块 ACK**：SCTP 保证有序可靠，网页版也一样，所以没有 CRC 校验与重传。
- **大文件**：桌面端按 `ReadAt` 分块读取，不把整个文件读进内存；浏览器端受内存限制，单文件上限 2 GiB。
- **P2P 直连**：默认 3 个公共 STUN，没有 TURN；对称 NAT 下会退回服务器中继（`/api/ws/relay`）。

## 许可

MIT，见 [LICENSE](LICENSE)。上游 [file-transfer-go](https://github.com/MatrixSeven/file-transfer-go) 亦为 MIT。
