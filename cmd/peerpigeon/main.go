package main

import (
    "log"
    "os"
    "strconv"
    "strings"
    "peerpigeon/internal/server"
)

func getenv(key, def string) string {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    return v
}

func main() {
    portStr := getenv("PORT", "3000")
    host := getenv("HOST", "localhost")
    maxConnStr := getenv("MAX_CONNECTIONS", "1000")
    cors := getenv("CORS_ORIGIN", "*")
    hubNs := getenv("HUB_MESH_NAMESPACE", "pigeonhub-mesh")
    isHubStr := getenv("IS_HUB", "false")
    bootstrap := getenv("BOOTSTRAP_HUBS", "")
    authToken := getenv("AUTH_TOKEN", "")

    port, _ := strconv.Atoi(portStr)
    maxConn, _ := strconv.Atoi(maxConnStr)
    isHub := strings.ToLower(isHubStr) == "true"

    s := server.NewServer(server.Options{
        Port:                port,
        Host:                host,
        MaxConnections:      maxConn,
        CORSOrigin:          cors,
        IsHub:               isHub,
        HubMeshNamespace:    hubNs,
        BootstrapHubs:       splitNonEmpty(bootstrap, ","),
        CleanupIntervalMs:   30000,
        PeerTimeoutMs:       300000,
        MaxMessageBytes:     1048576,
        MaxPortRetries:      10,
        VerboseLogging:      false,
        ReconnectIntervalMs: 5000,
        MaxReconnectAttempts: 10,
        AuthToken:           authToken,
    })

    if err := s.Start(); err != nil {
        log.Fatalf("start error: %v", err)
    }

    c := make(chan os.Signal, 1)
    <-c
    _ = s.Stop()
}

func splitNonEmpty(s, sep string) []string {
    if s == "" {
        return nil
    }
    parts := strings.Split(s, sep)
    out := make([]string, 0, len(parts))
    for _, p := range parts {
        p = strings.TrimSpace(p)
        if p != "" {
            out = append(out, p)
        }
    }
    return out
}
