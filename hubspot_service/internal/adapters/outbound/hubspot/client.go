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

// UpsertContactByEmail busca un contacto por email; si existe lo actualiza
// con los props recibidos, si no lo crea. Mismo patrón que UpsertContactByDNI
// pero indexando por email — pensado para flujos donde no se tiene DNI
// (registro publico de estudiante con key) y solo se cuenta con el correo.
//
// Maneja la race condition con otros writers (typicamente users_service
// hace un UpsertContactByDNI paralelo en su syncToHubspotAsync): si el
// search devuelve vacio pero el create recibe 409 "Contact already
// exists. Existing ID: NNN", extraemos el ID y caemos a UpdateContact.
// Sin esto, el flujo del asesor-crea-estudiante perdia las props
// adicionales que SyncStudentContact intentaba escribir.
func (c *Client) UpsertContactByEmail(ctx context.Context, props map[string]string, email string) (domain.RecordID, error) {
	id, err := c.FindContactByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if id != "" {
		if err := c.UpdateContact(ctx, id, props); err != nil {
			return "", err
		}
		return id, nil
	}
	// HubSpot requiere email para crear contactos via /contacts; lo
	// inyectamos si el caller no lo paso en props (en SendOTP solo
	// pasa otp_estudiante).
	if _, ok := props["email"]; !ok {
		merged := make(map[string]string, len(props)+1)
		for k, v := range props {
			merged[k] = v
		}
		merged["email"] = email
		props = merged
	}
	rid, err := c.createContact(ctx, props)
	if err == nil {
		return rid, nil
	}
	// Race condition recovery: HubSpot puede devolver dos formatos cuando
	// otro caller acaba de crear el contacto entre nuestro search y create:
	//   409 CONFLICT: "Contact already exists. Existing ID: NNN"
	//   400 VALIDATION_ERROR: "... NNN already has that value." (cuando el
	//                          email choca contra un contact existente)
	// Probamos extraer el ID del mensaje; si no, hacemos un re-search por
	// email (el indexing de HubSpot suele propagar en <1s, y el contact
	// ya existe a estas alturas). Sin esto los props del student que
	// queriamos setear se perderian.
	delete(props, "email") // el update no debe pisar email — ya esta seteado
	if existing := extractExistingIDFromError(err.Error()); existing != "" {
		if uerr := c.UpdateContact(ctx, domain.RecordID(existing), props); uerr != nil {
			return "", fmt.Errorf("upsert race recovery update failed: %w (original: %v)", uerr, err)
		}
		return domain.RecordID(existing), nil
	}
	// Fallback: re-search por email (el indexing pudo haber propagado).
	if existing, ferr := c.FindContactByEmail(ctx, email); ferr == nil && existing != "" {
		if uerr := c.UpdateContact(ctx, existing, props); uerr != nil {
			return "", fmt.Errorf("upsert race re-search update failed: %w (original: %v)", uerr, err)
		}
		return existing, nil
	}
	return "", err
}

// extractExistingIDFromError parsea los dos formatos de error con los que
// HubSpot reporta "ese email ya pertenece a otro contacto":
//   - "Existing ID: NNN"            (409 CONFLICT en /contacts create)
//   - "NNN already has that value." (400 VALIDATION_ERROR en /contacts create)
// Si no encuentra ninguno devuelve "".
func extractExistingIDFromError(msg string) string {
	if marker := "Existing ID: "; strings.Contains(msg, marker) {
		i := strings.Index(msg, marker)
		rest := msg[i+len(marker):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end > 0 {
			return rest[:end]
		}
	}
	if marker := " already has that value"; strings.Contains(msg, marker) {
		// El ID viene justo antes del marker: "... 12345 already has..."
		i := strings.Index(msg, marker)
		// Walk back over digits.
		end := i
		start := end
		for start > 0 && msg[start-1] >= '0' && msg[start-1] <= '9' {
			start--
		}
		if end-start > 0 {
			return msg[start:end]
		}
	}
	return ""
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

// AssociateContactToCompany crea la asociacion default Contact -> Company
// en HubSpot. Equivalente a hubspotClient.crm.associations.v4.basicApi.
// createDefault('0-1', contactID, '0-2', companyID) en JS (P1). Idempotente
// del lado de HubSpot: si la asociacion ya existe, el PUT no hace nada y
// devuelve 200/201.
func (c *Client) AssociateContactToCompany(ctx context.Context, contactID, companyID domain.RecordID) error {
	if contactID == "" || companyID == "" {
		return nil
	}
	path := fmt.Sprintf("/crm/v4/objects/contacts/%s/associations/default/companies/%s",
		string(contactID), string(companyID))
	return c.do(ctx, http.MethodPut, path, nil, nil)
}

// AssociateObjects: version generica de AssociateContactToCompany para
// cualquier par de objects. P1 lo usaba en createKey.js para asociar
// Key (2-32450705) <-> Asesor (2-32448565) y Key <-> Colegio (0-2).
func (c *Client) AssociateObjects(ctx context.Context, fromTypeID string, fromID domain.RecordID, toTypeID string, toID domain.RecordID) error {
	if fromID == "" || toID == "" {
		return nil
	}
	path := fmt.Sprintf("/crm/v4/objects/%s/%s/associations/default/%s/%s",
		fromTypeID, string(fromID), toTypeID, string(toID))
	return c.do(ctx, http.MethodPut, path, nil, nil)
}

// FindObjectByProp busca un object del custom type por una propiedad EQ.
// Reusa el helper searchByProperty (defensive matching). "" si no existe.
func (c *Client) FindObjectByProp(ctx context.Context, typeID, keyProp, keyValue string) (domain.RecordID, error) {
	if keyValue == "" {
		return "", nil
	}
	return c.searchByProperty(ctx, "/crm/v3/objects/"+typeID+"/search", keyProp, keyValue)
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
