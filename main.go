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
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		fmt.Println("unable to connect with MQTT Server::", mqttServer)
		os.Exit(1)
	}
	defer client.Disconnect(250)

	// Load the topic list + qos + handler function name from the DB,
	// mirroring the SELECT ... subscription_tbl / topics_tbl / device_type_tbl
	// join in the PHP script.
	db := ConnectDB()
	defer DisconnectDB()

	rows, err := db.Query(`SELECT CONCAT(t1.device_id, '/', t2.topic) AS topic_val, t3.dtype_fn
		FROM subscription_tbl t1
		LEFT JOIN topics_tbl t2 ON t1.topic_id = t2.topic_id
		LEFT JOIN device_type_tbl t3 ON t1.dtype_id = t3.dtype_id
		WHERE t1.status = 'S'`)
	if err != nil {
		fmt.Println("Query failed:", err)
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
			fmt.Println("Row scan failed:", err)
			continue
		}
		topics = append(topics, t)
	}
	rows.Close()

	for _, t := range topics {
		handler, ok := handlerRegistry[t.dtypeF]
		if !ok {
			fmt.Printf("No handler registered for dtype_fn %q (topic %s) — skipping\n", t.dtypeF, t.topic)
			continue
		}

		topicName := t.topic
		msgHandler := func(client mqtt.Client, msg mqtt.Message) {
			handler(topicName, string(msg.Payload()))
		}

		// qos 1, matching the PHP `"qos" => 1` in the $topics array.
		if token := client.Subscribe(t.topic, 1, msgHandler); token.Wait() && token.Error() != nil {
			fmt.Printf("Failed to subscribe to %s: %v\n", t.topic, token.Error())
			continue
		}
		fmt.Println("Subscribed to", t.topic)
	}

	// Block indefinitely, servicing MQTT callbacks — equivalent to the PHP
	// `while ($mqtt->proc()) {}` loop. Wait for SIGINT/SIGTERM to exit
	// cleanly instead of running forever with no shutdown path.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down.")
}
