package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	// Create background context listening for cancellation on SIGTERM and SIGINT
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ctx, cancelSig := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer cancelSig()

	// Initialize OpenTelemetry
	shutdown := initOpenTelemetry(ctx)
	defer shutdown()

	tracer := otel.Tracer("k8s-latency-probe")

	ctx, globalSpan := tracer.Start(ctx, "prober.main")
	defer globalSpan.End()

	// creates the in-cluster config
	config := must(rest.InClusterConfig())
	// creates the clientset
	clientset := must(kubernetes.NewForConfig(config))

	namespace := must(currentNamespace())

	buf := make([]byte, 8)
	_ = must(rand.Read(buf))
	instance := hex.EncodeToString(buf)

	// Create a new pod with a unique name
	_, createPodSpan := tracer.Start(ctx, "prober.create-pod")
	createPodSpan.SetAttributes(
		attribute.String("instance", instance),
	)

	pod := must(clientset.CoreV1().Pods(namespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("probe-%s", instance),
			Labels: map[string]string{
				"app": "probe",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "probe",
					Image: "busybox",
					Args:  []string{"sh", "-c", "while true; do echo hello; sleep 10;done"},
				},
			},
		},
	}, metav1.CreateOptions{}))

	fmt.Printf("Created pod %s\n", pod.Name)
	createPodSpan.End()

	found := make(chan struct{})
	go func(ctx context.Context) {
		ctx, span := tracer.Start(ctx, "prober.wait-for-pod")
		defer span.End()

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			// get pods in all the namespaces by omitting namespace
			// Or specify namespace to get pods in particular namespace
			pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("probe-instance=%s", instance),
			})
			if err != nil {
				panic(err.Error())
			}

			if len(pods.Items) > 0 {
				span.AddEvent("Pod found")
				found <- struct{}{}
				close(found)
				return
			}

			select {
			case <-ctx.Done():
				span.SetStatus(codes.Error, "context deadline exceeded")
				fmt.Println("Context done, exiting...")
				return
			case <-ticker.C:
			}
		}
	}(ctx)

	// Update the pod's labels
	_, updatePodSpan := tracer.Start(ctx, "prober.update-pod")
	_ = must(clientset.CoreV1().Pods(namespace).Patch(
		ctx,
		pod.Name,
		types.MergePatchType,
		fmt.Appendf(nil, "{\"metadata\":{\"labels\":{\"probe-instance\":\"%s\"}}}", instance),
		metav1.PatchOptions{},
	))
	updatePodSpan.End()

	select {
	case <-found:
		break
	case <-ctx.Done():
		fmt.Println("Context done, cleaning up and exiting...")
		break
	}

	_, cleanupSpan := tracer.Start(ctx, "prober.cleanup")

	err := clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("Deleted pod %s\n", pod.Name)
	cleanupSpan.End()
}

func must[V any](v V, e error) V {
	if e != nil {
		panic(e)
	}
	return v
}

// currentNamespace returns the namespace of the current pod.
func currentNamespace() (string, error) {
	// Get the namespace from the environment variable
	ns := os.Getenv("K8S_NAMESPACE_NAME")
	if ns != "" {
		return ns, nil
	}

	// If the environment variable is not set, read the namespace from the file
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", fmt.Errorf("failed to read namespace: %w", err)
	}

	return string(data), nil
}

// initOpenTelemetry initializes the OTLP exporter and tracer provider.
func initOpenTelemetry(ctx context.Context) func() {
	// Create OTLP trace exporter
	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient())
	if err != nil {
		panic(fmt.Sprintf("failed to create OTLP trace exporter: %v", err))
	}

	// Create a resource to describe this application
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("k8s-latency-probe"),
			semconv.ServiceVersionKey.String("0.0.1"),
		),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to create resource: %v", err))
	}

	// Create a trace provider with the exporter and resource
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)

	// Set the global tracer provider
	otel.SetTracerProvider(tp)

	// Return a shutdown function to flush and clean up
	return func() {
		if err := tp.Shutdown(ctx); err != nil {
			fmt.Printf("failed to shutdown tracer provider: %v\n", err)
		}
	}
}
