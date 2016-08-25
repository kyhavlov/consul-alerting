package main

import (
	"reflect"
	"testing"
	"time"
)

const testAlertKVPath = "test"

// Make sure we can properly serialize an AlertState struct to the KV store
// and read it back
func TestAlert_setGetAlert(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	expected := &AlertState{
		Status:  "passing",
		Details: "test",
	}

	setAlertState(testAlertKVPath, expected, client)
	alert, err := getAlertState(testAlertKVPath, client)

	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(alert, expected) {
		t.Errorf("expected \n%#v\n\n, got \n\n%#v\n\n", expected, alert)
	}
}

// Set up an alert and make sure it gets sent to our handler
func TestAlert_tryAlert(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	alertCh := make(chan *AlertState)

	setAlertState(testAlertKVPath, &AlertState{}, client)

	go tryAlert(testAlertKVPath, &WatchOptions{
		client: client,
		handlers: []AlertHandler{
			testHandler{alertCh},
		},
	})

	select {
	case <-alertCh:
	case <-time.After(1 * time.Second):
		t.Error("didn't get alert")
	}
}

// Make sure we handle the case where the alert state disappears from the KV store
func TestAlert_alertNotFound(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	alertCh := make(chan *AlertState)

	go tryAlert(testAlertKVPath, &WatchOptions{
		client: client,
		handlers: []AlertHandler{
			testHandler{alertCh},
		},
	})

	select {
	case alert := <-alertCh:
		t.Errorf("received alert, but should have got nothing: %v", alert)
	case <-time.After(1 * time.Second):
	}
}
