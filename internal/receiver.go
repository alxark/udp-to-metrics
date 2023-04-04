package internal

import (
	"context"
	"encoding/json"
	"github.com/prometheus/client_golang/prometheus"
	"log"
	"net"
)

type Receiver struct {
	log              *log.Logger
	udpPort          int
	defaultNamespace string
	defaultSubsystem string
	metrics          map[string]*Metric
}

func NewReceiver(log *log.Logger, defaultNamespace string, defaultSubsystem string, udpPort int, metrics []Metric) (r *Receiver, err error) {
	r = &Receiver{}
	r.udpPort = udpPort
	r.defaultNamespace = defaultNamespace
	r.defaultSubsystem = defaultSubsystem
	r.metrics = make(map[string]*Metric)
	r.log = log

	// registering new metrics
	for _, metric := range metrics {
		if metric.Namespace == "" {
			metric.SetNamespace(r.defaultNamespace)
		}

		if metric.Subsystem == "" {
			metric.SetSubsystem(r.defaultSubsystem)
		}

		r.metrics[metric.GetFullName()] = &Metric{
			Namespace: metric.Namespace,
			Subsystem: metric.Subsystem,
			Type:      metric.Type,
			Name:      metric.Name,
			Labels:    metric.Labels,
		}

		if err := r.metrics[metric.GetFullName()].Register(); err != nil {
			return r, err
		}

		r.log.Printf("registered metric: %s", metric.GetFullName())
	}

	return r, nil
}

// Run starts the receiver
func (r *Receiver) Run(ctx context.Context) (err error) {
	r.log.Printf("starting receiver, total metrics: %d", len(r.metrics))

	var receiverMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: r.defaultNamespace,
		Subsystem: r.defaultSubsystem,
		Name:      "receive_errors",
		Help:      "receive errors",
	}, []string{"error"})

	var handledMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: r.defaultNamespace,
		Subsystem: r.defaultSubsystem,
		Name:      "handled",
		Help:      "handled messages",
	}, []string{"metric"})

	prometheus.MustRegister(receiverMetric, handledMetric)

	addr := net.UDPAddr{
		Port: r.udpPort,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := make([]byte, 2048)

	r.log.Printf("receiver started on %d", r.udpPort)

	for ctx.Err() == nil {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			r.log.Print(err.Error())
			receiverMetric.WithLabelValues("socket_read_error").Inc()
			continue
		}

		// r.log.Print(string(buf))

		var msg Message
		err = json.Unmarshal(buf[:n], &msg)
		if err != nil {
			receiverMetric.WithLabelValues("json_unmarshal_error").Inc()
			continue
		}

		if msg.Name == "" {
			receiverMetric.WithLabelValues("empty_name").Inc()
			continue
		}

		if _, ok := r.metrics[msg.Name]; ok {
			handledMetric.WithLabelValues(msg.Name).Inc()

			if err := r.metrics[msg.Name].HandleMessage(msg); err != nil {
				receiverMetric.WithLabelValues("metric_handle_error").Inc()
			}
		} else {
			receiverMetric.WithLabelValues("metric_not_found").Inc()
		}
	}

	r.log.Print("receiver thread exited")

	return nil
}
