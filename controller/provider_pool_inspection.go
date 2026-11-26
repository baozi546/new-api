package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
)

var inspectingAccount sync.Map

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
		evaluated, updateErr := evaluateProviderPoolAccountInspection(context.Background(), channel, account, testUserID, cfg, cfg.Model, question, index+1, false)
		if updateErr != nil {
			round.Failed = true
		}
		round.Accounts = append(round.Accounts, evaluated)
	}
	round.Time = time.Now().Unix()
	raw, err := common.Marshal(round)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{inspectionKey(&channel, "last"): string(raw)})
}

func resolveSingleAccountInspectionModel(channel *model.Channel, cfg inspectionConfig) string {
	if modelName := strings.TrimSpace(cfg.Model); modelName != "" {
		return modelName
	}
	if channel.TestModel != nil {
		if modelName := strings.TrimSpace(*channel.TestModel); modelName != "" {
			return modelName
		}
	}
	for _, m := range channel.GetModels() {
		if modelName := strings.TrimSpace(m); modelName != "" {
			return modelName
		}
	}
	return ""
}

// inspectSingleProviderPoolAccount runs an explicit manual check on one account
// even if that account is currently disabled, degraded, or previously flagged,
// so administrators can verify whether upstream risk controls have been lifted.
func inspectSingleProviderPoolAccount(ctx context.Context, channel model.Channel, account service.ProviderPoolAccount) (accountMark, string, error) {
	lockKey := fmt.Sprintf("%d:%s", channel.Id, account.AccountID)
	if _, busy := inspectingAccount.LoadOrStore(lockKey, true); busy {
		return accountMark{}, "", errors.New("Account inspection already running")
	}
	defer inspectingAccount.Delete(lockKey)

	cfg := inspectionSettings(&channel)
	modelName := resolveSingleAccountInspectionModel(&channel, cfg)
	if modelName == "" {
		return accountMark{}, "", errors.New("Model is required")
	}
	if len(cfg.Questions) == 0 {
		return accountMark{}, "", errors.New("invalid inspection settings")
	}
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return accountMark{}, "", err
	}
	var previous inspectionRound
	readInspection(&channel, "last", &previous)
	index := 0
	if previous.Sequence > 0 {
		index = (previous.Sequence - 1) % len(cfg.Questions)
	}
	question := cfg.Questions[index]
	mark, updateErr := evaluateProviderPoolAccountInspection(ctx, channel, account, testUserID, cfg, modelName, question, index+1, true)
	if updateErr != nil {
		return mark, modelName, updateErr
	}
	_ = mergeSingleAccountInspectionRound(&channel, mark, question.Prompt, modelName)
	return mark, modelName, nil
}

