package collector

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
)

const (
	namespace = "attunity"
)

var (
	http2          = kingpin.Flag("http2", "Enable HTTP/2 support").Bool()
	taskStateNames = []string{"RUNNING", "STOPPED", "ERROR", "RECOVERING"}
)

// Config is the destination type for when the config file gets
// unmarshalled by the YAML package
type Config struct {
	Link          string         `yaml:"weblink"`
	Username      string         `yaml:"user"`
	Password      string         `yaml:"password"`
	Timeout       int            `yaml:"timeout,omitempty"`
	Verify        bool           `yaml:"verify_https"`
	IncludedTasks []*TasksFilter `yaml:"included_tasks,omitempty"`
	ExcludedTasks []*TasksFilter `yaml:"excluded_tasks,omitempty"`
}

// TasksFilter is used in the config.yml to specify a set of tasks on a server that
// should be excluded or included.
type TasksFilter struct {
	Server string   `yaml:"server"`
	Tasks  []string `yaml:"tasks"`
}

// AttunityCollector implements the Prometheus Collector interface.
// You need to provide it with the AEM server to reach out to,
// along with the username and password to auth to it.
type AttunityCollector struct {
	// the base URL of the server
	// e.g. attunity.example.com
	APIURL string

	// encoded basic http auth info string
	auth string

	// Enterprisemanager.apisessionid header to be included
	// in all API requests
	SessionID string

	// A list of tasks to get details on
	// If empty, get all tasks.
	// The key is server name.
	IncludedTasks []*TasksFilter

	// A list of tasks to NOT get details on
	// Takes precedence over IncludedTasks
	// The key is server name.
	ExcludedTasks []*TasksFilter

	// How long to wait for API responses; 0=infinite
	Timeout int

	httpClient *http.Client
}

// NewAttunityCollector returns a pointer to an attunityCollector object
// which implements the Prometheus Collector interface.
// It should be registered to a Prometheus Registry.
func NewAttunityCollector(cfg *Config) *AttunityCollector {
	var (
		client = &http.Client{}
		auth   = base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		apiURL = cfg.Link + "/api/v1"
	)

	// This is required to force HTTP/1.1
	// Attunity for some reason advertises HTTP/2 despite not supporting it.
	if !*http2 {
		transport := &http.Transport{
			TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		}
		client.Transport = transport
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client.Transport = transport

	if err := validateConfig(*cfg); err != nil {
		logrus.Fatal("Error validating config file: ", err)
	}

	// Set timeout
	if cfg.Timeout > 0 {
		client.Timeout = time.Duration(cfg.Timeout) * time.Second
	}

	collector := &AttunityCollector{
		APIURL:        apiURL,
		auth:          auth,
		IncludedTasks: cfg.IncludedTasks,
		ExcludedTasks: cfg.ExcludedTasks,
		httpClient:    client,
	}
	collector.loginAem()
	return collector
}

func (collector *AttunityCollector) loginAem() (err error) {
	req, err := http.NewRequest("GET", collector.APIURL+"/login", nil)
	if err != nil {
		logrus.Fatal(err)
	}
	req.Header.Set("Authorization", "Basic: "+collector.auth)

	resp, err := collector.httpClient.Do(req)
	if err != nil {
		logrus.Fatal(err)
	}
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		logrus.Fatalf("HTTP %d, PATH: %s; %s", resp.StatusCode, req.URL, body)
	}

	collector.SessionID = resp.Header.Get("Enterprisemanager.apisessionid")
	return err
}

func validateConfig(conf Config) (err []error) {
	if conf.Link == "" {
		err = append(err, errors.New("Link not defined"))
	}

	if conf.Username == "" {
		err = append(err, errors.New("User not defined"))
	}

	if conf.Password == "" {
		err = append(err, errors.New("Password not defined"))
	}

	return
}

// Describe implements the Describe method of the Prometheus Collector interface
func (a *AttunityCollector) Describe(ch chan<- *prometheus.Desc) {

}

