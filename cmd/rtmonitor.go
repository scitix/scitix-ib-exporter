package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time

// *** MODIFIED: DeviceMetrics 结构体现在使用 uint64 来存储需要计算的计数器 ***
type DeviceMetrics struct {
	IBDev         string
	PortSpeed     string // 端口速率通常是固定值，保持 string 即可
	RX            uint64
	TX            uint64
	Queue0Rx      uint64
	Queue0Tx      uint64
	Queue5Rx      uint64
	Queue5Tx      uint64
	Queue0Discard uint64
	Queue5Discard uint64
	OOS           uint64
	QPNum         uint64
	MRNum         uint64
	RxPrio5Pause  string
	TxPrio5Pause  string
	NpCnpSent     string
	RpCnpHandled  string
	LastUpdated   time.Time // *** NEW: 用于精确计算时间差
}

// Model 现在管理一个设备集合和一个 table 组件
type model struct {
	// *** CRITICAL: 这个 map 现在将用来存储上一次的 metrics 数据，是计算速率的关键 ***
	devices       map[string]DeviceMetrics
	deviceOrder   []string
	tbl           table.Model
	columnWeights []table.Column // *** NEW: 保存列的权重信息 ***
	width, height int
}

func recalculateColumnWidths(weights []table.Column, availableWidth int) []table.Column {
	// 1. 计算所有权重的总和
	totalWeight := 0
	for _, col := range weights {
		totalWeight += col.Width
	}

	if totalWeight == 0 {
		return weights // 避免除以零
	}

	newColumns := make([]table.Column, len(weights))
	allocatedWidth := 0

	// 2. 按比例计算每列的宽度
	for i, col := range weights {
		// 使用浮点数进行精确计算，避免整数除法导致精度丢失
		newWidth := int(float64(col.Width) / float64(totalWeight) * float64(availableWidth))

		newColumns[i] = table.Column{
			Title: col.Title,
			Width: newWidth,
		}
		allocatedWidth += newWidth
	}

	// 将剩余的像素分配给最宽的列，以填满空间。
	if remainingWidth := availableWidth - allocatedWidth; remainingWidth > 0 && len(newColumns) > 0 {
		// 找到最宽的列来承担这个误差
		maxWidthIndex := 0
		for i, col := range newColumns {
			if col.Width > newColumns[maxWidthIndex].Width {
				maxWidthIndex = i
			}
		}
		newColumns[maxWidthIndex].Width += remainingWidth
	}

	return newColumns
}

func initialModel() model {

	content, _ := os.ReadFile("/sys/class/infiniband/mlx5_0/ports/1/link_layer")
	contentStr := string(content)
	trimmedContent := strings.TrimSpace(contentStr)

	var columnWeights []table.Column
	if strings.Contains(trimmedContent, "Ethernet") {
		columnWeights = []table.Column{
			{Title: "Device", Width: 8},
			{Title: "Speed", Width: 6},
			{Title: "Queue 0 RX(Gbps)", Width: 12},
			{Title: "Queue 0 TX(Gbps)", Width: 12},
			{Title: "Q0 Discard", Width: 8},
			{Title: "Queue 5 RX(Gbps)", Width: 12},
			{Title: "Queue 5 TX(Gbps)", Width: 12},
			{Title: "Q5 Discard", Width: 8},
			{Title: "OOS", Width: 5},
			{Title: "QP Num", Width: 7},
			{Title: "MR Num", Width: 7},
			{Title: "RX Pause", Width: 7},
			{Title: "TX Pause", Width: 7},
			{Title: "NP CNP Sent", Width: 9},
			{Title: "RP CNP Handled", Width: 9},
			{Title: "Time", Width: 8},
		}
	}
	if strings.Contains(trimmedContent, "InfiniBand") {
		columnWeights = []table.Column{
			{Title: "Device", Width: 8},
			{Title: "Speed", Width: 6},
			{Title: "RX(Gbps)", Width: 12},
			{Title: "TX(Gbps)", Width: 12},
			{Title: "OOS", Width: 5},
			{Title: "QP Num", Width: 7},
			{Title: "MR Num", Width: 7},
			{Title: "Time", Width: 8},
		}
	}

	discoveredDevices := GetIBDev()
	sort.Strings(discoveredDevices)

	// 首次运行时，previousMetrics 是空的，所以速率会是 0
	initialRows, initialMetrics := updateAndCalculateRates(make(map[string]DeviceMetrics), discoveredDevices)

	initialTableWidth := 120 // 使用一个合理的默认值
	initialColumns := recalculateColumnWidths(columnWeights, initialTableWidth)

	tbl := table.New(
		table.WithColumns(initialColumns),
		table.WithRows(initialRows),
		table.WithHeight(len(discoveredDevices)),
		table.WithFocused(true),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).Foreground(lipgloss.Color("212"))
	s.Selected = s.Selected.Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Bold(false)
	tbl.SetStyles(s)

	return model{
		devices:       initialMetrics,
		deviceOrder:   discoveredDevices,
		tbl:           tbl,
		columnWeights: columnWeights,
	}
}

