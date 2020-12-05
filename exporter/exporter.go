package exporter

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os/exec"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

const Namespace = "rpi"

var executor = exec.Command

// Exporter collects metrics from a local Raspberry Pi
type Exporter struct {
	logger         *logrus.Logger
	throttleStatus io.Reader

	underVoltageDetected *prometheus.Desc
	armFrequencyCapped   *prometheus.Desc
	throttledState       *prometheus.Desc
	softTempLimit        *prometheus.Desc
}

// New returns an initialized exporter
func New(logger *logrus.Logger) *Exporter {
	return &Exporter{
		logger: logger,
		underVoltageDetected: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "", "undervoltage_detected"),
			"Power supply voltage is currently under threshold",
			nil,
			nil,
		),
		armFrequencyCapped: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "", "arm_frequency_capped"),
			"ARM chip clock speed is currently capped",
			nil,
			nil,
		),
		time: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "", "time_seconds"),
			"current UNIX time according to the server.",
			nil,
			nil,
		),
	}
}

// Collect fetches the statistics from the configured memcached server, and
// delivers them as Prometheus metrics. It implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	c, err := memcache.New(e.address)
	if err != nil {
		ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, 0)
		level.Error(e.logger).Log("msg", "Failed to connect to memcached", "err", err)
		return
	}
	c.Timeout = e.timeout

	up := float64(1)
	stats, err := c.Stats()
	if err != nil {
		level.Error(e.logger).Log("msg", "Failed to collect stats from memcached", "err", err)
		up = 0
	}
	statsSettings, err := c.StatsSettings()
	if err != nil {
		level.Error(e.logger).Log("msg", "Could not query stats settings", "err", err)
		up = 0
	}

	if err := e.parseStats(ch, stats); err != nil {
		up = 0
	}
	if err := e.parseStatsSettings(ch, statsSettings); err != nil {
		up = 0
	}

	ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, up)
}

func getThrottled() (io.Reader, error) {
	cmd := exec.Command("vcgencmd", "get_throttled")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to read stdout pipe: %w", err)
	}

	cmd.Start()
	data, err := ioutil.ReadAll(out)
	if err != nil {
		return nil, fmt.Errorf("failed to read command output: %w", err)
	}
	err = cmd.Wait()
	if err != nil {
		err = fmt.Errorf("vcgencmd execution failed: %w", err)
	}

	return bytes.NewReader(data), err
}

func hasBit(n int, pos uint) bool {
	val := n & (1 << pos)
	return (val > 0)
}
