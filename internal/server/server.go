package server

import (
    "encoding/json"
    "log"
    "net"
    "net/http"
    "sort"
    "strings"
    "sync"
    "time"
    "github.com/gin-gonic/gin"
    "github.com/gorilla/websocket"
)

type Server struct {
    opts Options
    port int
    running bool
    startTime int64
    upgrader websocket.Upgrader
    engine *gin.Engine
    wsConns map[string]*websocket.Conn
    wsMu sync.Mutex
    peerData map[string]*peerInfo
    peersMu sync.Mutex
    networkPeers map[string]map[string]struct{}
    networkMu sync.Mutex
    hubs map[string]*hubInfo
    hubsMu sync.Mutex
    relayed map[string]int64
    relayMu sync.Mutex
    cleanupTicker *time.Ticker
    hubPeerId string
    bootstrapConns map[string]*bootstrapConn
    bootstrapMu sync.Mutex
    crossHubCache map[string]map[string]map[string]interface{}
}

func NewServer(o Options) *Server {
    s := &Server{opts: o, port: o.Port}
    s.wsConns = map[string]*websocket.Conn{}
    s.peerData = map[string]*peerInfo{}
    s.networkPeers = map[string]map[string]struct{}{}
    s.hubs = map[string]*hubInfo{}
    s.relayed = map[string]int64{}
    s.bootstrapConns = map[string]*bootstrapConn{}
    s.crossHubCache = map[string]map[string]map[string]interface{}{}
    s.upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
    if s.opts.IsHub {
        s.hubPeerId = s.generatePeerId()
    }
    return s
}

func (s *Server) Start() error {
    p, err := s.tryPort(s.port, s.opts.MaxPortRetries)
    if err != nil {
        return err
    }
    s.port = p
    s.engine = gin.New()
    s.engine.Use(gin.Recovery())
    s.engine.GET("/health", func(c *gin.Context) {
        writeJSON(c.Writer, 200, map[string]interface{}{"status": "healthy", "timestamp": time.Now().Format(time.RFC3339), "uptime": s.uptime(), "isHub": s.opts.IsHub, "connections": s.connectionsSize(), "peers": len(s.peerData), "hubs": len(s.hubs), "networks": len(s.networkPeers)}, s.opts.CORSOrigin)
    })
    s.engine.GET("/hubs", func(c *gin.Context) {
        writeJSON(c.Writer, 200, map[string]interface{}{"timestamp": time.Now().Format(time.RFC3339), "totalHubs": len(s.hubs), "hubs": s.getConnectedHubs()}, s.opts.CORSOrigin)
    })
    s.engine.GET("/stats", func(c *gin.Context) {
        writeJSON(c.Writer, 200, s.getStats(), s.opts.CORSOrigin)
    })
    s.engine.GET("/hubstats", func(c *gin.Context) {
        writeJSON(c.Writer, 200, s.getHubStats(), s.opts.CORSOrigin)
    })
    s.engine.GET("/ws", s.handleWS)
    s.engine.GET("/", s.handleWS)
    go func() {
        s.running = true
        s.startTime = nowMs()
        s.cleanupTicker = time.NewTicker(time.Duration(s.opts.CleanupIntervalMs) * time.Millisecond)
        for range s.cleanupTicker.C {
            s.performCleanup()
        }
    }()
    go func() {
        if s.opts.IsHub && len(s.opts.BootstrapHubs) > 0 {
            time.Sleep(1 * time.Second)
            s.connectToBootstrapHubs()
        }
    }()
    addr := s.opts.Host + ":" + itoa(s.port)
    return s.engine.Run(addr)
}

func (s *Server) Stop() error {
    s.running = false
    if s.cleanupTicker != nil {
        s.cleanupTicker.Stop()
    }
    s.disconnectBootstrap()
    return nil
}

func (s *Server) tryPort(port, maxRetries int) (int, error) {
    for i := 0; i <= maxRetries; i++ {
        p := port + i
        ln, err := net.Listen("tcp", s.opts.Host+":"+itoa(p))
        if err == nil {
            ln.Close()
            return p, nil
        }
    }
    return 0, http.ErrServerClosed
}

