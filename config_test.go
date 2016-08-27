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
	log_level = "warn"

	service "redis" {
		change_threshold = 15
		distinct_tags = true
		ignored_tags = ["seed", "node"]
	}

	handler "stdout" "log" {
		log_level = "error"
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
		LogLevel:        "warn",
		Services: map[string]ServiceConfig{
			"redis": ServiceConfig{
				Name:            "redis",
				ChangeThreshold: 15,
				DistinctTags:    true,
				IgnoredTags:     []string{"seed", "node"},
			},
		},
		Handlers: map[string]AlertHandler{
			"log": StdoutHandler{
				LogLevel: "error",
			},
			"admin": EmailHandler{
				Recipients: []string{"admin@example.com"},
			},
			"page_ops": PagerdutyHandler{
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
