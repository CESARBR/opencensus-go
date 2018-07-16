// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.


package graphite // import "go.opencensus.io/exporter/graphite"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	traceapi "cloud.google.com/go/trace/apiv2"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	monitoredrespb "google.golang.org/genproto/googleapis/api/monitoredres"
)

// Options contains options for configuring the exporter.
type Options struct {
	// ProjectNamespace is the identifier of the Graphite metric group
	ProjectNamespace string

	// GraphiteEndpoint is the URL for the Carbon/Graphite server
	// for example, the default is 127.0.0.1:8125
	GraphiteEndpoint string
}

// Exporter is a stats.Exporter
// implementation that uploads data to Graphite.
type Exporter struct {
	statsExporter *statsExporter
}

// NewExporter creates a new Exporter that implements both stats.Exporter
func NewExporter(o Options) (*Exporter, error) {
	if o.ProjectNamespace == "" {
		return nil, errors.New("empty ProjectNamespace")
	}
	se, err := newStatsExporter(o)
	if err != nil {
		return nil, err
	}
	return &Exporter{
		statsExporter: se,
	}, nil
}

// ExportView exports to the Graphite Monitoring if view data
// has one or more rows.
func (e *Exporter) ExportView(vd *view.Data) {
	e.statsExporter.ExportView(vd)
}

// Flush waits for exported data to be uploaded.
//
// This is useful if your program is ending and you do not
// want to lose recent stats or spans.
func (e *Exporter) Flush() {
	e.statsExporter.Flush()
}

func (o Options) handleError(err error) {
	log.Printf("Error exporting to Stackdriver: %v", err)
}