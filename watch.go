package main

import (
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

const watchWaitTime = 15 * time.Second
const errorWaitTime = 10 * time.Second

type WatchOptions struct {
	node            string
	service         string
	tag             string
	changeThreshold time.Duration
	diffCheckFunc   func(checks []*api.HealthCheck, lastStatus map[string]string, opts *WatchOptions) map[string]CheckUpdate
	client          *api.Client
	handlers        []AlertHandler
	stopCh          chan struct{}
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

	mode := NodeWatch
	if opts.service != "" {
		mode = ServiceWatch
	}

	name := mode + " " + opts.node

	// The base path in the consul KV store to keep the state for this watch
	keyPath := "service/consul-alerting/node/" + opts.node + "/"
	if mode == ServiceWatch {
		name = mode + " " + opts.service
		tagPath := ""
		if opts.tag != "" {
			tagPath = opts.tag + "/"
			name = name + fmt.Sprintf(" (tag: %s)", opts.tag)
		}
		keyPath = "service/consul-alerting/service/" + opts.service + "/" + tagPath
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
		updates := opts.diffCheckFunc(checks, lastCheckStatus, opts)

		// If there's any health check status changes, try to update the remote/local check caches and
		// see if the alert status changed
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
		} else {
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

type CheckUpdate struct {
	ServiceTag string
	*api.HealthCheck
}

// Spawns watches for services, adding more when new services are discovered
func discoverServices(nodeName string, config *Config, handlers []AlertHandler, shutdownOpts *ShutdownOpts, client *api.Client) {
	queryOpts := &api.QueryOptions{
		AllowStale: true,
		WaitTime:   watchWaitTime,
	}

	// Used to store services we've already started watches for
	services := make(map[string][]string)

	for {
		var queryMeta *api.QueryMeta
		currentServices := make(map[string][]string)
		var err error

		// Watch either all services or just the local node's, depending on whether GlobalMode is set
		if config.GlobalMode {
			currentServices, queryMeta, err = client.Catalog().Services(queryOpts)
		} else {
			var node *api.CatalogNode
			node, queryMeta, err = client.Catalog().Node(nodeName, queryOpts)
			if err == nil {
				// Build the map of service:[tags]
				for _, config := range node.Services {
					if _, ok := currentServices[config.Service]; ok {
						currentServices[config.Service] = config.Tags
					} else {
						currentServices[config.Service] = append(currentServices[config.Service], config.Tags...)
					}
				}
			}
		}

		if err != nil {
			log.Errorf("Error trying to watch services: %s, retrying in 10s...", err)
			time.Sleep(errorWaitTime)
			continue
		}

		// Update our WaitIndex for the next query
		queryOpts.WaitIndex = queryMeta.LastIndex

		for service, tags := range currentServices {
			serviceConfig := config.getServiceConfig(service)

			// Override the global changeThreshold config if we have a service-specific one
			changeThreshold := config.ChangeThreshold
			if serviceConfig != nil {
				changeThreshold = serviceConfig.ChangeThreshold
			}

			// See if we found a new service
			if _, ok := services[service]; !ok {
				log.Infof("Service found: %s, tags: %v", service, tags)
				services[service] = tags

				// Create a watch for each tag if DistinctTags is set
				if serviceConfig != nil && len(tags) > 0 && serviceConfig.DistinctTags {
					for _, tag := range tags {
						watchOpts := &WatchOptions{
							service:         service,
							tag:             tag,
							changeThreshold: time.Duration(changeThreshold),
							diffCheckFunc:   diffServiceChecks,
							client:          client,
							handlers:        handlers,
							stopCh:          shutdownOpts.stopCh,
						}
						shutdownOpts.count++
						go watch(watchOpts)
					}
				} else {
					// If it isn't, just start one watch for the service
					watchOpts := &WatchOptions{
						service:         service,
						changeThreshold: time.Duration(changeThreshold),
						diffCheckFunc:   diffServiceChecks,
						client:          client,
						handlers:        handlers,
						stopCh:          shutdownOpts.stopCh,
					}
					shutdownOpts.count++
					go watch(watchOpts)
				}
			} else {
				// Check for new, non-ignored tags if DistinctTags is set
				if serviceConfig != nil && len(tags) > 0 && serviceConfig.DistinctTags {
					services[service] = tags

					for _, tag := range tags {
						if !contains(serviceConfig.IgnoredTags, tag) && !contains(services[service], tag) {
							go watch(&WatchOptions{
								service:         service,
								tag:             tag,
								changeThreshold: time.Duration(changeThreshold),
								diffCheckFunc:   diffServiceChecks,
								client:          client,
								handlers:        handlers,
								stopCh:          shutdownOpts.stopCh,
							})
							shutdownOpts.count++
						}
					}
				}
			}
		}
	}
}

// Queries the catalog for nodes and starts watches for them
func discoverNodes(config *Config, handlers []AlertHandler, shutdownOpts *ShutdownOpts, client *api.Client) {
	queryOpts := &api.QueryOptions{
		AllowStale: true,
		WaitTime:   watchWaitTime,
	}

	// Used to store nodes we've already started watches for
	nodes := make([]string, 0)

	for {
		currentNodes, queryMeta, err := client.Catalog().Nodes(queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch node list: %s, retrying in 10s...", err)
			time.Sleep(errorWaitTime)
			continue
		}

		// Update our WaitIndex for the next query
		queryOpts.WaitIndex = queryMeta.LastIndex

		for _, node := range currentNodes {
			nodeName := node.Node
			if !contains(nodes, nodeName) {
				log.Infof("Discovered new node: %s", nodeName)
				opts := &WatchOptions{
					node:            nodeName,
					changeThreshold: time.Duration(config.ChangeThreshold),
					diffCheckFunc:   diffNodeChecks,
					client:          client,
					handlers:        handlers,
				}
				if config.GlobalMode {
					opts.stopCh = shutdownOpts.stopCh
					shutdownOpts.count++
				}
				nodes = append(nodes, nodeName)
				go watch(opts)
			}
		}
	}
}

// Starts the discovery of nodes/services, depending on how GlobalMode is set
func initializeWatches(nodeName string, config *Config, handlers []AlertHandler, shutdownOpts *ShutdownOpts, client *api.Client) {

	if config.GlobalMode {
		log.Info("Running in global mode, monitoring all nodes/services")
		go discoverServices(nodeName, config, handlers, shutdownOpts, client)
		go discoverNodes(config, handlers, shutdownOpts, client)
	} else {
		log.Infof("Running in local mode, monitoring node %s's services", nodeName)
		go discoverServices(nodeName, config, handlers, shutdownOpts, client)

		// We don't need to discover the local node, it won't change
		opts := &WatchOptions{
			node:            nodeName,
			changeThreshold: time.Duration(config.ChangeThreshold),
			diffCheckFunc:   diffNodeChecks,
			client:          client,
			handlers:        handlers,
			stopCh:          shutdownOpts.stopCh,
		}
		shutdownOpts.count++
		go watch(opts)
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
