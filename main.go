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
	config, err := parse(string(dat))
	if err != nil {
		log.Errorf("Error parsing config file: %v", err)
	}

	// Configure Consul client
	clientConfig := api.DefaultConfig()
	clientConfig.Address = config.ConsulAddress

	client, err := api.NewClient(clientConfig)
	if err != nil {
		panic(err)
	}

	if config.DevMode {
		registerTestServices(client)
	}

	// Get services to watch
	services, _, err := client.Catalog().Services(&api.QueryOptions{})

	// Initialize service watches
	for service, tags := range services {
		serviceConfig := config.getServiceConfig(service)

		// Watch each tag separately if the flag is set
		if serviceConfig != nil && len(tags) > 0 && serviceConfig.DistinctTags {
			for _, tag := range tags {
				go WatchService(service, tag, client)
			}
		} else {
			go WatchService(service, "", client)
		}
	}

	// Initialize node watches
	node, err := client.Agent().NodeName()
	if err != nil {
		log.Errorf("Error getting consul node name: %s", err)
	} else {
		go WatchNode(node, client)
	}

	c := make(chan os.Signal, 1)

	signal.Notify(c)

	for sig := range c {
		switch sig {
		case syscall.SIGHUP:
			log.Info("Reloading config")
			// TODO: reload config

		case syscall.SIGINT:
			shutdown(client, config)

		case syscall.SIGTERM:
			shutdown(client, config)

		case syscall.SIGQUIT:
			shutdown(client, config)

		default:
			log.Error("Unknown signal.")
		}
	}
}

func shutdown(client *api.Client, config *Config) {
	log.Info("Got interrupt signal, shutting down")
	if config.DevMode {
		client.Agent().CheckDeregister("memory usage")
		client.Agent().ServiceDeregister("redis")
		client.Agent().ServiceDeregister("nginx")
	}
	os.Exit(0)
}

func registerTestServices(client *api.Client) {
	client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name: "memory usage",
		AgentServiceCheck: api.AgentServiceCheck{
			Script:   "exit $(shuf -i 0-2 -n 1)",
			Interval: "10s",
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
			Interval: "6s",
		},
	})
}
