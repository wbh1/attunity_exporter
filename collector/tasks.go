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
	// misc.
	taskStateDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "server", "task_state"),
		"Number of tasks broken down by state",
		[]string{"server", "task", "state"},
		nil,
	)

	cpuPercentageDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "cpu_percentage"),
		"The time gap between the original change in the source endpoint and capturing it, in hh:mm:ss",
		[]string{"server", "task"},
		nil,
	)

	memoryMBDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "memory_mb"),
		"The current utilization of memory, in MB. A task's memory utilization is sampled every 10 seconds. When the task is not running, the value is set to zero (0).",
		[]string{"server", "task"},
		nil,
	)

	discUsageMBDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "disk_usage_mb"),
		"The current utilization of disk space, in MB. A task's disk utilization is sampled every minute.",
		[]string{"server", "task"},
		nil,
	)

	dataErrorCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "data_error_count"),
		"The total number of data errors in all tables involved in the task. The count is affected by data errors and the Reset Data Errors option available when you drill down to a task.",
		[]string{"server", "task"},
		nil,
	)

	// cdc_event_counters
	appliedInsertCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_insert_count"),
		"The number of records added in total for all tables",
		[]string{"server", "task"},
		nil,
	)

	appliedUpdateCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_update_count"),
		"The number of records updated in total for all tables",
		[]string{"server", "task"},
		nil,
	)

	appliedDeleteCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_delete_count"),
		"The number of records deleted in total for all tables",
		[]string{"server", "task"},
		nil,
	)

	appliedDDLCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_ddl_count"),
		"The total number of metadata changes, such as add column",
		[]string{"server", "task"},
		nil,
	)

	// full_load_counters
	tablesCompletedCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "tables_completed_count"),
		"The number of tables that have been loaded into the target endpoint",
		[]string{"server", "task"},
		nil,
	)

	tablesLoadingCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "tables_loading_count"),
		"The number of tables that are currently being loaded into the target endpoint",
		[]string{"server", "task"},
		nil,
	)

	tablesQueuedCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "tables_queued_count"),
		"The number of tables that are waiting to be loaded due to an error",
		[]string{"server", "task"},
		nil,
	)

	tablesWithErrorCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "tables_with_error_count"),
		"The number of tables that could not be loaded due to an error",
		[]string{"server", "task"},
		nil,
	)

	recordsCompletedCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "records_completed_count"),
		"The total number of records that have completed loading into the target endpoint",
		[]string{"server", "task", "source", "target"},
		nil,
	)

	estimatedRecordsForAllTablesCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "estimated_records_for_all_tables_count"),
		"The estimated number of records remaining to be loaded into the target endpoint",
		[]string{"server", "task", "source", "target"},
		nil,
	)

	// full_load_throughput
	sourceThroughputRecordsCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "source_throughput_records_count"),
		"The current source throughput, in rec/sec",
		[]string{"server", "task", "source"},
		nil,
	)

	sourceThroughputVolumeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "source_throughput_volume"),
		"The current source throughput, in kbyte/sec",
		[]string{"server", "task", "source"},
		nil,
	)

	targetThroughputRecordsCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "target_throughput_records_count"),
		"The current target throughput, in rec/sec",
		[]string{"server", "task", "target"},
		nil,
	)

	targetThroughputVolumeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "target_throughput_volume"),
		"The current target throughput, in kbyte/sec",
		[]string{"server", "task", "target"},
		nil,
	)

	// cdc_throughput
	cdcSourceThroughputRecordsCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "cdc_source_throughput_records_count"),
		"The current source throughput, in rec/sec",
		[]string{"server", "task", "source"},
		nil,
	)

	cdcSourceThroughputVolumeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "cdc_source_throughput_volume"),
		"The current source throughput, in kbyte/sec",
		[]string{"server", "task", "source"},
		nil,
	)

	cdcTargetThroughputRecordsCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "cdc_target_throughput_records_count"),
		"The current target throughput, in rec/sec",
		[]string{"server", "task", "target"},
		nil,
	)

	cdcTargetThroughputVolumeDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "cdc_target_throughput_volume"),
		"The current target throughput, in kbyte/sec",
		[]string{"server", "task", "target"},
		nil,
	)

	// cdc_transactions_counters
	commitChangeRecordsCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "commit_change_records_count"),
		"The number of COMMIT change records",
		[]string{"server", "task"},
		nil,
	)

	rollbackTransactionCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "rollback_transaction_count"),
		"The number of ROLLBACK transactions",
		[]string{"server", "task"},
		nil,
	)

	rollbackChangeRecordsCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "rollback_change_records_count"),
		"The number of ROLLBACK change records",
		[]string{"server", "task"},
		nil,
	)

	rollbackChangeVolumeMBDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "rollback_change_volume_mb"),
		"The volume of ROLLBACK change, in MB",
		[]string{"server", "task"},
		nil,
	)

	appliedTransactionsInProgressCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_transactions_in_progress_count"),
		"The number of transactions in progress",
		[]string{"server", "task"},
		nil,
	)

	appliedRecordsInProgressCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_records_in_progress_count"),
		"The sum of all records/events in all In-Progress transactions",
		[]string{"server", "task"},
		nil,
	)

	appliedCommittedTransactionCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_comitted_transaction_count"),
		"The number of transactions committed",
		[]string{"server", "task"},
		nil,
	)

	appliedRecordsCommittedCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_records_comitted_count"),
		"The sum of all records/events in all Completed transactions",
		[]string{"server", "task"},
		nil,
	)

	appliedVolumeCommittedMBDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "applied_volume_comitted_mb"),
		"The sum of all volume/events in all Completed transactions, in MB",
		[]string{"server", "task"},
		nil,
	)

	incomingAccumulatedChangesInMemoryCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "incoming_accumulated_changes_in_memory_count"),
		"The number of changes accumulated in memory until source commit",
		[]string{"server", "task"},
		nil,
	)

	incomingAccumulatedChangesOnDiskCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "incoming_accumulated_changes_on_disk_count"),
		"The number of changes accumulated on disk until source commit",
		[]string{"server", "task"},
		nil,
	)

	incomingApplyingChangesInMemoryCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "incoming_applying_changes_in_memory_count"),
		"The number of changes in memory during apply and until target commit",
		[]string{"server", "task"},
		nil,
	)

	incomingApplyingChangesOnDiskCountDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "incoming_applying_changes_on_disk_count"),
		"The number of changes on disk during apply and until target commit",
		[]string{"server", "task"},
		nil,
	)

	// cdc_latency
	taskTotalLatencyDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "total_latency_seconds"),
		"The overall latency (source latency + target latency + apply latency), in hh:mm:ss",
		[]string{"server", "task", "source", "target"},
		nil,
	)

	taskSourceLatencyDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "task", "source_latency_seconds"),
		"The time gap between the original change in the source endpoint and capturing it, in hh:mm:ss",
		[]string{"server", "task", "source", "target"},
		nil,
	)
)

