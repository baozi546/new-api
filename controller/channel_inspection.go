package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type inspectionQuestion struct {
	Prompt string `json:"prompt"`
	Answer string `json:"answer"`
}
type inspectionConfig struct {
	Title     string               `json:"title"`
	Published bool                 `json:"published"`
	Scheduled bool                 `json:"scheduled"`
	Minutes   int                  `json:"minutes"`
	Limit     int                  `json:"limit"`
	Model     string               `json:"model"`
	Questions []inspectionQuestion `json:"questions"`
}
type accountMark struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
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
	return fmt.Sprintf("CPAInspection/%d/%s/%s", c.Id, c.CPAPoolID, kind)
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
	err := model.DB.Select("id", "name", "base_url", "type", "cpa_pool_id").Where("cpa_pool_id <> ?", "").Order("id").Find(&all).Error
	result := all[:0]
	for _, ch := range all {
		if matchesCPAChannel(&ch) {
			result = append(result, ch)
		}
	}
	return result, err
}
func inspectionTarget(c *gin.Context) *model.Channel {
	id, err := strconv.Atoi(c.Param("id"))
	if err == nil && id > 0 {
		ch, err := model.GetChannelById(id, false)
		if err == nil && ch.CPAPoolID != "" && matchesCPAChannel(ch) {
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
				card["label"] = a.Label
			}
			cards = append(cards, card)
		}
		row := gin.H{"title": cfg.Title, "accounts": cards, "time": last.Time, "failed": last.Failed, "running": running, "scheduled": cfg.Scheduled, "minutes": cfg.Minutes}
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
	var prev inspectionRound
	readInspection(&ch, "last", &prev)
	round := inspectionRound{Sequence: prev.Sequence + 1, Accounts: []accountMark{}}
	if !cfg.valid() || cfg.Model == "" {
		return errors.New("invalid inspection settings")
	}
	index := prev.Sequence % len(cfg.Questions)
	q := cfg.Questions[index]
	ctx := context.Background()
	accounts, err := service.ListCPAAccounts(ctx)
	if err != nil {
		round = prev
		round.Failed = true
	} else {
		round.Prompt = q.Prompt
		round.Model = cfg.Model
		for _, a := range accounts {
			if a.ChannelPoolID != ch.CPAPoolID {
				continue
			}
			mark := accountMark{ID: a.ID, Label: a.Label, Result: "disabled"}
			if mark.Label == "" {
				mark.Label = a.ID
			}
			if !a.Disabled {
				mark.Number = index + 1
				var reply struct {
					Check service.CPACheck `json:"check"`
				}
				release, err := service.AcquireChannelSlot(ctx, ch.Id)
				if err != nil {
					return err
				}
				func() {
					defer release()
					err = service.CallCPAManagement(ctx, "POST", "credentials/quality-check", map[string]string{"name": a.ID, "channel_pool_id": ch.CPAPoolID, "model": cfg.Model, "question": q.Prompt, "answer": q.Answer}, &reply)
				}()
				mark.Time = time.Now().Unix()
				mark.Result = "inconclusive"
				if err == nil && reply.Check.Question == strings.TrimSpace(q.Prompt) {
					switch reply.Check.Result {
					case "passed", "wrong_answer", "rate_limited", "login_required", "inconclusive":
						mark.Result = reply.Check.Result
					}
				}
			}
			round.Accounts = append(round.Accounts, mark)
		}
		sort.Slice(round.Accounts, func(i, j int) bool { return round.Accounts[i].ID < round.Accounts[j].ID })
	}
	round.Time = time.Now().Unix()
	raw, err := common.Marshal(round)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{inspectionKey(&ch, "last"): string(raw)})
}
func StartChannelInspectionSchedule() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for now := range ticker.C {
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
