package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// Account inspections use the same chat converter and provider adaptor as the
// playground, pinned to one stable account ID with no fallback to a peer.
func inspectProviderPoolChannel(channel model.Channel, cfg inspectionConfig) error {
	if !cfg.valid() || cfg.Model == "" {
		return errors.New("invalid inspection settings")
	}
	var previous inspectionRound
	readInspection(&channel, "last", &previous)
	index := previous.Sequence % len(cfg.Questions)
	question := cfg.Questions[index]
	round := inspectionRound{Sequence: previous.Sequence + 1, Prompt: question.Prompt, Model: cfg.Model, Accounts: []accountMark{}}
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return err
	}
	accounts, err := service.ListProviderPoolAccounts(context.Background(), &channel)
	if err != nil {
		return err
	}
	category := service.GetProviderPoolSettings(channel.Id).Category
	for _, account := range accounts {
		if !service.ProviderAccountMatches(account, category) {
			continue
		}
		mark := accountMark{ID: account.AccountID, Label: account.Label, Time: time.Now().Unix(), Number: index + 1, Result: "disabled"}
		if !service.ProviderAccountUsable(account) {
			if account.NeedsLogin {
				mark.Result = "login_required"
			}
			if account.State.Health == "account_banned" {
				mark.Result = "account_banned"
			}
			round.Accounts = append(round.Accounts, mark)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		result := testChannel(ctx, &channel, testUserID, cfg.Model, string(constant.EndpointTypeOpenAI), false,
			accountInspectionInput{AccountID: account.AccountID, Prompt: question.Prompt, Effort: cfg.ReasoningEffort})
		cancel()
		mark.Result = "inconclusive"
		if result.localErr == nil && result.newAPIError == nil {
			var response struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if common.Unmarshal(result.response, &response) == nil && len(response.Choices) == 1 {
				mark.Result = "wrong_answer"
				if strings.TrimSpace(response.Choices[0].Message.Content) == strings.TrimSpace(question.Answer) {
					mark.Result = "passed"
				}
			}
		} else if result.statusCode == 429 {
			mark.Result = "rate_limited"
		} else if result.statusCode == http.StatusUnauthorized {
			mark.Result = "login_required"
		} else if result.statusCode == http.StatusForbidden && result.context != nil && result.context.GetBool("provider_pool_rejected") {
			mark.Result = "account_banned"
		}
		current, findErr := service.FindProviderPoolAccount(context.Background(), &channel, account.AccountID)
		if findErr == nil && current.NeedsLogin {
			mark.Result = "login_required"
		}
		threshold := cfg.DegradeAfter
		if threshold == 0 {
			threshold = 3
		}
		err := service.UpdateProviderAccount(account.OwnerID, account.AccountID, func(state *model.ProviderAccountState) {
			state.Health = mark.Result
			state.CheckedAt = time.Now().Unix()
			if mark.Result == "login_required" || mark.Result == "account_banned" {
				state.Disabled = true
			}
			if mark.Result == "passed" {
				state.WrongAnswers = 0
			}
			if mark.Result == "wrong_answer" {
				state.WrongAnswers++
				if state.WrongAnswers >= threshold {
					state.Degraded = true
					state.Disabled = true
				}
			}
			if mark.Result == "account_banned" {
				state.Disabled = true
			}
		})
		if err != nil {
			round.Failed = true
		}
		round.Accounts = append(round.Accounts, mark)
	}
	round.Time = time.Now().Unix()
	raw, err := common.Marshal(round)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{inspectionKey(&channel, "last"): string(raw)})
}
