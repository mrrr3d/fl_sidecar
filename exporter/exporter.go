package exporter

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

type Reporter struct {
	provider             *sdkmetric.MeterProvider
	meter                metric.Meter
	mu                   sync.Mutex
	metrics              map[string]float64
	gauges               map[string]metric.Float64ObservableGauge
	callbackRegistration metric.Registration
}

func NewReporter(ctx context.Context, endpoint string, interval int) (*Reporter, error) {
	exporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP metric exporter: %w", err)
	}

	podName := os.Getenv("POD_NAME")
	namespace := os.Getenv("POD_NAMESPACE")

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("fl_sidecar"),
			semconv.K8SNamespaceNameKey.String(namespace),
			semconv.K8SPodNameKey.String(podName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Duration(interval)*time.Second))),
	)

	meter := meterProvider.Meter("fl-train-events")

	r := &Reporter{
		provider: meterProvider,
		meter:    meter,
		metrics:  make(map[string]float64),
		gauges:   make(map[string]metric.Float64ObservableGauge),
	}

	log.Println("Init metric reporter success")
	return r, nil
}

func (r *Reporter) UpdateMetrics(newMetrics map[string]float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.metrics = newMetrics

	needsUpdate := false
	if len(r.metrics) != len(r.gauges) {
		needsUpdate = true
	} else {
		for name := range r.metrics {
			if _, ok := r.gauges[name]; !ok {
				needsUpdate = true
				break
			}
		}
	}

	if !needsUpdate {
		return
	}

	log.Println("Metric set changed, re-registering callback")

	if r.callbackRegistration != nil {
		if err := r.callbackRegistration.Unregister(); err != nil {
			log.Printf("Error unregistering callback: %v", err)
		}
	}

	r.gauges = make(map[string]metric.Float64ObservableGauge, len(r.metrics))
	instruments := make([]metric.Observable, 0, len(r.metrics))

	for name := range r.metrics {
		gauge, err := r.meter.Float64ObservableGauge("fl.training." + name)
		if err != nil {
			log.Printf("Error creating gauge for %s: %v", name, err)
			continue
		}
		r.gauges[name] = gauge
		instruments = append(instruments, gauge)
	}

	if len(instruments) == 0 {
		return
	}

	registration, err := r.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			for name, gauge := range r.gauges {
				if value, ok := r.metrics[name]; ok {
					o.ObserveFloat64(gauge, value)
				}
			}
			return nil
		},
		instruments...,
	)
	if err != nil {
		log.Printf("Error registering callback: %v", err)
		return
	}

	r.callbackRegistration = registration
}

func (r *Reporter) Shutdown(ctx context.Context) error {
	return r.provider.Shutdown(ctx)
}

func (r *Reporter) ForceFlush(ctx context.Context) error {
	return r.provider.ForceFlush(ctx)
}
