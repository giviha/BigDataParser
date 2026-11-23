package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bigdata/api"
	"bigdata/internal/parser"
	"bigdata/internal/tail"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	name := flag.String("name", "agent-01", "agent name")
	source := flag.String("source", "json", "json, syslog, nginx or apache")
	file := flag.String("file", "/logs/01.log", "log file")
	target := flag.String("target", "collector:50051", "collector address")
	manual := flag.Bool("manual", true, "parse access logs without regular expressions")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	conn, err := grpc.NewClient(*target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := api.NewCollectorClient(conn)
	lines := make(chan string)
	readError := make(chan error, 1)
	go func() {
		readError <- tail.Follow(ctx, *file, func() { log.Printf("ready %s", *file) }, func(line string) error {
			select {
			case lines <- line:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	batch := &api.Batch{Records: make([]*api.LogRecord, 0, 500)}
	for {
		select {
		case <-ctx.Done():
			return
		case err := <-readError:
			if ctx.Err() == nil {
				log.Fatal(err)
			}
			return
		case line := <-lines:
			row := parser.Parse(line, *name, *source, time.Now().UTC(), *manual)
			batch.Records = append(batch.Records, &api.LogRecord{Time: row.Time, Agent: row.Agent, Source: row.Source, Level: row.Level, Ip: row.IP, Status: uint32(row.Status), Message: row.Message})
			if len(batch.Records) < cap(batch.Records) {
				continue
			}
		case <-ticker.C:
			if len(batch.Records) == 0 {
				continue
			}
		}
		if !send(ctx, client, batch) {
			return
		}
		clear(batch.Records)
		batch.Records = batch.Records[:0]
		// Acknowledgment can take longer than a tick. Start a new flush interval
		// so a stale tick cannot send the first record of the next batch alone.
		ticker.Reset(time.Second)
	}
}

func send(ctx context.Context, client api.CollectorClient, batch *api.Batch) bool {
	delay := time.Second
	for ctx.Err() == nil {
		call, cancel := context.WithTimeout(ctx, 15*time.Second)
		reply, err := client.Send(call, batch)
		cancel()
		if err == nil && reply.GetOk() {
			return true
		}
		log.Printf("batch of %d pending, retry in %s: %v", len(batch.Records), delay, err)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
	return false
}
