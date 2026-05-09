package agent

// ResumeArgsWithID returns the argv that resumes the agent's prior
// conversation by exact ID, or nil if the agent does not support
// ID-based resume. Callers fall through to def.ResumeCmd or def.Cmd.
//
// Per-agent matrix:
//
//	claude   →  ["claude", "--resume", id]
//	codex    →  ["codex", "resume", id]
//	others   →  nil  (gemini/copilot/aider do not expose stable IDs)
func ResumeArgsWithID(id ID, convID string) []string {
	if convID == "" {
		return nil
	}
	switch id {
	case IDClaude:
		return []string{"claude", "--resume", convID}
	case IDCodex:
		return []string{"codex", "resume", convID}
	default:
		return nil
	}
}
