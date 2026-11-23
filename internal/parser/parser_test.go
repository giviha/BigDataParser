package parser

import (
	"testing"
	"time"
)

const sampleAccess = `192.0.2.8 - - [23/Nov/2026:14:30:00 +0300] "GET /index HTTP/1.1" 503 42`

func TestParse(t *testing.T) {
	now := time.Date(2026, 11, 23, 11, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		source, raw, level, message string
		status                      uint16
	}{
		{"nginx", sampleAccess, "error", "GET /index HTTP/1.1", 503},
		{"apache", sampleAccess, "error", "GET /index HTTP/1.1", 503},
		{"syslog", "<11>Nov 23 11:30:00 box app: disk full", "error", "app: disk full", 0},
		{"json", `{"time":"2026-11-23T11:30:00Z","level":"INFO","message":"hello"}`, "info", "hello", 0},
		{"json", "malformed", "unknown", "malformed", 0},
		{"nginx", "malformed", "unknown", "malformed", 0},
	} {
		for _, manual := range []bool{true, false} {
			row := Parse(tc.raw, "agent-01", tc.source, now, manual)
			if row.Time != now.Unix() || row.Source != tc.source || row.Agent != "agent-01" || row.Level != tc.level || row.Message != tc.message || row.Status != tc.status {
				t.Errorf("parse %q, manual=%t: %+v", tc.raw, manual, row)
			}
		}
	}
}

func BenchmarkAccess(b *testing.B) {
	now := time.Now()
	for _, kind := range []struct {
		name   string
		manual bool
	}{{"manual", true}, {"regexp", false}} {
		b.Run(kind.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Parse(sampleAccess, "agent-01", "nginx", now, kind.manual)
			}
		})
	}
}
