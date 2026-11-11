package store

import (
	"strings"
	"testing"
	"time"

	"github.com/rbee3u/yangjian/internal/model"
)

func TestWriteReadJSON(t *testing.T) {
	st := New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	in := map[string]string{"hello": "world"}
	if _, err := st.WriteJSON("nested/value.json", in); err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := st.ReadJSON("nested/value.json", &out); err != nil {
		t.Fatal(err)
	}
	if out["hello"] != "world" {
		t.Fatalf("out = %+v", out)
	}
}

func TestWriteObservationIdempotent(t *testing.T) {
	st := New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	block := &model.BlockRef{ChainID: 1, Number: 123, Hash: "0xabc"}
	firstAt := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	obs := model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         "run-1",
		Request:       map[string]any{"method": "eth_getCode", "params": []any{"0xabc", "0x7b"}},
		ResponseHash:  "keccak256:abc",
		Block:         block,
		RetrievedAt:   firstAt,
		Source:        model.SourceRef{Provider: "rpc"},
	}
	first, err := st.WriteObservation(obs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.ID, "sha256:") {
		t.Fatalf("id = %s", first.ID)
	}
	if first.ResponseRef == "" {
		t.Fatal("response_ref must be set")
	}

	obs.RetrievedAt = firstAt.Add(time.Hour)
	second, err := st.WriteObservation(obs)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("observation id changed: %s != %s", second.ID, first.ID)
	}
	if !second.RetrievedAt.Equal(firstAt) {
		t.Fatalf("existing observation retrieved_at changed: %s", second.RetrievedAt)
	}

	obs.Request = map[string]any{"method": "eth_getBlockByNumber", "params": []any{"latest", false}}
	third, err := st.WriteObservation(obs)
	if err != nil {
		t.Fatal(err)
	}
	if third.ID == first.ID {
		t.Fatal("different request must produce a different observation id")
	}
}

func TestWriteJSONRejectsInvalidEntity(t *testing.T) {
	st := New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteJSON("manifest.json", model.Manifest{}); err == nil {
		t.Fatal("expected schema validation error")
	}
}
