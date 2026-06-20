package service

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"pdu-monitor/internal/config"
	"pdu-monitor/internal/model"
	"pdu-monitor/internal/snmp"
)

type PDUService struct {
	cfg       *config.Config
	clients   map[string]*snmp.Client
	mu        sync.RWMutex
	cache     map[string]*model.PDUDevice
	stopCh    chan struct{}
	started   bool
	fastPoll  map[string]bool

	eventsMu sync.RWMutex
	events   []model.PowerEvent
	eventSeq int64

	boxDeviceMap map[string][]string
	boxConfigMap map[string]config.DistributionBoxConfig
	deviceCfgMap map[string]config.PDUDeviceConfig
}

func NewPDUService(cfg *config.Config) *PDUService {
	clients := make(map[string]*snmp.Client)
	for _, dev := range cfg.Devices {
		client := snmp.NewClient(
			dev.Address,
			cfg.SNMP.Community,
			cfg.SNMP.Timeout,
			cfg.SNMP.Retries,
		)
		client.SetFastMode(cfg.Refresh.FastTimeoutSec, cfg.Refresh.FastRetries)
		clients[dev.ID] = client
	}

	boxDeviceMap := make(map[string][]string)
	deviceCfgMap := make(map[string]config.PDUDeviceConfig)
	for _, d := range cfg.Devices {
		deviceCfgMap[d.ID] = d
		if d.DistributionBoxID != "" {
			boxDeviceMap[d.DistributionBoxID] = append(boxDeviceMap[d.DistributionBoxID], d.ID)
		}
	}
	boxConfigMap := make(map[string]config.DistributionBoxConfig)
	for _, b := range cfg.DistributionBoxes {
		boxConfigMap[b.ID] = b
	}

	return &PDUService{
		cfg:          cfg,
		clients:      clients,
		cache:        make(map[string]*model.PDUDevice),
		stopCh:       make(chan struct{}),
		fastPoll:     make(map[string]bool),
		events:       make([]model.PowerEvent, 0),
		boxDeviceMap: boxDeviceMap,
		boxConfigMap: boxConfigMap,
		deviceCfgMap: deviceCfgMap,
	}
}

func (s *PDUService) Start() {
	if s.started {
		return
	}
	s.started = true

	s.refreshAll()

	go s.refreshLoop()
	log.Printf("[PDU] background refresh started: normal=%ds fast=%ds",
		s.cfg.Refresh.NormalIntervalSec, s.cfg.Refresh.FastIntervalSec)
}

func (s *PDUService) Stop() {
	if !s.started {
		return
	}
	close(s.stopCh)
	s.started = false
	log.Println("[PDU] background refresh stopped")
}

func (s *PDUService) refreshLoop() {
	normalTicker := time.NewTicker(time.Duration(s.cfg.Refresh.NormalIntervalSec) * time.Second)
	fastTicker := time.NewTicker(time.Duration(s.cfg.Refresh.FastIntervalSec) * time.Second)
	pruneTicker := time.NewTicker(5 * time.Minute)
	defer normalTicker.Stop()
	defer fastTicker.Stop()
	defer pruneTicker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-normalTicker.C:
			s.refreshAll()
		case <-fastTicker.C:
			s.refreshOfflineFast()
		case <-pruneTicker.C:
			s.pruneOldEvents()
		}
	}
}

func (s *PDUService) refreshAll() {
	var wg sync.WaitGroup
	for _, devCfg := range s.cfg.Devices {
		wg.Add(1)
		go func(dc config.PDUDeviceConfig) {
			defer wg.Done()
			s.refreshOne(dc, false)
		}(devCfg)
	}
	wg.Wait()
}

func (s *PDUService) refreshOfflineFast() {
	s.mu.RLock()
	offlineIDs := make([]string, 0)
	for id, dev := range s.cache {
		if !dev.Online {
			offlineIDs = append(offlineIDs, id)
		}
	}
	s.mu.RUnlock()

	if len(offlineIDs) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, id := range offlineIDs {
		wg.Add(1)
		go func(devID string) {
			defer wg.Done()
			client, ok := s.clients[devID]
			if !ok {
				return
			}
			if client.QuickPing() {
				var devCfg config.PDUDeviceConfig
				for _, d := range s.cfg.Devices {
					if d.ID == devID {
						devCfg = d
						break
					}
				}
				if devCfg.ID != "" {
					log.Printf("[PDU] device %s came back online, full refresh...", devID)
					s.refreshOne(devCfg, true)
				}
			}
		}(id)
	}
	wg.Wait()
}

