package main

import (
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/consul/structs"
	"github.com/hashicorp/consul/testutil"
)

const testServiceName = "redis"
const testNodeCheckName = "memory usage"

// A special test handler that does nothing but send the alert to a channel
type testHandler struct {
	alerts chan *AlertState
}

func (t testHandler) Alert(alert *AlertState) {
	t.alerts <- alert
}

// Create a test Consul server and a client for making calls to it
func testConsul(t *testing.T) (*api.Client, *testutil.TestServer) {
	server := testutil.NewTestServer(t)

	config := api.DefaultConfig()
	config.Address = server.HTTPAddr
	client, err := api.NewClient(config)

	if err != nil {
		t.Fatal(err)
	}

	return client, server
}

// The basic flow of a service becoming unhealthy and then recovering
func TestWatch_alertService(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	// Add a service with passing health
	server.AddService(testServiceName, structs.HealthPassing, nil)

	alertCh := make(chan *AlertState)

	go watch(&WatchOptions{
		service:       testServiceName,
		diffCheckFunc: diffServiceChecks,
		client:        client,
		handlers: []AlertHandler{
			testHandler{alertCh},
		},
	})

	<-time.After(1 * time.Second)

	// Change service health to critical
	server.AddService(testServiceName, structs.HealthCritical, nil)

	select {
	case alert := <-alertCh:
		if alert.Status != structs.HealthCritical {
			t.Fatalf("expected alert on status %s, got %s", structs.HealthCritical, alert.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("didn't get alert within the timeout")
	}

	// Set service back to passing health
	server.AddService(testServiceName, structs.HealthPassing, nil)

	select {
	case alert := <-alertCh:
		if alert.Status != structs.HealthPassing {
			t.Fatalf("expected alert on status %s, got %s", structs.HealthPassing, alert.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("didn't get alert within the timeout")
	}
}

// The basic flow of a node becoming unhealthy and then recovering
func TestWatch_alertNode(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	// Create a node check
	server.AddCheck(testNodeCheckName, "", structs.HealthPassing)

	alertCh := make(chan *AlertState)

	go watch(&WatchOptions{
		node:          server.Config.NodeName,
		diffCheckFunc: diffServiceChecks,
		client:        client,
		handlers: []AlertHandler{
			testHandler{alertCh},
		},
	})

	<-time.After(1 * time.Second)

	// Change check health to critical
	server.AddCheck(testNodeCheckName, "", structs.HealthCritical)

	select {
	case alert := <-alertCh:
		if alert.Status != structs.HealthCritical {
			t.Fatalf("expected alert on status %s, got %s", structs.HealthCritical, alert.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("didn't get alert within the timeout")
	}

	// Set check back to passing
	server.AddCheck(testNodeCheckName, "", structs.HealthPassing)

	select {
	case alert := <-alertCh:
		if alert.Status != structs.HealthPassing {
			t.Fatalf("expected alert on status %s, got %s", structs.HealthPassing, alert.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("didn't get alert within the timeout")
	}
}

// Test that we don't alert if the status isn't stable throughout the changeThreshold
func TestWatch_changeThreshold(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	// Add a service with passing health
	server.AddService(testServiceName, structs.HealthPassing, nil)

	alertCh := make(chan *AlertState)

	changeThreshold := 5 * time.Second

	go watch(&WatchOptions{
		service:         testServiceName,
		changeThreshold: changeThreshold,
		diffCheckFunc:   diffServiceChecks,
		client:          client,
		handlers: []AlertHandler{
			testHandler{alertCh},
		},
	})

	<-time.After(1 * time.Second)

	// Change service health to critical
	server.AddService(testServiceName, structs.HealthCritical, nil)

	<-time.After(1 * time.Second)

	// Change service health back to passing so we never get an alert
	server.AddService(testServiceName, structs.HealthPassing, nil)

	<-time.After(1 * time.Second)

	select {
	case alert := <-alertCh:
		t.Fatalf("received an alert when we should have received nothing: %v", alert)

	// If we got nothing after changeThreshold seconds, success
	case <-time.After(changeThreshold):
	}
}
