package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

type AlertState struct {
	Status      string `json:"status"`
	Node        string `json:"node"`
	Service     string `json:"service"`
	Tag         string `json:"tag"`
	UpdateIndex int64  `json:"update_index"`
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
func setAlertState(kvPath string, alert *AlertState, client *api.Client) error {
	serialized, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("Error forming state for alert in Consul: %s", err)
	}

	_, err = client.KV().Put(&api.KVPair{
		Key:   kvPath,
		Value: serialized,
	}, nil)

	if err != nil {
		return fmt.Errorf("Error storing state for alert in Consul: %s", err)
	}

	return nil
}

// Waits for changeThreshold duration, then alerts if LastUpdated has not
// changed in the meantime (which would indicate another alert resetting the timer)
func tryAlert(kvPath string, update AlertState, watchOpts *WatchOptions) {
	// Lock the mutex while reading or writing the alert state to avoid race conditions
	watchOpts.alertLock.Lock()
	alert, err := getAlertState(kvPath, watchOpts.client)

	if err != nil {
		log.Error("Error fetching alert state: ", err)
		watchOpts.alertLock.Unlock()
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

	// Increment the update index and store it, so we can check later to see if it changed
	alert.UpdateIndex++
	updateIndex := alert.UpdateIndex

	// Set LastUpdated on the alert to reset the timer
	err = setAlertState(kvPath, alert, watchOpts.client)
	if err != nil {
		log.Error("Error setting alert state: ", err)
		watchOpts.alertLock.Unlock()
		return
	}
	watchOpts.alertLock.Unlock()

	changeThreshold := watchOpts.config.serviceChangeThreshold(watchOpts.service)
	log.Debugf("Starting timer for alert: '%s'", update.Message)
	time.Sleep(time.Duration(changeThreshold) * time.Second)

	watchOpts.alertLock.Lock()
	defer watchOpts.alertLock.Unlock()

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
	if alert.UpdateIndex == updateIndex && update.Status != alert.LastAlerted {
		for _, handler := range watchOpts.config.serviceHandlers(watchOpts.service) {
			handler.Alert(watchOpts.config.ConsulDatacenter, alert)
		}
		alert.LastAlerted = update.Status

		err = setAlertState(kvPath, alert, watchOpts.client)
		if err != nil {
			log.Error("Error setting alert state: ", err)
		}
	}
}

// Returns each failing check and its output, used for formatting alert details
func nodeDetails(checks []*api.HealthCheck) string {
	details := ""

	for _, check := range checks {
		if check.ServiceID == "" && (check.Status == api.HealthCritical || check.Status == api.HealthWarning) {
			details = details + fmt.Sprintf("=> (check) %s:\n%s", check.Name, check.Output)
		}
	}

	// Only set details if we have failing checks
	if details != "" {
		details = "Failing checks:\n" + details
	}

	return strings.TrimSpace(details)
}

// Returns each failing check and its output, grouped by node, used for formatting alert details
func serviceDetails(checks []*api.HealthCheck) string {
	details := ""
	// Make a map for combining the failing health check outputs on each node
	nodeStatuses := make(map[string]string)

	for _, check := range checks {
		if check.Status == api.HealthCritical || check.Status == api.HealthWarning {
			if _, ok := nodeStatuses[check.Node]; !ok {
				nodeStatuses[check.Node] = ""
			}
			nodeStatuses[check.Node] = nodeStatuses[check.Node] + fmt.Sprintf("==> (check) %s:\n%s", check.Name, check.Output)
		}
	}

	// Only set details if we have failing checks
	if len(nodeStatuses) > 0 {
		details = "Failing checks:\n"
		for node, status := range nodeStatuses {
			details = details + fmt.Sprintf("=> (node) %s\n%s", node, status)
		}
	}

	return strings.TrimSpace(details)
}
