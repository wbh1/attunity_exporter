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

	diskUsageDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "disk_usage_mb"),
		"The amount of disk space that the server is currently consuming, in MB. This is the sum of disk usage for all tasks on this server",
		[]string{"server"},
		nil,
	)

	memoryUsageDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "memory_usage_mb"),
		"The amount of memory that the server is currently consuming, in MB. This is the sum of memory usage for all active tasks on this server, excluding stopped tasks",
		[]string{"server"},
		nil,
	)

	attunityCPUPercentageDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "attunity_cpu_percentage"),
		"The current CPU usage of the Replicate server process + all task processes",
		[]string{"server"},
		nil,
	)

	machineCPUPercentageDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "machine_cpu_percentage"),
		"The current total CPU usage of all the processes running on the machine",
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
	Name                string              `json:"name"`
	License             license             `json:"license"`
	TaskSummary         taskSummary         `json:"task_summary"`
	ResourceUtilization resourceUtilization `json:"resource_utilization"`
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

type resourceUtilization struct {
	DiskUsage   int `json:"disk_usage_mb"`
	MemoryUsage int `json:"memory_mb"`
	AttunityCPU int `json:"attunity_cpu_percentage"`
	MachineCPU  int `json:"machine_cpu_percentage"`
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
