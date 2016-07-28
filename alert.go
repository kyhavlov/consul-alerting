package main

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"strings"
	"time"
)

// AlertState represents the last known state of an alert, stored Consul's KV store
type AlertState struct {
	Status      string `json:"status"`
	LastUpdated int64  `json:"last_updated"`
	Message     string `json:"message"`
}

// Returns a map of nodename/checkname strings to AlertStates from the given KV prefix
func getAlertStates(kvPath string, client *api.Client) (map[string]*AlertState, error) {
	alertStates := make(map[string]*AlertState)
	keys, _, err := client.KV().Keys(kvPath, "", nil)

	if err != nil {
		log.Error("Error loading previous alert states: ", err)
		return alertStates, err
	}

	for _, path := range keys {
		alertState, _ := getAlertState(path, client)

		keyName := strings.Split(path, "/")
		checkName := keyName[len(keyName)-2] + "/" + keyName[len(keyName)-1]
		alertStates[checkName] = alertState
	}

	return alertStates, nil
}

func getAlertState(kvPath string, client *api.Client) (*AlertState, error) {
	kvPair, _, err := client.KV().Get(kvPath, nil)
	alert := &AlertState{}

	if err != nil {
		log.Error("Error checking alert state during callback: ", err)
		return nil, err
	}

	if kvPair == nil {
		return alert, nil
	}

	err = json.Unmarshal(kvPair.Value, alert)

	if err != nil {
		log.Error("Error parsing alert state during callback: ", err)
		return nil, err
	}

	return alert, nil
}

func attemptAlert(changeThreshold int64, kvPath string, client *api.Client) {
	time.Sleep(time.Duration(changeThreshold) * time.Second)

	alertState, err := getAlertState(kvPath, client)

	if err != nil {
		log.Error("Error fetching alert state during callback: ", err)
		return
	}

	if time.Now().Unix()-changeThreshold >= alertState.LastUpdated {
		alertState.Message = alertState.Message + fmt.Sprintf(" for %d seconds", changeThreshold)
		alert(alertState)
	}
}

func alert(alertState *AlertState) {
	log.Warnf("Alert update: %s", alertState.Message)
}
