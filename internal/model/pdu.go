package model

type PortPowerState int

const (
	PowerOff PortPowerState = 0
	PowerOn  PortPowerState = 1
)

func (s PortPowerState) String() string {
	switch s {
	case PowerOn:
		return "on"
	case PowerOff:
		return "off"
	default:
		return "unknown"
	}
}

type PDUPort struct {
	PortIndex    int            `json:"port_index"`
	PortName     string         `json:"port_name"`
	PowerState   PortPowerState `json:"power_state"`
	PowerStateStr string        `json:"power_state_str"`
	Voltage      float64        `json:"voltage"`
	Current      float64        `json:"current"`
	PowerWatts   float64        `json:"power_watts"`
}

type PDUDevice struct {
	DeviceID    string    `json:"device_id"`
	DeviceName  string    `json:"device_name"`
	Address     string    `json:"address"`
	Online      bool      `json:"online"`
	TotalPower  float64   `json:"total_power_watts"`
	Ports       []PDUPort `json:"ports"`
}

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
