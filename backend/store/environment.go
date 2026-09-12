package store

import (
	"context"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func (s *Store) GetEnvironmentByID(ctx context.Context, id string) (*storepb.EnvironmentSetting_Environment, error) {
	environments, err := s.GetEnvironmentSetting(ctx)
	if err != nil {
		return nil, err
	}
	for _, environment := range environments.GetEnvironments() {
		if environment.Id == id {
			return environment, nil
		}
	}
	return nil, nil
}
