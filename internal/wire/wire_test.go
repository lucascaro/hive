package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
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
		Version: PROTOCOL_VERSION, Client: "hive/0.2.0", BuildID: "abc1234",
		Mode: ModeAttach, SessionID: "abc",
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if err := WriteJSON(&buf, FrameWelcome, Welcome{
		Version: PROTOCOL_VERSION, BuildID: "def5678",
		Mode: ModeAttach, SessionID: "abc", Cols: 80, Rows: 24,
	}); err != nil {
		t.Fatalf("write welcome: %v", err)
	}

	var hello Hello
	if ft, err := ReadJSON(&buf, &hello); err != nil || ft != FrameHello {
		t.Fatalf("read hello: ft=%s err=%v", ft, err)
	}
	if hello.Client != "hive/0.2.0" || hello.Mode != ModeAttach || hello.SessionID != "abc" || hello.BuildID != "abc1234" {
		t.Errorf("hello = %+v", hello)
	}

	var welcome Welcome
	if ft, err := ReadJSON(&buf, &welcome); err != nil || ft != FrameWelcome {
		t.Fatalf("read welcome: ft=%s err=%v", ft, err)
	}
	if welcome.SessionID != "abc" || welcome.Cols != 80 || welcome.Rows != 24 || welcome.Mode != ModeAttach || welcome.BuildID != "def5678" {
		t.Errorf("welcome = %+v", welcome)
	}
}

