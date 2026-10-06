package cmd

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// The opener must never be the program that parses a command line: the address
// it is given comes from the server, so a `&` in it would start a second
// program under `cmd /c start`.
func TestBrowserCommand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		goos    string
		command string
		args    []string
	}{
		{goos: "darwin", command: "open"},
		{goos: "windows", command: "rundll32", args: []string{"url.dll,FileProtocolHandler"}},
		{goos: "linux", command: "xdg-open"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()

			command, args := browserCommand(tc.goos)
			require.Equal(t, tc.command, command)
			require.Equal(t, tc.args, args)
			require.NotEqual(t, "cmd", command, "the address must not reach the Windows command line")
		})
	}
}

// The reported attack: a server names `https://evil/x?a&calc.exe`, and appending
// it to `cmd /c start` split it into a second command. The opened URL is one
// argument to a program that resolves URL protocols instead.
func TestBrowserCommandHandsTheURLOverAsOneArgument(t *testing.T) {
	t.Parallel()

	address := "https://evil.example.com/x?a&calc.exe"
	command, args := browserCommand("windows")
	argv := slices.Concat(args, []string{address})

	require.Equal(t, "rundll32", command)
	require.Equal(t, []string{"url.dll,FileProtocolHandler", address}, argv)
	require.NotContains(t, argv, "/c")
}

// Only a web address may be handed to the operating system: `javascript:` or a
// local file would otherwise be resolved by whatever the OS associates with it.
func TestValidateBrowserURL(t *testing.T) {
	t.Parallel()

	for _, accepted := range []string{
		"https://mx.example.com/device?user_code=7Q2X-9M4K",
		"http://localhost:8080/device",
		"HTTPS://mx.example.com/device",
	} {
		require.NoError(t, validateBrowserURL(accepted), "address %q must be openable", accepted)
	}

	for _, refused := range []string{
		"javascript:alert(1)",
		"file:///etc/passwd",
		"cmd:/c/calc.exe",
		"https://",
		"https://mx.example.com/%zz",
		"mx.example.com/device",
		"",
	} {
		require.Error(t, validateBrowserURL(refused), "address %q must be refused", refused)
	}
}

// The refusal has to happen before anything is started, so a server-supplied
// address that is not a web page never reaches an opener.
func TestOpenBrowserRefusesANonWebAddress(t *testing.T) {
	t.Parallel()

	require.Error(t, openBrowser("javascript:alert(1)"))
	require.Error(t, openBrowser("file:///etc/passwd"))
}
