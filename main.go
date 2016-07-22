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

	makeTestServices(client)

	// Get services to watch
	services, _, err := client.Catalog().Services(&api.QueryOptions{})

	// Initialize watches
	for service, tags := range services {
		serviceConfig := config.getServiceConfig(service)

		// Watch each tag separately if the flag is set
		if serviceConfig != nil && len(tags) > 0 && serviceConfig.DistinctTags {
			for _, tag := range tags {
				go watch(service, tag, client)
			}
		} else {
			go watch(service, "", client)
		}
	}

	c := make(chan os.Signal, 1)

	signal.Notify(c)

	for sig := range c {
		switch sig {
		case syscall.SIGHUP:
			log.Info("Reloading config")
			// TODO: reload config

		case syscall.SIGINT:
			shutdown(client)

		case syscall.SIGTERM:
			shutdown(client)

		case syscall.SIGQUIT:
			shutdown(client)

		default:
			log.Error("Unknown signal.")
		}
	}
}

func shutdown(client *api.Client) {
	log.Info("Got interrupt signal, shutting down")
	os.Exit(0)
}

func makeTestServices(client *api.Client) {
	// Register ourselves as a service
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
