package main

import (
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

// Spawns watches for services, adding more when new services are discovered
func discoverServices(nodeName string, config *Config, shutdownCh chan struct{}, client *api.Client) {
	if config.ServiceScope == GlobalMode {
		log.Info("Discovering services from catalog")
	} else {
		log.Infof("Discovering services on local node (%s)", nodeName)
	}

	queryOpts := &api.QueryOptions{
		AllowStale: true,
		WaitTime:   watchWaitTime,
	}

	// Used to store services we've already started watches for
	services := make(map[string]bool)

	// Share a stop channel among watches for faster shutdown
	stopCh := make(map[string]chan struct{})

	// Loop indefinitely to run the watch, doing repeated blocking queries to Consul
	for {
		// Check for shutdown event
		select {
		case <-shutdownCh:
			log.Infof("Shutting down service watches (count: %d)...", len(services))

			// Use a wait group to shut down all the watches at the same time
			var wg sync.WaitGroup
			for service, _ := range services {
				wg.Add(1)
				ch := stopCh[service]
				go func() {
					defer wg.Done()
					ch <- struct{}{}
					ch <- struct{}{}
				}()
			}
			wg.Wait()
			log.Info("Finished shutting down service watches")
			<-shutdownCh
			return
		default:
		}

		var queryMeta *api.QueryMeta
		currentServices := make(map[string][]string)
		var err error

		// Watch either all services or just the local node's, depending on whether GlobalMode is set
		if config.ServiceScope == GlobalMode {
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

		// Reset the map so we can detect removed services
		for service, _ := range services {
			services[service] = false
		}

		// Compare the new list of services with our stored one to see if we need to
		// spawn any new watches
		for service, tags := range currentServices {
			serviceConfig := config.serviceConfig(service)

			// If DistinctTags is specified, spawn a separate watch for each tag on the service
			if serviceConfig != nil && serviceConfig.DistinctTags {
				for _, tag := range tags {
					if _, ok := services[service+":"+tag]; !ok && !contains(serviceConfig.IgnoredTags, tag) {
						watchOpts := &WatchOptions{
							service: service,
							tag:     tag,
							config:  config,
							client:  client,
							stopCh:  make(chan struct{}, 0),
						}
						stopCh[service+":"+tag] = watchOpts.stopCh
						log.Infof("Discovered new service: %s (tag: %s)", service, tag)
						go watch(watchOpts)
					}
					services[service+":"+tag] = true
				}
			} else {
				if _, ok := services[service]; !ok {
					watchOpts := &WatchOptions{
						service: service,
						config:  config,
						client:  client,
						stopCh:  make(chan struct{}, 0),
					}
					stopCh[service] = watchOpts.stopCh
					log.Infof("Discovered new service: %s", service)
					go watch(watchOpts)
				}
				services[service] = true
			}
		}

		// Shut down watched for removed services
		for service, alive := range services {
			if !alive {
				log.Infof("Service %s left, removing", service)

				ch := stopCh[service]
				delete(services, service)
				delete(stopCh, service)
				go func() {
					ch <- struct{}{}
					ch <- struct{}{}
				}()
			}
		}
	}
}

// Queries the local agent for nodes and starts watches for them
func discoverNodes(nodeName string, config *Config, shutdownCh chan struct{}, client *api.Client) {
	// Used to store nodes we've already started watches for
	nodes := make(map[string]bool, 0)

	// Share a stop channel among watches for faster shutdown
	stopCh := make(map[string]chan struct{})

	index := 0

	// Loop indefinitely to run the watch, doing repeated blocking queries to Consul
	for {
		// Check for shutdown event
		select {
		case <-shutdownCh:
			log.Infof("Shutting down node watches (count: %d)...", len(nodes))

			// Use a wait group to shut down all the watches at the same time
			var wg sync.WaitGroup
			for node, _ := range nodes {
				wg.Add(1)
				ch := stopCh[node]
				go func() {
					defer wg.Done()
					ch <- struct{}{}
					ch <- struct{}{}
				}()
			}
			wg.Wait()
			log.Info("Finished shutting down node watches")

			<-shutdownCh
			return
		default:
		}

		// Get the local agent's list of all nodes in the cluster
		members, err := client.Agent().Members(false)

		if err != nil {
			log.Errorf("Error querying node list: %s, retrying in 10s...", err)
			time.Sleep(errorWaitTime)
			continue
		}

		// If our node's position in the list changed, find it again
		if len(members) >= index || members[index].Name != nodeName {
			for i, m := range members {
				if m.Name == nodeName {
					index = i
					break
				}
			}
		}

		currentNodes := selectWatchedNodes(index, config.nodesWatchedCount,config.nodesWatchedPercent, members)

		// Reset the map so we can detect removed nodes
		for node, _ := range nodes {
			nodes[node] = false
		}

		// Compare the new list of nodes with our stored one to see if we need to
		// spawn any new watches
		for _, node := range currentNodes {
			if _, ok := nodes[node]; !ok {
				log.Infof("Discovered new node: %s", node)
				opts := &WatchOptions{
					node:   node,
					config: config,
					client: client,
					stopCh: make(chan struct{}, 0),
				}
				stopCh[node] = opts.stopCh
				go watch(opts)
			}
			nodes[node] = true
		}

		// Shut down watches for removed nodes
		for node, alive := range nodes {
			if !alive {
				log.Infof("Node %s left, removing", node)

				ch := stopCh[node]
				delete(nodes, node)
				delete(stopCh, node)
				go func() {
					ch <- struct{}{}
					ch <- struct{}{}
				}()
			}
		}

		time.Sleep(5*time.Second)
	}
}

// Pick the next N nodes starting at index to monitor, ignoring those in 'left' state
func selectWatchedNodes(index int, max int, percentage bool, members []*api.AgentMember) []string {
	currentNodes := make([]string, 0)
	currentIndex := index
	count := 0

	maxNodes := max
	if percentage {
		maxNodes = (len(members)*100)/max
	}

	for count < maxNodes {
		if currentIndex == len(members) {
			currentIndex = 0
		}

		// Ignore nodes in the 'left' (3) state, consul leaves them in the member list for a while
		if members[currentIndex].Status != 3 {
			currentNodes = append(currentNodes, members[currentIndex].Name)
			count++
		}
		currentIndex++

		// If we looped through the whole list, exit
		if currentIndex == index {
			break
		}
	}

	return currentNodes
}