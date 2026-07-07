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
	appVersion   = "1.1"
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
	// Load the topic list + qos + handler function name from the DB,
	// mirroring the SELECT ... subscription_tbl / topics_tbl / device_type_tbl
	// join in the PHP script.
	db := ConnectDB()
	defer DisconnectDB()
	fmt.Println("MySQL connection established. Loading MQTT subscriptions...")

	rows, err := db.Query(`SELECT CONCAT(t1.device_id, '/', t2.topic) AS topic_val, t3.dtype_fn
		FROM subscription_tbl t1
		LEFT JOIN topics_tbl t2 ON t1.topic_id = t2.topic_id
		LEFT JOIN device_type_tbl t3 ON t1.dtype_id = t3.dtype_id
		WHERE t1.status = 'S'`)
	if err != nil {
		recordError("Could not load subscriptions from MySQL: %v", err)
		os.Exit(1)
	}

	type topicInfo struct {
		topic  string
		dtypeF string
	}
	var topics []topicInfo

	for rows.Next() {
		var t topicInfo
		if err := rows.Scan(&t.topic, &t.dtypeF); err != nil {
			recordError("Could not read a subscription row from MySQL: %v", err)
			continue
		}
		topics = append(topics, t)
	}
	if err := rows.Err(); err != nil {
		recordError("Could not finish reading subscriptions from MySQL: %v", err)
	}
	rows.Close()
	fmt.Printf("Loaded %d subscriptions. Subscribing to MQTT topics...\n", len(topics))

	for _, t := range topics {
		handler, ok := handlerRegistry[t.dtypeF]
		if !ok {
			recordError("No message handler is configured for dtype_fn %q on topic %s; skipping this subscription", t.dtypeF, t.topic)
			continue
		}

		topicName := t.topic
		messageHandler := handler
		msgHandler := func(client mqtt.Client, msg mqtt.Message) {
			recordMessageReceived()
			messageHandler(topicName, string(msg.Payload()))
		}

		// qos 1, matching the PHP `"qos" => 1` in the $topics array.
		if token := client.Subscribe(t.topic, 1, msgHandler); token.Wait() && token.Error() != nil {
			recordError("Could not subscribe to MQTT topic %s: %v", t.topic, token.Error())
			continue
		}
		fmt.Println("Subscribed to", t.topic)
	}

	done := make(chan struct{})
	startHeartbeat(60*time.Second, done)
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
