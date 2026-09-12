package v1

import (
	"context"
	"log/slog"
	"testing"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/config"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestUpdateDebugConfigDrivesRuntimeDebugAndLogLevel(t *testing.T) {
	t.Cleanup(func() { log.LogLevel.Set(slog.LevelInfo) })

	profile := &config.Profile{}
	service := NewSettingService(nil, profile)

	enabled, err := service.UpdateDebugConfig(context.Background(), connect.NewRequest(&v1pb.UpdateDebugConfigRequest{Enabled: true}))
	if err != nil {
		t.Fatalf("UpdateDebugConfig(enabled=true) returned error: %v", err)
	}
	if !enabled.Msg.GetEnabled() {
		t.Error("UpdateDebugConfig(enabled=true) response = false, want true")
	}
	if !profile.RuntimeDebug.Load() {
		t.Error("RuntimeDebug = false after enabling, want true")
	}
	if got := log.LogLevel.Level(); got != slog.LevelDebug {
		t.Errorf("log level = %v after enabling, want %v", got, slog.LevelDebug)
	}

	disabled, err := service.UpdateDebugConfig(context.Background(), connect.NewRequest(&v1pb.UpdateDebugConfigRequest{Enabled: false}))
	if err != nil {
		t.Fatalf("UpdateDebugConfig(enabled=false) returned error: %v", err)
	}
	if disabled.Msg.GetEnabled() {
		t.Error("UpdateDebugConfig(enabled=false) response = true, want false")
	}
	if profile.RuntimeDebug.Load() {
		t.Error("RuntimeDebug = true after disabling, want false")
	}
	if got := log.LogLevel.Level(); got != slog.LevelInfo {
		t.Errorf("log level = %v after disabling, want %v", got, slog.LevelInfo)
	}
}

func TestGetDebugConfigReportsRuntimeDebug(t *testing.T) {
	profile := &config.Profile{}
	service := NewSettingService(nil, profile)

	off, err := service.GetDebugConfig(context.Background(), connect.NewRequest(&v1pb.GetDebugConfigRequest{}))
	if err != nil {
		t.Fatalf("GetDebugConfig returned error: %v", err)
	}
	if off.Msg.GetEnabled() {
		t.Error("GetDebugConfig = true with RuntimeDebug off, want false")
	}

	profile.RuntimeDebug.Store(true)
	on, err := service.GetDebugConfig(context.Background(), connect.NewRequest(&v1pb.GetDebugConfigRequest{}))
	if err != nil {
		t.Fatalf("GetDebugConfig returned error: %v", err)
	}
	if !on.Msg.GetEnabled() {
		t.Error("GetDebugConfig = false with RuntimeDebug on, want true")
	}
}
