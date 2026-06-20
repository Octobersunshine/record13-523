package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"pdu-monitor/internal/model"
	"pdu-monitor/internal/service"
)

type PDUHandler struct {
	svc *service.PDUService
}

func NewPDUHandler(svc *service.PDUService) *PDUHandler {
	return &PDUHandler{svc: svc}
}

func (h *PDUHandler) GetAllPorts(c *gin.Context) {
	devices, err := h.svc.GetAllPorts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{
			Code:    500,
			Message: err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{
		Code:    0,
		Message: "success",
		Data:    devices,
	})
}

func (h *PDUHandler) GetOnlineDevices(c *gin.Context) {
	devices, err := h.svc.GetOnlineDevices()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{
			Code:    500,
			Message: err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{
		Code:    0,
		Message: "success",
		Data:    devices,
	})
}

func (h *PDUHandler) GetPowerData(c *gin.Context) {
	devices, err := h.svc.GetPowerData()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{
			Code:    500,
			Message: err.Error(),
		})
		return
	}

	type PowerSummary struct {
		DeviceID    string  `json:"device_id"`
		DeviceName  string  `json:"device_name"`
		Address     string  `json:"address"`
		Online      bool    `json:"online"`
		TotalPower  float64 `json:"total_power_watts"`
		PortCount   int     `json:"port_count"`
		OnCount     int     `json:"ports_on"`
		OffCount    int     `json:"ports_off"`
	}

	var summaries []PowerSummary
	for _, d := range devices {
		on, off := 0, 0
		for _, p := range d.Ports {
			if p.PowerState == model.PowerOn {
				on++
			} else {
				off++
			}
		}
		summaries = append(summaries, PowerSummary{
			DeviceID:   d.DeviceID,
			DeviceName: d.DeviceName,
			Address:    d.Address,
			Online:     d.Online,
			TotalPower: d.TotalPower,
			PortCount:  len(d.Ports),
			OnCount:    on,
			OffCount:   off,
		})
	}

	c.JSON(http.StatusOK, model.APIResponse{
		Code:    0,
		Message: "success",
		Data:    summaries,
	})
}

func (h *PDUHandler) GetAnomalies(c *gin.Context) {
	result, err := h.svc.AnalyzeAnomalies()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.APIResponse{
			Code:    500,
			Message: err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, model.APIResponse{
		Code:    0,
		Message: "success",
		Data:    result,
	})
}

func RegisterRoutes(r *gin.Engine, h *PDUHandler) {
	v1 := r.Group("/api/v1/pdu")
	{
		v1.GET("/ports", h.GetAllPorts)
		v1.GET("/online", h.GetOnlineDevices)
		v1.GET("/power", h.GetPowerData)
		v1.GET("/anomalies", h.GetAnomalies)
	}
}
