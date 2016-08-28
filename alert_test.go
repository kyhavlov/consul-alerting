package main

import (
	"github.com/hashicorp/consul/api"
	"reflect"
	"testing"
	"time"
)

const testAlertKVPath = "test"

func testAlertConfig() (*Config, chan *AlertState) {
	alertCh := make(chan *AlertState)

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

	setAlertState(testAlertKVPath, &AlertState{}, client)

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
