package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// funcGauge is a gauge whose samples are computed at scrape time. Queue depths
// and lease counters have no cheap mutation hook (or live in a sync.Map), so
// reporting them from a snapshot keeps them exact without maintaining a second
// counter that can drift.
type funcGauge struct {
	desc     *prometheus.Desc
	snapshot func(emit func(labelValues []string, value float64))
}

func newFuncGauge(subsystem, name, help string, labelNames []string, snapshot func(emit func(labelValues []string, value float64))) *funcGauge {
	return &funcGauge{
		desc:     prometheus.NewDesc(prometheus.BuildFQName(namespace, subsystem, name), help, labelNames, nil),
		snapshot: snapshot,
	}
}

// Describe implements prometheus.Collector.
func (g *funcGauge) Describe(ch chan<- *prometheus.Desc) {
	ch <- g.desc
}

// Collect implements prometheus.Collector.
func (g *funcGauge) Collect(ch chan<- prometheus.Metric) {
	g.snapshot(func(labelValues []string, value float64) {
		ch <- prometheus.MustNewConstMetric(g.desc, prometheus.GaugeValue, value, labelValues...)
	})
}
