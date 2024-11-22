package copilot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	clientID        = "Iv1.b507a08c87ecfe98"
	deviceCodeURL   = "https://github.com/login/device/code"
	accessTokenURL  = "https://github.com/login/oauth/access_token"
	copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
	completionURL   = "https://copilot-proxy.githubusercontent.com/v1/engines/copilot-codex/completions"
	editorVersion   = "Neovim/0.6.1"
	pluginVersion   = "copilot.vim/1.16.0"
	userAgent       = "GithubCopilot/1.155.0"
	contentType     = "application/json"
	acceptEncoding  = "gzip,deflate,br"
)

var (
	log = logrus.StandardLogger()
)

type shortLivedToken struct {
	Token string
}

func (t shortLivedToken) Expired() bool {
	if t.Token == "" {
		return true
	}
	qs, err := url.ParseQuery(t.Token)
	if err != nil {
		return true
	}
	exp := qs.Get("exp")
	if exp == "" {
		log.Warnf("no exp found in short lived token")
		return true
	}
	parsedInt, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		log.Errorf("parse token expiration time: %v", err)
		return true
	}
	expTime := time.Unix(parsedInt, 0)
	return time.Now().After(expTime)
}

type longLivedToken struct {
	Token string
}

type Copilot struct {
	SlToken shortLivedToken
	LlToken longLivedToken
}

func New() *Copilot {
	return &Copilot{}
}

func (c *Copilot) GetDeviceCodeCmd() (*DeviceCodeResponse, error) {
	payload := strings.NewReader(fmt.Sprintf(`{"client_id":"%s","scope":"read:user"}`, clientID))
	req, err := http.NewRequest("POST", deviceCodeURL, payload)
	if err != nil {
		return nil, fmt.Errorf("new request failed: %w", err)
	}

	req.Header = http.Header{
		"Accept":                {contentType},
		"Editor-Version":        {editorVersion},
		"Editor-Plugin-Version": {pluginVersion},
		"Content-Type":          {contentType},
		"User-Agent":            {userAgent},
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("client do request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body failed: %w", err)
	}

	var deviceResp DeviceCodeResponse
	if err := json.Unmarshal(bodyBytes, &deviceResp); err != nil {
		return nil, fmt.Errorf("unmarshal device code response failed: %w", err)
	}

	return &deviceResp, nil
}

func (c *Copilot) CheckAuthStatusCmd(deviceCode string) (bool, error) {
	payload := fmt.Sprintf(`{"client_id":"%s","device_code":"%s","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`, clientID, deviceCode)
	req, err := http.NewRequest("POST", accessTokenURL, strings.NewReader(payload))
	if err != nil {
		return true, fmt.Errorf("new request failed: %w", err)
	}

	req.Header = http.Header{
		"Editor-Version":        {editorVersion},
		"Editor-Plugin-Version": {pluginVersion},
		"Content-Type":          {contentType},
		"User-Agent":            {userAgent},
		"Accept-Encoding":       {acceptEncoding},
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return true, fmt.Errorf("client do request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return true, fmt.Errorf("checkAuth request failed: %s", resp.Status)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return true, fmt.Errorf("read checkAuth response body failed: %w", err)
	}

	ct := strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]
	switch ct {
	case "application/x-www-form-urlencoded":
		q, err := url.ParseQuery(string(bodyBytes))
		if err != nil {
			return true, fmt.Errorf("parse checkAuth response failed: %w", err)
		}
		urlErr := q.Get("error")
		if urlErr == "authorization_pending" {
			time.Sleep(5 * time.Second)
			return false, nil
		}
		if urlErr != "" {
			return true, fmt.Errorf("checkAuth response error: %s", urlErr)
		}

		token := q.Get("access_token")
		if token != "" {
			c.LlToken = longLivedToken{Token: token}
			return true, nil
		}
		return true, fmt.Errorf("checkAuth response error: %s", q.Get("error"))
	default:
		return true, fmt.Errorf("checkAuth response unsupported content-type: %s", ct)
	}
}

func (c *Copilot) GetCopilotCompletion(prompt string, stop string, n, maxTokens int, topP, temperature float32) (<-chan CompletionResponse, error) {
	if c.SlToken.Token == "" {
		return nil, fmt.Errorf("token is empty")
	}
	payload := map[string]interface{}{
		"prompt":      prompt,
		"suffix":      "",
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"top_p":       topP,
		"n":           n,
		"stop":        []string{stop},
		"nwo":         "github/copilot.vim",
		"stream":      true,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", completionURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.SlToken.Token))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get completion failed: %s", resp.Status)
	}

	ch := make(chan CompletionResponse)

	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			log.Debug(line)
			if strings.HasPrefix(line, "data: {") {
				var completion CompletionResponse
				err := json.Unmarshal([]byte(line[6:]), &completion)
				if err != nil {
					continue
				}
				ch <- completion
			}
		}
		close(ch)
	}()
	return ch, nil
}

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationUri string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type CompletionChoice struct {
	Text string `json:"text"`
}

type CompletionResponse struct {
	Choices []CompletionChoice `json:"choices"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

func (c *Copilot) RefreshToken() error {
	if c.LlToken.Token == "" {
		return fmt.Errorf("long lived token is empty")
	}

	req, err := http.NewRequest("GET", copilotTokenURL, nil)
	if err != nil {
		return fmt.Errorf("new request failed: %w", err)
	}

	req.Header = http.Header{
		"Authorization":         {fmt.Sprintf("token %s", c.LlToken.Token)},
		"Editor-Version":        {editorVersion},
		"Editor-Plugin-Version": {pluginVersion},
		"User-Agent":            {userAgent},
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("client do request failed: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("decode getcode response failed: %w", err)
	}

	c.SlToken.Token = tokenResp.Token
	return nil
}
