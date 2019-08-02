package collector

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	serverDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "", "server"),
		"Servers currently found in Attunity",
		[]string{"server", "state", "platform", "host"},
		nil,
	)

	serverTasksDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "tasks"),
		"Tasks running on this server by state",
		[]string{"server", "state"},
		nil,
	)

	serverLicenseExpirationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "days_to_license_expiration"),
		"Days until the license expires on a specified server",
		[]string{"server"},
		nil,
	)
)

type server struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Platform string `json:"platform"`
	State    string `json:"state"`
}

type serverDetails struct {
	Name        string      `json:"name"`
	License     license     `json:"license"`
	TaskSummary taskSummary `json:"task_summary"`
}

type license struct {
	DaysToExpiration int `json:"days_to_expiration"`
}

type taskSummary struct {
	Running    int `json:"running"`
	Stopped    int `json:"stopped"`
	Recovering int `json:"recovering"`
	Error      int `json:"error"`
}

func (a *AttunityCollector) servers() ([]server, error) {

	type serverList struct {
		Items []server `json:"serverList"`
	}

	var (
		sl = serverList{}
	)
	if err := a.APIRequest("/servers", &sl); err != nil {
		return nil, err
	}

	return sl.Items, nil

}

func (a *AttunityCollector) serverDetails(server string) (sd serverDetails, err error) {
	type response struct {
		Items serverDetails `json:"server_details"`
	}
	var (
		path = "/servers/" + server
		r    = response{}
	)

	if err = a.APIRequest(path, &r); err != nil {
		return
	}

	sd = r.Items

	return
}
