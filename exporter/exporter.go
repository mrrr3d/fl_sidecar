package exporter

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

type Reporter struct {
	provider *sdkmetric.MeterProvider

	mu       sync.Mutex
	counter  int64
	loss     float64
	accuracy float64
}

func NewReporter(ctx context.Context, endpoint string, interval int) (*Reporter, error) {
	exporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(endpoint),
	)
	if err != nil {
		log.Fatalf("Create OTLP metric exporter failed: %v", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Duration(interval)*time.Second))),
	)

	meter := meterProvider.Meter("fl-train-events")

	r := &Reporter{
		provider: meterProvider,
	}

	lossGauge, err := meter.Float64ObservableGauge("fl.training.epoch.loss")
	if err != nil {
		meterProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create loss gauge failed: %w", err)
	}

	accuracyGauge, err := meter.Float64ObservableGauge("fl.training.epoch.accuracy")
	if err != nil {
		meterProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create accuracy gauge failed: %w", err)
	}

	epochGauge, err := meter.Int64ObservableGauge("fl.training.epoch.counter")
	if err != nil {
		meterProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create epoch gauge failed: %w", err)
	}

	_, err = meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			log.Printf("Meter callback triggered, Observing Epoch: %d, Loss: %.4f, Accuracy: %.4f", r.counter, r.loss, r.accuracy)
			r.mu.Lock()
			o.ObserveInt64(epochGauge, r.counter)
			o.ObserveFloat64(accuracyGauge, r.accuracy)
			o.ObserveFloat64(lossGauge, r.loss)
			r.mu.Unlock()
			return nil
		},
		epochGauge,
		accuracyGauge,
		lossGauge,
	)
	if err != nil {
		meterProvider.Shutdown(ctx)
		return nil, fmt.Errorf("register callback failed: %w", err)
	}

	log.Println("Init metric reporter success")
	return r, nil
}

func (r *Reporter) UpdateMetrics(counter int64, loss, accuracy float64) {
	r.mu.Lock()
	r.counter = counter
	r.loss = loss
	r.accuracy = accuracy
	r.mu.Unlock()
}

func (r *Reporter) Shutdown(ctx context.Context) error {
	return r.provider.Shutdown(ctx)
}

func (r *Reporter) ForceFlush(ctx context.Context) error {
	return r.provider.ForceFlush(ctx)
}
