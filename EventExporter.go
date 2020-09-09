package main

import (
	"encoding/xml"
	"github.com/prometheus/client_golang/prometheus"
	"io/ioutil"
	"log"
	"net/http"
	"os"
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

	eventTimeStamp time.Time
}

// NewExporter returns an instance of Exporter configured with the modem's
// address, admin username and password.
func NewEventExporterFactory(addr, username, password string) *EventExporter {
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

	EventRow []EventRow `xml:"tr"`
}

// Collect runs our scrape loop returning each Prometheus metric.
func (e *EventExporter) Collect(ch chan<- prometheus.Metric) {

	path := "tmp/cm-event.log"

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
//	fmt.Printf("%+v", eventTable)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Println("Error opening event log destination file : ")
		log.Println(err)
	}
	defer file.Close()

	tne := "Time Not Established"

	for i := 0; i < len(eventTable.EventRow); i++ {

		row := eventTable.EventRow[i]

		evt := row.EventFirstTime
		var  eventLogTime  time.Time

		if evt != tne {
			// Get the Timestamp from the line
			eventLogTimeFormat := "2006-01-02, 15:04:05"
			eventLogTime, err = time.Parse(eventLogTimeFormat, evt)
			if (err != nil) {
				log.Println("Error while formatting Event Log Date : ")
				log.Println(err)
			}
		} else {
			eventLogTime = e.eventTimeStamp
		}

		if e.eventTimeStamp.After( eventLogTime ) {
			continue // Skip ones we have already written
		} else
		{
			formattedEventLog := "[" + row.EventFirstTime + "] " + row.EventLevel + " - " + row.EventText + "\n"
			file.WriteString(formattedEventLog)
			e.eventTimeStamp = eventLogTime
		}

	}

	file.Sync()

	e.mu.Lock()
	e.totalEventScrapes.Collect(ch)
	e.scrapeEventErrors.Collect(ch)
	e.mu.Unlock()
}

