package prometheusmetrics

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rcrowley/go-metrics"
)

// Provider provides a container with config parameters for the
// Prometheus Exporter
type Provider struct {
	r             metrics.Registry
	namespace     string
	subsystem     string
	promRegistry  prometheus.Registerer // Prometheus registry
	flushInterval time.Duration         // interval to update prom metrics

	gauges        map[string]prometheus.Gauge
	customMetrics map[string]*CustomCollector
	constLabels   prometheus.Labels

	histogramBuckets []float64
	timerBuckets     []float64

	ticker *time.Ticker
	mutex  *sync.Mutex
}

// NewProvider returns a Provider that produces Prometheus metrics.
// Namespace and subsystem are applied to all produced metrics.
func NewProvider(
	r metrics.Registry,
	namespace string,
	subsystem string,
	promRegistry prometheus.Registerer,
	flushInterval time.Duration,
) *Provider {
	return &Provider{
		r:             r,
		namespace:     namespace,
		subsystem:     subsystem,
		promRegistry:  promRegistry,
		flushInterval: flushInterval,

		gauges:        make(map[string]prometheus.Gauge),
		customMetrics: make(map[string]*CustomCollector),

		histogramBuckets: []float64{0.05, 0.1, 0.25, 0.50, 0.75, 0.9, 0.95, 0.99},
		timerBuckets:     []float64{0.50, 0.95, 0.99, 0.999},

		mutex: new(sync.Mutex),
	}
}

func (p *Provider) WithHistogramBuckets(b []float64) *Provider {
	p.histogramBuckets = b
	return p
}

func (p *Provider) WithTimerBuckets(b []float64) *Provider {
	p.timerBuckets = b
	return p
}

// WithConstLabels adds additional labels to all metrics in this registry
func (p *Provider) WithConstLabels(labels prometheus.Labels) *Provider {
	p.constLabels = labels
	return p
}

func (p *Provider) UpdatePrometheusMetrics() {
	p.ticker = time.NewTicker(p.flushInterval)
	for range p.ticker.C {
		p.UpdatePrometheusMetricsOnce()
	}
}

func (p *Provider) Stop() {
	if p.ticker == nil {
		return
	}
	p.ticker.Stop()
}

func (p *Provider) UpdatePrometheusMetricsOnce() error {
	p.r.Each(func(name string, i interface{}) {
		switch metric := i.(type) {
		case metrics.Counter:
			p.gaugeFromNameAndValue(name, float64(metric.Count()))
		case metrics.Gauge:
			p.gaugeFromNameAndValue(name, float64(metric.Value()))
		case metrics.GaugeFloat64:
			p.gaugeFromNameAndValue(name, metric.Value())
		case metrics.Histogram:
			samples := metric.Snapshot().Sample().Values()
			if len(samples) > 0 {
				lastSample := samples[len(samples)-1]
				p.gaugeFromNameAndValue(name, float64(lastSample))
			}

			p.histogramFromNameAndMetric(name, metric, p.histogramBuckets)
		case metrics.Meter:
			lastSample := metric.Snapshot().Rate1()
			p.gaugeFromNameAndValue(name, lastSample)
		case metrics.Timer:
			lastSample := metric.Snapshot().Rate1()
			p.gaugeFromNameAndValue(name, lastSample)

			p.histogramFromNameAndMetric(name, metric, p.timerBuckets)
		}
	})
	return nil
}

func (p *Provider) flattenKey(key string) string {
	key = strings.Replace(key, " ", "_", -1)
	key = strings.Replace(key, ".", "_", -1)
	key = strings.Replace(key, "-", "_", -1)
	key = strings.Replace(key, "=", "_", -1)
	key = strings.Replace(key, "/", "_", -1)
	return key
}

func (p *Provider) createKey(name string) string {
	return fmt.Sprintf("%s_%s_%s", p.namespace, p.subsystem, name)
}

func (p *Provider) gaugeFromNameAndValue(name string, val float64) {
	key := p.createKey(name)
	g, ok := p.gauges[key]
	if !ok {
		g = prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   p.flattenKey(p.namespace),
			Subsystem:   p.flattenKey(p.subsystem),
			Name:        p.flattenKey(name),
			Help:        name,
			ConstLabels: p.constLabels,
		})
		p.promRegistry.Register(g)
		p.gauges[key] = g
	}
	g.Set(val)
}

func (p *Provider) histogramFromNameAndMetric(name string, goMetric interface{}, buckets []float64) {
	key := p.createKey(name)

	collector, ok := p.customMetrics[key]
	if !ok {
		collector = NewCustomCollector(p.mutex)
		p.promRegistry.MustRegister(collector)
		p.customMetrics[key] = collector
	}

	var ps []float64
	var count uint64
	var sum float64
	var typeName string

	switch metric := goMetric.(type) {
	case metrics.Histogram:
		snapshot := metric.Snapshot()
		ps = snapshot.Percentiles(buckets)
		count = uint64(snapshot.Count())
		sum = float64(snapshot.Sum())
		typeName = "histogram"
	case metrics.Timer:
		snapshot := metric.Snapshot()
		ps = snapshot.Percentiles(buckets)
		count = uint64(snapshot.Count())
		sum = float64(snapshot.Sum())
		typeName = "timer"
	default:
		panic(fmt.Sprintf("unexpected metric type %T", goMetric))
	}

	bucketVals := make(map[float64]uint64)
	for ii, bucket := range buckets {
		bucketVals[bucket] = uint64(ps[ii])
	}

	desc := prometheus.NewDesc(
		prometheus.BuildFQName(
			p.flattenKey(p.namespace),
			p.flattenKey(p.subsystem),
			fmt.Sprintf("%s_%s", p.flattenKey(name), typeName),
		),
		p.flattenKey(name),
		[]string{},
		p.constLabels,
	)

	if constHistogram, err := prometheus.NewConstHistogram(
		desc,
		count,
		sum,
		bucketVals,
	); err == nil {
		p.mutex.Lock()
		collector.metric = constHistogram
		p.mutex.Unlock()
	}
}

// for collecting prometheus.constHistogram objects
type CustomCollector struct {
	prometheus.Collector

	metric prometheus.Metric
	mutex  *sync.Mutex
}

func NewCustomCollector(mutex *sync.Mutex) *CustomCollector {
	return &CustomCollector{
		mutex: mutex,
	}
}

func (c *CustomCollector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.Lock()
	if c.metric != nil {
		val := c.metric
		ch <- val
	}
	c.mutex.Unlock()
}

func (c *CustomCollector) Describe(ch chan<- *prometheus.Desc) {
	// empty method to fulfill prometheus.Collector interface
}
