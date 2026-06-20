package service

import (
	"fmt"
	"log"
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
	return &PDUService{
		cfg:      cfg,
		clients:  clients,
		cache:    make(map[string]*model.PDUDevice),
		stopCh:   make(chan struct{}),
		fastPoll: make(map[string]bool),
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
	defer normalTicker.Stop()
	defer fastTicker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-normalTicker.C:
			s.refreshAll()
		case <-fastTicker.C:
			s.refreshOfflineFast()
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
	defer s.mu.Unlock()

	if err != nil {
		prev, exists := s.cache[dc.ID]
		if !exists {
			s.cache[dc.ID] = &model.PDUDevice{
				DeviceID:    dc.ID,
				DeviceName:  dc.Name,
				Address:     dc.Address,
				Online:      false,
				Ports:       []model.PDUPort{},
				LastUpdated: now,
			}
		} else {
			if prev.Online {
				log.Printf("[PDU] device %s went offline: %v", dc.ID, err)
			}
			prev.Online = false
			prev.LastUpdated = now
		}
		return
	}

	device.LastUpdated = now
	prev, exists := s.cache[dc.ID]
	if exists && !prev.Online && device.Online {
		log.Printf("[PDU] device %s is back online (power restored)", dc.ID)
	}
	s.cache[dc.ID] = device
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
