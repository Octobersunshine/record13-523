package main

import (
	"flag"
	"log"

	"github.com/gin-gonic/gin"

	"pdu-monitor/internal/config"
	"pdu-monitor/internal/handler"
	"pdu-monitor/internal/service"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("loaded %d PDU device(s) from config", len(cfg.Devices))
	for _, d := range cfg.Devices {
		log.Printf("  - %s (%s) @ %s", d.Name, d.ID, d.Address)
	}

	pduService := service.NewPDUService(cfg)
	pduService.Start()
	defer pduService.Stop()

	pduHandler := handler.NewPDUHandler(pduService)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	handler.RegisterRoutes(r, pduHandler)

	log.Printf("PDU monitor starting on %s", cfg.Server.Addr)
	if err := r.Run(cfg.Server.Addr); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
