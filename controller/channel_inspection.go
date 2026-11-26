package controller

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type inspectionQuestion struct {
	Prompt string `json:"prompt"`
	Answer string `json:"answer"`
}
type inspectionConfig struct {
	ReasoningEffort string               `json:"reasoning_effort"`
	DegradeAfter    int                  `json:"degrade_after"`
	Title           string               `json:"title"`
	Published       bool                 `json:"published"`
	Scheduled       bool                 `json:"scheduled"`
	Minutes         int                  `json:"minutes"`
	Limit           int                  `json:"limit"`
	Model           string               `json:"model"`
	Questions       []inspectionQuestion `json:"questions"`
}
type accountMark struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Reason string `json:"reason,omitempty"`
	Time   int64  `json:"time"`
	Result string `json:"result"`
	Number int    `json:"number"`
}
type inspectionRound struct {
	Prompt   string        `json:"prompt"`
	Model    string        `json:"model"`
	Time     int64         `json:"time"`
	Sequence int           `json:"sequence"`
	Failed   bool          `json:"failed"`
	Accounts []accountMark `json:"accounts"`
}

var inspecting sync.Map

func inspectionKey(c *model.Channel, kind string) string {
	return fmt.Sprintf("ProviderInspection/%d/%s", c.Id, kind)
}
func readInspection(c *model.Channel, kind string, dst any) {
	common.OptionMapRWMutex.RLock()
	v := common.OptionMap[inspectionKey(c, kind)]
	common.OptionMapRWMutex.RUnlock()
	if v != "" {
		_ = common.Unmarshal([]byte(v), dst)
	}
}
func inspectionSettings(c *model.Channel) inspectionConfig {
	cfg := inspectionConfig{Title: c.Name, Minutes: 30, Questions: []inspectionQuestion{{"只输出整数。37 乘以 19 等于多少？", "703"}, {"只输出整数。2 小时 17 分钟一共是多少分钟？", "137"}, {"只输出整数。3、8、13、18、29 的中位数是多少？", "13"}}}
	readInspection(c, "settings", &cfg)
	return cfg
}
func (cfg inspectionConfig) valid() bool {
	if cfg.DegradeAfter < 0 || cfg.DegradeAfter > 20 || !slices.Contains([]string{"", "none", "minimal", "low", "medium", "high", "xhigh"}, cfg.ReasoningEffort) {
		return false
	}
	if cfg.Minutes < 1 || cfg.Minutes > 10080 || cfg.Limit < 0 || cfg.Limit > 1000000 || len(cfg.Title) > 160 || (cfg.Published && strings.TrimSpace(cfg.Title) == "") || len(cfg.Model) > 160 || (cfg.Scheduled && strings.TrimSpace(cfg.Model) == "") || len(cfg.Questions) < 1 || len(cfg.Questions) > 32 {
		return false
	}
	for _, q := range cfg.Questions {
		if strings.TrimSpace(q.Prompt) == "" || strings.TrimSpace(q.Answer) == "" || len(q.Prompt) > 2048 || len(q.Answer) > 256 {
			return false
		}
	}
	return true
}
func inspectionChannels() ([]model.Channel, error) {
	var all []model.Channel
	err := model.DB.Where("type = ?", constant.ChannelTypeTaskPlugin).Order("id").Find(&all).Error
	result := all[:0]
	for _, ch := range all {
		if service.IsProviderPool(&ch) {
			result = append(result, ch)
		}
	}
	return result, err
}
func inspectionTarget(c *gin.Context) *model.Channel {
	id, err := strconv.Atoi(c.Param("id"))
	if err == nil && id > 0 {
		ch, err := model.GetChannelById(id, true)
		if err == nil && service.IsProviderPool(ch) {
			return ch
		}
	}
	common.ApiErrorT(c, "Invalid parameters")
	return nil
}
func ListChannelInspections(c *gin.Context)     { listChannelInspections(c, true) }
func CustomerChannelInspections(c *gin.Context) { listChannelInspections(c, false) }
func listChannelInspections(c *gin.Context, admin bool) {
	channels, err := inspectionChannels()
	if err != nil {
		common.ApiErrorT(c, "Request failed")
		return
	}
	if !admin {
		userGroup, groupErr := model.GetUserGroup(c.GetInt("id"), false)
		if groupErr != nil {
			common.ApiErrorT(c, "Request failed")
			return
		}
		visible := channels[:0]
		for _, channel := range channels {
			for _, group := range channel.GetGroups() {
				if service.GroupInUserUsableGroups(userGroup, group) {
					visible = append(visible, channel)
					break
				}
			}
		}
		channels = visible
	}
	rows := make([]gin.H, 0, len(channels))
	for _, ch := range channels {
		cfg := inspectionSettings(&ch)
		if !admin && !cfg.Published {
			continue
		}
		var last inspectionRound
		readInspection(&ch, "last", &last)
		_, running := inspecting.Load(ch.Id)
		cards := make([]gin.H, 0, len(last.Accounts))
		for _, a := range last.Accounts {
			card := gin.H{"code": common.GenerateHMAC(fmt.Sprintf("quality/%d/%s", ch.Id, a.ID))[:10], "time": a.Time, "result": a.Result, "number": a.Number}
			if admin {
				card["account_id"] = a.ID
				card["label"] = a.Label
				if a.Reason != "" {
					card["reason"] = a.Reason
				}
			}
			cards = append(cards, card)
		}
		row := gin.H{"name": ch.Name, "groups": strings.Split(ch.Group, ","), "title": cfg.Title, "accounts": cards, "time": last.Time, "failed": last.Failed, "running": running, "scheduled": cfg.Scheduled, "minutes": cfg.Minutes}
		if admin {
			row["id"] = ch.Id
			row["name"] = ch.Name
			row["settings"] = cfg
			row["question"] = last.Prompt
			row["model"] = last.Model
		}
		rows = append(rows, row)
	}
	common.ApiSuccess(c, rows)
}
func SaveChannelInspection(c *gin.Context) {
	ch := inspectionTarget(c)
	if ch == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 96<<10)
	var cfg inspectionConfig
	if c.ShouldBindJSON(&cfg) != nil || !cfg.valid() {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Title = strings.TrimSpace(cfg.Title)
	raw, err := common.Marshal(cfg)
	if err == nil {
		err = model.UpdateOptionsBulk(map[string]string{inspectionKey(ch, "settings"): string(raw), service.ChannelLimitKey(ch.Id): strconv.Itoa(cfg.Limit)})
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "channel.update", map[string]any{"source": "inspection_settings", "channel_id": ch.Id})
	common.ApiSuccess(c, nil)
}
func RunChannelInspection(c *gin.Context) {
	ch := inspectionTarget(c)
	if ch == nil {
		return
	}
	cfg := inspectionSettings(ch)
	if strings.TrimSpace(cfg.Model) == "" {
		common.ApiErrorT(c, "Model is required")
		return
	}
	if !launchInspection(*ch, cfg) {
		common.ApiErrorT(c, "Inspection already running")
		return
	}
	recordManageAudit(c, "channel.test", map[string]any{"channel_id": ch.Id})
	common.ApiSuccess(c, nil)
}
func launchInspection(ch model.Channel, cfg inspectionConfig) bool {
	if _, busy := inspecting.LoadOrStore(ch.Id, true); busy {
		return false
	}
	go func() {
		defer inspecting.Delete(ch.Id)
		if err := inspectChannel(ch, cfg); err != nil {
			common.SysError("channel inspection failed: " + err.Error())
		}
	}()
	return true
}
func inspectChannel(ch model.Channel, cfg inspectionConfig) error {
	return inspectProviderPoolChannel(ch, cfg)
}
func StartChannelInspectionSchedule() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for now := range ticker.C {
			if !common.IsMasterNode {
				continue
			}
			channels, err := inspectionChannels()
			if err != nil {
				continue
			}
			for _, ch := range channels {
				cfg := inspectionSettings(&ch)
				if !cfg.Scheduled || !cfg.valid() {
					continue
				}
				var last inspectionRound
				readInspection(&ch, "last", &last)
				if now.Unix()-last.Time >= int64(cfg.Minutes*60) {
					launchInspection(ch, cfg)
				}
			}
		}
	}()
}
