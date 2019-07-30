package collector

import (
	"encoding/base64"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "attunity"
)

var (
	taskStateNames = []string{"RUNNING", "STOPPED", "ERROR", "RECOVERING"}
)

// Config is the destination type for when the config file gets
// unmarshalled by the YAML package
type Config struct {
	Server   string   `yaml:"server"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	Tasks    []string `yaml:"tasks,omitempty"`
}

// AttunityCollector implements the Prometheus Collector interface.
// You need to provide it with the AEM server to reach out to,
// along with the username and password to auth to it.
type AttunityCollector struct {
	// the base URL of the server
	// e.g. attunity.example.com
	ServerURL string

	// base-64 encoded string to be added
	// to the Authentication HTTP header
	AuthHeader string

	// A list of tasks to get details on
	Tasks []string

	httpClient *http.Client
}

// NewAttunityCollector returns a pointer to an attunityCollector object
// which implements the Prometheus Collector interface.
// It should be registered to a Prometheus Registry.
func NewAttunityCollector(cfg *Config) *AttunityCollector {
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))

	return &AttunityCollector{
		ServerURL:  cfg.Server,
		AuthHeader: auth,
		Tasks:      cfg.Tasks,
	}

}

// Describe implements the Describe method of the Prometheus Collector interface
func (a *AttunityCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "task_count"),
		"Number of tasks per state per server",
		[]string{"server", "state"},
		nil,
	)

	prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "task_state"),
		"Number of tasks broken down by state",
		[]string{"server", "state"},
		nil,
	)

	prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "servers"),
		"Number of currently running tasks in Attunity",
		[]string{"server", "state"},
		nil,
	)
}

// Collect implements the Collect method of the Prometheus Collector interface
func (a *AttunityCollector) Collect(ch chan<- prometheus.Metric) {

}