func (s *PDUService) refreshOne(dc config.PDUDeviceConfig, isFastRecover bool) {
	device, err := s.readDevice(dc)
	now := time.Now()

	s.mu.Lock()
	prev, prevExists := s.cache[dc.ID]
	var wasOnline bool
	if prevExists {
		wasOnline = prev.Online
	}
	s.mu.Unlock()

	if err != nil {
		s.mu.Lock()
		if !prevExists {
			s.cache[dc.ID] = &model.PDUDevice{
				DeviceID:    dc.ID,
				DeviceName:  dc.Name,
				Address:     dc.Address,
				Online:      false,
				Ports:       []model.PDUPort{},
				LastUpdated: now,
			}
		} else {
			if wasOnline {
				s.recordEventLocked(dc, prev, model.EventOffline, now)
				log.Printf("[PDU] device %s went offline: %v", dc.ID, err)
			}
			prev.Online = false
			prev.LastUpdated = now
		}
		s.mu.Unlock()
		return
	}

	device.LastUpdated = now
	s.mu.Lock()
	if prevExists && !wasOnline && device.Online {
		s.recordEventLocked(dc, prev, model.EventOnline, now)
		log.Printf("[PDU] device %s is back online (power restored)", dc.ID)
	}
	s.cache[dc.ID] = device
	s.mu.Unlock()
}

func (s *PDUService) recordEventLocked(dc config.PDUDeviceConfig, prev *model.PDUDevice, evType model.EventType, now time.Time) {
	s.eventSeq++

	affectedPorts := 0
	if evType == model.EventOffline && prev != nil {
		for _, p := range prev.Ports {
			if p.PowerState == model.PowerOn {
				affectedPorts++
			}
		}
	}

	event := model.PowerEvent{
		ID:                 fmt.Sprintf("ev-%d-%d", now.UnixNano(), s.eventSeq),
		DeviceID:           dc.ID,
		DeviceName:         dc.Name,
		DeviceAddress:      dc.Address,
		DistributionBoxID:  dc.DistributionBoxID,
		EventType:          evType,
		EventTypeStr:       evType.String(),
		Timestamp:          now,
		PrevOnline:         prev != nil && prev.Online,
		TotalAffectedPorts: affectedPorts,
	}

	s.events = append(s.events, event)
	s.pruneOldEventsLocked()
}

func (s *PDUService) pruneOldEvents() {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	s.pruneOldEventsLocked()
}

func (s *PDUService) pruneOldEventsLocked() {
	cutoff := time.Now().Add(-time.Duration(s.cfg.Correlation.MaxEventAgeMin) * time.Minute)
	i := 0
	for j, e := range s.events {
		if e.Timestamp.After(cutoff) {
			i = j
			break
		}
	}
	if i > 0 {
		s.events = s.events[i:]
	}
}

func (s *PDUService) GetAllPorts() ([]model.PDUDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	devices := make([]model.PDUDevice, 0, len(s.cache))
	for _, devCfg := range s.cfg.Devices {
		if dev, ok := s.cache[devCfg.ID]; ok {
			devices = append(devices, *dev)
		} else {
			devices = append(devices, model.PDUDevice{
				DeviceID:   devCfg.ID,
				DeviceName: devCfg.Name,
				Address:    devCfg.Address,
				Online:     false,
				Ports:      []model.PDUPort{},
			})
		}
	}
	return devices, nil
}

func (s *PDUService) GetOnlineDevices() ([]model.PDUDevice, error) {
	all, err := s.GetAllPorts()
	if err != nil {
		return nil, err
	}
	var online []model.PDUDevice
	for _, d := range all {
		if d.Online {
			online = append(online, d)
		}
	}
	return online, nil
}

func (s *PDUService) GetPowerData() ([]model.PDUDevice, error) {
	return s.GetAllPorts()
}

