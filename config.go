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
	ConsulAddress    string   `mapstructure:"consul_address"`
	ConsulToken      string   `mapstructure:"consul_token"`
	ConsulDatacenter string   `mapstructure:"datacenter"`
	DevMode          bool     `mapstructure:"dev_mode"`
	NodeWatch        string   `mapstructure:"node_watch"`
	ServiceWatch     string   `mapstructure:"service_watch"`
	ChangeThreshold  int      `mapstructure:"change_threshold"`
	DefaultHandlers  []string `mapstructure:"default_handlers"`
	LogLevel         string   `mapstructure:"log_level"`

	Services map[string]ServiceConfig
	Handlers map[string]AlertHandler
}

type ServiceConfig struct {
	Name            string
	ChangeThreshold int      `mapstructure:"change_threshold"`
	DistinctTags    bool     `mapstructure:"distinct_tags"`
	IgnoredTags     []string `mapstructure:"ignored_tags"`
	Handlers        []string `mapstructure:"handlers"`
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
	config, _ := ParseConfig(`
	handler "stdout" "default" {
		loglevel = "warn"
	}
	`)
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

	// Decode the full thing into a map[string]interface for ease of use
	var config Config
	var m map[string]interface{}
	if err := hcl.DecodeObject(&m, list); err != nil {
		return nil, err
	}
	delete(m, "service")
	delete(m, "handler")

	// Set defaults for unset keys
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

	// Decode the simple (non service/handler) objects into Config
	if err := mapstructure.WeakDecode(&m, &config); err != nil {
		return nil, err
	}

	// Use parser function for service blocks
	config.Services = make(map[string]ServiceConfig)
	if obj := list.Filter("service"); len(obj.Items) > 0 {
		err = parseServices(obj, &config)
		if err != nil {
			return nil, err
		}
	}

	// Use parser function for handler blocks
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
		"email": map[string]interface{}{
			"max_retries": 5,
		},
		"pagerduty": map[string]interface{}{
			"max_retries": 5,
		},
		"slack": map[string]interface{}{
			"max_retries": 5,
		},
	}

	for _, s := range list.Items {
		if len(s.Keys) < 2 {
			return fmt.Errorf("didn't specify type/name for handler at line %d", s.Pos().Line)
		}
		handlerType := s.Keys[0].Token.Value().(string)
		name := s.Keys[1].Token.Value().(string)
		id := handlerType + "." + name

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

		// Decode based on the handler type.
		// TODO: look into a more compact way to do this when we have more handlers
		switch handlerType {
		case "stdout":
			var handler StdoutHandler
			if err := mapstructure.WeakDecode(m, &handler); err != nil {
				return err
			}
			handler.logger = log.StandardLogger()
			config.Handlers[id] = handler
		case "email":
			var handler EmailHandler
			if err := mapstructure.WeakDecode(m, &handler); err != nil {
				return err
			}
			config.Handlers[id] = handler
		case "pagerduty":
			var handler PagerdutyHandler
			if err := mapstructure.WeakDecode(m, &handler); err != nil {
				return err
			}
			config.Handlers[id] = handler
		case "slack":
			var handler SlackHandler
			if err := mapstructure.WeakDecode(m, &handler); err != nil {
				return err
			}
			config.Handlers[id] = handler
		default:
			return fmt.Errorf("Unknown handler type: %s", handlerType)
		}

		log.Infof("Loaded handler: %s", id)
	}

	return nil
}

func (config *Config) serviceConfig(service string) *ServiceConfig {
	if s, ok := config.Services[service]; ok {
		return &s
	} else {
		return nil
	}
}

// Loads the configured alert handlers for a given service, filtering if applicable
func (c *Config) serviceHandlers(service string) []AlertHandler {
	handlers := make([]AlertHandler, 0)
	filters := make([]string, 0)
	serviceConfig := c.serviceConfig(service)
	if serviceConfig != nil {
		filters = serviceConfig.Handlers
	}
	if len(filters) == 0 {
		filters = c.DefaultHandlers
	}
	for name, handler := range c.Handlers {
		if len(filters) == 0 || contains(filters, name) {
			handlers = append(handlers, handler)
		}
	}
	return handlers
}

// Compute the changeThreshold for alerts on a service, defaulting to the global threshold
// if no config for the service is specified
func (c *Config) serviceChangeThreshold(service string) int {
	changeThreshold := c.ChangeThreshold

	// Override the global changeThreshold config if we have a service-specific one
	if c.serviceConfig(service) != nil {
		changeThreshold = c.serviceConfig(service).ChangeThreshold
	}

	return changeThreshold
}
