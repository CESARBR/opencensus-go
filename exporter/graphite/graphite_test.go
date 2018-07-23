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
	"testing"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"time"
	"context"
	"net"
	"fmt"
	"os"
	"strconv"
	"log"
	"bufio"
	"strings"
)

var closeConn = false
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

func startServer(e *Exporter) {
	l, err := net.Listen("tcp", e.opts.Host+":"+strconv.Itoa(e.opts.Port))
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		return
	}

	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on " + e.opts.Host + ":" + strconv.Itoa(e.opts.Port))
	for {
		if closeConn {
			l.Close()
			return
		}
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn)
	}
}

// Handles incoming requests.
func handleRequest(conn net.Conn) {
	if closeConn {
		conn.Close()
		return
	}
	// Make a buffer to hold incoming data.
	buf := make([]byte, 1024)
	r   := bufio.NewReader(conn)

	defer conn.Close()
	// Read the incoming connection into the buffer.
	reqLen, err := r.Read(buf)
	data := string(buf[:reqLen])

	switch err {
	case nil:
		output = output + data
	default:
		log.Fatalf("Receive data failed:%s", err)
		return
	}
}

var output = ""

func TestMetricsEndpointOutput(t *testing.T) {
	exporter, err := NewExporter(Options{})
	if err != nil {
		t.Fatalf("failed to create graphite exporter: %v", err)
	}
	closeConn = false
	go startServer(exporter)

	view.RegisterExporter(exporter)

	names := []string{"foo", "bar", "baz"}

	var measures mSlice
	for _, name := range names {
		measures.createAndAppend("tests."+name, name, "")
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


	for stay, timeout := true, time.After(3*time.Second); stay; {
		select {
		case <-timeout:
			stay = false
		default:
		}
	}

	if strings.Contains(output, "collected before with the same name and label values") {
		t.Fatal("metric name and labels being duplicated but must be unique")
	}


	if strings.Contains(output, "error(s) occurred") {
		t.Fatal("error reported by graphite registry")
	}

	for _, name := range names {
		if !strings.Contains(output, "tests."+name) {
			t.Fatalf("measurement missing in output: %v", name)
		}
	}
	closeConn = true
}

func TestMetricsPathOutput(t *testing.T) {
	exporter, err := NewExporter(Options{})
	if err != nil {
		t.Fatalf("failed to create graphite exporter: %v", err)
	}

	closeConn = false
	go startServer(exporter)

	view.RegisterExporter(exporter)

	name := "foo.bar"

	var measures mSlice
	measures.createAndAppend(name, name, "")

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


	for stay, timeout := true, time.After(3*time.Second); stay; {
		select {
		case <-timeout:
			stay = false
		default:
		}
	}

	lines := strings.Split(output, "\n")
	ok := false
	for _, line := range(lines) {
		if ok {
			break
		}
		for _, sentence := range(strings.Split(line, " ")) {
			if (sentence == "opencensus.foo.bar")  {
				ok = true
				break
			}

			if ok {
				break
			}
		}
	}
	if !ok {
		t.Fatal("path not correct")
	}
	closeConn = true
}

func TestDistributionData(t *testing.T) {
	exporter, err := NewExporter(Options{})
	if err != nil {
		t.Fatalf("failed to create graphite exporter: %v", err)
	}
	closeConn = false
	go startServer(exporter)
	view.RegisterExporter(exporter)
	reportPeriod := time.Millisecond
	view.SetReportingPeriod(reportPeriod)

	m := stats.Float64("tests/bills", "payments by denomination", stats.UnitDimensionless)
	v := &view.View{
		Name:        "cash/register",
		Description: "this is a test",
		Measure:     m,

		Aggregation: view.Distribution(1, 5, 10, 20, 50, 100, 250),
	}

	if err := view.Register(v); err != nil {
		t.Fatalf("Register error: %v", err)
	}
	defer view.Unregister(v)

	// Give the reporter ample time to process registration
	<-time.After(10 * reportPeriod)

	values := []float64{0.25, 245.67, 12, 1.45, 199.9, 7.69, 187.12}

	ctx := context.Background()
	ms := make([]stats.Measurement, len(values))
	for _, value := range values {
		mx := m.M(value)
		ms = append(ms, mx)
	}
	wantLines := []string{
		`opencensus.cash/register.bucket.tests/bills 1`,
		`opencensus.cash/register.bucket.tests/bills 5`,
		`opencensus.cash/register.bucket.tests/bills 10`,
		`opencensus.cash/register.bucket.tests/bills 20`,
		`opencensus.cash/register.bucket.tests/bills 50`,
		`opencensus.cash/register.bucket.tests/bills 100`,
		`opencensus.cash/register.bucket.tests/bills 250`,
		`opencensus.cash/register.bucket.tests/bills.sum 654.0799999999999`, // Summation of the input values
		`opencensus.cash/register.bucket.tests/bills.count 7`,
	}
	stats.Record(ctx, ms...)

	// Give the recorder ample time to process recording
	<-time.After(10 * reportPeriod)

	for stay, timeout := true, time.After(3*time.Second); stay; {
		select {
		case <-timeout:
			stay = false
		default:
		}
	}

	str := strings.Trim(string(output), "\n")
	lines := strings.Split(str, "\n")
	nonComments := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.Contains(line, "#") {
			nonComments = append(nonComments, line)
		}
	}

	got := strings.Join(nonComments, "\n")
	want := strings.Join(wantLines, "\n")

	if strings.Contains(output, want) {
		t.Fatalf("\ngot:\n%s\n\nwant:\n%s\n", got, want)
	}
	closeConn = true
}