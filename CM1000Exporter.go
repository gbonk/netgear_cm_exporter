package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gocolly/colly"
	"github.com/prometheus/client_golang/prometheus"
)

type CM1000StatusExporter struct {
	StatusExporter
}

// Collect runs our scrape loop returning each Prometheus metric.
func (e *CM1000StatusExporter) Collect(ch chan<- prometheus.Metric) {
	e.totalScrapes.Inc()

	c := colly.NewCollector()
	c.SetRequestTimeout(30 * time.Second)

	// OnRequest callback adds basic auth header.
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Add("Authorization", e.authHeaderValue)
	})

	// OnError callback counts any errors that occur during scraping.
	c.OnError(func(r *colly.Response, err error) {
		log.Println("Status scrape failed:", err)
		log.Printf("Status Code: %d %s", r.StatusCode, http.StatusText(r.StatusCode))
		e.scrapeErrors.Inc()
	})

	// Callback to parse the tbody block of table with id=dsTable, the downstream table info.
	c.OnHTML(`#dsTable tbody`, func(elem *colly.HTMLElement) {
		elem.DOM.Find("tr").Each(func(i int, row *goquery.Selection) {
			if i == 0 {
				return // no rows were returned
			}
			var (
				channel    string
				lockStatus string
				modulation string
				channelID  string
				freqMHz    string
				power      float64
				snr        float64
				corrErrs   float64
				unCorrErrs float64
			)
			row.Find("td").Each(func(j int, col *goquery.Selection) {
				text := strings.TrimSpace(col.Text())

				switch j {
				case 0:
					channel = text
				case 1:
					lockStatus = text
				case 2:
					modulation = text
				case 3:
					channelID = text
				case 4:
					{
						var freqHZ float64
						fmt.Sscanf(text, "%f Hz", &freqHZ)
						freqMHz = fmt.Sprintf("%0.2f MHz", freqHZ/1e6)
					}
				case 5:
					fmt.Sscanf(text, "%f dBmV", &power)
				case 6:
					fmt.Sscanf(text, "%f dB", &snr)
				case 7:
					fmt.Sscanf(text, "%f", &corrErrs)
				case 8:
					fmt.Sscanf(text, "%f", &unCorrErrs)
				}
			})
			labels := []string{channel, lockStatus, modulation, channelID, freqMHz}

			ch <- prometheus.MustNewConstMetric(e.dsChannelSNR, prometheus.GaugeValue, snr, labels...)
			ch <- prometheus.MustNewConstMetric(e.dsChannelPower, prometheus.GaugeValue, power, labels...)
			ch <- prometheus.MustNewConstMetric(e.dsChannelCorrectableErrs, prometheus.CounterValue, corrErrs, labels...)
			ch <- prometheus.MustNewConstMetric(e.dsChannelUncorrectableErrs, prometheus.CounterValue, unCorrErrs, labels...)
		})
	})

	// Callback to parse the tbody block of table with id=usTable, the upstream channel info.
	c.OnHTML(`#usTable tbody`, func(elem *colly.HTMLElement) {
		elem.DOM.Find("tr").Each(func(i int, row *goquery.Selection) {
			if i == 0 {
				return // no rows were returned
			}
			var (
				channel     string
				lockStatus  string
				channelType string
				channelID   string
				symbolRate  float64
				freqMHz     string
				power       float64
			)
			row.Find("td").Each(func(j int, col *goquery.Selection) {
				text := strings.TrimSpace(col.Text())
				switch j {
				case 0:
					channel = text
				case 1:
					lockStatus = text
				case 2:
					channelType = text
				case 3:
					channelID = text
				case 4:
					{
						fmt.Sscanf(text, "%f Ksym/sec", &symbolRate)
						symbolRate = symbolRate * 1000 // convert to sym/sec
					}
				case 5:
					{
						var freqHZ float64
						fmt.Sscanf(text, "%f Hz", &freqHZ)
						freqMHz = fmt.Sprintf("%0.2f MHz", freqHZ/1e6)
					}
				case 6:
					fmt.Sscanf(text, "%f dBmV", &power)
				}
			})
			labels := []string{channel, lockStatus, channelType, channelID, freqMHz}

			ch <- prometheus.MustNewConstMetric(e.usChannelPower, prometheus.GaugeValue, power, labels...)
			ch <- prometheus.MustNewConstMetric(e.usChannelSymbolRate, prometheus.GaugeValue, symbolRate, labels...)
		})
	})

	e.mu.Lock()
	c.Visit(e.url)
	e.totalScrapes.Collect(ch)
	e.scrapeErrors.Collect(ch)
	e.mu.Unlock()
}

// Describe returns Prometheus metric descriptions for the exporter metrics.
func (e *CM1000StatusExporter) Describe(ch chan<- *prometheus.Desc) {
	// Exporter metrics.
	ch <- e.totalScrapes.Desc()
	ch <- e.scrapeErrors.Desc()
	// Downstream metrics.
	ch <- e.dsChannelSNR
	ch <- e.dsChannelPower
	ch <- e.dsChannelCorrectableErrs
	ch <- e.dsChannelUncorrectableErrs
	// Upstream metrics.
	ch <- e.usChannelPower
	ch <- e.usChannelSymbolRate
}
