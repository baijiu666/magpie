//go:build windows

package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/yetone/magpie/internal/proc"
)

// TestHostTreeHelper is the host endHost is handed: one that starts a child
// of its own, both then sitting there the way a plugin leaves a browser or a
// daemon behind.
func TestHostTreeHelper(t *testing.T) {
	switch os.Getenv("MAGPIE_HOST_TREE_HELPER") {
	case "host":
		kid := exec.Command(os.Args[0], "-test.run=^TestHostTreeHelper$")
		kid.Env = append(os.Environ(), "MAGPIE_HOST_TREE_HELPER=child")
		if err := kid.Start(); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(os.Getenv("MAGPIE_HOST_TREE_PID"), []byte(strconv.Itoa(kid.Process.Pid)), 0o600)
		time.Sleep(60 * time.Second)
		_ = kid.Process.Kill()
	case "child":
		time.Sleep(60 * time.Second)
	}
}

// A host ended ends what it started.
//
// TerminateProcess ends the host alone. A plugin that started a browser (a
// headless Chromium behind a WAF, say) leaves it running with no parent,
// holding its temp profile and its memory: one plugin doing that on every
// failed refresh left 620 msedge processes and 57GB of %TEMP% in an hour
// (MYCF711/magpie-magicrew-auth#39).
func TestEndHostEndsWhatTheHostStarted(t *testing.T) {
	if testing.Short() {
		t.Skip("starts processes")
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := proc.Command(os.Args[0], "-test.run=^TestHostTreeHelper$")
	cmd.Env = append(os.Environ(), "MAGPIE_HOST_TREE_HELPER=host", "MAGPIE_HOST_TREE_PID="+pidFile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	kid := waitPID(t, pidFile)
	t.Cleanup(func() {
		if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(kid)); err == nil {
			_ = windows.TerminateProcess(h, 1)
			_ = windows.CloseHandle(h)
		}
	})

	endHost(cmd)

	if living(kid) {
		time.Sleep(2 * time.Second)
	}
	if living(kid) {
		t.Fatalf("the child %d the host started outlived it", kid)
	}
}

// waitPID waits for the helper's child to be written down.
func waitPID(t *testing.T, file string) int {
	t.Helper()
	for i := 0; i < 200; i++ {
		if b, err := os.ReadFile(file); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the host never started a child")
	return 0
}

// living says whether pid is still running.
func living(pid int) bool {
	const stillActive = 259 // STILL_ACTIVE
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
