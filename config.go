package main

import (
	"github.com/hashicorp/hcl"
)

type Config struct {
	ConsulAddress string          `hcl:"consul_address"`
	DevMode       bool            `hcl:"dev_mode"`
	Services      []ServiceConfig `hcl:"service"`
}

type ServiceConfig struct {
	Name            string `hcl:",key"`
	ChangeThreshold int    `hcl:"change_threshold"`
	DistinctTags    bool   `hcl:"distinct_tags"`
}

func parse(config string) (*Config, error) {
	result := &Config{}

	if err := hcl.Decode(&result, config); err != nil {
		return nil, err
	}

	// Set default service config
	for _, service := range result.Services {
		if service.ChangeThreshold == 0 {
			service.ChangeThreshold = 60
		}
	}

	return result, nil
}

func (config *Config) getServiceConfig(name string) *ServiceConfig {
	for _, service := range config.Services {
		if service.Name == name {
			return &service
		}
	}
	return nil
}
