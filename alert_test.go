package main

import (
	"github.com/hashicorp/consul/api"
	"reflect"
	"testing"
	"time"
)

const testAlertKVPath = "test"

func testAlertConfig() (*Config, chan *AlertState) {
	alertCh := make(chan *AlertState, 1)

	config := &Config{
		Handlers: map[string]AlertHandler{
			"test": testHandler{alertCh},
		},
	}

	return config, alertCh
}

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

	config, alertCh := testAlertConfig()

	go tryAlert(testAlertKVPath, AlertState{
		Status: api.HealthCritical,
	}, &WatchOptions{
		client: client,
		config: config,
	})

	select {
	case <-alertCh:
	case <-time.After(1 * time.Second):
		t.Error("didn't get alert")
	}
}

// Set up two handlers but only add one to DefaultHandlers
func TestAlert_defaultHandler(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	alertCh := make(chan *AlertState)
	ignoredCh := make(chan *AlertState)

	config := &Config{
		DefaultHandlers: []string{"test"},
		Handlers: map[string]AlertHandler{
			"test":         testHandler{alertCh},
			"test_ignored": testHandler{ignoredCh},
		},
	}

	go tryAlert(testAlertKVPath, AlertState{
		Status: api.HealthCritical,
	}, &WatchOptions{
		client: client,
		config: config,
	})

	select {
	case <-alertCh:
	case <-time.After(1 * time.Second):
		t.Error("didn't get alert")
	}

	select {
	case <-ignoredCh:
		t.Error("got unexpected alert on ignored alert handler")
	case <-time.After(1 * time.Second):
	}
}

// Set up two handlers but configure the service to only alert on one
func TestAlert_specifyHandler(t *testing.T) {
	client, server := testConsul(t)
	defer server.Stop()

	alertCh := make(chan *AlertState)
	ignoredCh := make(chan *AlertState)

	config := &Config{
		Services: map[string]ServiceConfig{
			testServiceName: ServiceConfig{
				Name:     testServiceName,
				Handlers: []string{"test"},
			},
		},
		Handlers: map[string]AlertHandler{
			"test":         testHandler{alertCh},
			"test_ignored": testHandler{ignoredCh},
		},
	}

	go tryAlert(testAlertKVPath, AlertState{
		Status: api.HealthCritical,
	}, &WatchOptions{
		service: testServiceName,
		client:  client,
		config:  config,
	})

	select {
	case <-alertCh:
	case <-time.After(1 * time.Second):
		t.Error("didn't get alert")
	}

	select {
	case <-ignoredCh:
		t.Error("got unexpected alert on ignored alert handler")
	case <-time.After(1 * time.Second):
	}
}
