package model

import (
	"strings"
	"testing"
)

func TestNormalizeAddress(t *testing.T) {
	got, err := NormalizeAddress("0x8ad599c3a0ff1de082011efddc58f1908eb6e6d8")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
		t.Fatalf("address = %s", got)
	}
	if _, err := NormalizeAddress("not-an-address"); err == nil {
		t.Fatal("expected invalid address error")
	}
}

func TestEntityIDs(t *testing.T) {
	address := "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"
	if got := ContractID(1, address); got != "1:0x8ad599c3a0ff1de082011efddc58f1908eb6e6d8" {
		t.Fatalf("contract id = %s", got)
	}
	if got := ContractVersionID(1, address, 123); got != "1:0x8ad599c3a0ff1de082011efddc58f1908eb6e6d8@123" {
		t.Fatalf("contract version id = %s", got)
	}
}

func TestContentHashStable(t *testing.T) {
	a := ContentHash([]byte("hello"))
	b := ContentHash([]byte("hello"))
	if a != b {
		t.Fatal("content hash must be stable")
	}
	if !strings.HasPrefix(a, "sha256:") {
		t.Fatalf("content hash = %s", a)
	}
	if a == ContentHash([]byte("world")) {
		t.Fatal("different content must have different hash")
	}
}

func TestRelationID(t *testing.T) {
	got := RelationID("onchain_reference", "from", "to", "sha256:abc")
	want := "onchain_reference:from:to:sha256:abc"
	if got != want {
		t.Fatalf("relation id = %s, want %s", got, want)
	}
}
