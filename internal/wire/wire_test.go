package wire

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		ftype   FrameType
		payload []byte
	}{
		{"empty", FrameData, nil},
		{"small", FrameData, []byte("hello")},
		{"binary", FrameData, []byte{0x00, 0x01, 0xff, 0xfe, 0x7f}},
		{"control", FrameHello, []byte(`{"version":0}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, tc.ftype, tc.payload); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}
			gotType, gotPayload, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if gotType != tc.ftype {
				t.Errorf("type: got %s, want %s", gotType, tc.ftype)
			}
			if !bytes.Equal(gotPayload, tc.payload) {
				t.Errorf("payload mismatch: got %q, want %q", gotPayload, tc.payload)
			}
		})
	}
}

func TestFrameTooLargeOnWrite(t *testing.T) {
	var buf bytes.Buffer
	big := make([]byte, MaxPayload+1)
	if err := WriteFrame(&buf, FrameData, big); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("got %v, want ErrFrameTooLarge", err)
	}
}

func TestFrameTooLargeOnRead(t *testing.T) {
	// Forge a header that claims an absurd payload length.
	hdr := []byte{byte(FrameData), 0xff, 0xff, 0xff, 0xff}
	_, _, err := ReadFrame(bytes.NewReader(hdr))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("got %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameTruncated(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteFrame(&buf, FrameData, []byte("hello"))
	// Truncate mid-payload.
	truncated := buf.Bytes()[:7]
	_, _, err := ReadFrame(bytes.NewReader(truncated))
	if err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("got %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestHelloWelcomeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameHello, Hello{
		Version: PROTOCOL_VERSION, Client: "hive/0.2.0", Mode: ModeAttach, SessionID: "abc",
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if err := WriteJSON(&buf, FrameWelcome, Welcome{
		Version: PROTOCOL_VERSION, Mode: ModeAttach, SessionID: "abc", Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatalf("write welcome: %v", err)
	}

	var hello Hello
	if ft, err := ReadJSON(&buf, &hello); err != nil || ft != FrameHello {
		t.Fatalf("read hello: ft=%s err=%v", ft, err)
	}
	if hello.Client != "hive/0.2.0" || hello.Mode != ModeAttach || hello.SessionID != "abc" {
		t.Errorf("hello = %+v", hello)
	}

	var welcome Welcome
	if ft, err := ReadJSON(&buf, &welcome); err != nil || ft != FrameWelcome {
		t.Fatalf("read welcome: ft=%s err=%v", ft, err)
	}
	if welcome.SessionID != "abc" || welcome.Cols != 80 || welcome.Rows != 24 || welcome.Mode != ModeAttach {
		t.Errorf("welcome = %+v", welcome)
	}
}

func TestV1ControlFrameRoundTrips(t *testing.T) {
	cases := []struct {
		name string
		ft   FrameType
		v    any
	}{
		{"list", FrameListSessions, ListSessionsReq{}},
		{"sessions", FrameSessions, SessionsResp{Sessions: []SessionInfo{
			{ID: "1", Name: "main", Color: "#fa0", Order: 0, Created: "2026-04-30T00:00:00Z", Alive: true},
		}}},
		{"create", FrameCreateSession, CreateSpec{Name: "x", Color: "#0af", Cols: 100, Rows: 30}},
		{"kill", FrameKillSession, KillSessionReq{SessionID: "id"}},
		{"update", FrameUpdateSession, UpdateSessionReq{
			SessionID: "id",
			Name:      ptrStr("renamed"),
			Order:     ptrInt(2),
		}},
		{"event", FrameSessionEvent, SessionEvent{
			Kind:    SessionEventAdded,
			Session: SessionInfo{ID: "1", Name: "x", Order: 0, Alive: true},
		}},
		{"list-projects", FrameListProjects, ListProjectsReq{}},
		{"projects", FrameProjects, ProjectsResp{Projects: []ProjectInfo{
			{ID: "p1", Name: "hive", Color: "#fa0", Cwd: "/h", Order: 0, Created: "2026-04-30T00:00:00Z"},
		}}},
		{"create-project", FrameCreateProject, CreateProjectReq{Name: "x", Color: "#0af", Cwd: "/x"}},
		{"kill-project", FrameKillProject, KillProjectReq{ProjectID: "p1", KillSessions: true}},
		{"update-project", FrameUpdateProject, UpdateProjectReq{
			ProjectID: "p1",
			Name:      ptrStr("renamed"),
			Cwd:       ptrStr("/new"),
		}},
		{"project-event", FrameProjectEvent, ProjectEvent{
			Kind:    ProjectEventAdded,
			Project: ProjectInfo{ID: "p1", Name: "x", Order: 0},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteJSON(&buf, tc.ft, tc.v); err != nil {
				t.Fatalf("write: %v", err)
			}
			ft, _, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if ft != tc.ft {
				t.Errorf("type: got %s, want %s", ft, tc.ft)
			}
		})
	}
}

func ptrStr(s string) *string { return &s }
func ptrInt(i int) *int       { return &i }

func TestFrameTypeStringUnknown(t *testing.T) {
	s := FrameType(0xab).String()
	if !strings.Contains(s, "0xab") {
		t.Errorf("unknown stringer = %q", s)
	}
}
