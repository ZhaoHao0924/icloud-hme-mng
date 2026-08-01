package server

import "github.com/gin-gonic/gin"

const serviceName = "icloud-hme"

type healthData struct {
	Service         string `json:"service"`
	Version         string `json:"version"`
	Status          string `json:"status"`
	ConfigAvailable bool   `json:"config_available"`
}

func (s *Server) health(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	configAvailable := s.mgr.ConfigAvailable()
	status := "ok"
	if !configAvailable {
		status = "degraded"
	}
	ok(c, healthData{
		Service:         serviceName,
		Version:         s.version,
		Status:          status,
		ConfigAvailable: configAvailable,
	})
}
