package daemon

import (
	"os"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
)

// The ACP tests re-execute this test binary as a fake ACP agent.
func TestMain(m *testing.M) {
	if acptest.IsAgent() {
		acptest.Main()
		return
	}
	os.Exit(m.Run())
}
