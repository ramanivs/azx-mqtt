package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const subscriptionQuery = `SELECT CONCAT(t1.device_id, '/', t2.topic) AS topic_val, t3.dtype_fn
		FROM subscription_tbl t1
		LEFT JOIN topics_tbl t2 ON t1.topic_id = t2.topic_id
		LEFT JOIN device_type_tbl t3 ON t1.dtype_id = t3.dtype_id
		WHERE t1.status = 'S'`

type topicInfo struct {
	topic  string
	dtypeF string
}

type subscriptionLoad struct {
	loadedFromDB int
	topics       map[string]topicInfo
}

type subscriptionManager struct {
	client mqtt.Client
	mu     sync.RWMutex
	active map[string]topicInfo
}

func newSubscriptionManager(client mqtt.Client) *subscriptionManager {
	return &subscriptionManager{
		client: client,
		active: make(map[string]topicInfo),
	}
}

func loadSubscriptions() (subscriptionLoad, error) {
	rows, err := ConnectDB().Query(subscriptionQuery)
	if err != nil {
		return subscriptionLoad{}, err
	}
	defer rows.Close()

	loaded := subscriptionLoad{topics: make(map[string]topicInfo)}
	for rows.Next() {
		var t topicInfo
		if err := rows.Scan(&t.topic, &t.dtypeF); err != nil {
			recordError("Could not read a subscription row from MySQL: %v", err)
			continue
		}

		loaded.loadedFromDB++
		if shouldSubscribe(t.topic) {
			loaded.topics[t.topic] = t
			continue
		}

		fmt.Printf("Skipping topic %q because it does not start with \"Azx\" or \"Evt\".\n", t.topic)
	}
	if err := rows.Err(); err != nil {
		return loaded, err
	}

	return loaded, nil
}

func (m *subscriptionManager) subscribeInitial(loaded subscriptionLoad) error {
	fmt.Printf("Total topics loaded      : %d\n", loaded.loadedFromDB)
	fmt.Printf("Topics subscribed        : %d\n", len(loaded.topics))
	fmt.Printf("Topics skipped           : %d\n", loaded.loadedFromDB-len(loaded.topics))
	fmt.Println("Subscribing to MQTT topics...")

	for _, t := range loaded.topics {
		if err := m.subscribeTopic(t); err != nil {
			recordError("Could not subscribe to MQTT topic %s: %v", t.topic, err)
			continue
		}
		fmt.Println("Subscribed to", t.topic)
	}
	return nil
}

func (m *subscriptionManager) refreshSubscriptions() {
	loaded, err := loadSubscriptions()
	if err != nil {
		recordError("Could not refresh subscriptions from MySQL: %v", err)
		fmt.Println("Keeping current MQTT subscriptions active; will retry on the next scheduled refresh.")
		return
	}

	m.mu.RLock()
	activeSnapshot := make(map[string]topicInfo, len(m.active))
	for topic, info := range m.active {
		activeSnapshot[topic] = info
	}
	m.mu.RUnlock()

	added := 0
	removed := 0
	unchanged := 0

	for topic, t := range loaded.topics {
		if _, ok := activeSnapshot[topic]; ok {
			unchanged++
			continue
		}

		if err := m.subscribeTopic(t); err != nil {
			recordError("Could not subscribe to new MQTT topic %s: %v", topic, err)
			continue
		}
		added++
		fmt.Printf("[+] New topic detected: %s\n", topic)
	}

	for topic, t := range activeSnapshot {
		if _, ok := loaded.topics[topic]; ok {
			continue
		}

		if err := m.unsubscribeTopic(t); err != nil {
			recordError("Could not unsubscribe from removed MQTT topic %s: %v", topic, err)
			continue
		}
		removed++
		fmt.Printf("[-] Topic removed: %s\n", topic)
	}

	m.printRefreshSummary(loaded.loadedFromDB, added, removed, unchanged)
}

func refreshSubscriptions(manager *subscriptionManager) {
	manager.refreshSubscriptions()
}

func (m *subscriptionManager) subscribeTopic(t topicInfo) error {
	handler, ok := handlerRegistry[t.dtypeF]
	if !ok {
		return fmt.Errorf("no message handler is configured for dtype_fn %q on topic %s", t.dtypeF, t.topic)
	}

	topicName := t.topic
	messageHandler := handler
	msgHandler := func(client mqtt.Client, msg mqtt.Message) {
		recordMessageReceived()
		messageHandler(topicName, string(msg.Payload()))
	}

	// qos 1, matching the PHP `"qos" => 1` in the $topics array.
	if token := m.client.Subscribe(t.topic, 1, msgHandler); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	m.mu.Lock()
	m.active[t.topic] = t
	m.mu.Unlock()
	return nil
}

func subscribeTopic(manager *subscriptionManager, t topicInfo) error {
	return manager.subscribeTopic(t)
}

func (m *subscriptionManager) unsubscribeTopic(t topicInfo) error {
	if token := m.client.Unsubscribe(t.topic); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	m.mu.Lock()
	delete(m.active, t.topic)
	m.mu.Unlock()
	return nil
}

func unsubscribeTopic(manager *subscriptionManager, t topicInfo) error {
	return manager.unsubscribeTopic(t)
}

func (m *subscriptionManager) activeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.active)
}

func (m *subscriptionManager) printRefreshSummary(loadedFromDB, added, removed, unchanged int) {
	fmt.Println("------------------------------------------------")
	fmt.Println("Subscription Refresh")
	fmt.Println()
	fmt.Printf("Loaded from DB : %d\n", loadedFromDB)
	fmt.Printf("Currently Active : %d\n", m.activeCount())
	fmt.Println()
	fmt.Printf("Added : %d\n", added)
	fmt.Printf("Removed : %d\n", removed)
	fmt.Printf("Unchanged : %d\n", unchanged)
	fmt.Println()
	fmt.Println("Next refresh in 5 minutes")
	fmt.Println("------------------------------------------------")
}

func startSubscriptionRefresh(interval time.Duration, manager *subscriptionManager, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refreshSubscriptions(manager)
			case <-done:
				return
			}
		}
	}()
}

func shouldSubscribe(topic string) bool {
	allowedPrefixes := []string{"Azx", "Evt"}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(topic, prefix) {
			return true
		}
	}
	return false
}
