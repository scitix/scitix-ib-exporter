package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
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

func GetRoceData(allIBDev []string) []IBCounter {
	var counters []IBCounter

	for i := 0; i < len(allIBDev); i++ {
		// 获取以太网接口
		netDir := filepath.Join("/sys/class/infiniband", allIBDev[i], "device", "net")

		entries, err := os.ReadDir(netDir)
		if err != nil {
			log.Printf("Failed to read %s: %v\n", netDir, err)
			os.Exit(1)
		}

		if len(entries) != 1 {
			log.Printf("Expected one net interface for %s, found %d\n", allIBDev[i], len(entries))
			os.Exit(1)
		}

		fields := map[string]bool{
			"rx_prio5_pause":          true,
			"rx_prio5_pause_duration": true,
			"tx_prio5_pause":          true,
			"tx_prio5_pause_duration": true,
		}

		cmd := exec.Command("ethtool", "-S", entries[0].Name())
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil
		}

		if err := cmd.Start(); err != nil {
			return nil
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])

			if _, ok := fields[key]; ok {
				num, err := strconv.ParseUint(val, 10, 64)
				if err != nil {
					continue // or log parse error
				}
				counters = append(counters, IBCounter{
					IBDev:        allIBDev[i],
					counterName:  key,
					counterValue: num,
				})
			}
		}
		if err := scanner.Err(); err != nil {
			return nil
		}

		cmd.Wait()

	}
	return counters
}

func GetAllIBCounter() []IBCounter {
	IBDev := GetIBDev()
	ibCounters := getIBDevCounter(IBDev)

	QPNums := getQPNum(IBDev)
	ibCounters = append(ibCounters, QPNums...)

	portUtil := getPortSpeed(IBDev)
	ibCounters = append(ibCounters, portUtil...)

	roceData := GetRoceData(IBDev)
	ibCounters = append(ibCounters, roceData...)

	return ibCounters
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("=========> start to get ib counter in service<==========")

	ibCounters := GetAllIBCounter()

	updateMetrics(ibCounters)

	h := promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func main() {
	// command line define
	port := flag.String("port", "9315", "port to run the server on")
	logfile := flag.String("log", "./ib-exporter.log", "log file path")
	termi := flag.Bool("termi", false, "Print log to terminal and file")
	runonce := flag.Bool("runonce", false, "Run once and exit")
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

	if *runonce {
		log.Println("Running once and exiting...")
		ibCounters := GetAllIBCounter()
		prometheusMetric := countersToPrometheusFormat(ibCounters)
		fmt.Println(prometheusMetric)
		os.Exit(0)
	}

	prometheus.MustRegister(ibcounterGauge)

	http.HandleFunc("/metrics", metricsHandler)
	log.Printf("Starting server on :%s", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
