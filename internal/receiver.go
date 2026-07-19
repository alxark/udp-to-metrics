package internal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/prometheus/client_golang/prometheus"
	"log"
	"net"
	"sync"
)

type Receiver struct {
	log              *log.Logger
	udpPort          int
	defaultNamespace string
	defaultSubsystem string
	metrics          map[string]*Metric
	receiverMetric   *prometheus.CounterVec
	handledMetric    *prometheus.CounterVec
	conn             *net.UDPConn
	registered       []prometheus.Collector
	mu               sync.Mutex
	closed           bool
}

func NewReceiver(log *log.Logger, defaultNamespace string, defaultSubsystem string, udpPort int, metrics []Metric) (r *Receiver, err error) {
	r = &Receiver{}
	r.udpPort = udpPort
	r.defaultNamespace = defaultNamespace
	r.defaultSubsystem = defaultSubsystem
	r.metrics = make(map[string]*Metric)
	r.log = log
	r.receiverMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: r.defaultNamespace,
		Subsystem: r.defaultSubsystem,
		Name:      "receive_errors",
		Help:      "receive errors",
	}, []string{"error"})
	r.handledMetric = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: r.defaultNamespace,
		Subsystem: r.defaultSubsystem,
		Name:      "handled",
		Help:      "handled messages",
	}, []string{"metric"})

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
			Buckets:   metric.Buckets,
			Help:      metric.Help,
		}

		if err := r.metrics[metric.GetFullName()].Register(); err != nil {
			r.unregisterMetrics()
			return r, err
		}
		r.registered = append(r.registered, r.metrics[metric.GetFullName()].collector())

		r.log.Printf("registered metric: %s", metric.GetFullName())
	}

	if err := prometheus.Register(r.receiverMetric); err != nil {
		r.unregisterMetrics()
		return r, err
	}
	r.registered = append(r.registered, r.receiverMetric)
	if err := prometheus.Register(r.handledMetric); err != nil {
		r.unregisterMetrics()
		return r, err
	}
	r.registered = append(r.registered, r.handledMetric)

	return r, nil
}

func (r *Receiver) Listen() error {
	addr := net.UDPAddr{
		Port: r.udpPort,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		conn.Close()
		return errors.New("receiver is closed")
	}
	if r.conn != nil {
		conn.Close()
		return errors.New("receiver is already listening")
	}
	r.conn = conn
	return nil
}

// Run starts the receiver.
func (r *Receiver) Run(ctx context.Context) error {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn == nil {
		return errors.New("receiver is not listening")
	}

	r.log.Printf("starting receiver, total metrics: %d", len(r.metrics))

	buf := make([]byte, 2048)

	r.log.Printf("receiver started on %d", r.udpPort)

	for ctx.Err() == nil {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				break
			}
			r.log.Print(err.Error())
			r.receiverMetric.WithLabelValues("socket_read_error").Inc()
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				continue
			}
			return err
		}

		var msg Message
		err = json.Unmarshal(buf[:n], &msg)
		if err != nil {
			r.receiverMetric.WithLabelValues("json_unmarshal_error").Inc()
			continue
		}

		if msg.Name == "" {
			r.receiverMetric.WithLabelValues("empty_name").Inc()
			continue
		}

		if _, ok := r.metrics[msg.Name]; ok {
			r.handledMetric.WithLabelValues(msg.Name).Inc()

			if err := r.metrics[msg.Name].HandleMessage(msg); err != nil {
				r.receiverMetric.WithLabelValues("metric_handle_error").Inc()
			}
		} else {
			r.receiverMetric.WithLabelValues("metric_not_found").Inc()
		}
	}

	r.log.Print("receiver thread exited")

	return nil
}

func (r *Receiver) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	conn := r.conn
	r.mu.Unlock()

	if conn == nil {
		r.unregisterMetrics()
		return nil
	}
	err := conn.Close()
	r.unregisterMetrics()
	return err
}

func (r *Receiver) unregisterMetrics() {
	for _, collector := range r.registered {
		if collector != nil {
			prometheus.Unregister(collector)
		}
	}
	r.registered = nil
}
