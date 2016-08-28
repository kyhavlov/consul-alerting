package main

import (
	"fmt"
	"net"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/bluele/slack"
	"github.com/darkcrux/gopherduty"
	"github.com/hashicorp/consul/api"
	"gopkg.in/gomail.v2"
)

// AlertHandlers are responsible for alerting to some external endpoint
// when given an alert (email, pagerduty, etc)
type AlertHandler interface {
	Alert(*AlertState)
}

type StdoutHandler struct {
	LogLevel string `mapstructure:"log_level"`
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
	Recipients []string `mapstructure:"recipients"`
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

type PagerdutyHandler struct {
	ServiceKey string `mapstructure:"service_key"`
	MaxRetries int    `mapstructure:"max_retries"`
}

func (p PagerdutyHandler) Alert(alert *AlertState) {
	client := gopherduty.NewClient(p.ServiceKey)
	client.MaxRetry = p.MaxRetries
	incidentKey := alert.Service + "-" + alert.Tag + "-" + alert.Node
	if alert.Status != api.HealthPassing {
		client.Trigger(incidentKey, alert.Message, "", "", alert.Details)
	} else {
		client.Resolve(incidentKey, alert.Message, alert.Details)
	}
}

type SlackHandler struct {
	Token       string `mapstructure:"api_token"`
	ChannelName string `mapstructure:"channel_name"`
}

const slackMessageFormat = `
*%s*
%s
`

func (p SlackHandler) Alert(alert *AlertState) {
	api := slack.New(p.Token)
	err := api.ChatPostMessage(p.ChannelName, fmt.Sprintf(slackMessageFormat, alert.Message, alert.Details), nil)

	if err != nil {
		log.Errorf("Error sending alert to Slack (channel: %s): %s", p.ChannelName, err)
	}
}
