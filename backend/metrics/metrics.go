// Package metrics owns the server's Prometheus instruments.
//
// The server builds one Metrics in NewServer and passes the same value to the
// subsystems it instruments. A nil *Metrics disables instrumentation: every
// recorder checks its receiver, so a test that builds a runner as a zero-value
// literal, or a constructor called without metrics, keeps working and records
// nothing.
//
// Instruments are never exported directly. Callers record through typed methods,
// which keeps the label vocabulary bounded and the proto-to-label mapping in one
// place. The package test holds a golden list of metric names and a allowlist of
// label names: adding either without updating the test fails the package.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// namespace is the Prometheus namespace every instrument uses.
const namespace = "metaxisdata"

// Metrics groups the process's instruments by area. The zero value is not
// usable; call New.
type Metrics struct {
	HTTP               httpMetrics
	RPC                rpcMetrics
	Runner             runnerMetrics
	Schemasync         schemasyncMetrics
	Lineage            lineageMetrics
	OpenLineage        openLineageMetrics
	Quality            qualityMetrics
	Query              queryMetrics
	LLM                llmMetrics
	Maintenance        maintenanceMetrics
	LineageValidation  lineageValidationMetrics
	InstanceConnection instanceConnectionMetrics
	Notification       notificationMetrics
}

// New builds every instrument without registering it. A test that wants
// isolated counters calls New once per case; the server registers that one
// value's collectors on its private registry.
func New() *Metrics {
	return &Metrics{
		HTTP:               newHTTPMetrics(),
		RPC:                newRPCMetrics(),
		Runner:             newRunnerMetrics(),
		Schemasync:         newSchemasyncMetrics(),
		Lineage:            newLineageMetrics(),
		OpenLineage:        newOpenLineageMetrics(),
		Quality:            newQualityMetrics(),
		Query:              newQueryMetrics(),
		LLM:                newLLMMetrics(),
		Maintenance:        newMaintenanceMetrics(),
		LineageValidation:  newLineageValidationMetrics(),
		InstanceConnection: newInstanceConnectionMetrics(),
		Notification:       newNotificationMetrics(),
	}
}

// Register registers every instrument on reg. It returns the first failure so
// the server refuses to start with a broken registry instead of serving an
// incomplete endpoint.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	for _, collector := range m.collectors() {
		if err := reg.Register(collector); err != nil {
			return err
		}
	}
	return nil
}

// collectors returns every instrument this process must register.
func (m *Metrics) collectors() []prometheus.Collector {
	var collectors []prometheus.Collector
	collectors = append(collectors, m.HTTP.collectors()...)
	collectors = append(collectors, m.RPC.collectors()...)
	collectors = append(collectors, m.Runner.collectors()...)
	collectors = append(collectors, m.Schemasync.collectors()...)
	collectors = append(collectors, m.Lineage.collectors()...)
	collectors = append(collectors, m.OpenLineage.collectors()...)
	collectors = append(collectors, m.Quality.collectors()...)
	collectors = append(collectors, m.Query.collectors()...)
	collectors = append(collectors, m.LLM.collectors()...)
	collectors = append(collectors, m.Maintenance.collectors()...)
	collectors = append(collectors, m.LineageValidation.collectors()...)
	collectors = append(collectors, m.InstanceConnection.collectors()...)
	collectors = append(collectors, m.Notification.collectors()...)
	return collectors
}
