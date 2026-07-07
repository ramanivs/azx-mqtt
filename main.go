package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTT broker settings — equivalent to the top-level $server/$port/etc.
// variables in the PHP script.
const (
	appVersion   = "1.2"
	mqttServer   = "172.16.50.122"
	mqttPort     = 12883
	mqttUsername = "azonix"
	mqttPassword = "abcdef12345"
)

// handlerRegistry maps the dtype_fn value stored in device_type_tbl to an
// actual Go function. PHP's phpMQTT library calls the "function" name
// dynamically per-subscription; Go doesn't have PHP's call-by-string-name,
// so subscriptions are dispatched through this lookup table instead.
var handlerRegistry = map[string]func(topic, msg string){
	"procSMT_Msg": procSMTMsg,
	"procWLC_Msg": procWLCMsg,
}

func main() {
	printStartupBanner()
	fmt.Println("Starting MQTT to MySQL bridge...")
	rand.Seed(time.Now().UnixNano())
	clientID := fmt.Sprintf("azxmqtt_%d", 100000+rand.Intn(900000))

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", mqttServer, mqttPort))
	opts.SetClientID(clientID)
	opts.SetUsername(mqttUsername)
	opts.SetPassword(mqttPassword)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)

	client := mqtt.NewClient(opts)
	fmt.Printf("Connecting to MQTT broker %s:%d...\n", mqttServer, mqttPort)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		recordError("Could not connect to MQTT broker %s:%d: %v", mqttServer, mqttPort, token.Error())
		os.Exit(1)
	}
	fmt.Println("MQTT connection established.")
	defer client.Disconnect(250)

	fmt.Printf("Connecting to MySQL database %s at %s:%d...\n", dbName, dbHost, dbPort)
	ConnectDB()
	defer DisconnectDB()
	fmt.Println("MySQL connection established. Loading MQTT subscriptions...")

	subscriptionManager := newSubscriptionManager(client)
	loaded, err := loadSubscriptions()
	if err != nil {
		recordError("Could not load subscriptions from MySQL: %v", err)
		os.Exit(1)
	}

	if err := subscriptionManager.subscribeInitial(loaded); err != nil {
		recordError("Could not complete initial MQTT subscription setup: %v", err)
	}

	done := make(chan struct{})
	startHeartbeat(60*time.Second, done)
	startSubscriptionRefresh(5*time.Minute, subscriptionManager, done)
	fmt.Println("Bridge is running. Press Ctrl+C to stop.")
	printStats("Current stats:")

	// Block indefinitely, servicing MQTT callbacks — equivalent to the PHP
	// `while ($mqtt->proc()) {}` loop. Wait for SIGINT/SIGTERM to exit
	// cleanly instead of running forever with no shutdown path.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	close(done)

	fmt.Println("Shutdown signal received. Disconnecting from MQTT and MySQL.")
	printStats("Final stats:")
}

func printStartupBanner() {
	fmt.Println("========================================")
	fmt.Printf(" AZX MQTT Bridge v%s\n", appVersion)
	fmt.Println(" MQTT -> MySQL message processor")
	fmt.Println("========================================")
}
