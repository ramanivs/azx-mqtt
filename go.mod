module azxmqtt

go 1.21

require (
	github.com/eclipse/paho.mqtt.golang v1.4.3
	github.com/go-sql-driver/mysql v1.7.1
)

require (
	github.com/gorilla/websocket v1.5.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/sync v0.3.0 // indirect
)

replace golang.org/x/net => github.com/golang/net v0.17.0

replace golang.org/x/sync => github.com/golang/sync v0.3.0
