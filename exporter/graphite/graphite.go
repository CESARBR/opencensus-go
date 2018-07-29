// Copyright 2017, OpenCensus Authors
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

// Package graphite contains a Graphite exporter that supports exporting
// OpenCensus views as Graphite metrics.
package graphite // import "go.opencensus.io/exporter/graphite"

import (
	"bytes"
	"log"
	"strconv"
	"sync"
	"time"

	"errors"
	"fmt"
	"sort"
	"strings"

	"go.opencensus.io/exporter/graphite/client"
	"go.opencensus.io/internal"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// Create a document of how we are mapping and exporting views to graphite

// Exporter exports stats to Graphite
type Exporter struct {
	// Options used to register and log stats
	opts Options
	c    *collector
}

// Options contains options for configuring the exporter.
type Options struct {
	// Host contains de host address for the graphite server
	// The default value is "127.0.0.1"
	Host string

	// Port is the port in which the carbon endpoint is available
	// The default value is 2003
	Port int

	// Namespace is optional and will be the first element in the path
	Namespace string

	OnError func(err error)
}

// NewExporter returns an exporter that exports stats to Graphite.
func NewExporter(o Options) (*Exporter, error) {
	if o.Host == "" {
		// default Host
		o.Host = "127.0.0.1"
	}

	if o.Port == 0 {
		// default Port
		o.Port = 2003
	}

	collector := newCollector(o)
	e := &Exporter{
		opts:       o,
		c:          collector,
	}

	return e, nil
}

var _ view.Exporter = (*Exporter)(nil)

// registerViews creates the view map and prevents duplicated views
func (c *collector) registerViews(views ...*view.View) {
	count := 0
	for _, view := range views {
		sig := viewSignature(c.opts.Host, view)
		c.registeredViewsMu.Lock()
		_, ok := c.registeredViews[sig]
		c.registeredViewsMu.Unlock()
		if !ok {
			desc := internal.Sanitize(view.Name)
			c.registeredViewsMu.Lock()
			c.registeredViews[sig] = desc
			c.registeredViewsMu.Unlock()
			count++
		}
	}
	if count == 0 {
		return
	}
}

func (o *Options) onError(err error) {
	if o.OnError != nil {
		o.OnError(err)
	} else {
		log.Printf("Failed to export to Graphite: %v", err)
	}
}

// ExportView exports to the Graphite if view data has one or more rows.
// Each OpenCensus stats records will be converted to
// corresponding Graphite Metric
func (e *Exporter) ExportView(vd *view.Data) {
	if len(vd.Rows) == 0 {
		return
	}
	e.c.addViewData(vd)

	extractData(vd, e)
}

func (c *collector) formatMetric(desc string, v *view.View, row *view.Row, vd *view.Data, e *Exporter) {
	switch data := row.Data.(type) {
	case *view.CountData:
		names := []string{e.opts.Namespace, vd.View.Name, buildPath(tagValues(row.Tags)), vd.View.Measure.Name()}
		metric, _ := newConstMetric(buildPath(names), float64(data.Value))
		go sendRequest(e, metric)
	case *view.DistributionData:
		indicesMap := make(map[float64]int)
		buckets := make([]float64, 0, len(v.Aggregation.Buckets))
		for i, b := range v.Aggregation.Buckets {
			if _, ok := indicesMap[b]; !ok {
				indicesMap[b] = i
				buckets = append(buckets, b)
			}
		}
		sort.Float64s(buckets)

		for _, bucket := range buckets {
			names := []string{e.opts.Namespace, vd.View.Name, buildPath(tagValues(row.Tags)), "bucket", vd.View.Measure.Name()}
			metric, _ := newConstMetric(buildPath(names), float64(bucket))
			go sendRequest(e, metric)
		}

		names := []string{e.opts.Namespace, vd.View.Name, buildPath(tagValues(row.Tags)), "bucket", vd.View.Measure.Name(), "count"}
		metric, _ := newConstMetric(buildPath(names), float64(data.Count))
		go sendRequest(e, metric)

		names = []string{e.opts.Namespace, vd.View.Name, buildPath(tagValues(row.Tags)), "bucket", vd.View.Measure.Name(), "sum"}
		metric, _ = newConstMetric(buildPath(names), float64(data.Sum()))
		go sendRequest(e, metric)
	case *view.SumData:
		names := []string{e.opts.Namespace, vd.View.Name, buildPath(tagValues(row.Tags)), vd.View.Measure.Name()}
		metric, _ := newConstMetric(buildPath(names), float64(data.Value))
		go sendRequest(e, metric)
	case *view.LastValueData:
		names := []string{e.opts.Namespace, vd.View.Name, buildPath(tagValues(row.Tags)), vd.View.Measure.Name()}
		metric, _ := newConstMetric(buildPath(names), float64(data.Value))
		go sendRequest(e, metric)
	default:
		e.opts.OnError(errors.New(fmt.Sprintf("aggregation %T is not yet supported", data)))
	}
}

// extractData extracts stats data and adds to the dataBuffer
func extractData(vd *view.Data, e *Exporter) {
	sig := viewSignature(e.c.opts.Namespace, vd.View)
	e.c.registeredViewsMu.Lock()
	desc := e.c.registeredViews[sig]
	e.c.registeredViewsMu.Unlock()

	for _, row := range vd.Rows {
		e.c.formatMetric(desc, vd.View, row, vd, e)
	}
}

func buildPath(names []string) string {
	var values []string
	for _, name := range names {
		if name != "" {
			values = append(values, name)
		}
	}
	return strings.Join(values, ".")
}

func tagValues(t []tag.Tag) []string {
	var values []string
	for _, t := range t {
		values = append(values, fmt.Sprintf("%s_%s", t.Key.Name(), t.Value))
	}
	return values
}

type collector struct {
	opts Options
	mu   sync.Mutex // mu guards all the fields.

	// viewData are accumulated and atomically
	// appended to on every Export invocation, from
	// stats. These views are cleared out when
	// Collect is invoked and the cycle is repeated.
	viewData map[string]*view.Data

	registeredViewsMu sync.Mutex

	registeredViews map[string]string
}

func (c *collector) addViewData(vd *view.Data) {
	c.registerViews(vd.View)
	sig := viewSignature(c.opts.Host, vd.View)

	c.mu.Lock()
	c.viewData[sig] = vd
	c.mu.Unlock()
}

type constMetric struct {
	desc string
	val  float64
}

func newConstMetric(desc string, value float64) (constMetric, error) {
	return constMetric{
		desc: desc,
		val:  value,
	}, nil
}

func newCollector(opts Options) *collector {
	return &collector{
		opts:            opts,
		registeredViews: make(map[string]string),
		viewData:        make(map[string]*view.Data),
	}
}

func viewName(namespace string, v *view.View) string {
	var name string
	if namespace != "" {
		name = namespace + "_"
	}
	return name + internal.Sanitize(v.Name)
}

func viewSignature(namespace string, v *view.View) string {
	var buf bytes.Buffer
	buf.WriteString(viewName(namespace, v))
	for _, k := range v.TagKeys {
		buf.WriteString("-" + k.Name())
	}
	return buf.String()
}

// sendRequest sends a package of data containing one metric
func sendRequest(e *Exporter, data constMetric) {
	Graphite, err := client.NewGraphite(e.opts.Host, e.opts.Port)

	if err != nil {
		e.opts.OnError(errors.New(fmt.Sprintf("Error creating graphite: %#v", err)))
	} else {
		Graphite.SendMetric(data.desc, strconv.FormatFloat(data.val, 'f', -1, 64), time.Now())
	}
}

func (c *collector) cloneViewData() map[string]*view.Data {
	c.mu.Lock()
	defer c.mu.Unlock()

	viewDataCopy := make(map[string]*view.Data)
	for sig, viewData := range c.viewData {
		viewDataCopy[sig] = viewData
	}
	return viewDataCopy
}
