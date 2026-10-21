package metrics

import (
	"database/sql"

	"github.com/prometheus/client_golang/prometheus"
)

// dbPoolCollector reports the metadata PostgreSQL pool's database/sql stats. It
// reads stats on every scrape rather than holding the pool, so the store keeps
// ownership of its connections.
type dbPoolCollector struct {
	stats        func() sql.DBStats
	maxOpen      *prometheus.Desc
	connections  *prometheus.Desc
	waitCount    *prometheus.Desc
	waitDuration *prometheus.Desc
}

// NewDBPoolCollector builds a collector for the metadata database pool. stats is
// called once per scrape.
func NewDBPoolCollector(stats func() sql.DBStats) prometheus.Collector {
	fqName := func(name string) string {
		return prometheus.BuildFQName(namespace, "db_pool", name)
	}
	return &dbPoolCollector{
		stats: stats,
		maxOpen: prometheus.NewDesc(
			fqName("max_open_connections"),
			"Maximum number of open connections to the metadata database.",
			nil, nil,
		),
		connections: prometheus.NewDesc(
			fqName("connections"),
			"Metadata database connections by state.",
			[]string{"state"}, nil,
		),
		waitCount: prometheus.NewDesc(
			fqName("wait_total"),
			"Total number of connections waited for on the metadata database.",
			nil, nil,
		),
		waitDuration: prometheus.NewDesc(
			fqName("wait_duration_seconds_total"),
			"Total time blocked waiting for a metadata database connection, in seconds.",
			nil, nil,
		),
	}
}

// Describe implements prometheus.Collector.
func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.maxOpen
	ch <- c.connections
	ch <- c.waitCount
	ch <- c.waitDuration
}

// Collect implements prometheus.Collector.
func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.stats()
	ch <- prometheus.MustNewConstMetric(c.maxOpen, prometheus.GaugeValue, float64(stats.MaxOpenConnections))
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.InUse), "in_use")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(stats.Idle), "idle")
	ch <- prometheus.MustNewConstMetric(c.waitCount, prometheus.CounterValue, float64(stats.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.waitDuration, prometheus.CounterValue, stats.WaitDuration.Seconds())
}