func (s *Server) handleWS(c *gin.Context) {
    peerId := c.Query("peerId")
    if s.opts.AuthToken != "" {
        auth := c.GetHeader("Authorization")
        if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != s.opts.AuthToken {
            token := c.Query("token")
            if token != s.opts.AuthToken {
                http.Error(c.Writer, "unauthorized", http.StatusUnauthorized)
                return
            }
        }
    }
    if !validatePeerId(peerId) {
        http.Error(c.Writer, "invalid peerId", http.StatusForbidden)
        return
    }
    conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        return
    }
    s.wsMu.Lock()
    if _, ok := s.wsConns[peerId]; ok {
        old := s.wsConns[peerId]
        if old != nil {
            old.Close()
        }
        delete(s.wsConns, peerId)
    }
    if len(s.wsConns) >= s.opts.MaxConnections {
        s.wsMu.Unlock()
        conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "max connections"), time.Now().Add(time.Second))
        conn.Close()
        return
    }
    s.wsConns[peerId] = conn
    s.wsMu.Unlock()
    s.peersMu.Lock()
    s.peerData[peerId] = &peerInfo{PeerId: peerId, ConnectedAt: nowMs(), LastActivity: nowMs(), RemoteAddress: c.ClientIP(), Connected: true}
    s.peersMu.Unlock()
    s.sendToConn(conn, outboundMessage{Type: "connected", Data: map[string]interface{}{"peerId": peerId}, FromPeerId: "system", NetworkName: "global", Timestamp: nowMs()})
    go s.readLoop(peerId, conn)
}

func (s *Server) readLoop(peerId string, conn *websocket.Conn) {
    for {
        _, data, err := conn.ReadMessage()
        if err != nil {
            s.handleDisconnect(peerId, websocket.CloseAbnormalClosure, err.Error())
            return
        }
        s.handleMessage(peerId, data)
    }
}

func (s *Server) handleMessage(peerId string, data []byte) {
    var msg inboundMessage
    if err := json.Unmarshal(data, &msg); err != nil {
        return
    }
    s.peersMu.Lock()
    if pi, ok := s.peerData[peerId]; ok {
        pi.LastActivity = nowMs()
    }
    s.peersMu.Unlock()
    resp := outboundMessage{Type: msg.Type, Data: msg.Data, FromPeerId: firstNonEmpty(msg.FromPeerId, peerId), TargetPeer: msg.TargetPeer, NetworkName: firstNonEmpty(msg.NetworkName, "global"), Timestamp: nowMs()}
    switch msg.Type {
    case "announce":
        s.handleAnnounce(peerId, msg, resp)
    case "goodbye":
        s.broadcastToOthers(peerId, resp)
        s.cleanupPeer(peerId)
    case "offer", "answer", "ice-candidate":
        s.handleSignaling(peerId, msg, resp)
    case "peer-discovered":
        s.handlePeerDiscovered(peerId, msg)
    case "ping":
        s.handlePing(peerId)
    case "cleanup":
    default:
    }
}

func (s *Server) handleAnnounce(peerId string, msg inboundMessage, resp outboundMessage) {
    netName := firstNonEmpty(msg.NetworkName, "global")
    isHub := false
    if m, ok := msg.Data.(map[string]interface{}); ok {
        if v, ok := m["isHub"].(bool); ok && v {
            isHub = true
        }
    }
    s.peersMu.Lock()
    pi := s.peerData[peerId]
    if pi != nil {
        pi.Announced = true
        pi.AnnouncedAt = nowMs()
        pi.NetworkName = netName
        pi.IsHub = isHub || netName == s.opts.HubMeshNamespace
        if m, ok := msg.Data.(map[string]interface{}); ok {
            pi.Data = m
        }
    }
    s.peersMu.Unlock()
    if pi != nil && pi.IsHub {
        s.registerHub(peerId, netName, pi.Data)
    }
    s.networkMu.Lock()
    if _, ok := s.networkPeers[netName]; !ok {
        s.networkPeers[netName] = map[string]struct{}{}
    }
    s.networkPeers[netName][peerId] = struct{}{}
    s.networkMu.Unlock()
    s.broadcastPeerDiscovered(peerId, netName, isHub, pi.Data)
    s.sendExistingPeersToNew(peerId, netName)
    s.sendCachedCrossHubPeersToNew(peerId, netName)
}

func (s *Server) registerHub(peerId, netName string, data map[string]interface{}) {
    s.hubsMu.Lock()
    s.hubs[peerId] = &hubInfo{PeerId: peerId, RegisteredAt: nowMs(), LastActivity: nowMs(), NetworkName: netName, Data: data}
    s.hubsMu.Unlock()
}

func (s *Server) broadcastPeerDiscovered(peerId, netName string, isHub bool, data map[string]interface{}) {
    peers := s.getActivePeers("", netName)
    for _, other := range peers {
        if other == peerId {
            continue
        }
        s.forwardToLocalTarget(other, outboundMessage{Type: "peer-discovered", Data: mergeMap(data, map[string]interface{}{"peerId": peerId, "isHub": isHub}), FromPeerId: "system", TargetPeer: other, NetworkName: netName, Timestamp: nowMs()})
    }
}

