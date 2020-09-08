package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "netgear_cm"

var (
	version   string
	revision  string
	branch    string
	buildUser string
	buildDate string
)

// Status Exporter represents an instance of the Netgear cable modem exporter.
type StatusExporter struct {
	url, authHeaderValue string

	mu sync.Mutex

	// Exporter metrics.
	totalScrapes prometheus.Counter
	scrapeErrors prometheus.Counter

	// Downstream metrics.
	dsChannelSNR               *prometheus.Desc
	dsChannelPower             *prometheus.Desc
	dsChannelCorrectableErrs   *prometheus.Desc
	dsChannelUncorrectableErrs *prometheus.Desc

	// Upstream metrics.
	usChannelPower      *prometheus.Desc
	usChannelSymbolRate *prometheus.Desc
}

// basicAuth returns the base64 encoding of the username and password
// separated by a colon. Borrowed the net/http package.
func basicAuth(username, password string) string {
	auth := fmt.Sprintf("%s:%s", username, password)
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

type CMExporter interface {
	Collect(ch chan<- prometheus.Metric)

	Describe(ch chan<- *prometheus.Desc)
}

// Returns an instance of StatusExporter configured with the modem's
// address, admin username and password.
func NewStatusExporter(addr, username, password string) StatusExporter {
	var (
		dsLabelNames = []string{"channel", "lock_status", "modulation", "channel_id", "frequency"}
		usLabelNames = []string{"channel", "lock_status", "channel_type", "channel_id", "frequency"}
	)

	return StatusExporter{
		// Modem access details.
		url:             "http://" + addr + "/DocsisStatus.asp",
		authHeaderValue: "Basic " + basicAuth(username, password),

		// Collection metrics.
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "status_scrapes_total",
			Help:      "Total number of scrapes of the modem status page.",
		}),
		scrapeErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "status_scrape_errors_total",
			Help:      "Total number of failed scrapes of the modem status page.",
		}),

		// Downstream metrics.
		dsChannelSNR: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "downstream_channel", "snr_db"),
			"Downstream channel signal to noise ratio in dB.",
			dsLabelNames, nil,
		),
		dsChannelPower: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "downstream_channel", "power_dbmv"),
			"Downstream channel power in dBmV.",
			dsLabelNames, nil,
		),
		dsChannelCorrectableErrs: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "downstream_channel", "correctable_errors_total"),
			"Downstream channel correctable errors.",
			dsLabelNames, nil,
		),
		dsChannelUncorrectableErrs: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "downstream_channel", "uncorrectable_errors_total"),
			"Downstream channel uncorrectable errors.",
			dsLabelNames, nil,
		),

		// Upstream metrics.
		usChannelPower: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "upstream_channel", "power_dbmv"),
			"Upstream channel power in dBmV.",
			usLabelNames, nil,
		),
		usChannelSymbolRate: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "upstream_channel", "symbol_rate"),
			"Upstream channel symbol rate per second",
			usLabelNames, nil,
		),
	}
}

func NewStatusExporterFactory(addr, username, password string, modemType string) CMExporter {

	switch modemType {
	case "CM600":
		return &CM600StatusExporter{StatusExporter: NewStatusExporter(addr, username, password)}
	case "CM1000":
		return &CM1000StatusExporter{StatusExporter: NewStatusExporter(addr, username, password)}
	default:
		log.Println("The modem type" + modemType + " is not known. Defaulting to CM600")
		return &CM600StatusExporter{StatusExporter: NewStatusExporter(addr, username, password)}
	}

}

func main() {
	var (
		configFile  = flag.String("config.file", "netgear_cm_exporter.yml", "Path to configuration file.")
		showVersion = flag.Bool("version", false, "Print version information.")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("netgear_cm_exporter version=%s revision=%s branch=%s buildUser=%s buildDate=%s\n",
			version, revision, branch, buildUser, buildDate)
		os.Exit(0)
	}

	config, err := NewConfigFromFile(*configFile)
	if err != nil {
		log.Fatal(err)
	}

	//		exporter := NewEventExporterFactory( config.Modem.Address, config.Modem.Username, config.Modem.Password )

	exporter := NewStatusExporterFactory(config.Modem.Address, config.Modem.Username, config.Modem.Password, config.Modem.Model)

	prometheus.MustRegister(exporter)

	http.Handle(config.Telemetry.MetricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, config.Telemetry.MetricsPath, http.StatusMovedPermanently)
	})

	log.Printf("exporter listening on %s", config.Telemetry.ListenAddress)
	if err := http.ListenAndServe(config.Telemetry.ListenAddress, nil); err != nil {
		log.Fatalf("failed to start netgear exporter: %s", err)
	}
}
