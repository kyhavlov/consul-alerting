package main

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

// Spawns watches for services, adding more when new services are discovered
func discoverServices(nodeName string, config *Config, shutdownCh chan struct{}, client *api.Client) {
	if config.ServiceWatch == GlobalMode {
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
	stopCh := make(chan struct{}, 0)

	// Loop indefinitely to run the watch, doing repeated blocking queries to Consul
	for {
		// Check for shutdown event
		select {
		case <-shutdownCh:
			log.Infof("Shutting down service watches (count: %d)...", len(services))
			for _, _ = range services {
				stopCh <- struct{}{}
				stopCh <- struct{}{}
			}
			log.Info("Finished shutting down service watches")
			<-shutdownCh
			return
		default:
		}

		var queryMeta *api.QueryMeta
		currentServices := make(map[string][]string)
		var err error

		// Watch either all services or just the local node's, depending on whether GlobalMode is set
		if config.ServiceWatch == GlobalMode {
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

		// Compare the new list of services with our stored one to see if we need to
		// spawn any new watches
		for service, tags := range currentServices {
			serviceConfig := config.serviceConfig(service)

			// If DistinctTags is specified, spawn a separate watch for each tag on the service
			if serviceConfig != nil && serviceConfig.DistinctTags {
				for _, tag := range tags {
					if _, ok := services[service + ":" + tag]; !ok && !contains(serviceConfig.IgnoredTags, tag) {
						watchOpts := &WatchOptions{
							service: service,
							tag:     tag,
							config:  config,
							client:  client,
							stopCh:  stopCh,
						}
						services[service + ":" + tag] = true
						go watch(watchOpts)
					}
				}
			} else {
				if _, ok := services[service]; !ok {
					watchOpts := &WatchOptions{
						service: service,
						config:  config,
						client:  client,
						stopCh:  stopCh,
					}
					services[service] = true
					go watch(watchOpts)
				}
			}
		}
	}
}

// Queries the catalog for nodes and starts watches for them
func discoverNodes(config *Config, shutdownCh chan struct{}, client *api.Client) {
	queryOpts := &api.QueryOptions{
		AllowStale: true,
		WaitTime:   watchWaitTime,
	}

	// Used to store nodes we've already started watches for
	nodes := make(map[string]bool, 0)

	// Share a stop channel among watches for faster shutdown
	stopCh := make(chan struct{}, 0)

	// Loop indefinitely to run the watch, doing repeated blocking queries to Consul
	for {
		// Check for shutdown event
		select {
		case <-shutdownCh:
			log.Infof("Shutting down node watches (count: %d)...", len(nodes))
			for _, _ = range nodes {
				stopCh <- struct{}{}
				stopCh <- struct{}{}
			}
			log.Info("Finished shutting down node watches")
			<-shutdownCh
			return
		default:
		}
		currentNodes, queryMeta, err := client.Catalog().Nodes(queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch node list: %s, retrying in 10s...", err)
			time.Sleep(errorWaitTime)
			continue
		}

		// Update our WaitIndex for the next query
		queryOpts.WaitIndex = queryMeta.LastIndex

		// Compare the new list of nodes with our stored one to see if we need to
		// spawn any new watches
		for _, node := range currentNodes {
			nodeName := node.Node
			if _, ok := nodes[nodeName]; !ok {
				log.Infof("Discovered new node: %s", nodeName)
				opts := &WatchOptions{
					node:   nodeName,
					config: config,
					client: client,
					stopCh: stopCh,
				}
				nodes[nodeName] = true
				go watch(opts)
			}
		}
	}
}
