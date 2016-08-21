package main

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

const lockWaitTime = 15 * time.Second

type LockHelper struct {
	target   string
	path     string
	client   *api.Client
	lock     *api.Lock
	stopCh   chan struct{}
	lockCh   chan struct{}
	callback func()
	acquired bool
}

func (l *LockHelper) start() {
	clean := false
	for !clean {
		select {
		case <-l.stopCh:
			clean = true
		default:
			log.Infof("Waiting to acquire lock on %s...", l.target)
			intChan, err := l.lock.Lock(l.lockCh)
			if intChan != nil {
				log.Infof("Acquired lock for %s alerts", l.target)
				l.callback()
				l.acquired = true
				<-intChan
				l.acquired = false
				log.Infof("Lost lock for %s alerts", l.target)
				l.lock.Unlock()
				l.lock.Destroy()
			} else {
				if err != nil {
					log.Warnf("Error getting lock for %s: %s", l.target, err)
				}
				time.Sleep(lockWaitTime)
			}
		}
	}
}

func (l *LockHelper) stop() {
	l.stopCh <- struct{}{}
	l.lockCh <- struct{}{}
	l.lock.Unlock()
	l.lock.Destroy()
	l.acquired = false
}
