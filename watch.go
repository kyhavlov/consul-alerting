package main

import (
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

const watchWaitTime = 15 * time.Second
const errorWaitTime = 10 * time.Second

// The settings to use when performing a watch on a service or node
type WatchOptions struct {
	// The node name in Consul to use. Only used when watching a node.
	node string

	// The service to watch. Only used when watching a service.
	service string

	// Optional. The tag to use when watching a service. If not specified, all nodes in
	// the service will be used when checking its health.
	tag string

	// The duration that this service's health must remain stable before being alerted on
	changeThreshold time.Duration

	// The Consul client object to use for making requests
	client *api.Client

	// The list of AlertHandlers to call when an alert happens
	handlers []AlertHandler

	// A channel to use in order to stop the watch and release its lock.
	stopCh chan struct{}
}

const ServiceWatch = "service"
const NodeWatch = "node"

/*  Watches a service or node for changes in health, updating the given handlers when an alert fires.

Each watch is responsible for alerting on its own node/service, by watching the health check
endpoint for the node/service.

The general workflow for a watch is:
1. Block until acquiring the lock
2. Upon acquiring the lock, read the previous check/alert state from the Consul K/V store
3. While we have the lock, loop through the following:
	- Do a blocking query for up to watchWaitTime to get new health check updates
	- Compare the returned health checks to the local cache to see if any changed
	- If we got relevant health check updates (checks for our specific service tag, for example)
	  then see if that changes the overall service/node health
	- If it does, try to alert with the latest info for this node/service. At this point we spawn
	  a goroutine to wait for changeThreshold seconds before firing an alert if the status stays
	  stable, and go back to the beginning of 3.

This ensures that only one process can manage the alerts for a node/service at any given time, and
that the check/alert state is persisted across restarts/lock acquisitions.
*/
func watch(opts *WatchOptions) {
	// Set wait time to make the consul query block until an update happens
	client := opts.client
	queryOpts := &api.QueryOptions{
		AllowStale: true,
		WaitTime:   watchWaitTime,
	}

	// Figure out whether we're watching a node or service
	mode := NodeWatch
	diffCheckFunc := diffNodeChecks
	if opts.service != "" {
		mode = ServiceWatch
		diffCheckFunc = diffServiceChecks
	}

	name := mode + " " + opts.node

	// The base path in the consul KV store to keep the state for this watch
	keyPath := alertingKVRoot + "/node/" + opts.node + "/"
	if mode == ServiceWatch {
		name = mode + " " + opts.service
		tagPath := ""
		if opts.tag != "" {
			tagPath = opts.tag + "/"
			name = name + fmt.Sprintf(" (tag: %s)", opts.tag)
		}
		keyPath = alertingKVRoot + "/service/" + opts.service + "/" + tagPath
	}
	lockPath := keyPath + "leader"
	alertPath := keyPath + "alert"

	// Load previously stored check states for this watch from consul
	lastCheckStatus := make(map[string]string)

	// Set the default alert state in case there's no pre-existing state
	alertState := &AlertState{
		Status:  api.HealthPassing,
		Node:    opts.node,
		Service: opts.service,
		Tag:     opts.tag,
	}

	// Set up a callback to be run when we acquire the lock/gain leadership so we can
	// load the last check/alert states
	loadCheckStates := func() {
		storedCheckStates, err := getCheckStates(keyPath, client)

		if err != nil {
			log.Error("Error loading previous check states from consul: ", err)
		}

		for checkName, checkState := range storedCheckStates {
			log.Debugf("Loaded check %s for %s, state: %s", checkName, name, checkState.Status)
			lastCheckStatus[checkName] = checkState.Status
		}

		alert, err := getAlertState(alertPath, client)

		if err != nil {
			log.Error("Error loading previous alert state from consul: ", err)
		}

		if alert != nil {
			alertState = alert
		}
	}

	// Set up the lock this thread will use to determine leader status
	apiLock, err := client.LockKey(lockPath)

	if err != nil {
		log.Fatalf("Error initializing lock for %s: %s", name, err)
	}

	lock := LockHelper{
		target:   name,
		path:     lockPath,
		client:   client,
		lock:     apiLock,
		stopCh:   make(chan struct{}, 1),
		lockCh:   make(chan struct{}, 1),
		callback: loadCheckStates,
	}
	go lock.start()

	log.Debugf("Initialized watch for %s", name)

	// The main loop for the watch, do blocking queries to monitor the state of this service/node
	// and read changes in the health status for potential alerts
	for {
		// Check for shutdown event
		select {
		case <-opts.stopCh:
			log.Infof("Shutting down watch for %s", name)
			lock.stop()
			<-opts.stopCh
			break
		default:
		}

		// Sleep if we don't hold the lock
		if !lock.acquired {
			time.Sleep(1 * time.Second)
			continue
		}

		var checks []*api.HealthCheck
		var queryMeta *api.QueryMeta
		var err error

		// Do a blocking query (a consul watch) for the health checks
		if mode == NodeWatch {
			checks, queryMeta, err = client.Health().Node(opts.node, queryOpts)
		} else {
			checks, queryMeta, err = client.Health().Checks(opts.service, queryOpts)
		}

		// Try again in 10s if we got an error during the blocking request
		if err != nil {
			log.Errorf("Error trying to watch %s: %s, retrying in 10s...", mode, err)
			time.Sleep(errorWaitTime)
			continue
		}

		// Update our WaitIndex for the next query
		queryOpts.WaitIndex = queryMeta.LastIndex

		// Filter out health checks whose statuses haven't changed
		updates := diffCheckFunc(checks, lastCheckStatus, opts)

		// If there's any health check status changes, try to update the remote/local check caches and
		// see if the alert status changed. If it has, we want to start a quiescence timer that will
		// alert if it lives past the changeThreshold
		if len(updates) > 0 {
			success := true

			// Try to write the health updates to consul
			for _, update := range updates {
				log.Debugf("Got health check update for '%s' (%s) for %s", update.HealthCheck.Name, update.Status, name)
				if !updateCheckState(update, client) {
					success = false
				}
			}

			// Update the alert details
			if mode == NodeWatch {
				failingChecks := make([]string, 0)
				for _, check := range checks {
					if check.ServiceID == "" && (check.Status == api.HealthCritical || check.Status == api.HealthWarning) {
						failingChecks = append(failingChecks, check.Name)
					}
				}
				alertState.Details = fmt.Sprintf("Failing checks: %v", failingChecks)
			} else {
				unhealthyNodes := make([]string, 0)
				for _, check := range checks {
					if check.Status == api.HealthCritical || check.Status == api.HealthWarning {
						unhealthyNodes = append(unhealthyNodes, check.Node)
					}
				}
				alertState.Details = fmt.Sprintf("Unhealthy nodes: %v", unhealthyNodes)
			}

			if success {
				for checkHash, update := range updates {
					lastCheckStatus[checkHash] = update.Status
				}

				// If the alert status changed, try to trigger an alert
				newStatus := computeHealth(lastCheckStatus)
				if alertState.Status != newStatus {
					log.Debugf("%s state changed to %s, attempting alert", name, newStatus)
					alertState.Status = newStatus
					alertState.Message = fmt.Sprintf("%s is now %s", name, alertState.Status)
					if setAlertState(alertPath, alertState, client) {
						go tryAlert(alertPath, opts)
					}
				}
			}
		}
	}
}

// Returns a map of checks whose status differs from their entry in lastStatus
func diffServiceChecks(checks []*api.HealthCheck, lastStatus map[string]string, opts *WatchOptions) map[string]CheckUpdate {
	updates := make(map[string]CheckUpdate)

	for _, check := range checks {
		checkHash := check.Node + "/" + check.CheckID
		// Determine whether the check changed status
		if oldStatus, ok := lastStatus[checkHash]; ok && oldStatus != check.Status {
			// If it did, make sure it's for our tag (if specified)
			if opts.tag != "" {
				nodeServices, err := opts.client.Agent().Services()

				if err != nil {
					log.Errorf("Error trying to get service info for node '%s': %s", check.Node, err)
					continue
				}

				if nodeService, ok := nodeServices[opts.service]; ok && contains(nodeService.Tags, opts.tag) {
					updates[checkHash] = CheckUpdate{ServiceTag: opts.tag, HealthCheck: check}
				}
			} else {
				updates[checkHash] = CheckUpdate{HealthCheck: check}
			}
		} else if !ok {
			updates[checkHash] = CheckUpdate{ServiceTag: opts.tag, HealthCheck: check}
		}
	}

	return updates
}

// Returns a map of checks whose status differs from their entry in lastStatus
func diffNodeChecks(checks []*api.HealthCheck, lastStatus map[string]string, opts *WatchOptions) map[string]CheckUpdate {
	updates := make(map[string]CheckUpdate)

	for _, check := range checks {
		checkHash := opts.node + "/" + check.CheckID
		if check.ServiceID == "" {
			// Determine whether the check changed status
			if oldStatus, ok := lastStatus[checkHash]; ok {
				if oldStatus != check.Status {
					updates[checkHash] = CheckUpdate{HealthCheck: check}
				}
			} else {
				updates[checkHash] = CheckUpdate{HealthCheck: check}
			}
		}
	}

	return updates
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
