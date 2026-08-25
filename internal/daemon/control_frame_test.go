package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// recordOps captures what a frame handler wrote, so a handler can be
// driven directly instead of through a socket. That seam is the point
// of splitting handleControlFrame out of serveControl: before it,
// asserting "a malformed payload answers bad_payload and keeps the
// connection open" meant standing up a daemon, dialing it, and
// round-tripping frames.
type recordOps struct {
	errs    []wire.Error
	frames  []wire.FrameType
	mutated []string
}

func (r *recordOps) ops() controlOps {
	return controlOps{
		writeJSON: func(t wire.FrameType, _ any) error {
			r.frames = append(r.frames, t)
			return nil
		},
		sendError: func(code, msg string) {
			r.errs = append(r.errs, wire.Error{Code: code, Message: msg})
		},
		sendWorktrees:  func(projectID, _ string) { r.mutated = append(r.mutated, projectID) },
		finishMutation: func(projectID string, _ error, _ string) { r.mutated = append(r.mutated, projectID) },
	}
}

func newFrameTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	skipOnWindows(t)
	tmp := shortTempDir(t)
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{
			Shell: "/bin/bash", Cols: 80, Rows: 24,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// TestControlFrameBadPayload pins the prologue that decodeReq now
// owns for twelve frames: a payload that is not the frame's request
// type is answered with bad_payload, and the connection keeps reading
// rather than dropping the client.
func TestControlFrameBadPayload(t *testing.T) {
	d := newFrameTestDaemon(t)
	// A JSON array parses as JSON but never as any of the request
	// structs, so it exercises the unmarshal failure for every frame.
	bad := []byte(`["not","an","object"]`)
	frames := []wire.FrameType{
		wire.FrameCreateSession,
		wire.FrameKillSession,
		wire.FrameRestartSession,
		wire.FrameUpdateSession,
		wire.FrameCreateProject,
		wire.FrameKillProject,
		wire.FrameUpdateProject,
		wire.FrameListWorktrees,
		wire.FrameRemoveWorktree,
		wire.FrameCreateWorktree,
		wire.FrameDeleteBranch,
		wire.FrameRenameWorktree,
	}
	for _, ft := range frames {
		t.Run(fmt.Sprintf("frame_%#x", byte(ft)), func(t *testing.T) {
			rec := &recordOps{}
			if done := d.handleControlFrame(context.Background(), rec.ops(), ft, bad); done {
				t.Fatalf("frame %#x: a bad payload closed the connection", byte(ft))
			}
			if len(rec.errs) != 1 || rec.errs[0].Code != "bad_payload" {
				t.Fatalf("frame %#x: got %+v, want exactly one bad_payload error", byte(ft), rec.errs)
			}
			if len(rec.mutated) != 0 {
				t.Errorf("frame %#x: a bad payload still reached the registry: %v", byte(ft), rec.mutated)
			}
		})
	}
}

// TestControlFrameListsAnswerInPlace: the two list frames need no
// payload and answer on the same call, so they are the cheapest proof
// that the dispatch wiring survived the split.
func TestControlFrameListsAnswerInPlace(t *testing.T) {
	d := newFrameTestDaemon(t)
	for ft, want := range map[wire.FrameType]wire.FrameType{
		wire.FrameListSessions: wire.FrameSessions,
		wire.FrameListProjects: wire.FrameProjects,
	} {
		rec := &recordOps{}
		if done := d.handleControlFrame(context.Background(), rec.ops(), ft, nil); done {
			t.Fatalf("frame %#x closed the connection", byte(ft))
		}
		if len(rec.frames) != 1 || rec.frames[0] != want {
			t.Errorf("frame %#x: got frames %v, want [%#x]", byte(ft), rec.frames, byte(want))
		}
	}
}

// TestControlFrameUnknownIsIgnored: an unrecognised frame must not
// close the connection — a newer client sending a frame this daemon
// does not know is a version skew, not a protocol violation.
func TestControlFrameUnknownIsIgnored(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	if done := d.handleControlFrame(context.Background(), rec.ops(), wire.FrameType(0xFE), nil); done {
		t.Fatal("an unknown frame closed the connection")
	}
	if len(rec.errs) != 0 || len(rec.frames) != 0 {
		t.Errorf("an unknown frame answered: errs=%v frames=%v", rec.errs, rec.frames)
	}
}

// TestDecodeReqEmptyPayload: jsonUnmarshal treats an empty payload as
// "leave the zero value alone", which several frames rely on for
// requests whose fields are all optional. Pinning it here keeps that
// permissiveness deliberate rather than incidental.
func TestDecodeReqEmptyPayload(t *testing.T) {
	var got []wire.Error
	send := func(code, msg string) { got = append(got, wire.Error{Code: code, Message: msg}) }

	req, ok := decodeReq[wire.ListWorktreesReq](nil, send)
	if !ok || len(got) != 0 {
		t.Fatalf("empty payload rejected: ok=%v errs=%v", ok, got)
	}
	if req.ProjectID != "" {
		t.Errorf("empty payload produced %+v, want the zero value", req)
	}

	raw, err := json.Marshal(wire.ListWorktreesReq{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if req, ok = decodeReq[wire.ListWorktreesReq](raw, send); !ok || req.ProjectID != "p1" {
		t.Fatalf("round trip failed: ok=%v req=%+v errs=%v", ok, req, got)
	}
}
