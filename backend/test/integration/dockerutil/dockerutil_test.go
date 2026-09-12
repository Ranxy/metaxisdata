package dockerutil

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsUnavailable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"daemon not running", errors.New("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?"), true},
		{"connection refused", errors.New("error during connect: Post \"http://%2Fvar%2Frun%2Fdocker.sock/v1.41/containers/create\": dial unix /var/run/docker.sock: connect: connection refused"), true},
		{"socket permission denied", errors.New("Got permission denied while trying to connect to the Docker daemon socket"), true},
		{"docker binary missing", errors.New(`exec: "docker": executable file not found in $PATH`), true},
		{"podman socket missing", errors.New("podman: no such file or directory"), true},
		{"container start failure", errors.New("container exited with code 1: mysql did not become ready"), false},
		{"unrelated error", errors.New("port 5432 is already allocated"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsUnavailable(tt.err))
		})
	}
}

func TestWrapUnavailable(t *testing.T) {
	require.EqualError(t, WrapUnavailable(errors.New("port is already allocated")), "port is already allocated")

	wrapped := WrapUnavailable(errors.New("Cannot connect to the Docker daemon"))
	require.ErrorIs(t, wrapped, ErrUnavailable)
}
