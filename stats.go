package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

type runtimeStats struct {
	startedAt        time.Time
	messagesReceived uint64
	databaseWrites   uint64
	errors           uint64
}

var appStats = runtimeStats{startedAt: time.Now()}

func recordMessageReceived() {
	atomic.AddUint64(&appStats.messagesReceived, 1)
}

func recordDatabaseWrite() {
	atomic.AddUint64(&appStats.databaseWrites, 1)
}

func recordError(format string, args ...interface{}) {
	atomic.AddUint64(&appStats.errors, 1)
	fmt.Printf("ERROR: "+format+"\n", args...)
}

func statsSnapshot() (messagesReceived, databaseWrites, errors uint64, uptime time.Duration) {
	return atomic.LoadUint64(&appStats.messagesReceived),
		atomic.LoadUint64(&appStats.databaseWrites),
		atomic.LoadUint64(&appStats.errors),
		time.Since(appStats.startedAt).Round(time.Second)
}

func printStats(prefix string) {
	messagesReceived, databaseWrites, errors, uptime := statsSnapshot()
	fmt.Printf("%s uptime=%s messages_received=%d database_writes=%d errors=%d\n",
		prefix, uptime, messagesReceived, databaseWrites, errors)
}

func startHeartbeat(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				printStats("Heartbeat:")
			case <-done:
				return
			}
		}
	}()
}
