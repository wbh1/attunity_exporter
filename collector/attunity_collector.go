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

	taskSourceLatencyDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "source_latency_seconds"),
		"Source latency for this task",
		[]string{"server", "task"},
		nil,
	)
)

// Config is the destination type for when the config file gets
// unmarshalled by the YAML package
type Config struct {
	Server        string         `yaml:"server"`
	Username      string         `yaml:"user"`
	Password      string         `yaml:"password"`
	Timeout       int            `yaml:"timeout,omitempty"`
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

	// Enterprisemanager.apisessionid header to be included
	// in all API requests
	SessionID string

	// A list of tasks to get details on
	// If empty, get all tasks.
	// The key is server name.
	IncludedTasks []map[string][]string

	// A list of tasks to NOT get details on
	// Takes precedence over IncludedTasks
	// The key is server name.
	ExcludedTasks []map[string][]string

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

	incl := make([]map[string][]string, 1)
	for i := 0; i < len(cfg.IncludedTasks); i++ {
		for _, t := range cfg.IncludedTasks[i].Tasks {
			incl[i] = make(map[string][]string)
			incl[i][cfg.IncludedTasks[i].Server] = append(incl[i][cfg.IncludedTasks[i].Server], t)

		}
	}

	excl := make([]map[string][]string, 1)
	for i := 0; i < len(cfg.ExcludedTasks); i++ {
		for _, t := range cfg.ExcludedTasks[i].Tasks {
			excl[i] = make(map[string][]string)
			excl[i][cfg.ExcludedTasks[i].Server] = append(excl[i][cfg.ExcludedTasks[i].Server], t)
		}
	}

	return &AttunityCollector{
		APIURL:        apiURL,
		SessionID:     sessionID,
		IncludedTasks: incl,
		ExcludedTasks: excl,
		httpClient:    client,
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

	exclIndex := -1
	for x, y := range a.ExcludedTasks {
		if _, ok := y[server]; ok {
			exclIndex = x
			logrus.Debugf("Found index for %s %v", server, exclIndex)
			break
		}
	}

	inclIndex := -1
	for x, y := range a.IncludedTasks {
		if _, ok := y[server]; ok {
			inclIndex = x
			logrus.Debugf("Found index for %s %v", server, inclIndex)
			break
		}
	}

	if exclIndex >= 0 || inclIndex >= 0 {
		var filtered []task
		// Filter to EXCLUDE specific tasks
		if len(a.ExcludedTasks[exclIndex][server]) > 0 && len(a.IncludedTasks[inclIndex][server]) > 0 {
			filtered = a.filterIncluded(&tl.Items, a.IncludedTasks[inclIndex][server])
			filtered = a.filterExcluded(&filtered, a.ExcludedTasks[exclIndex][server])
		} else if len(a.ExcludedTasks[exclIndex][server]) > 0 {
			filtered = a.filterExcluded(&tl.Items, a.ExcludedTasks[exclIndex][server])
		} else if len(a.IncludedTasks[inclIndex][server]) > 0 {
			filtered = a.filterIncluded(&tl.Items, a.IncludedTasks[inclIndex][server])
		}

		return filtered, nil
	}

	return tl.Items, nil

}

func (a *AttunityCollector) filterExcluded(tl *[]task, excluded []string) []task {
	filtered := *tl
	length := len(*tl)

	for _, t1 := range excluded {
		matched := false
		for i, t2 := range *tl {
			if t1 == t2.Name {
				matched = true
				if i < length {
					filtered = append(filtered[:i], filtered[i+1:]...)
				} else {
					filtered = filtered[:i]
				}
				length--
			}
		}
		if !matched {
			logrus.Error("No task found matching: ", t1)
		}
	}
	return filtered[:length]
}

func (a *AttunityCollector) filterIncluded(tl *[]task, included []string) []task {
	var filtered []task
	for _, t1 := range included {
		matched := false
		for _, t2 := range *tl {
			if t1 == t2.Name {
				matched = true
				filtered = append(filtered, t2)
				break
			}
		}
		if !matched {
			logrus.Error("No task found matching: ", t1)
		}
	}
	return filtered
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

	tl, err := latency(td.Item.CDCLatency.TotalLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskTotalLatencyDesc, prometheus.GaugeValue, tl.Seconds(), server, t.Name)
	}

	sl, err := latency(td.Item.CDCLatency.SourceLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskSourceLatencyDesc, prometheus.GaugeValue, sl.Seconds(), server, t.Name)
	}

}

func latency(hhmmss string) (time.Duration, error) {
	times := strings.Split(hhmmss, ":")
	hoursInt, err := strconv.Atoi(times[0])
	if err != nil {
		logrus.Error(err)
		return time.Second, err
	}
	minsInt, err := strconv.Atoi(times[1])
	if err != nil {
		logrus.Error(err)
		return time.Second, err
	}
	secsInt, err := strconv.Atoi(times[2])
	if err != nil {
		logrus.Error(err)
		return time.Second, err
	}
	latency := (time.Duration(hoursInt) * time.Hour) + (time.Duration(minsInt) * time.Minute) + (time.Duration(secsInt) * time.Second)

	return latency, nil
}

func (a *AttunityCollector) newAPIRequest(path string) (req *http.Request, err error) {
	req, err = http.NewRequest("GET", a.APIURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Enterprisemanager.apisessionid", a.SessionID)

	return
}
