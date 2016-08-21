package main

import (
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/consul-template/test"
)

func TestParseConfig_missingFile(t *testing.T) {
	_, _, err := ParseConfig(path.Join(os.TempDir(), "nonexistant.json"))
	if err == nil {
		t.Fatal("expected error, but nothing was returned")
	}

	expected := "no such file or directory"
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected %q to include %q", err.Error(), expected)
	}
}

func TestParseConfig_correctValues(t *testing.T) {
	configFile := test.CreateTempfile([]byte(`
	consul_address = "localhost:8500"
	global_mode = true
	change_threshold = 30
	log_level = "warn"

	service "redis" {
		change_threshold = 15
		distinct_tags = true
		ignored_tags = ["seed", "node"]
	}

	handlers {
		stdout {
			enabled = true
			log_level = "error"
		}
		email {
			enabled = false
			recipients = ["admin@example.com"]
		}
	}
	`), t)
	defer test.DeleteTempfile(configFile, t)

	config, handlers, err := ParseConfig(configFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	expected := &Config{
		ConsulAddress:   "localhost:8500",
		GlobalMode:      true,
		ChangeThreshold: 30,
		LogLevel:        "warn",
		Services: []ServiceConfig{
			ServiceConfig{
				Name:            "redis",
				ChangeThreshold: 15,
				DistinctTags:    true,
				IgnoredTags:     []string{"seed", "node"},
			},
		},
		Handlers: HandlerConfig{
			StdoutHandler{
				Enabled: true,
				LogLevel: "error",
			},
			EmailHandler{
				Enabled:    false,
				Recipients: []string{"admin@example.com"},
			},
		},
	}
	expectedHandlers := []AlertHandler{
		StdoutHandler{true},
	}

	if !reflect.DeepEqual(config, expected) {
		t.Fatalf("expected \n%#v\n\n, got \n\n%#v\n\n", expected, config)
	}

	if len(handlers) != 1 {
		t.Fatalf("expected %d handlers, got %d", 1, len(handlers))
	}

	if !reflect.DeepEqual(handlers, expectedHandlers) {
		t.Fatalf("expected \n%#v\n\n, got \n\n%#v\n\n", expectedHandlers, handlers)
	}
}
