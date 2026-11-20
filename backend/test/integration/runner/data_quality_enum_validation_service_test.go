//go:build integration

package runner

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// A rule that binds a literal to an enum column is validated against the labels
// the synced schema declares, end to end through the real server and the real
// PostgreSQL target: an undeclared value is refused at save, and a rule whose
// stored value stops being a declared label fails its run as ERROR(UNSUPPORTED)
// with the label list instead of reaching SQLSTATE 22P02.
func TestDataQualityEnumValidationRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TYPE public.product_status AS ENUM ('active','inactive','archived','draft');
CREATE TABLE public.enum_products (
	id INT PRIMARY KEY,
	status public.product_status NOT NULL
);`))
	// A query rule is refused at save against an instance with no read-only
	// source, even when the save itself fails validation; add one first.
	_, err := env.CreateDataSource(ctx, common.FormatInstance(instanceID), &v1pb.DataSource{
		Type:     v1pb.DataSourceType_READ_ONLY,
		Username: "postgres",
		Password: "postgres",
		Host:     env.PostgresHost,
		Port:     env.PostgresPort,
	}, "readonly", false)
	require.NoError(t, err)
	env.SyncDatabase(ctx, t, databaseName)

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	guid := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "enum_products")
	client := v1connect.NewDataQualityServiceClient(&http.Client{Timeout: 30 * time.Second}, env.BaseURL)
	fixture := &qualityFixture{env: env, ctx: ctx, dataQuality: client}

	create := func(values []string) (*v1pb.QualityRule, error) {
		resp, err := client.CreateQualityRule(ctx, withToken(env.AdminToken(), &v1pb.CreateQualityRuleRequest{
			Parent: "workspaces/-",
			Rule: &v1pb.QualityRule{
				Title:     "status must be a declared product_status label",
				Dimension: v1pb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
				RuleType:  v1pb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
				Target:    &v1pb.QualityTarget{MetaGuid: guid, MetaType: v1pb.MetaType_TABLE},
				Severity:  v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
				Config: &v1pb.QualityRuleConfig{Config: &v1pb.QualityRuleConfig_AcceptedValues{
					AcceptedValues: &v1pb.AcceptedValuesConfig{
						Column:    "status",
						Values:    values,
						AllowNull: false,
					},
				}},
			},
		}))
		if err != nil {
			return nil, err
		}
		return resp.Msg, nil
	}

	// A value the enum does not declare is refused with the declared labels.
	_, err = create([]string{"draft", "discontinued"})
	require.Error(t, err, "an undeclared enum label must be refused at save")
	require.Contains(t, err.Error(), "valid labels")
	require.Contains(t, err.Error(), `"active"`)

	// A subset of the declared labels is a legal rule.
	rule, err := create([]string{"draft", "active"})
	require.NoError(t, err)

	// The same rule edited behind the API to name a removed label must fail the
	// next run as ERROR(UNSUPPORTED), naming the labels, rather than erroring at
	// parameter binding with PostgreSQL's SQLSTATE 22P02.
	resourceID := rule.GetName()[strings.LastIndex(rule.GetName(), "/")+1:]
	// The runner judges the newest immutable revision, so both the revision row
	// and the head carry the edited definition.
	_, err = env.Store.GetDB().ExecContext(ctx, `
UPDATE quality_rule_revision
SET config = jsonb_set(config, '{acceptedValues,values}', '["draft","discontinued"]'::jsonb)
WHERE rule_id = (SELECT id FROM quality_rule WHERE resource_id = $1)
  AND revision = (SELECT revision FROM quality_rule WHERE resource_id = $1)`, resourceID)
	require.NoError(t, err)
	_, err = env.Store.GetDB().ExecContext(ctx, `
UPDATE quality_rule
SET config = jsonb_set(config, '{acceptedValues,values}', '["draft","discontinued"]'::jsonb)
WHERE resource_id = $1`, resourceID)
	require.NoError(t, err)

	execution := fixture.runToCompletion(t, rule.GetName())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_ERROR, execution.GetOutcome())
	require.Equal(t, "UNSUPPORTED", execution.GetErrorCode())
	require.Contains(t, execution.GetErrorMessage(), "valid labels")
	require.Contains(t, execution.GetErrorMessage(), `"archived"`)
}
