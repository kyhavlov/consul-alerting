package main

import (
	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
	"time"
	"fmt"
)

func watch(service string, tag string, client *api.Client) {
	queryOpts := &api.QueryOptions{
		WaitTime: 10 * time.Minute,
	}

	checks := make(map[string]map[string]string)

	tagDisplay := ""
	if tag != "" {
		tagDisplay = fmt.Sprintf(" (tag: %s)", tag)
	}

	log.Infof("Starting watch for service: %s%s", service, tagDisplay)

	for {
		services, queryMeta, err := client.Health().Service(service, tag, false, queryOpts)

		if err != nil {
			log.Errorf("Error trying to watch service: %s, retrying in 10s...", err)
			time.Sleep(10 * time.Second)
			continue
		}

		queryOpts.WaitIndex = queryMeta.LastIndex

		//log.Debugf("Got watch return for service %s with len %v", service, len(services))

		for _, serviceEntry := range services {
			if _, ok := checks[serviceEntry.Service.ID]; !ok {
				checks[serviceEntry.Service.ID] = make(map[string]string)
			}

			for _, check := range serviceEntry.Checks {
				if oldStatus, ok := checks[serviceEntry.Service.ID][check.CheckID]; ok && oldStatus != check.Status {
					log.Infof("Check '%s' in service '%s'%s on node %s is now %s",
						check.CheckID, serviceEntry.Service.ID, tagDisplay, serviceEntry.Node.Node, check.Status)
				}
				checks[serviceEntry.Service.ID][check.CheckID] = check.Status
			}
		}
	}
}
