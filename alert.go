package main

import (
	"encoding/json"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

type AlertState struct {
	Status      string `json:"status"`
	Node        string `json:"node"`
	Service     string `json:"service"`
	Tag         string `json:"tag"`
	LastUpdated int64  `json:"last_updated"`
	Message     string `json:"message"`
	Details     string `json:"details"`
}

// Parses a CheckState from a given Consul K/V path
func getAlertState(kvPath string, client *api.Client) (*AlertState, error) {
	kvPair, _, err := client.KV().Get(kvPath, nil)
	check := &AlertState{}

	if err != nil {
		log.Error("Error loading alert state: ", err)
		return nil, err
	}

	if kvPair == nil {
		return nil, nil
	}

	if string(kvPair.Value) == "" {
		return nil, nil
	}

	err = json.Unmarshal(kvPair.Value, check)

	if err != nil {
		log.Error("Error parsing alert state: ", err)
		return nil, err
	}

	return check, nil
}

// Sets an alert state in at a given K/V path, returns true if succeeded
func setAlertState(kvPath string, alert *AlertState, client *api.Client) bool {
	alert.LastUpdated = time.Now().Unix()

	serialized, err := json.Marshal(alert)
	if err != nil {
		log.Errorf("Error forming state for alert in Consul: %s", err)
		return false
	}

	_, err = client.KV().Put(&api.KVPair{
		Key:   kvPath,
		Value: serialized,
	}, nil)

	if err != nil {
		log.Errorf("Error storing state for alert in Consul: %s", err)
		return false
	}
	return true
}

// Sleeps for changeThreshold duration, then alerts if the state has not changed
func tryAlert(kvPath string, watchOpts *WatchOptions) {
	time.Sleep(time.Duration(watchOpts.changeThreshold) * time.Second)

	alertState, err := getAlertState(kvPath, watchOpts.client)

	if err != nil {
		log.Error("Error fetching alert state: ", err)
		return
	}

	if alertState == nil {
		log.Errorf("Alert state not found at path %s", kvPath)
		return
	}

	if time.Now().Unix()-int64(watchOpts.changeThreshold) >= alertState.LastUpdated {
		for _, handler := range watchOpts.handlers {
			handler.Alert(alertState)
		}
	}
}
