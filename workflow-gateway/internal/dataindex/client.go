package dataindex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/kubesmarts/logic-operator/workflow-gateway/internal/platform"
)

var ErrEndpointNotDiscovered = errors.New("data index endpoint not yet discovered")
var ErrInstanceNotFound = errors.New("workflow instance not found")

type WorkflowInstance struct {
	ID        string     `json:"id"`
	Namespace string     `json:"namespace"`
	Name      string     `json:"name"`
	Version   string     `json:"version"`
	Status    string     `json:"status"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

type InstanceFilter struct {
	Namespace string
	Name      string
	Version   string
	Status    string
}

type Client struct {
	http  *http.Client
	store *platform.PlatformStore
}

func NewClient(store *platform.PlatformStore) *Client {
	return &Client{
		http:  &http.Client{Timeout: 10 * time.Second},
		store: store,
	}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) post(ctx context.Context, endpoint string, req gqlRequest) (*gqlResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("data index unreachable: %w", err)
	}
	defer resp.Body.Close()

	var gqlResp gqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("invalid data index response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("data index error: %s", gqlResp.Errors[0].Message)
	}
	return &gqlResp, nil
}

func (c *Client) resolveEndpoint() (string, error) {
	url, ok := c.store.Endpoint()
	if !ok {
		return "", ErrEndpointNotDiscovered
	}
	return url, nil
}

func buildFilter(f InstanceFilter) map[string]any {
	filter := map[string]any{}
	if f.Namespace != "" {
		filter["namespace"] = map[string]string{"eq": f.Namespace}
	}
	if f.Name != "" {
		filter["name"] = map[string]string{"eq": f.Name}
	}
	if f.Version != "" {
		filter["version"] = map[string]string{"eq": f.Version}
	}
	if f.Status != "" {
		// WorkflowInstanceStatusFilterInput uses eq with an enum value
		filter["status"] = map[string]string{"eq": f.Status}
	}
	return filter
}

const listQuery = `query ListInstances($filter: WorkflowInstanceFilterInput) {
  getWorkflowInstances(filter: $filter) {
    id namespace name version status startedAt endedAt
  }
}`

const getQuery = `query GetInstance($id: String!) {
  getWorkflowInstance(id: $id) {
    id namespace name version status startedAt endedAt
  }
}`

func (c *Client) ListInstances(ctx context.Context, filter InstanceFilter) ([]WorkflowInstance, error) {
	ep, err := c.resolveEndpoint()
	if err != nil {
		return nil, err
	}
	resp, err := c.post(ctx, ep, gqlRequest{
		Query:     listQuery,
		Variables: map[string]any{"filter": buildFilter(filter)},
	})
	if err != nil {
		return nil, err
	}
	var data struct {
		GetWorkflowInstances []WorkflowInstance `json:"getWorkflowInstances"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse instance list: %w", err)
	}
	return data.GetWorkflowInstances, nil
}

func (c *Client) GetInstance(ctx context.Context, id string) (*WorkflowInstance, error) {
	ep, err := c.resolveEndpoint()
	if err != nil {
		return nil, err
	}
	resp, err := c.post(ctx, ep, gqlRequest{
		Query:     getQuery,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return nil, err
	}
	var data struct {
		GetWorkflowInstance *WorkflowInstance `json:"getWorkflowInstance"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse instance: %w", err)
	}
	if data.GetWorkflowInstance == nil {
		return nil, ErrInstanceNotFound
	}
	return data.GetWorkflowInstance, nil
}
