package audit

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// poolCollector exports pgxpool statistics as artstudio_db_pool_*.
type poolCollector struct {
	pool                                *pgxpool.Pool
	total, idle, acquired, maxConns     *prometheus.Desc
	acquireCount, acquireDurationSecond *prometheus.Desc
}

func newPoolCollector(pool *pgxpool.Pool) *poolCollector {
	d := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc("artstudio_db_pool_"+name, help, nil, nil)
	}
	return &poolCollector{
		pool:                  pool,
		total:                 d("total_conns", "Connections in the pool."),
		idle:                  d("idle_conns", "Idle connections."),
		acquired:              d("acquired_conns", "Connections currently acquired."),
		maxConns:              d("max_conns", "Configured maximum connections."),
		acquireCount:          d("acquire_total", "Cumulative successful acquires."),
		acquireDurationSecond: d("acquire_duration_seconds_total", "Cumulative time spent acquiring, in seconds."),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{c.total, c.idle, c.acquired, c.maxConns, c.acquireCount, c.acquireDurationSecond} {
		ch <- d
	}
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(s.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.idle, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.acquired, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.maxConns, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.acquireCount, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.acquireDurationSecond, prometheus.CounterValue, s.AcquireDuration().Seconds())
}
