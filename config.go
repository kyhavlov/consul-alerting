package main

import (
	"fmt"
	"io/ioutil"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/hcl"
)

type Config struct {
	ConsulAddress   string `hcl:"consul_address"`
	DevMode         bool   `hcl:"dev_mode"`
	GlobalMode      bool   `hcl:"global_mode"`
	ChangeThreshold int    `hcl:"change_threshold"`

	LogLevel string `hcl:"log_level"`

	Services []ServiceConfig `hcl:"service"`
	Handlers HandlerConfig   `hcl:"handlers"`
}

type ServiceConfig struct {
	Name            string   `hcl:",key"`
	ChangeThreshold int      `hcl:"change_threshold"`
	DistinctTags    bool     `hcl:"distinct_tags"`
	IgnoredTags     []string `hcl:"ignored_tags"`
}

type HandlerConfig struct {
	StdoutHandler `hcl:"stdout"`
	EmailHandler  `hcl:"email"`
}

// Reads the config file at the given path and returns a Config object and an array
// of AlertHandlers
func ParseConfig(path string) (*Config, []AlertHandler, error) {
	// Read the file contents
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("Error loading config file: %s", err)
	}
	raw := string(bytes)

	config := &Config{}

	if err := hcl.Decode(&config, raw); err != nil {
		return nil, nil, err
	}

	// Set default global config
	if config.ConsulAddress == "" {
		config.ConsulAddress = "localhost:8500"
	}

	if config.ChangeThreshold == 0 {
		config.ChangeThreshold = 60
	}

	if config.LogLevel == "" {
		config.LogLevel = "INFO"
	}

	// Set default service config
	for _, service := range config.Services {
		if service.ChangeThreshold == 0 {
			service.ChangeThreshold = config.ChangeThreshold
		}
	}

	// Configure alert handlers
	handlers := make([]AlertHandler, 0)

	if config.Handlers.StdoutHandler.Enabled {
		if config.Handlers.StdoutHandler.LogLevel == "" {
			config.Handlers.StdoutHandler.LogLevel = "warn"
		}
		_, err = log.ParseLevel(config.Handlers.StdoutHandler.LogLevel)
		if err != nil {
			return nil, nil, fmt.Errorf("Error parsing loglevel %s: %s", config.Handlers.StdoutHandler.LogLevel, err)
		}
		log.Infof("Handler 'stdout' enabled with loglevel %s", config.Handlers.StdoutHandler.LogLevel)
		handlers = append(handlers, config.Handlers.StdoutHandler)
	}

	if config.Handlers.EmailHandler.Enabled {
		log.Info("Handler 'email' enabled")
		handlers = append(handlers, config.Handlers.EmailHandler)
	}

	return config, handlers, nil
}

func (config *Config) getServiceConfig(name string) *ServiceConfig {
	for _, service := range config.Services {
		if service.Name == name {
			return &service
		}
	}
	return nil
}
