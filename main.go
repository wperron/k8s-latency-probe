package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/signal"
	"syscall"
	"time"

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

	// creates the in-cluster config
	config := must(rest.InClusterConfig())
	// creates the clientset
	clientset := must(kubernetes.NewForConfig(config))

	buf := make([]byte, 8)
	_ = must(rand.Read(buf))
	instance := hex.EncodeToString(buf)

	pod := must(clientset.CoreV1().Pods("default").Create(ctx, &corev1.Pod{
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

	found := make(chan struct{})
	go func(ctx context.Context) {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				fmt.Println("Context done, exiting...")
				return
			case <-ticker.C:
				// get pods in all the namespaces by omitting namespace
				// Or specify namespace to get pods in particular namespace
				pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
					LabelSelector: fmt.Sprintf("probe-instance=%s", instance),
				})
				if err != nil {
					panic(err.Error())
				}

				if len(pods.Items) > 0 {
					found <- struct{}{}
					close(found)
					return
				}
			}
		}
	}(ctx)

	done := make(chan struct{})
	go func(ctx context.Context) {
		start := time.Now()

		select {
		case <-ctx.Done():
			fmt.Println("context deadline expired before pod was found...")
			return
		case <-found:
			fmt.Printf("pod found latency_ms=%d\n", time.Since(start).Milliseconds())
			fmt.Println("deleting pod...")

			err := clientset.CoreV1().Pods("default").Delete(ctx, pod.Name, metav1.DeleteOptions{})
			if err != nil {
				panic(err.Error())
			}
			fmt.Printf("Deleted pod %s\n", pod.Name)
			done <- struct{}{}
		}
	}(ctx)

	// Update the pod's labels
	_ = must(clientset.CoreV1().Pods("default").Patch(
		ctx,
		pod.Name,
		types.MergePatchType,
		fmt.Appendf(nil, "{\"metadata\":{\"labels\":{\"probe-instance\":\"%s\"}}}", instance),
		metav1.PatchOptions{},
	))

	select {
	case <-done:
		break
	case <-ctx.Done():
		break
	}
	fmt.Println("Context done, exiting...")
}

func must[V any](v V, e error) V {
	if e != nil {
		panic(e)
	}
	return v
}
