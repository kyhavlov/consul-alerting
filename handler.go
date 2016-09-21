package main

import (
	"fmt"
	"net"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/darkcrux/gopherduty"
	"github.com/hashicorp/consul/api"
	"github.com/nlopes/slack"
	"gopkg.in/gomail.v2"
	"time"
)

// AlertHandlers are responsible for alerting to some external endpoint
// when given an alert (email, pagerduty, etc)
type AlertHandler interface {
	Alert(datacenter string, alert *AlertState)
}

type StdoutHandler struct {
	LogLevel string `mapstructure:"log_level"`
	logger   *log.Logger
}

func (handler StdoutHandler) Alert(datacenter string, alert *AlertState) {
	text := []string{alert.Message}
	if alert.Details != "" {
		text = append(text, strings.Split(alert.Details, "\n")...)
	}
	for _, line := range text {
		switch strings.ToLower(handler.LogLevel) {
		case "panic":
			handler.logger.Panic(line)
		case "fatal":
			handler.logger.Fatal(line)
		case "error":
			handler.logger.Error(line)
		case "warn", "warning":
			handler.logger.Warn(line)
		case "info":
			handler.logger.Info(line)
		case "debug":
			handler.logger.Debug(line)
		}
	}
}

type EmailHandler struct {
	Recipients []string `mapstructure:"recipients"`
	MaxRetries int      `mapstructure:"max_retries"`
}

func (handler EmailHandler) Alert(datacenter string, alert *AlertState) {
	for _, recipient := range handler.Recipients {
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

		tries := 0
		for tries <= handler.MaxRetries {
			if err := d.DialAndSend(m); err != nil {
				log.Error("Error sending alert email: ", err)
				log.Error("Retrying email in 5s...")
				time.Sleep(5 * time.Second)
				tries++
			} else {
				break
			}
		}
	}
}

type PagerdutyHandler struct {
	ServiceKey string `mapstructure:"service_key"`
	MaxRetries int    `mapstructure:"max_retries"`
}

func (handler PagerdutyHandler) Alert(datacenter string, alert *AlertState) {
	client := gopherduty.NewClient(handler.ServiceKey)
	client.MaxRetry = handler.MaxRetries

	// This key needs to be unique to the datacenter and service/node we're alerting on
	incidentKey := datacenter + "-" + alert.Service + "-" + alert.Tag + "-" + alert.Node

	if alert.Status != api.HealthPassing {
		client.Trigger(incidentKey, alert.Message, "", "", alert.Details)
	} else {
		client.Resolve(incidentKey, alert.Message, alert.Details)
	}
}

type SlackHandler struct {
	Token       string `mapstructure:"api_token"`
	ChannelName string `mapstructure:"channel_name"`
	MaxRetries  int    `mapstructure:"max_retries"`
}

const slackMessageFormat = `
*%s*
%s
`

func (handler SlackHandler) Alert(datacenter string, alert *AlertState) {
	api := slack.New(handler.Token)
	message := fmt.Sprintf(slackMessageFormat, alert.Message, alert.Details)
	tries := 0

	for tries <= handler.MaxRetries {
		_, _, err := api.PostMessage(handler.ChannelName, message, slack.PostMessageParameters{})

		if err != nil {
			log.Errorf("Error sending alert to Slack (channel: %s): %s", handler.ChannelName, err)
			log.Errorf("Retrying alert to slack in 5s...")
			time.Sleep(5 * time.Second)
		} else {
			break
		}

		tries++
	}
}
