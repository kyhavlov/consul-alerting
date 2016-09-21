package main

import (
	"fmt"
	"os"
	"testing"

	log "github.com/Sirupsen/logrus"
	"github.com/Sirupsen/logrus/hooks/test"
	"github.com/nlopes/slack"
)

func TestHandler_stdout(t *testing.T) {
	logger, hook := test.NewNullLogger()
	handler := StdoutHandler{"warn", logger}

	detail1 := "detail line 1"
	detail2 := "detail line 2"

	alert := &AlertState{
		Message: "service is failing",
		Details: detail1 + "\n" + detail2,
	}
	handler.Alert("", alert)

	if len(hook.Entries) != 3 {
		t.Errorf("expected %d lines of output, got %d", 3, len(hook.Entries))
	}

	if hook.Entries[0].Message != alert.Message {
		t.Errorf("expected message line '%s', got '%s'", alert.Message, hook.Entries[0])
	}

	if hook.Entries[1].Message != detail1 || hook.Entries[2].Message != detail2 {
		t.Errorf("expected detail lines: '%s', '%s'; got: '%s', '%s'", detail1, detail2,
			hook.Entries[1].Message, hook.Entries[2].Message)
	}

	if hook.LastEntry().Level != log.WarnLevel {
		t.Errorf("expected loglevel %s, got %s", log.WarnLevel, hook.LastEntry().Level)
	}
}

func TestHandler_slack(t *testing.T) {
	token := os.Getenv("TEST_SLACK_TOKEN")
	if token == "" {
		t.Skip("TEST_SLACK_TOKEN not set, skipping")
	}

	channel := os.Getenv("TEST_SLACK_CHANNEL")
	if token == "" {
		t.Skip("TEST_SLACK_CHANNEL not set, skipping")
	}

	handler := SlackHandler{
		Token:       token,
		ChannelName: channel,
	}
	hook := new(test.Hook)
	log.AddHook(hook)

	detail1 := "detail line 1"
	detail2 := "detail line 2"

	alert := &AlertState{
		Message: "service is failing",
		Details: detail1 + "\n" + detail2,
	}
	handler.Alert("", alert)

	api := slack.New(token)
	groups, err := api.GetGroups(true)
	if err != nil {
		t.Fatal(err)
	}

	id := ""
	for _, group := range groups {
		if group.Name == channel {
			id = group.ID
		}
	}

	history, err := api.GetGroupHistory(id, slack.HistoryParameters{
		Count: 1,
	})

	if err != nil {
		t.Fatal(err)
	}

	expected := fmt.Sprintf(slackMessageFormat, alert.Message, alert.Details)

	if history.Messages[0].Text != expected {
		t.Errorf("expected `%s`, got `%s`", expected, history.Messages[0].Text)
	}
}
