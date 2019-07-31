package collector

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

	taskCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "task_count"),
		"Number of tasks per state per server",
		[]string{"server", "state"},
		nil,
	)

	taskStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "task_state"),
		"Number of tasks broken down by state",
		[]string{"server", "task", "state"},
		nil,
	)

	serversDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "servers"),
		"Servers currently found in Attunity",
		[]string{"server", "state", "platform", "host"},
		nil,
	)

	taskTotalLatencyDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "total_latency_seconds"),
		"Total latency for this task",
		[]string{"server", "task"},
		nil,
	)
)

// Config is the destination type for when the config file gets
// unmarshalled by the YAML package
type Config struct {
	Server   string   `yaml:"server"`
	Username string   `yaml:"user"`
	Password string   `yaml:"password"`
	Timeout  int      `yaml:"timeout,omitempty"`
	Tasks    []string `yaml:"tasks,omitempty"`
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
	Tasks []string

	// How long to wait for API responses; 0=infinite
	Timeout int

	httpClient *http.Client
}

type server struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Platform string `json:"platform"`
	State    string `json:"state"`
}

type task struct {
	Name       string     `json:"name"`
	State      string     `json:"state"`
	CDCLatency cdcLatency `json:"cdc_latency"`
}

type cdcLatency struct {
	SourceLatency string `json:"source_latency"`
	TotalLatency  string `json:"total_latency"`
}

// NewAttunityCollector returns a pointer to an attunityCollector object
// which implements the Prometheus Collector interface.
// It should be registered to a Prometheus Registry.
func NewAttunityCollector(cfg *Config) *AttunityCollector {
	var (
		client = &http.Client{}
		auth   = base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		apiURL = cfg.Server + "/api/v1"
	)

	// This is required to force HTTP/1.1
	// Attunity for some reason advertises HTTP/2 despite not supporting it.
	if !*http2 {
		transport := &http.Transport{
			TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		}
		client.Transport = transport
	}

	// Set timeout
	if cfg.Timeout > 0 {
		client.Timeout = time.Duration(cfg.Timeout) * time.Second
	}

	req, err := http.NewRequest("GET", apiURL+"/login", nil)
	if err != nil {
		logrus.Fatal(err)
	}
	req.Header.Set("Authorization", "Basic: "+auth)
	resp, err := client.Do(req)
	if err != nil {
		logrus.Fatal(err)
	}
	sessionID := resp.Header.Get("Enterprisemanager.apisessionid")

	return &AttunityCollector{
		APIURL:     apiURL,
		SessionID:  sessionID,
		Tasks:      cfg.Tasks,
		httpClient: client,
	}

}

// Describe implements the Describe method of the Prometheus Collector interface
func (a *AttunityCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- taskCountDesc
	ch <- taskStateDesc
	ch <- serversDesc
}

// Collect implements the Collect method of the Prometheus Collector interface
func (a *AttunityCollector) Collect(ch chan<- prometheus.Metric) {

	servers, err := a.servers()
	if err != nil {
		logrus.Error(err)
	} else {
		for _, s := range servers {
			ch <- prometheus.MustNewConstMetric(serversDesc, prometheus.GaugeValue, 1.0, s.Name, s.State, s.Platform, s.Host)
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(len(servers))
	for _, s := range servers {
		go func(s server) {
			taskStates, err := a.taskStates(s.Name)
			if err != nil {
				logrus.Error(err)
			} else {
				for _, t := range taskStates {
					for _, tsn := range taskStateNames {
						v := 0.0
						if t.State == tsn {
							v = 1.0
						}
						ch <- prometheus.MustNewConstMetric(taskStateDesc, prometheus.GaugeValue, v, s.Name, t.Name, tsn)
					}

					t.details(s.Name, a, ch)
				}
			}
			wg.Done()
		}(s)
	}
	wg.Wait()
}

func (a *AttunityCollector) servers() ([]server, error) {

	type serverList struct {
		Items []server `json:"serverList"`
	}

	var (
		sl = serverList{}
	)
	req, err := a.newAPIRequest("/servers")
	if err != nil {
		return nil, err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &sl); err != nil {
		return nil, err
	}

	return sl.Items, nil

}

func (a *AttunityCollector) taskStates(server string) ([]task, error) {
	type taskList struct {
		Items []task `json:"taskList"`
	}

	var (
		path = "/servers/" + server + "/tasks"
		tl   = taskList{}
	)

	req, err := a.newAPIRequest(path)
	if err != nil {
		return nil, err
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &tl); err != nil {
		return nil, err
	}

	return tl.Items, nil

}

func (t task) details(server string, a *AttunityCollector, ch chan<- prometheus.Metric) {
	type taskDetailsList struct {
		Item task `json:"task"`
	}

	var (
		path = "/servers/" + server + "/tasks/" + t.Name
		td   = taskDetailsList{}
	)

	req, err := a.newAPIRequest(path)
	if err != nil {
		logrus.Error(err)
		return
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		logrus.Error(err)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logrus.Error(err)
		return
	}

	if err := json.Unmarshal(body, &td); err != nil {
		logrus.Error(err, string(body))
		return
	}

	times := strings.Split(td.Item.CDCLatency.TotalLatency, ":")
	hoursInt, err := strconv.Atoi(times[0])
	if err != nil {
		logrus.Error(err)
		return
	}
	minsInt, err := strconv.Atoi(times[1])
	if err != nil {
		logrus.Error(err)
		return
	}
	secsInt, err := strconv.Atoi(times[2])
	if err != nil {
		logrus.Error(err)
		return
	}
	latency := (time.Duration(hoursInt) * time.Hour) + (time.Duration(minsInt) * time.Minute) + (time.Duration(secsInt) * time.Second)

	ch <- prometheus.MustNewConstMetric(taskTotalLatencyDesc, prometheus.GaugeValue, latency.Seconds(), server, t.Name)

}

func (a *AttunityCollector) newAPIRequest(path string) (req *http.Request, err error) {
	req, err = http.NewRequest("GET", a.APIURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Enterprisemanager.apisessionid", a.SessionID)

	return
}
