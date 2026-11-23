package parser

import (
	"encoding/json"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Record struct {
	Time    int64  `json:"time"`
	Agent   string `json:"agent"`
	Source  string `json:"source"`
	Level   string `json:"level"`
	IP      string `json:"ip"`
	Status  uint16 `json:"status"`
	Message string `json:"message"`
}

var access = regexp.MustCompile(`^([^ ]+) [^ ]+ [^ ]+ \[([^\]]+)\] "([^"]*)" ([0-9]{3})( |$)`)
var syslog = regexp.MustCompile(`^(?:<([0-9]{1,3})>)?([A-Z][a-z]{2} +[0-9]{1,2} [0-9:]{8}) ([^ ]+) (.+)$`)
var levels = [...]string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"}

func Parse(raw, agent, source string, now time.Time, manual bool) Record {
	record := Record{Time: now.Unix(), Agent: agent, Source: source, Level: "unknown", Message: strings.ToValidUTF8(raw, "�")}
	raw = record.Message
	fallback := record
	switch source {
	case "nginx", "apache":
		var ip, stamp, request, code string
		if manual {
			ip, stamp, request, code = splitAccess(raw)
		} else if m := access.FindStringSubmatch(raw); m != nil {
			ip, stamp, request, code = m[1], m[2], m[3], m[4]
		}
		t, dateErr := time.Parse("02/Jan/2006:15:04:05 -0700", stamp)
		status, statusErr := strconv.ParseUint(code, 10, 16)
		if net.ParseIP(ip) != nil && dateErr == nil && statusErr == nil && len(code) == 3 && status >= 100 && status < 600 {
			record.Time, record.Status, record.IP, record.Message = t.Unix(), uint16(status), ip, request
			record.Level = "info"
			if status >= 500 {
				record.Level = "error"
			} else if status >= 400 {
				record.Level = "warning"
			}
		}
	case "syslog":
		if m := syslog.FindStringSubmatch(raw); m != nil {
			t, err := time.ParseInLocation("2006 Jan _2 15:04:05", strconv.Itoa(now.Year())+" "+m[2], now.Location())
			pri, priErr := strconv.Atoi(m[1])
			if err == nil && (m[1] == "" || priErr == nil && pri >= 0 && pri <= 191) {
				if t.After(now.Add(24 * time.Hour)) {
					t = t.AddDate(-1, 0, 0)
				}
				record.Time, record.Message = t.Unix(), m[4]
				if m[1] != "" {
					record.Level = levels[pri%8]
				}
			}
		}
	case "json":
		var event struct {
			Time    string  `json:"time"`
			Level   string  `json:"level"`
			IP      string  `json:"ip"`
			Status  uint16  `json:"status"`
			Message *string `json:"message"`
		}
		if json.Unmarshal([]byte(raw), &event) == nil && event.Message != nil {
			if event.Time != "" {
				t, err := time.Parse(time.RFC3339, event.Time)
				if err != nil {
					return record
				}
				record.Time = t.Unix()
			}
			record.Message, record.IP, record.Status = *event.Message, event.IP, event.Status
			if event.Level != "" {
				record.Level = strings.ToLower(event.Level)
			}
		}
	}
	if record.Time < 0 || record.Time > 4294967295 {
		return fallback
	}
	return record
}

func splitAccess(raw string) (ip, stamp, request, code string) {
	head, rest, ok := strings.Cut(raw, " [")
	if !ok {
		return
	}
	fields := strings.Split(head, " ")
	if len(fields) != 3 || fields[1] == "" || fields[2] == "" {
		return
	}
	stamp, rest, ok = strings.Cut(rest, `] "`)
	if !ok {
		return "", "", "", ""
	}
	request, rest, ok = strings.Cut(rest, `"`)
	if !ok || !strings.HasPrefix(rest, " ") {
		return "", "", "", ""
	}
	code, _, _ = strings.Cut(rest[1:], " ")
	return fields[0], stamp, request, code
}
