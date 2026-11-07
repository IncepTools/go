// Package dto holds the reports module's response shapes.
package dto

import "time"

// RunStatus is one job's last outcome.
type RunStatus struct {
	Job      string     `json:"job"`
	Runs     int        `json:"runs"`
	LastRun  *time.Time `json:"last_run,omitempty"`
	LastErr  string     `json:"last_error,omitempty"`
	Duration string     `json:"last_duration,omitempty"`
}
