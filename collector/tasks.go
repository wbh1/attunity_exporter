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
		ch <- prometheus.MustNewConstMetric(taskTotalLatencyDesc, prometheus.GaugeValue, tl.Seconds(), server, t.Name, td.Item.SourceEndpoint.Name, td.Item.TargetEndpoint.Name)
	}

	// Create SourceLatency metric
	sl, err := latency(td.Item.CDCLatency.SourceLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskSourceLatencyDesc, prometheus.GaugeValue, sl.Seconds(), server, t.Name, td.Item.SourceEndpoint.Name, td.Item.TargetEndpoint.Name)
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
			logrus.Error("No task found to exclude matching: ", regexString)
		}
	}
	return filtered
}

func filterIncluded(tl *[]task, included []string) []task {
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
	return filtered
}
