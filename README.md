# k8s-latency-probe

`k8s-latency-probe` is a Kubernetes utility designed to measure latency in
Kubernetes clusters by creating, updating, and deleting pods while collecting
telemetry data using OpenTelemetry.

## Features

- Creates a pod in the cluster and measures the time taken for various
  operations.
- Updates the pod's metadata and tracks the latency.
- Deletes the pod and ensures cleanup.
- Exports telemetry data using OpenTelemetry's OTLP exporter.
- Designed to run as a Kubernetes CronJob for periodic latency measurements.

## Prerequisites

- Kubernetes cluster with access to the API server.
- OpenTelemetry Collector or compatible backend for receiving telemetry data.
- Docker for building the container image.

## Installation

### 1. Build the Docker Image

Build the Docker image using the provided `Dockerfile`:

```bash
docker build -t ghcr.io/<your-username>/k8s-latency-probe:latest .

### 2. Deploy to Kubernetes
Apply the provided probe.yaml manifest to your cluster:

```bash
kubectl apply -f probe.yaml
```

This will create the necessary ServiceAccount, ClusterRole, ClusterRoleBinding,
and a CronJob to run the probe periodically.

## Usage

The k8s-latency-probe runs as a CronJob in Kubernetes. By default, it runs every
minute as specified in the probe.yaml file. You can modify the schedule by
editing the schedule field in the CronJob spec.

### Environment Variables

- `K8S_NAMESPACE_NAME`: The namespace in which the probe operates. If not set,
  it defaults to the namespace of the pod.

## Telemetry

The probe uses OpenTelemetry to export trace data. It is configured to use the
OTLP exporter. Ensure you have an OpenTelemetry Collector or compatible backend
running and accessible from the cluster.

### Example Trace

The following spans are recorded during the probe's execution:

1. `prober.main`: The main span for the probe's execution.
2. `prober.create-pod`: Measures the time taken to create a pod.
3. `prober.wait-for-pod`: Measures the time taken for the pod to become
   available.
4. `prober.update-pod`: Measures the time taken to update the pod's metadata.
5. `prober.cleanup`: Measures the time taken to delete the pod.

## Development

### Requirements

- Go 1.24 or later
- Docker

## License

This project is licensed under the MIT License. See the LICENSE file for
details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

## Acknowledgments

This project uses the following libraries:

- OpenTelemetry
- Kubernetes Go Client
