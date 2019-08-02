package collector

import (
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	taskStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "task_state"),
		"Number of tasks broken down by state",
		[]string{"server", "task", "state"},
		nil,
	)

	taskTotalLatencyDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "total_latency_seconds"),
		"Total latency for this task",
		[]string{"server", "task", "source", "target"},
		nil,
	)

	taskSourceLatencyDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "source_latency_seconds"),
		"Source latency for this task",
		[]string{"server", "task", "source", "target"},
		nil,
	)
)

type task struct {
	Name           string     `json:"name"`
	State          string     `json:"state"`
	CDCLatency     cdcLatency `json:"cdc_latency"`
	SourceEndpoint endpoint   `json:"source_endpoint"`
	TargetEndpoint endpoint   `json:"target_endpoint"`
}

type cdcLatency struct {
	SourceLatency string `json:"source_latency"`
	TotalLatency  string `json:"total_latency"`
}

type endpoint struct {
	Name string `json:"name"`
}

func (a *AttunityCollector) taskStates(server string) ([]task, error) {
	type taskList struct {
		Items []task `json:"taskList"`
	}

	var (
		path             = "/servers/" + server + "/tasks"
		tl               = taskList{}
		explicitIncludes bool
		excl             = &[]string{}
		incl             = &[]string{}
	)

	if err := a.APIRequest(path, &tl); err != nil {
		return nil, err
	}

	for _, s := range a.ExcludedTasks {
		if s.Server == server {
			excl = &s.Tasks
		}
	}
	for _, s := range a.IncludedTasks {
		if s.Server == server {
			incl = &s.Tasks
		}
	}
	// if there are any excplicitly included tasks,
	// set this variable to true. It will then be used
	// to determine whether to skip or include a server's tasks
	if len(a.IncludedTasks) > 0 {
		explicitIncludes = true
	}

	// If tasks are explicitly specified for inclusion,
	// but none of those tasks are for the server that is an
	// argument to this calling function, we can just return with nothing.
	if explicitIncludes && len(*incl) == 0 {
		return nil, nil
	}

	if len(*excl) > 0 || len(*incl) > 0 {
		var filtered []task

		if len(*excl) > 0 && len(*incl) > 0 {
			filtered = filterIncluded(&tl.Items, *incl)
			// Must filter excluded after you filter the included
			// using the slice returned by filterIncluded
			filtered = filterExcluded(&filtered, *excl)

		} else if len(*excl) > 0 {
			filtered = filterExcluded(&tl.Items, *excl)

		} else if len(*incl) > 0 {
			filtered = filterIncluded(&tl.Items, *incl)

		}

		return filtered, nil
	}

	// If no includes/excludes are specified, return the whole task list
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

	if err := a.APIRequest(path, &td); err != nil {
		logrus.Error(err)
		return
	}

	// Create TotalLatency metric
	tl, err := latency(td.Item.CDCLatency.TotalLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskTotalLatencyDesc, prometheus.GaugeValue, tl.Seconds(), server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name)
	}

	// Create SourceLatency metric
	sl, err := latency(td.Item.CDCLatency.SourceLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskSourceLatencyDesc, prometheus.GaugeValue, sl.Seconds(), server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name)
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

func filterExcluded(tl *[]task, excluded []string) []task {
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
			logrus.Error("No task found to exclude matching: ", t1)
		}
	}
	return filtered[:length]
}

func filterIncluded(tl *[]task, included []string) []task {
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
			logrus.Error("No task found to include matching: ", t1)
		}
	}
	return filtered
}
