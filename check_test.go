package main

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul/api"
)

func testSetCheckState(update CheckUpdate, client *api.Client, t *testing.T) {
	success := updateCheckState(update, client)

	if !success {
		t.Fatal("Failed to write check state to Consul")
	}
}

// Make sure we can serialize/deserialize a CheckUpdate for a node
func TestCheck_getSetCheckNode(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	expected := &api.HealthCheck{
		Node:    "node1",
		CheckID: "testcheck",
		Status:  "warning",
	}

	testSetCheckState(CheckUpdate{
		HealthCheck: expected,
	}, client, t)

	check, err := getCheckState(alertingKVRoot+fmt.Sprintf("/node/%s/%s", expected.Node, expected.CheckID), client)

	if err != nil {
		t.Fatal(err)
	}

	if check.Status != expected.Status {
		t.Errorf("expected status %s, got %s", expected.Status, check.Status)
	}
}

// Make sure we can serialize/deserialize a CheckUpdate for a service
func TestCheck_getSetCheckService(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	expected := &api.HealthCheck{
		ServiceName: "redis",
		ServiceID:   "redis",
		Node:        "node1",
		CheckID:     "testcheck",
		Status:      "warning",
	}
	update := CheckUpdate{
		ServiceTag:  "alpha",
		HealthCheck: expected,
	}

	testSetCheckState(update, client, t)

	check, err := getCheckState(alertingKVRoot+fmt.Sprintf("/service/%s/%s/%s/%s",
		expected.ServiceName,
		update.ServiceTag,
		expected.Node,
		expected.CheckID), client)

	if err != nil {
		t.Fatal(err)
	}

	if check.Status != expected.Status {
		t.Errorf("expected status %s, got %s", expected.Status, check.Status)
	}
}

// Make sure we can fetch multiple checks under a prefix with getCheckStates
func TestCheck_getSetChecks(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	node := "node1"

	expected := map[string]*api.HealthCheck{
		node + "/testcheck1": &api.HealthCheck{
			Node:    node,
			CheckID: "testcheck1",
			Status:  "warning",
		},
		node + "/testcheck2": &api.HealthCheck{
			Node:    node,
			CheckID: "testcheck2",
			Status:  "passing",
		},
	}

	for _, check := range expected {
		testSetCheckState(CheckUpdate{
			HealthCheck: check,
		}, client, t)
	}

	checks, err := getCheckStates(alertingKVRoot+"/node/"+node+"/", client)

	if err != nil {
		t.Fatal(err)
	}

	if len(checks) != 2 {
		t.Errorf("Expected 2 checks, got %d", len(checks))
	}

	for check, state := range checks {
		if expected[check].Status != state.Status {
			t.Errorf("expected status %s for check %s, got %s", expected[check].Status, check, state)
		}
	}
}
