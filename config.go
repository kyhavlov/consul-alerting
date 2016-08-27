package main

import (
	"fmt"
	"io/ioutil"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/hcl"
	"github.com/hashicorp/hcl/hcl/ast"
	"github.com/mitchellh/mapstructure"
)

const LocalMode = "local"
const GlobalMode = "global"

type Config struct {
	ConsulAddress   string `mapstructure:"consul_address"`
	ConsulToken     string `mapstructure:"token"`
	DevMode         bool   `mapstructure:"dev_mode"`
	NodeWatch       string `mapstructure:"node_watch"`
	ServiceWatch    string `mapstructure:"service_watch"`
	ChangeThreshold int    `mapstructure:"change_threshold"`
	LogLevel        string `mapstructure:"log_level"`

	Services map[string]ServiceConfig
	Handlers map[string]AlertHandler
}

type ServiceConfig struct {
	Name            string
	ChangeThreshold int      `mapstructure:"change_threshold"`
	DistinctTags    bool     `mapstructure:"distinct_tags"`
	IgnoredTags     []string `mapstructure:"ignored_tags"`
}

// Parses a given file path for config and returns a Config object and an array
// of AlertHandlers
func ParseConfigFile(path string) (*Config, error) {
	// Read the file contents
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Error loading config file: %s", err)
	}
	raw := string(bytes)

	return ParseConfig(raw)
}

func DefaultConfig() *Config {
	config, _ := ParseConfig("{}")
	return config
}

// Parses the given config string and returns a Config object and an array
// of AlertHandlers
func ParseConfig(raw string) (*Config, error) {
	// Parse the file (could be HCL or JSON)
	root, err := hcl.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("error parsing: %s", err)
	}

	// Top-level item should be a list
	list, ok := root.Node.(*ast.ObjectList)
	if !ok {
		return nil, fmt.Errorf("error parsing: root should be an object")
	}

	list = list.Children()

	// Decode the full thing into a map[string]interface for ease
	var config Config
	var m map[string]interface{}
	if err := hcl.DecodeObject(&m, list); err != nil {
		return nil, err
	}
	delete(m, "service")
	delete(m, "handler")

	// Set defaults
	defaultConfig := map[string]interface{}{
		"consul_address":   "localhost:8500",
		"node_watch":       "local",
		"service_watch":    "local",
		"change_threshold": 60,
		"log_level":        "info",
	}
	for k, v := range defaultConfig {
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}

	// Decode the rest
	if err := mapstructure.WeakDecode(&m, &config); err != nil {
		return nil, err
	}

	config.Services = make(map[string]ServiceConfig)
	if obj := list.Filter("service"); len(obj.Items) > 0 {
		err = parseServices(obj, &config)
		if err != nil {
			return nil, err
		}
	}

	config.Handlers = make(map[string]AlertHandler)
	if obj := list.Filter("handler"); len(obj.Items) > 0 {
		err = parseHandlers(obj, &config)
		if err != nil {
			return nil, err
		}
	}

	// Validate config
	validWatchModes := []string{LocalMode, GlobalMode}

	if !contains(validWatchModes, config.NodeWatch) {
		return nil, fmt.Errorf("Invalid value for node_watch: %s", config.NodeWatch)
	}

	if !contains(validWatchModes, config.ServiceWatch) {
		return nil, fmt.Errorf("Invalid value for service_watch: %s", config.ServiceWatch)
	}

	return &config, nil
}

// Parse the raw service objects into the config
func parseServices(list *ast.ObjectList, config *Config) error {
	config.Services = make(map[string]ServiceConfig)

	for _, s := range list.Items {
		name := s.Keys[0].Token.Value().(string)

		var m map[string]interface{}
		var service ServiceConfig
		if err := hcl.DecodeObject(&m, s.Val); err != nil {
			return err
		}

		if _, ok := m["change_threshold"]; !ok {
			m["change_threshold"] = config.ChangeThreshold
		}

		if err := mapstructure.WeakDecode(m, &service); err != nil {
			return err
		}

		service.Name = name
		config.Services[name] = service
	}

	return nil
}

// Parse the raw handler objects into the config
func parseHandlers(list *ast.ObjectList, config *Config) error {
	config.Handlers = make(map[string]AlertHandler)

	defaultConfig := map[string]map[string]interface{}{
		"stdout": map[string]interface{}{
			"log_level": "warn",
		},
		"pagerduty": map[string]interface{}{
			"max_retries": 5,
		},
	}

	for _, s := range list.Items {
		if len(s.Keys) < 2 {
			return fmt.Errorf("didn't specify type/name for handler at line %d", s.Pos().Line)
		}
		handlerType := s.Keys[0].Token.Value().(string)
		name := s.Keys[1].Token.Value().(string)

		var m map[string]interface{}
		if err := hcl.DecodeObject(&m, s.Val); err != nil {
			return err
		}

		// Set defaults
		if _, ok := defaultConfig[handlerType]; ok {
			for key, val := range defaultConfig[handlerType] {
				if _, ok := m[key]; !ok {
					m[key] = val
				}
			}
		}

		switch handlerType {
		case "stdout":
			var handler StdoutHandler
			if err := mapstructure.WeakDecode(m, &handler); err != nil {
				return err
			}
			config.Handlers[name] = handler
		case "email":
			var handler EmailHandler
			if err := mapstructure.WeakDecode(m, &handler); err != nil {
				return err
			}
			config.Handlers[name] = handler
		case "pagerduty":
			var handler PagerdutyHandler
			if err := mapstructure.WeakDecode(m, &handler); err != nil {
				return err
			}
			config.Handlers[name] = handler
		default:
			return fmt.Errorf("Unknown handler type: %s", handlerType)
		}

		log.Infof("Loaded %s handler: %s", handlerType, name)
	}

	return nil
}

func (config *Config) getServiceConfig(name string) *ServiceConfig {
	if service, ok := config.Services[name]; ok {
		return &service
	} else {
		return nil
	}
}

func (c *Config) getServiceHandlers(service string) []AlertHandler {
	handlers := make([]AlertHandler, 0)
	for _, handler := range c.Handlers {
		handlers = append(handlers, handler)
	}
	return handlers
}
