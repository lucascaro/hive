package muxnative

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	maxMsgSize = 4 * 1024 * 1024 // 4 MiB — enough for large capture-pane output
)

// Request is the JSON envelope sent from the client to the daemon.
type Request struct {
	Op         string   `json:"op"`
	Session    string   `json:"session,omitempty"`
	WindowName string   `json:"window_name,omitempty"`
	WorkDir    string   `json:"work_dir,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	Target     string   `json:"target,omitempty"`
	Lines      int      `json:"lines,omitempty"`
	NewName    string   `json:"new_name,omitempty"`
	Keys       string   `json:"keys,omitempty"`
}

// Response is the JSON envelope sent from the daemon to the client.
type Response struct {
	OK      bool         `json:"ok"`
	Error   string       `json:"error,omitempty"`
	Bool    bool         `json:"bool,omitempty"`
	Int     int          `json:"int,omitempty"`
	Strings []string     `json:"strings,omitempty"`
	Windows []WindowInfo `json:"windows,omitempty"`
	Content string       `json:"content,omitempty"`
}

// WindowInfo is the wire representation of a window entry.
type WindowInfo struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// writeMsg encodes v as JSON and writes it with a 4-byte big-endian length header.
func writeMsg(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// readMsg reads a 4-byte big-endian length header then decodes the JSON body into v.
func readMsg(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxMsgSize {
		return fmt.Errorf("message too large: %d bytes", n)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// errResp is a convenience helper for building error responses.
func errResp(msg string) Response { return Response{Error: msg} }

// okResp is a convenience helper for building success responses.
func okResp() Response { return Response{OK: true} }
