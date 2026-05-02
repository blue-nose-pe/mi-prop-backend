// Package hubspotclient implementa ports.HubspotClient contra el CRM v3
// de HubSpot vía HTTP REST. No usamos un SDK de terceros — el subset de
// operaciones que necesitamos es chico y un cliente propio nos da control
// fino sobre timeouts, retries y formato de errores.
//
// Endpoints cubiertos:
//   POST   /crm/v3/objects/contacts                                     (create)
//   PATCH  /crm/v3/objects/contacts/{id}                                (update)
//   POST   /crm/v3/objects/contacts/search                              (find by prop)
//   POST   /crm/v3/objects/{typeId}                                     (custom create)
//   PATCH  /crm/v3/objects/{typeId}/{id}                                (custom update)
//   POST   /crm/v3/objects/{typeId}/search                              (custom find)
//
// Auth: Authorization: Bearer <HUBSPOT_API_TOKEN>.
package hubspotclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"hubspot_service/internal/core/domain"
	"hubspot_service/internal/core/ports"
)

const (
	baseURL    = "https://api.hubapi.com"
	defaultTO  = 30 * time.Second
)

type Client struct {
	token string
	http  *http.Client
}

var _ ports.HubspotClient = (*Client)(nil)

func New(token string) *Client {
	return &Client{
		token: token,
		http: &http.Client{Timeout: defaultTO},
	}
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
	return domain.RecordID(resp.Results[0].ID), nil
}

// do ejecuta el request y deserializa la respuesta. Maneja status >= 400
// como ErrHubspotUpstream con cuerpo en el error message para diagnóstico.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrHubspotUpstream, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: %s %s -> %d %s",
			domain.ErrHubspotUpstream, method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
	Results []objectResponse `json:"results"`
}
