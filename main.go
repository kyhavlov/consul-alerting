package main

import (
	"os"
	"syscall"

	"github.com/hashicorp/consul/api"
	log "github.com/Sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

func main() {
	// Set up logging
	formatter := new(prefixed.TextFormatter)
	formatter.ForceColors = true

	log.SetFormatter(formatter)
	log.SetLevel(log.DebugLevel)

	// Configure client
	config := api.DefaultConfig()
	config.Address = "linux-server:8500"

	client, err := api.NewClient(config)
	if err != nil {
		panic(err)
	}

	// Registry ourselves as a service
	client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "consu",
	})

	client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "redis",
		Tags: []string{"alpha", "beta"},
		Port: 2000,
		Check: &api.AgentServiceCheck{
			Script: "exit $((RANDOM % 3))",
			Interval: "10s",
		},
	})

	c := make(chan os.Signal, 1)

	go watch(client)

	for signal := range c {
		switch signal {
		case syscall.SIGHUP:
			log.Info("Reloading config")

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
	client.Agent().ServiceDeregister("consu")
	os.Exit(0)
}