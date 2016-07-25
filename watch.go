package main

import (
	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"time"
	"fmt"
	"encoding/json"
)

// Watches a service for changes in health
func WatchService(service string, tag string, client *api.Client) {
	// Set the options for the watch query
	queryOpts := &api.QueryOptions{
		WaitTime: 5 * time.Minute,
	}

	lastCheckStatus := make(map[string]string)

	tagDisplay := ""
	if tag != "" {
		tagDisplay = fmt.Sprintf(" (tag: %s)", tag)
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
			if oldStatus, ok := lastCheckStatus[check.CheckID]; ok && oldStatus != check.Status {
				// If it did, make sure it's for our tag (if specified)
				if tag != "" {
					node, _, err := client.Catalog().Node(check.Node, nil)
					if err != nil {
						log.Errorf("Error trying to get service info for node '%s': %s", check.Node, err)
						continue
					}

					if nodeService, ok := node.Services[service]; ok && contains(nodeService.Tags, tag) {
						processUpdate(CheckUpdate{ServiceTag: tag, HealthCheck: check}, client)
					}
				} else {
					processUpdate(CheckUpdate{ServiceTag: tag, HealthCheck: check}, client)
				}
			}
			lastCheckStatus[check.CheckID] = check.Status
		}
	}
}

// Watches node for changes in health
func WatchNode(node string, client *api.Client) {
	// Set the options for the watch query
	queryOpts := &api.QueryOptions{
		WaitTime: 5 * time.Minute,
	}

	lastCheckStatus := make(map[string]string)

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
				if oldStatus, ok := lastCheckStatus[check.CheckID]; ok && oldStatus != check.Status {
					processUpdate(CheckUpdate{HealthCheck: check}, client)
				}
				lastCheckStatus[check.CheckID] = check.Status
			}
		}
	}
}

type CheckUpdate struct {
	ServiceTag string
	*api.HealthCheck
}

type AlertState struct {
	Status      string `json:"status"`
	LastUpdated int64  `json:"last_updated"`
}

// processUpdate updates the state of an alert stored in the Consul key-value store
// based on the given CheckUpdate
func processUpdate(update CheckUpdate, client *api.Client) {
	check := update.HealthCheck

	kvPath := "service/consul-alerting"

	if check.ServiceID != "" {
		log.Infof("Check '%s' in service '%s'%s on node %s is now %s",
			check.CheckID, check.ServiceID, update.ServiceTag, check.Node, check.Status)

		tagPath := ""
		if update.ServiceTag != "" {
			tagPath = fmt.Sprintf("%s/", update.ServiceTag)
		}

		kvPath = kvPath + fmt.Sprintf("/service/%s/%s%s", check.ServiceID, tagPath, check.CheckID)
	} else {
		log.Infof("Check '%s' on node %s is now %s",
			check.CheckID, check.Node, check.Status)
		kvPath = kvPath + fmt.Sprintf("/node/%s/%s", check.Node, check.CheckID)
	}

	status, err := json.Marshal(AlertState{
		Status: check.Status,
		LastUpdated: time.Now().Unix(),
	})
	if err != nil {
		log.Errorf("Error forming state for alert in Consul: %s", err)
	}

	_, err = client.KV().Put(&api.KVPair{
		Key: kvPath,
		Value: status,
	}, nil)

	if err != nil {
		log.Errorf("Error storing state for alert in Consul: %s", err)
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}