package main

import (
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

func main() {
	// Set up logging
	formatter := new(prefixed.TextFormatter)
	formatter.ForceColors = true

	log.SetFormatter(formatter)
	log.SetLevel(log.DebugLevel)

	// Load config
	dat, err := ioutil.ReadFile("test.hcl")
	if err != nil {
		log.Errorf("Error loading config file: %v", err)
	}

	// Parse config
	config, handlers, err := parse(string(dat))
	if err != nil {
		log.Errorf("Error parsing config file: %v", err)
	}

	// Set log level
	level, err := log.ParseLevel(config.LogLevel)
	if err != nil {
		level = log.InfoLevel
	}
	log.SetLevel(level)

	// Configure Consul client
	clientConfig := api.DefaultConfig()
	clientConfig.Address = config.ConsulAddress

	client, err := api.NewClient(clientConfig)
	if err != nil {
		log.Fatal("Error initializing client: ", err)
	}

	if config.DevMode {
		registerTestServices(client)
	}

	// Get services to watch
	services := make(map[string][]string)
	if config.GlobalMode {
		log.Info("Discovering services to watch from catalog")
		services, _, err = client.Catalog().Services(&api.QueryOptions{})
		if err != nil {
			log.Fatal("Error initializing services: ", err)
		}
	} else {
		log.Info("Discovering services to watch on local agent")
		serviceMap, err := client.Agent().Services()
		if err != nil {
			log.Fatal("Error initializing services: ", err)
		}
		for _, config := range serviceMap {
			services[config.Service] = config.Tags
		}
	}
	log.Infof("Services: %v", services)

	shutdownOpts := &ShutdownOpts{
		stopCh: make(chan struct{}, 0),
	}

	// Initialize service watches
	for service, tags := range services {
		serviceConfig := config.getServiceConfig(service)

		// Watch each tag separately if the flag is set
		if serviceConfig != nil && len(tags) > 0 && serviceConfig.DistinctTags {
			for _, tag := range tags {
				if !contains(serviceConfig.IgnoredTags, tag) {
					go WatchService(service, tag, &WatchOptions{
						changeThreshold: serviceConfig.ChangeThreshold,
						client:          client,
						handlers:        handlers,
						stopCh:          shutdownOpts.stopCh,
					})
					shutdownOpts.count++
				}
			}
		} else {
			go WatchService(service, "", &WatchOptions{
				changeThreshold: config.ChangeThreshold,
				client:          client,
				handlers:        handlers,
				stopCh:          shutdownOpts.stopCh,
			})
			shutdownOpts.count++
		}
	}

	// Get nodes to watch
	nodes := make([]string, 0)
	if config.GlobalMode {
		log.Info("Discovering nodes to watch from catalog")
		allNodes, _, err := client.Catalog().Nodes(&api.QueryOptions{})
		if err == nil {
			for _, node := range allNodes {
				nodes = append(nodes, node.Node)
			}
		} else {
			log.Errorf("Error getting nodes from catalog: %s", err)
		}
	} else {
		log.Info("Watching local node")
		node, err := client.Agent().NodeName()
		if err == nil {
			nodes = append(nodes, node)
		} else {
			log.Errorf("Error getting local node name: %s", err)
		}
	}
	log.Infof("Nodes: %v", nodes)

	// Initialize node watches
	for _, node := range nodes {
		go WatchNode(node, &WatchOptions{
			changeThreshold: config.ChangeThreshold,
			client:          client,
			handlers:        handlers,
		})
	}

	// Set up signal handling for graceful shutdown
	c := make(chan os.Signal, 1)

	signal.Notify(c)

	for sig := range c {
		switch sig {
		case syscall.SIGHUP:
			log.Info("Reloading config")
			// TODO: reload config

		case syscall.SIGINT:
			shutdown(client, config, shutdownOpts)

		case syscall.SIGTERM:
			shutdown(client, config, shutdownOpts)

		case syscall.SIGQUIT:
			shutdown(client, config, shutdownOpts)

		default:
			log.Error("Unknown signal.")
		}
	}
}

type ShutdownOpts struct {
	stopCh chan struct{}
	count  int
}

func shutdown(client *api.Client, config *Config, opts *ShutdownOpts) {
	log.Info("Got interrupt signal, shutting down")
	if config.DevMode {
		client.Agent().CheckDeregister("memory usage")
		client.Agent().ServiceDeregister("redis")
		client.Agent().ServiceDeregister("nginx")
	}

	for i := 0; i < opts.count*2; i++ {
		opts.stopCh <- struct{}{}
	}

	os.Exit(0)
}

func registerTestServices(client *api.Client) {
	client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name: "memory usage",
		AgentServiceCheck: api.AgentServiceCheck{
			Script:   "exit $(shuf -i 0-2 -n 1)",
			Interval: "20s",
		},
	})

	client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "redis",
		Tags: []string{"alpha", "beta"},
		Port: 2000,
		Check: &api.AgentServiceCheck{
			Script:   "exit $(shuf -i 0-2 -n 1)",
			Interval: "10s",
		},
	})

	client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "nginx",
		Tags: []string{"gamma", "delta"},
		Port: 3000,
		Check: &api.AgentServiceCheck{
			Script:   "exit $(shuf -i 0-2 -n 1)",
			Interval: "8s",
		},
	})
}