func (s *PDUService) AnalyzeAnomalies() (*model.AnomalyAnalysisResult, error) {
	s.eventsMu.RLock()
	defer s.eventsMu.RUnlock()

	result := &model.AnomalyAnalysisResult{
		GeneratedAt: time.Now(),
		TotalEvents: len(s.events),
		Anomalies:   make([]model.CorrelatedAnomaly, 0),
	}

	window := time.Duration(s.cfg.Correlation.TimeWindowSec) * time.Second
	cutoff := time.Now().Add(-time.Duration(s.cfg.Correlation.MaxEventAgeMin) * time.Minute)

	boxEvents := make(map[string][]model.PowerEvent)
	singleOfflineEvents := make([]model.PowerEvent, 0)

	for _, ev := range s.events {
		if ev.Timestamp.Before(cutoff) {
			continue
		}
		if ev.EventType == model.EventOffline {
			if ev.DistributionBoxID != "" {
				boxEvents[ev.DistributionBoxID] = append(boxEvents[ev.DistributionBoxID], ev)
			} else {
				singleOfflineEvents = append(singleOfflineEvents, ev)
			}
		}
	}

	for boxID, events := range boxEvents {
		boxCfg, hasCfg := s.boxConfigMap[boxID]
		boxDeviceIDs := s.boxDeviceMap[boxID]
		totalDevices := len(boxDeviceIDs)
		if totalDevices == 0 {
			totalDevices = s.cfg.Correlation.MinDevices
		}

		sort.Slice(events, func(i, j int) bool {
			return events[i].Timestamp.Before(events[j].Timestamp)
		})

		clusters := clusterEvents(events, window)
		for _, cluster := range clusters {
			affected := len(cluster)
			if affected < s.cfg.Correlation.MinDevices {
				continue
			}
			ratio := float64(affected) / float64(totalDevices)
			if ratio < s.cfg.Correlation.FaultRatio && affected < s.cfg.Correlation.MinDevices {
				continue
			}

			anomaly := model.CorrelatedAnomaly{
				CorrelationID:      model.CorrelationDistributionBox,
				CorrelationType:    model.CorrelationDistributionBox,
				CorrelationTypeStr: model.CorrelationDistributionBox.String(),
				Start:              cluster[0].Timestamp,
				End:                cluster[len(cluster)-1].Timestamp,
				DistributionBoxID:  boxID,
				DistributionBoxName: func() string {
					if hasCfg {
						return boxCfg.Name
					}
					return boxID
				}(),
				DistributionBoxDesc: func() string {
					if hasCfg {
						return boxCfg.Description
					}
					return ""
				}(),
				TotalDevices:      totalDevices,
				AffectedRatio:     ratio,
				Events:            cluster,
			}

			affectedIDs := make([]string, 0, affected)
			affectedNames := make([]string, 0, affected)
			for _, ev := range cluster {
				affectedIDs = append(affectedIDs, ev.DeviceID)
				affectedNames = append(affectedNames, ev.DeviceName)
			}
			anomaly.AffectedDevices = affectedIDs
			anomaly.AffectedDeviceNames = affectedNames

			recovered := 0
			for _, id := range affectedIDs {
				if dev, ok := s.cache[id]; ok && dev.Online {
					recovered++
				}
			}
			anomaly.Recovered = recovered == affected
			if recovered > 0 && recovered < affected {
				anomaly.End = s.findLastOnlineTime(affectedIDs)
			}

			switch {
			case ratio >= 0.95:
				anomaly.Severity = model.SeverityCritical
				anomaly.Confidence = 0.98
			case ratio >= 0.8:
				anomaly.Severity = model.SeverityHigh
				anomaly.Confidence = 0.9
			case ratio >= 0.7:
				anomaly.Severity = model.SeverityMedium
				anomaly.Confidence = 0.75
			default:
				anomaly.Severity = model.SeverityLow
				anomaly.Confidence = 0.6
			}

			switch {
			case anomaly.Severity == model.SeverityCritical:
				anomaly.Summary = fmt.Sprintf("%s 级配电箱故障：%d/%d 台设备在 %.0f 秒内同时掉电",
					anomaly.Severity, affected, totalDevices, anomaly.End.Sub(anomaly.Start).Seconds())
			default:
				anomaly.Summary = fmt.Sprintf("检测到 %s 下 %d 台设备集中掉电",
					anomaly.DistributionBoxName, affected)
			}

			switch anomaly.Severity {
			case model.SeverityCritical:
				anomaly.SuggestedAction = "立即排查：1) 检查配电箱主空开是否跳闸 2) 检查配电房进线电压 3) 通知电工现场检查并切换备用电源"
			case model.SeverityHigh:
				anomaly.SuggestedAction = "优先排查：1) 检查配电箱回路空开 2) 检查电路接线端子是否松动 3) 核实是否为过载保护触发"
			case model.SeverityMedium:
				anomaly.SuggestedAction = "建议排查：1) 核实是否有计划性断电 2) 检查相关设备电源模块健康状态 3) 持续观察后续是否再次出现"
			default:
				anomaly.SuggestedAction = "继续观察：关联度较低，可能是多个独立事件的巧合"
			}

			result.Anomalies = append(result.Anomalies, anomaly)
		}
	}

	for _, ev := range s.events {
		if ev.EventType != model.EventOffline || ev.Timestamp.Before(cutoff) {
			continue
		}

		isPartOfCorrelated := false
		for _, anom := range result.Anomalies {
			if anom.CorrelationType == model.CorrelationDistributionBox && anom.DistributionBoxID == ev.DistributionBoxID {
				for _, devID := range anom.AffectedDevices {
					if devID == ev.DeviceID {
						isPartOfCorrelated = true
						break
					}
				}
			}
		}
		if isPartOfCorrelated {
			continue
		}

		if dev, ok := s.cache[ev.DeviceID]; !ok || !dev.Online {
			continue
		}

		anomaly := model.CorrelatedAnomaly{
			CorrelationID:      model.CorrelationSingleDevice,
			CorrelationType:    model.CorrelationSingleDevice,
			CorrelationTypeStr: model.CorrelationSingleDevice.String(),
			Severity:           model.SeverityLow,
			Summary:            fmt.Sprintf("单设备掉电：%s (%s)", ev.DeviceName, ev.DeviceID),
			Confidence:         0.9,
			Start:              ev.Timestamp,
			End:                ev.Timestamp,
			Recovered: func() bool {
				for _, re := range s.events {
					if re.DeviceID == ev.DeviceID && re.EventType == model.EventOnline && re.Timestamp.After(ev.Timestamp) {
						return true
					}
				}
				return false
			}(),
			DistributionBoxID:  ev.DistributionBoxID,
			DistributionBoxName: func() string {
				if b, ok := s.boxConfigMap[ev.DistributionBoxID]; ok {
					return b.Name
				}
				return ""
			}(),
			TotalDevices:      1,
			AffectedDevices:   []string{ev.DeviceID},
			AffectedDeviceNames: []string{ev.DeviceName},
			Events:            []model.PowerEvent{ev},
			AffectedRatio:     0,
			SuggestedAction:   "检查该设备自身：1) PDU端口空开 2) 设备电源模块 3) 电源连接线缆",
		}
		result.Anomalies = append(result.Anomalies, anomaly)
	}

	sort.Slice(result.Anomalies, func(i, j int) bool {
		sevOrder := map[model.OutageSeverity]int{
			model.SeverityCritical: 0,
			model.SeverityHigh:     1,
			model.SeverityMedium:   2,
			model.SeverityLow:      3,
		}
		if sevOrder[result.Anomalies[i].Severity] != sevOrder[result.Anomalies[j].Severity] {
			return sevOrder[result.Anomalies[i].Severity] < sevOrder[result.Anomalies[j].Severity]
		}
		return result.Anomalies[i].Start.After(result.Anomalies[j].Start)
	})

	return result, nil
}