func updateAndCalculateRates(previousMetrics map[string]DeviceMetrics, deviceOrder []string) ([]table.Row, map[string]DeviceMetrics) {
	newRows := []table.Row{}
	newMetricsMap := make(map[string]DeviceMetrics)

	allCounters := GetAllIBCounter()
	currentTime := time.Now()

	currentRawMetrics := make(map[string]DeviceMetrics)
	for _, c := range allCounters {
		if _, exists := currentRawMetrics[c.IBDev]; !exists {
			currentRawMetrics[c.IBDev] = DeviceMetrics{IBDev: c.IBDev}
		}

		metrics := currentRawMetrics[c.IBDev]

		if allCounters[0].DevLinkType == "Ethernet" {
			switch c.CounterName {
			case "portSpeed":
				metrics.PortSpeed = fmt.Sprintf("%d", c.CounterValue)
			case "rx_prio0_bytes":
				metrics.Queue0Rx = c.CounterValue
			case "tx_prio0_bytes":
				metrics.Queue0Tx = c.CounterValue
			case "rx_prio5_bytes":
				metrics.Queue5Rx = c.CounterValue
			case "tx_prio5_bytes":
				metrics.Queue5Tx = c.CounterValue
			case "rx_prio0_discards":
				metrics.Queue0Discard = c.CounterValue
			case "rx_prio5_discards":
				metrics.Queue5Discard = c.CounterValue
			case "out_of_sequence":
				metrics.OOS = c.CounterValue
			case "QPNum":
				metrics.QPNum = c.CounterValue
			case "MRNum":
				metrics.MRNum = c.CounterValue
			case "rx_prio5_pause":
				metrics.RxPrio5Pause = fmt.Sprintf("%d", c.CounterValue)
			case "tx_prio5_pause":
				metrics.TxPrio5Pause = fmt.Sprintf("%d", c.CounterValue)
			case "np_cnp_sent":
				metrics.NpCnpSent = fmt.Sprintf("%d", c.CounterValue)
			case "rp_cnp_handled":
				metrics.RpCnpHandled = fmt.Sprintf("%d", c.CounterValue)
			}
		}

		if allCounters[0].DevLinkType == "InfiniBand" {
			switch c.CounterName {
			case "portSpeed":
				metrics.PortSpeed = fmt.Sprintf("%d", c.CounterValue)
			case "port_rcv_data":
				metrics.RX = c.CounterValue
			case "port_xmit_data":
				metrics.TX = c.CounterValue
			case "out_of_sequence":
				metrics.OOS = c.CounterValue
			case "QPNum":
				metrics.QPNum = c.CounterValue
			case "MRNum":
				metrics.MRNum = c.CounterValue
			}
		}

		if c.DevLinkType == "Ethernet" {
			switch c.CounterName {
			case "portSpeed":
				metrics.PortSpeed = fmt.Sprintf("%d", c.CounterValue)
			case "rx_prio0_bytes":
				metrics.Queue0Rx = c.CounterValue
			case "tx_prio0_bytes":
				metrics.Queue0Tx = c.CounterValue
			case "rx_prio5_bytes":
				metrics.Queue5Rx = c.CounterValue
			case "tx_prio5_bytes":
				metrics.Queue5Tx = c.CounterValue
			case "rx_prio0_discards":
				metrics.Queue0Discard = c.CounterValue
			case "rx_prio5_discards":
				metrics.Queue5Discard = c.CounterValue
			case "out_of_sequence":
				metrics.OOS = c.CounterValue
			case "QPNum":
				metrics.QPNum = c.CounterValue
			case "MRNum":
				metrics.MRNum = c.CounterValue
			case "rx_prio5_pause":
				metrics.RxPrio5Pause = fmt.Sprintf("%d", c.CounterValue)
			case "tx_prio5_pause":
				metrics.TxPrio5Pause = fmt.Sprintf("%d", c.CounterValue)
			case "np_cnp_sent":
				metrics.NpCnpSent = fmt.Sprintf("%d", c.CounterValue)
			case "rp_cnp_handled":
				metrics.RpCnpHandled = fmt.Sprintf("%d", c.CounterValue)
			}
		}

		if c.DevLinkType == "InfiniBand" {
			switch c.CounterName {
			case "portSpeed":
				metrics.PortSpeed = fmt.Sprintf("%d", c.CounterValue)
			case "rx_vport_rdma_unicast_bytes":
				metrics.RX = c.CounterValue
			case "tx_vport_rdma_unicast_bytes":
				metrics.TX = c.CounterValue
			case "out_of_sequence":
				metrics.OOS = c.CounterValue
			case "QPNum":
				metrics.QPNum = c.CounterValue
			case "MRNum":
				metrics.MRNum = c.CounterValue
			case "np_cnp_sent":
				metrics.NpCnpSent = fmt.Sprintf("%d", c.CounterValue)
			case "rp_cnp_handled":
				metrics.RpCnpHandled = fmt.Sprintf("%d", c.CounterValue)
			}
		}

		currentRawMetrics[c.IBDev] = metrics
	}

	// 3. 遍历排序好的设备列表，计算速率并生成行

	if allCounters[0].DevLinkType == "Ethernet" {
		for _, deviceName := range deviceOrder {
			currentMetrics, ok := currentRawMetrics[deviceName]
			if !ok {
				continue // 如果没有获取到该设备的数据，则跳过
			}
			currentMetrics.LastUpdated = currentTime

			prevMetrics, hasPrevious := previousMetrics[deviceName]

			var q0RxGbps, q0TxGbps, q5RxGbps, q5TxGbps float64
			var q0Discard, q5Discard, oos uint64

			// 只有在存在上一次数据时，才进行速率计算
			if hasPrevious {
				// 计算时间差（秒）
				duration := currentMetrics.LastUpdated.Sub(prevMetrics.LastUpdated).Seconds()
				if duration > 0 {
					// 速率(Gbps) = (当前字节数 - 上次字节数) * 8 bits/byte / 时间差(s) / 1e9 (G)
					q0RxGbps = float64(currentMetrics.Queue0Rx-prevMetrics.Queue0Rx) * 8 / duration / 1e9
					q0TxGbps = float64(currentMetrics.Queue0Tx-prevMetrics.Queue0Tx) * 8 / duration / 1e9
					q5RxGbps = float64(currentMetrics.Queue5Rx-prevMetrics.Queue5Rx) * 8 / duration / 1e9
					q5TxGbps = float64(currentMetrics.Queue5Tx-prevMetrics.Queue5Tx) * 8 / duration / 1e9
					q0Discard = currentMetrics.Queue0Discard - prevMetrics.Queue0Discard
					q5Discard = currentMetrics.Queue5Discard - prevMetrics.Queue5Discard
					// rx = float64(currentMetrics.RX-prevMetrics.RX) * 8 / duration / 1e9
					// tx = float64(currentMetrics.TX-prevMetrics.TX) * 8 / duration / 1e9
					oos = currentMetrics.OOS - prevMetrics.OOS
				}
			}

			// 生成表格行，使用计算出的速率
			newRows = append(newRows, table.Row{
				currentMetrics.IBDev,
				currentMetrics.PortSpeed,
				fmt.Sprintf("%.2f", q0RxGbps),
				fmt.Sprintf("%.2f", q0TxGbps),
				fmt.Sprintf("%d", q0Discard),
				fmt.Sprintf("%.2f", q5RxGbps),
				fmt.Sprintf("%.2f", q5TxGbps),
				fmt.Sprintf("%d", q5Discard),
				fmt.Sprintf("%d", oos),
				fmt.Sprintf("%d", currentMetrics.QPNum),
				fmt.Sprintf("%d", currentMetrics.MRNum),
				currentMetrics.RxPrio5Pause,
				currentMetrics.TxPrio5Pause,
				currentMetrics.NpCnpSent,
				currentMetrics.RpCnpHandled,
				currentTime.Format("15:04:05"),
			})

			newMetricsMap[deviceName] = currentMetrics
		}
	}

	if allCounters[0].DevLinkType == "InfiniBand" {
		for _, deviceName := range deviceOrder {
			currentMetrics, ok := currentRawMetrics[deviceName]
			if !ok {
				continue // 如果没有获取到该设备的数据，则跳过
			}
			currentMetrics.LastUpdated = currentTime

			prevMetrics, hasPrevious := previousMetrics[deviceName]

			var rx, tx float64
			var oos uint64

			// 只有在存在上一次数据时，才进行速率计算
			if hasPrevious {
				// 计算时间差（秒）
				duration := currentMetrics.LastUpdated.Sub(prevMetrics.LastUpdated).Seconds()
				if duration > 0 {
					// 速率(Gbps) = (当前字节数 - 上次字节数) * 8 bits/byte / 时间差(s) / 1e9 (G)
					rx = float64(currentMetrics.RX-prevMetrics.RX) * 8 * 4 / duration / 1e9
					tx = float64(currentMetrics.TX-prevMetrics.TX) * 8 * 4 / duration / 1e9
					oos = currentMetrics.OOS - prevMetrics.OOS
				}
			}

			// 生成表格行，使用计算出的速率
			newRows = append(newRows, table.Row{
				currentMetrics.IBDev,
				currentMetrics.PortSpeed,
				fmt.Sprintf("%.2f", rx),
				fmt.Sprintf("%.2f", tx),
				fmt.Sprintf("%d", oos),
				fmt.Sprintf("%d", currentMetrics.QPNum),
				fmt.Sprintf("%d", currentMetrics.MRNum),
				currentTime.Format("15:04:05"),
			})

			newMetricsMap[deviceName] = currentMetrics
		}
	}

	return newRows, newMetricsMap
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tickCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// 1. 计算表格可用的总宽度 (减去边框和内边距)
		tableWidth := m.width - 4 // 根据你的 containerStyle 调整

		// 2. 使用我们的新函数，根据权重重新计算列宽
		newColumns := recalculateColumnWidths(m.columnWeights, tableWidth)

		// 3. 将新计算出的列定义应用到表格上
		m.tbl.SetColumns(newColumns)

		// 4. 同时也要设置表格的总高度
		m.tbl.SetHeight(m.height - 6)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}

	case tickMsg:
		// *** MODIFIED: Update 逻辑现在非常简洁 ***
		// 1. 传入上一次的 metrics (m.devices)，计算新数据
		newRows, newMetrics := updateAndCalculateRates(m.devices, m.deviceOrder)

		// 2. 更新 model 的状态
		m.tbl.SetRows(newRows)
		m.devices = newMetrics // *** CRITICAL: 保存当前数据，作为下一次的 "previous" 数据 ***

		// 3. 请求下一次 tick
		return m, tickCmd()
	}

	// 把消息传递给 table 组件，让它处理滚动等内部事件
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m model) View() string {
	containerStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63"))

	tableStr := containerStyle.Render(m.tbl.View())

	helpStyle := lipgloss.NewStyle().MarginTop(1).Foreground(lipgloss.Color("241"))
	helpStr := helpStyle.Render("使用 ↑/↓ 箭头或 PageUp/PageDown 翻页。按 q 或 Ctrl+C 退出。")

	finalView := lipgloss.JoinVertical(lipgloss.Left, tableStr, helpStr)

	return finalView
}
