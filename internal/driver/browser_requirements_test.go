package driver_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBrowserDriverPrerequisites(t *testing.T) {
	for _, path := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		if _, err := os.Stat(path); err == nil {
			t.Skip("cannot hide an installed macOS browser by changing PATH")
		}
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []string{"0", "1"} {
		t.Run(enabled, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestBrowserDriverWaitsForVisibleSelectorAndScrolls$", "-test.v")
			cmd.Env = append(os.Environ(), "PATH="+t.TempDir(), "SENDIT_TEST_BROWSER="+enabled)
			out, err := cmd.CombinedOutput()
			if enabled == "1" {
				if err == nil || !strings.Contains(string(out), "browser driver requires Chrome or Chromium") {
					t.Fatalf("requested browser verification must fail without Chrome: err=%v\n%s", err, out)
				}
			} else if err != nil || !strings.Contains(string(out), "--- SKIP:") {
				t.Fatalf("unrequested browser verification must remain optional: err=%v\n%s", err, out)
			}
		})
	}
}
