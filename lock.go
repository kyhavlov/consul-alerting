package main

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/hashicorp/consul/api"
)

const lockWaitTime = 15 * time.Second

// LockHelper is a struct to help with acquiring and holding a Consul lock
type LockHelper struct {
	// The name of the service/node being fought over for the lock
	target string

	// Consul client object to use for making lock API calls
	client *api.Client

	// The Lock object to use for acquisition
	lock *api.Lock

	// A channel used for interrupting the start() loop
	stopCh chan struct{}

	// A channel used for interrupting the lock acquisition
	lockCh chan struct{}

	// A function to be run after acquiring the lock
	callback func()

	// Indicates whether we currently hold the lock
	acquired bool
}

// Try to acquire the lock if we don't have it, and then block until we lose it
func (l *LockHelper) start() {
	shutdown := false
	for !shutdown {
		select {
		case <-l.stopCh:
			shutdown = true
		default:
			log.Debugf("Waiting to acquire lock on %s...", l.target)

			// Lock() returns an interrupt channel on success that can be used to block until we lose the lock
			intChan, err := l.lock.Lock(l.lockCh)

			if intChan != nil {
				// Run the callback to update check states before setting acquired to true
				l.callback()
				l.acquired = true
				log.Infof("Acquired lock for %s", l.target)

				<-intChan

				l.acquired = false
				log.Infof("Lost lock for %s", l.target)
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

// Shut down the lock acquisition loop, which will cause the lock to get released if it's currently acquired
func (l *LockHelper) stop() {
	l.stopCh <- struct{}{}
	l.lockCh <- struct{}{}
	l.lock.Unlock()
	l.lock.Destroy()
	l.acquired = false
}
