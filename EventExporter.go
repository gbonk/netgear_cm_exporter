package main

import (
	"encoding/xml"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type EventExporter struct {
	url, authHeaderValue string

	mu sync.Mutex

	// Exporter metrics.
	totalEventScrapes prometheus.Counter
	scrapeEventErrors prometheus.Counter

	eventTime        *prometheus.Desc
	eventPriority    *prometheus.Desc
	eventDescription *prometheus.Desc
}

// NewExporter returns an instance of Exporter configured with the modem's
// address, admin username and password.
func NewEventExporter(addr, username, password string) *EventExporter {
	var (
		dsLabelNames = []string{"time", "priority", "description"}
	)

	return &EventExporter{
		// Modem access details.
		url:             "http://" + addr + "/EventLog.asp",
		authHeaderValue: "Basic " + basicAuth(username, password),

		// Collection metrics.
		totalEventScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "status_event_scrapes_total",
			Help:      "Total number of scrapes of the modem event page.",
		}),
		scrapeEventErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "status_event_scrape_errors_total",
			Help:      "Total number of failed scrapes of the modem event page.",
		}),

		// Events.
		eventTime: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "event", "time"),
			"Time of the Event.",
			dsLabelNames, nil,
		),
		eventPriority: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "event", "priority"),
			"Priority of the Event.",
			dsLabelNames, nil,
		),
		eventDescription: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "event", "description"),
			"Description of the Event.",
			dsLabelNames, nil,
		),
	}
}

// Describe returns Prometheus metric descriptions for the exporter metrics.
func (e *EventExporter) Describe(ch chan<- *prometheus.Desc) {
	// Exporter metrics.
	ch <- e.totalEventScrapes.Desc()
	ch <- e.scrapeEventErrors.Desc()
	// Event Data.
	ch <- e.eventTime
	ch <- e.eventPriority
	ch <- e.eventDescription
}

func parseXMLTable(value string, a string, b string) string {
	// Get substring between two strings.
	posFirst := strings.Index(value, a)
	if posFirst == -1 {
		return ""
	}
	posLast := strings.Index(value, b)
	if posLast == -1 {
		return ""
	}
	posLastAdjusted := posLast + len(b)

	return value[posFirst:posLastAdjusted]
}

type EventRow struct {
	XMLName xml.Name `xml:"tr"`

	EventIndex     string `xml:"docsDevEvIndex"`
	EventFirstTime string `xml:"docsDevEvFirstTime"`
	EventLastTime  string `xml:"docsDevEvLastTime"`
	EventCounts    int    `xml:"docsDevEvCounts"`
	EventLevel     string `xml:"docsDevEvLevel"`
	EventId        int    `xml:"docsDevEvId"`
	EventText      string `xml:"docsDevEvText"`
}

type EventTable struct {
	XMLName xml.Name `xml:"docsDevEventTable"`

	EventRows []EventRow `xml:"tr"`
}

// Collect runs our scrape loop returning each Prometheus metric.
func (e *EventExporter) Collect(ch chan<- prometheus.Metric) {

	e.totalEventScrapes.Inc()

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", e.url, nil)

	if err != nil {
		log.Println("Error setting request auth header")
		log.Println(err)
		return
	}

	req.Header.Add("Authorization", e.authHeaderValue)

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Error Calling Server for Events.")
		log.Println(err)
		return
	}

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		log.Println(err)
		return
	}

	xmlData := parseXMLTable(string(body), "<docsDevEventTable>", "</docsDevEventTable>")

	eventTable := EventTable{}
	if err := xml.Unmarshal([]byte(xmlData), &eventTable); err != nil {
		panic(err)
	}
	fmt.Printf("%+v", eventTable)

	e.mu.Lock()
	e.totalEventScrapes.Collect(ch)
	e.scrapeEventErrors.Collect(ch)
	e.mu.Unlock()
}
