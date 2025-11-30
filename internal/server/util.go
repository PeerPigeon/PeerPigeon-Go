package server

import (
    "crypto/sha1"
    "encoding/json"
    "fmt"
    "net/http"
    "regexp"
    "strconv"
    "time"
)

var peerIdRe = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func validatePeerId(id string) bool {
    return peerIdRe.MatchString(id)
}

func nowMs() int64 { return time.Now().UnixMilli() }

func decodeJSON(b []byte, v interface{}) error { return json.Unmarshal(b, v) }

func writeJSON(w http.ResponseWriter, status int, v interface{}, cors string) {
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Access-Control-Allow-Origin", cors)
    w.WriteHeader(status)
    enc := json.NewEncoder(w)
    enc.Encode(v)
}

func itoa(i int) string { return strconv.Itoa(i) }

func hashSignalData(data interface{}) string {
    b, _ := json.Marshal(data)
    h := sha1.Sum(b)
    return fmt.Sprintf("%x", h[:])
}

