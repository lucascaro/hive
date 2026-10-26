// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// Request/reply correlation and run provenance (spec 528).

// bootID is random and fixed for this hived process: WELCOME.boot_id.
var bootID = sync.OnceValue(func() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand, Go 1.24+)
	return hex.EncodeToString(b)
})

// peekReqID reads the optional req_id of a control request payload
// without caring what else it holds. 0 means none (or not JSON — the
// handler's own decode reports that).
func peekReqID(payload []byte) int {
	var p struct {
		ReqID int `json:"req_id"`
	}
	if json.Unmarshal(payload, &p) != nil || p.ReqID < 0 {
		return 0
	}
	return p.ReqID
}

// withReqID returns ops for handling one frame that carried req_id id:
// every ERROR it sends echoes the id (including ones raised later from
// a goroutine that captured this copy), and ack answers it.
func (o controlOps) withReqID(id int) controlOps {
	if id == 0 {
		return o
	}
	write := o.writeJSON
	o.reqID = id
	o.writeJSON = func(ft wire.FrameType, v any) error {
		if ft == wire.FrameError {
			if e, ok := v.(wire.Error); ok && e.ReqID == 0 {
				e.ReqID = id
				v = e
			}
		}
		return write(ft, v)
	}
	o.sendError = func(code, msg string) {
		_ = o.writeJSON(wire.FrameError, wire.Error{Code: code, Message: msg})
	}
	return o
}

// ack answers the request being handled with ACK, when it carried a
// req_id; otherwise it does nothing.
func (o controlOps) ack(m wire.AckMsg) {
	if o.reqID == 0 {
		return
	}
	m.ReqID = o.reqID
	_ = o.writeJSON(wire.FrameAck, m)
}

// composePrincipal is the provenance label for a connection acting as
// principal and tagging its work with a workflow run: principal plus
// "/workflow:<run>". With no run it is principal unchanged. It reports
// an ERROR code and message when the run id is not acceptable: a run id
// is only meaningful from a plugin (the user has no workflow), and must
// match the run-id grammar, which has no "/" or ":" so it cannot pose
// as another principal.
func composePrincipal(principal, run string) (label, code, msg string) {
	if run == "" {
		return principal, "", ""
	}
	if principal == "" {
		return "", wire.ErrCodeModeNotAllowed, "workflow_run_id is only accepted from a plugin connection"
	}
	if !wire.ValidWorkflowRunID(run) {
		return "", wire.ErrCodeInvalidWorkflowRunID, fmt.Sprintf("workflow_run_id %q must match [a-z0-9][a-z0-9-]{0,62}", run)
	}
	return principal + wire.WorkflowPrincipalSep + run, "", ""
}

// acpAgents is the answer to LIST_ACP_AGENTS: every agent that can run
// as an ACP session, under the user's current settings. It runs the
// login-shell availability probe, so call it off the read loop.
func acpAgents() []wire.AcpAgentInfo {
	st := agent.SpawnSettings()
	out := []wire.AcpAgentInfo{}
	for _, d := range agent.All() {
		spec := d.ACP()
		if spec == nil {
			continue
		}
		info := wire.AcpAgentInfo{
			ID: string(d.ID), Name: d.Name,
			Modes:          []wire.AcpAgentMode{},
			DefaultMode:    spec.DefaultMode,
			Ceiling:        st.ACPCeiling(d.ID),
			NoApprovalGate: spec.NoApprovalGate,
			Experimental:   spec.Experimental,
		}
		for _, m := range spec.Modes {
			info.Modes = append(info.Modes, wire.AcpAgentMode{ID: m.ID, Label: m.Label})
		}
		info.Available, info.Reason = d.ACPAvailable(st)
		out = append(out, info)
	}
	return out
}
