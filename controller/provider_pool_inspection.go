package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
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
	for _, account := range accounts {
		mark := accountMark{ID: account.AccountID, Label: account.Label, Time: time.Now().Unix(), Number: index + 1, Result: "disabled"}
		if !service.ProviderAccountUsable(account) {
			if account.NeedsLogin {
				mark.Result = "login_required"
				mark.Reason = "Account login is required"
			}
			if account.State.Health == "account_banned" {
				mark.Result = "account_banned"
				mark.Reason = "Account was marked as banned"
			}
			if mark.Reason == "" {
				mark.Reason = "Account is disabled"
			}
			round.Accounts = append(round.Accounts, mark)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		result := testChannel(ctx, &channel, testUserID, cfg.Model, string(constant.EndpointTypeOpenAI), false,
			accountInspectionInput{AccountID: account.AccountID, Group: account.Profile.Category, Prompt: question.Prompt, Effort: cfg.ReasoningEffort})
		cancel()
		mark.Result = "inconclusive"
		mark.Reason = inspectionCallFailureReason(result, &channel)
		statusCode := result.statusCode
		if statusCode == 0 && result.newAPIError != nil {
			statusCode = result.newAPIError.StatusCode
		}
		if result.localErr == nil && result.newAPIError == nil {
			var response struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if common.Unmarshal(result.response, &response) == nil && len(response.Choices) == 1 {
				actual := response.Choices[0].Message.Content
				if inspectionAnswerMatches(actual, question.Answer) {
					mark.Result = "passed"
				} else {
					mark.Result = "wrong_answer"
					mark.Reason = inspectionMismatchReason(question.Answer, actual)
				}
			} else {
				mark.Reason = "Inspection response was not a valid chat completion"
			}
		} else if statusCode == http.StatusTooManyRequests {
			mark.Result = "rate_limited"
		} else if statusCode == http.StatusUnauthorized {
			mark.Result = "login_required"
		} else if statusCode == http.StatusForbidden && result.context != nil && result.context.GetBool("provider_pool_rejected") {
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
			state.LastError = mark.Reason
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

func inspectionCallFailureReason(result testResult, channel *model.Channel) string {
	var reason string
	if result.newAPIError != nil {
		reason = result.newAPIError.MaskSensitiveErrorWithStatusCode(channel.GetBaseURL())
	} else if result.localErr != nil {
		reason = types.NewError(result.localErr, "").MaskSensitiveErrorWithStatusCode(channel.GetBaseURL())
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	const maxReasonRunes = 512
	runes := []rune(reason)
	if len(runes) > maxReasonRunes {
		reason = string(runes[:maxReasonRunes]) + "…"
	}
	return reason
}

func normalizeInspectionAnswer(raw string) string {
	const trimCutset = ".,!?;:。，！？；：、\"'“”‘’「」『』（）()[]【】`*"
	trimmed := strings.TrimSpace(raw)
	normalized := strings.TrimFunc(trimmed, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(trimCutset, r)
	})
	if normalized == "" {
		return trimmed
	}
	return normalized
}

func inspectionAnswerMatches(actual, expected string) bool {
	expectedTrimmed := strings.TrimSpace(expected)
	actualTrimmed := strings.TrimSpace(actual)
	if actualTrimmed == expectedTrimmed {
		return true
	}
	expectedNorm := normalizeInspectionAnswer(expectedTrimmed)
	actualNorm := normalizeInspectionAnswer(actualTrimmed)
	return expectedNorm != "" && actualNorm == expectedNorm
}

func inspectionMismatchReason(expected, actual string) string {
	expectedText := strings.Join(strings.Fields(strings.TrimSpace(expected)), " ")
	actualText := strings.Join(strings.Fields(strings.TrimSpace(actual)), " ")
	if actualText == "" {
		actualText = "(empty)"
	}
	const maxActualRunes = 256
	runes := []rune(actualText)
	if len(runes) > maxActualRunes {
		actualText = string(runes[:maxActualRunes]) + "…"
	}
	return fmt.Sprintf("expected %q, got %q", expectedText, actualText)
}
