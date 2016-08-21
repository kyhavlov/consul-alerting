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
	text := fmt.Sprintf("%s (%s)", alert.Message, alert.Details)
	switch strings.ToLower(s.LogLevel) {
	case "panic":
		log.Panic(text)
	case "fatal":
		log.Fatal(text)
	case "error":
		log.Error(text)
	case "warn", "warning":
		log.Warn(text)
	case "info":
		log.Info(text)
	case "debug":
		log.Debug(text)
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

		m.SetHeader("Subject", alert.Message)
		m.SetBody("text/plain", alert.Details)

		d := gomail.NewPlainDialer(records[0].Host, 25, "", "")

		if err := d.DialAndSend(m); err != nil {
			log.Error(err)
		}
	}
}
