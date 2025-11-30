# PeerPigeon

PeerPigeon 是一个轻量的 WebSocket 信令与节点发现服务器，支持可选的 Hub 模式，通过与其他 Hub 建立引导连接（bootstrap）实现跨网络的节点感知。

## 特性
- 提供用于节点注册与信令交互的 WebSocket 端点
- 提供健康与运行统计的 HTTP 端点
- 在网络命名空间内进行节点发现广播
- 可选 Hub 模式，支持连接其他 Hub，实现跨 Hub 节点传递
- 纯 Go 实现，依赖简单，易部署

## 快速开始
### 环境要求
- 建议 Go 1.21+

### 安装与启动
```bash
go mod tidy
PORT=3000 HOST=localhost go run ./cmd/peerpigeon
```

### 服务检查
```bash
curl http://localhost:3000/health
curl http://localhost:3000/stats
curl http://localhost:3000/hubstats
curl http://localhost:3000/hubs
```

## 配置
启动时读取的环境变量：
- `PORT`（默认 `3000`）：监听端口
- `HOST`（默认 `localhost`）：绑定主机地址
- `MAX_CONNECTIONS`（默认 `1000`）：最大并发 WS 连接数
- `CORS_ORIGIN`（默认 `*`）：HTTP 端点的允许跨域来源
- `IS_HUB`（默认 `false`）：是否启用 Hub 模式（启用后生成 Hub peerId）
- `HUB_MESH_NAMESPACE`（默认 `pigeonhub-mesh`）：Hub 使用的命名空间
- `BOOTSTRAP_HUBS`（默认空）：其他 Hub 的 WebSocket 地址（逗号分隔，仅 `IS_HUB=true` 时尝试连接）
- `AUTH_TOKEN`（默认空）：若设置，WS 客户端必须携带授权令牌

示例：
```bash
PORT=3001 HOST=0.0.0.0 IS_HUB=true BOOTSTRAP_HUBS="ws://other-host:3000/ws" \
  go run ./cmd/peerpigeon
```

## 使用说明
### WebSocket 端点
- 地址：`ws://<host>:<port>/ws?peerId=<40位十六进制>`
- `peerId` 必须是 40 位的十六进制字符串。

### 鉴权（可选）
当设置了 `AUTH_TOKEN` 时，客户端需提供以下其一：
- Header：`Authorization: Bearer <token>`
- Query：`?token=<token>`

### 加入网络（announce）
WS 连接成功后发送 `announce` 消息：
```json
{
  "type": "announce",
  "networkName": "global",
  "data": { "isHub": false }
}
```
服务器会向新加入的节点返回当前网络中已存在的 `peer-discovered` 列表，并向其他节点广播该节点加入事件。

### 信令消息
用于在节点间交换类似 WebRTC 的负载：
- `offer`、`answer`、`ice-candidate`

示例：
```json
{
  "type": "offer",
  "targetPeerId": "<peer-id>",
  "networkName": "global",
  "data": { "sdp": "..." }
}
```

### Ping/Pong
发送 `{"type":"ping"}`，收到 `{"type":"pong"}`（包含时间戳）。

## HTTP API
- `GET /health`：健康状态与基本指标
- `GET /stats`：运行时统计（连接数、节点数、Hub 数等）
- `GET /hubstats`：引导连接与 Hub 连接详情
- `GET /hubs`：已登记的 Hub 列表

## 工作原理
### 服务启动
- 初始化 Gin 引擎并注册 HTTP 路由。
- 启动定时清理任务，完成连接维护与中继去重数据清理。
- 若开启 Hub 模式且配置了 `BOOTSTRAP_HUBS`，将主动连接到远端 Hub。

关键代码位置：
- 启动与路由：`internal/server/server.go:56-94`
- 清理定时器：`internal/server/server.go:79-85`, `internal/server/server.go:455-472`
- 端口探测：`internal/server/server.go:105-115`

### WebSocket 握手与鉴权
- 访问 `GET /ws` 进行 WS 升级。
- 若设置了 `AUTH_TOKEN`，需在 Header 或 Query 中提供令牌。
- 连接与节点信息以内存结构维护，包括最近活动时间、网络命名空间等。

关键代码位置：
- 握手与鉴权：`internal/server/server.go:117-158`
- 节点状态更新：`internal/server/server.go:171-197`

### Announce 与节点发现
- `announce` 将节点加入到指定网络命名空间，并可标记为 Hub。
- 新节点事件通过 `peer-discovered` 广播给同网络内其他节点。
- 新节点也会收到当前网络的存量节点列表。

关键代码位置：
- Announce：`internal/server/server.go:199-231`
- 注册 Hub：`internal/server/server.go:233-237`
- 广播发现：`internal/server/server.go:239-247`
- 回放存量节点：`internal/server/server.go:249-258`

### 本地路由与信令
- 当目标节点本地在线且处于同一网络时，直接本地转发。
- 否则通过引导 Hub 中继（若可用），并进行中继去重。

关键代码位置：
- 本地转发：`internal/server/server.go:402-405`
- 信令处理：`internal/server/server.go:281-309`
- 去重哈希：`internal/server/util.go:33-37`

### Hub 模式与跨 Hub 感知
- Hub 模式下生成 Hub peerId，并可按 `BOOTSTRAP_HUBS` 配置去连接远端 Hub。
- 引导连接建立后，会向对端宣布本地能力与本地活跃节点。
- 远端 `peer-discovered` 将缓存并转发给本地节点。

关键代码位置：
- 连接引导 Hub：`internal/server/hubs.go:26-54`
- 向引导端与本地宣布：`internal/server/hubs.go:98-136`
- 处理引导消息：`internal/server/hubs.go:138-167`
- 缓存跨 Hub 节点：`internal/server/server.go:446-453`
- 转发到本地节点：`internal/server/server.go:438-444`

## 测试
```bash
go test ./...
```

## 常见问题
- 依赖下载慢：设置 Go 模块代理
```bash
go env -w GOPROXY=https://mirrors.aliyun.com/goproxy/,https://goproxy.cn,https://goproxy.io,https://proxy.golang.org,direct
```
- 端口冲突：服务会在 `MaxPortRetries` 的范围内递增探测可用端口。
- 非法 `peerId`：必须是 40 位十六进制字符串。

## PeerPigeon for JavaScript
来自传奇开发者 Daniel Raeder
https://github.com/PeerPigeon/PeerPigeon