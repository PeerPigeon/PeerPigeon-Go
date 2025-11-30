package server

type Options struct {
    Port                int
    Host                string
    MaxConnections      int
    CORSOrigin          string
    IsHub               bool
    HubMeshNamespace    string
    BootstrapHubs       []string
    CleanupIntervalMs   int
    PeerTimeoutMs       int
    MaxMessageBytes     int
    MaxPortRetries      int
    VerboseLogging      bool
    ReconnectIntervalMs int
    MaxReconnectAttempts int
    AuthToken           string
}

type inboundMessage struct {
    Type        string      `json:"type"`
    Data        interface{} `json:"data"`
    TargetPeer  string      `json:"targetPeerId"`
    NetworkName string      `json:"networkName"`
    FromPeerId  string      `json:"fromPeerId"`
}

type outboundMessage struct {
    Type        string      `json:"type"`
    Data        interface{} `json:"data"`
    FromPeerId  string      `json:"fromPeerId"`
    TargetPeer  string      `json:"targetPeerId,omitempty"`
    NetworkName string      `json:"networkName"`
    Timestamp   int64       `json:"timestamp"`
}

type peerInfo struct {
    PeerId        string
    ConnectedAt   int64
    LastActivity  int64
    RemoteAddress string
    Connected     bool
    Announced     bool
    AnnouncedAt   int64
    NetworkName   string
    Data          map[string]interface{}
    IsHub         bool
}
