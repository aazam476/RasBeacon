// Package regdb reads the Wi-Fi regulatory database: which radio channels
// each country allows. Linux keeps it in /lib/firmware/regulatory.db (the
// wireless-regdb package); the kernel reads the same file when RasBeacon
// runs `iw reg set <country>`.
//
// # The file format
//
// The file is a list of big-endian records (see db2fw.py in wireless-regdb
// and fwdb_* in the kernel's net/wireless/reg.c):
//
//	header      magic "RGDB", version 20
//	countries   2 letters + a pointer to a collection, ..., then 4 zero bytes
//	collection  header length, number of rules, DFS region, then a pointer
//	            to each rule (2 bytes each)
//	rule        length, flags, max power, start and end (kHz), max width (kHz)
//
// A pointer is an offset in the file divided by 4.
package regdb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// File is where Linux keeps the database.
const File = "/lib/firmware/regulatory.db"

const (
	magic   = 0x52474442 // "RGDB"
	version = 20
)

// Rule flags (FWDB_FLAG_* in the kernel).
const (
	flagNoOFDM = 1 << 0
	flagDFS    = 1 << 2 // radar detection needed before using the channel
	flagNoIR   = 1 << 3 // "no initiating radiation": may not start a network
)

// Rule is one frequency range a country allows.
type Rule struct {
	StartKHz, EndKHz, MaxWidthKHz uint32
	Flags                         uint8
}

// DB is the database: each country's rules, by its two-letter code.
type DB map[string][]Rule

// Parse reads the database file's contents.
func Parse(data []byte) (DB, error) {
	if len(data) < 8 || binary.BigEndian.Uint32(data) != magic {
		return nil, errors.New("not a regulatory database")
	}
	if v := binary.BigEndian.Uint32(data[4:]); v != version {
		return nil, fmt.Errorf("regulatory database version %d, want %d", v, version)
	}
	// read2 and read4 return 0 past the end, so a damaged file can't crash
	// the reading; the checks below refuse it.
	read2 := func(at int) int {
		if at+2 > len(data) {
			return 0
		}
		return int(binary.BigEndian.Uint16(data[at:]))
	}
	read4 := func(at int) uint32 {
		if at+4 > len(data) {
			return 0
		}
		return binary.BigEndian.Uint32(data[at:])
	}

	db := DB{}
	for at := 8; ; at += 4 {
		if at+4 > len(data) {
			return nil, errors.New("the country list doesn't end")
		}
		if read4(at) == 0 {
			break // the end of the country list
		}
		country := string(data[at : at+2])
		collection := read2(at+2) * 4
		if collection+3 > len(data) {
			return nil, fmt.Errorf("country %s points outside the file", country)
		}
		headerLength, ruleCount := int(data[collection]), int(data[collection+1])
		// The rule pointers start after the header, rounded up to 2 bytes.
		pointers := collection + (headerLength+1)/2*2
		if pointers+2*ruleCount > len(data) {
			return nil, fmt.Errorf("the rules of %s are cut off", country)
		}
		var rules []Rule
		for i := range ruleCount {
			rule := read2(pointers+2*i) * 4
			if rule+16 > len(data) {
				return nil, fmt.Errorf("a rule of %s points outside the file", country)
			}
			rules = append(rules, Rule{
				Flags:       data[rule+1],
				StartKHz:    read4(rule + 4),
				EndKHz:      read4(rule + 8),
				MaxWidthKHz: read4(rule + 12),
			})
		}
		db[country] = rules
	}
	return db, nil
}

// HotspotChannelAllowed reports whether a hotspot may use the 20 MHz
// channel centered at mhz in country. The whole channel (10 MHz either
// side) must be inside one rule that allows 20 MHz, and that rule may not
// need radar detection or forbid starting a network. A country the
// database doesn't list follows the world rules ("00"), as the kernel does.
func (db DB) HotspotChannelAllowed(country string, mhz int) bool {
	rules, ok := db[country]
	if !ok {
		rules = db["00"]
	}
	low, high := uint32(mhz-10)*1000, uint32(mhz+10)*1000
	for _, r := range rules {
		usable := r.Flags&(flagDFS|flagNoIR|flagNoOFDM) == 0 && r.MaxWidthKHz >= 20000
		if usable && r.StartKHz <= low && high <= r.EndKHz {
			return true
		}
	}
	return false
}

// ChannelMHz is a Wi-Fi channel's center frequency: channels 1 to 13 are on
// 2.4 GHz, 5 MHz apart from 2412 MHz (14, Japan's old one, is at 2484);
// 5 GHz channels are at 5000 + 5 × n.
func ChannelMHz(channel int) int {
	switch {
	case channel == 14:
		return 2484
	case channel < 14:
		return 2407 + 5*channel
	}
	return 5000 + 5*channel
}
