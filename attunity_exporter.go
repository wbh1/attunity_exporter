package main

import (
	"io/ioutil"
	"net/http"

	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/wbh1/attunity_exporter/collector"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	cfg     = collector.Config{}
	port    = kingpin.Flag("port", "the port to bind the exporter to.").Default("9103").String()
	cfgFile = kingpin.Flag("config-file", "path the config.yml file").Default("config.yml").String()
	debug   = kingpin.Flag("debug", "Enable debug logging").Bool()
)

func main() {
	kingpin.Version("v1.1.0")
	kingpin.Parse()

	if *debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	f, err := ioutil.ReadFile(*cfgFile)
	if err != nil {
		logrus.Fatal(err)
	}

	if err = yaml.UnmarshalStrict(f, &cfg); err != nil {
		logrus.Fatal(err)
	}

	prometheus.MustRegister(collector.NewAttunityCollector(&cfg))

	http.Handle("/metrics", promhttp.Handler())

	logrus.Info("Starting attunity_exporter on :", *port)

	// Run in a goroutine so we can log that it actually started
	ch := make(chan error)
	go func() {
		ch <- http.ListenAndServe(":"+*port, nil)
	}()
	logrus.Info("Started attunity_exporter on :", *port)

	// Wait for goroutine to return a value to the channel
	logrus.Fatal(<-ch)
}
