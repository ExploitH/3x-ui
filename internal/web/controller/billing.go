package controller

import (
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// BillingController exposes admin-only cost and physical traffic views. Provider
// snapshots are supplied as explicit, redacted cycle imports; this controller
// never calls a provider API or mutates production infrastructure.
type BillingController struct{}

func NewBillingController(g *gin.RouterGroup) *BillingController {
	a := &BillingController{}
	g.GET("/summary", a.summary)
	g.GET("/cycles/:nodeId", a.cycles)
	g.POST("/cycles", a.importCycle)
	return a
}

func (a *BillingController) summary(c *gin.Context) {
	asOf := time.Now().UnixMilli()
	if raw := c.Query("asOf"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			if err == nil {
				err = errors.New("asOf must be positive")
			}
			jsonMsg(c, "invalid asOf", err)
			return
		}
		asOf = parsed
	}
	result, err := service.CalculateMonthlyBilling(database.GetDB(), asOf)
	if err != nil {
		jsonMsg(c, "failed to calculate billing", err)
		return
	}
	jsonObj(c, result, nil)
}

func (a *BillingController) cycles(c *gin.Context) {
	nodeID, err := strconv.Atoi(c.Param("nodeId"))
	if err != nil {
		jsonMsg(c, "invalid nodeId", err)
		return
	}
	var from, to int64
	if raw := c.Query("from"); raw != "" {
		from, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			jsonMsg(c, "invalid from", err)
			return
		}
	}
	if raw := c.Query("to"); raw != "" {
		to, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			jsonMsg(c, "invalid to", err)
			return
		}
	}
	rows, err := service.ListNodeTrafficCycles(database.GetDB(), nodeID, from, to)
	if err != nil {
		jsonMsg(c, "failed to list traffic cycles", err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *BillingController) importCycle(c *gin.Context) {
	var input service.NodeTrafficCycleInput
	if err := c.ShouldBindJSON(&input); err != nil {
		jsonMsg(c, "invalid traffic cycle", err)
		return
	}
	if err := service.UpsertNodeTrafficCycle(database.GetDB(), input); err != nil {
		jsonMsg(c, "failed to import traffic cycle", err)
		return
	}
	row, err := service.GetNodeTrafficCycle(database.GetDB(), input.NodeId, input.CycleStart, input.Source)
	if err != nil {
		jsonMsg(c, "traffic cycle imported but read-back failed", err)
		return
	}
	jsonObj(c, row, nil)
}
