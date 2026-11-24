package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// CPASharedPoolID is scoped to the configured CPA service, including its relay aliases.
const CPASharedPoolID = "00000000000000000000000000000000"

type CPACheck struct {
	Time        int64  `json:"time"`
	Question    string `json:"question"`
	Result      string `json:"result"`
	Consecutive int    `json:"consecutive"`
	Model       string `json:"model"`
}

// Decode only the fields needed by the console. CPA's full response can contain
// provider metadata, private paths and OAuth identity claims.
type CPAAccount struct {
	PoolManagedLimit bool     `json:"pool_managed_limit,omitempty"`
	ChannelPoolID    string   `json:"channel_pool_id,omitempty"`
	ID               string   `json:"id"`
	Provider         string   `json:"provider"`
	Label            string   `json:"label"`
	Disabled         bool     `json:"disabled"`
	RequiresLogin    bool     `json:"requires_sign_in"`
	Unavailable      bool     `json:"unavailable"`
	QuotaBlocked     bool     `json:"quota_blocked"`
	Status           string   `json:"status"`
	Limit            *int     `json:"account_concurrency"`
	Pool             string   `json:"account_pool"`
	Check            CPACheck `json:"account_check"`
}

// Only exact relay endpoints share a pool. An unrelated gateway on the same host
// can serve an entirely different credential set under another path.
func CPAChannelEndpoint(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443") {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	path := strings.TrimSuffix(strings.TrimRight(u.EscapedPath(), "/"), "/v1")
	return u.Scheme + "://" + u.Host + path
}

var cpaManagementHTTP = &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func CPARelayEndpoints() []string {
	base := CPAChannelEndpoint(os.Getenv("CPA_MANAGEMENT_URL"))
	if base == "" {
		return []string{}
	}
	urls := []string{base}
	for _, alias := range strings.Split(os.Getenv("CPA_RELAY_URLS"), ",") {
		if key := CPAChannelEndpoint(alias); key != "" {
			urls = append(urls, key)
		}
	}
	return urls
}

func CallCPAManagement(ctx context.Context, method, path string, input, output any) error {
	base := strings.TrimRight(os.Getenv("CPA_MANAGEMENT_URL"), "/")
	key := os.Getenv("CPA_MANAGEMENT_TOKEN")
	if base == "" || key == "" {
		return errors.New("CPA management is not configured")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Invalid CPA management URL")
	}
	body, err := common.Marshal(input)
	if err != nil {
		return err
	}
	var reader io.Reader
	if input != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/v8/management/"+path, reader)
	if err != nil {
		return errors.New("Invalid CPA request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := cpaManagementHTTP.Do(req)
	if err != nil {
		return errors.New("CPA management is unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("CPA management returned HTTP %d", res.StatusCode)
	}
	if output == nil {
		return nil
	}
	if err = common.DecodeJson(io.LimitReader(res.Body, 16<<20), output); err != nil {
		return errors.New("Invalid CPA management response")
	}
	return nil
}

func ListCPAAccounts(ctx context.Context) ([]CPAAccount, error) {
	var response struct {
		Files []struct {
			CPAAccount
			Claim struct {
				Plan string `json:"plan_type"`
			} `json:"id_token"`
		} `json:"files"`
	}
	if err := CallCPAManagement(ctx, "GET", "credentials", nil, &response); err != nil {
		return nil, err
	}
	out := make([]CPAAccount, 0, len(response.Files))
	for _, row := range response.Files {
		if row.Provider != "codex" {
			continue
		}
		a := row.CPAAccount
		a.ChannelPoolID = CPASharedPoolID
		if a.Pool == "" {
			switch strings.ToLower(row.Claim.Plan) {
			case "plus", "pro":
				a.Pool = strings.ToLower(row.Claim.Plan)
			default:
				a.Pool = "unassigned"
			}
		}
		out = append(out, a)
	}
	return out, nil
}

func StartCPAAccountChecks() {
	interval, err := time.ParseDuration(os.Getenv("CPA_CHECK_INTERVAL"))
	model := strings.TrimSpace(os.Getenv("CPA_CHECK_MODEL"))
	if err != nil || interval < time.Minute || model == "" || os.Getenv("CPA_MANAGEMENT_TOKEN") == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			accounts, err := ListCPAAccounts(context.Background())
			if err != nil {
				common.SysError(err.Error())
				continue
			}
			// Channels configured in the console own the schedule for their pool.
			// Legacy deployments continue polling their other pools.
			managed := map[string]bool{}
			common.OptionMapRWMutex.RLock()
			for key := range common.OptionMap {
				if strings.HasPrefix(key, "CPAInspection/") && strings.HasSuffix(key, "/settings") {
					parts := strings.Split(key, "/")
					if len(parts) == 4 {
						managed[parts[2]] = true
					}
				}
			}
			common.OptionMapRWMutex.RUnlock()
			for _, a := range accounts {
				if a.Disabled || a.Limit == nil || managed[a.ChannelPoolID] {
					continue
				}
				if err := CallCPAManagement(context.Background(), "POST", "credentials/quality-check", map[string]string{"name": a.ID, "model": model}, nil); err != nil {
					common.SysError(err.Error())
				}
			}
		}
	}()
}
