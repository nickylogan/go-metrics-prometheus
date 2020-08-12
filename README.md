# go-metrics-prometheus

[![Build Status](https://api.travis-ci.org/deathowl/go-metrics-prometheus.svg)](https://travis-ci.org/deathowl/go-metrics-prometheus)

This is a reporter for the go-metrics library which will post the metrics to the prometheus client registry. It just updates the registry, taking care of exporting the metrics is still your responsibility.

Usage:

```golang
import (
    "github.com/rcrowley/go-metrics"
    prometheusmetrics "github.com/deathowl/go-metrics-prometheus"
    "github.com/prometheus/client_golang/prometheus"
)

func main() {
    metricsRegistry := metrics.NewRegistry()
    p := prometheusmetrics.NewProvider(
        metricsRegistry,
        "whatever", "something",
        prometheus.DefaultRegisterer,
        1*time.Second,
    )
    go p.UpdatePrometheusMetrics()
}
```
