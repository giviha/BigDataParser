package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"bigdata/internal/parser"
)

type DB struct {
	URL    string
	Client *http.Client
}

func (db DB) query(ctx context.Context, sql string, params url.Values, body io.Reader) (io.ReadCloser, error) {
	endpoint, err := url.Parse(db.URL)
	if err != nil {
		return nil, err
	}
	values := endpoint.Query()
	values.Set("query", sql)
	values.Set("wait_end_of_query", "1")
	values.Set("output_format_json_quote_64bit_integers", "0")
	for key, entries := range params {
		values.Set("param_"+key, entries[0])
	}
	endpoint.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return nil, err
	}
	response, err := db.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("ClickHouse %s: %s", response.Status, detail)
	}
	return response.Body, nil
}

func (db DB) Insert(ctx context.Context, records []parser.Record) error {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	for _, record := range records {
		if err := enc.Encode(record); err != nil {
			return err
		}
	}
	response, err := db.query(ctx, "INSERT INTO logs (time,agent,source,level,ip,status,message) FORMAT JSONEachRow", nil, &body)
	if err != nil {
		return err
	}
	defer response.Close()
	_, err = io.Copy(io.Discard, response)
	return err
}

func (db DB) Search(ctx context.Context, filters url.Values) ([]parser.Record, error) {
	params := url.Values{}
	conditions := []string{"1"}
	for _, name := range []string{"agent", "source", "level", "text", "from", "to"} {
		value := filters.Get(name)
		if value == "" {
			continue
		}
		switch name {
		case "from", "to":
			stamp, err := time.Parse("2006-01-02T15:04", value)
			if err != nil {
				return nil, fmt.Errorf("некорректное время %s", name)
			}
			operator := ">="
			if name == "to" {
				operator = "<="
			}
			conditions = append(conditions, "time "+operator+" {"+name+":DateTime('UTC')}")
			value = stamp.Format("2006-01-02 15:04:05")
		case "text":
			conditions = append(conditions, "positionCaseInsensitiveUTF8(message,{text:String}) > 0")
		default:
			conditions = append(conditions, name+" = {"+name+":String}")
		}
		params.Set(name, value)
	}
	sql := "SELECT toUnixTimestamp(time) AS time, agent, source, level, ip, status, message FROM logs WHERE " + strings.Join(conditions, " AND ") + " ORDER BY logs.time DESC LIMIT 100 FORMAT JSONEachRow"
	body, err := db.query(ctx, sql, params, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	records := make([]parser.Record, 0)
	dec := json.NewDecoder(body)
	for {
		var record parser.Record
		if err := dec.Decode(&record); err == io.EOF {
			return records, nil
		} else if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
}

func (db DB) Counts(ctx context.Context) (map[string]uint64, error) {
	body, err := db.query(ctx, "SELECT agent, count() AS total FROM logs GROUP BY agent FORMAT JSONEachRow", nil, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	counts := make(map[string]uint64)
	dec := json.NewDecoder(body)
	for {
		var row struct {
			Agent string
			Total uint64
		}
		if err := dec.Decode(&row); err == io.EOF {
			return counts, nil
		} else if err != nil {
			return nil, err
		}
		counts[row.Agent] = row.Total
	}
}
