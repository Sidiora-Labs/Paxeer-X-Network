package engine

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// taskDuration records the wall-clock execution time of the task handler
	// (measured inside the goroutine, not submission latency).
	taskDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "seictl_task_execution_duration_seconds",
			Help:    "Wall-clock execution time of task handlers in seconds (measured inside the goroutine, not submission latency).",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"type", "status"},
	)

	// taskSubmissions counts the total number of task submissions.
	taskSubmissions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seictl_task_submissions_total",
			Help: "Total number of tasks submitted.",
		},
		[]string{"type"},
	)

	// taskFailures counts the total number of task failures.
	taskFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seictl_task_failures_total",
			Help: "Total number of tasks that failed.",
		},
		[]string{"type"},
	)

	// taskPanics counts handler panics recovered by the engine. A non-zero
	// value means a handler crashed and was converted to a failed task rather
	// than taking the sidecar down; it also increments taskFailures.
	taskPanics = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seictl_task_panics_total",
			Help: "Total number of task handler panics recovered by the engine.",
		},
		[]string{"type"},
	)
)

func init() {
	prometheus.MustRegister(taskDuration)
	prometheus.MustRegister(taskSubmissions)
	prometheus.MustRegister(taskFailures)
	prometheus.MustRegister(taskPanics)
}
