package cmd

import (
	"strings"
	"testing"
)

func TestGreetingTextRendersTheBanner(t *testing.T) {
	got := greetingText(8083)

	// Routing the banner through slog is the regression this guards: the handler
	// quotes a message containing a newline, so the art collapsed into one line.
	if strings.Contains(got, `\n`) {
		t.Fatalf("banner contains escaped newlines, so it was never rendered as art:\n%s", got)
	}
	// Each art row must begin its own output line.
	for _, row := range []string{"████╗ ████║", "╚═╝     ╚═╝"} {
		if !strings.Contains(got, "\n"+row) {
			t.Errorf("banner is missing the art row starting at a line boundary: %q", row)
		}
	}
	if !strings.Contains(got, "\nServer has started on port 8083 🚀\n") {
		t.Errorf("banner does not carry the startup line:\n%s", got)
	}
}