// TestHelloWelcomeOlderClientBuildIDOmitted verifies that a Hello
// without BuildID (older client) decodes cleanly with BuildID == ""
// — the omitempty contract that lets the daemon distinguish "unknown"
// from "mismatch".
func TestHelloWelcomeOlderClientBuildIDOmitted(t *testing.T) {
	var buf bytes.Buffer
	// Older client: no BuildID set.
	if err := WriteJSON(&buf, FrameHello, Hello{
		Version: PROTOCOL_VERSION, Client: "hive/old", Mode: ModeControl,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var hello Hello
	if _, err := ReadJSON(&buf, &hello); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if hello.BuildID != "" {
		t.Errorf("expected empty BuildID, got %q", hello.BuildID)
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

// TestCreateSpecInsertAfterSnakeCase pins the wire spelling of the
// insert-anchor field: JS readers key off the snake_case name, and an
// older daemon must not see a stray empty field.
func TestCreateSpecInsertAfterSnakeCase(t *testing.T) {
	blob, err := json.Marshal(CreateSpec{Name: "s", InsertAfterSessionID: "abc"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), `"insert_after_session_id":"abc"`) {
		t.Errorf("marshal: got %s, want an insert_after_session_id key", blob)
	}

	empty, err := json.Marshal(CreateSpec{Name: "s"})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(empty), "insert_after_session_id") {
		t.Errorf("empty anchor should be omitted, got %s", empty)
	}

	var back CreateSpec
	if err := json.Unmarshal([]byte(`{"insert_after_session_id":"xyz"}`), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.InsertAfterSessionID != "xyz" {
		t.Errorf("unmarshal: got %q, want %q", back.InsertAfterSessionID, "xyz")
	}
}

// TestSessionInfoRoundTrip populates every SessionInfo field —
// including the lifecycle Phase — and asserts an exact round trip.
// The table above only checks frame types; this is the field-level
// contract test golden principle 6 asks for.
func TestSessionInfoRoundTrip(t *testing.T) {
	want := SessionInfo{
		ID:             "sess-1",
		Name:           "stone-valley claude",
		Color:          "#fa0",
		Order:          3,
		Created:        "2026-04-30T00:00:00Z",
		Alive:          true,
		Agent:          "claude",
		ProjectID:      "proj-1",
		WorktreePath:   "/repo/.worktrees/stone-valley",
		WorktreeBranch: "stone-valley",
		LastError:      "boom",
		Phase:          PhaseWorktree,
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameSessionEvent, SessionEvent{
		Kind: SessionEventUpdated, Session: want,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var got SessionEvent
	if _, err := ReadJSON(&buf, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Session != want {
		t.Errorf("round trip:\n got %+v\nwant %+v", got.Session, want)
	}
}

// TestSessionInfoPhaseReadyOmitted pins the back-compat property that
// makes PhaseReady the empty string: a ready session puts no "phase"
// key on the wire at all, so a client predating the field is
// unaffected and a decoded zero value means ready.
func TestSessionInfoPhaseReadyOmitted(t *testing.T) {
	b, err := json.Marshal(SessionInfo{ID: "1", Phase: PhaseReady})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("phase")) {
		t.Errorf("PhaseReady must be omitted from the wire, got %s", b)
	}
}

// TestWorktreeFrameRoundTrips satisfies golden principle 6 for the
// worktree frames: every field populated with a non-zero value,
// encoded, decoded, and compared for deep equality. A missing or
// misspelled json tag shows up here as a zeroed field rather than as a
// silent GUI bug months later.
func TestWorktreeFrameRoundTrips(t *testing.T) {
	cases := []struct {
		name string
		ft   FrameType
		v    any
		into func() any
	}{
		{
			"list", FrameListWorktrees,
			ListWorktreesReq{ProjectID: "p1"},
			func() any { return &ListWorktreesReq{} },
		},
		{
			"worktrees", FrameWorktrees,
			WorktreesResp{
				ProjectID: "p1",
				RepoRoot:  "/repo",
				Worktrees: []WorktreeInfo{{
					Path:        "/repo/.worktrees/feat",
					Branch:      "feat",
					Detached:    true,
					IsMain:      true,
					Uncommitted: true,
					Unpushed:    3,
					Unknown:     true,
					SessionIDs:  []string{"s1", "s2"},
				}},
				OrphanBranches: []BranchInfo{{
					Name: "old", Upstream: "origin/old", Ahead: 2, Merged: true,
				}},
			},
			func() any { return &WorktreesResp{} },
		},
		{
			"remove", FrameRemoveWorktree,
			RemoveWorktreeReq{ProjectID: "p1", Path: "/repo/.worktrees/x", Force: true, DeleteBranch: true},
			func() any { return &RemoveWorktreeReq{} },
		},
		{
			"create", FrameCreateWorktree,
			CreateWorktreeReq{ProjectID: "p1", Branch: "resurrect-me"},
			func() any { return &CreateWorktreeReq{} },
		},
		{
			"delete-branch", FrameDeleteBranch,
			DeleteBranchReq{ProjectID: "p1", Branch: "stale", Force: true},
			func() any { return &DeleteBranchReq{} },
		},
		{
			"rename", FrameRenameWorktree,
			RenameWorktreeReq{ProjectID: "p1", Path: "/repo/.worktrees/a", NewBranch: "b"},
			func() any { return &RenameWorktreeReq{} },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteJSON(&buf, tc.ft, tc.v); err != nil {
				t.Fatalf("write: %v", err)
			}
			got := tc.into()
			ft, err := ReadJSON(&buf, got)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if ft != tc.ft {
				t.Errorf("type: got %s, want %s", ft, tc.ft)
			}
			// got is a pointer; compare the pointee to the value.
			if !reflect.DeepEqual(reflect.ValueOf(got).Elem().Interface(), tc.v) {
				t.Errorf("round-trip mismatch:\n got %#v\nwant %#v",
					reflect.ValueOf(got).Elem().Interface(), tc.v)
			}
		})
	}
}

// The wire contract is snake_case; a camelCase tag would decode as
// zero on the JS side, which reads as "no worktree" — the exact
// silent-drift failure principle 6 exists to prevent.
func TestWorktreePayloadsUseSnakeCase(t *testing.T) {
	cases := []struct {
		v    any
		want []string
	}{
		{WorktreesResp{ProjectID: "p", RepoRoot: "/r"}, []string{`"project_id"`, `"repo_root"`}},
		{WorktreeInfo{Path: "/p", IsMain: true, Unpushed: 1, SessionIDs: []string{"s"}},
			[]string{`"is_main"`, `"session_ids"`}},
		{RemoveWorktreeReq{ProjectID: "p", Path: "/p", DeleteBranch: true},
			[]string{`"project_id"`, `"delete_branch"`}},
		{RenameWorktreeReq{ProjectID: "p", Path: "/p", NewBranch: "b"}, []string{`"new_branch"`}},
		{DeleteBranchReq{ProjectID: "p", Branch: "b", Force: true}, []string{`"project_id"`, `"branch"`, `"force"`}},
		{CreateSpec{WorktreePath: "/p"}, []string{`"worktree_path"`}},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.v)
		if err != nil {
			t.Fatalf("marshal %T: %v", tc.v, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(b), want) {
				t.Errorf("%T encoded as %s, missing %s", tc.v, b, want)
			}
		}
	}
}

// The frame bytes are the protocol's identity — reassigning one
// silently reinterprets an old client's frames as a different command.
func TestWorktreeFrameTypeValues(t *testing.T) {
	cases := map[FrameType]struct {
		b    byte
		name string
	}{
		FrameListWorktrees:  {0x16, "LIST_WORKTREES"},
		FrameWorktrees:      {0x17, "WORKTREES"},
		FrameRemoveWorktree: {0x18, "REMOVE_WORKTREE"},
		FrameCreateWorktree: {0x19, "CREATE_WORKTREE"},
		FrameRenameWorktree: {0x1a, "RENAME_WORKTREE"},
		FrameDeleteBranch:   {0x1b, "DELETE_BRANCH"},
	}
	for ft, want := range cases {
		if byte(ft) != want.b {
			t.Errorf("%s = 0x%02x, want 0x%02x", want.name, byte(ft), want.b)
		}
		if ft.String() != want.name {
			t.Errorf("String() = %q, want %q", ft.String(), want.name)
		}
	}
}

// The GUI fans control frames out by event name; a missing mapping
// means the browser never hears the reply it is waiting for.
func TestWorktreesHasControlEventName(t *testing.T) {
	name, ok := ControlEventName(FrameWorktrees)
	if !ok {
		t.Fatal("FrameWorktrees has no control event name")
	}
	if name != "worktree:list" {
		t.Errorf("ControlEventName(FrameWorktrees) = %q, want worktree:list", name)
	}
	// Request frames are client → server; they must NOT be fanned out.
	for _, ft := range []FrameType{
		FrameListWorktrees,
		FrameRemoveWorktree,
		FrameCreateWorktree,
		FrameRenameWorktree,
		FrameDeleteBranch,
	} {
		if _, ok := ControlEventName(ft); ok {
			t.Errorf("%s is client→server but has a control event name", ft)
		}
	}
}

// TestWelcomeCarriesDaemonContract pins the field the GUI's
// reload-vs-restart decision is built on. Its omitempty contract is
// load-bearing: a daemon that predates the field sends nothing, which
// must decode to 0 ("unknown"), and the GUI must never read 0 as a
// match — silently reloading into a daemon of unknown behavior is the
// worst outcome the feature can produce.
func TestWelcomeCarriesDaemonContract(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameWelcome, Welcome{
		Version: PROTOCOL_VERSION, BuildID: "def5678", DaemonContract: 7,
		Mode: ModeControl,
	}); err != nil {
		t.Fatalf("write welcome: %v", err)
	}
	var w Welcome
	if _, err := ReadJSON(&buf, &w); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if w.DaemonContract != 7 {
		t.Errorf("DaemonContract = %d, want 7", w.DaemonContract)
	}
}

func TestWelcomeOmitsDaemonContractWhenZero(t *testing.T) {
	var buf bytes.Buffer
	// A daemon built before the contract field existed.
	if err := WriteJSON(&buf, FrameWelcome, Welcome{
		Version: PROTOCOL_VERSION, BuildID: "old", Mode: ModeControl,
	}); err != nil {
		t.Fatalf("write welcome: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte("daemon_contract")) {
		t.Errorf("zero DaemonContract must be omitted; frame = %s", buf.Bytes())
	}
	var w Welcome
	if _, err := ReadJSON(&buf, &w); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if w.DaemonContract != 0 {
		t.Errorf("DaemonContract = %d, want 0", w.DaemonContract)
	}
}
