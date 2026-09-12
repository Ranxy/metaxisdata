// Package dockerutil centralizes how the integration harnesses decide whether a
// Docker-compatible container runtime is reachable. Every suite built on it skips
// instead of failing on machines without a working container runtime, which is
// what AGENTS.md documents for the integration tests.
package dockerutil

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/testcontainers/testcontainers-go"
)

// ErrUnavailable reports that no Docker-compatible container runtime could be
// reached. Callers turn it into a skip rather than a failure.
var ErrUnavailable = errors.New("docker is unavailable")

// unavailableMarkers are the fragments a container-runtime error carries when no
// daemon could be reached, as opposed to a container failing to start.
var unavailableMarkers = []string{
	"cannot connect",
	"is not running",
	"daemon",
	"no such file or directory",
	"permission denied",
	"connection refused",
	"executable file not found",
	"not found in $path",
}

// Available reports whether a Docker-compatible container runtime is reachable.
// DockerProvider.Health closes the provider it measures.
func Available(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}
	return provider.Health(probeCtx) == nil
}

// IsUnavailable reports whether err means no container runtime was reachable.
func IsUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "docker") && !strings.Contains(msg, "podman") {
		return false
	}
	for _, marker := range unavailableMarkers {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// WrapUnavailable marks a container start failure caused by a missing container
// runtime with ErrUnavailable so callers can skip instead of failing the suite.
func WrapUnavailable(err error) error {
	if IsUnavailable(err) {
		return errors.Wrap(ErrUnavailable, err.Error())
	}
	return err
}