// Collect implements the Collect method of the Prometheus Collector interface
func (a *AttunityCollector) Collect(ch chan<- prometheus.Metric) {
	// login during each round of collect to ensure session is valid.
	// This is a little costly, but, error handling is not clean for us to do
	// re-login in a clean way
	a.loginAem()

	// Collect information on what servers are active
	servers, err := a.servers()
	if err != nil {
		logrus.Error(err)
	} else {
		for _, s := range servers {
			ch <- prometheus.MustNewConstMetric(serverDesc, prometheus.GaugeValue, 1.0, s.Name, s.State, s.Platform, s.Host)
		}
	}

	// For each server, concurrently collect detailed information on
	// the tasks that are running on them.
	wg := sync.WaitGroup{}
	wg.Add(len(servers))
	for _, s := range servers {
		go func(s server) {
			taskStates, err := a.taskStates(s.Name)
			if err != nil {
				logrus.Error(err)
			} else {
				for _, t := range taskStates {
					// fmt.Printf("\n\nTEST:::\n%+v", t)
					// inspired by: https://github.com/prometheus/node_exporter/blob/v0.18.1/collector/systemd_linux.go#L222
					for _, tsn := range taskStateNames {
						value := 0.0
						if t.State == tsn {
							value = 1.0
						}
						ch <- prometheus.MustNewConstMetric(taskStateDesc, prometheus.GaugeValue, value, s.Name, t.Name, tsn)
					}

					// Get details on each of the tasks and send them to the channel, too
					t.details(s.Name, a, ch)
				}
			}
			wg.Done()
		}(s)
	}

	// For each server, collect high level details such as
	// how many tasks are in each state on them and
	// how many days until license expiration
	wg.Add(len(servers))
	for _, s := range servers {
		go func(s server) {
			serverDeets, err := a.serverDetails(s.Name)
			if err != nil {
				logrus.Error(err)
			} else {
				// Create metrics for task totals by state
				// These counts will not be affected by included/excluded task in the config file
				ch <- prometheus.MustNewConstMetric(serverTasksDesc, prometheus.GaugeValue, float64(serverDeets.TaskSummary.Running), s.Name, "RUNNING")
				ch <- prometheus.MustNewConstMetric(serverTasksDesc, prometheus.GaugeValue, float64(serverDeets.TaskSummary.Stopped), s.Name, "STOPPED")
				ch <- prometheus.MustNewConstMetric(serverTasksDesc, prometheus.GaugeValue, float64(serverDeets.TaskSummary.Error), s.Name, "ERROR")
				ch <- prometheus.MustNewConstMetric(serverTasksDesc, prometheus.GaugeValue, float64(serverDeets.TaskSummary.Recovering), s.Name, "RECOVERING")

				// Create metric for license expiration
				ch <- prometheus.MustNewConstMetric(serverLicenseExpirationDesc, prometheus.GaugeValue, float64(serverDeets.License.DaysToExpiration), s.Name)

				// Create metrics for Resource Utilization
				ch <- prometheus.MustNewConstMetric(diskUsageDesc, prometheus.GaugeValue, float64(serverDeets.ResourceUtilization.DiskUsage), s.Name)
				ch <- prometheus.MustNewConstMetric(memoryUsageDesc, prometheus.GaugeValue, float64(serverDeets.ResourceUtilization.MemoryUsage), s.Name)
				ch <- prometheus.MustNewConstMetric(attunityCPUPercentageDesc, prometheus.GaugeValue, float64(serverDeets.ResourceUtilization.AttunityCPU), s.Name)
				ch <- prometheus.MustNewConstMetric(machineCPUPercentageDesc, prometheus.GaugeValue, float64(serverDeets.ResourceUtilization.MachineCPU), s.Name)
			}
			wg.Done()
		}(s)
	}
	wg.Wait()
}

// APIRequest performs an API Request by taking a path to GET and a pointer
// to a data structure into which the JSON response can be unmarshalled
func (a *AttunityCollector) APIRequest(path string, datastructure interface{}) (err error) {
	req, err := http.NewRequest("GET", a.APIURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Enterprisemanager.apisessionid", a.SessionID)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return
	}

	if resp.StatusCode != 200 {
		err = errors.New("Unexpected status code")
		body, _ := ioutil.ReadAll(resp.Body)
		return errors.Wrapf(err, "HTTP %d; PATH: %s; %s", resp.StatusCode, path, body)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	if err = json.Unmarshal(body, datastructure); err != nil {
		err = errors.Wrap(err, string(body))
		return
	}

	//fmt.Printf("%+v", datastructure)

	return
}
