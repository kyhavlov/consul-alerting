package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

// AlertState represents the last known state of an alert, stored Consul's KV store
type AlertState struct {
	Status      string `json:"status"`
	Node        string `json:"node"`
	Service     string `json:"service"`
	Tag         string ``
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
		alertState, err := getAlertState(path, client)

		if err != nil {
			log.Error("Error loading alert state: ", err)
			return alertStates, err
		}

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
		log.Error("Error checking alert state: ", err)
		return nil, err
	}

	if kvPair == nil {
		return alert, nil
	}

	err = json.Unmarshal(kvPair.Value, alert)

	if err != nil {
		log.Error("Error parsing alert state: ", err)
		return nil, err
	}

	return alert, nil
}

func attemptAlert(changeThreshold int64, kvPath string, client *api.Client, handlers []AlertHandler) {
	time.Sleep(time.Duration(changeThreshold) * time.Second)

	alertState, err := getAlertState(kvPath, client)

	if err != nil {
		log.Error("Error fetching alert state: ", err)
		return
	}

	if time.Now().Unix()-changeThreshold >= alertState.LastUpdated {
		alertState.Message = alertState.Message + fmt.Sprintf(" for %d seconds", changeThreshold)
		for _, handler := range handlers {
			handler.Alert(alertState)
		}
	}
}
