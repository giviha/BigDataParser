package main

import (
	"context"
	"encoding/json"
	"flag"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"bigdata/api"
	"bigdata/internal/parser"
	"bigdata/internal/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type request struct {
	rows []parser.Record
	done chan error
}

type collector struct {
	api.UnimplementedCollectorServer
	db    storage.DB
	queue chan request
	saved atomic.Uint64
	rate  atomic.Uint64
}

func (c *collector) Send(ctx context.Context, batch *api.Batch) (*api.Reply, error) {
	if len(batch.GetRecords()) == 0 || len(batch.GetRecords()) > 500 {
		return nil, status.Error(codes.InvalidArgument, "batch size must be 1–500")
	}
	req := request{rows: make([]parser.Record, 0, len(batch.GetRecords())), done: make(chan error, 1)}
	for _, row := range batch.GetRecords() {
		if row.GetTime() < 0 || row.GetTime() > 4294967295 || row.GetStatus() > 65535 || row.GetAgent() == "" {
			return nil, status.Error(codes.InvalidArgument, "invalid log record")
		}
		req.rows = append(req.rows, parser.Record{Time: row.Time, Agent: row.Agent, Source: row.Source, Level: row.Level, IP: row.Ip, Status: uint16(row.Status), Message: row.Message})
	}
	select {
	case c.queue <- req:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	select {
	case err := <-req.done:
		if err != nil {
			return nil, status.Error(codes.Unavailable, "ClickHouse insert failed")
		}
		return &api.Reply{Ok: true}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func (c *collector) writer() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	rows := make([]parser.Record, 0, 4500)
	var waiting []request
	flush := func() {
		if len(rows) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err := c.db.Insert(ctx, rows)
		cancel()
		if err == nil {
			c.saved.Add(uint64(len(rows)))
		} else {
			log.Printf("insert: %v", err)
		}
		for _, req := range waiting {
			req.done <- err
		}
		clear(rows)
		clear(waiting)
		rows = rows[:0]
		waiting = waiting[:0]
	}
	for {
		select {
		case req := <-c.queue:
			rows = append(rows, req.rows...)
			waiting = append(waiting, req)
			if len(rows) >= 4000 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func main() {
	endpoint := flag.String("clickhouse", "http://clickhouse:8123", "ClickHouse HTTP endpoint")
	grpcAddr := flag.String("grpc", ":50051", "gRPC listen address")
	webAddr := flag.String("web", ":8080", "web listen address")
	flag.Parse()
	c := &collector{db: storage.DB{URL: *endpoint, Client: &http.Client{Timeout: 10 * time.Second}}, queue: make(chan request, 32)}
	go c.writer()
	go func() {
		var prev uint64
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			current := c.saved.Load()
			c.rate.Store(current - prev)
			prev = current
		}
	}()
	listener, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatal(err)
	}
	server := grpc.NewServer(grpc.MaxRecvMsgSize(40 << 20))
	api.RegisterCollectorServer(server, c)
	go func() { log.Fatal(server.Serve(listener)) }()

	page := template.Must(template.New("logs").Funcs(template.FuncMap{"date": func(ts int64) string { return time.Unix(ts, 0).UTC().Format("2006-01-02 15:04:05") }}).Parse(pageHTML))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /metrics.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(struct {
			Rate  uint64 `json:"rate"`
			Saved uint64 `json:"saved"`
		}{c.rate.Load(), c.saved.Load()})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		filters := r.URL.Query()
		for _, key := range []string{"from", "to"} {
			if value := filters.Get(key); value != "" {
				if _, err := time.Parse("2006-01-02T15:04", value); err != nil {
					http.Error(w, "Некорректная дата", http.StatusBadRequest)
					return
				}
			}
		}
		rows, err := c.db.Search(r.Context(), filters)
		if err != nil {
			log.Print(err)
			http.Error(w, "Ошибка чтения ClickHouse", http.StatusBadGateway)
			return
		}
		counts, err := c.db.Counts(r.Context())
		if err != nil {
			log.Print(err)
			http.Error(w, "Ошибка подсчёта записей", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		model := struct {
			Rows    []parser.Record
			Counts  map[string]uint64
			Filters url.Values
			Rate    uint64
			Saved   uint64
		}{rows, counts, filters, c.rate.Load(), c.saved.Load()}
		if err := page.Execute(w, model); err != nil {
			log.Print(err)
		}
	})
	httpServer := &http.Server{Addr: *webAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Web %s, gRPC %s", *webAddr, *grpcAddr)
	log.Fatal(httpServer.ListenAndServe())
}
