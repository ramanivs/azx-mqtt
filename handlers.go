package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// procSMTMsg mirrors procSMT_Msg() — handles smart meter, smart plug/switch,
// and Evtron EV charger payloads and upserts them into smartmeter_tbl /
// device_tbl.
func procSMTMsg(topic, msg string) {
	currentTime := time.Now().In(istLocation)

	fmt.Printf("Received message on topic %s: %s\n", topic, msg)

	stnid := topicValue(topic)
	status := "A"
	switch_ := "N"
	var (
		version, localIP, stationStatus, evseStatus, elapsedTime string
		dateTTime                                                string
		power1, power2, power3                                   float64
		factor1, factor2, factor3                                float64
		frequency1, frequency2, frequency3                       float64
		voltage1, voltage2, voltage3                             float64
		current1, current2, current3                             float64
		energy, temperature, freeHeap, rfid, totalEnergy         float64
		uptime                                                   float64
	)

	logRawJson(topic, msg)

	upperMsg := strings.ToUpper(strings.TrimSpace(msg))
	if upperMsg == "ONLINE" {
		return
	} else if upperMsg == "OFFLINE" {
		status = "U"
	} else {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(msg), &data); err != nil {
			recordError("Could not decode smart meter payload on topic %s as JSON: %v", topic, err)
			return
		}

		if _, ok := data["L1_Voltage"]; ok {
			energy = getFloat(data, "Total_Energy", 0)
			power1 = getFloat(data, "L1_Power", 0)
			power2 = getFloat(data, "L2_Power", 0)
			power3 = getFloat(data, "L3_Power", 0)
			factor1 = getFloat(data, "L1_Power_Factor", 0)
			factor2 = getFloat(data, "L2_Power_Factor", 0)
			factor3 = getFloat(data, "L3_Power_Factor", 0)
			frequency1 = getFloat(data, "L1_Frequency", 0)
			frequency2 = getFloat(data, "L2_Frequency", 0)
			frequency3 = getFloat(data, "L3_Frequency", 0)
			voltage1 = getFloat(data, "L1_Voltage", 0)
			voltage2 = getFloat(data, "L2_Voltage", 0)
			voltage3 = getFloat(data, "L3_Voltage", 0)
			current1 = getFloat(data, "L1_Current", 0)
			current2 = getFloat(data, "L2_Current", 0)
			current3 = getFloat(data, "L3_Current", 0)
			dateTTime = getString(data, "Date_Time", "")

		} else if _, ok := data["ENERGY"]; ok {
			energy = getNestedFloat(data, "ENERGY", "Today", 0)

			// ENERGY.Power / Factor / Frequency / Voltage / Current are
			// nested *inside* the ENERGY object, each holding a 3-element
			// array — reach one level deeper than getIndexedFloat does.
			energyObj, _ := data["ENERGY"].(map[string]interface{})
			power1 = getIndexedFloat(energyObj, "Power", 0, 0)
			power2 = getIndexedFloat(energyObj, "Power", 1, 0)
			power3 = getIndexedFloat(energyObj, "Power", 2, 0)
			factor1 = getIndexedFloat(energyObj, "Factor", 0, 0)
			factor2 = getIndexedFloat(energyObj, "Factor", 1, 0)
			factor3 = getIndexedFloat(energyObj, "Factor", 2, 0)
			frequency1 = getIndexedFloat(energyObj, "Frequency", 0, 0)
			frequency2 = getIndexedFloat(energyObj, "Frequency", 1, 0)
			frequency3 = getIndexedFloat(energyObj, "Frequency", 2, 0)
			voltage1 = getIndexedFloat(energyObj, "Voltage", 0, 0)
			voltage2 = getIndexedFloat(energyObj, "Voltage", 1, 0)
			voltage3 = getIndexedFloat(energyObj, "Voltage", 2, 0)
			current1 = getIndexedFloat(energyObj, "Current", 0, 0)
			current2 = getIndexedFloat(energyObj, "Current", 1, 0)
			current3 = getIndexedFloat(energyObj, "Current", 2, 0)
			dateTTime = getString(data, "Time", "")

		} else {
			// Smart plug/switch & Evtron charger
			energy = getFloat(data, "Last_KWH", 0)
			version = getString(data, "Version", "0")
			uptime = getFloat(data, "Uptime", 0)
			power1 = getFloat(data, "Wattage", 0)
			factor1 = getFloat(data, "Power_Factor", 0)
			voltage1 = getFloat(data, "AC_Voltage", 0)
			current1 = getFloat(data, "Amps", 0)
			dateTTime = getString(data, "Date_Time", "")
			stationStatus = getString(data, "Station_Status", "0")
			localIP = getString(data, "Local_IP", "0")
			temperature = getFloat(data, "Temperature", 0)
			freeHeap = getFloat(data, "Free_Heap", 0)
			rfid = getFloat(data, "RFID_Tag", 0)
			evseStatus = getString(data, "EVSE_Status", "0")
			elapsedTime = getString(data, "Elapsed_Time", "0")
			totalEnergy = getFloat(data, "Total_Energy", 0)

			if _, ok := data["EVSE_Status"]; ok {
				// Evtron charger
				switch_ = boolToSwitch(getFloat(data, "Relay_Status", 0) == 1)
			} else {
				switch_ = boolToSwitch(getBool(data, "Relay_Status"))
			}
		}
	}

	formattedTime := dbFormat(dateTTime, currentTime)
	istDateTime := time.Date(formattedTime.Year(), formattedTime.Month(), formattedTime.Day(),
		formattedTime.Hour(), formattedTime.Minute(), formattedTime.Second(), 0, istLocation)
	utcDatetime := toUTC(istDateTime)

	mysqlDB := ConnectDB()

	_, err := mysqlDB.Exec(`INSERT INTO smartmeter_tbl (device_id, status, event_dt, switch,
			voltage_phase1, voltage_phase2, voltage_phase3, power_phase1, power_phase2, power_phase3,
			current_phase1, current_phase2, current_phase3, factor_phase1, factor_phase2, factor_phase3,
			frequency_phase1, frequency_phase2, frequency_phase3, energy, version, local_ip, uptime,
			station_status, evse_status, elapsed_time, temperature, free_heap, rfid_tag, total_energy)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stnid, status, utcDatetime, switch_,
		voltage1, voltage2, voltage3, power1, power2, power3,
		current1, current2, current3, factor1, factor2, factor3,
		frequency1, frequency2, frequency3, energy,
		version, localIP, uptime, stationStatus, evseStatus, elapsedTime,
		temperature, freeHeap, rfid, totalEnergy,
	)
	if err != nil {
		recordError("Could not write smart meter data for device %s to MySQL: %v", stnid, err)
		return
	}
	recordDatabaseWrite()

	updateSwitchStatus(mysqlDB, stnid, switch_)
}

// procWLCMsg mirrors procWLC_Msg() — handles water-level controller
// payloads and upserts them into waterlevel_ctrl_tbl / device_tbl.
func procWLCMsg(topic, msg string) {
	currentTime := time.Now().In(istLocation)

	fmt.Printf("Received message on topic %s: %s\n", topic, msg)

	stnid := topicValue(topic)
	status := "A"
	switch_ := "N"
	var (
		power, factor, voltage, current float64
		dryrun, automode                string
		sumptank, overheadtank          string
		waterlevel                      float64
		tankstatus                      string
		energy                          float64
		dateTTime                       string
	)

	logRawJson(topic, msg)

	upperMsg := strings.ToUpper(strings.TrimSpace(msg))
	if upperMsg == "ONLINE" {
		return
	} else if upperMsg == "OFFLINE" {
		status = "U"
	} else {
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(msg), &data); err != nil {
			recordError("Could not decode water-level payload on topic %s as JSON: %v", topic, err)
			return
		}

		if _, ok := data["Station_ID"]; ok {
			switch_ = boolToSwitch(getBool(data, "Relay_Status"))
			energy = getFloat(data, "Last_KWH", 0)
			power = getFloat(data, "Wattage", 0)
			factor = getFloat(data, "Power_Factor", 0)
			voltage = getFloat(data, "AC_Voltage", 0)
			current = getFloat(data, "Amps", 0)
			automode = ynFromBool(getBool(data, "Auto_Mode"), "Y", "N")
			dryrun = ynFromBool(getBool(data, "Dry_Run"), "N", "Y")
			sumptank = ynFromBool(getBool(data, "Sump_Tank"), "Y", "N")
			overheadtank = ynFromBool(getBool(data, "Overhead_Tank"), "Y", "N")
			waterlevel = getFloat(data, "Water_Level", 0)
			tankstatus = getString(data, "Status", "")
			dateTTime = getString(data, "Date_Time", "")
		}
	}

	formattedTime := dbFormat(dateTTime, currentTime)
	istDateTime := time.Date(formattedTime.Year(), formattedTime.Month(), formattedTime.Day(),
		formattedTime.Hour(), formattedTime.Minute(), formattedTime.Second(), 0, istLocation)
	utcDatetime := toUTC(istDateTime)

	mysqlDB := ConnectDB()

	_, err := mysqlDB.Exec(`INSERT INTO waterlevel_ctrl_tbl (device_id, status, event_dt, voltage, current, power,
			power_factor, dry_run, auto_mode, sump_tank, overhead_tank, water_level, description, switch, energy)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stnid, status, utcDatetime, voltage, current, power,
		factor, dryrun, automode, sumptank, overheadtank, waterlevel, tankstatus, switch_, energy,
	)
	if err != nil {
		recordError("Could not write water-level data for device %s to MySQL: %v", stnid, err)
		return
	}
	recordDatabaseWrite()

	updateSwitchStatus(mysqlDB, stnid, switch_)
}

// updateSwitchStatus mirrors the shared "select switch from device_tbl ...
// then UPDATE" block that appears at the end of both PHP handlers.
func updateSwitchStatus(db *sql.DB, stnid, switch_ string) {
	var currentSwitch sql.NullString
	err := db.QueryRow(`SELECT switch FROM device_tbl WHERE device_id = ?`, stnid).Scan(&currentSwitch)
	if err != nil {
		recordError("Could not read current switch status for device %s from MySQL: %v", stnid, err)
		return
	}

	if !currentSwitch.Valid {
		_, err = db.Exec(`UPDATE device_tbl SET switch = ?, status = 'A' WHERE device_id = ?`, switch_, stnid)
	} else {
		_, err = db.Exec(`UPDATE device_tbl SET switch = ? WHERE device_id = ?`, switch_, stnid)
	}
	if err != nil {
		recordError("Could not update switch status for device %s in MySQL: %v", stnid, err)
		return
	}
	recordDatabaseWrite()
}

func boolToSwitch(b bool) string {
	if b {
		return "O"
	}
	return "F"
}

func ynFromBool(b bool, whenTrue, whenFalse string) string {
	if b {
		return whenTrue
	}
	return whenFalse
}
