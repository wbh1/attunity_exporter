package collector

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
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
	taskStateNames = []string{"RUNNING", "STOPPED", "ERROR", "RECOVERY"}
)

// Config is the destination type for when the config file gets
// unmarshalled by the YAML package
type Config struct {
	Link          string         `yaml:"weblink"`
	Username      string         `yaml:"user"`
	Password      string         `yaml:"password"`
	IgnoreCert    bool           `yaml:"ignore_cert"`
	Timeout       int            `yaml:"timeout,omitempty"`
	IncludedTasks []*TasksFilter `yaml:"included_tasks,omitempty"`
	ExcludedTasks []*TasksFilter `yaml:"excluded_tasks,omitempty"`
	IncludedTags  []*string      `yaml:"included_tags,omitempty"`
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

	// A list of tags to get task details on
	IncludedTags []*string

	// How long to wait for API responses; 0=infinite
	Timeout int

	httpClient *http.Client

	auth string
}

// NewAttunityCollector returns a pointer to an attunityCollector object
// which implements the Prometheus Collector interface.
// It should be registered to a Prometheus Registry.
func NewAttunityCollector(cfg *Config) *AttunityCollector {
	var (
		auth   = base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		apiURL = cfg.Link + "/api/v1"
	)

	if err := validateConfig(*cfg); err != nil {
		logrus.Fatal("Error validating config file: ", err)
	}

	client, sessionID := configureClient(cfg.Timeout, apiURL, auth, cfg.IgnoreCert)

	return &AttunityCollector{
		APIURL:        apiURL,
		SessionID:     sessionID,
		IncludedTasks: cfg.IncludedTasks,
		ExcludedTasks: cfg.ExcludedTasks,
		IncludedTags:  cfg.IncludedTags,
		httpClient:    client,
		auth:          auth,
	}

}

func configureClient(timeout int, apiURL string, auth string, ignoreCert bool) (*http.Client, string) {
	client := &http.Client{}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: ignoreCert},
	}
	// This is required to force HTTP/1.1
	// Attunity for some reason advertises HTTP/2 despite not supporting it. :(
	if !*http2 {
		transport.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)

	}
	client.Transport = transport

	// Set timeout
	if timeout > 0 {
		client.Timeout = time.Duration(timeout) * time.Second
	}

	sessionID := getSessionID(client, apiURL, auth)

	return client, sessionID

}

func getSessionID(client *http.Client, apiURL, auth string) string {
	req, err := http.NewRequest("GET", apiURL+"/login", nil)
	if err != nil {
		logrus.Fatal(err)
	}
	req.Header.Set("Authorization", "Basic: "+auth)

	resp, err := client.Do(req)
	if err != nil {
		logrus.Fatal(err)
	}
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		logrus.Fatalf("HTTP %d, PATH: %s; %s", resp.StatusCode, req.URL, body)
	}

	return resp.Header.Get("Enterprisemanager.apisessionid")

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
	// Hi I do nothing
}

// Collect implements the Collect method of the Prometheus Collector interface
func (a *AttunityCollector) Collect(ch chan<- prometheus.Metric) {

	// Collect information on what servers are active
	servers, err := a.servers()
	if err != nil {

		// If the error is because the session_id expired, attempt to get a new one and collect info again
		// else, just fail with an invalid metric containing the error
		if strings.Contains(err.Error(), "INVALID_SESSION_ID") {
			a.SessionID = getSessionID(a.httpClient, a.APIURL, a.auth)

			servers, err = a.servers()
			if err != nil {
				logrus.Error(err)
				ch <- prometheus.NewInvalidMetric(prometheus.NewDesc("attunity_error", "Error scraping target", nil, nil), err)
				return
			}

		} else {
			logrus.Error(err)
			ch <- prometheus.NewInvalidMetric(prometheus.NewDesc("attunity_error", "Error scraping target", nil, nil), err)
			return
		}

	} // end error handling for a.servers()

	for _, s := range servers {
		ch <- prometheus.MustNewConstMetric(serverDesc, prometheus.GaugeValue, 1.0, s.Name, s.State, s.Platform, s.Host)
	}

	// For each server, concurrently collect detailed information on
	// the tasks that are running on them.
	wg := sync.WaitGroup{}
	wg.Add(len(servers))
	for _, s := range servers {
		// If the Server is not monitored, then it will not have any tasks so we can skip this bit.
		if s.State != "MONITORED" {
			wg.Done()
			continue
		}
		go func(s server) {
			taskStates, err := a.taskStates(s.Name)
			if err != nil {
				logrus.Error(err)
			} else {
				for _, t := range taskStates {
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

	return
}
