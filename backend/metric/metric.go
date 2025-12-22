// Package metric provides the API definition for metrics.
package metric

import (
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/metric"
)

const (
	// InstanceCountMetricName is the metric name for instance count.
	InstanceCountMetricName metric.Name = "mt.instance.count"
	// IssueCountMetricName is the metric name for issue count.
	IssueCountMetricName metric.Name = "mt.issue.count"
	// ProjectCountMetricName is the metric name for project count.
	ProjectCountMetricName metric.Name = "mt.project.count"
	// UserCountMetricName is the metric name for user count.
	UserCountMetricName metric.Name = "mt.user.count"
	// ServiceAccountCountMetricName is the metric name for service account count.
	ServiceAccountCountMetricName metric.Name = "mt.service-account.count"
	// OpenAPIMetricName is the metric name for OpenAPI.
	OpenAPIMetricName metric.Name = "mt.api.call"
	// PrincipalRegistrationMetricName is the metric name for the principal registration event.
	PrincipalRegistrationMetricName metric.Name = "mt.principal.registration"
	// PrincipalLoginMetricName is the metric name for principal login event.
	PrincipalLoginMetricName metric.Name = "mt.principal.login"
	// IssueCreateMetricName is the metric name for issue creation event.
	IssueCreateMetricName metric.Name = "mt.issue.create"
	// APIRequestMetricName is the metric name for api request.
	APIRequestMetricName metric.Name = "mt.api.request"
	// InstanceCreateMetricName is the metric name for instance creation event.
	InstanceCreateMetricName metric.Name = "mt.instance.create"
)

// InstanceCountMetric is the API message for mt.instance.count.
type InstanceCountMetric struct {
	Engine        storepb.Engine
	EnvironmentID string
	Count         int
}

// IssueCountMetric is the API message for mt.issue.count.
type IssueCountMetric struct {
	Count int
}

// ProjectCountMetric is the API message for project count metric.
type ProjectCountMetric struct {
	Count int
}
