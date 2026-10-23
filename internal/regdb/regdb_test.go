package regdb

import (
	"encoding/binary"
	"os"
	"testing"
)

// encode writes a database in the file's format (as wireless-regdb's
// db2fw.py does), one collection per country, so tests can make their own.
func encode(countries []string, rules map[string][]Rule) []byte {
	put2 := func(b []byte, v int) []byte { return binary.BigEndian.AppendUint16(b, uint16(v)) }
	put4 := func(b []byte, v uint32) []byte { return binary.BigEndian.AppendUint32(b, v) }

	data := put4(put4(nil, magic), version)
	// The country list, with room for the pointers filled in below.
	listAt := len(data)
	for _, c := range countries {
		data = append(data, c...)
		data = put2(data, 0)
	}
	data = put4(data, 0)
	for i, c := range countries {
		var ruleAt []int
		for _, r := range rules[c] {
			ruleAt = append(ruleAt, len(data))
			data = append(data, 16, r.Flags)
			data = put2(data, 2000) // 20 dBm
			data = put4(put4(put4(data, r.StartKHz), r.EndKHz), r.MaxWidthKHz)
		}
		binary.BigEndian.PutUint16(data[listAt+4*i+2:], uint16(len(data)/4))
		data = append(data, 3, byte(len(ruleAt)), 0, 0)
		for _, at := range ruleAt {
			data = put2(data, at/4)
		}
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return data
}

// testDB has the world rules, and a country with radar channels and a
// country without the upper 5 GHz channels.
var testDB = encode([]string{"00", "JP", "US"}, map[string][]Rule{
	"00": {{StartKHz: 2402000, EndKHz: 2472000, MaxWidthKHz: 40000}},
	"JP": {
		{StartKHz: 2402000, EndKHz: 2482000, MaxWidthKHz: 40000},
		{StartKHz: 5170000, EndKHz: 5250000, MaxWidthKHz: 80000},
		{StartKHz: 5250000, EndKHz: 5330000, MaxWidthKHz: 80000, Flags: flagDFS},
	},
	"US": {
		{StartKHz: 2402000, EndKHz: 2472000, MaxWidthKHz: 40000},
		{StartKHz: 5170000, EndKHz: 5250000, MaxWidthKHz: 80000},
		{StartKHz: 5735000, EndKHz: 5835000, MaxWidthKHz: 80000},
		{StartKHz: 5850000, EndKHz: 5895000, MaxWidthKHz: 40000, Flags: flagNoIR},
	},
})

func TestHotspotChannelAllowed(t *testing.T) {
	db, err := Parse(testDB)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		country string
		channel int
		want    bool
	}{
		{"US", 6, true},
		{"US", 13, false}, // past the US 2.4 GHz range
		{"JP", 13, true},
		{"US", 149, true},
		{"JP", 149, false}, // Japan has no 149
		{"JP", 52, false},  // needs radar detection
		{"US", 177, false}, // "no IR": may not start a network
		{"US", 36, true},
		{"FR", 6, true},   // not listed: the world rules
		{"FR", 36, false}, // the world rules have no 5 GHz
	}
	for _, tt := range tests {
		if got := db.HotspotChannelAllowed(tt.country, ChannelMHz(tt.channel)); got != tt.want {
			t.Errorf("%s channel %d = %v, want %v", tt.country, tt.channel, got, tt.want)
		}
	}
}

func TestChannelMHz(t *testing.T) {
	for channel, mhz := range map[int]int{1: 2412, 13: 2472, 14: 2484, 36: 5180, 165: 5825} {
		if got := ChannelMHz(channel); got != mhz {
			t.Errorf("ChannelMHz(%d) = %d, want %d", channel, got, mhz)
		}
	}
}

func TestParse_RefusesDamagedFiles(t *testing.T) {
	cut := testDB[:len(testDB)-6]
	pointsOutside := append([]byte(nil), testDB...)
	binary.BigEndian.PutUint16(pointsOutside[10:], 0xffff)
	wrongVersion := append([]byte(nil), testDB...)
	binary.BigEndian.PutUint32(wrongVersion[4:], 19)
	for name, data := range map[string][]byte{
		"empty":                  nil,
		"another file":           []byte("PK\x03\x04 a zip file"),
		"another version":        wrongVersion,
		"cut short":              cut,
		"a pointer past the end": pointsOutside,
		"no end to the list":     testDB[:12],
	} {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

// The real database (testdata/README.md): the rules the Hotspot page
// depends on.
func TestParse_TheRealDatabase(t *testing.T) {
	data, err := os.ReadFile("testdata/regulatory.db")
	if err != nil {
		t.Fatal(err)
	}
	db, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		country string
		channel int
		want    bool
	}{
		{"US", 149, true},
		{"JP", 149, false}, // Japan has no channel 149
		{"JP", 36, true},
		{"US", 12, false}, // the US stops at 11
		{"GB", 13, true},
		{"DE", 52, false}, // radar detection
	}
	for _, tt := range tests {
		if got := db.HotspotChannelAllowed(tt.country, ChannelMHz(tt.channel)); got != tt.want {
			t.Errorf("%s channel %d = %v, want %v", tt.country, tt.channel, got, tt.want)
		}
	}
}
