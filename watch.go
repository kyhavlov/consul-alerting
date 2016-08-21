package main

import (
	"encoding/json"
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

const watchWaitTime = 15 * time.Second
const errorWaitTime = 10 * time.Second

type WatchOptions struct {
	changeThreshold int
	client          *api.Client
	handlers        []AlertHandler
	stopCh          chan struct{}
}

// Watches a service for changes in health, updating the given handlers when an alert fires
func WatchService(service string, tag string, watchOpts *WatchOptions) {
	// Set wait time to make the consul query block until an update happens
	client := watchOpts.client
	queryOpts := &api.QueryOptions{
		WaitTime: watchWaitTime,
	}

	tagDisplay := ""
	tagPath := ""
	if tag != "" {
		tagPath = tag + "/"
		tagDisplay = fmt.Sprintf(" (tag: %s)", tag)
	}

	keyPath := "service/consul-alerting/service/" + service + "/" + tagPath

	// Load previous alert states for this service from consul
	lastCheckStatus := make(map[string]string)
	storedAlertStates, err := getAlertStates(keyPath, client)

	if err != nil {
		log.Error("Error loading previous alert states from consul: ", err)
	}

	for checkName, alertState := range storedAlertStates {
		lastCheckStatus[checkName] = alertState.Status
	}

	// Set up the lock this thread will use to determine leader status
	lockPath := keyPath + "leader"
	apiLock, err := client.LockKey(lockPath)

	if err != nil {
		log.Fatalf("Error initializing lock for service %s%s %s", service, tagDisplay, err)
	}

	lock := LockHelper{
		target: service + tagDisplay,
		path:   lockPath,
		client: client,
		lock:   apiLock,
		stopCh: make(chan struct{}, 1),
		lockCh: make(chan struct{}, 1),
	}
	go lock.start()

	log.Debugf("Initialized watch for service: %s%s", service, tagDisplay)

	for {
		select {
		case <-watchOpts.stopCh:
			log.Infof("Shutting down watch for service %s%s", service, tagDisplay)
			lock.stop()
			<-watchOpts.stopCh
			break
		default:
		}

		if !lock.acquired {
			time.Sleep(1 * time.Second)
			continue
		}

		// Do a blocking query (a consul watch) for the health of the service
		checks, queryMeta, err := client.Health().Checks(service, queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch service: %s, retrying in 10s...", err)
			time.Sleep(errorWaitTime)
			continue
		}

		queryOpts.WaitIndex = queryMeta.LastIndex

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
						processUpdate(CheckUpdate{ServiceTag: tag, HealthCheck: check}, watchOpts)
					}
				} else {
					processUpdate(CheckUpdate{ServiceTag: tag, HealthCheck: check}, watchOpts)
				}
				lastCheckStatus[check.Node+"/"+check.CheckID] = check.Status
			} else {
				lastCheckStatus[check.Node+"/"+check.CheckID] = api.HealthPassing
			}
		}
	}
}

// Watches a node for changes in health
func WatchNode(node string, watchOpts *WatchOptions) {
	// Set the options for the watch query
	queryOpts := &api.QueryOptions{
		WaitTime: watchWaitTime,
	}

	client := watchOpts.client

	lastCheckStatus := make(map[string]string)
	storedAlertStates, err := getAlertStates("service/consul-alerting/node/"+node+"/", client)

	if err != nil {
		log.Error("Error loading previous alert states from consul: ", err)
	}

	for checkName, alertState := range storedAlertStates {
		lastCheckStatus[checkName] = alertState.Status
	}

	log.Debugf("Initialized watch for node: %s", node)

	for {
		// Do a blocking query (a consul watch) for the health of the node
		checks, queryMeta, err := client.Health().Node(node, queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch node: %s, retrying in 10s...", err)
			time.Sleep(errorWaitTime)
			continue
		}

		queryOpts.WaitIndex = queryMeta.LastIndex

		//log.Debugf("Got watch return for node %s", node)

		for _, check := range checks {
			if check.ServiceID == "" {
				// Determine whether the check changed status
				if oldStatus, ok := lastCheckStatus[node+"/"+check.CheckID]; ok && oldStatus != check.Status {
					processUpdate(CheckUpdate{HealthCheck: check}, watchOpts)
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
func processUpdate(update CheckUpdate, watchOpts *WatchOptions) {
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
		Node:        check.Node,
		Service:     check.ServiceName,
		Tag:         update.ServiceTag,
		LastUpdated: time.Now().Unix(),
		Message:     message,
	})
	if err != nil {
		log.Errorf("Error forming state for alert in Consul: %s", err)
	}

	_, err = watchOpts.client.KV().Put(&api.KVPair{
		Key:   kvPath,
		Value: status,
	}, nil)

	if err != nil {
		log.Errorf("Error storing state for alert in Consul: %s", err)
	}

	go attemptAlert(int64(watchOpts.changeThreshold), kvPath, watchOpts.client, watchOpts.handlers)
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
