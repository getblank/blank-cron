package main

import (
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/robfig/cron"
)

const (
	uriRun = "cron.run"
)

var (
	storeSchedulers = map[string]*cron.Cron{}
	locker          sync.RWMutex
	runningTasks    = map[string]map[int]struct{}{}
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

	runningTasks[storeName] = map[int]struct{}{}

	c := cron.New()
	for i := range tasks {
		t := tasks[i]
		t.index = i
		err := c.AddFunc(t.Schedule, func() {
			runTask(storeName, t.index, t.AllowConcurrent)
		})
		if err != nil {
			log.WithFields(log.Fields{"store": storeName, "taskIndex": i, "schedule": t.Schedule, "error": err}).Error("Can't add sheduled task")
			continue
		}
		log.WithFields(log.Fields{"store": storeName, "taskIndex": i, "schedule": t.Schedule}).Debug("Scheduled task added to cron")
	}
	c.Start()
	storeSchedulers[storeName] = c
}

func runTask(storeName string, index int, allowConcurrent bool) {
	log.WithFields(log.Fields{"store": storeName, "index": index}).Debug("Time to run scheduled task")

	if !canRunTask(storeName, index, allowConcurrent) {
		log.WithFields(log.Fields{"store": storeName, "index": index}).Debug("Concurrent is not allowed. Prev task is not completed.")
		return
	}

	tqLocker.RLock()
	client := tqClient
	tqLocker.RUnlock()

	if client == nil {
		log.Warn("Can't run task because no connection to TaskQ")
		return
	}

	if !checkAndMarkTaskRunning(storeName, index) {
		log.WithFields(log.Fields{"store": storeName, "index": index}).Debug("Concurrent is not allowed. Can't start task because prev task is not completed.")
	}

	log.WithFields(log.Fields{"store": storeName, "index": index}).Debug("Starting scheduled task")
	res, err := client.Call(uriRun, storeName, index)
	markTaskCompleted(storeName, index)
	log.WithFields(log.Fields{"res": res, "error": err, "store": storeName, "index": index}).Debug("Running scheduled task result")
}

func canRunTask(storeName string, index int, allowConcurrent bool) bool {
	if allowConcurrent {
		return true
	}

	locker.RLock()
	_, ok := runningTasks[storeName][index]
	locker.RUnlock()

	return !ok
}

// if returns true, task can be run
func checkAndMarkTaskRunning(storeName string, index int) bool {
	locker.Lock()
	defer locker.Unlock()

	_, ok := runningTasks[storeName][index]
	if ok {
		return false
	}

	runningTasks[storeName][index] = struct{}{}
	return true
}

func markTaskCompleted(storeName string, index int) {
	locker.Lock()
	delete(runningTasks[storeName], index)
	locker.Unlock()
}
