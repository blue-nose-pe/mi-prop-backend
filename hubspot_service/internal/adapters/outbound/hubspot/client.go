// Package hubspotclient implementa ports.HubspotClient contra el CRM v3
// de HubSpot vía HTTP REST.
//
// Rate limiting: el cliente delega TODAS las requests HTTP al
// requestsengine.Engine. El engine coordina entre N réplicas usando
// Redis (un Lua atómico por slot por API key). Sin esto, cuando hubspot-
// service tiene >1 réplica, todas atacan la misma key y reciben 429.
//
// Endpoints cubiertos:
//   POST   /crm/v3/objects/contacts                                     (create)
//   PATCH  /crm/v3/objects/contacts/{id}                                (update)
//   POST   /crm/v3/objects/contacts/search                              (find by prop)
//   POST   /crm/v3/objects/{typeId}                                     (custom create)
//   PATCH  /crm/v3/objects/{typeId}/{id}                                (custom update)
//   POST   /crm/v3/objects/{typeId}/search                              (custom find)
//
// El token Bearer lo agrega el engine (rotando entre los tokens del pool).
package hubspotclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
	"hubspot_service/internal/shared/requestsengine"
)

const baseURL = "https://api.hubapi.com"

type Client struct {
	engine *requestsengine.Engine
}

var _ ports.HubspotClient = (*Client)(nil)

// New construye el cliente sobre un engine ya inicializado.
// El engine es responsabilidad del caller — típicamente main.go lo
// crea una vez y lo cierra al shutdown.
func New(engine *requestsengine.Engine) *Client {
	return &Client{engine: engine}
}

// ---------- Contacts ----------

func (c *Client) UpsertContactByDNI(ctx context.Context, props map[string]string, dni string) (domain.RecordID, error) {
	id, err := c.FindContactByDNI(ctx, dni)
	if err != nil {
		return "", err
	}
	if id != "" {
		if err := c.UpdateContact(ctx, id, props); err != nil {
			return "", err
		}
		return id, nil
	}
	return c.createContact(ctx, props)
}

func (c *Client) createContact(ctx context.Context, props map[string]string) (domain.RecordID, error) {
	var resp objectResponse
	if err := c.do(ctx, http.MethodPost, "/crm/v3/objects/contacts",
		objectRequest{Properties: props}, &resp); err != nil {
		return "", err
	}
	return domain.RecordID(resp.ID), nil
}

func (c *Client) UpdateContact(ctx context.Context, recordID domain.RecordID, props map[string]string) error {
	path := "/crm/v3/objects/contacts/" + string(recordID)
	return c.do(ctx, http.MethodPatch, path, objectRequest{Properties: props}, nil)
}

func (c *Client) FindContactByDNI(ctx context.Context, dni string) (domain.RecordID, error) {
	return c.searchByProperty(ctx, "/crm/v3/objects/contacts/search", "dni", dni)
}

func (c *Client) FindContactByEmail(ctx context.Context, email string) (domain.RecordID, error) {
	return c.searchByProperty(ctx, "/crm/v3/objects/contacts/search", "email", email)
}

// ---------- Custom objects ----------

func (c *Client) UpsertCustomObjectByProp(
	ctx context.Context,
	typeID, keyProp, keyValue string,
	props map[string]string,
) (domain.RecordID, error) {
	id, err := c.searchByProperty(ctx, "/crm/v3/objects/"+typeID+"/search", keyProp, keyValue)
	if err != nil {
		return "", err
	}
	if id != "" {
		if err := c.do(ctx, http.MethodPatch, "/crm/v3/objects/"+typeID+"/"+string(id),
			objectRequest{Properties: props}, nil); err != nil {
			return "", err
		}
		return id, nil
	}
	var resp objectResponse
	if err := c.do(ctx, http.MethodPost, "/crm/v3/objects/"+typeID,
		objectRequest{Properties: props}, &resp); err != nil {
		return "", err
	}
	return domain.RecordID(resp.ID), nil
}

// ---------- helpers ----------

// searchByProperty: POST /search con filterGroups EQ.
//
// Defensive matching: HubSpot devuelve resultados ruidosos cuando la
// propiedad EQ no existe en el portal (en vez de 400). Validamos
// explicitamente que el primer resultado traiga la prop con el valor
// pedido, sino devolvemos "no encontrado".
func (c *Client) searchByProperty(ctx context.Context, path, prop, value string) (domain.RecordID, error) {
	body := searchRequest{
		FilterGroups: []filterGroup{{
			Filters: []filter{{PropertyName: prop, Operator: "EQ", Value: value}},
		}},
		Properties: []string{prop},
		Limit:      1,
	}
	var resp searchResponse
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return "", err
	}
	if len(resp.Results) == 0 {
		return "", nil
	}
	got, ok := resp.Results[0].Properties[prop]
	if !ok || got != value {
		return "", nil
	}
	return domain.RecordID(resp.Results[0].ID), nil
}

// do delega al engine — el engine maneja rate limit (5 keys × 10 rps =
// 50 rps globales coordinados via Redis), reintentos con backoff
// exponencial y respeto del header Retry-After de HubSpot.
//
// El error pattern se mantiene compatible con el código previo:
// status >= 400 → ErrHubspotUpstream con detalle del status + body.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var raw []byte
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		raw = buf
	}

	status, respBody, _, err := c.engine.Do(
		ctx,
		method+" "+path, // ID del task — útil para logs/audit
		method,
		baseURL+path,
		raw,
		nil, // engine ya setea Authorization, Content-Type y Accept
	)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrHubspotUpstream, err)
	}

	if status >= 400 {
		return fmt.Errorf("%w: %s %s -> %d %s",
			domain.ErrHubspotUpstream, method, path, status, strings.TrimSpace(string(respBody)))
	}
	if out == nil || status == http.StatusNoContent || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

// ---------- DTOs internos ----------

type objectRequest struct {
	Properties map[string]string `json:"properties"`
}

type objectResponse struct {
	ID string `json:"id"`
}

type searchRequest struct {
	FilterGroups []filterGroup `json:"filterGroups"`
	Properties   []string      `json:"properties"`
	Limit        int           `json:"limit"`
}

type filterGroup struct {
	Filters []filter `json:"filters"`
}

type filter struct {
	PropertyName string `json:"propertyName"`
	Operator     string `json:"operator"`
	Value        string `json:"value"`
}

type searchResponse struct {
	Results []searchResultItem `json:"results"`
}

type searchResultItem struct {
	ID         string            `json:"id"`
	Properties map[string]string `json:"properties"`
}
