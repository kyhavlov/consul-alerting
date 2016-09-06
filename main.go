package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

const usage = `Usage: consul-alerting [--help] [options]

Options:

    -config=<path>    Sets the path to a configuration file on disk.
`

func init() {
	// Set up logging
	formatter := new(prefixed.TextFormatter)
	formatter.ForceColors = true

	log.SetFormatter(formatter)
	log.SetLevel(log.DebugLevel)
}

func main() {
	// Parse command line options
	var config_path string
	var help bool
	flag.StringVar(&config_path, "config", "", "")
	flag.BoolVar(&help, "help", false, "")
	flag.Parse()

	if help {
		fmt.Print(usage)
		os.Exit(0)
	}

	// Load the configuration
	var config *Config

	if config_path != "" {
		var err error
		config, err = ParseConfigFile(config_path)
		if err != nil {
			log.Fatal(err)
			os.Exit(2)
		}
	} else {
		config = DefaultConfig()
	}

	// Set log level
	level, err := log.ParseLevel(config.LogLevel)
	if err != nil {
		log.Errorf("Error setting loglevel '%s': %s", level, err)
		os.Exit(2)
	}
	log.SetLevel(level)

	// Initialize Consul client
	clientConfig := api.DefaultConfig()
	clientConfig.Address = config.ConsulAddress
	addressSplit := strings.Split(config.ConsulAddress, "://")
	if len(addressSplit) > 1 {
		clientConfig.Address = addressSplit[1]
		clientConfig.Scheme = addressSplit[0]
	}
	clientConfig.Token = config.ConsulToken

	log.Infof("Using Consul agent at %s", clientConfig.Address)
	client, err := api.NewClient(clientConfig)
	if err != nil {
		log.Fatal("Error initializing client: ", err)
	}
	var nodeName string
	for {
		nodeName, err = client.Agent().NodeName()
		if err == nil {
			break
		}
		log.Error("Error connecting to Consul agent: ", err)
		log.Error("Retrying in 10s...")
		time.Sleep(10 * time.Second)
	}

	// Get datacenter info if it wasn't specified in the config
	if config.ConsulDatacenter == "" {
		agentInfo, err := client.Agent().Self()

		for err != nil {
			agentInfo, err = client.Agent().Self()
			log.Error("Error fetching datacenter from Consul: ", err)
			log.Error("Retrying in 10s...")
			time.Sleep(10 * time.Second)
		}

		config.ConsulDatacenter = agentInfo["Config"]["Datacenter"].(string)
	}
	log.Info("Using datacenter: ", config.ConsulDatacenter)

	if config.DevMode {
		registerTestServices(client)
	}

	shutdownOpts := &ShutdownOpts{
		stopCh: make(chan struct{}, 0),
	}

	go discoverServices(nodeName, config, shutdownOpts, client)

	// If NodeWatch is set to global mode, monitor the catalog for new nodes
	if config.NodeWatch == GlobalMode {
		log.Info("Discovering nodes from catalog")
		go discoverNodes(config, shutdownOpts, client)
	} else {
		log.Infof("Monitoring local node (%s)'s checks", nodeName)
		// We're in local mode so we don't need to discover the local node; it won't change
		opts := &WatchOptions{
			node:   nodeName,
			config: config,
			client: client,
			stopCh: shutdownOpts.stopCh,
		}
		shutdownOpts.count++
		go watch(opts)
	}

	// Set up signal handling for graceful shutdown
	c := make(chan os.Signal, 1)

	signal.Notify(c)

	for sig := range c {
		switch sig {
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

// Used to shutdown gracefully by releasing any held locks
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

	log.Info("Releasing locks...")
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
			Interval: "8s",
		},
	})
}
