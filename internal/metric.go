package internal

import (
	"errors"
	"github.com/prometheus/client_golang/prometheus"
)

type Message struct {
	Name    string   `json:"name"`
	Labels  []string `json:"labels"`
	Command string   `json:"command"`
	Value   float64  `json:"value"`
}

type Metric struct {
	Namespace string   `json:"namespace"`
	Subsystem string   `json:"subsystem"`
	Type      string   `json:"type"`
	Name      string   `json:"name"`
	Labels    []string `json:"labels"`
	Help      string   `json:"help"`

	gaugeVec *prometheus.GaugeVec
	gauge    prometheus.Gauge

	counter    prometheus.Counter
	counterVec *prometheus.CounterVec
}

func (m *Metric) GetFullName() string {
	return m.Namespace + "_" + m.Subsystem + "_" + m.Name
}

func (m *Metric) SetNamespace(namespace string) {
	m.Namespace = namespace
}

func (m *Metric) SetSubsystem(subsystem string) {
	m.Subsystem = subsystem
}

func (m *Metric) Register() error {
	switch m.Type {
	case "gauge":
		if len(m.Labels) > 0 {
			m.gaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: m.Namespace,
				Subsystem: m.Subsystem,
				Name:      m.Name,
				Help:      m.Help,
			}, m.Labels)
			return prometheus.Register(m.gaugeVec)
		} else {
			m.gauge = prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: m.Namespace,
				Subsystem: m.Subsystem,
				Name:      m.Name,
				Help:      m.Help,
			})
			return prometheus.Register(m.gauge)
		}
	case "counter":
		if len(m.Labels) > 0 {
			m.counterVec = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: m.Namespace,
				Subsystem: m.Subsystem,
				Name:      m.Name,
				Help:      m.Help,
			}, m.Labels)
			return prometheus.Register(m.counterVec)
		} else {
			m.counter = prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: m.Namespace,
				Subsystem: m.Subsystem,
				Name:      m.Name,
				Help:      m.Help,
			})
			return prometheus.Register(m.counter)
		}
	default:
		return errors.New("no such metric type")
	}
}

func (m *Metric) HandleMessage(msg Message) error {
	switch m.Type {
	case "counter":
		if len(m.Labels) > 0 {
			if len(m.Labels) != len(msg.Labels) {
				return errors.New("invalid number of labels")
			}

			switch msg.Command {
			case "inc":
				m.counterVec.WithLabelValues(msg.Labels...).Inc()
			case "add":
				m.counterVec.WithLabelValues(msg.Labels...).Add(msg.Value)
			default:
				return errors.New("invalid command")
			}
		} else {
			switch msg.Command {
			case "inc":
				m.counter.Inc()
			case "add":
				m.counter.Add(msg.Value)
			default:
				return errors.New("invalid command")
			}
		}
	default:
		return errors.New("no such metric type")
	}

	return nil
}
