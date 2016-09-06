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
	LastAlerted string `json:"last_alerted"`
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
func setAlertState(kvPath string, alert *AlertState, client *api.Client) {
	alert.LastUpdated = time.Now().Unix()

	serialized, err := json.Marshal(alert)
	if err != nil {
		log.Errorf("Error forming state for alert in Consul: %s", err)
		return
	}

	_, err = client.KV().Put(&api.KVPair{
		Key:   kvPath,
		Value: serialized,
	}, nil)

	if err != nil {
		log.Errorf("Error storing state for alert in Consul: %s", err)
		return
	}
}

// Waits for changeThreshold duration, then alerts if LastUpdated has not
// changed in the meantime (which would indicate another alert resetting the timer)
func tryAlert(kvPath string, update AlertState, watchOpts *WatchOptions) {
	alert, err := getAlertState(kvPath, watchOpts.client)

	if err != nil {
		log.Error("Error fetching alert state: ", err)
		return
	}

	// Create a new alert state if there's no pre-existing one
	if alert == nil {
		alert = &AlertState{
			Node:        watchOpts.node,
			Service:     watchOpts.service,
			Tag:         watchOpts.tag,
			LastAlerted: api.HealthPassing,
		}
	}

	alert.Status = update.Status
	alert.Message = update.Message
	alert.Details = update.Details

	// Set LastUpdated on the alert to reset the timer
	setAlertState(kvPath, alert, watchOpts.client)

	changeThreshold := watchOpts.config.serviceChangeThreshold(watchOpts.service)
	log.Debugf("Starting timer for alert: '%s'", update.Message)
	time.Sleep(time.Duration(changeThreshold) * time.Second)

	alert, err = getAlertState(kvPath, watchOpts.client)

	if err != nil {
		log.Error("Error fetching alert state: ", err)
		return
	}

	if alert == nil {
		log.Errorf("Alert state not found at path %s", kvPath)
		return
	}

	// If no new alerts were triggered during the sleep, send the alert to each handler to be processed
	if time.Now().Unix()-int64(changeThreshold) >= alert.LastUpdated && update.Status != alert.LastAlerted {
		for _, handler := range watchOpts.config.serviceHandlers(watchOpts.service) {
			handler.Alert(alert)
		}
		alert.LastAlerted = update.Status
		setAlertState(kvPath, alert, watchOpts.client)
	}
}
