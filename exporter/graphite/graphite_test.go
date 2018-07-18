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

package graphite

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

func newView(measureName string, agg *view.Aggregation) *view.View {
	m := stats.Int64(measureName, "bytes", stats.UnitBytes)
	return &view.View{
		Name:        "foo",
		Description: "bar",
		Measure:     m,
		Aggregation: agg,
	}
}

func TestOnlyCumulativeWindowSupported(t *testing.T) {
	count1 := &view.CountData{Value: 1}
	lastValue1 := &view.LastValueData{Value: 56.7}
	tests := []struct {
		vds  *view.Data
		want int
	}{
		0: {
			vds: &view.Data{
				View: newView("TestOnlyCumulativeWindowSupported/m1", view.Count()),
			},
			want: 0, // no rows present
		},
		1: {
			vds: &view.Data{
				View: newView("TestOnlyCumulativeWindowSupported/m2", view.Count()),
				Rows: []*view.Row{
					{Data: count1},
				},
			},
			want: 1,
		},
		2: {
			vds: &view.Data{
				View: newView("TestOnlyCumulativeWindowSupported/m3", view.LastValue()),
				Rows: []*view.Row{
					{Data: lastValue1},
				},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		collector := newCollector(Options{})
		collector.addViewData(tt.vds)
	}
}

func TestCollectNonRacy(t *testing.T) {
	// Despite enforcing the singleton, for this case we
	// need an exporter hence won't be using NewExporter.
	exp, err := NewExporter(Options{})
	if err != nil {
		t.Fatalf("NewExporter: %v", err)
	}

	// Synchronize and make sure every goroutine has terminated before we exit
	var waiter sync.WaitGroup
	waiter.Add(3)
	defer waiter.Wait()

	doneCh := make(chan bool)
	// 1. Viewdata write routine at 700ns
	go func() {
		defer waiter.Done()
		tick := time.NewTicker(700 * time.Nanosecond)
		defer tick.Stop()

		defer func() {
			close(doneCh)
		}()

		for i := 0; i < 1e3; i++ {
			count1 := &view.CountData{Value: 1}
			vds := []*view.Data{
				{View: newView(fmt.Sprintf("TestCollectNonRacy/m2-%d", i), view.Count()), Rows: []*view.Row{{Data: count1}}},
			}
			for _, v := range vds {
				exp.ExportView(v)
			}
			<-tick.C
		}
	}()

	inMetricsChan := make(chan constMetric, 1000)
	// 2. Simulating the Prometheus metrics consumption routine running at 900ns
	go func() {
		defer waiter.Done()
		tick := time.NewTicker(900 * time.Nanosecond)
		defer tick.Stop()

		for {
			select {
			case <-doneCh:
				return
			case <-inMetricsChan:
			}
		}
	}()
}

type mSlice []*stats.Int64Measure

func (measures *mSlice) createAndAppend(name, desc, unit string) {
	m := stats.Int64(name, desc, unit)
	*measures = append(*measures, m)
}

type vCreator []*view.View

func (vc *vCreator) createAndAppend(name, description string, keys []tag.Key, measure stats.Measure, agg *view.Aggregation) {
	v := &view.View{
		Name:        name,
		Description: description,
		TagKeys:     keys,
		Measure:     measure,
		Aggregation: agg,
	}
	*vc = append(*vc, v)
}

func TestMetricsEndpointOutput(t *testing.T) {
	exporter, err := NewExporter(Options{})
	if err != nil {
		t.Fatalf("failed to create graphite exporter: %v", err)
	}
	view.RegisterExporter(exporter)

	names := []string{"foo", "bar", "baz"}

	var measures mSlice
	for _, name := range names {
		measures.createAndAppend("tests/"+name, name, "")
	}

	var vc vCreator
	for _, m := range measures {
		vc.createAndAppend(m.Name(), m.Description(), nil, m, view.Count())
	}

	if err := view.Register(vc...); err != nil {
		t.Fatalf("failed to create views: %v", err)
	}
	defer view.Unregister(vc...)

	view.SetReportingPeriod(time.Millisecond)

	for _, m := range measures {
		stats.Record(context.Background(), m.M(1))
	}

	srv := httptest.NewServer(exporter)
	defer srv.Close()

	var i int
	var output string
	for {
		time.Sleep(10 * time.Millisecond)
		if i == 1000 {
			t.Fatal("no output at /metrics (10s wait)")
		}
		i++

		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatalf("failed to get /metrics: %v", err)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		resp.Body.Close()

		output = string(body)
		if output != "" {
			break
		}
	}

	if strings.Contains(output, "collected before with the same name and label values") {
		t.Fatal("metric name and labels being duplicated but must be unique")
	}

	if strings.Contains(output, "error(s) occurred") {
		t.Fatal("error reported by graphite registry")
	}

	for _, name := range names {
		if !strings.Contains(output, "tests_"+name+" 1") {
			t.Fatalf("measurement missing in output: %v", name)
		}
	}
}
