package main

import (
	"context"
	"fmt"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"net/http"
	"os"
)

const (
	namespace = "service_ready"
)

var (
	readyLabels = []string{"namespace", "name", "host"}
)

type Ingresses []Ingress

type Ingress struct {
	namespace string
	name      string
	host      string
}

type Exporter struct {
	up *prometheus.Desc
}

func NewExporter(logger log.Logger) *Exporter {
	return &Exporter{
		up: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "up"),
			"Ready check is ok.",
			readyLabels,
			nil,
		),
	}
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- e.up
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	client, err := k8s("~/.kube/config")
	if err != nil {
		panic(err)
	}

	for _, ingress := range getIngresses(client) {
		namespace, name, host, status := getReadyz(ingress)
		ch <- prometheus.MustNewConstMetric(e.up, prometheus.GaugeValue, float64(status), namespace, name, host)
	}
}

func k8s(kubeconfig string) (*kubernetes.Clientset, error) {
	var config *rest.Config
	var err error

	if os.Getenv("KUBECONFIG_CONTENT") != "" {
		config, err = clientcmd.RESTConfigFromKubeConfig([]byte(os.Getenv("KUBECONFIG_CONTENT")))
		if err != nil {
			return nil, err
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				return nil, err
			}
		}
	}
	return kubernetes.NewForConfig(config)
}

func getIngresses(client *kubernetes.Clientset) Ingresses {
	var ingresses Ingresses

	ctx := context.Background()

	namespaces, err := client.CoreV1().Namespaces().List(ctx, v1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, namespace := range namespaces.Items {
		ings, err := client.NetworkingV1beta1().Ingresses(namespace.Name).List(ctx, v1.ListOptions{})
		if err != nil {
			panic(err)
		}

		for _, ing := range ings.Items {
			for _, rule := range ing.Spec.Rules {
				var ingress Ingress
				ingress.namespace = namespace.Name
				ingress.name = ing.Name
				ingress.host = rule.Host
				ingresses = append(ingresses, ingress)
			}
		}
	}
	return ingresses
}

func getReadyz(ingress Ingress) (string, string, string, int) {
	resp, err := http.Get(fmt.Sprintf("http://%s/readyz", ingress.host))
	if err != nil {
		return ingress.namespace, ingress.name, ingress.host, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode > 299 {
		return ingress.namespace, ingress.name, ingress.host, 0
	}
	return ingress.namespace, ingress.name, ingress.host, 1
}

func main() {
	var (
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9150").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	)
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	level.Info(logger).Log("msg", "Starting memcached_exporter", "version", version.Info())
	level.Info(logger).Log("msg", "Build context", "context", version.BuildContext())

	prometheus.MustRegister(NewExporter(logger))

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Service Ready Exporter</title></head>
             <body>
             <h1>Service Ready Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})

	level.Info(logger).Log("msg", "Listening on address", "address", *listenAddress)
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		level.Error(logger).Log("msg", "Error running HTTP server", "err", err)
		os.Exit(1)
	}
}
