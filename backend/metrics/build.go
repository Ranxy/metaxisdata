package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// NewBuildInfoCollector reports the build the process was started from. It is a
// constant 1 labeled with the injected version and commit, the standard
// *_build_info pattern; the series changes on upgrade, which is expected.
func NewBuildInfoCollector(version, commit string) prometheus.Collector {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace:   namespace,
		Name:        "build_info",
		Help:        "Build information: always 1, labeled with the release version and git commit.",
		ConstLabels: prometheus.Labels{"version": version, "git_commit": commit},
	}, func() float64 { return 1 })
}
