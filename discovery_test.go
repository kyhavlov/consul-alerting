package main

import (
	"github.com/hashicorp/consul/consul/structs"
	"github.com/hashicorp/consul/testutil"
	"testing"
	"time"
)

// Waits up to the timeout to receive an alert with the given status on the channel
func testWaitForAlert(t *testing.T, alertCh chan *AlertState, status string, timeout time.Duration) {
	select {
	case alert := <-alertCh:
		if alert.Status != structs.HealthCritical {
			t.Fatalf("expected alert on status %s, got %s", structs.HealthCritical, alert.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("didn't get alert within the timeout")
	}
}

// Alert on a pre-existing service
func TestDiscovery_existingServiceLocal(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	// Add a service with passing health
	server.AddService(testServiceName, structs.HealthPassing, nil)

	alertCh := make(chan *AlertState)

	config := DefaultConfig()
	config.ChangeThreshold = 0
	config.Handlers["test"] = testHandler{alertCh}
	go discoverServices(server.Config.NodeName, config, &ShutdownOpts{}, client)

	<-time.After(1 * time.Second)

	// Change service health to critical
	server.AddService(testServiceName, structs.HealthCritical, nil)

	testWaitForAlert(t, alertCh, structs.HealthCritical, 5*time.Second)
}

// Alert on a service registered after starting up
func TestDiscovery_discoveredServiceLocal(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	alertCh := make(chan *AlertState)

	config := DefaultConfig()
	config.ChangeThreshold = 0
	config.Handlers["test"] = testHandler{alertCh}
	go discoverServices(server.Config.NodeName, config, &ShutdownOpts{}, client)

	<-time.After(1 * time.Second)

	// Register service with critical health
	server.AddService(testServiceName, structs.HealthCritical, nil)

	testWaitForAlert(t, alertCh, structs.HealthCritical, 5*time.Second)
}

// Alert on a pre-existing service on another node
func TestDiscovery_existingServiceGlobal(t *testing.T) {
	client, server1 := testConsul(t)
	defer server1.Stop()

	server2 := testutil.NewTestServerConfig(t, func(c *testutil.TestServerConfig) {
		c.Bootstrap = false
	})
	defer server2.Stop()

	server1.JoinLAN(server2.LANAddr)

	// Add a service with passing health on the remote server
	server2.AddService(testServiceName, structs.HealthPassing, nil)

	alertCh := make(chan *AlertState)

	config := DefaultConfig()
	config.ChangeThreshold = 0
	config.ServiceWatch = GlobalMode
	config.Handlers["test"] = testHandler{alertCh}
	go discoverServices(server1.Config.NodeName, config, &ShutdownOpts{}, client)

	<-time.After(1 * time.Second)

	// Set the service to critical health
	server2.AddService(testServiceName, structs.HealthCritical, nil)

	testWaitForAlert(t, alertCh, structs.HealthCritical, 5*time.Second)
}

// Alert on a service registered on another node after starting up
func TestDiscovery_discoveredServiceGlobal(t *testing.T) {
	client, server1 := testConsul(t)
	defer server1.Stop()

	server2 := testutil.NewTestServerConfig(t, func(c *testutil.TestServerConfig) {
		c.Bootstrap = false
	})
	defer server2.Stop()

	server1.JoinLAN(server2.LANAddr)

	alertCh := make(chan *AlertState)

	config := DefaultConfig()
	config.ChangeThreshold = 0
	config.ServiceWatch = GlobalMode
	config.Handlers["test"] = testHandler{alertCh}
	go discoverServices(server1.Config.NodeName, config, &ShutdownOpts{}, client)

	<-time.After(1 * time.Second)

	// Add a new service with critical health on the remote
	server2.AddService(testServiceName, structs.HealthCritical, nil)

	testWaitForAlert(t, alertCh, structs.HealthCritical, 5*time.Second)
}

// Alert on an existing node
func TestDiscovery_existingNode(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	// Register a check on the new node with critical status
	server.AddCheck("nodecheck", "", structs.HealthCritical)

	alertCh := make(chan *AlertState)

	config := DefaultConfig()
	config.ChangeThreshold = 0
	config.Handlers["test"] = testHandler{alertCh}
	go discoverNodes(config, &ShutdownOpts{}, client)

	<-time.After(1 * time.Second)

	testWaitForAlert(t, alertCh, structs.HealthCritical, 5*time.Second)
}

// Alert on a discovered node
func TestDiscovery_discoveredNode(t *testing.T) {
	client, server1 := testConsul(t)
	defer server1.Stop()

	alertCh := make(chan *AlertState)

	config := DefaultConfig()
	config.ChangeThreshold = 0
	config.Handlers["test"] = testHandler{alertCh}
	go discoverNodes(config, &ShutdownOpts{}, client)

	<-time.After(1 * time.Second)

	server2 := testutil.NewTestServerConfig(t, func(c *testutil.TestServerConfig) {
		c.Bootstrap = false
	})
	defer server2.Stop()

	server1.JoinLAN(server2.LANAddr)

	// Register a check on the new node with critical status
	server2.AddCheck("nodecheck", "", structs.HealthCritical)

	testWaitForAlert(t, alertCh, structs.HealthCritical, 5*time.Second)
}