func evaluateProviderPoolAccountInspection(
	parentCtx context.Context,
	channel model.Channel,
	account service.ProviderPoolAccount,
	testUserID int,
	cfg inspectionConfig,
	modelName string,
	question inspectionQuestion,
	questionNumber int,
	forceInspect bool,
) (accountMark, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	mark := accountMark{ID: account.AccountID, Label: account.Label, Time: time.Now().Unix(), Number: questionNumber, Result: "inconclusive"}
	ctx, cancel := context.WithTimeout(parentCtx, 3*time.Minute)
	result := testChannel(ctx, &channel, testUserID, modelName, string(constant.EndpointTypeOpenAI), false,
		accountInspectionInput{
			AccountID:    account.AccountID,
			Group:        account.Profile.Category,
			Prompt:       question.Prompt,
			Effort:       cfg.ReasoningEffort,
			ForceInspect: forceInspect,
		})
	cancel()
	mark.Time = time.Now().Unix()
	mark.Reason = inspectionCallFailureReason(result, &channel)
	statusCode := result.statusCode
	if statusCode == 0 && result.newAPIError != nil {
		statusCode = result.newAPIError.StatusCode
	}
	if result.localErr == nil && result.newAPIError == nil {
		if actual, ok := extractInspectionReplyText(result.response); ok {
			if inspectionAnswerMatches(actual, question.Answer) {
				mark.Result = "passed"
				mark.Reason = ""
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
	if result.localErr != nil || result.newAPIError != nil {
		current, findErr := service.FindProviderPoolAccount(context.Background(), &channel, account.AccountID)
		if findErr == nil && current.NeedsLogin {
			mark.Result = "login_required"
		}
	}
	threshold := cfg.DegradeAfter
	if threshold == 0 {
		threshold = 3
	}
	err := service.UpdateProviderAccount(account.OwnerID, account.AccountID, func(state *model.ProviderAccountState) {
		state.Health = mark.Result
		state.LastError = mark.Reason
		state.CheckedAt = mark.Time
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
	})
	return mark, err
}

func mergeSingleAccountInspectionRound(channel *model.Channel, mark accountMark, prompt, modelName string) error {
	var last inspectionRound
	readInspection(channel, "last", &last)
	replaced := false
	for i := range last.Accounts {
		if last.Accounts[i].ID == mark.ID {
			last.Accounts[i] = mark
			replaced = true
			break
		}
	}
	if !replaced {
		last.Accounts = append(last.Accounts, mark)
	}
	if last.Time == 0 {
		last.Time = mark.Time
	}
	if last.Prompt == "" {
		last.Prompt = prompt
	}
	if last.Model == "" {
		last.Model = modelName
	}
	raw, err := common.Marshal(last)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{inspectionKey(channel, "last"): string(raw)})
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

func extractInspectionReplyText(raw []byte) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if common.Unmarshal(raw, &payload) != nil {
		return "", false
	}
	if len(payload.Choices) > 0 {
		msgRaw := payload.Choices[0].Message.Content
		var text string
		if len(msgRaw) > 0 && common.Unmarshal(msgRaw, &text) == nil {
			return text, true
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if len(msgRaw) > 0 && common.Unmarshal(msgRaw, &parts) == nil {
			var b strings.Builder
			for _, part := range parts {
				if part.Text != "" {
					b.WriteString(part.Text)
				}
			}
			return b.String(), true
		}
	}
	if len(payload.Content) > 0 {
		var b strings.Builder
		for _, block := range payload.Content {
			if block.Text != "" {
				b.WriteString(block.Text)
			}
		}
		return b.String(), true
	}
	if len(payload.Candidates) > 0 {
		var b strings.Builder
		for _, part := range payload.Candidates[0].Content.Parts {
			if part.Text != "" {
				b.WriteString(part.Text)
			}
		}
		return b.String(), true
	}
	if payload.OutputText != "" {
		return payload.OutputText, true
	}
	if len(payload.Output) > 0 {
		var b strings.Builder
		for _, item := range payload.Output {
			for _, part := range item.Content {
				if part.Text != "" {
					b.WriteString(part.Text)
				}
			}
		}
		return b.String(), true
	}
	return "", false
}

// inspectStandardChannel runs one inspection round against any non-account-pool
// testable channel (e.g. OpenAI, Claude/Anthropic, Gemini, DeepSeek, New API,
// custom channels) using the same rotating question & answer matching logic.
func inspectStandardChannel(channel model.Channel, cfg inspectionConfig) error {
	if !cfg.valid() || cfg.Model == "" {
		return errors.New("invalid inspection settings")
	}
	var previous inspectionRound
	readInspection(&channel, "last", &previous)
	index := previous.Sequence % len(cfg.Questions)
	question := cfg.Questions[index]
	round := inspectionRound{
		Sequence: previous.Sequence + 1,
		Prompt:   question.Prompt,
		Model:    cfg.Model,
		Accounts: []accountMark{},
	}
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return err
	}
	mark := accountMark{
		ID:     fmt.Sprintf("channel-%d", channel.Id),
		Label:  channel.Name,
		Time:   time.Now().Unix(),
		Number: index + 1,
		Result: "inconclusive",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	result := testChannel(ctx, &channel, testUserID, cfg.Model, string(constant.EndpointTypeOpenAI), false,
		accountInspectionInput{
			Prompt: question.Prompt,
			Effort: cfg.ReasoningEffort,
		})
	cancel()
	mark.Time = time.Now().Unix()
	mark.Reason = inspectionCallFailureReason(result, &channel)
	statusCode := result.statusCode
	if statusCode == 0 && result.newAPIError != nil {
		statusCode = result.newAPIError.StatusCode
	}
	if result.localErr == nil && result.newAPIError == nil {
		if actual, ok := extractInspectionReplyText(result.response); ok {
			if inspectionAnswerMatches(actual, question.Answer) {
				mark.Result = "passed"
				mark.Reason = ""
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
	}
	round.Accounts = append(round.Accounts, mark)
	round.Time = time.Now().Unix()
	raw, err := common.Marshal(round)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{inspectionKey(&channel, "last"): string(raw)})
}
