package client

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Change these to be your own graphite server if you so please

const (
	graphiteHost = "127.0.0.1"
	graphitePort = 2003
)

var output = ""
var closeConn = false

func startServer() {
	l, err := net.Listen("tcp", graphiteHost+":"+strconv.Itoa(graphitePort))
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		return
	}

	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Listening on " + graphiteHost + ":" + strconv.Itoa(graphitePort))
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
		go handleRequest(l, conn)
	}
}

// Handles incoming requests.
func handleRequest(l net.Listener, conn net.Conn) {
	if closeConn {
		conn.Close()
		l.Close()
		return
	}
	// Make a buffer to hold incoming data.
	buf := make([]byte, 1024)
	r := bufio.NewReader(conn)

	defer conn.Close()
	// Read the incoming connection into the buffer.
	reqLen, err := r.Read(buf)
	data := string(buf[:reqLen])

	if err != nil {
		log.Fatalf("Receive data failed:%s", err)
		return
	} else {
		output = output + data
	}
}

func TestNewGraphite(t *testing.T) {
	closeConn = false
	go startServer()
	gh, err := NewGraphite(graphiteHost, graphitePort)
	if err != nil {
		t.Error(err)
	}

	if _, ok := gh.conn.(*net.TCPConn); !ok {
		t.Error("GraphiteHost.conn is not a TCP connection")
	}
	closeConn = true
}

func TestGraphiteFactoryTCP(t *testing.T) {
	closeConn = false
	go startServer()
	gr, err := NewGraphite(graphiteHost, graphitePort)

	if err != nil {
		t.Error(err)
	}

	if _, ok := gr.conn.(*net.TCPConn); !ok {
		t.Error("GraphiteHost.conn is not a TCP connection")
	}

	closeConn = true
}

func TestSendMetric(t *testing.T) {
	closeConn = false
	go startServer()
	gr, err := NewGraphite(graphiteHost, graphitePort)

	if err != nil {
		t.Error(err)
	}

	if _, ok := gr.conn.(*net.TCPConn); !ok {
		t.Error("GraphiteHost.conn is not a TCP connection")
	}

	metricName := "graphite.path"
	metricValue := "2"
	gr.SendMetric(metricName, metricValue, time.Now())
	<-time.After(10 * time.Millisecond)

	if !strings.Contains(output, metricName+" "+metricValue) {
		t.Fatal("metric name and value are not being sent")
	}

	closeConn = true
	gr.Disconnect()
	<-time.After(10 * time.Millisecond)
}

func TestInvalidHost(t *testing.T) {
	go startServer()
	_, err := NewGraphite("Invalid", graphitePort)
	if err == nil {
		t.Fatal("an error should have been raised")
	}
}
