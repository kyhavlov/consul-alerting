package main

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

// Spawns watches for services, adding more when new services are discovered
func discoverServices(nodeName string, config *Config, shutdownOpts *ShutdownOpts, client *api.Client) {
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
	services := make(map[string][]string)

	// Loop indefinitely to run the watch, doing repeated blocking queries to Consul
	for {
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

			// See if we found a new service
			if _, ok := services[service]; !ok {
				log.Infof("Service found: %s, tags: %v", service, tags)
				services[service] = tags

				// Create a watch for each tag if DistinctTags is set
				if serviceConfig != nil && len(tags) > 0 && serviceConfig.DistinctTags {
					for _, tag := range tags {
						if !contains(serviceConfig.IgnoredTags, tag) {
							watchOpts := &WatchOptions{
								service: service,
								tag:     tag,
								config:  config,
								client:  client,
								stopCh:  shutdownOpts.stopCh,
							}
							shutdownOpts.count++
							go watch(watchOpts)
						}
					}
				} else {
					// If it isn't, just start one watch for the service
					watchOpts := &WatchOptions{
						service: service,
						config:  config,
						client:  client,
						stopCh:  shutdownOpts.stopCh,
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
								service: service,
								tag:     tag,
								config:  config,
								client:  client,
								stopCh:  shutdownOpts.stopCh,
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
func discoverNodes(config *Config, shutdownOpts *ShutdownOpts, client *api.Client) {
	queryOpts := &api.QueryOptions{
		AllowStale: true,
		WaitTime:   watchWaitTime,
	}

	// Used to store nodes we've already started watches for
	nodes := make([]string, 0)

	// Loop indefinitely to run the watch, doing repeated blocking queries to Consul
	for {
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
			if !contains(nodes, nodeName) {
				log.Infof("Discovered new node: %s", nodeName)
				opts := &WatchOptions{
					node:   nodeName,
					config: config,
					client: client,
					stopCh: shutdownOpts.stopCh,
				}
				shutdownOpts.count++
				nodes = append(nodes, nodeName)
				go watch(opts)
			}
		}
	}
}