func (s *PDUService) findLastOnlineTime(deviceIDs []string) time.Time {
	last := time.Time{}
	for _, id := range deviceIDs {
		for _, ev := range s.events {
			if ev.DeviceID == id && ev.EventType == model.EventOnline && ev.Timestamp.After(last) {
				last = ev.Timestamp
			}
		}
	}
	if last.IsZero() {
		last = time.Now()
	}
	return last
}

func clusterEvents(events []model.PowerEvent, window time.Duration) [][]model.PowerEvent {
	if len(events) == 0 {
		return nil
	}

	var clusters [][]model.PowerEvent
	currentCluster := []model.PowerEvent{events[0]}

	for i := 1; i < len(events); i++ {
		if events[i].Timestamp.Sub(currentCluster[len(currentCluster)-1].Timestamp) <= window {
			currentCluster = append(currentCluster, events[i])
		} else {
			clusters = append(clusters, currentCluster)
			currentCluster = []model.PowerEvent{events[i]}
		}
	}
	if len(currentCluster) > 0 {
		clusters = append(clusters, currentCluster)
	}

	return clusters
}

func (s *PDUService) readDevice(dc config.PDUDeviceConfig) (*model.PDUDevice, error) {
	client, ok := s.clients[dc.ID]
	if !ok {
		return nil, fmt.Errorf("no snmp client for device %s", dc.ID)
	}

	device := &model.PDUDevice{
		DeviceID:   dc.ID,
		DeviceName: dc.Name,
		Address:    dc.Address,
		Ports:      make([]model.PDUPort, 0, dc.PortCount),
	}

	if err := client.Connect(); err != nil {
		device.Online = false
		return device, fmt.Errorf("connect failed: %w", err)
	}
	defer client.Close()

	device.Online = true

	statusMap, err := client.Walk(snmp.OIDPortStatus)
	if err != nil {
		log.Printf("[PDU] walk port status on %s failed: %v", dc.ID, err)
	}
	nameMap, err := client.Walk(snmp.OIDPortName)
	if err != nil {
		log.Printf("[PDU] walk port name on %s failed: %v", dc.ID, err)
	}
	voltageMap, err := client.Walk(snmp.OIDPortVoltage)
	if err != nil {
		log.Printf("[PDU] walk voltage on %s failed: %v", dc.ID, err)
	}
	currentMap, err := client.Walk(snmp.OIDPortCurrent)
	if err != nil {
		log.Printf("[PDU] walk current on %s failed: %v", dc.ID, err)
	}
	powerMap, err := client.Walk(snmp.OIDPortPower)
	if err != nil {
		log.Printf("[PDU] walk power on %s failed: %v", dc.ID, err)
	}

	totalPower := 0.0
	for i := 1; i <= dc.PortCount; i++ {
		port := model.PDUPort{
			PortIndex: i,
		}

		statusOID := fmt.Sprintf("%s.%d", snmp.OIDPortStatus, i)
		if v, ok := statusMap[statusOID]; ok {
			state := snmp.ToInt(v)
			port.PowerState = model.PortPowerState(state)
			port.PowerStateStr = port.PowerState.String()
		} else {
			port.PowerState = model.PowerOff
			port.PowerStateStr = "off"
		}

		nameOID := fmt.Sprintf("%s.%d", snmp.OIDPortName, i)
		if v, ok := nameMap[nameOID]; ok {
			port.PortName = snmp.ToString(v)
		} else {
			port.PortName = fmt.Sprintf("Port %d", i)
		}

		voltageOID := fmt.Sprintf("%s.%d", snmp.OIDPortVoltage, i)
		if v, ok := voltageMap[voltageOID]; ok {
			port.Voltage = snmp.ToFloat(v) / 10.0
		}

		currentOID := fmt.Sprintf("%s.%d", snmp.OIDPortCurrent, i)
		if v, ok := currentMap[currentOID]; ok {
			port.Current = snmp.ToFloat(v) / 10.0
		}

		powerOID := fmt.Sprintf("%s.%d", snmp.OIDPortPower, i)
		if v, ok := powerMap[powerOID]; ok {
			port.PowerWatts = snmp.ToFloat(v)
		}

		totalPower += port.PowerWatts
		device.Ports = append(device.Ports, port)
	}

	totalPowerResult, err := client.Get([]string{snmp.OIDDeviceTotalPower + ".0"})
	if err == nil {
		if v, ok := totalPowerResult[snmp.OIDDeviceTotalPower+".0"]; ok {
			device.TotalPower = snmp.ToFloat(v)
		}
	}
	if device.TotalPower == 0 {
		device.TotalPower = totalPower
	}

	return device, nil
}
