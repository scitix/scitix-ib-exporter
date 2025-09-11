package main

import (
	"archive/zip"
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	Version = "0.0.2"
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
		ibcounterGauge.WithLabelValues(counter.CounterName, counter.IBDev).Set(float64(counter.CounterValue))
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

func getMRNum(allIBDev []string) []IBCounter {
	var counters []IBCounter

	for _, IBDev := range allIBDev {
		var counter IBCounter

		cmd := exec.Command("rdma", "resource", "show", IBDev)

		outputBytes, err := cmd.CombinedOutput()
		if err != nil {
			return counters
		}

		outputStr := string(outputBytes)

		// 3. 使用与之前相同的、经过验证的正则表达式进行解析
		re := regexp.MustCompile(`(\w+_\d+):\s+.*?qp\s+(\d+)\s+.*?mr\s+(\d+)`)
		matches := re.FindStringSubmatch(outputStr)

		// 4. 检查解析结果。我们期望有4个匹配项：
		//    matches[0]: 完整的匹配字符串
		//    matches[1]: 设备名 (第一个捕获组)
		//    matches[2]: QP 值 (第二个捕获组)
		//    matches[3]: MR 值 (第三个捕获组)
		if len(matches) < 4 {
			return counters
		}

		// 5. 将字符串数值转换为整数
		// qp, err1 := strconv.Atoi(matches[2])
		mr, err2 := strconv.ParseUint(matches[3], 10, 64)

		if err2 != nil {
			return counters
		}
		netDevPath := path.Join(IBSYSPATH, IBDev, "device/net/")
		entries, err := os.ReadDir(netDevPath)
		if err != nil {
			log.Fatalf("error: fail to read path %s: %v", netDevPath, err)
		}

		// just one net device is expected
		for _, entry := range entries {
			if entry.IsDir() {
				counter.NetDev = entry.Name()
				log.Printf("Get IBDev:%s, NetDev is:%s", counter.IBDev, counter.NetDev)
			}
		}
		counter.IBDev = IBDev
		counter.CounterName = "MRNum"
		counter.CounterValue = mr
		log.Printf("ibDev:%11s, counterName:%35s:%d", counter.IBDev, counter.CounterName, counter.CounterValue)
		counters = append(counters, counter)
	}
	return counters
}

func getQPNum(allIBDev []string) []IBCounter {
	var counters []IBCounter

	for _, IBDev := range allIBDev {
		var counter IBCounter
		var QPNum uint64
		bdf := GetIBDevBDF(IBDev)
		qpPath := path.Join("/sys/kernel/debug/mlx5", bdf, "QPs")
		entries, err := os.ReadDir(qpPath)
		if err != nil {
			log.Printf("fail to read pat:%s, err:%v", qpPath, err)
			return counters
		}

		for _, entry := range entries {
			if entry.IsDir() {
				QPNum++
			}
		}
		netDevPath := path.Join(IBSYSPATH, IBDev, "device/net/")
		entries, err = os.ReadDir(netDevPath)
		if err != nil {
			log.Fatalf("error: fail to read path %s: %v", netDevPath, err)
		}

		// just one net device is expected
		for _, entry := range entries {
			if entry.IsDir() {
				counter.NetDev = entry.Name()
				log.Printf("Get IBDev:%s, NetDev is:%s", counter.IBDev, counter.NetDev)
			}
		}
		counter.IBDev = IBDev
		counter.CounterName = "QPNum"
		counter.CounterValue = QPNum
		log.Printf("ibDev:%11s, counterName:%35s:%d", counter.IBDev, counter.CounterName, counter.CounterValue)
		counters = append(counters, counter)
	}
	return counters
}

func getPortSpeed(allIBDev []string) []IBCounter {
	var counters []IBCounter

	for i := range allIBDev {
		var counter IBCounter
		netDevPath := path.Join(IBSYSPATH, allIBDev[i], "device/net/")
		entries, err := os.ReadDir(netDevPath)
		if err != nil {
			log.Fatalf("error: fail to read path %s: %v", netDevPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				counter.NetDev = entry.Name()
				log.Printf("Get IBDev:%s, NetDev is:%s", counter.IBDev, counter.NetDev)
			}
		}
		counter.IBDev = allIBDev[i]
		counter.CounterName = "portSpeed"

		ratePath := path.Join("/sys/class/infiniband", allIBDev[i], "ports/1/rate")
		rateByte, err := os.ReadFile(ratePath)
		if err != nil {
			log.Printf("Fail to read the file, path:%s", ratePath)
		}
		rate := string(rateByte)
		if strings.Contains(rate, "200") {
			counter.CounterValue = 200000
		}
		if strings.Contains(rate, "400") {
			counter.CounterValue = 400000
		}
		log.Printf("ibDev:%11s, counterName:%35s:%d", counter.IBDev, counter.CounterName, counter.CounterValue)
		counters = append(counters, counter)
	}
	return counters
}

func GetRoceData(allIBDev []string) []IBCounter {
	var counters []IBCounter

	content, _ := os.ReadFile("/sys/class/infiniband/mlx5_0/ports/1/link_layer")
	contentStr := string(content)
	trimmedContent := strings.TrimSpace(contentStr)

	for i := range allIBDev {
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

		var fields map[string]bool
		if strings.Contains(trimmedContent, "Ethernet") {
			fields = map[string]bool{
				"rx_prio0_bytes":          true,
				"tx_prio0_bytes":          true,
				"rx_prio0_discards":       true,
				"rx_prio5_bytes":          true,
				"tx_prio5_bytes":          true,
				"rx_prio5_discards":       true,
				"rx_prio0_pause":          true,
				"rx_prio0_pause_duration": true,
				"tx_prio0_pause":          true,
				"tx_prio0_pause_duration": true,
				"rx_prio5_pause":          true,
				"rx_prio5_pause_duration": true,
				"tx_prio5_pause":          true,
				"tx_prio5_pause_duration": true,
			}
		}
		if strings.Contains(trimmedContent, "InfiniBand") {
			fields = map[string]bool{
				"rx_vport_rdma_unicast_bytes": true,
				"tx_vport_rdma_unicast_bytes": true,
			}
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
					NetDev:       entries[0].Name(),
					CounterName:  key,
					CounterValue: num,
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

	MRNums := getMRNum(IBDev)
	ibCounters = append(ibCounters, MRNums...)

	roceData := GetRoceData(IBDev)
	ibCounters = append(ibCounters, roceData...)

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
	logfile := flag.String("log", "/var/log/ib-exporter.log", "log file path")
	termi := flag.Bool("termi", false, "Print log to terminal and file")
	runonce := flag.Bool("runonce", false, "Run once and exit")
	runDuration := flag.Int("t", 5, "The total time for the task to run")
	archiveThresholdMB := flag.Int("r", 5, "The size threshold in MB for archiving the data folder")
	dataPath := flag.String("datapath", "/var/log/ibtestdata", "Path for storing data files")
	monitor := flag.Bool("monitor", false, "Monitor the IB devices and export metrics")
	version := flag.Bool("version", false, "Version of the application")
	flag.Parse()

	if *version {
		fmt.Printf("ib-exporter for High-Precision Monitoring version: %s\n", Version)
		return
	}

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

	if *monitor {
		p := tea.NewProgram(initialModel(), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			log.Fatalf("fail to load the app: %v", err)
		}
		os.Exit(0)
	}

	if *runonce {
		// testdataDir := filepath.Join("/var/log", "ibtestdata")
		testdataDir := *dataPath
		err := os.MkdirAll(testdataDir, 0755)
		if err != nil {
			log.Fatalf("Fatal: Could not create 'testdata' directory: %v", err)
		}

		log.Println("Checking data directory for potential archiving...")
		thresholdBytes := int64(*archiveThresholdMB) * 1024 * 1024

		archiveDir := filepath.Dir(testdataDir)
		if err := manageDataArchives(testdataDir, archiveDir, thresholdBytes); err != nil {
			log.Fatalf("Fatal: Failed to manage data archives: %v", err)
		}

		timestamp := time.Now().Format("20060102_150405")
		dataFilename := fmt.Sprintf("data_%s.log", timestamp)
		finalDataPath := filepath.Join(testdataDir, dataFilename)
		dataFile, err := os.Create(finalDataPath)
		if err != nil {
			log.Fatalf("Fatal: Could not create data log file: %v", err)
		}
		defer dataFile.Close()

		log.Printf("Run-once mode activated. Writing data to %s", finalDataPath)

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		done := make(chan bool)
		go func() { time.Sleep(time.Duration(*runDuration) * time.Second); done <- true }()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				ibCounters := GetAllIBCounter()
				for _, counter := range ibCounters {
					_, err := fmt.Fprintf(dataFile, "%d,%s,%s,%s,%s,%d\n",
						time.Now().UnixNano(),
						counter.IBDev,
						counter.NetDev,
						counter.DevLinkType,
						counter.CounterName,
						counter.CounterValue)
					if err != nil {
						log.Printf("Error writing to log file: %v", err)
					}
				}
			}
		}
	}

	prometheus.MustRegister(ibcounterGauge)

	http.HandleFunc("/metrics", metricsHandler)
	log.Printf("Starting server on :%s", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}

func manageDataArchives(dataDir, archiveDir string, thresholdBytes int64) error {
	// 1. Calculate the total size of the data directory
	var totalSize int64
	err := filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("could not calculate directory size: %w", err)
	}

	// 2. If size is below the threshold, do nothing.
	if totalSize < thresholdBytes {
		log.Printf("Data directory size (%d bytes) is under the %dMB threshold. No action needed.", totalSize, thresholdBytes)
		return nil
	}
	log.Printf("Data directory size (%d bytes) exceeds %dMB threshold. Starting archival process.", totalSize, thresholdBytes)

	// 3. Create a new timestamped archive file
	timestamp := time.Now().Format("20060102_150405")
	archiveName := fmt.Sprintf("ibtestdata_%s.zip", timestamp)
	archivePath := filepath.Join(archiveDir, archiveName)
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("could not create archive file: %w", err)
	}
	defer archiveFile.Close()

	zipWriter := zip.NewWriter(archiveFile)
	defer zipWriter.Close()

	// 4. Find all .log files and add them to the archive
	filesToArchive, err := filepath.Glob(filepath.Join(dataDir, "*.log"))
	if err != nil {
		return fmt.Errorf("could not find log files to archive: %w", err)
	}

	for _, filePath := range filesToArchive {
		log.Printf("Archiving %s", filepath.Base(filePath))
		file, err := os.Open(filePath)
		if err != nil {
			continue
		} // Skip files that can't be opened

		zipEntry, err := zipWriter.Create(filepath.Base(filePath))
		if err != nil {
			file.Close()
			continue
		}

		_, err = io.Copy(zipEntry, file)
		file.Close() // Close file before deleting

		if err == nil {
			os.Remove(filePath) // Delete original file after successful archival
		}
	}
	log.Printf("Successfully created archive: %s", archiveName)

	// 5. Rotate old archives, keeping the 5 most recent
	allArchives, err := filepath.Glob(filepath.Join(archiveDir, "testdata_*.zip"))
	if err != nil {
		return fmt.Errorf("could not find archives for rotation: %w", err)
	}

	if len(allArchives) > 5 {
		sort.Strings(allArchives) // Sorts alphabetically, which works for our timestamp format
		archivesToDelete := allArchives[:len(allArchives)-5]
		log.Printf("Found %d archives, cleaning up the oldest %d.", len(allArchives), len(archivesToDelete))
		for _, oldArchive := range archivesToDelete {
			log.Printf("Deleting old archive: %s", filepath.Base(oldArchive))
			os.Remove(oldArchive)
		}
	}

	return nil
}
