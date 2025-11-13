package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	IBSYSPATH = "/sys/class/infiniband/"
	tempRegex = regexp.MustCompile(`Temperature
$$
C
$$
\s*:\s*([\d\.]+)`)
	voltRegex = regexp.MustCompile(`Voltage
$$
mV
$$
\s*:\s*([\d\.]+)`)
	biasRegex = regexp.MustCompile(`Bias Current
$$
mA
$$
\s*:\s*([\d\.,\s]+)`)
	rxPowerRegex = regexp.MustCompile(`Rx Power Current
$$
dBm
$$
\s*:\s*([\d\.,\s-]+)`)
	txPowerRegex = regexp.MustCompile(`Tx Power Current
$$
dBm
$$
\s*:\s*([\d\.,\s-]+)`)
	linkTypeRegex = regexp.MustCompile(`Cable Type\s*:\s*(.+)`)
)

type IBCounter struct {
	IBDev        string  `json:"ib_dev"`
	NetDev       string  `json:"net_dev"`
	DevLinkType  string  `json:"dev_link_type"`
	CounterName  string  `json:"counter_name"`
	CounterValue float64 `json:"counter_value"`
}

func (c *IBCounter) toPrometheusFormat() string {
	return fmt.Sprintf("ib_hca_counter{device=\"%s\", counter_name=\"%s\"} %f", c.IBDev, c.CounterName, c.CounterValue)
}

func countersToPrometheusFormat(counters []IBCounter) string {
	var b strings.Builder
	for _, c := range counters {
		b.WriteString(c.toPrometheusFormat())
		b.WriteString("\n")
	}
	return b.String()
}

func listFiles(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Fail to Read dir:%s", dir)
		return nil, err
	}

	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}
	log.Printf("Get files:%s from dir: %s", fileNames, dir)

	return fileNames, nil
}

func isDevActive(IBDev string) bool {
	path := path.Join(IBSYSPATH, IBDev, "ports/1/state")
	contents, err := os.ReadFile(path)
	log.Printf("Get IBDev:%s, port State is:%s", IBDev, string(contents))
	if err != nil {
		log.Printf("Fail to ReadFile from path:%s", path)
		return false
	}
	if strings.Contains(string(contents), "ACTIVE") {
		log.Printf("Get IBDev:%s, ==>ACTIVE port State, state is:%s<==", IBDev, strings.ReplaceAll(string(contents), "\n", ""))
		return true
	}
	return false
}

func IsIBLink(IBDev string) bool {
	path := path.Join(IBSYSPATH, IBDev, "ports/1/link_layer")
	contents, err := os.ReadFile(path)
	log.Printf("Get IBDev:%s, Link_layer is :%s", IBDev, string(contents))
	if err != nil {
		log.Printf("Fail to ReadFile from path:%s", path)
		return false
	}
	if strings.Contains(string(contents), "InfiniBand") {
		log.Printf("Get IBDev:%s, ==>InfiniBand Link_layer, link_layer:%s<==", IBDev, strings.ReplaceAll(string(contents), "\n", ""))
		return true
	}
	return false
}
func isPhysicalIBDevice(deviceName string) bool {
	filePath := fmt.Sprintf("/sys/class/infiniband/%s/device/sriov_numvfs", deviceName)
	_, err := os.Stat(filePath)
	if err == nil {

		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	log.Printf("Warning: Could not check device type for %s due to an error: %v. Assuming it's not a physical device.\n", deviceName, err)
	return false
}
func containsAny(s string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(s, keyword) {
			return true
		}
	}
	return false
}

func GetIBDev() []string {
	allIBDev, err := listFiles(IBSYSPATH)
	if err != nil {
		log.Fatal("Fail to get all IB Dev", err)
		return nil
	}

	var activeIBDev []string
	for _, ibDev := range allIBDev {
		// skip virtual functions and mezz devices
		if containsAny(ibDev, "mezz", "mlx5_5", "mlx5_6", "mlx5_7", "mlx5_8") {
			continue
		}

		if isDevActive(ibDev) {
			activeIBDev = append(activeIBDev, ibDev)
		}
	}
	log.Printf("Get allIBDev:%s, Infiniband Link_layer && active state dev:%s", allIBDev, activeIBDev)
	return activeIBDev
}

func GetIBCounter(allIBDev []string, counterType string) ([]IBCounter, error) {
	var allCounter []IBCounter
	for _, perIBDev := range allIBDev {
		var ibCounter IBCounter
		ibCounter.IBDev = perIBDev

		netDevPath := path.Join(IBSYSPATH, perIBDev, "device/net/")
		entries, err := os.ReadDir(netDevPath)
		if err != nil {
			log.Fatalf("error: fail to read path %s: %v", netDevPath, err)
		}

		// just one net device is expected
		for _, entry := range entries {
			if entry.IsDir() {
				ibCounter.NetDev = entry.Name()
				log.Printf("Get IBDev:%s, NetDev is:%s", ibCounter.IBDev, ibCounter.NetDev)
			}
		}

		// get DevLinkType InfiniBand or Ethernet
		linkLayerPath := path.Join(IBSYSPATH, perIBDev, "ports/1/link_layer")
		contents, err := os.ReadFile(linkLayerPath)
		if err != nil {
			log.Printf("Fail to ReadFile from path:%s", linkLayerPath)
			ibCounter.DevLinkType = "Unknown"
		} else {
			ibCounter.DevLinkType = strings.ReplaceAll(string(contents), "\n", "")
			log.Printf("Get IBDev:%s, DevLinkType is:%s", ibCounter.IBDev, ibCounter.DevLinkType)
		}

		// Get IB port counter
		counterPath := path.Join(IBSYSPATH, perIBDev, "ports/1", counterType)
		ibCounterName, err := listFiles(counterPath)
		if err != nil {
			log.Printf("Fail to get the counter from path :%s", counterPath)
			return nil, err
		}
		for _, counter := range ibCounterName {
			// counter Name
			ibCounter.CounterName = counter

			counterValuePath := path.Join(counterPath, counter)
			contents, err := os.ReadFile(counterValuePath)
			if err != nil {
				log.Printf("Fail to read the ib counter from path: %s", counterValuePath)
			}
			// counter Value
			value, err := strconv.ParseFloat(strings.ReplaceAll(string(contents), "\n", ""), 10)
			if err != nil {
				log.Fatal("Error covering string to float64")
				return nil, err
			}

			ibCounter.CounterValue = value
			log.Printf("ibDev:%11s, counterName:%35s:%f", ibCounter.IBDev, ibCounter.CounterName, ibCounter.CounterValue)
			allCounter = append(allCounter, ibCounter)
		}
	}
	return allCounter, nil
}
