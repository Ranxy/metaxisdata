package server

import (
	"time"

	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
)

// connectLongUnaryMethods are the unary procedures that get connectLongUnaryTimeout
// instead of connectUnaryTimeout. They are the calls a user waits on while the
// server reads a target database or waits on a model, and each one already
// bounds itself; the server-side deadline is only the backstop for a dependency
// that never answers.
//
// The keys are the generated procedure constants, so a rename fails the build
// instead of silently moving a slow method back under the default.
var connectLongUnaryMethods = map[string]time.Duration{
	// A profile runs two queries, each with a 120s budget, after waiting for one
	// of the instance's connections.
	v1connect.DataQualityServiceProfileColumnsProcedure: connectLongUnaryTimeout,
	// One synchronous model call each: the rule draft and the label suggestion
	// (which also profiles the requested columns first).
	v1connect.DataQualityServiceSuggestQualityRuleProcedure:  connectLongUnaryTimeout,
	v1connect.DataQualityServiceSuggestColumnLabelsProcedure: connectLongUnaryTimeout,
	// These open the target database and scan it while the caller waits.
	v1connect.InstanceServiceSyncInstanceProcedure:       connectLongUnaryTimeout,
	v1connect.InstanceServiceBatchSyncInstancesProcedure: connectLongUnaryTimeout,
	v1connect.DatabaseServiceSyncDatabaseProcedure:       connectLongUnaryTimeout,
	// A validate-only write pings every data source it names, and one ping may take
	// up to its own 60s timeout, so the request needs room for the sum.
	v1connect.InstanceServiceCreateInstanceProcedure:   connectLongUnaryTimeout,
	v1connect.InstanceServiceUpdateInstanceProcedure:   connectLongUnaryTimeout,
	v1connect.InstanceServiceCreateDataSourceProcedure: connectLongUnaryTimeout,
	v1connect.InstanceServiceUpdateDataSourceProcedure: connectLongUnaryTimeout,
	// Two sequential 30s exchanges with the identity provider land exactly on the
	// default ceiling.
	v1connect.IdentityProviderServiceTestIdentityProviderProcedure: connectLongUnaryTimeout,
}
