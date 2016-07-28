package main

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"time"
)

const watchWaitTime = 5 * time.Minute

// Watches a service for changes in health
func WatchService(service string, tag string, changeThreshold int, client *api.Client) {
	// Set wait time to make the consul query block until an update happens
	queryOpts := &api.QueryOptions{
		WaitTime: watchWaitTime,
	}

	tagDisplay := ""
	tagPath := ""
	if tag != "" {
		tagPath = tag + "/"
		tagDisplay = fmt.Sprintf(" (tag: %s)", tag)
	}

	lastCheckStatus := make(map[string]string)
	storedAlertStates, err := getAlertStates("service/consul-alerting/service/"+service+"/"+tagPath, client)

	if err != nil {
		log.Error("Error loading previous alert states from consul: ", err)
	}

	for checkName, alertState := range storedAlertStates {
		lastCheckStatus[checkName] = alertState.Status
	}

	log.Infof("Starting watch for service: %s%s", service, tagDisplay)

	for {
		// Do a blocking query (a consul watch) for the health of the service
		checks, queryMeta, err := client.Health().Checks(service, queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch service: %s, retrying in 10s...", err)
			time.Sleep(10 * time.Second)
			continue
		}

		queryOpts.WaitIndex = queryMeta.LastIndex

		//log.Debugf("Got watch return for service %s", service)

		for _, check := range checks {
			// Determine whether the check changed status
			if oldStatus, ok := lastCheckStatus[check.Node+"/"+check.CheckID]; ok && oldStatus != check.Status {
				// If it did, make sure it's for our tag (if specified)
				if tag != "" {
					node, _, err := client.Catalog().Node(check.Node, nil)
					if err != nil {
						log.Errorf("Error trying to get service info for node '%s': %s", check.Node, err)
						continue
					}

					if nodeService, ok := node.Services[service]; ok && contains(nodeService.Tags, tag) {
						processUpdate(CheckUpdate{ServiceTag: tag, HealthCheck: check}, changeThreshold, client)
					}
				} else {
					processUpdate(CheckUpdate{ServiceTag: tag, HealthCheck: check}, changeThreshold, client)
				}
			}
			lastCheckStatus[check.Node+"/"+check.CheckID] = check.Status
		}
	}
}

// Watches a node for changes in health
func WatchNode(node string, changeThreshold int, client *api.Client) {
	// Set the options for the watch query
	queryOpts := &api.QueryOptions{
		WaitTime: watchWaitTime,
	}

	lastCheckStatus := make(map[string]string)
	storedAlertStates, err := getAlertStates("service/consul-alerting/node/"+node+"/", client)

	if err != nil {
		log.Error("Error loading previous alert states from consul: ", err)
	}

	for checkName, alertState := range storedAlertStates {
		lastCheckStatus[checkName] = alertState.Status
	}

	log.Infof("Starting watch for node: %s", node)

	for {
		// Do a blocking query (a consul watch) for the health of the node
		checks, queryMeta, err := client.Health().Node(node, queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch node: %s, retrying in 10s...", err)
			time.Sleep(10 * time.Second)
			continue
		}

		queryOpts.WaitIndex = queryMeta.LastIndex

		//log.Debugf("Got watch return for node %s", node)

		for _, check := range checks {
			if check.ServiceID == "" {
				// Determine whether the check changed status
				if oldStatus, ok := lastCheckStatus[node+"/"+check.CheckID]; ok && oldStatus != check.Status {
					processUpdate(CheckUpdate{HealthCheck: check}, changeThreshold, client)
				}
				lastCheckStatus[node+"/"+check.CheckID] = check.Status
			}
		}
	}
}

type CheckUpdate struct {
	ServiceTag string
	*api.HealthCheck
}

// processUpdate updates the state of an alert stored in the Consul key-value store
// based on the given CheckUpdate
func processUpdate(update CheckUpdate, changeThreshold int, client *api.Client) {
	check := update.HealthCheck

	kvPath := "service/consul-alerting"
	message := ""

	if check.ServiceID != "" {
		tagPath, tagInfo := "", ""
		if update.ServiceTag != "" {
			tagPath = fmt.Sprintf("%s/", update.ServiceTag)
			tagInfo = fmt.Sprintf(" (tag: %s)", update.ServiceTag)
		}
		message = fmt.Sprintf("Check '%s' in service '%s'%s on node %s is %s",
			check.CheckID, check.ServiceID, tagInfo, check.Node, check.Status)

		kvPath = kvPath + fmt.Sprintf("/service/%s/%s%s/%s", check.ServiceID, tagPath, check.Node, check.CheckID)
	} else {
		message = fmt.Sprintf("Check '%s' on node %s is %s", check.CheckID, check.Node, check.Status)
		kvPath = kvPath + fmt.Sprintf("/node/%s/%s", check.Node, check.CheckID)
	}

	log.Debug(message)

	status, err := json.Marshal(AlertState{
		Status:      check.Status,
		LastUpdated: time.Now().Unix(),
		Message:     message,
	})
	if err != nil {
		log.Errorf("Error forming state for alert in Consul: %s", err)
	}

	_, err = client.KV().Put(&api.KVPair{
		Key:   kvPath,
		Value: status,
	}, nil)

	if err != nil {
		log.Errorf("Error storing state for alert in Consul: %s", err)
	}

	go attemptAlert(int64(changeThreshold), kvPath, client)
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
