package v1

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

func TestConvertToEnvironment(t *testing.T) {
	t.Parallel()

	environment := convertToEnvironment(&storepb.EnvironmentSetting_Environment{
		Id:    "prod",
		Title: "Production",
		Color: "red",
		Tags:  map[string]string{"team": "platform"},
	})
	if environment.GetName() != "environments/prod" {
		t.Errorf("name = %q, want environments/prod", environment.GetName())
	}
	if environment.GetTitle() != "Production" {
		t.Errorf("title = %q, want Production", environment.GetTitle())
	}
	if environment.GetColor() != "red" {
		t.Errorf("color = %q, want red", environment.GetColor())
	}
	if environment.GetTags()["team"] != "platform" {
		t.Errorf("tags = %v, want team=platform", environment.GetTags())
	}
}

func TestCreateEnvironmentRejectsMissingEnvironment(t *testing.T) {
	t.Parallel()

	service := NewEnvironmentService(nil)
	_, err := service.CreateEnvironment(context.Background(), connect.NewRequest(&v1pb.CreateEnvironmentRequest{}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error code = %v, want InvalidArgument", connect.CodeOf(err))
	}
}

func TestUpdateEnvironmentValidatesMaskBeforeMutating(t *testing.T) {
	t.Parallel()

	service := NewEnvironmentService(nil)

	// A missing mask is rejected before the store is touched; the service holds a
	// nil store here, so reaching it would panic.
	_, err := service.UpdateEnvironment(context.Background(), connect.NewRequest(&v1pb.UpdateEnvironmentRequest{
		Environment: &v1pb.Environment{Name: "environments/prod"},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("missing update_mask error code = %v, want InvalidArgument", connect.CodeOf(err))
	}

	// An unknown mask path is also rejected before the store call.
	_, err = service.UpdateEnvironment(context.Background(), connect.NewRequest(&v1pb.UpdateEnvironmentRequest{
		Environment: &v1pb.Environment{Name: "environments/prod"},
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"bogus"}},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("unknown update_mask error code = %v, want InvalidArgument", connect.CodeOf(err))
	}

	// A malformed resource name never reaches the store either.
	_, err = service.UpdateEnvironment(context.Background(), connect.NewRequest(&v1pb.UpdateEnvironmentRequest{
		Environment: &v1pb.Environment{Name: ""},
		UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad name error code = %v, want InvalidArgument", connect.CodeOf(err))
	}
}

func TestDeleteEnvironmentRejectsMalformedName(t *testing.T) {
	t.Parallel()

	service := NewEnvironmentService(nil)
	_, err := service.DeleteEnvironment(context.Background(), connect.NewRequest(&v1pb.DeleteEnvironmentRequest{Name: "prod"}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error code = %v, want InvalidArgument", connect.CodeOf(err))
	}
}
