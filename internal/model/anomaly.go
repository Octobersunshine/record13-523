package model

import "time"

type EventType int

const (
	EventOffline  EventType = iota
	EventOnline
)

func (e EventType) String() string {
	switch e {
	case EventOffline:
		return "offline"
	case EventOnline:
		return "online"
	}
	return "unknown"
}

type PowerEvent struct {
	ID                string    `json:"id"`
	DeviceID          string    `json:"device_id"`
	DeviceName        string    `json:"device_name"`
	DeviceAddress    string    `json:"device_address"`
	DistributionBoxID string   `json:"distribution_box_id"`
	EventType         EventType `json:"event_type"`
	EventTypeStr      string    `json:"event_type_str"`
	Timestamp         time.Time `json:"timestamp"`
	PrevOnline        bool      `json:"prev_online"`
	TotalAffectedPorts int      `json:"total_affected_ports"`
}

type CorrelationType int

const (
	CorrelationNone CorrelationType = iota
	CorrelationSingleDevice
	CorrelationDistributionBox
	CorrelationUnknown
)

func (c CorrelationType) String() string {
	switch c {
	case CorrelationNone:
		return "no_outage"
	case CorrelationSingleDevice:
		return "single_device_fault"
	case CorrelationDistributionBox:
		return "distribution_box_fault"
	}
	return "unknown"
}

type OutageSeverity string

const (
	SeverityLow      OutageSeverity = "low"
	SeverityMedium   OutageSeverity = "medium"
	SeverityHigh     OutageSeverity = "high"
	SeverityCritical OutageSeverity = "critical"
)

type CorrelatedAnomaly struct {
	CorrelationID      CorrelationType `json:"correlation_id"`
	CorrelationType   CorrelationType `json:"correlation_type"`
	CorrelationTypeStr string          `json:"correlation_type_str"`
	Severity          OutageSeverity  `json:"severity"`
	Summary           string        `json:"summary"`
	Confidence        float64       `json:"confidence"`
	Start             time.Time     `json:"start_time"`
	End               time.Time     `json:"end_time"`
	Recovered         bool          `json:"recovered"`
	DistributionBoxID    string        `json:"distribution_box_id"`
	DistributionBoxName string        `json:"distribution_box_name"`
	DistributionBoxDesc string        `json:"distribution_box_description"`
	TotalDevices      int           `json:"total_devices_in_box"`
	AffectedDevices   []string      `json:"affected_device_ids"`
	AffectedDeviceNames []string    `json:"affected_device_names"`
	Events            []PowerEvent  `json:"events"`
	AffectedRatio     float64       `json:"affected_ratio"`
	SuggestedAction  string       `json:"suggested_action"`
}

type AnomalyAnalysisResult struct {
	GeneratedAt       time.Time           `json:"generated_at"`
	TotalEvents     int               `json:"total_events_in_window"`
	Anomalies     []CorrelatedAnomaly `json:"anomalies"`
}
