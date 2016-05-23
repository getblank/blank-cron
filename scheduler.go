package main

import (
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/robfig/cron"
)

const (
	uriRun = "cron.run"
)

var (
	storeSchedulers = map[string]*cron.Cron{}
	locker          sync.Mutex
)

func updateScheduler(storeName string, tasks []task) {
	locker.Lock()
	defer locker.Unlock()
	if currentScheduler, ok := storeSchedulers[storeName]; ok {
		currentScheduler.Stop()
		delete(storeSchedulers, storeName)
	}
	if len(tasks) == 0 {
		return
	}
	c := cron.New()
	for i := range tasks {
		t := tasks[i]
		t.index = i
		c.AddFunc(t.Schedule, func() {
			runTask(storeName, t.index)
		})
		log.WithFields(log.Fields{"store": storeName, "taskIndex": i, "schedule": t.Schedule}).Debug("Scheduled task")
	}
	c.Start()
	storeSchedulers[storeName] = c
}

func runTask(storeName string, index int) {
	tqLocker.RLock()
	client := tqClient
	tqLocker.RUnlock()
	if client == nil {
		log.Warn("Can't run task because no connection to TaskQ")
		return
	}
	res, err := client.Call(uriRun, storeName, index)
	log.WithFields(log.Fields{"res": res, "err": err, "store": storeName, "index": index}).Debug("Running scheduled task result")
}
