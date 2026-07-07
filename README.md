# azxmqtt (Go port of azx_mqtt.php)

## Build & run

```
go mod tidy   # fetches paho.mqtt.golang and go-sql-driver/mysql
go build -o azxmqtt .
./azxmqtt
```

Requires Go 1.21+. This was compiled and `go vet`-checked successfully
against Go 1.22 during conversion.

## What changed vs. the PHP version, and why

1. **Parameterized SQL everywhere.** The PHP built every INSERT/UPDATE/SELECT
   by string-interpolating values directly into the query, which is a SQL
   injection risk (a device sending a crafted `Station_Status` string could
   break out of the query). The Go version uses `?` placeholders and
   `database/sql`'s query args, which is a strict correctness/security
   improvement with no behavior change for well-formed data.

2. **Callback dispatch by name.** `phpMQTT`'s `subscribe()` accepts a
   `"function"` key and calls that named function when a message arrives.
   Go doesn't have call-by-string-name for functions, so `main.go` builds a
   `handlerRegistry map[string]func(topic, msg string)` keyed by the
   `dtype_fn` value from `device_type_tbl`, currently containing
   `"procSMT_Msg"` and `"procWLC_Msg"`. If you add new device types with a
   new `dtype_fn` value, register the corresponding Go function in that map.

3. **Persistent connection instead of `proc()` polling.** PHP's
   `while ($mqtt->proc()) {}` polls the socket in a loop. The Go MQTT client
   (`paho.mqtt.golang`) runs its network loop on internal goroutines and
   invokes your handler directly, so `main()` just blocks on a
   SIGINT/SIGTERM channel instead. This also means, unlike the PHP script,
   `defer client.Disconnect(250)` will actually run on a clean shutdown
   (in the PHP, `$mqtt->close()` after the infinite loop was unreachable
   dead code).

4. **Single shared `*sql.DB`.** `database/sql` handles connection pooling
   and is safe for concurrent use out of the box, so the `ConnectDB()`
   singleton in `database.go` is a direct, safe analog of PHP's
   `Database::connect()` — no per-goroutine connection needed even though
   MQTT message handlers can technically fire concurrently.

5. **Loose-typing helpers.** PHP's `$data['key'] ?? default` and
   `$data['key'] == true` are forgiving about whether a JSON field arrived
   as a number, string, or bool. `helpers.go` has `getFloat`, `getString`,
   `getBool`, `getNestedFloat`/`getIndexedFloat` (for the Tasmato-style
   `ENERGY.Power[0..2]` payloads) to replicate that leniency when reading
   from the decoded `map[string]interface{}`.

6. **Timezone handling.** `dbFormat()` is a faithful port: if the device's
   own `Date_Time` field parses and is within 2 minutes of "now", it's
   used; otherwise the script's own current time is used. Everything is
   computed in `Asia/Kolkata` and converted to UTC before insertion, exactly
   as the PHP did with `DateTime`/`DateTimeZone`.

## Config

Broker and DB credentials are top-of-file constants in `main.go` and
`database.go`, matching the hardcoded values in the original PHP. Consider
moving these to environment variables before deploying (not done here, to
keep the port behavior-for-behavior faithful to the original).

## Files

- `main.go` — connects to MQTT + DB, loads subscriptions, dispatches messages
- `database.go` — MySQL connection singleton
- `handlers.go` — `procSMTMsg` / `procWLCMsg` (smart meter / water-level controller)
- `helpers.go` — `dbFormat`, `topicValue`, `logRawJson`, JSON field getters
