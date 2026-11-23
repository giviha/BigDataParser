package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func main() {
	dir := flag.String("dir", "/logs", "output directory")
	index := flag.Int("index", 1, "source number")
	source := flag.String("source", "json", "log format")
	seconds := flag.Int("seconds", 10, "duration")
	rate := flag.Int("rate", 100, "lines per second")
	run := flag.String("run", "demo", "label included in events")
	initOnly := flag.Bool("init", false, "create file and exit")
	flag.Parse()
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(*run) {
		log.Fatal("invalid run label")
	}
	if *index < 1 || *index > 15 || *rate < 1 || *rate > 100000 || *seconds < 1 || *seconds > 3600 {
		log.Fatal("invalid index, rate or duration")
	}
	if *source != "json" && *source != "syslog" && *source != "nginx" {
		log.Fatal("invalid source")
	}
	path := filepath.Join(*dir, fmt.Sprintf("%02d.log", *index))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	if *initOnly {
		return
	}
	writer := bufio.NewWriterSize(file, 64<<10)
	start := time.Now()
	written := 0
	for tick := 1; tick <= *seconds*20; tick++ {
		time.Sleep(time.Until(start.Add(time.Duration(tick) * 50 * time.Millisecond)))
		stamp := time.Now().UTC()
		for written < tick*(*rate)/20 {
			switch *source {
			case "json":
				_, err = fmt.Fprintf(writer, `{"time":"%s","level":"info","message":%q,"ip":"192.0.2.1"}`+"\n", stamp.Format(time.RFC3339), fmt.Sprintf("run %s event %d", *run, written))
			case "syslog":
				_, err = fmt.Fprintf(writer, "<14>%s service app: run %s event %d\n", stamp.Format("Jan _2 15:04:05"), *run, written)
			case "nginx":
				_, err = fmt.Fprintf(writer, `192.0.2.1 - - [%s] "GET /%s/%d HTTP/1.1" 200 42`+"\n", stamp.Format("02/Jan/2006:15:04:05 -0700"), *run, written)
			}
			if err != nil {
				log.Fatal(err)
			}
			written++
		}
		if err := writer.Flush(); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("source=%02d format=%s written=%d seconds=%.3f\n", *index, *source, written, time.Since(start).Seconds())
}
