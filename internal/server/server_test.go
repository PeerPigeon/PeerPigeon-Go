package server

import "testing"

func TestValidatePeerId(t *testing.T) {
    if !validatePeerId("0123456789abcdef0123456789abcdef01234567") {
        t.Fatalf("expected valid")
    }
    if validatePeerId("xyz") {
        t.Fatalf("expected invalid")
    }
}

func TestXORDistance(t *testing.T) {
    d1 := xorDistance("0", "f")
    d2 := xorDistance("0", "0")
    if d1 <= d2 {
        t.Fatalf("distance ordering wrong")
    }
}