func (s *Server) sendExistingPeersToNew(peerId, netName string) {
    peers := s.getActivePeers(peerId, netName)
    conn := s.getConn(peerId)
    for _, p := range peers {
        pi := s.getPeerInfo(p)
        if conn != nil && pi != nil {
            s.sendToConn(conn, outboundMessage{Type: "peer-discovered", Data: mergeMap(pi.Data, map[string]interface{}{"peerId": p, "isHub": pi.IsHub}), FromPeerId: "system", TargetPeer: peerId, NetworkName: netName, Timestamp: nowMs()})
        }
    }
}

func (s *Server) sendCachedCrossHubPeersToNew(peerId, netName string) {
    s.bootstrapMu.Lock()
    cache := s.crossHubCache[netName]
    s.bootstrapMu.Unlock()
    if cache == nil {
        return
    }
    conn := s.getConn(peerId)
    count := 0
    for id, data := range cache {
        if _, ok := s.wsConns[id]; ok {
            continue
        }
        if conn != nil {
            s.sendToConn(conn, outboundMessage{Type: "peer-discovered", Data: mergeMap(data, map[string]interface{}{"peerId": id}), FromPeerId: "system", TargetPeer: peerId, NetworkName: netName, Timestamp: nowMs()})
            count++
        }
    }
    if count > 0 {}
}

func (s *Server) handleSignaling(peerId string, msg inboundMessage, resp outboundMessage) {
    target := msg.TargetPeer
    netName := firstNonEmpty(msg.NetworkName, "global")
    if target == "" {
        return
    }
    if s.getConn(target) != nil {
        tp := s.getPeerInfo(target)
        tn := "global"
        if tp != nil && tp.NetworkName != "" {
            tn = tp.NetworkName
        }
        if netName != tn {
            return
        }
        s.forwardToLocalTarget(target, resp)
        return
    }
    dataHash := hashSignalData(msg.Data)
    id := msg.Type + ":" + peerId + ":" + target + ":" + dataHash
    s.relayMu.Lock()
    if _, ok := s.relayed[id]; ok {
        s.relayMu.Unlock()
        return
    }
    s.relayed[id] = nowMs()
    s.relayMu.Unlock()
    s.forwardSignalToBootstrap(target, resp)
}

func (s *Server) forwardSignalToBootstrap(target string, resp outboundMessage) {
    s.bootstrapMu.Lock()
    for _, b := range s.bootstrapConns {
        if b.connected && b.ws != nil {
            b.ws.WriteJSON(resp)
        }
    }
    s.bootstrapMu.Unlock()
}

func (s *Server) handlePeerDiscovered(fromHub string, msg inboundMessage) {}

func (s *Server) handlePing(peerId string) {
    conn := s.getConn(peerId)
    if conn != nil {
        s.sendToConn(conn, outboundMessage{Type: "pong", Data: map[string]interface{}{"timestamp": nowMs()}, FromPeerId: "system", TargetPeer: peerId, NetworkName: "global", Timestamp: nowMs()})
    }
}

func (s *Server) handleDisconnect(peerId string, code int, reason string) {
    pi := s.getPeerInfo(peerId)
    netName := "global"
    isHub := false
    if pi != nil {
        netName = firstNonEmpty(pi.NetworkName, "global")
        isHub = pi.IsHub
    }
    s.broadcastToOthers(peerId, outboundMessage{Type: "peer-disconnected", Data: map[string]interface{}{"peerId": peerId, "isHub": isHub, "reason": reason, "timestamp": nowMs()}, FromPeerId: "system", NetworkName: netName, Timestamp: nowMs()})
    s.cleanupPeer(peerId)
}

func (s *Server) cleanupPeer(peerId string) {
    s.wsMu.Lock()
    delete(s.wsConns, peerId)
    s.wsMu.Unlock()
    s.peersMu.Lock()
    pi := s.peerData[peerId]
    delete(s.peerData, peerId)
    s.peersMu.Unlock()
    if pi != nil && pi.IsHub {
        s.hubsMu.Lock()
        delete(s.hubs, peerId)
        s.hubsMu.Unlock()
    }
    if pi != nil && pi.NetworkName != "" {
        s.networkMu.Lock()
        if set, ok := s.networkPeers[pi.NetworkName]; ok {
            delete(set, peerId)
            if len(set) == 0 {
                delete(s.networkPeers, pi.NetworkName)
            }
        }
        s.networkMu.Unlock()
        s.bootstrapMu.Lock()
        if cache, ok := s.crossHubCache[pi.NetworkName]; ok {
            delete(cache, peerId)
        }
        s.bootstrapMu.Unlock()
    }
}

func (s *Server) sendToConn(conn *websocket.Conn, msg outboundMessage) bool {
    if conn == nil {
        return false
    }
    b, _ := json.Marshal(msg)
    conn.WriteMessage(websocket.TextMessage, b)
    return true
}

