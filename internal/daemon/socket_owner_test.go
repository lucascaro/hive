package daemon

import (
	"os"
	"testing"
)

// An inode number is reusable, so a replacement daemon's fresh socket
// can be os.SameFile as the one this daemon bound — observed on Linux
// /tmp, where the dying daemon then unlinked the live daemon's socket
// and left it serving an inode nobody can dial.
//
// This fabricates that collision exactly: a Daemon whose sockInfo IS
// the stat of a live daemon's socket (what inode reuse produces) must
// still leave the file alone, because something is serving it.
func TestRemoveOwnSocketLeavesALiveSocketAlone(t *testing.T) {
	skipOnWindows(t)
	live := startTestDaemon(t)
	sock := live.SocketPath()
	info, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("stat live socket: %v", err)
	}

	dying := &Daemon{sock: sock, sockInfo: info}
	dying.removeOwnSocket()

	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("the live daemon's socket was unlinked: %v", err)
	}
	c := dial(t, live)
	_ = c.Close()
}

// The other half: with nothing serving the path, teardown still cleans
// up after itself.
func TestRemoveOwnSocketRemovesADeadSocket(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	sock := tmp + "/s"
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	(&Daemon{sock: sock, sockInfo: info}).removeOwnSocket()

	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("stat after removeOwnSocket = %v, want not-exist", err)
	}
}
