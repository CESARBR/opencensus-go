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
	"fmt"
	"log"
	"net/http"
	"sync"

	"go.opencensus.io/internal"
	"go.opencensus.io/stats/view"
	"strings"
	//"bufio"
	"github.com/marpaia/graphite-golang"
)

// Exporter exports stats to Graphite, users need
// to register the exporter as an http.Handler to be
// able to export.
type Exporter struct {
	opts    Options
	c       *collector
	handler http.Handler
}

// Options contains options for configuring the exporter.
type Options struct {
	Host string
	Port int
	OnError   func(err error)
}

// NewExporter returns an exporter that exports stats to Graphite.
func NewExporter(o Options) (*Exporter, error) {
	if o.Host == "" {
		// default Host
		o.Host = "127.0.0.1"
	}

	if o.Port == 0 {
		// default Port
		o.Port = 8125
	}

	collector := newCollector(o)
	e := &Exporter{
		opts: o,
		c:    collector,
	}

	return e, nil
}

var _ http.Handler = (*Exporter)(nil)
var _ view.Exporter = (*Exporter)(nil)

func (c *collector) registerViews(views ...*view.View) {
	count := 0
	for _, view := range views {
		sig := viewSignature(c.opts.Host, view)
		c.registeredViewsMu.Lock()
		_, ok := c.registeredViews[sig]
		c.registeredViewsMu.Unlock()
		if !ok {
			desc := viewName(c.opts.Host, view)

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
// Each OpenCensus AggregationData will be converted to
// corresponding Graphite Metric: SumData will be converted
// to Untyped Metric, CountData will be a Counter Metric,
// DistributionData will be a Histogram Metric.
func (e *Exporter) ExportView(vd *view.Data) {
	if len(vd.Rows) == 0 {
		return
	}
	e.c.addViewData(vd)

	buildRequest(vd, e)
}

type carbonData struct {
	path string
	value string
}

func buildRequest(vd *view.Data, e *Exporter) {
	for _, row := range vd.Rows {
		data := carbonData {
			vd.View.Measure.Name(),
			ExtractValue(row.Data),
		}
		SendDataToCarbon(data, e)
	}
}

func SendDataToCarbon(data carbonData, e *Exporter) {
	Graphite, err := graphite.NewGraphite(e.opts.Host, e.opts.Port)

	// if you couldn't connect to graphite, use a nop
	if err != nil {
		Graphite = graphite.NewGraphiteNop(e.opts.Host, e.opts.Port)
	}

	log.Printf("Loaded Graphite connection: %#v", Graphite)
	Graphite.SimpleSend(data.path, data.value)
}

func ExtractValue(data view.AggregationData ) string {
	str := fmt.Sprintf("%v", data)
	str = strings.Replace(str, "&", "", -1)
	str = strings.Replace(str, "{", "", -1)
	str = strings.Replace(str, "}", "", -1)
	return str
}


// ServeHTTP serves the Graphite endpoint.
func (e *Exporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.handler.ServeHTTP(w, r)
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

// Collect fetches the statistics from OpenCensus
// and delivers them as Graphite Metrics.
func (c *collector) Collect(ch chan<- *constMetric) {
	// We need a copy of all the view data up until this point.
	viewData := c.cloneViewData()

	for _, vd := range viewData {
		sig := viewSignature(c.opts.Host, vd.View)
		c.registeredViewsMu.Lock()
		desc := c.registeredViews[sig]
		c.registeredViewsMu.Unlock()

		for _, row := range vd.Rows {
			metric, err := c.toMetric(desc, vd.View, row)
			if err != nil {
				c.opts.onError(err)
			} else {
				ch <- metric
			}
		}
	}

}

type constMetric struct {
	desc string
	val  float64
}

func NewConstMetric(desc string, value float64) (*constMetric, error) {
	return &constMetric{
		desc: desc,
		val:  value,
	}, nil
}

func (c *collector) toMetric(desc string, v *view.View, row *view.Row) (*constMetric, error) {
	switch data := row.Data.(type) {
	case *view.CountData:
		return NewConstMetric(desc, float64(data.Value))

	default:
		return nil, fmt.Errorf("aggregation %T is not yet supported", v.Aggregation)
	}
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
