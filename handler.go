package main

import (
	"fmt"
	"gopkg.in/gomail.v2"
	"net"
	"strings"

	log "github.com/Sirupsen/logrus"
)

// AlertHandlers are responsible for alerting to some external endpoint
// when given an alert (email, pagerduty, etc)
type AlertHandler interface {
	Alert(*AlertState)
}

type StdoutHandler struct {
	Enabled  bool   `hcl:"enabled"`
	LogLevel string `hcl:"log_level"`
}

func (s StdoutHandler) Alert(alert *AlertState) {
	switch strings.ToLower(s.LogLevel) {
	case "panic":
		log.Panic(alert.Message)
	case "fatal":
		log.Fatal(alert.Message)
	case "error":
		log.Error(alert.Message)
	case "warn", "warning":
		log.Warn(alert.Message)
	case "info":
		log.Info(alert.Message)
	case "debug":
		log.Debug(alert.Message)
	}
}

type EmailHandler struct {
	Enabled    bool     `hcl:"enabled"`
	Server     string   `hcl:"domain"`
	Recipients []string `hcl:"recipients"`
}

func (e EmailHandler) Alert(alert *AlertState) {
	for _, recipient := range e.Recipients {
		// Get the mail server to use for this recipient
		records, err := net.LookupMX(strings.Split(recipient, "@")[1])
		if err != nil {
			log.Error("Error looking up email server: ", err)
			continue
		}

		m := gomail.NewMessage()
		m.SetAddressHeader("From", "consul-alerting@noreply.com", "Consul Alerting")
		m.SetAddressHeader("To", recipient, "")

		if alert.Service == "" {
			m.SetHeader("Subject", fmt.Sprintf("Node %s is now %s", alert.Node, alert.Status))
		} else {
			service := alert.Service
			if alert.Tag != "" {
				service = fmt.Sprintf("%s (%s)", service, alert.Tag)
			}
			m.SetHeader("Subject", fmt.Sprintf("Service %s is now %s", service, alert.Status))
		}
		m.SetBody("text/plain", alert.Message)

		d := gomail.NewPlainDialer(records[0].Host, 25, "", "")

		if err := d.DialAndSend(m); err != nil {
			log.Error(err)
		}
	}
}
