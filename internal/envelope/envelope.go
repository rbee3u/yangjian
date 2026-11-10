package envelope

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"
)

const SchemaVersion = "1"

type Envelope struct {
	SchemaVersion string `json:"schema_version"`
	OK            bool   `json:"ok"`
	Data          any    `json:"data"`
	Error         *Error `json:"error"`
	Meta          Meta   `json:"meta"`
}

type Meta struct {
	Command     string  `json:"command"`
	RunID       string  `json:"run_id,omitempty"`
	ChainID     int64   `json:"chain_id,omitempty"`
	BlockNumber *uint64 `json:"block_number,omitempty"`
	BlockHash   string  `json:"block_hash,omitempty"`
	Status      string  `json:"status,omitempty"`
	DurationMS  int64   `json:"duration_ms"`
}

func Success(command string, data any, meta Meta) Envelope {
	meta.Command = command
	return Envelope{
		SchemaVersion: SchemaVersion,
		OK:            true,
		Data:          data,
		Meta:          meta,
	}
}

func Failure(command string, err *Error, meta Meta) Envelope {
	meta.Command = command
	return Envelope{
		SchemaVersion: SchemaVersion,
		OK:            false,
		Error:         err,
		Meta:          meta,
	}
}

func Write(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

func NewRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "run-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return hex.EncodeToString(b[:])
}

func ExitCode(env Envelope) int {
	if env.Error != nil {
		return env.Error.ExitCode()
	}
	if env.Meta.Status == "partial" {
		return 3
	}
	return 0
}
