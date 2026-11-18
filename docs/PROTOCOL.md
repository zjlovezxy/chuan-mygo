# 协议说明

客户端与 [file-transfer-go](https://github.com/MatrixSeven/file-transfer-go)（网页版 chuan）完全互通：同一套 HTTP 房间接口、同一个 WebSocket 信令、同一个 DataChannel 上的消息格式。本文是与网页版实现对照后的结果，`internal/protocol` 里是它的 Go 版本。

## 1. 房间

| 接口 | 说明 |
| --- | --- |
| `POST /api/create-room` | 建房，返回 `{"success":true,"code":"AB12CD","message":"房间创建成功"}` |
| `GET /api/room-info?code=AB12CD` | `{"success":true,"exists":true,"sender_online":false,"receiver_online":false,"is_room_full":false,"created_at":"…"}`；不存在时 `{"success":false,"exists":false,"message":"房间不存在或已过期"}` |
| `GET /api/webrtc-room-status?code=…` | 同上，网页版兼容别名 |

- 取件码 6 位，字符集 `123456789ABCDEFGHIJKLMNPQRSTUVWXYZ`（去掉 `0` 与 `O`）。
- 一个房间最多两个连接：一个 `sender`、一个 `receiver`。房满时新连接会先升级 WebSocket，再收到
  `{"type":"error","message":"当前房间人数已满，正在传输中无法加入"}` 然后被关闭。
- 房间默认 1 小时过期，服务端每 5 分钟清理一次。

## 2. 信令（WebSocket）

- 路径：`/api/ws/webrtc`（旧版 `/ws/webrtc` 亦可），查询参数 `code` 与 `role=sender|receiver`。
- 信封：

```json
{ "type": "offer", "from": "client id", "to": "", "payload": { } }
```

`from` 由服务端覆写为它生成的客户端 ID，客户端发的值会被忽略。`payload` 对服务端是不透明数据。

| type | 方向 | payload |
| --- | --- | --- |
| `peer-joined` | 服务端 → 客户端 | `{"role":"sender"}` / `{"role":"receiver"}`：对方进来了 |
| `offer` | sender → receiver | `{"type":"offer","sdp":"…"}` |
| `answer` | receiver → sender | `{"type":"answer","sdp":"…"}` |
| `ice-candidate` | 双向 | `{"candidate":"…","sdpMid":"0","sdpMLineIndex":0,"usernameFragment":"…"}`，空 candidate 表示收集结束 |
| `disconnection` | 服务端 → 客户端 | `{"role":"sender","message":"对方已停止传输"}` |
| `sync-request` | 双向 | 状态同步（网页版用于重连后对齐 UI，可忽略） |
| `relay-request` | 双向 | `{"reason":"P2P连接失败"}`，请求改走中继 |
| `error` | 服务端 → 客户端 | `{"message":"…"}` |

连接流程：

1. 两端各自连上信令 WebSocket。
2. 先到的一方等待；后到的一方进来时，服务端向双方（或向在场的一方）推 `peer-joined`。
3. **sender 收到 `peer-joined(role=receiver)` 后建 `RTCPeerConnection` 并发送 `offer`**；receiver 收到 `peer-joined(role=sender)` 时只建连接等 offer。
4. receiver 回 `answer`；双方交换 `ice-candidate`，直到 DataChannel 打开。
5. 15 秒内没连上就降级中继。

## 3. 数据通道

- 只建立一个 DataChannel：**label 固定为 `shared-channel`，`ordered: true`**，由 sender 创建，receiver 通过 `ondatachannel` 拿到。
- 通道里跑三种逻辑业务，用 JSON 消息的 `channel` 字段区分：`file-transfer`、`text-transfer`、`chat`。
- JSON 文本帧＝控制消息，二进制帧＝文件块（靠上一条 `file-chunk-info` 定位）。
- **没有逐块 ACK，没有 CRC32**：SCTP 保证可靠有序，网页版也是这样。

### 文件发送

```
{ "type": "file-metadata",  "channel": "file-transfer", "payload": {"id":"file_1730_ab12cd34","name":"a.zip","size":123456789,"type":"application/zip"} }
{ "type": "file-chunk-info","channel": "file-transfer", "payload": {"fileId":"file_…","chunkIndex":0,"totalChunks":471} }
<binary 256 KiB>
{ "type": "file-chunk-info", … "chunkIndex":1 }
<binary 256 KiB>
…
{ "type": "file-complete",  "channel": "file-transfer", "payload": {"fileId":"file_…"} }
```

- 分块 256 KiB；`BufferedAmount > 2 MiB` 时暂停发送（背压）。
- 进度与速度只在本地计算（2 秒滑动窗口、1 秒节流），不走协议。
- 其它可选消息：`file-list`（发送端列出待发文件）、`file-request`（`{"fileId","fileName"}`，请求重发）、`file-chunk-ack`（历史遗留，收到忽略）。

### 文本

```
{ "type": "text-sync",  "channel": "text-transfer", "payload": {"text":"…"} }
{ "type": "text-typing","channel": "text-transfer", "payload": {"typing":true} }
```

## 4. 中继兜底

- 路径：`/api/ws/relay`（旧版 `/ws/relay`），参数同样是 `code` 与 `role`。
- 服务端自己是透明的：把收到的文本帧 / 二进制帧原样转发给房间里的另一方，客户端在两端分别用 `relay-data` / `relay-binary` 包一层，配合 `relay-ready`、`relay-peer-joined`、`relay-peer-left`、`relay-ping` / `relay-pong` 维护连接状态。
- 本仓库的 `internal/server` 实现了转发与 `relay-ready`/`relay-peer-joined`/`relay-peer-left` 通知；客户端 v0.1 只记录「对方请求中继」的日志，尚未自动切换。

## 5. 与网页版互通的注意事项

- 网页版在 `peer-joined` 后 15 秒等 P2P，超时自动发 `relay-request`；客户端如果只支持 P2P，需要在房间页提示「网络无法直连」。
- 取件码输入不区分大小写展示，但协议里是大写字符串，比较时按原样（服务端按原样索引）。
- 文件名字段不做 URL 编码，直接放 UTF-8；接收端负责清洗路径分隔符（`internal/transfer/SanitizeName`）。
