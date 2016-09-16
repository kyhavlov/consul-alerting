package main

import (
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"sync"
)

// Maximum time to wait for a blocking (watch) query to Consul
const watchWaitTime = 10 * time.Second

// Time to wait before retrying after getting an api error from Consul
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

	// The config to use for the watch
	config *Config

	// The Consul client object to use for making requests
	client *api.Client

	// A lock to use for avoiding race conditions with quiescence timers when alerting
	alertLock *sync.Mutex

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
2. Upon acquiring the lock, read the previous checks/alert state from the Consul K/V store into the local cache
3. While we have the lock, loop through the following:
	- Do a blocking query for up to watchWaitTime to get new health check updates
	- Compare the returned health checks to the local cache to see if any changed
	- If we got relevant health check updates (checks for our specific service tag, for example)
	  see if they affect the overall service/node health
	- If they do, try to alert with the latest info for this node/service. At this point we spawn
	  a goroutine to wait for changeThreshold seconds before firing an alert if the status stays
	  stable, and go back to the beginning of #3.

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

	// Initialize the mutex used for locking alert state
	opts.alertLock = &sync.Mutex{}

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
	lastAlertStatus := api.HealthPassing

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
	}

	// Set up the lock this thread will use to determine leader status
	apiLock, err := client.LockKey(lockPath)

	if err != nil {
		log.Fatalf("Error initializing lock for %s: %s", name, err)
	}

	lock := LockHelper{
		target:   name,
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
			lock.stop()
			<-opts.stopCh
			return
		default:
		}

		// Sleep and continue until we hold the lock
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
		// see if the alert status changed. If it has, we start a quiescence timer that will alert if
		// it lives past the changeThreshold
		if len(updates) > 0 {
			success := true

			// Try to write the health updates to consul
			for _, update := range updates {
				log.Debugf("Got health check update for '%s' (%s) for %s", update.HealthCheck.Name, update.Status, name)
				if !updateCheckState(update, client) {
					success = false
				}
			}

			// Update the alert details to include info about any failing checks
			alert := AlertState{}
			if mode == NodeWatch {
				alert.Details = nodeDetails(checks)
			} else {
				alert.Details = serviceDetails(checks)
			}

			if success {
				for checkHash, update := range updates {
					lastCheckStatus[checkHash] = update.Status
				}

				// If the alert status changed, try to trigger an alert
				newStatus := computeHealth(lastCheckStatus)
				if lastAlertStatus != newStatus {
					lastAlertStatus = newStatus
					alert.Status = newStatus
					alert.Message = fmt.Sprintf("[%s] %s is now %s", opts.config.ConsulDatacenter, name, newStatus)
					go tryAlert(alertPath, alert, opts)
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
				node, _, err := opts.client.Catalog().Node(check.Node, &api.QueryOptions{})

				if err != nil {
					log.Errorf("Error trying to get service info for node '%s': %s", check.Node, err)
					continue
				}

				if nodeService, ok := node.Services[opts.service]; ok && contains(nodeService.Tags, opts.tag) {
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