type task struct {
	Name                    string                  `json:"name"`
	State                   string                  `json:"state"`
	CDCEventCounters        cdcEventCounters        `json:"cdc_event_counters"`
	FullLoadCounters        fullLoadCounters        `json:"full_load_counters"`
	FullLoadThroughput      fullLoadThroughput      `json:"full_load_throughput"`
	CDCThroughput           cdcThroughput           `json:"cdc_throughput"`
	CDCTransactionsCounters cdcTransactionsCounters `json:"cdc_transactions_counters"`
	CDCLatency              cdcLatency              `json:"cdc_latency"`
	SourceEndpoint          endpoint                `json:"source_endpoint"`
	TargetEndpoint          endpoint                `json:"target_endpoint"`
	MemoryMB                int                     `json:"memory_mb"`
	DiskUsageMB             int                     `json:"disk_usage_mb"`
	DataErrorCount          int                     `json:"data_error_count"`
	CPUPercentage           int                     `json:"cpu_percentage"`
}

type cdcEventCounters struct {
	AppliedInsertCount int `json:"applied_insert_count"`
	AppliedUpdateCount int `json:"applied_update_count"`
	AppliedDeleteCount int `json:"applied_delete_count"`
	AppliedDDLCount    int `json:"applied_ddl_count"`
}

type fullLoadCounters struct {
	TablesCompletedCount              int `json:"tables_completed_count"`
	TablesLoadingCount                int `json:"tables_loading_count"`
	TablesQueuedCount                 int `json:"tables_queued_count"`
	TablesWithErrorCount              int `json:"tables_with_error_count"`
	RecordsCompletedCount             int `json:"records_completed_count"`
	EstimatedRecordsForAllTablesCount int `json:"estimated_records_for_all_tables_count"`
}

type fullLoadThroughput struct {
	SourceThroughputRecordsCount int `json:"source_throughput_records_count"`
	SourceThroughputVolume       int `json:"source_throughput_volume"`
	TargetThroughputRecordsCount int `json:"target_throughput_records_count"`
	TargetThroughputVolume       int `json:"target_throughput_volume"`
}

