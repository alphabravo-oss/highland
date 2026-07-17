package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type LonghornClient interface {
	Get(context.Context, string, string) (map[string]any, error)
	List(context.Context, string) ([]map[string]any, error)
	Action(context.Context, string, string, string, map[string]any) (map[string]any, error)
	Create(context.Context, string, map[string]any) (map[string]any, error)
	Update(context.Context, string, string, map[string]any) (map[string]any, error)
}

type longhornManagerClient struct {
	base   string
	client *http.Client
}

type LonghornHTTPError struct {
	Status  int
	Message string
}

func (e *LonghornHTTPError) Error() string {
	return fmt.Sprintf("Longhorn manager returned %d: %s", e.Status, e.Message)
}

func NewLonghornManagerClient(managerURL string) LonghornClient {
	return &longhornManagerClient{
		base: strings.TrimRight(managerURL, "/") + "/v1",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *longhornManagerClient) Get(ctx context.Context, collection, name string) (map[string]any, error) {
	result := map[string]any{}
	err := c.request(ctx, http.MethodGet, "/"+url.PathEscape(collection)+"/"+url.PathEscape(name), nil, &result)
	return result, err
}

func (c *longhornManagerClient) List(ctx context.Context, collection string) ([]map[string]any, error) {
	var result struct {
		Data []map[string]any `json:"data"`
	}
	err := c.request(ctx, http.MethodGet, "/"+url.PathEscape(collection), nil, &result)
	if result.Data == nil {
		result.Data = []map[string]any{}
	}
	return result.Data, err
}

func (c *longhornManagerClient) Action(ctx context.Context, collection, name, action string, parameters map[string]any) (map[string]any, error) {
	result := map[string]any{}
	path := "/" + url.PathEscape(collection) + "/" + url.PathEscape(name) + "?action=" + url.QueryEscape(action)
	err := c.request(ctx, http.MethodPost, path, parameters, &result)
	return result, err
}

func (c *longhornManagerClient) Create(ctx context.Context, collection string, body map[string]any) (map[string]any, error) {
	result := map[string]any{}
	err := c.request(ctx, http.MethodPost, "/"+url.PathEscape(collection), body, &result)
	return result, err
}

func (c *longhornManagerClient) Update(ctx context.Context, collection, name string, body map[string]any) (map[string]any, error) {
	result := map[string]any{}
	err := c.request(ctx, http.MethodPut, "/"+url.PathEscape(collection)+"/"+url.PathEscape(name), body, &result)
	return result, err
}

func (c *longhornManagerClient) request(ctx context.Context, method, path string, body map[string]any, target any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("Longhorn manager request failed: %w", err)
	}
	defer response.Body.Close()
	encoded, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if readErr != nil {
		return readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(encoded))
		if len(message) > 512 {
			message = message[:512]
		}
		return &LonghornHTTPError{Status: response.StatusCode, Message: message}
	}
	if len(encoded) == 0 || target == nil {
		return nil
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("decode Longhorn manager response: %w", err)
	}
	return nil
}

func longhornActions(resource map[string]any) map[string]any {
	actions, _ := resource["actions"].(map[string]any)
	return actions
}

func longhornHasAction(resource map[string]any, action string) bool {
	_, ok := longhornActions(resource)[action]
	return ok
}

func longhornString(resource map[string]any, key string) string {
	value, _ := resource[key].(string)
	return value
}

func longhornInt(resource map[string]any, key string) int {
	switch value := resource[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	}
	return 0
}
