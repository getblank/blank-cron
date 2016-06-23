package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/getblank/wango"
	"github.com/spf13/cobra"
)

var (
	buildTime string
	gitHash   string
	version   = "0.0.10"
)

var (
	srAddress *string
	tqAddress string
	srClient  *wango.Wango
	srLocker  sync.RWMutex
	tqClient  *wango.Wango
	tqLocker  sync.RWMutex
)

type service struct {
	Type    string `json:"type"`
	Address string `json:"address"`
	Port    string `json:"port"`
}

type task struct {
	Schedule string `json:"schedule"`
	index    int
}

func main() {
	if os.Getenv("BLANK_DEBUG") != "" {
		log.SetLevel(log.DebugLevel)
	}
	var verFlag *bool
	rootCmd := &cobra.Command{
		Use:   "router",
		Short: "Router for Blank platform",
		Long:  "The next generation of web applications. This is the router subsytem.",
		Run: func(cmd *cobra.Command, args []string) {
			if *verFlag {
				printVersion()
				return
			}
			log.Info("Router started")
			go connectToTaskQ()
			connectToSr()
		},
	}
	srAddress = rootCmd.PersistentFlags().StringP("service-registry", "s", "ws://localhost:1234", "Service registry uri")
	verFlag = rootCmd.PersistentFlags().BoolP("version", "v", false, "Prints version and exit")

	if err := rootCmd.Execute(); err != nil {
		println(err.Error())
		os.Exit(-1)
	}

}

func printVersion() {
	fmt.Printf("Build time:  		%s\n", buildTime)
	fmt.Printf("Commit hash: 		%s\n", gitHash)
	fmt.Printf("Version:     		%s\n", version)
}

func connectedToSR(w *wango.Wango) {
	log.Info("Connected to SR: ", *srAddress)
	srLocker.Lock()
	srClient = w
	srLocker.Unlock()

	srClient.Call("register", map[string]interface{}{"type": "cron"})
	srClient.Subscribe("config", configUpdateHandler)
	srClient.Subscribe("registry", registryUpdateHandler)
}

func connectToSr() {
	var reconnectChan = make(chan struct{})
	for {
		log.Info("Attempt to connect to SR: ", *srAddress)
		client, err := wango.Connect(*srAddress, "http://getblank.net")
		if err != nil {
			log.Warn("Can'c connect to service registry: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		client.SetSessionCloseCallback(func(c *wango.Conn) {
			srLocker.Lock()
			srClient = nil
			srLocker.Unlock()
			reconnectChan <- struct{}{}
		})
		connectedToSR(client)
		<-reconnectChan
	}
}

func connectedToTaskQ(w *wango.Wango) {
	log.Info("Connected to TaskQ: ", tqAddress)
	tqLocker.Lock()
	tqClient = w
	tqLocker.Unlock()
}

func connectToTaskQ() {
	for {
		tqLocker.RLock()
		addr := tqAddress
		tqLocker.RUnlock()
		if addr != "" {
			break
		}
	}
	var reconnectChan = make(chan struct{})
	for {
		log.Info("Attempt to connect to TaskQ: ", tqAddress)
		client, err := wango.Connect(tqAddress, "http://getblank.net")
		if err != nil {
			log.Warn("Can'c connect to taskq: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		client.SetSessionCloseCallback(func(c *wango.Conn) {
			log.Warn("Disconnected from TaskQ")
			tqLocker.Lock()
			tqClient = nil
			tqLocker.Unlock()
			reconnectChan <- struct{}{}
		})
		connectedToTaskQ(client)
		<-reconnectChan
	}
}

func reconnectToTaskQ() {
	tqLocker.RLock()
	defer tqLocker.RUnlock()
	if tqClient != nil {
		tqClient.Disconnect()
	}
}

func configUpdateHandler(_ string, _event interface{}) {
	log.Debug("Config update has arrived")
	conf, ok := _event.(map[string]interface{})
	if !ok {
		log.Warn("Invalid format of config")
		return
	}
	for storeName, _store := range conf {
		store, ok := _store.(map[string]interface{})
		if !ok {
			log.Warn("Invalid format of store")
			return
		}
		tasksInterface, ok := store["tasks"]
		if !ok || tasksInterface == nil {
			continue
		}
		encoded, err := json.Marshal(tasksInterface)
		if err != nil {
			log.WithFields(log.Fields{"error": err, "store": storeName}).Error("Can't marshal tasks")
			continue
		}
		var tasks []task
		err = json.Unmarshal(encoded, &tasks)
		if err != nil {
			log.WithFields(log.Fields{"error": err, "store": storeName}).Error("Can't unmarshal tasks")
			continue
		}
		updateScheduler(storeName, tasks)
	}
}

func registryUpdateHandler(_ string, _event interface{}) {
	encoded, err := json.Marshal(_event)
	if err != nil {
		log.WithField("error", err).Error("Can't marshal registry update event")
		return
	}
	var services map[string][]service
	err = json.Unmarshal(encoded, &services)
	if err != nil {
		log.WithField("error", err).Error("Can't unmarshal registry update event to []Services")
		return
	}
	tqServices, ok := services["taskQueue"]
	if !ok {
		log.Warn("No taskq services in registry")
		return
	}
	var _tqAddress string
	for _, service := range tqServices {
		if tqAddress == service.Address+":"+service.Port {
			break
		}
		_tqAddress = service.Address + ":" + service.Port
	}
	if _tqAddress != "" {
		tqLocker.Lock()
		tqAddress = _tqAddress
		tqLocker.Unlock()
		reconnectToTaskQ()
	}
}
