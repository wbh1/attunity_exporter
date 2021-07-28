package collector

import (
	"regexp"
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

	fullLoadTablesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "full_load_table_count"),
		"Full load tables completed for this task",
		[]string{"server", "task", "source", "target", "state"},
		nil,
	)
)

type task struct {
	Name             string           `json:"name"`
	State            string           `json:"state"`
	AssignedTags     []string         `json:"assigned_tags"`
	CDCLatency       cdcLatency       `json:"cdc_latency"`
	FullLoadCounters fullLoadCounters `json:"full_load_counters"`
	SourceEndpoint   endpoint         `json:"source_endpoint"`
	TargetEndpoint   endpoint         `json:"target_endpoint"`
}

type cdcLatency struct {
	SourceLatency string `json:"source_latency"`
	TotalLatency  string `json:"total_latency"`
}

type fullLoadCounters struct {
	TablesCompleted float64 `json:"tables_completed_count"`
	TablesLoading   float64 `json:"tables_loading_count"`
	TablesQueued    float64 `json:"tables_queued_count"`
	TablesErrored   float64 `json:"tables_with_error_count"`
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

	// Do filtering
	filtered := tl.Items
	if len(a.IncludedTags) > 0 {
		filterIncludedTags(&filtered, a.IncludedTags)
	}
	if len(*incl) > 0 {
		filterIncluded(&filtered, *incl)
	}
	if len(*excl) > 0 {
		filterExcluded(&filtered, *excl)
	}

	return filtered, nil

}

func (t task) details(server string, a *AttunityCollector, ch chan<- prometheus.Metric) {
	var (
		path = "/servers/" + server + "/tasks/" + t.Name
	)

	if err := a.APIRequest(path, &t); err != nil {
		logrus.Error(err)
		return
	}

	// Create TotalLatency metric
	tl, err := latency(t.CDCLatency.TotalLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskTotalLatencyDesc, prometheus.GaugeValue, tl.Seconds(), server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name)
	}

	// Create SourceLatency metric
	sl, err := latency(t.CDCLatency.SourceLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskSourceLatencyDesc, prometheus.GaugeValue, sl.Seconds(), server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name)
	}

	/*
		Create metrics for full load of tables...
	*/

	// Completed
	ch <- prometheus.MustNewConstMetric(fullLoadTablesDesc, prometheus.GaugeValue, t.FullLoadCounters.TablesCompleted, server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name, "completed")

	// Loading
	ch <- prometheus.MustNewConstMetric(fullLoadTablesDesc, prometheus.GaugeValue, t.FullLoadCounters.TablesLoading, server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name, "loading")

	// Queued
	ch <- prometheus.MustNewConstMetric(fullLoadTablesDesc, prometheus.GaugeValue, t.FullLoadCounters.TablesQueued, server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name, "queued")

	// Errored
	ch <- prometheus.MustNewConstMetric(fullLoadTablesDesc, prometheus.GaugeValue, t.FullLoadCounters.TablesErrored, server, t.Name, t.SourceEndpoint.Name, t.TargetEndpoint.Name, "error")

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

func filterExcluded(tl *[]task, excluded []string) {
	filtered := *tl
	lastIndex := (len(filtered) - 1)

	for _, regexString := range excluded {
		matched := false
		regex, err := regexp.Compile(regexString)
		if err != nil {
			logrus.Error(err)
			continue
		}

		// Do not increment index on each loop.
		// Handle incrementing inside of the loop.
		for index := 0; index <= lastIndex; {

			taskName := &filtered[index].Name

			if regex.Match([]byte(*taskName)) {
				logrus.WithFields(logrus.Fields{
					"task_name": *taskName,
					"regex":     regexString,
				}).Debug("Excluding task")

				// Hooray! The provided regex matched something and wasn't a waste of time.
				matched = true

				// Replace the value at the current index with the value at
				// the last index of the slice.
				filtered[index] = filtered[lastIndex]

				// Set the value of the last index of the slice
				// to the nil value of `task`. The value that was previously
				// there is now at filtered[index], so we did not lose it.
				// We will just NOT increment `index` so that the
				// new value will get checked, too.
				filtered[lastIndex] = task{}

				// Set the `filtered` slice to be everything up to
				// the last index, which we just set to a nil value.
				filtered = filtered[:lastIndex]

				// The last index will now be one less than before.
				// This is the same as if we just did
				// lastIndex = len(filtered)
				// everytime, except this should be slightly more performant.
				lastIndex--
			} else {
				// If no match was found, increment the index
				// so that we check the next value in the `filtered` slice
				index++
				logrus.WithFields(logrus.Fields{
					"task_name": *taskName,
					"regex":     regexString,
				}).Debug("Did not match regex")
			}
		}
		// Log an error if the regex didn't match any tasks.
		// This should warn users if they're providing a useless regex.
		if !matched {
			logrus.Warn("No task found to exclude matching: ", regexString)
		}
	}
	*tl = filtered
}

func filterIncluded(tl *[]task, included []string) {
	var filtered []task
	for _, regexString := range included {
		regex, err := regexp.Compile(regexString)
		if err != nil {
			logrus.Error(err)
			continue
		}
		matched := false
		for _, t2 := range *tl {
			if regex.Match([]byte(t2.Name)) {
				matched = true
				filtered = append(filtered, t2)
				break
			}
		}
		if !matched {
			logrus.Error("No task found to include matching: ", regexString)
		}
	}

	*tl = filtered

}

func filterIncludedTags(tl *[]task, tags []*string) {
	var filtered []task
	for _, task := range *tl {
		matched := false
		for _, taskTag := range task.AssignedTags {
			for _, tag := range tags {
				if *tag == taskTag {
					matched = true
					continue
				}
			}
		}

		if matched == true {
			filtered = append(filtered, task)
		}
	}

	*tl = filtered

}
