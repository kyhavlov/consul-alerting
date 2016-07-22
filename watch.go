package main

import (
	"github.com/hashicorp/consul/api"
	log "github.com/Sirupsen/logrus"
)

func watch(client *api.Client) {
	queryOpts := &api.QueryOptions{}

	checks := make(map[string]string)

	for {
		services, queryMeta, err := client.Health().Service("redis", "", false, queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch service: %s", err)
		}

		queryOpts.WaitIndex = queryMeta.LastIndex

		for _, serviceEntry := range services {
			log.Debugf("Got update for service %s", serviceEntry.Service.ID)
			for _, check := range serviceEntry.Checks {
				if oldStatus, ok := checks[check.CheckID]; ok && oldStatus != check.Status {
					log.Debugf("Check '%s' is now %s", check.CheckID, check.Status)
				}
				checks[check.CheckID] = check.Status
			}
		}
	}
}