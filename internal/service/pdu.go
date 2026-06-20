package service

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"pdu-monitor/internal/config"
	"pdu-monitor/internal/model"
	"pdu-monitor/internal/snmp"
)

type PDUService struct {
	cfg     *config.Config
	clients map[string]*snmp.Client
	mu      sync.RWMutex
	cache   map[string]*model.PDUDevice
}

func NewPDUService(cfg *config.Config) *PDUService {
	clients := make(map[string]*snmp.Client)
	for _, dev := range cfg.Devices {
		clients[dev.ID] = snmp.NewClient(
			dev.Address,
			cfg.SNMP.Community,
			cfg.SNMP.Timeout,
			cfg.SNMP.Retries,
		)
	}
	return &PDUService{
		cfg:     cfg,
		clients: clients,
		cache:   make(map[string]*model.PDUDevice),
	}
}

func (s *PDUService) GetAllPorts() ([]model.PDUDevice, error) {
	var devices []model.PDUDevice
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, len(s.cfg.Devices))

	for _, devCfg := range s.cfg.Devices {
		wg.Add(1)
		go func(dc config.PDUDeviceConfig) {
			defer wg.Done()
			device, err := s.readDevice(dc)
			if err != nil {
				errCh <- fmt.Errorf("device %s: %w", dc.ID, err)
				return
			}
			mu.Lock()
			devices = append(devices, *device)
			mu.Unlock()
		}(devCfg)
	}

	wg.Wait()
	close(errCh)

	var errs []string
	for e := range errCh {
		errs = append(errs, e.Error())
	}

	if len(errs) == len(s.cfg.Devices) {
		return nil, fmt.Errorf("all devices failed: %s", strings.Join(errs, "; "))
	}

	s.mu.Lock()
	for i := range devices {
		s.cache[devices[i].DeviceID] = &devices[i]
	}
	s.mu.Unlock()

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
		log.Printf("[PDU] device %s (%s) offline: %v", dc.ID, dc.Address, err)
		return device, nil
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
