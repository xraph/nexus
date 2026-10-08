package contract

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
)

type keyRow struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenantId"`
	TenantName string            `json:"tenantName"`
	Name       string            `json:"name"`
	Prefix     string            `json:"prefix"`
	Scopes     []string          `json:"scopes"`
	Status     key.Status        `json:"status"`
	ExpiresAt  *string           `json:"expiresAt"`
	LastUsedAt *string           `json:"lastUsedAt"`
	CreatedAt  *string           `json:"createdAt"`
	Metadata   map[string]string `json:"metadata"`
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}
func projectKey(k *key.APIKey, tenantName string) keyRow {
	return keyRow{ID: k.ID.String(), TenantID: k.TenantID.String(), TenantName: tenantName, Name: k.Name, Prefix: k.Prefix, Scopes: nonNil(k.Scopes), Status: k.Status, ExpiresAt: optionalTime(k.ExpiresAt), LastUsedAt: optionalTime(k.LastUsedAt), CreatedAt: formatTime(k.CreatedAt), Metadata: k.Metadata}
}
func getKey(ctx context.Context, gw *nexus.Gateway, kid string) (*key.APIKey, error) {
	if _, err := id.ParseKeyID(kid); err != nil {
		return nil, badRequest("id must be a key ID")
	}
	return gw.Keys().Get(ctx, kid)
}
func keyWithTenant(ctx context.Context, gw *nexus.Gateway, k *key.APIKey) (keyRow, error) {
	t, err := gw.Tenants().Get(ctx, k.TenantID.String())
	if err != nil {
		return keyRow{}, err
	}
	return projectKey(k, t.Name), nil
}

type keysListRequest struct {
	TenantID json.RawMessage `json:"tenantId"`
	Status   key.Status      `json:"status"`
	Cursor   string          `json:"cursor"`
	Limit    int             `json:"limit"`
}
type keysListResponse struct {
	Items      []keyRow `json:"items"`
	NextCursor string   `json:"nextCursor"`
}

func keysList(ctx context.Context, gw *nexus.Gateway, in keysListRequest) (keysListResponse, error) {
	tid, err := tenantScope(ctx, gw, in.TenantID)
	if err != nil {
		return keysListResponse{}, err
	}
	switch in.Status {
	case "", key.KeyActive, key.KeyExpired, key.KeyRevoked:
	default:
		return keysListResponse{}, badRequest("invalid key status")
	}
	page, err := gw.Keys().ListPage(ctx, &key.ListOptions{TenantID: tid, Status: in.Status, Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		return keysListResponse{}, err
	}
	out := keysListResponse{Items: []keyRow{}, NextCursor: page.NextCursor}
	names := map[string]string{}
	for _, k := range page.Items {
		name, ok := names[k.TenantID.String()]
		if !ok {
			t, findErr := gw.Tenants().Get(ctx, k.TenantID.String())
			if findErr != nil {
				return keysListResponse{}, findErr
			}
			name = t.Name
			names[k.TenantID.String()] = name
		}
		out.Items = append(out.Items, projectKey(k, name))
	}
	return out, nil
}
func keysGet(ctx context.Context, gw *nexus.Gateway, in idRequest) (keyRow, error) {
	k, err := getKey(ctx, gw, in.ID)
	if err != nil {
		return keyRow{}, err
	}
	return keyWithTenant(ctx, gw, k)
}

type keyCreateRequest struct {
	TenantID  string          `json:"tenantId"`
	Name      string          `json:"name"`
	Scopes    json.RawMessage `json:"scopes"`
	ExpiresAt *string         `json:"expiresAt"`
}
type secretKeyResponse struct {
	Key          keyRow `json:"key"`
	RawKey       string `json:"rawKey"`
	RevokedKeyID string `json:"revokedKeyId,omitempty"`
}

func keysCreate(ctx context.Context, gw *nexus.Gateway, in keyCreateRequest) (secretKeyResponse, error) {
	t, err := getTenant(ctx, gw, in.TenantID)
	if err != nil {
		return secretKeyResponse{}, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return secretKeyResponse{}, badRequest("name is required")
	}
	input := &key.CreateInput{TenantID: t.ID.String(), Name: name}
	if len(in.Scopes) > 0 {
		if json.Unmarshal(in.Scopes, &input.Scopes) != nil || len(input.Scopes) == 0 {
			return secretKeyResponse{}, badRequest("scopes must be a non-empty array")
		}
		for _, sc := range input.Scopes {
			if !key.KnownScope(sc) {
				return secretKeyResponse{}, badRequest("unknown key scope")
			}
		}
	}
	if in.ExpiresAt != nil {
		expires, parseErr := parseBound(in.ExpiresAt)
		if parseErr != nil {
			return secretKeyResponse{}, parseErr
		}
		input.ExpiresAt = &expires
	}
	k, raw, err := gw.Keys().Create(ctx, input)
	if err != nil {
		return secretKeyResponse{}, err
	}
	return secretKeyResponse{Key: projectKey(k, t.Name), RawKey: raw}, nil
}
func keysRotate(ctx context.Context, gw *nexus.Gateway, in idRequest) (secretKeyResponse, error) {
	old, err := getKey(ctx, gw, in.ID)
	if err != nil {
		return secretKeyResponse{}, err
	}
	t, err := gw.Tenants().Get(ctx, old.TenantID.String())
	if err != nil {
		return secretKeyResponse{}, err
	}
	k, raw, err := gw.Keys().Rotate(ctx, in.ID)
	if err != nil {
		return secretKeyResponse{}, err
	}
	return secretKeyResponse{Key: projectKey(k, t.Name), RawKey: raw, RevokedKeyID: in.ID}, nil
}
func keysRevoke(ctx context.Context, gw *nexus.Gateway, in idRequest) (keyRow, error) {
	k, err := getKey(ctx, gw, in.ID)
	if err != nil {
		return keyRow{}, err
	}
	out, err := keyWithTenant(ctx, gw, k)
	if err != nil {
		return keyRow{}, err
	}
	if err := gw.Keys().Revoke(ctx, in.ID); err != nil {
		return keyRow{}, err
	}
	out.Status = key.KeyRevoked
	return out, nil
}
