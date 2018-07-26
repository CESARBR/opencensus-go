package client

import (
	"fmt"
	"time"
)

type Metric struct {
	Name      string
	Value     string
	Timestamp time.Time
}

func (metric Metric) String() string {
	return fmt.Sprintf(
		"%s %s %s",
		metric.Name,
		metric.Value,
		metric.Timestamp.Format("2006-01-02 15:04:05"),
	)
}
