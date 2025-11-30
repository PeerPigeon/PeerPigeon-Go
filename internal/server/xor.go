package server

func xorDistance(a, b string) int {
    n := len(a)
    if len(b) < n {
        n = len(b)
    }
    d := 0
    for i := 0; i < n; i++ {
        ai := hexNibble(a[i])
        bi := hexNibble(b[i])
        d += ai ^ bi
    }
    return d
}

func hexNibble(c byte) int {
    if c >= '0' && c <= '9' {
        return int(c - '0')
    }
    if c >= 'a' && c <= 'f' {
        return int(c-'a') + 10
    }
    if c >= 'A' && c <= 'F' {
        return int(c-'A') + 10
    }
    return 0
}

func findClosestPeers(target string, peers []string, max int) []string {
    if len(peers) == 0 || max <= 0 {
        return []string{}
    }
    type pair struct{ id string; d int }
    arr := make([]pair, 0, len(peers))
    for _, p := range peers {
        arr = append(arr, pair{id: p, d: xorDistance(target, p)})
    }
    for i := 0; i < len(arr); i++ {
        for j := i + 1; j < len(arr); j++ {
            if arr[j].d < arr[i].d {
                arr[i], arr[j] = arr[j], arr[i]
            }
        }
    }
    if len(arr) > max {
        arr = arr[:max]
    }
    out := make([]string, len(arr))
    for i := range arr {
        out[i] = arr[i].id
    }
    return out
}

