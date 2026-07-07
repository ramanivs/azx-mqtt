package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var istLocation *time.Location

func init() {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// Fallback to a fixed +5:30 offset if the tzdata isn't available.
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	istLocation = loc
}

// dbFormat mirrors the PHP dbFormat() function: if the device supplied a
// timestamp that parses cleanly and is within 2 minutes of "now", use it;
// otherwise fall back to the current time. Returns a time.Time in the
// Asia/Kolkata zone (matching PHP's date_default_timezone_set call).
func dbFormat(dateTTime string, currentTime time.Time) time.Time {
	if dateTTime == "" {
		return currentTime
	}

	// PHP format 'Y-m-d\TH:i:s'  ->  Go layout "2006-01-02T15:04:05"
	received, err := time.ParseInLocation("2006-01-02T15:04:05", dateTTime, istLocation)
	if err != nil {
		recordError("Device timestamp %q is not in the expected format YYYY-MM-DDTHH:MM:SS; using the current time", dateTTime)
		return currentTime
	}

	diff := currentTime.Sub(received)
	if diff < 0 {
		diff = -diff
	}
	if diff < 2*time.Minute {
		return received
	}
	return currentTime
}

// toUTC converts a Y-m-d + H:i:s pair (already in IST) into a UTC
// "2006-01-02 15:04:05" string suitable for a MySQL DATETIME column.
func toUTC(t time.Time) string {
	return t.In(time.UTC).Format("2006-01-02 15:04:05")
}

// logRawJson mirrors logRawJson() — appends the raw topic/payload to a
// local log file.
func logRawJson(topic, msg string) {
	f, err := os.OpenFile("mqtt_messages.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		recordError("Could not open mqtt_messages.log for writing: %v", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "Topic:: %s\n%s\n", topic, msg)
}

// topicValue mirrors topicValue(): if the topic starts with "tele/",
// return the segment after it; otherwise return the first segment.
func topicValue(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) == 0 {
		return topic
	}
	if parts[0] == "tele" && len(parts) > 1 {
		return parts[1]
	}
	return parts[0]
}

// ---- JSON field helpers -------------------------------------------------
//
// PHP's `$data['key'] ?? default` is forgiving about type (a payload field
// might arrive as a JSON number, string, or bool). These helpers replicate
// that leniency when reading from a decoded map[string]interface{}.

func getFloat(data map[string]interface{}, key string, def float64) float64 {
	v, ok := data[key]
	if !ok || v == nil {
		return def
	}
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return def
		}
		return f
	case bool:
		if val {
			return 1
		}
		return 0
	default:
		return def
	}
}

func getString(data map[string]interface{}, key string, def string) string {
	v, ok := data[key]
	if !ok || v == nil {
		return def
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// getBool mirrors PHP's `$data['key'] == true` loose comparison: any
// non-zero number, non-empty/non-"0" string, or JSON true counts as true.
func getBool(data map[string]interface{}, key string) bool {
	v, ok := data[key]
	if !ok || v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case string:
		return val != "" && val != "0"
	default:
		return false
	}
}

// getNestedFloat reads data[outer][innerKey] where the nested value is
// itself a map (e.g. ENERGY.Power) — used for the Tasmota-style payloads.
func getNestedFloat(data map[string]interface{}, outer, innerKey string, def float64) float64 {
	nestedRaw, ok := data[outer]
	if !ok {
		return def
	}
	nested, ok := nestedRaw.(map[string]interface{})
	if !ok {
		return def
	}
	return getFloat(nested, innerKey, def)
}

func getNestedString(data map[string]interface{}, outer, innerKey string, def string) string {
	nestedRaw, ok := data[outer]
	if !ok {
		return def
	}
	nested, ok := nestedRaw.(map[string]interface{})
	if !ok {
		return def
	}
	return getString(nested, innerKey, def)
}

// getIndexedFloat reads data[outer][index] where the nested value is a
// JSON array (e.g. ENERGY.Power[0]) — Tasmota reports 3-phase arrays this way.
func getIndexedFloat(data map[string]interface{}, outer string, index int, def float64) float64 {
	nestedRaw, ok := data[outer]
	if !ok {
		return def
	}
	arr, ok := nestedRaw.([]interface{})
	if !ok || index >= len(arr) {
		return def
	}
	switch val := arr[index].(type) {
	case float64:
		return val
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return def
		}
		return f
	default:
		return def
	}
}
