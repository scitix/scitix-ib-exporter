package main

import (
	"flag"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	// ibRegistry     *prometheus.Registry
	ibcounterGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "node_ib_counters",
			Help: "collected node ib counter",
		},
		[]string{"metricsName", "IBDev"},
	)
)

func getIBDevCounter(IBDev []string) []IBCounter {
	var ibCounters []IBCounter
	var wg sync.WaitGroup
	var mu sync.Mutex
	counterTypes := []string{"counters", "hw_counters"}

	wg.Add(len(counterTypes))
	for _, counterType := range counterTypes {
		go func(ct string) {
			defer wg.Done()
			ibPortCounter, err := GetIBCounter(IBDev, ct)
			if err != nil {
				log.Printf("Get IB Counter failed, err:%s", err)
				return
			}
			mu.Lock()
			ibCounters = append(ibCounters, ibPortCounter...)
			mu.Unlock()
		}(counterType)
	}
	wg.Wait()
	return ibCounters
}

func updateMetrics(counters []IBCounter) {
	for _, counter := range counters {
		ibcounterGauge.WithLabelValues(counter.counterName, counter.IBDev).Set(float64(counter.counterValue))
	}
}

func GetIBDevBDF(mlxDev string) string {
	var bdf string
	path := path.Join(IBSYSPATH, mlxDev, "device", "uevent")
	contentTmp, err := os.ReadFile(path)
	if err != nil {
		slog.Error("fail to read uevnet", "err", err)
		return ""
	}

	lines := strings.Split(string(contentTmp), "\n")
	for i := 0; i < len(lines); i++ {
		if strings.Contains(lines[i], "PCI_SLOT_NAME") {
			bdf = strings.Split(lines[i], "=")[1]
		}
	}
	return bdf
}

func getQPNum(allIBDev []string) []IBCounter {
	var counters []IBCounter

	for _, IBDev := range allIBDev {
		var counter IBCounter
		var QPNum uint64
		bdf := GetIBDevBDF(IBDev)
		path := path.Join("/sys/kernel/debug/mlx5", bdf, "QPs")
		entries, err := os.ReadDir(path)
		if err != nil {
			log.Printf("fail to read pat:%s, err:%v", path, err)
			return counters
		}

		for _, entry := range entries {
			if entry.IsDir() {
				QPNum++
			}
		}
		counter.IBDev = IBDev
		counter.counterName = "QPNum"
		counter.counterValue = QPNum
		log.Printf("ibDev:%11s, counterName:%35s:%d", counter.IBDev, counter.counterName, counter.counterValue)
		counters = append(counters, counter)
	}
	return counters
}

func getPortSpeed(allIBDev []string) []IBCounter {
	var counters []IBCounter

	for i := 0; i < len(allIBDev); i++ {
		var counter IBCounter
		counter.IBDev = allIBDev[i]
		counter.counterName = "portSpeed"

		ratePath := path.Join("/sys/class/infiniband", allIBDev[i], "ports/1/rate")
		rateByte, err := os.ReadFile(ratePath)
		if err != nil {
			log.Printf("Fail to read the file, path:%s", ratePath)
		}
		rate := string(rateByte)
		if strings.Contains(rate, "200") {
			counter.counterValue = 200000
		}
		if strings.Contains(rate, "400") {
			counter.counterValue = 400000
		}
		log.Printf("ibDev:%11s, counterName:%35s:%d", counter.IBDev, counter.counterName, counter.counterValue)
		counters = append(counters, counter)
	}
	return counters
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("=========> start to get ib counter in service<==========")

	IBDev := GetIBDev()
	ibCounters := getIBDevCounter(IBDev)

	QPNums := getQPNum(IBDev)
	ibCounters = append(ibCounters, QPNums...)

	portUtil := getPortSpeed(IBDev)
	ibCounters = append(ibCounters, portUtil...)

	updateMetrics(ibCounters)

	h := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func main() {
	// command line define
	port := flag.String("port", "9315", "port to run the server on")
	logfile := flag.String("log", "./ib-exporter.log", "log file path")
	termi := flag.Bool("termi", false, "Print log to terminal and file")
	flag.Parse()

	// log setting
	var logOutput io.Writer
	if *termi {
		logOutput = io.MultiWriter(os.Stdout, &lumberjack.Logger{
			Filename:   *logfile,
			MaxSize:    10,
			MaxBackups: 10,
			MaxAge:     28,
			Compress:   true,
		})
	} else {
		logOutput = &lumberjack.Logger{
			Filename:   *logfile,
			MaxSize:    10,
			MaxBackups: 10,
			MaxAge:     28,
			Compress:   true,
		}
	}
	log.SetOutput(logOutput)

	prometheus.MustRegister(ibcounterGauge)

	http.HandleFunc("/metrics", metricsHandler)
	log.Printf("Starting server on :%s", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
