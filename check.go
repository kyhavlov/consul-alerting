package main

import (
	"encoding/json"
	"strings"

	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

// CheckState is used for storing recent state for a given health check on a specific node,
// in order to preserve alert state across restarts
type CheckState struct {
	Status string `json:"status"`
}

// Returns a map of nodename/checkname strings to CheckStates from the given KV prefix
func getCheckStates(kvPath string, client *api.Client) (map[string]*CheckState, error) {
	checkStates := make(map[string]*CheckState)
	keys, _, err := client.KV().Keys(kvPath, "", nil)

	if err != nil {
		log.Error("Error loading previous check states: ", err)
		return checkStates, err
	}

	for _, path := range keys {
		checkState, err := getCheckState(path, client)

		if err != nil {
			log.Error("Error loading check states: ", err)
			return checkStates, err
		} else if checkState == nil {
			continue
		}

		keyName := strings.Split(path, "/")
		checkName := keyName[len(keyName)-2] + "/" + keyName[len(keyName)-1]
		checkStates[checkName] = checkState
	}

	return checkStates, nil
}

// Parses a CheckState from a given Consul K/V path
func getCheckState(kvPath string, client *api.Client) (*CheckState, error) {
	kvPair, _, err := client.KV().Get(kvPath, nil)
	check := &CheckState{}

	if err != nil {
		log.Error("Error loading check state: ", err)
		return nil, err
	}

	if kvPair == nil {
		return check, nil
	}

	if string(kvPair.Value) == "" {
		return nil, nil
	}

	err = json.Unmarshal(kvPair.Value, check)

	if err != nil {
		log.Error("Error parsing check state: ", err)
		return nil, err
	}

	return check, nil
}

// Updates the last known state of a check in Consul. Returns true if succeeded.
func updateCheckState(update CheckUpdate, client *api.Client) bool {
	check := update.HealthCheck

	kvPath := "service/consul-alerting"

	if check.ServiceID != "" {
		tagPath := ""
		if update.ServiceTag != "" {
			tagPath = fmt.Sprintf("%s/", update.ServiceTag)
		}
		kvPath = kvPath + fmt.Sprintf("/service/%s/%s%s/%s", check.ServiceID, tagPath, check.Node, check.CheckID)
	} else {
		kvPath = kvPath + fmt.Sprintf("/node/%s/%s", check.Node, check.CheckID)
	}

	status, err := json.Marshal(CheckState{
		Status: check.Status,
	})
	if err != nil {
		log.Errorf("Error forming state for alert in Consul: %s", err)
		return false
	}

	_, err = client.KV().Put(&api.KVPair{
		Key:   kvPath,
		Value: status,
	}, nil)

	if err != nil {
		log.Errorf("Error storing state for alert in Consul: %s", err)
		return false
	}

	return true
}

// Given a map of node/checkID:statuses, compute the health of the node/service
func computeHealth(checks map[string]string) string {
	health := api.HealthPassing

	for _, status := range checks {
		switch status {
		case api.HealthWarning:
			if health != api.HealthCritical {
				health = api.HealthWarning
			}
		case api.HealthCritical:
			health = api.HealthCritical
		}
	}

	return health
}
