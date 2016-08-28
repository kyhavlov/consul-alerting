package main

import (
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
)

func TestConfig_missingFile(t *testing.T) {
	_, err := ParseConfigFile(path.Join(os.TempDir(), "nonexistant.json"))
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expected := "no such file or directory"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to include %q", err.Error(), expected)
	}
}

func TestConfig_correctValues(t *testing.T) {
	configString := `
	consul_address = "localhost:8500"
	token = "test_token"

	node_watch = "local"
	service_watch = "global"

	change_threshold = 30
	default_handlers = ["stdout.warn", "email.admin"]

	log_level = "warn"

	service "redis" {
		change_threshold = 15
		distinct_tags = true
		ignored_tags = ["seed", "node"]
	}

	service "webapp" {
		handlers = ["email.admin"]
	}

	handler "stdout" "warn" {
		log_level = "warn"
	}

	handler "email" "admin" {
		recipients = ["admin@example.com"]
	}

	handler "pagerduty" "page_ops" {
		service_key = "asdf1234"
		max_retries = 10
	}
	`

	config, err := ParseConfig(configString)
	if err != nil {
		t.Fatal(err)
	}

	expected := &Config{
		ConsulAddress:   "localhost:8500",
		ConsulToken:     "test_token",
		NodeWatch:       "local",
		ServiceWatch:    "global",
		ChangeThreshold: 30,
		DefaultHandlers: []string{"stdout.warn", "email.admin"},
		LogLevel:        "warn",
		Services: map[string]ServiceConfig{
			"redis": ServiceConfig{
				Name:            "redis",
				ChangeThreshold: 15,
				DistinctTags:    true,
				IgnoredTags:     []string{"seed", "node"},
			},
			"webapp": ServiceConfig{
				Name:            "webapp",
				ChangeThreshold: 30,
				Handlers:        []string{"email.admin"},
			},
		},
		Handlers: map[string]AlertHandler{
			"stdout.warn": StdoutHandler{
				LogLevel: "warn",
			},
			"email.admin": EmailHandler{
				Recipients: []string{"admin@example.com"},
			},
			"pagerduty.page_ops": PagerdutyHandler{
				ServiceKey: "asdf1234",
				MaxRetries: 10,
			},
		},
	}

	if !reflect.DeepEqual(config, expected) {
		t.Fatalf("expected \n%#v\n\n, got \n\n%#v\n\n", expected, config)
	}

	if len(config.Handlers) != len(expected.Handlers) {
		t.Fatalf("expected %d handlers, got %d", len(expected.Handlers), len(config.Handlers))
	}
}

func TestConfig_defaultHandlers(t *testing.T) {
	config := &Config{
		DefaultHandlers: []string{"stdout.warn"},
		Handlers: map[string]AlertHandler{
			"stdout.warn": StdoutHandler{
				LogLevel: "warn",
			},
		},
	}

	handlers := config.serviceHandlers("")

	if len(handlers) != len(config.Handlers) {
		t.Fatalf("expected %d handlers, got %d", len(config.Handlers), len(handlers))
	}

	if !reflect.DeepEqual(config.Handlers["stdout.warn"], handlers[0]) {
		t.Fatalf("expected \n%#v\n\n, got \n\n%#v\n\n", config.Handlers["stdout.warn"], config)
	}
}

func TestConfig_serviceHandlers(t *testing.T) {
	config := &Config{
		Services: map[string]ServiceConfig{
			"webapp": ServiceConfig{
				Name:     "webapp",
				Handlers: []string{"stdout.warn"},
			},
		},
		Handlers: map[string]AlertHandler{
			"stdout.warn": StdoutHandler{
				LogLevel: "warn",
			},
		},
	}

	handlers := config.serviceHandlers("webapp")

	if len(handlers) != len(config.Handlers) {
		t.Fatalf("expected %d handlers, got %d", len(config.Handlers), len(handlers))
	}

	if !reflect.DeepEqual(config.Handlers["stdout.warn"], handlers[0]) {
		t.Fatalf("expected \n%#v\n\n, got \n\n%#v\n\n", config.Handlers["stdout.warn"], config)
	}
}