func (s *Server) broadcastToOthers(sender string, msg outboundMessage) int {
    s.wsMu.Lock()
    ids := make([]string, 0, len(s.wsConns))
    for id := range s.wsConns {
        if id != sender {
            ids = append(ids, id)
        }
    }
    s.wsMu.Unlock()
    count := 0
    for _, id := range ids {
        conn := s.getConn(id)
        m := msg
        m.TargetPeer = id
        if s.sendToConn(conn, m) {
            count++
        }
    }
    return count
}

func (s *Server) forwardToLocalTarget(target string, msg outboundMessage) bool {
    conn := s.getConn(target)
    return s.sendToConn(conn, msg)
}

func (s *Server) getConn(id string) *websocket.Conn {
    s.wsMu.Lock()
    c := s.wsConns[id]
    s.wsMu.Unlock()
    return c
}

func (s *Server) getPeerInfo(id string) *peerInfo {
    s.peersMu.Lock()
    pi := s.peerData[id]
    s.peersMu.Unlock()
    return pi
}

func (s *Server) getActivePeers(exclude, netName string) []string {
    s.networkMu.Lock()
    set := s.networkPeers[netName]
    s.networkMu.Unlock()
    if set == nil {
        return []string{}
    }
    out := make([]string, 0, len(set))
    for id := range set {
        if id != exclude && s.getConn(id) != nil {
            out = append(out, id)
        }
    }
    sort.Strings(out)
    return out
}

func (s *Server) forwardToLocalPeers(netName string, msg outboundMessage) {
    peers := s.getActivePeers("", netName)
    for _, id := range peers {
        conn := s.getConn(id)
        s.sendToConn(conn, msg)
    }
}

func (s *Server) cacheCrossHubPeer(netName, id string, data map[string]interface{}) {
    s.bootstrapMu.Lock()
    if _, ok := s.crossHubCache[netName]; !ok {
        s.crossHubCache[netName] = map[string]map[string]interface{}{}
    }
    s.crossHubCache[netName][id] = data
    s.bootstrapMu.Unlock()
}

func (s *Server) performCleanup() {
    s.wsMu.Lock()
    total := len(s.wsConns)
    s.wsMu.Unlock()
    for netName := range s.networkPeers {
        s.getActivePeers("", netName)
    }
    cleaned := total - s.connectionsSize()
    if cleaned > 0 {}
    now := nowMs()
    s.relayMu.Lock()
    for id, ts := range s.relayed {
        if now-ts > 5000 {
            delete(s.relayed, id)
        }
    }
    s.relayMu.Unlock()
}

func (s *Server) connectionsSize() int {
    s.wsMu.Lock()
    n := len(s.wsConns)
    s.wsMu.Unlock()
    return n
}

func (s *Server) getStats() map[string]interface{} {
    s.bootstrapMu.Lock()
    connected := 0
    for _, info := range s.bootstrapConns {
        if info.connected {
            connected++
        }
    }
    s.bootstrapMu.Unlock()
    return map[string]interface{}{
        "isRunning": s.running,
        "isHub": s.opts.IsHub,
        "hubPeerId": s.hubPeerId,
        "hubMeshNamespace": s.opts.HubMeshNamespace,
        "connections": s.connectionsSize(),
        "peers": len(s.peerData),
        "hubs": len(s.hubs),
        "networks": len(s.networkPeers),
        "bootstrapHubs": map[string]interface{}{"total": len(s.opts.BootstrapHubs), "connected": connected},
        "maxConnections": s.opts.MaxConnections,
        "uptime": s.uptime(),
        "host": s.opts.Host,
        "port": s.port,
    }
}

func (s *Server) uptime() int64 {
    if !s.running || s.startTime == 0 {
        return 0
    }
    return nowMs() - s.startTime
}

func (s *Server) generatePeerId() string {
    const chars = "0123456789abcdef"
    b := make([]byte, 40)
    for i := range b {
        b[i] = chars[time.Now().UnixNano()%16]
        time.Sleep(time.Nanosecond)
    }
    return string(b)
}

func firstNonEmpty(a, b string) string {
    if strings.TrimSpace(a) != "" {
        return a
    }
    return b
}

func mergeMap(a, b map[string]interface{}) map[string]interface{} {
    out := map[string]interface{}{}
    for k, v := range a {
        out[k] = v
    }
    for k, v := range b {
        out[k] = v
    }
    return out
}

func (s *Server) emitBootstrapConnected(uri string) {
    if s.opts.VerboseLogging {
        log.Printf("bootstrap connected: %s", uri)
    }
}

func (s *Server) emitHubDiscovered(hubPeerId, fromURI string) {
    if s.opts.VerboseLogging {
        log.Printf("hub discovered: %s via %s", hubPeerId, fromURI)
    }
}
