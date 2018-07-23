package graphite_client

import (
	"bytes"
	"fmt"
	"net"
	"time"
)

// Graphite is a struct that defines the relevant properties of a graphite
// connection
type Graphite struct {
	Host     string
	Port     int
	Timeout  time.Duration
	conn     net.Conn
}

// defaultTimeout is the default number of seconds that we're willing to wait
// before forcing the connection establishment to fail
const defaultTimeout = 5

// Given a Graphite struct, Connect populates the Graphite.conn field with an
// appropriate TCP connection
func (graphite *Graphite) Connect() error {
	if graphite.conn != nil {
		graphite.conn.Close()
	}

	address := fmt.Sprintf("%s:%d", graphite.Host, graphite.Port)

	if graphite.Timeout == 0 {
		graphite.Timeout = defaultTimeout * time.Second
	}

	var err error
	var conn net.Conn

	conn, err = net.DialTimeout("tcp", address, graphite.Timeout)

	graphite.conn = conn

	return err
}

// Given a Graphite struct, Disconnect closes the Graphite.conn field
func (graphite *Graphite) Disconnect() error {
	err := graphite.conn.Close()
	graphite.conn = nil
	return err
}

// sendMetrics is an internal function that is used to write to the TCP
// connection in order to communicate metrics to the remote Graphite host
func (graphite *Graphite) sendMetrics(metrics []Metric) error {
	zeroedMetric := Metric{}
	buf := bytes.NewBufferString("")
	for _, metric := range metrics {
		if metric == zeroedMetric {
			continue
		}
		if metric.Timestamp == 0 {
			metric.Timestamp = time.Now().Unix()
		}
		metricName := metric.Name
		buf.WriteString(fmt.Sprintf("%s %s %d\n", metricName, metric.Value, metric.Timestamp))
	}
	_, err := graphite.conn.Write(buf.Bytes())
	if err != nil {
		return err
	}
	return nil
}

// The SendMetric method can be used to just pass a metric name and value and
// have it be sent to the Graphite host
func (graphite *Graphite) SendMetric(stat string, value string, timestamp int64) error {
	metrics := make([]Metric, 1)
	metrics[0] = NewMetric(stat, value, timestamp)
	err := graphite.sendMetrics(metrics)
	if err != nil {
		return err
	}
	return nil
}

// NewGraphite is a factory method that's used to create a new Graphite
func NewGraphite(host string, port int) (*Graphite, error) {
	return GraphiteFactory(host, port)
}

func GraphiteFactory(host string, port int) (*Graphite, error) {
	var graphite *Graphite

	graphite = &Graphite{Host: host, Port: port}
	err := graphite.Connect()
	if err != nil {
		return nil, err
	}

	return graphite, nil
}