type cdcThroughput struct {
	CDCSourceThroughputRecordsCount currentThroughput `json:"source_throughput_records_count"`
	CDCSourceThroughputVolume       currentThroughput `json:"source_throughput_volume"`
	CDCTargetThroughputRecordsCount currentThroughput `json:"target_throughput_records_count"`
	CDCTargetThroughputVolume       currentThroughput `json:"target_throughput_volume"`
}

type currentThroughput struct {
	Throughput int `json:"current"`
}

type cdcTransactionsCounters struct {
	CommitChangeRecordsCount                int `json:"commit_change_records_count"`
	RollbackTransactionCount                int `json:"rollback_transaction_count"`
	RollbackChangeRecordsCount              int `json:"rollback_change_records_count"`
	RollbackChangeVolumeMB                  int `json:"rollback_change_volume_mb"`
	AppliedTransactionsInProgressCount      int `json:"applied_transactions_in_progress_count"`
	AppliedRecordsInProgressCount           int `json:"applied_records_in_progress_count"`
	AppliedCommittedTransactionCount        int `json:"applied_comitted_transaction_count"`
	AppliedRecordsCommittedCount            int `json:"applied_records_comitted_count"`
	AppliedVolumeCommittedMB                int `json:"applied_volume_comitted_mb"`
	IncomingAccumulatedChangesInMemoryCount int `json:"incoming_accumulated_changes_in_memory_count"`
	IncomingAccumulatedChangesOnDiskCount   int `json:"incoming_accumulated_changes_on_disk_count"`
	IncomingApplyingChangesInMemoryCount    int `json:"incoming_applying_changes_in_memory_count"`
	IncomingApplyingChangesonDiskCount      int `json:"incoming_applying_changes_on_disk_count"`
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
	var (
		path = "/servers/" + server + "/tasks/" + t.Name
		td   = task{}
	)

	if err := a.APIRequest(path, &td); err != nil {
		logrus.Error(err)
		return
	}

	// Create TotalLatency metric
	tl, err := latency(td.CDCLatency.TotalLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskTotalLatencyDesc, prometheus.GaugeValue, tl.Seconds(), server, t.Name, td.SourceEndpoint.Name, td.TargetEndpoint.Name)
	}

	// Create SourceLatency metric
	sl, err := latency(td.CDCLatency.SourceLatency)
	if err != nil {
		logrus.Error(err)
	} else {
		ch <- prometheus.MustNewConstMetric(taskSourceLatencyDesc, prometheus.GaugeValue, sl.Seconds(), server, t.Name, td.SourceEndpoint.Name, td.TargetEndpoint.Name)
	}

	// misc.
	ch <- prometheus.MustNewConstMetric(cpuPercentageDesc, prometheus.GaugeValue, float64(td.CPUPercentage), server, td.Name)
	ch <- prometheus.MustNewConstMetric(memoryMBDesc, prometheus.GaugeValue, float64(td.MemoryMB), server, td.Name)
	ch <- prometheus.MustNewConstMetric(discUsageMBDesc, prometheus.GaugeValue, float64(td.DiskUsageMB), server, td.Name)
	ch <- prometheus.MustNewConstMetric(dataErrorCountDesc, prometheus.GaugeValue, float64(td.DataErrorCount), server, td.Name)

	// cdc_event_counters
	ch <- prometheus.MustNewConstMetric(appliedInsertCountDesc, prometheus.GaugeValue, float64(td.CDCEventCounters.AppliedInsertCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedUpdateCountDesc, prometheus.GaugeValue, float64(td.CDCEventCounters.AppliedUpdateCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedDeleteCountDesc, prometheus.GaugeValue, float64(td.CDCEventCounters.AppliedDeleteCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedDDLCountDesc, prometheus.GaugeValue, float64(td.CDCEventCounters.AppliedDDLCount), server, td.Name)

	// full_load_counters
	ch <- prometheus.MustNewConstMetric(tablesCompletedCountDesc, prometheus.GaugeValue, float64(td.FullLoadCounters.TablesCompletedCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(tablesLoadingCountDesc, prometheus.GaugeValue, float64(td.FullLoadCounters.TablesLoadingCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(tablesQueuedCountDesc, prometheus.GaugeValue, float64(td.FullLoadCounters.TablesQueuedCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(tablesWithErrorCountDesc, prometheus.GaugeValue, float64(td.FullLoadCounters.TablesWithErrorCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(recordsCompletedCountDesc, prometheus.GaugeValue, float64(td.FullLoadCounters.RecordsCompletedCount), server, td.Name, td.SourceEndpoint.Name, td.TargetEndpoint.Name)
	ch <- prometheus.MustNewConstMetric(estimatedRecordsForAllTablesCountDesc, prometheus.GaugeValue, float64(td.FullLoadCounters.EstimatedRecordsForAllTablesCount), server, td.Name, td.SourceEndpoint.Name, td.TargetEndpoint.Name)

	// full_load_throughput
	ch <- prometheus.MustNewConstMetric(sourceThroughputRecordsCountDesc, prometheus.GaugeValue, float64(td.FullLoadThroughput.SourceThroughputRecordsCount), server, td.Name, td.SourceEndpoint.Name)
	ch <- prometheus.MustNewConstMetric(sourceThroughputVolumeDesc, prometheus.GaugeValue, float64(td.FullLoadThroughput.SourceThroughputVolume), server, td.Name, td.SourceEndpoint.Name)
	ch <- prometheus.MustNewConstMetric(targetThroughputRecordsCountDesc, prometheus.GaugeValue, float64(td.FullLoadThroughput.TargetThroughputRecordsCount), server, td.Name, td.TargetEndpoint.Name)
	ch <- prometheus.MustNewConstMetric(targetThroughputVolumeDesc, prometheus.GaugeValue, float64(td.FullLoadThroughput.TargetThroughputVolume), server, td.Name, td.TargetEndpoint.Name)

	// cdc_throughput
	ch <- prometheus.MustNewConstMetric(cdcSourceThroughputRecordsCountDesc, prometheus.GaugeValue, float64(td.CDCThroughput.CDCSourceThroughputRecordsCount.Throughput), server, td.Name, td.SourceEndpoint.Name)
	ch <- prometheus.MustNewConstMetric(cdcSourceThroughputVolumeDesc, prometheus.GaugeValue, float64(td.CDCThroughput.CDCSourceThroughputVolume.Throughput), server, td.Name, td.SourceEndpoint.Name)
	ch <- prometheus.MustNewConstMetric(cdcTargetThroughputRecordsCountDesc, prometheus.GaugeValue, float64(td.CDCThroughput.CDCTargetThroughputRecordsCount.Throughput), server, td.Name, td.TargetEndpoint.Name)
	ch <- prometheus.MustNewConstMetric(cdcTargetThroughputVolumeDesc, prometheus.GaugeValue, float64(td.CDCThroughput.CDCTargetThroughputVolume.Throughput), server, td.Name, td.TargetEndpoint.Name)

	// cdc_transactions_counters
	ch <- prometheus.MustNewConstMetric(commitChangeRecordsCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.CommitChangeRecordsCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(rollbackTransactionCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.RollbackTransactionCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(rollbackChangeRecordsCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.RollbackChangeRecordsCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(rollbackChangeVolumeMBDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.RollbackChangeVolumeMB), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedTransactionsInProgressCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.AppliedTransactionsInProgressCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedRecordsInProgressCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.AppliedRecordsInProgressCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedCommittedTransactionCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.AppliedCommittedTransactionCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedRecordsCommittedCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.AppliedRecordsCommittedCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(appliedVolumeCommittedMBDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.AppliedVolumeCommittedMB), server, td.Name)
	ch <- prometheus.MustNewConstMetric(incomingAccumulatedChangesInMemoryCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.IncomingAccumulatedChangesInMemoryCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(incomingAccumulatedChangesOnDiskCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.IncomingAccumulatedChangesOnDiskCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(incomingApplyingChangesInMemoryCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.IncomingApplyingChangesInMemoryCount), server, td.Name)
	ch <- prometheus.MustNewConstMetric(incomingApplyingChangesOnDiskCountDesc, prometheus.GaugeValue, float64(td.CDCTransactionsCounters.IncomingApplyingChangesonDiskCount), server, td.Name)
}

func latency(hhmmss string) (time.Duration, error) {
	times := strings.Split(hhmmss, ":")
	hoursInt, err := strconv.Atoi(times[0])
	if err != nil {
		logrus.Error("hours")
		logrus.Error(hhmmss)
		logrus.Error(times[0])
		logrus.Error(err)
		return time.Second, err
	}
	minsInt, err := strconv.Atoi(times[1])
	if err != nil {
		logrus.Error("minutes")
		logrus.Error(err)
		return time.Second, err
	}
	secsInt, err := strconv.Atoi(times[2])
	if err != nil {
		logrus.Error("seconds")
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
