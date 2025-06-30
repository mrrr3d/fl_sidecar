package main

import (
	"context"
	"fl_sidecar/exporter"
	"fl_sidecar/watcher"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	metricFile       string
	endpoint         string
	reporterInterval int
)

func main() {
	flag.StringVar(&metricFile, "metricfile", "", "Path to the metric file")
	flag.StringVar(&endpoint, "endpoint", "", "Target endpoint address")
	flag.IntVar(&reporterInterval, "interval", 60, "Reporter automatic push interval in seconds")
	flag.Parse()

	if metricFile == "" || endpoint == "" {
		fmt.Println("Error: -metricfile and -endpoint are required")
		flag.Usage()
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("exiting")
		cancel()
	}()

	reporter, err := exporter.NewReporter(ctx, endpoint, reporterInterval)
	if err != nil {
		log.Fatalf("Init metrics reporter failed: %v", err)
	}
	defer reporter.Shutdown(ctx)

	fileWatcher, err := watcher.New(metricFile)
	if err != nil {
		log.Fatalf("Init file watcher failed: %v", err)
	}

	updateChan := fileWatcher.Start(ctx)

	log.Printf("Start watching file %s", metricFile)

	for {
		select {
		case content, ok := <-updateChan:
			if !ok {
				log.Fatalf("Watcher channel closed")
				return
			}

			go parseAndPushMtrics(reporter, string(content))

		case <-ctx.Done():
			log.Printf("exiting")
			return
		}
	}
}

func parseAndPushMtrics(reporter *exporter.Reporter, content string) {
	var epoch int64
	var loss, accuracy float64

	_, err := fmt.Sscanf(content, "%d,%f,%f", &epoch, &loss, &accuracy)
	if err != nil {
		log.Printf("Parse content failed, content: %q, err: %v", string(content), err)
		return
	}

	reporter.UpdateMetrics(epoch, loss, accuracy)

	flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)

	err = reporter.ForceFlush(flushCtx)
	if err != nil {
		log.Printf("Force flush err: %v", err)
	} else {
		log.Printf("Push metrics success, epoch: %d, loss: %f, accuracy: %f", epoch, loss, accuracy)
	}

	flushCancel()
}
